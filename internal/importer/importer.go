// Package importer loads CSV, JSON or SQL files into a table.
package importer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/transform"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

type Source string // "csv" | "json" | "sql"

type Options struct {
	Source      Source
	CSV         config.CSV
	Header      bool              // csv: first row is the header
	Mapping     map[string]string // file column -> table column; unlisted = same name, "" value = skip column
	CreateTable bool              // create the table first; all columns TEXT
	Table       db.ObjRef
	Batch       int    // rows per Apply, default 500
	EmptyIsNull bool   // an empty cell (or JSON "") becomes NULL, the way the CSV export writes it
	OnConflict  string // "" (plain insert); "ignore" and "replace" are not implemented yet
}

type Progress struct {
	Rows, Batches int64
	Err           error
}

// open returns the file's columns and a pull function over its rows (io.EOF at the end).
func open(r io.Reader, o Options) ([]string, func() ([]any, error), error) {
	switch o.Source {
	case "csv":
		return openCSV(r, o)
	case "json":
		return openJSON(r)
	}
	return nil, nil, fmt.Errorf("unknown import source %q", o.Source)
}

func openCSV(r io.Reader, o Options) ([]string, func() ([]any, error), error) {
	br := bufio.NewReader(r)
	if b, _ := br.Peek(3); string(b) == "\ufeff" {
		br.Discard(3)
	}
	cr := csv.NewReader(br)
	if d := []rune(o.CSV.Delimiter); len(d) > 0 {
		cr.Comma = d[0]
	}
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true // shortcut: Quote and LineBreak settings are ignored (encoding/csv fixes them), add a hand parser if someone needs another quote char
	rec, err := cr.Read()
	if err == io.EOF {
		return nil, func() ([]any, error) { return nil, io.EOF }, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var cols []string
	pending := rec
	if o.Header {
		cols, pending = rec, nil
	} else {
		for i := range rec {
			cols = append(cols, fmt.Sprintf("column%d", i+1))
		}
	}
	return cols, func() ([]any, error) {
		rec := pending
		pending = nil
		if rec == nil {
			if rec, err = cr.Read(); err != nil {
				return nil, err
			}
		}
		if len(rec) > len(cols) {
			return nil, fmt.Errorf("record has %d fields, expected %d", len(rec), len(cols))
		}
		out := make([]any, len(cols))
		for i := range out {
			if i < len(rec) {
				out[i] = rec[i]
			}
		}
		return out, nil
	}, nil
}

// object decodes one JSON object keeping key order.
func object(dec *json.Decoder) (keys []string, vals []any, err error) {
	if t, err := dec.Token(); err != nil {
		return nil, nil, err
	} else if t != json.Delim('{') {
		return nil, nil, fmt.Errorf("expected a JSON object, got %v", t)
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, nil, err
		}
		keys = append(keys, k.(string))
		vals = append(vals, v)
	}
	_, err = dec.Token()
	return keys, vals, err
}

