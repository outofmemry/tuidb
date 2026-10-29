// sidebar_marks.go: Sidebar marks: marking several items, bulk admin, exporting the marked tables.

package ui

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
	"webtuidb/internal/export"
)

// toggleMark is mark: tick or untick the table under the cursor, then step down.
func (s *sidebar) toggleMark() tea.Cmd {
	r, ok := s.current()
	if !ok || r.kind != sbObject {
		return nil
	}
	switch r.obj.Kind {
	case db.KindTable, db.KindView, db.KindMatView:
	default:
		return s.w.a.say("only tables and views can be marked")
	}
	if s.marks == nil {
		s.marks = map[string]bool{}
	}
	k := objKey(r.obj.ObjRef)
	if s.marks[k] {
		delete(s.marks, k)
	} else {
		s.marks[k] = true
	}
	s.step(1)
	return nil
}

// markedRefs lists the marked tables in sidebar order.
func (s *sidebar) markedRefs() []db.ObjRef {
	var out []db.ObjRef
	for _, o := range s.objs {
		if s.marks[objKey(o.ObjRef)] {
			out = append(out, o.ObjRef)
		}
	}
	return out
}

// adminMany drops or truncates every marked table in one batch (one gate, one confirmation).
func (s *sidebar) adminMany(op db.AdminOp, refs []db.ObjRef) tea.Cmd {
	verb := map[db.AdminOp]string{db.OpDropObject: "delete", db.OpTruncate: "truncate"}[op]
	var names []string
	for _, r := range refs {
		names = append(names, r.Name)
	}
	word := fmt.Sprint(len(refs)) // the count, not the generic verb: a stale mark cannot be confirmed by habit
	f := newForm(strings.ToUpper(verb[:1])+verb[1:]+" "+plural(len(refs), "table")+"?", []formField{
		{Kind: fLabel, Value: strings.Join(names, ", ")},
		{Key: "cascade", Label: "Cascade", Kind: fBool, Value: "false"},
		{Kind: fLabel, Value: "This cannot be undone. Type " + word + " (the number of tables) to confirm."},
		{Key: "confirm", Label: "Type " + word, Kind: fText},
	}, func(v map[string]string) tea.Cmd {
		var stmts []db.Statement
		for _, r := range refs {
			st, err := s.w.sess.PlanAdmin(op, r, map[string]string{"cascade": v["cascade"]})
			if err != nil {
				return s.w.a.fail(err)
			}
			stmts = append(stmts, st...)
		}
		return s.w.execute(stmts, func(err error) tea.Cmd {
			if err != nil { // a MySQL batch may be half applied: show what is there now
				return s.load()
			}
			for _, r := range refs {
				s.w.closeTabsFor(r, op == db.OpDropObject)
			}
			s.marks = nil
			return tea.Batch(s.load(), s.w.a.say("%s done for %s", verb, plural(len(refs), "table")))
		})
	})
	f.validate = func(v map[string]string) error {
		if v["confirm"] != word {
			return fmt.Errorf("type “%s” to confirm", word)
		}
		return nil
	}
	f.idx = 3
	f.submit = "Enter: " + verb
	s.w.a.push(f)
	return nil
}

