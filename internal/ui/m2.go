package ui

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
	"webtuidb/internal/export"
	"webtuidb/internal/importer"
)

// liveMsg runs f on the UI loop. Background jobs report through it because wsMsg results wait
// while a dialog is open, and these jobs feed dialogs.
type liveMsg struct{ f func() tea.Cmd }

// pump turns a channel of UI-loop callbacks into messages, one at a time, until it is closed.
func pump(ch <-chan func() tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		f, ok := <-ch
		if !ok {
			return nil
		}
		return liveMsg{func() tea.Cmd { return tea.Batch(f(), pump(ch)) }}
	}
}

// liveCmd runs f off the UI loop; the callback it returns runs on it.
func liveCmd(f func() func() tea.Cmd) tea.Cmd {
	return func() tea.Msg { return liveMsg{f()} }
}

// liveCmd is the package liveCmd with the workspace's generation captured now: a result that
// arrives after the workspace closed or switched database/session is dropped, not shown.
func (w *Workspace) liveCmd(f func() func() tea.Cmd) tea.Cmd {
	gen := w.gen
	return liveCmd(func() func() tea.Cmd {
		cb := f()
		return func() tea.Cmd {
			if w.closed || w.gen != gen {
				return nil
			}
			return cb()
		}
	})
}

// job runs work in the background (one at a time per workspace; prefix . cancels it). work reports
// progress as UI-loop callbacks; then runs on success and returns the final message.
func (w *Workspace) job(label string, work func(ctx context.Context, sess db.Session, report func(func() tea.Cmd)) error, then func() (string, tea.Cmd)) tea.Cmd {
	if w.jobCancel != nil {
		return w.a.say("another export, import or search is still running (cancel it first)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.jobCancel = cancel
	je := w.addJob(label, cancel) // the jobs centre (alt+J) lists it until dismissed
	ch, sess, start := make(chan func() tea.Cmd, 16), w.sess, time.Now()
	go func() {
		defer close(ch)
		var err error
		func() { // a panic in work must still end the job, or the workspace stays busy for good
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("internal error: %v", r)
				}
			}()
			err = work(ctx, sess, func(f func() tea.Cmd) { ch <- je.noting(w, f) })
		}()
		stopped := err != nil && ctx.Err() != nil // decided when the work ended, not when the callback runs
		ch <- func() tea.Cmd {
			cancel()
			w.jobCancel = nil
			w.log("-- "+label, time.Since(start), err)
			switch {
			case stopped:
				return tea.Batch(w.a.say("%s: stopped", label), je.finish(w, nil, true))
			case err != nil:
				return tea.Batch(w.a.fail(fmt.Errorf("%s: %w", label, err)), je.finish(w, err, false))
			}
			msg, cmd := then()
			return tea.Batch(w.a.say("%s", msg), cmd, je.finish(w, nil, false))
		}
	}()
	return tea.Batch(w.a.say("%s…", label), pump(ch))
}

// targetRef is the table the sidebar or the open table tab points at.
func (w *Workspace) targetRef() (db.ObjRef, bool) {
	if w.focusSB {
		if r, ok := w.sb.current(); ok && r.kind == sbObject {
			switch r.obj.Kind {
			case db.KindTable, db.KindView, db.KindMatView:
				return r.obj.ObjRef, true
			}
		}
		return db.ObjRef{}, false
	}
	if t, ok := w.tab().(*tableTab); ok {
		return t.ref, true
	}
	return db.ObjRef{}, false
}

// gridResult is the loaded page of a grid as a Result (deleted rows left out).
func gridResult(g *Grid) *db.Result {
	res := &db.Result{HasRows: true}
	for _, c := range g.Cols {
		res.Columns = append(res.Columns, db.ResultColumn{Name: c.Name, Type: c.Type})
	}
	for _, r := range g.Rows {
		if r.State != rowDeleted {
			res.Rows = append(res.Rows, r.Cur)
		}
	}
	return res
}

// ---- export -----------------------------------------------------------------------------