func openJSON(r io.Reader) ([]string, func() ([]any, error), error) {
	br := bufio.NewReader(r)
	var first byte
	for {
		b, err := br.ReadByte()
		if err == io.EOF {
			return nil, func() ([]any, error) { return nil, io.EOF }, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if !strings.ContainsRune(" \t\r\n", rune(b)) {
			first = b
			br.UnreadByte()
			break
		}
	}
	dec := json.NewDecoder(br)
	dec.UseNumber()
	array := first == '['
	if array {
		dec.Token()
	}
	nextObj := func() ([]string, []any, error) {
		if array && !dec.More() {
			return nil, nil, io.EOF
		}
		return object(dec)
	}
	cols, firstVals, err := nextObj()
	if err == io.EOF {
		return nil, func() ([]any, error) { return nil, io.EOF }, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return cols, func() ([]any, error) {
		keys, vals := cols, firstVals
		if firstVals != nil {
			firstVals = nil
		} else if keys, vals, err = nextObj(); err != nil {
			return nil, err
		}
		out := make([]any, len(cols)) // missing keys are NULL
		for i, k := range keys {
			j := slices.Index(cols, k)
			if j < 0 {
				return nil, fmt.Errorf("unexpected key %q (not in the first object)", k)
			}
			out[j] = vals[i]
		}
		return out, nil
	}, nil
}

func text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case bool, float64:
		return fmt.Sprint(x)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// Preview parses up to n rows without touching the database. For sql the rows are the statements.
func Preview(r io.Reader, o Options, n int) (cols []string, rows [][]string, err error) {
	if o.Source == "sql" {
		b, err := io.ReadAll(r)
		if err != nil {
			return nil, nil, err
		}
		// shortcut: previews split as postgres, the real dialect is only known in Run
		for _, s := range db.Split("postgres", string(b)) {
			if len(rows) == n {
				break
			}
			rows = append(rows, []string{s.Text})
		}
		return []string{"statement"}, rows, nil
	}
	cols, next, err := open(r, o)
	if err != nil {
		return nil, nil, err
	}
	for len(rows) < n {
		rec, err := next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		row := make([]string, len(rec))
		for i, v := range rec {
			row[i] = text(v)
		}
		rows = append(rows, row)
	}
	return cols, rows, nil
}

// cell is the value handed to Plan.
func cell(v any, emptyIsNull bool) any {
	switch x := v.(type) {
	case nil:
		return db.Null
	case string:
		if emptyIsNull && x == "" {
			return db.Null
		}
	case bool:
		return x
	}
	return text(v)
}

// Run imports everything from r into sess, stopping at the first error.
func Run(ctx context.Context, sess db.Session, r io.Reader, o Options, progress func(Progress)) (err error) {
	var p Progress
	defer func() {
		if err != nil && progress != nil {
			p.Err = err
			progress(p)
		}
	}()
	if o.OnConflict != "" {
		return fmt.Errorf("on conflict %q: not supported for %s", o.OnConflict, sess.Caps().Dialect)
	}
	batch := o.Batch
	if batch <= 0 {
		batch = 500
	}
	step := func(stmts []db.Statement, rows int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := sess.Apply(ctx, stmts); err != nil {
			if rows > 0 {
				return fmt.Errorf("rows %d-%d: %w", p.Rows+1, p.Rows+int64(rows), err)
			}
			return err
		}
		p.Rows += int64(rows)
		p.Batches++
		if progress != nil {
			progress(p)
		}
		return nil
	}

	if o.Source == "sql" {
		b, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		var stmts []db.Statement
		for _, s := range db.Split(sess.Caps().Dialect, string(b)) {
			stmts = append(stmts, db.Statement{Text: s.Text})
			if len(stmts) == batch {
				if err := step(stmts, len(stmts)); err != nil {
					return err
				}
				stmts = nil
			}
		}
		if len(stmts) > 0 {
			return step(stmts, len(stmts))
		}
		return nil
	}

	cols, next, err := open(r, o)
	if err != nil {
		return err
	}
	target := make([]string, len(cols)) // "" = skipped
	var def []db.Column
	for i, c := range cols {
		target[i] = c
		if m, ok := o.Mapping[c]; ok {
			target[i] = m
		}
		if target[i] != "" {
			def = append(def, db.Column{Name: target[i], Type: "TEXT", Nullable: true})
		}
	}
	if len(def) == 0 {
		return fmt.Errorf("no columns to import")
	}
	if o.CreateTable {
		stmts, err := sess.PlanTable(nil, &db.TableDef{Schema: o.Table.Schema, Name: o.Table.Name, Columns: def})
		if err != nil {
			return err
		}
		if err := step(stmts, 0); err != nil {
			return err
		}
		p.Batches = 0 // creating the table is not a data batch
	}

	var changes []db.Change
	flush := func() error {
		if len(changes) == 0 {
			return nil
		}
		stmts, err := sess.Plan(changes)
		if err != nil {
			return err
		}
		n := len(changes)
		changes = nil
		return step(stmts, n)
	}
	for {
		rec, err := next()
		if err == io.EOF {
			return flush()
		}
		if err != nil {
			return err
		}
		c := db.Change{Kind: db.Insert, ObjRef: o.Table}
		for i, v := range rec {
			if target[i] != "" {
				c.Set = append(c.Set, db.KeyVal{Col: target[i], Val: cell(v, o.EmptyIsNull)})
			}
		}
		changes = append(changes, c)
		if len(changes) == batch {
			if err := flush(); err != nil {
				return err
			}
		}
	}
}
