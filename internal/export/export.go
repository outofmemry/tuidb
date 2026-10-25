// Package export writes query results and whole tables as CSV, JSON, SQL INSERTs or XLSX.
package export

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

type Format string // "csv" | "json" | "sql" | "xlsx"

type Options struct {
	Format    Format
	CSV       config.CSV
	Columns   []string // subset to write; empty = all
	Gzip      bool     // wrap the writer in gzip (csv/json/sql only)
	Table     string   // for sql INSERTs / xlsx sheet name
	Schema    string   // sql only: qualifies Table
	Dialect   string   // for sql identifier quoting
	Structure bool     // sql only: prepend CREATE TABLE from the DDL passed in
	Drop      bool     // sql only: prepend DROP TABLE IF EXISTS
	DDL       string   // the CREATE TABLE text when Structure is set (caller fetches via Session.DDL)
	// RawFormulas turns off the CSV default of prefixing text cells that start with = + - @ tab or CR with ',
	// which stops a spreadsheet running them. Numbers are never prefixed.
	RawFormulas bool
}

const pageSize = 1000

// Ext returns the file extension for o ("csv", "csv.gz", "json", "sql", "xlsx").
func Ext(o Options) string {
	if o.Gzip && o.Format != "xlsx" {
		return string(o.Format) + ".gz"
	}
	return string(o.Format)
}

// Write renders res (columns + rows) to w. For xlsx, Gzip is ignored.
func Write(w io.Writer, res *db.Result, o Options) error {
	s, err := open(w, o)
	if err != nil {
		return err
	}
	defer s.abort()
	if err := s.add(res); err != nil {
		return err
	}
	return s.finish()
}

// Table streams a whole table page by page through sess.Rows; progress may be nil.
func Table(ctx context.Context, sess db.Session, ref db.ObjRef, filters db.FilterSet, o Options, w io.Writer, progress func(int64)) error {
	if o.Table == "" {
		o.Table = ref.Name
	}
	o.Schema = ref.Schema
	s, err := open(w, o)
	if err != nil {
		return err
	}
	defer s.abort()
	cols, err := sess.Columns(ctx, ref) // OFFSET paging needs a stable order
	if err != nil {
		return err
	}
	var sort []db.Sort
	for _, c := range cols {
		if c.PK {
			sort = append(sort, db.Sort{Column: c.Name})
		}
	}
	if len(sort) == 0 {
		for _, c := range cols {
			sort = append(sort, db.Sort{Column: c.Name})
		}
	}
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		res, err := sess.Rows(ctx, db.RowQuery{ObjRef: ref, Filters: filters, Sort: sort, Columns: o.Columns, Limit: pageSize, Offset: int(n)})
		if err != nil {
			return err
		}
		if err := s.add(res); err != nil {
			return err
		}
		n += int64(len(res.Rows))
		if progress != nil {
			progress(n)
		}
		if len(res.Rows) < pageSize {
			return s.finish()
		}
	}
}

type enc interface {
	head(cols []string) error
	row(vals []any) error
	close() error
}

type stream struct {
	e       enc
	o       Options
	names   []string // output columns, fixed by the first page; later pages are matched by name
	started bool
	done    bool // finish ran, so abort has nothing to release
	gz      *gzip.Writer
}

func open(w io.Writer, o Options) (*stream, error) {
	s := &stream{o: o}
	if o.Gzip && o.Format != "xlsx" {
		s.gz = gzip.NewWriter(w)
		w = s.gz
	}
	switch o.Format {
	case "csv":
		s.e = &csvEnc{w: w, c: o.CSV, safe: !o.RawFormulas}
	case "json":
		s.e = &jsonEnc{w: w}
	case "sql":
		d, ok := db.DialectFor(o.Dialect)
		if !ok {
			d = db.PG
		}
		s.e = &sqlEnc{w: w, d: d, o: o}
	case "xlsx":
		s.e = &xlsxEnc{w: w, f: excelize.NewFile(), name: o.Table}
	default:
		return nil, fmt.Errorf("unknown export format %q", o.Format)
	}
	return s, nil
}

