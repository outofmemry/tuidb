// export.go: Exporting a table, a result or a selection to a file (CSV, JSON, SQL ...), and the export forms.

package ui

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
	"webtuidb/internal/export"
)

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
		nullAs := ""
		if f == "csv" {
			csv.NullAs, nullAs = v["null_as"], v["null_as"]
		}
		if v["line_break"] == "CRLF" {
			csv.LineBreak = "\r\n"
		} else if v["line_break"] == "LF" {
			csv.LineBreak = "\n"
		}
		return export.Options{Format: f, CSV: csv, Columns: cols, Gzip: v["gzip"] == "true" && f != "xlsx", Table: name, Dialect: w.caps.Dialect,
			NoHeader: f == "csv" && v["header"] == "false", NullAs: nullAs,
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
		{Key: "header", Label: "Header row", Kind: fBool, Value: fmt.Sprint(cfg.Export.Header), Show: isCSV},
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
		path, o := w.expandPath(strings.TrimSpace(v["path"]), name), optsOf(v)
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

// expandPath lets the File field hold a pattern: {connection} {database} {table} {tag} {date} {time} in the
// file name part are expanded (the folder is kept as typed).
func (w *Workspace) expandPath(path, table string) string {
	if !strings.Contains(path, "{") {
		return path
	}
	dir, base := filepath.Split(path)
	ext := filepath.Ext(base)
	return dir + export.Filename(strings.TrimSuffix(base, ext), w.exportVars(table)) + ext
}

// runExport writes a whole table (ref set, streamed with the filters) or a result to path.
func (w *Workspace) runExport(path string, o export.Options, ref *db.ObjRef, filters db.FilterSet, res *db.Result) tea.Cmd {
	var rows int64
	w.jobFile = path
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

// numberedPath puts -n before the extension: out.csv.gz, 2 -> out-2.csv.gz.
func numberedPath(path string, n int) string {
	base, ext := path, ""
	for _, e := range []string{".csv.gz", ".json.gz", ".sql.gz"} {
		if strings.HasSuffix(path, e) {
			base, ext = strings.TrimSuffix(path, e), e
		}
	}
	if ext == "" {
		ext = filepath.Ext(path)
		base = strings.TrimSuffix(path, ext)
	}
	return fmt.Sprintf("%s-%d%s", base, n, ext)
}

// runExportAll writes every result tab to its own file, path-1, path-2...
func (w *Workspace) runExportAll(path string, o export.Options, results []*db.Result) tea.Cmd {
	w.jobFile = numberedPath(path, 1)
	return w.job("export "+plural(len(results), "result"), func(ctx context.Context, _ db.Session, _ func(func() tea.Cmd)) error {
		for i, res := range results {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := writeAtomic(numberedPath(path, i+1), func(f io.Writer) error { return export.Write(f, res, o) }); err != nil {
				return err
			}
		}
		return nil
	}, func() (string, tea.Cmd) {
		return fmt.Sprintf("exported %s next to %s", plural(len(results), "file"), path), nil
	})
}

// exportName expands [export] filename ({connection} {database} {table} {tag} {date} {time}) plus the extension.
func (w *Workspace) exportName(table, ext string) string {
	name := export.Filename(orDefault(w.a.d.Cfg.Export.Filename, "{table}-{date}"), w.exportVars(table))
	if strings.HasSuffix(strings.ToLower(name), "."+strings.ToLower(ext)) { // "{table}.csv" already names the extension
		return name
	}
	return name + "." + ext
}

// exportVars are the {tokens} a file-name pattern can use.
func (w *Workspace) exportVars(table string) map[string]string {
	return map[string]string{"connection": w.conn.Name, "database": w.sess.CurrentDatabase(), "table": table, "tag": w.conn.Tag}
}

// exportResultForm is the export dialog for rows that are not a table or a result tab, with Format preselected.
func (w *Workspace) exportResultForm(name string, res *db.Result, format string) tea.Cmd {
	cfg := w.a.d.Cfg
	auto := w.exportName(name, format)
	fields := []formField{
		{Key: "format", Label: "Format", Kind: fChoice, Options: []string{"csv", "json", "sql", "xlsx"}, Value: format},
		{Key: "header", Label: "Header row", Kind: fBool, Value: fmt.Sprint(cfg.Export.Header), Show: func(v map[string]string) bool { return v["format"] == "csv" }},
		{Key: "gzip", Label: "Gzip", Kind: fBool, Show: func(v map[string]string) bool { return v["format"] != "xlsx" }},
		{Key: "path", Label: "File", Kind: fText, Value: auto},
	}
	if res.Truncated || len(res.Rows) >= boardExportLimit { // the panel's rows are read in memory: the file would be partial
		fields = append(fields, formField{Kind: fLabel, Value: fmt.Sprintf("Only the first %d rows are exported: the query returns more.", len(res.Rows))})
	}
	f := newForm("Export "+name, fields, func(v map[string]string) tea.Cmd {
		fm := export.Format(v["format"])
		o := export.Options{Format: fm, CSV: cfg.CSV, Gzip: v["gzip"] == "true" && fm != "xlsx", Table: name, Dialect: w.caps.Dialect,
			NoHeader: fm == "csv" && v["header"] == "false"}
		path := w.expandPath(strings.TrimSpace(v["path"]), name)
		return w.a.ifFree(func() tea.Cmd { return w.runExport(path, o, nil, db.FilterSet{}, res) }, path)
	})
	f.validate = func(v map[string]string) error {
		if strings.TrimSpace(v["path"]) == "" {
			return fmt.Errorf("file name required")
		}
		return nil
	}
	f.onChange = func(key string, v map[string]string) {
		if key == "path" {
			return
		}
		next := w.exportName(name, v["format"])
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