func (w *Workspace) exportAction() tea.Cmd {
	var (
		name    string
		ref     *db.ObjRef
		filters db.FilterSet
		cols    []string
		res     *db.Result // a query result, or the current page of a table
		page    *db.Result
	)
	q, _ := w.tab().(*queryTab)
	switch t, isTable := w.tab().(*tableTab); {
	case w.focusSB || w.tab() == nil:
		if refs := w.sb.markedRefs(); len(refs) > 0 { // marked tables: one file each
			return w.exportTables(refs)
		}
		if r, ok := w.targetRef(); ok {
			ref, name = &r, r.Name
		}
	case isTable && t.view == viewData:
		r := t.ref
		ref, name, filters, cols, page = &r, r.Name, t.applied(), t.visibleCols(), gridResult(t.grid)
	case q != nil && q.curResult() != nil && q.curResult().raw != nil:
		name, res = "result", q.curResult().raw
	}
	if ref == nil && res == nil {
		return w.a.say("open a table or run a query first")
	}
	var all []*db.Result // every result tab with rows
	if q != nil && res != nil {
		for _, r := range q.res {
			if r.raw != nil {
				all = append(all, r.raw)
			}
		}
	}
	cfg := w.a.d.Cfg
	optsOf := func(v map[string]string) export.Options {
		f := export.Format(v["format"])
		csv := cfg.CSV
		if f == "csv" {
			csv.NullAs = v["null_as"]
		}
		if v["line_break"] == "CRLF" {
			csv.LineBreak = "\r\n"
		} else if v["line_break"] == "LF" {
			csv.LineBreak = "\n"
		}
		return export.Options{Format: f, CSV: csv, Columns: cols, Gzip: v["gzip"] == "true" && f != "xlsx", Table: name, Dialect: w.caps.Dialect,
			Structure: v["structure"] == "true" && f == "sql", Drop: v["drop"] == "true" && f == "sql"}
	}
	sql := func(v map[string]string) bool { return v["format"] == "sql" }
	isCSV := func(v map[string]string) bool { return v["format"] == "csv" }
	lb := "LF"
	if strings.Contains(cfg.CSV.LineBreak, "\r") {
		lb = "CRLF"
	}
	fields := []formField{
		{Key: "format", Label: "Format", Kind: fChoice, Options: []string{"csv", "json", "sql", "xlsx"}, Value: "csv"},
		{Key: "null_as", Label: "NULL written as", Kind: fText, Value: cfg.CSV.NullAs, Show: isCSV},
		{Key: "line_break", Label: "Line breaks", Kind: fChoice, Options: []string{"LF", "CRLF"}, Value: lb, Show: isCSV},
		{Kind: fLabel, Value: "⚠ " + export.CheckCSV(cfg.CSV), Show: func(v map[string]string) bool { return isCSV(v) && export.CheckCSV(cfg.CSV) != "" }},
		{Key: "gzip", Label: "Gzip", Kind: fBool, Show: func(v map[string]string) bool { return v["format"] != "xlsx" }},
	}
	if ref != nil {
		fields = append(fields,
			formField{Key: "structure", Label: "Include structure", Kind: fBool, Show: sql},
			formField{Key: "drop", Label: "Drop table first", Kind: fBool, Show: sql},
			formField{Key: "page", Label: "Current page only", Kind: fBool})
		if w.caps.Schemas {
			fields = append(fields, formField{Key: "many", Label: "Pick several tables (all schemas, one file each)", Kind: fBool})
		}
	}
	if len(all) > 1 {
		fields = append(fields, formField{Key: "all", Label: "All result tabs (one file each: name-1, name-2...)", Kind: fBool})
	}
	auto := w.exportName(name, "csv")
	fields = append(fields, formField{Key: "path", Label: "File", Kind: fText, Value: auto})
	f := newForm("Export "+name, fields, func(v map[string]string) tea.Cmd {
		if v["many"] == "true" {
			return w.pickExportTables(ref)
		}
		path, o := strings.TrimSpace(v["path"]), optsOf(v)
		run := func() tea.Cmd {
			if v["all"] == "true" {
				return w.runExportAll(path, o, all)
			}
			if ref != nil && v["page"] != "true" {
				return w.runExport(path, o, ref, filters, nil)
			}
			if res == nil {
				res = page
			}
			return w.runExport(path, o, nil, filters, res)
		}
		if v["all"] == "true" {
			paths := make([]string, len(all))
			for i := range all {
				paths[i] = numberedPath(path, i+1)
			}
			return w.a.ifFree(run, paths...)
		}
		return w.a.ifFree(run, path)
	})
	f.validate = func(v map[string]string) error {
		if strings.TrimSpace(v["path"]) == "" {
			return fmt.Errorf("file name required")
		}
		return nil
	}
	f.onChange = func(key string, v map[string]string) { // the default name follows the format until the user edits it
		if key == "path" {
			return
		}
		next := w.exportName(name, export.Ext(optsOf(v)))
		for _, fl := range f.fields {
			if fl.Key == "path" && fl.edit.Value() == auto {
				fl.edit = newLine(next)
			}
		}
		auto = next
	}
	w.a.push(f)
	return nil
}