func (s *stream) add(res *db.Result) error {
	at := map[string]int{}
	for i, c := range res.Columns {
		at[c.Name] = i
	}
	if !s.started {
		s.started = true
		if len(s.o.Columns) == 0 {
			for _, c := range res.Columns {
				s.names = append(s.names, c.Name)
			}
		}
		for _, want := range s.o.Columns {
			if _, ok := at[want]; !ok {
				return fmt.Errorf("no column %q", want)
			}
			s.names = append(s.names, want)
		}
		if err := s.e.head(s.names); err != nil {
			return err
		}
	}
	for _, r := range res.Rows {
		vals := make([]any, len(s.names))
		for i, n := range s.names {
			if j, ok := at[n]; ok && j < len(r) {
				vals[i] = r[j]
			}
		}
		if err := s.e.row(vals); err != nil {
			return err
		}
	}
	return nil
}

// abort releases the gzip writer when an export fails part-way. The output is then incomplete (no gzip
// trailer, no closing "]"), so the caller must discard it; every caller writes a temp file and removes it on error.
func (s *stream) abort() {
	if s.gz != nil && !s.done {
		s.gz.Close()
	}
}

func (s *stream) finish() error {
	s.done = true
	if !s.started {
		if err := s.add(&db.Result{}); err != nil {
			return err
		}
	}
	if err := s.e.close(); err != nil {
		return err
	}
	if s.gz != nil {
		return s.gz.Close()
	}
	return nil
}

// ---- csv ----

type csvEnc struct {
	w    io.Writer
	c    config.CSV
	safe bool // prefix formula-looking text with '
}

// neutralize defuses a spreadsheet formula: Excel/LibreOffice evaluate a cell starting with = + - @ tab or CR.
func neutralize(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

// CSVField quotes v for c: QuoteMode "always" quotes every non-empty field (an empty one stays empty, which
// the importer reads as NULL), anything else only fields holding the delimiter, quote, CR or LF.
func CSVField(c config.CSV, v string) string {
	d, q := orDef(c.Delimiter, ","), orDef(c.Quote, `"`)
	if (c.QuoteMode == "always" && v != "") || strings.ContainsAny(v, d+q+"\r\n") {
		return q + strings.ReplaceAll(v, q, q+q) + q
	}
	return v
}

// CheckCSV returns a warning when the settings cannot round-trip, or "".
func CheckCSV(c config.CSV) string {
	if orDef(c.Delimiter, ",") == "," && c.Decimal == "," {
		return `delimiter and decimal separator are both ",": decimal numbers will be quoted or split`
	}
	return ""
}

func (e *csvEnc) field(s string) string { return CSVField(e.c, s) }

func (e *csvEnc) write(cells []string) error {
	for i, c := range cells {
		cells[i] = e.field(c)
	}
	if len(cells) == 1 && cells[0] == "" { // a blank line would be skipped on re-import
		q := orDef(e.c.Quote, `"`)
		cells[0] = q + q
	}
	_, err := io.WriteString(e.w, strings.Join(cells, orDef(e.c.Delimiter, ","))+orDef(e.c.LineBreak, "\n"))
	return err
}

func (e *csvEnc) head(cols []string) error {
	cells := append([]string(nil), cols...)
	if e.safe {
		for i, c := range cells {
			cells[i] = neutralize(c)
		}
	}
	return e.write(cells)
}

func (e *csvEnc) row(vals []any) error {
	cells := make([]string, len(vals))
	for i, v := range vals {
		if v == nil || db.IsNull(v) { // an empty cell is NULL; the importer's EmptyIsNull reads it back
			continue
		}
		cells[i] = db.FormatValue(v)
		switch v.(type) {
		case int64, uint64, float64: // a number cannot carry a payload, and "'-5" would stop being one
			if _, ok := v.(float64); ok && e.c.Decimal != "" {
				cells[i] = strings.Replace(cells[i], ".", e.c.Decimal, 1)
			}
		default:
			if e.safe {
				cells[i] = neutralize(cells[i])
			}
		}
	}
	return e.write(cells)
}

func (e *csvEnc) close() error { return nil }

func orDef(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// ---- json ----

type jsonEnc struct {
	w    io.Writer
	cols []string
	n    int
}

func (e *jsonEnc) head(cols []string) error {
	e.cols = cols
	_, err := io.WriteString(e.w, "[")
	return err
}

func (e *jsonEnc) row(vals []any) error {
	var b strings.Builder
	if e.n > 0 {
		b.WriteString(",")
	}
	b.WriteString("\n  {")
	for i, v := range vals {
		if i > 0 {
			b.WriteString(", ")
		}
		k, _ := json.Marshal(e.cols[i])
		b.Write(k)
		b.WriteString(": ")
		j, err := jsonMarshal(jsonValue(v))
		if err != nil {
			return err
		}
		b.Write(j)
	}
	b.WriteString("}")
	e.n++
	_, err := io.WriteString(e.w, b.String())
	return err
}

func (e *jsonEnc) close() error {
	_, err := io.WriteString(e.w, "\n]\n")
	return err
}

func jsonMarshal(v any) ([]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimRight(b.String(), "\n")), nil
}

func jsonValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case string, bool, int64, uint64:
		return x
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil
		}
		return x
	}
	if db.IsNull(v) {
		return nil
	}
	return db.FormatValue(v)
}

