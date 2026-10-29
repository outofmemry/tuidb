// Package importer loads CSV, JSON or SQL files into a table.
package importer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
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
	return PreviewContext(context.Background(), r, o, n)
}

// PreviewContext is Preview that stops when ctx is cancelled. An sql preview reads only as much of r as it needs
// for n statements.
func PreviewContext(ctx context.Context, r io.Reader, o Options, n int) (cols []string, rows [][]string, err error) {
	if r, err = decode(r, o.Encoding); err != nil {
		return nil, nil, err
	}
	o = defaults(o)
	if o.Source == "sql" {
		// shortcut: previews split as postgres, the real dialect is only known in Run
		stop := errors.New("enough")
		err := streamSQL(ctx, r, "postgres", func(text string) error {
			if len(rows) >= n {
				return stop
			}
			rows = append(rows, []string{text})
			if len(rows) >= n {
				return stop
			}
			return nil
		})
		if err != nil && err != stop {
			return nil, nil, err
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
		var stmts []db.Statement
		err := streamSQL(ctx, r, dialect, func(text string) error {
			stmts = append(stmts, db.Statement{Text: text})
			if len(stmts) == batch {
				err := step(stmts, len(stmts))
				stmts = nil
				return err
			}
			return nil
		})
		if err != nil {
			return err
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
	create := func() error {
		stmts, err := sess.PlanTable(nil, &db.TableDef{Schema: o.Table.Schema, Name: o.Table.Name, Columns: def})
		if err != nil {
			return err
		}
		if err := step(stmts, 0); err != nil {
			return err
		}
		created = true
		p.Batches = 0 // creating the table is not a data batch
		return nil
	}
	// MySQL commits DDL implicitly and its pinned session refuses it, so the CREATE runs on the plain session
	// before the transaction opens; PostgreSQL's CREATE goes inside the transaction and rolls back with the data
	mysqlish := dialect == "mysql" || o.Driver == config.MySQL || o.Driver == config.MariaDB
	if o.CreateTable && mysqlish {
		if err := create(); err != nil {
			return err
		}
	}
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

	if o.CreateTable && !mysqlish {
		if err := create(); err != nil {
			return err
		}
	}

	var pk, setCols []string
	for _, c := range target {
		if c != "" {
			setCols = append(setCols, c)
		}
	}
	if o.OnConflict == "update" || o.OnConflict == "replace" {
		if dialect == "postgres" || mysqlish { // ON CONFLICT needs the key columns as its target; ON DUPLICATE KEY needs them to fire
			cols, err := sess.Columns(ctx, o.Table)
			if err != nil {
				return err
			}
			var key []string
			for _, c := range cols {
				if c.PK {
					key = append(key, c.Name)
				}
			}
			if len(key) == 0 && !mysqlish {
				return fmt.Errorf("on conflict %q: %s has no primary key", o.OnConflict, o.Table.Name)
			}
			// the conflict only fires for a row that carries the whole key; otherwise every row just inserts
			for _, k := range key {
				if !slices.Contains(setCols, k) {
					return fmt.Errorf("on conflict %q: primary key column %q is not among the imported columns, so no row could ever conflict", o.OnConflict, k)
				}
			}
			if !mysqlish {
				pk = key
			}
		}
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
		stmts = upsert(stmts, o.OnConflict, dialect, pk, setCols)
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
				c.Set = append(c.Set, db.KeyVal{Col: target[i], Val: cell(v, o)})
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

// decode wraps r so the rest of the package sees UTF-8. A byte the chosen encoding cannot map would come out as
// U+FFFD without an error (a UTF-8 file read as utf-16le becomes CJK-looking rows), so a replacement character the
// source did not contain is an error.
func decode(r io.Reader, name string) (io.Reader, error) {
	switch strings.ToLower(name) {
	case "", "utf-8", "utf8":
		return r, nil
	}
	e, err := ianaindex.IANA.Encoding(name)
	if err != nil || e == nil {
		return nil, fmt.Errorf("unknown encoding %q", name)
	}
	g := &fffdGuard{name: name}
	return &guarded{r: e.NewDecoder().Reader(io.TeeReader(r, g)), g: g}, nil
}

// fffdGuard is the writer the raw bytes are tee'd into: it counts U+FFFD as the file itself spells it in UTF-8 or
// UTF-16, to compare with the replacement characters the decoder produced. The 2-byte tail catches a sequence
// split across two reads.
type fffdGuard struct {
	name string
	raw  int // U+FFFD the file really contains (approximate for single-byte encodings, where it never occurs)
	out  int // U+FFFD in the decoded text
	tail []byte
}

var rawFFFD = [][]byte{{0xEF, 0xBF, 0xBD}, {0xFD, 0xFF}, {0xFF, 0xFD}}

func (g *fffdGuard) Write(p []byte) (int, error) {
	buf := append(g.tail, p...)
	for _, pat := range rawFFFD {
		g.raw += bytes.Count(buf, pat) - bytes.Count(g.tail, pat) // the tail was counted by the previous write
	}
	g.tail = append(g.tail[:0], buf[max(0, len(buf)-2):]...)
	return len(p), nil
}

type guarded struct {
	r io.Reader
	g *fffdGuard
}

func (g *guarded) Read(p []byte) (int, error) {
	n, err := g.r.Read(p)
	if c := bytes.Count(p[:n], []byte("\ufffd")); c > 0 {
		g.g.out += c
		if g.g.out > g.g.raw {
			return 0, fmt.Errorf("the file is not valid %s: some bytes cannot be decoded, pick another encoding", g.g.name)
		}
	}
	return n, err
}

// sqlChunk is how much of a .sql file is read at a time (a variable so tests can use small chunks).
var sqlChunk = 1 << 20

// streamSQL reads r chunk by chunk and calls fn for every statement in order, without holding the whole file.
// The last statement in the buffer may be cut off by the chunk boundary, so it is carried into the next chunk and
// only run once more text shows it is complete (or at EOF). ctx is checked per statement and per read.
// shortcut: the splitter restarts at the carried statement, so a MySQL DELIMITER set in an earlier chunk is forgotten.
func streamSQL(ctx context.Context, r io.Reader, dialect string, fn func(text string) error) error {
	buf := make([]byte, sqlChunk)
	carry := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := io.ReadFull(r, buf)
		eof := rerr == io.EOF || rerr == io.ErrUnexpectedEOF
		if rerr != nil && !eof {
			return rerr
		}
		text := carry + string(buf[:n])
		stmts := db.Split(dialect, text)
		run := stmts
		carry = text
		if !eof && len(stmts) > 0 {
			run = stmts[:len(stmts)-1]
			carry = text[stmts[len(stmts)-1].Start:]
		} else if !eof {
			run = nil
		} else {
			carry = ""
		}
		for _, s := range run {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := fn(s.Text); err != nil {
				return err
			}
		}
		if eof {
			return nil
		}
	}
}

// swapQuote exchanges '"' and q so encoding/csv, which only knows '"', can read a file quoted with q.
// Parsed fields come back with the two swapped, so openCSV swaps them again.
type swapQuote struct{ q byte }

func (swapQuote) Reset() {}
func (t swapQuote) Transform(dst, src []byte, atEOF bool) (int, int, error) {
	n := min(len(dst), len(src))
	for i := 0; i < n; i++ {
		switch src[i] {
		case '"':
			dst[i] = t.q
		case t.q:
			dst[i] = '"'
		default:
			dst[i] = src[i]
		}
	}
	if n < len(src) {
		return n, n, transform.ErrShortDst
	}
	return n, n, nil
}

// upsert turns plain INSERT statements into upserts for onConflict "update" or "replace". Both are the same
// upsert over the mapped columns: MySQL REPLACE would delete the row first, wiping unmapped columns and cascading.
func upsert(stmts []db.Statement, onConflict, dialect string, pk, cols []string) []db.Statement {
	if onConflict != "update" && onConflict != "replace" {
		return stmts
	}
	d, _ := db.DialectFor(dialect)
	out := make([]db.Statement, len(stmts))
	for i, st := range stmts {
		out[i] = st
		if !strings.HasPrefix(st.Text, "INSERT INTO ") {
			continue
		}
		var set []string
		for _, c := range cols {
			if slices.Contains(pk, c) {
				continue
			}
			if dialect == "mysql" {
				set = append(set, d.Quote(c)+" = VALUES("+d.Quote(c)+")")
			} else {
				set = append(set, d.Quote(c)+" = EXCLUDED."+d.Quote(c))
			}
		}
		if dialect == "mysql" && len(set) == 0 && len(cols) > 0 {
			set = append(set, d.Quote(cols[0])+" = "+d.Quote(cols[0]))
		}
		var tail string
		switch {
		case dialect == "mysql":
			tail = " ON DUPLICATE KEY UPDATE " + strings.Join(set, ", ")
		case len(set) == 0:
			tail = " ON CONFLICT DO NOTHING"
		default:
			q := make([]string, len(pk))
			for j, c := range pk {
				q[j] = d.Quote(c)
			}
			tail = " ON CONFLICT (" + strings.Join(q, ", ") + ") DO UPDATE SET " + strings.Join(set, ", ")
		}
		out[i].Text = strings.TrimRight(st.Text, "; ") + tail
		if st.Display != "" {
			out[i].Display = strings.TrimRight(st.Display, "; ") + tail
		}
	}
	return out
}

// uniqueTargets rejects two file columns that land on the same table column, naming both. When the table is
// being created the later ones get a numeric suffix instead (def is updated in place).
func uniqueTargets(cols, target []string, def []db.Column, dialect string, create bool) error {
	key := func(n string) string {
		if dialect == "mysql" {
			return strings.ToLower(n)
		}
		return n
	}
	first := map[string]int{}
	j := 0
	for i, t := range target {
		if t == "" {
			continue
		}
		k := key(t)
		if f, dup := first[k]; dup {
			if !create {
				return fmt.Errorf("file columns %d (%q) and %d (%q) both go to table column %q; map one to another column or skip it", f+1, cols[f], i+1, cols[i], t)
			}
			for n := 2; ; n++ {
				cand := fmt.Sprintf("%s_%d", t, n)
				if _, taken := first[key(cand)]; !taken {
					t = cand
					break
				}
			}
			target[i], def[j].Name = t, t
			k = key(t)
		}
		first[k] = i
		j++
	}
	return nil
}