// runExport writes a whole table (ref set, streamed with the filters) or a result to path.
func (w *Workspace) runExport(path string, o export.Options, ref *db.ObjRef, filters db.FilterSet, res *db.Result) tea.Cmd {
	var rows int64
	return w.job("export "+filepath.Base(path), func(ctx context.Context, sess db.Session, report func(func() tea.Cmd)) error {
		if o.Structure && ref != nil {
			ddl, err := sess.DDL(ctx, *ref)
			if err != nil {
				return err
			}
			o.DDL = ddl
		}
		return writeAtomic(path, func(f io.Writer) error { // never leave a half-written file that looks complete
			if ref != nil {
				return export.Table(ctx, sess, *ref, filters, o, f, func(n int64) {
					rows = n
					report(func() tea.Cmd { return w.a.say("exporting… %d rows", n) })
				})
			}
			rows = int64(len(res.Rows))
			return export.Write(f, res, o)
		})
	}, func() (string, tea.Cmd) { return fmt.Sprintf("exported %d rows to %s", rows, path), nil })
}

// ---- import -----------------------------------------------------------------------------

func (w *Workspace) importAction() tea.Cmd {
	ref, hasRef := w.targetRef()
	return w.importWizard(ref, hasRef)
}

// importWizard is the import dialog aimed at ref (hasRef: it is a real target, not just a default).
func (w *Workspace) importWizard(ref db.ObjRef, hasRef bool) tea.Cmd {
	csv := func(v map[string]string) bool { return v["source"] == "csv" }
	notSQL := func(v map[string]string) bool { return v["source"] != "sql" }
	f := newForm("Import data", []formField{
		{Key: "path", Label: "File", Kind: fText},
		{Key: "source", Label: "Format", Kind: fChoice, Options: []string{"csv", "json", "sql"}, Value: "csv"},
		{Key: "header", Label: "First row is the header", Kind: fBool, Value: "true", Show: csv},
		{Key: "delim", Label: "Delimiter", Kind: fText, Value: w.a.d.Cfg.CSV.Delimiter, Show: csv},
		{Key: "create", Label: "Create the table", Kind: fBool, Show: notSQL},
		{Key: "table", Label: "Table", Kind: fText, Value: ref.Name, Show: notSQL},
	}, func(v map[string]string) tea.Cmd {
		path := strings.TrimSpace(v["path"])
		o := importer.Options{Source: importer.Source(v["source"]), CSV: w.a.d.Cfg.CSV, Header: v["header"] == "true", CreateTable: v["create"] == "true"}
		if d := v["delim"]; d != "" {
			o.CSV.Delimiter = d
		}
		o.Table = ref
		if !hasRef || ref.Name != v["table"] {
			o.Table = db.ObjRef{Schema: w.sess.CurrentSchema(), Name: strings.TrimSpace(v["table"]), Kind: db.KindTable}
		}
		if o.Source == "sql" {
			return w.startImport(path, o)
		}
		file, err := os.Open(path)
		if err != nil {
			return w.a.fail(err)
		}
		cols, rows, err := importer.Preview(file, o, 100)
		file.Close()
		if err != nil {
			return w.a.fail(err)
		}
		if o.CreateTable { // the new table's columns are the file's
			return w.mapColumns(path, o, cols, rows, cols)
		}
		if known, ok := w.meta[o.Table]; ok {
			return w.mapColumns(path, o, cols, rows, columnNames(known))
		}
		sess, tbl := w.sess, o.Table
		return w.liveCmd(func() func() tea.Cmd {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			have, err := sess.Columns(ctx, tbl)
			return func() tea.Cmd {
				if err != nil {
					return w.a.fail(err)
				}
				return w.mapColumns(path, o, cols, rows, columnNames(have))
			}
		})
	})
	f.validate = func(v map[string]string) error {
		if st, err := os.Stat(strings.TrimSpace(v["path"])); err != nil || st.IsDir() {
			return fmt.Errorf("cannot read that file")
		}
		if v["source"] != "sql" && strings.TrimSpace(v["table"]) == "" {
			return fmt.Errorf("table name required")
		}
		return nil
	}
	f.onChange = func(key string, v map[string]string) { // pick the format from the file extension
		if key != "path" {
			return
		}
		for _, fl := range f.fields {
			if fl.Key == "source" {
				switch strings.ToLower(filepath.Ext(v["path"])) {
				case ".json":
					fl.Value = "json"
				case ".sql":
					fl.Value = "sql"
				case ".csv", ".tsv", ".txt":
					fl.Value = "csv"
				}
			}
		}
	}
	w.a.push(f)
	return nil
}