// ---- sql ----

type sqlEnc struct {
	w   io.Writer
	d   db.Dialect
	o   Options
	ins string
}

func (e *sqlEnc) head(cols []string) error {
	q := make([]string, len(cols))
	for i, c := range cols {
		q[i] = e.d.Quote(c)
	}
	t := e.d.QualifiedName(db.ObjRef{Schema: e.o.Schema, Name: e.o.Table})
	e.ins = fmt.Sprintf("INSERT INTO %s (%s) VALUES (", t, strings.Join(q, ", "))
	if e.o.Drop {
		if _, err := fmt.Fprintf(e.w, "DROP TABLE IF EXISTS %s;\n", t); err != nil {
			return err
		}
	}
	if e.o.Structure {
		ddl := strings.TrimSpace(e.o.DDL)
		if _, err := fmt.Fprintf(e.w, "%s;\n", strings.TrimSuffix(ddl, ";")); err != nil {
			return err
		}
	}
	return nil
}

func (e *sqlEnc) row(vals []any) error {
	lits := make([]string, len(vals))
	for i, v := range vals {
		lits[i] = e.lit(v)
	}
	_, err := io.WriteString(e.w, e.ins+strings.Join(lits, ", ")+");\n")
	return err
}

// lit is Dialect.Literal (display-only) fixed up for binary, special floats and zoned times.
func (e *sqlEnc) lit(v any) string {
	switch x := v.(type) {
	case []byte:
		if e.d.BackslashEscapes {
			return fmt.Sprintf("X'%x'", x)
		}
		return fmt.Sprintf("'\\x%x'", x)
	case float64:
		switch {
		case math.IsNaN(x):
			return "'NaN'"
		case math.IsInf(x, 1):
			return "'Infinity'"
		case math.IsInf(x, -1):
			return "'-Infinity'"
		}
	case time.Time:
		return "'" + x.Format(time.RFC3339Nano) + "'"
	}
	return e.d.Literal(v)
}

func (e *sqlEnc) close() error { return nil }

// ---- xlsx ----

type xlsxEnc struct {
	w     io.Writer
	f     *excelize.File
	name  string
	sheet string
	n     int
}

func (e *xlsxEnc) put(vals []any) error {
	e.n++
	cell, err := excelize.CoordinatesToCellName(1, e.n)
	if err != nil {
		return err
	}
	return e.f.SetSheetRow(e.sheet, cell, &vals)
}

func (e *xlsxEnc) head(cols []string) error {
	e.sheet = "Sheet1"
	if e.name != "" { // Excel caps sheet names at 31 chars
		e.sheet = strings.Map(func(r rune) rune {
			if strings.ContainsRune(`:\/?*[]`, r) {
				return '_'
			}
			return r
		}, e.name)
		if r := []rune(e.sheet); len(r) > 31 {
			e.sheet = string(r[:31])
		}
		if err := e.f.SetSheetName("Sheet1", e.sheet); err != nil {
			return err
		}
	}
	h := make([]any, len(cols))
	for i, c := range cols {
		h[i] = c
	}
	return e.put(h)
}

func (e *xlsxEnc) row(vals []any) error {
	out := make([]any, len(vals))
	for i, v := range vals {
		switch x := v.(type) {
		case nil:
		case int64, uint64, float64, bool, string:
			out[i] = x
		default:
			if !db.IsNull(v) {
				out[i] = db.FormatValue(v)
			}
		}
	}
	return e.put(out)
}

func (e *xlsxEnc) close() error { return e.f.Write(e.w) }
