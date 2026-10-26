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
	"webtuidb/internal/store"
)

// ---- schemas ---------------------------------------------------------------------------------

// newSchema is new_schema (and the item menu): CREATE SCHEMA through the safe-mode gate.
func (s *sidebar) newSchema() tea.Cmd {
	if !s.w.caps.Schemas {
		return s.w.a.say("%s has no schemas (a MySQL or ClickHouse schema is a database)", s.w.conn.Driver)
	}
	f := newForm("New schema", []formField{{Key: "name", Label: "Name", Kind: fText}}, func(v map[string]string) tea.Cmd {
		name := strings.TrimSpace(v["name"])
		return s.schemaAdmin(db.OpCreateSchema, name, nil, "created")
	})
	f.validate = func(v map[string]string) error {
		if strings.TrimSpace(v["name"]) == "" {
			return fmt.Errorf("a schema needs a name")
		}
		return nil
	}
	s.w.a.push(f)
	return nil
}

func (s *sidebar) schemaAdmin(op db.AdminOp, name string, opts map[string]string, done string) tea.Cmd {
	stmts, err := s.w.sess.PlanAdmin(op, db.ObjRef{Schema: name, Name: name}, opts)
	if err != nil {
		return s.w.a.fail(err)
	}
	return s.w.execute(stmts, func(err error) tea.Cmd {
		if err != nil {
			return nil
		}
		if op == db.OpDropSchema && name == s.schema { // the open schema is gone: fall back to the first other one
			s.schema = s.w.sess.CurrentSchema()
		}
		s.sel, s.top = 0, 0
		return tea.Batch(s.load(), s.w.a.say("schema %s %s", name, done))
	})
}

// dropSchema picks a schema, then asks for its name before dropping it.
func (s *sidebar) dropSchema() tea.Cmd {
	if !s.w.caps.Schemas {
		return s.w.a.say("%s has no schemas", s.w.conn.Driver)
	}
	var items []pickItem
	for _, sc := range s.schemas {
		items = append(items, pickItem{Label: sc, Value: sc})
	}
	s.w.a.push(&pickerDlg{title: "Drop which schema?", filter: true, items: items, empty: "no schemas", onPick: func(it pickItem, _ bool) tea.Cmd {
		name := it.Value.(string)
		f := newForm("Drop schema "+name+"?", []formField{
			{Key: "cascade", Label: "Cascade (drops what is in it)", Kind: fBool, Value: "false"},
			{Kind: fLabel, Value: "This cannot be undone. Type the name to confirm."},
			{Key: "confirm", Label: "Type its name", Kind: fText},
		}, func(v map[string]string) tea.Cmd {
			return s.schemaAdmin(db.OpDropSchema, name, map[string]string{"cascade": v["cascade"]}, "dropped")
		})
		f.validate = func(v map[string]string) error {
			if v["confirm"] != name {
				return fmt.Errorf("type %q to confirm", name)
			}
			return nil
		}
		f.idx = 2
		f.submit = "Enter: drop"
		s.w.a.push(f)
		return nil
	}})
	return nil
}

// ---- custom item groups --------------------------------------------------------------------------

// groupsOf names the custom groups holding the object key.
func (s *sidebar) groupsOf(key string) []string {
	var out []string
	for _, g := range s.w.a.d.State.Groups(s.w.conn.Name) {
		if contains(g.Items, key) {
			out = append(out, g.Name)
		}
	}
	return out
}

// editGroups changes the connection's groups and forgets the ones left empty.
func (s *sidebar) editGroups(f func([]store.ItemGroup) []store.ItemGroup) error {
	var keep []store.ItemGroup
	for _, g := range f(s.w.a.d.State.Groups(s.w.conn.Name)) {
		if len(g.Items) > 0 {
			keep = append(keep, g)
		}
	}
	return s.w.a.d.State.SetGroups(s.w.conn.Name, keep)
}

func (s *sidebar) addToGroup(ref db.ObjRef) tea.Cmd {
	key := objKey(ref)
	add := func(name string) tea.Cmd {
		err := s.editGroups(func(gs []store.ItemGroup) []store.ItemGroup {
			for i := range gs {
				if gs[i].Name == name {
					if !contains(gs[i].Items, key) {
						gs[i].Items = append(gs[i].Items, key)
					}
					return gs
				}
			}
			return append(gs, store.ItemGroup{Name: name, Items: []string{key}})
		})
		if err != nil {
			return s.w.a.fail(err)
		}
		return s.w.a.say("%s is in group %s", ref.Name, name)
	}
	items := []pickItem{{Label: "＋ New group…", Value: ""}}
	for _, g := range s.w.a.d.State.Groups(s.w.conn.Name) {
		items = append(items, pickItem{Label: g.Name, Detail: plural(len(g.Items), "item"), Value: g.Name})
	}
	s.w.a.push(&pickerDlg{title: "Add " + ref.Name + " to a group", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		if name := it.Value.(string); name != "" {
			return add(name)
		}
		s.w.a.push(newForm("New group", []formField{{Key: "name", Label: "Name", Kind: fText}}, func(v map[string]string) tea.Cmd {
			if n := strings.TrimSpace(v["name"]); n != "" {
				return add(n)
			}
			return nil
		}))
		return nil
	}})
	return nil
}

func (s *sidebar) removeFromGroup(ref db.ObjRef, group string) tea.Cmd {
	key := objKey(ref)
	err := s.editGroups(func(gs []store.ItemGroup) []store.ItemGroup {
		for i := range gs {
			if gs[i].Name != group {
				continue
			}
			var keep []string
			for _, it := range gs[i].Items {
				if it != key {
					keep = append(keep, it)
				}
			}
			gs[i].Items = keep
		}
		return gs
	})
	if err != nil {
		return s.w.a.fail(err)
	}
	return s.w.a.say("%s left group %s", ref.Name, group)
}

// ---- multi-select ------------------------------------------------------------------------------------

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
			return fmt.Errorf("type %q to confirm", word)
		}
		return nil
	}
	f.idx = 3
	f.submit = "Enter: " + verb
	s.w.a.push(f)
	return nil
}

// ---- export: several tables, several schemas, one file each ----------------------------------------------

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