func columnNames(cols []db.Column) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Name
	}
	return out
}

// mapColumns shows the preview and one choice per file column: a table column, or skip.
func (w *Workspace) mapColumns(path string, o importer.Options, fileCols []string, rows [][]string, tableCols []string) tea.Cmd {
	const skip = "(skip)"
	fields := []formField{{Kind: fHeading, Value: fmt.Sprintf("Preview (%d rows read)", len(rows))}}
	for i, r := range rows {
		if i == 5 {
			break
		}
		fields = append(fields, formField{Kind: fLabel, Value: fit(strings.Join(r, " | "), 60)})
	}
	fields = append(fields, formField{Kind: fHeading, Value: "Column mapping: file column → " + o.Table.Name})
	for i, c := range fileCols {
		def := skip
		for _, tc := range tableCols {
			if strings.EqualFold(tc, c) {
				def = tc
			}
		}
		fields = append(fields, formField{Key: fmt.Sprint(i), Label: c, Kind: fChoice, Options: append([]string{skip}, tableCols...), Value: def})
	}
	f := newForm("Import "+filepath.Base(path), fields, func(v map[string]string) tea.Cmd {
		o.Mapping = map[string]string{}
		for i, c := range fileCols {
			if v[fmt.Sprint(i)] != skip {
				o.Mapping[c] = v[fmt.Sprint(i)]
			}
		}
		for _, c := range fileCols { // importer: a listed empty value skips the column
			if _, ok := o.Mapping[c]; !ok {
				o.Mapping[c] = ""
			}
		}
		return w.startImport(path, o)
	})
	f.submit = "Enter: import"
	f.validate = func(v map[string]string) error { // two file columns into one table column would fail mid-import
		from := map[string]string{}
		for i, c := range fileCols {
			t := v[fmt.Sprint(i)]
			if t == skip || t == "" {
				continue
			}
			if prev, dup := from[strings.ToLower(t)]; dup {
				return fmt.Errorf("file columns %q and %q both map to %q", prev, c, t)
			}
			from[strings.ToLower(t)] = c
		}
		return nil
	}
	w.a.push(f)
	return nil
}

