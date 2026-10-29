// import.go: Import wizard: pick a file, map its columns, load it into a table.

package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
	"webtuidb/internal/importer"
)

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
		{Key: "encoding", Label: "File encoding", Kind: fChoice, Options: []string{"utf-8", "latin1", "windows-1252", "utf-16le", "utf-16be", "shift_jis", "gbk"}, Value: "utf-8"},
		{Key: "nullas", Label: "NULL as", Kind: fText, Hint: "cell text that becomes NULL (empty: the [csv] null_as setting)", Show: notSQL},
		{Key: "emptynull", Label: "Empty is NULL", Kind: fBool, Value: "false", Show: notSQL},
		{Key: "keepquote", Label: "Keep formula apostrophe", Kind: fBool, Value: "false", Show: notSQL},
		{Key: "create", Label: "Create the table", Kind: fBool, Show: notSQL},
		{Key: "table", Label: "Table", Kind: fText, Value: ref.Name, Show: notSQL},
	}, nil)
	for _, fl := range f.fields { // the last answers come back (a failed or finished import must not cost the typing)
		if val, ok := w.lastImport[fl.Key]; ok && !(fl.Key == "table" && hasRef) {
			fl.Value = val
			if fl.Kind == fText {
				fl.edit = newLine(val)
			}
		}
	}
	opts := func(v map[string]string) importer.Options {
		o := importer.Options{Source: importer.Source(v["source"]), CSV: w.a.d.Cfg.CSV, Header: v["header"] == "true", CreateTable: v["create"] == "true", Encoding: v["encoding"], Driver: w.conn.Driver,
			NullAs: v["nullas"], EmptyIsNull: v["emptynull"] == "true", KeepFormulaQuote: v["keepquote"] == "true"}
		if d := v["delim"]; d != "" {
			o.CSV.Delimiter = d
		}
		o.Table = ref
		if !hasRef || ref.Name != v["table"] {
			o.Table = db.ObjRef{Schema: w.sess.CurrentSchema(), Name: strings.TrimSpace(v["table"]), Kind: db.KindTable}
		}
		return o
	}
	var cols []string // the preview validate read
	var rows [][]string
	f.onSubmit = func(v map[string]string) tea.Cmd {
		path, o, cols, rows := strings.TrimSpace(v["path"]), opts(v), cols, rows
		if o.Source == "sql" {
			return w.startImport(path, o)
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
	}
	f.validate = func(v map[string]string) error {
		w.lastImport = v
		path := strings.TrimSpace(v["path"])
		if st, err := os.Stat(path); err != nil || st.IsDir() {
			return fmt.Errorf("cannot read that file")
		}
		if v["source"] == "sql" {
			return nil
		}
		if strings.TrimSpace(v["table"]) == "" {
			return fmt.Errorf("table name required")
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		pctx, pcancel := context.WithTimeout(context.Background(), 30*time.Second) // IO-7: a preview must not hang on a huge file
		defer pcancel()
		cols, rows, err = importer.PreviewContext(pctx, file, opts(v), 100) // a file the importer cannot read keeps the form open
		return err
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
			return fmt.Sprintf("processed %d rows into %s", rows, o.Table.Name), tea.Batch(cmds...)
		})
	})
}