// pickExportTables lists the tables of every schema; the ticked ones are exported together.
func (w *Workspace) pickExportTables(first *db.ObjRef) tea.Cmd {
	sess, sysOK := w.sess, w.a.d.Cfg.Sidebar.LoadSystemSchemas
	cur := w.sess.CurrentSchema()
	return liveCmd(func() func() tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var refs []db.ObjRef
		schemas, err := sess.Schemas(ctx)
		for _, sc := range schemas {
			if err != nil || !sysOK && sc != cur && isSystemSchema(sc) {
				continue
			}
			var objs []db.Object
			if objs, err = sess.Objects(ctx, sc); err != nil {
				break
			}
			for _, o := range objs {
				if o.Kind == db.KindTable {
					refs = append(refs, o.ObjRef)
				}
			}
		}
		return func() tea.Cmd {
			if err != nil {
				return w.a.fail(err)
			}
			var items []checkItem
			for _, r := range refs {
				items = append(items, checkItem{Label: r.Schema + "." + r.Name, Checked: first != nil && *first == r})
			}
			w.a.push(&checklistDlg{title: "Export which tables? (all schemas)", items: items, onOK: func(items []checkItem) tea.Cmd {
				var picked []db.ObjRef
				for i, it := range items {
					if it.Checked {
						picked = append(picked, refs[i])
					}
				}
				if len(picked) == 0 {
					return w.a.say("no table picked")
				}
				return w.exportTables(picked)
			}})
			return nil
		}
	})
}

// exportTables writes each table to its own file in one folder, in one background job.
func (w *Workspace) exportTables(refs []db.ObjRef) tea.Cmd {
	cfg := w.a.d.Cfg
	isCSV := func(v map[string]string) bool { return v["format"] == "csv" }
	fields := []formField{
		{Key: "format", Label: "Format", Kind: fChoice, Options: []string{"csv", "json", "sql", "xlsx"}, Value: "csv"},
		{Key: "gzip", Label: "Gzip", Kind: fBool, Show: func(v map[string]string) bool { return v["format"] != "xlsx" }},
		{Key: "structure", Label: "Include structure", Kind: fBool, Show: func(v map[string]string) bool { return v["format"] == "sql" }},
	}
	if warn := export.CheckCSV(cfg.CSV); warn != "" {
		fields = append(fields, formField{Kind: fLabel, Value: "⚠ " + warn, Show: isCSV})
	}
	fields = append(fields, formField{Key: "folder", Label: "Folder", Kind: fText, Value: "."})
	f := newForm("Export "+plural(len(refs), "table")+": one file each", fields, func(v map[string]string) tea.Cmd {
		folder := strings.TrimSpace(v["folder"])
		fm := export.Format(v["format"])
		base := export.Options{Format: fm, CSV: cfg.CSV, Gzip: v["gzip"] == "true" && fm != "xlsx", Dialect: w.caps.Dialect, Structure: v["structure"] == "true" && fm == "sql"}
		ext := export.Ext(base)
		paths := make([]string, len(refs))
		for i, r := range refs {
			name := r.Name
			if r.Schema != "" && w.caps.Schemas {
				name = r.Schema + "." + r.Name
			}
			paths[i] = filepath.Join(folder, w.exportName(name, ext))
		}
		run := func() tea.Cmd {
			if err := os.MkdirAll(folder, 0o755); err != nil {
				return w.a.fail(err)
			}
			w.jobFile = paths[0]
			return w.job("export "+plural(len(refs), "table"), func(ctx context.Context, sess db.Session, report func(func() tea.Cmd)) error {
				for i, r := range refs {
					o := base
					if o.Structure {
						ddl, err := sess.DDL(ctx, r)
						if err != nil {
							return err
						}
						o.DDL = ddl
					}
					err := writeAtomic(paths[i], func(out io.Writer) error { return export.Table(ctx, sess, r, db.FilterSet{}, o, out, nil) })
					if err != nil {
						return fmt.Errorf("%s: %w", r.Name, err)
					}
					n := i + 1
					report(func() tea.Cmd { return w.a.say("exported %d of %d tables…", n, len(refs)) })
				}
				return nil
			}, func() (string, tea.Cmd) {
				return fmt.Sprintf("exported %s to %s", plural(len(refs), "table"), folder), nil
			})
		}
		return w.a.ifFree(run, paths...)
	})
	f.validate = func(v map[string]string) error {
		if strings.TrimSpace(v["folder"]) == "" {
			return fmt.Errorf("folder required")
		}
		return nil
	}
	w.a.push(f)
	return nil
}