func (w *Workspace) startImport(path string, o importer.Options) tea.Cmd {
	// importing writes to the table, so it goes through safe mode like any other write
	gate := []db.Statement{{Text: "IMPORT " + path + " INTO " + o.Table.Name}}
	return w.gate(gate, func() tea.Cmd {
		var rows int64
		return w.job("import "+filepath.Base(path), func(ctx context.Context, sess db.Session, report func(func() tea.Cmd)) error {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			return importer.Run(ctx, sess, f, o, func(p importer.Progress) {
				rows = p.Rows
				report(func() tea.Cmd { return w.a.say("importing… %d rows in %d batches", p.Rows, p.Batches) })
			})
		}, func() (string, tea.Cmd) {
			var cmds []tea.Cmd
			if o.CreateTable {
				w.forget(nil)
				cmds = append(cmds, w.sb.load())
			}
			for _, t := range w.tabs {
				if tt, ok := t.(*tableTab); ok && tt.ref == o.Table {
					cmds = append(cmds, tt.load())
				}
			}
			return fmt.Sprintf("imported %d rows into %s", rows, o.Table.Name), tea.Batch(cmds...)
		})
	})
}

// ---- search in database -----------------------------------------------------------------

func (w *Workspace) searchInDatabase() tea.Cmd {
	f := newForm("Search in database", []formField{
		{Key: "value", Label: "Value", Kind: fText},
		{Key: "op", Label: "Operator", Kind: fChoice, Options: []string{"Contains", "Not contains", "=", "Has prefix", "Has suffix"}, Value: "Contains"},
		{Key: "tables", Label: "Tables", Kind: fText, Hint: "empty = all tables, or a, b, c"},
	}, func(v map[string]string) tea.Cmd {
		if w.jobCancel != nil {
			return w.a.say("another export, import or search is still running (cancel it first)")
		}
		o := db.SearchOptions{Value: v["value"], Op: v["op"]}
		for _, n := range strings.Split(v["tables"], ",") {
			if n = strings.TrimSpace(n); n != "" {
				o.Tables = append(o.Tables, n)
			}
		}
		total := len(o.Tables)
		if total == 0 { // the sidebar lists the current schema's objects
			for _, ob := range w.sb.objs {
				if ob.Kind == db.KindTable {
					total++
				}
			}
		}
		var hits []db.SearchHit
		done := 0
		p := &pickerDlg{empty: "no hits yet", hint: "Enter: open the table with the filter   Esc: stop",
			onClose: func() {
				if w.jobCancel != nil {
					w.jobCancel()
				}
			}}
		title := func(state string) {
			p.title = fmt.Sprintf("Search %q: %d/%d tables · %d with hits%s", o.Value, done, total, len(hits), state)
		}
		title("")
		p.onPick = func(it pickItem, _ bool) tea.Cmd {
			if w.jobCancel != nil {
				w.jobCancel()
			}
			h := it.Value.(db.SearchHit)
			return w.openFiltered(h.Ref, h.Filters)
		}
		w.a.push(p)
		return w.job("search "+o.Value, func(ctx context.Context, sess db.Session, report func(func() tea.Cmd)) error {
			return db.Search(ctx, sess, o, func(h db.SearchHit) {
				report(func() tea.Cmd {
					done++
					if h.Count > 0 || h.Err != nil {
						hits = append(hits, h)
						detail := fmt.Sprintf("%d rows", h.Count)
						if h.Err != nil {
							detail = "error: " + h.Err.Error()
						}
						p.items = append(p.items, pickItem{Label: h.Ref.Name, Detail: detail, Value: h})
					}
					title("")
					return nil
				})
			})
		}, func() (string, tea.Cmd) {
			title(" · done")
			return fmt.Sprintf("search finished: %d table(s) with hits", len(hits)), nil
		})
	})
	w.a.push(f)
	return nil
}

// openFiltered opens a table with filters already applied in its filter bar.
func (w *Workspace) openFiltered(ref db.ObjRef, f db.FilterSet) tea.Cmd {
	w.openObject(ref, false) // its first load is dropped: applyFilters loads with the filters in place
	t, ok := w.tab().(*tableTab)
	if !ok || t.ref != ref {
		return nil
	}
	t.filters, t.showFilters = f, true
	return t.applyFilters()
}
