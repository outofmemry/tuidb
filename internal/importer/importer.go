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
	OnConflict  string // "" or "stop" (plain insert, first error stops); "update" and "replace" upsert (PostgreSQL, MySQL)
	// Driver is the connection's driver name. Redshift, Greenplum and Vertica share the postgres dialect but have
	// no ON CONFLICT, so an upsert is only offered for config.Postgres (and MySQL-family dialects); "" = unknown,
	// judged by the dialect alone.
	Driver string
	// NullAs is the cell text that becomes NULL, checked before EmptyIsNull; "" falls back to CSV.NullAs for csv.
	NullAs string
	// KeepFormulaQuote keeps the apostrophe the CSV export puts before a cell that starts with = + - @ tab or CR.
	// By default one leading ' is stripped when the rest starts with one of those (and is not a number), so an
	// export read back is lossless.
	KeepFormulaQuote bool
	// Encoding of the input file: "" or utf-8 = as is; latin1, windows-1252, utf-16le, utf-16be, ... (IANA names).
	Encoding string
	// Quote is the CSV quote character, default CSV.Quote, else '"'.
	// shortcut: ASCII only, done by swapping it with '"' around encoding/csv; add a hand parser for more.
	Quote rune
	// ColumnTypes sets the type of a created column by table column name (file column name also works); default TEXT.
	ColumnTypes map[string]string
}

type Progress struct {
	// Rows is the number of rows sent to the database. Apply reports no affected count, so with ON CONFLICT DO
	// NOTHING it can exceed the rows really inserted: callers say "processed", not "inserted".
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
	delim := ','
	if o.CSV.Delimiter != "" {
		d := []rune(o.CSV.Delimiter)
		if len(d) != 1 {
			return nil, nil, fmt.Errorf("csv delimiter %q must be exactly one character", o.CSV.Delimiter)
		}
		delim = d[0]
	}
	q := o.Quote
	if q == 0 {
		if cq := []rune(o.CSV.Quote); len(cq) == 1 {
			q = cq[0]
		} else if len(cq) > 1 {
			return nil, nil, fmt.Errorf("csv quote %q must be exactly one character", o.CSV.Quote)
		}
	}
	if q == 0 {
		q = '"'
	}
	if q >= 0x7f || q == delim || q == '\n' || q == '\r' {
		return nil, nil, fmt.Errorf("csv quote %q must be a printable ASCII character that is not the delimiter or a line break", q)
	}
	var cr *csv.Reader
	back := func(f []string) {}
	if q != '"' {
		cr = csv.NewReader(transform.NewReader(br, swapQuote{byte(q)}))
		sw := strings.NewReplacer(`"`, string(q), string(q), `"`)
		back = func(f []string) {
			for i := range f {
				f[i] = sw.Replace(f[i])
			}
		}
	} else {
		cr = csv.NewReader(br)
	}
	cr.Comma = delim
	cr.FieldsPerRecord = -1
	// Strict quotes: a stray quote must fail with its line, not swallow the rest of the file into one field.
	// shortcut: LineBreak settings are ignored (encoding/csv fixes them), add a hand parser if someone needs another
	read := func() ([]string, error) {
		rec, err := cr.Read()
		back(rec)
		return rec, err
	}
	rec, err := read()
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
		for i, c := range cols {
			if strings.TrimSpace(c) == "" {
				return nil, nil, fmt.Errorf("column %d has no name; give it a name in the file", i+1)
			}
			if !o.KeepFormulaQuote {
				cols[i] = unquoteFormula(c)
			}
		}
	} else {
		for i := range rec {
			cols = append(cols, fmt.Sprintf("column%d", i+1))
		}
	}
	return cols, func() ([]any, error) {
		rec := pending
		pending = nil
		if rec == nil {
			if rec, err = read(); err != nil {
				return nil, err
			}
		}
		if len(rec) != len(cols) {
			line, _ := cr.FieldPos(0)
			return nil, fmt.Errorf("line %d has %d fields, expected %d", line, len(rec), len(cols))
		}
		out := make([]any, len(cols))
		for i := range out {
			out[i] = rec[i]
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
	if r, err = decode(r, o.Encoding); err != nil {
		return nil, nil, err
	}
	o = defaults(o)
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
			// the same transform as the import, so the preview shows what the table will get
			if c := cell(v, o); db.IsNull(c) {
				row[i] = "NULL"
			} else {
				row[i] = text(c)
			}
		}
		rows = append(rows, row)
	}
	return cols, rows, nil
}

// unquoteFormula undoes the export's formula guard: one leading ' before = + - @ tab or CR. A number after the
// apostrophe is kept as is, because the export never guards numbers, so that ' belonged to the data.
func unquoteFormula(s string) string {
	if len(s) > 1 && s[0] == '\'' && strings.ContainsRune("=+-@\t\r", rune(s[1])) {
		if _, err := strconv.ParseFloat(s[1:], 64); err != nil {
			return s[1:]
		}
	}
	return s
}

// defaults fills the options both Preview and Run derive from the config.
func defaults(o Options) Options {
	if o.NullAs == "" && o.Source == "csv" {
		o.NullAs = o.CSV.NullAs
	}
	return o
}

// cell is the value handed to Plan.
func cell(v any, o Options) any {
	switch x := v.(type) {
	case nil:
		return db.Null
	case string:
		if o.NullAs != "" && x == o.NullAs {
			return db.Null
		}
		if o.EmptyIsNull && x == "" {
			return db.Null
		}
		if o.Source == "csv" && !o.KeepFormulaQuote {
			return unquoteFormula(x)
		}
		return x
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
	if r, err = decode(r, o.Encoding); err != nil {
		return err
	}
	dialect := sess.Caps().Dialect
	o = defaults(o)
	// ON CONFLICT is PostgreSQL's alone: Redshift, Greenplum and Vertica report the same dialect without it
	pgConflict := dialect == "postgres" && (o.Driver == "" || o.Driver == config.Postgres)
	switch o.OnConflict {
	case "", "stop":
	case "update", "replace":
		if o.Source == "sql" || !pgConflict && dialect != "mysql" {
			return fmt.Errorf("on conflict %q: not supported for %s", o.OnConflict, dialect)
		}
		if o.CreateTable { // a new table has no key to conflict on, and it would be left behind empty
			return fmt.Errorf("on conflict %q cannot be combined with creating the table: a new table has no primary key", o.OnConflict)
		}
	default:
		return fmt.Errorf("on conflict %q: not supported for %s", o.OnConflict, dialect)
	}
	batch := o.Batch
	if batch <= 0 {
		batch = 500
	}
	var ap db.Session = sess // the session the batches run on: a pinned one inside the transaction
	inTx := false
	step := func(stmts []db.Statement, rows int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := ap.Apply(ctx, stmts); err != nil {
			if rows > 0 {
				err = fmt.Errorf("rows %d-%d: %w", p.Rows+1, p.Rows+int64(rows), err)
				if p.Rows > 0 && !inTx {
					err = fmt.Errorf("%w (processed %d rows; rows before the failure were committed)", err, p.Rows)
				}
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
		b, err := readAll(ctx, r)
		if err != nil {
			return err
		}
		var stmts []db.Statement
		for _, s := range db.Split(sess.Caps().Dialect, string(b)) {
			if err := ctx.Err(); err != nil {
				return err
			}
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
			typ := "TEXT"
			if t := o.ColumnTypes[target[i]]; t != "" {
				typ = t
			} else if t := o.ColumnTypes[c]; t != "" {
				typ = t
			}
			def = append(def, db.Column{Name: target[i], Type: typ, Nullable: true})
		}
	}
	if len(def) == 0 {
		return fmt.Errorf("no columns to import")
	}
	if err := uniqueTargets(cols, target, def, dialect, o.CreateTable); err != nil {
		return err
	}
	created := false
	// the transaction opens before the CREATE, so a failure in the data takes the new table with it
	if o.Source != "sql" && sess.Caps().Transactions {
		if pn, ok := sess.(db.Pinner); ok {
			pin, perr := pn.Pin(ctx)
			if perr != nil {
				return perr
			}
			if _, berr := pin.Exec(ctx, "BEGIN", 0); berr != nil {
				pin.Close()
				return fmt.Errorf("begin: %w", berr)
			}
			ap, inTx = pin, true
			defer func() { // one transaction for the whole file: all rows or none
				defer pin.Close()
				if err != nil {
					pin.Exec(context.Background(), "ROLLBACK", 0)
					if created && (o.Driver == config.MySQL || o.Driver == config.MariaDB) {
						// MySQL commits CREATE TABLE implicitly, so the rollback does not take the table with it
						err = fmt.Errorf("%w (rolled back; the new table stays, MySQL commits CREATE TABLE)", err)
					} else {
						err = fmt.Errorf("%w (rolled back, no rows were imported)", err)
					}
				} else if _, cerr := pin.Exec(context.Background(), "COMMIT", 0); cerr != nil {
					err = fmt.Errorf("commit: %w", cerr)
				}
			}()
		}
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
