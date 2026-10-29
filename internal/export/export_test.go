package export

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func res() *db.Result {
	return &db.Result{
		Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}, {Name: "at"}},
		Rows: [][]any{
			{int64(1), "it's", time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)},
			{int64(2), nil, nil},
			{int64(3), "a,\"b\"\nc", nil},
		},
	}
}

func write(t *testing.T, o Options) string {
	t.Helper()
	var b bytes.Buffer
	if err := Write(&b, res(), o); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestWrite(t *testing.T) {
	cases := []struct {
		name string
		o    Options
		want string
	}{
		{"csv", Options{Format: "csv"}, "id,name,at\n1,it's,2024-01-02 03:04:05\n2,,\n3,\"a,\"\"b\"\"\nc\",\n"},
		{"csv subset", Options{Format: "csv", Columns: []string{"name"}}, "name\nit's\n\"\"\n\"a,\"\"b\"\"\nc\"\n"},
		{"json", Options{Format: "json", Columns: []string{"id", "name"}}, "[\n  {\"id\": 1, \"name\": \"it's\"},\n  {\"id\": 2, \"name\": null},\n  {\"id\": 3, \"name\": \"a,\\\"b\\\"\\nc\"}\n]\n"},
		{"sql", Options{Format: "sql", Table: "t", Columns: []string{"id", "name"}, Dialect: "postgres", Drop: true, Structure: true, DDL: "CREATE TABLE t (id int)"},
			"DROP TABLE IF EXISTS \"t\";\nCREATE TABLE t (id int);\n" +
				"INSERT INTO \"t\" (\"id\", \"name\") VALUES (1, 'it''s');\n" +
				"INSERT INTO \"t\" (\"id\", \"name\") VALUES (2, NULL);\n" +
				"INSERT INTO \"t\" (\"id\", \"name\") VALUES (3, 'a,\"b\"\nc');\n"},
	}
	for _, c := range cases {
		if got := write(t, c.o); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestCSVNeutralizesFormulas(t *testing.T) {
	r := &db.Result{Columns: []db.ResultColumn{{Name: "=h"}, {Name: "n"}}, Rows: [][]any{{"=cmd|' /C calc'!A0", int64(-5)}, {"@x", -1.5}, {"+1+1", "ok"}, {"-12.34", []byte("-7")}, {"-1e5", "+3"}}}
	var b bytes.Buffer
	if err := Write(&b, r, Options{Format: "csv"}); err != nil {
		t.Fatal(err)
	}
	if want := "'=h,n\n'=cmd|' /C calc'!A0,-5\n'@x,-1.5\n'+1+1,ok\n-12.34,\\x2d37\n-1e5,+3\n"; b.String() != want {
		t.Errorf("got %q want %q", b.String(), want)
	}
	b.Reset()
	Write(&b, r, Options{Format: "csv", RawFormulas: true})
	if !strings.HasPrefix(b.String(), "=h,n\n=cmd") {
		t.Errorf("raw: %q", b.String())
	}
}

type errSess struct{ db.Session }

func (errSess) Columns(context.Context, db.ObjRef) ([]db.Column, error) {
	return nil, io.ErrUnexpectedEOF
}

func TestTableErrorClosesGzip(t *testing.T) {
	var b bytes.Buffer
	if err := Table(context.Background(), errSess{}, db.ObjRef{Name: "t"}, db.FilterSet{}, Options{Format: "csv", Gzip: true}, &b, nil); err == nil {
		t.Fatal("want error")
	}
	if b.Len() <= 10 { // a closed gzip writer adds its trailer to the 10-byte header
		t.Errorf("gzip left unflushed: %d bytes", b.Len())
	}
}

func TestExtAndGzip(t *testing.T) {
	if Ext(Options{Format: "csv", Gzip: true}) != "csv.gz" || Ext(Options{Format: "xlsx", Gzip: true}) != "xlsx" || Ext(Options{Format: "sql"}) != "sql" {
		t.Error("bad ext")
	}
	var b bytes.Buffer
	if err := Write(&b, res(), Options{Format: "csv", Gzip: true}); err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(&b)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(zr)
	if !strings.HasPrefix(string(out), "id,name,at\n") {
		t.Errorf("got %q", out)
	}
}

func TestXLSX(t *testing.T) {
	var b bytes.Buffer
	if err := Write(&b, res(), Options{Format: "xlsx", Table: "people", Gzip: true}); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(&b)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := f.GetRows("people")
	if err != nil || len(rows) != 4 || rows[0][1] != "name" || rows[1][0] != "1" || rows[1][2] != "2024-01-02 03:04:05" {
		t.Errorf("rows=%v err=%v", rows, err)
	}
	if typ, _ := f.GetCellType("people", "A2"); typ == excelize.CellTypeSharedString || typ == excelize.CellTypeInlineString {
		t.Errorf("A2 type %v, want non-string", typ)
	}
}

// pager serves n rows of (id) through Rows.
type pager struct {
	db.Session
	n, calls int
}

func (p *pager) Columns(context.Context, db.ObjRef) ([]db.Column, error) {
	return []db.Column{{Name: "id", PK: true}}, nil
}

func (p *pager) Rows(_ context.Context, q db.RowQuery) (*db.Result, error) {
	p.calls++
	r := &db.Result{Columns: []db.ResultColumn{{Name: "id"}}}
	for i := q.Offset; i < p.n && i < q.Offset+q.Limit; i++ {
		r.Rows = append(r.Rows, []any{int64(i)})
	}
	return r, nil
}

func TestTablePages(t *testing.T) {
	p := &pager{n: 2500}
	var b bytes.Buffer
	var last int64
	if err := Table(context.Background(), p, db.ObjRef{Name: "t"}, db.FilterSet{}, Options{Format: "csv"}, &b, func(n int64) { last = n }); err != nil {
		t.Fatal(err)
	}
	if p.calls != 3 || last != 2500 || strings.Count(b.String(), "\n") != 2501 {
		t.Errorf("calls=%d last=%d lines=%d", p.calls, last, strings.Count(b.String(), "\n"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Table(ctx, p, db.ObjRef{Name: "t"}, db.FilterSet{}, Options{Format: "csv"}, &b, nil); err == nil {
		t.Error("want ctx error")
	}
}

type sorter struct {
	pager
	sort []db.Sort
	wide bool
}

func (s *sorter) Rows(ctx context.Context, q db.RowQuery) (*db.Result, error) {
	s.sort = q.Sort
	r, _ := s.pager.Rows(ctx, q)
	if q.Offset > 0 { // page 2 has the columns reordered plus a new one
		r.Columns = []db.ResultColumn{{Name: "extra"}, {Name: "id"}}
		for i, row := range r.Rows {
			r.Rows[i] = []any{"x", row[0]}
		}
	}
	return r, nil
}

func TestTableOrderSchemaAndPageColumns(t *testing.T) {
	s := &sorter{pager: pager{n: 1001}}
	var b bytes.Buffer
	err := Table(context.Background(), s, db.ObjRef{Schema: "archive", Name: "users"}, db.FilterSet{}, Options{Format: "sql", Dialect: "postgres", Drop: true}, &b, nil)
	if err != nil || len(s.sort) != 1 || s.sort[0].Column != "id" {
		t.Fatalf("err=%v sort=%v", err, s.sort)
	}
	out := b.String()
	if !strings.Contains(out, `DROP TABLE IF EXISTS "archive"."users"`) || !strings.Contains(out, `INSERT INTO "archive"."users" ("id") VALUES (1000);`) {
		t.Errorf("got %.200q", out)
	}
}

type serialPager struct{ pager }

func (p *serialPager) Columns(context.Context, db.ObjRef) ([]db.Column, error) {
	return []db.Column{{Name: "id", PK: true, Extra: "serial"}, {Name: "n", Extra: "GENERATED BY DEFAULT AS IDENTITY"}, {Name: "x"}}, nil
}

// T-P2-08: a restored table must accept the next insert, so the sequences move past the exported ids.
func TestTableSQLSetsSequences(t *testing.T) {
	var b bytes.Buffer
	if err := Table(context.Background(), &serialPager{pager{n: 3}}, db.ObjRef{Schema: "shop", Name: "orders"}, db.FilterSet{}, Options{Format: "sql", Dialect: "postgres"}, &b, nil); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	want := `INSERT INTO "shop"."orders" ("id") VALUES (2);
SELECT setval(pg_get_serial_sequence('"shop"."orders"', 'id'), coalesce(max("id"), 1), max("id") IS NOT NULL) FROM "shop"."orders";
SELECT setval(pg_get_serial_sequence('"shop"."orders"', 'n'), coalesce(max("n"), 1), max("n") IS NOT NULL) FROM "shop"."orders";
`
	if !strings.HasSuffix(out, want) || strings.Count(out, "setval") != 2 {
		t.Errorf("got\n%s", out)
	}
	b.Reset()
	if err := Table(context.Background(), &serialPager{pager{n: 3}}, db.ObjRef{Name: "orders"}, db.FilterSet{}, Options{Format: "sql", Dialect: "mysql"}, &b, nil); err != nil || strings.Contains(b.String(), "setval") {
		t.Errorf("mysql: %v\n%s", err, b.String())
	}
}

func TestSQLLiterals(t *testing.T) {
	r := &db.Result{Columns: []db.ResultColumn{{Name: "a"}, {Name: "b"}, {Name: "c"}}, Rows: [][]any{{[]byte{0xde, 0xad}, math.NaN(), time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", 3600))}}}
	for d, want := range map[string]string{"mysql": "VALUES (X'dead', 'NaN', '2024-01-02T03:04:05+01:00')", "postgres": `VALUES ('\xdead', 'NaN', '2024-01-02T03:04:05+01:00')`} {
		var b bytes.Buffer
		if err := Write(&b, r, Options{Format: "sql", Table: "t", Dialect: d}); err != nil || !strings.Contains(b.String(), want) {
			t.Errorf("%s: %v %q", d, err, b.String())
		}
	}
}

func TestCSVSingleEmptyAndJSONNaN(t *testing.T) {
	r := &db.Result{Columns: []db.ResultColumn{{Name: "a"}}, Rows: [][]any{{""}, {math.Inf(1)}}}
	var b bytes.Buffer
	Write(&b, r, Options{Format: "csv"})
	if b.String() != "a\n\"\"\n+Inf\n" {
		t.Errorf("csv %q", b.String())
	}
	b.Reset()
	if err := Write(&b, r, Options{Format: "json"}); err != nil || !strings.Contains(b.String(), `"a": null`) {
		t.Errorf("json %v %q", err, b.String())
	}
}

func TestJSONColumnsNested(t *testing.T) {
	r := &db.Result{Columns: []db.ResultColumn{{Name: "j", Type: "jsonb"}, {Name: "t", Type: "text"}},
		Rows: [][]any{{`{"a": [1, 2]}`, `{"a":1}`}, {"not json", "x"}}}
	var b bytes.Buffer
	if err := Write(&b, r, Options{Format: "json"}); err != nil {
		t.Fatal(err)
	}
	want := "[\n  {\"j\": {\"a\":[1,2]}, \"t\": \"{\\\"a\\\":1}\"},\n  {\"j\": \"not json\", \"t\": \"x\"}\n]\n"
	if b.String() != want {
		t.Errorf("got %q\nwant %q", b.String(), want)
	}
}

func TestXLSXSheetName(t *testing.T) {
	var b bytes.Buffer
	if err := Write(&b, res(), Options{Format: "xlsx", Table: "a/b" + strings.Repeat("é", 40)}); err != nil {
		t.Fatal(err)
	}
	f, _ := excelize.OpenReader(&b)
	if n := f.GetSheetName(0); n != "a_b"+strings.Repeat("é", 28) {
		t.Errorf("sheet %q", n)
	}
}

func TestCSVFieldQuoteMode(t *testing.T) {
	needed, always := config.CSV{QuoteMode: "needed"}, config.CSV{QuoteMode: "always"}
	for _, c := range []struct {
		c    config.CSV
		in   string
		want string
	}{{needed, "abc", "abc"}, {needed, "a,b", `"a,b"`}, {needed, `a"b`, `"a""b"`}, {needed, "a\nb", "\"a\nb\""},
		{always, "abc", `"abc"`}, {always, "", ""}, {config.CSV{Delimiter: ";"}, "a,b", "a,b"}} {
		if got := CSVField(c.c, c.in); got != c.want {
			t.Errorf("%+v %q: %q", c.c, c.in, got)
		}
	}
	if CheckCSV(config.CSV{Delimiter: ",", Decimal: ","}) == "" || CheckCSV(config.CSV{Delimiter: ";", Decimal: ","}) != "" || CheckCSV(config.CSV{Decimal: ","}) == "" {
		t.Error("CheckCSV")
	}
}

func TestCSVNoHeaderAndNullAs(t *testing.T) {
	got := write(t, Options{Format: "csv", NoHeader: true, NullAs: "NULL", Columns: []string{"id", "name"}})
	if strings.Contains(got, "id,name") || !strings.Contains(got, "2,NULL\n") {
		t.Errorf("got %q", got)
	}
	if got := write(t, Options{Format: "csv", CSV: config.CSV{NullAs: `\N`}, Columns: []string{"id", "name"}}); !strings.Contains(got, `2,\N`) {
		t.Errorf("[csv] null_as fallback: %q", got)
	}
}

func TestFilename(t *testing.T) {
	v := map[string]string{"table": "a/b", "connection": "prod", "date": "2026-10-28", "time": "010203"}
	for in, want := range map[string]string{
		"{table}-{date}":      "a_b-2026-10-28",
		"{connection}_{time}": "prod_010203",
		"{tag}-{nope}":        "{nope}",
		"{tag}-{table}":       "a_b",
		"{database}.csv":      "export.csv",
		"{table}-{tag}.csv":   "a_b.csv",
		"{tag}":               "export",
		"../{table}":          "_a_b",
		"":                    "export",
	} {
		if got := Filename(in, v); got != want {
			t.Errorf("Filename(%q) = %q, want %q", in, got, want)
		}
	}
	for _, name := range []string{"CON", "nul", "COM1", "lpt9"} {
		if got := Filename("{table}.csv", map[string]string{"table": name}); got != "_"+name+".csv" {
			t.Errorf("reserved %s: %q", name, got)
		}
	}
	if got := Filename("{table}", map[string]string{"table": "console"}); got != "console" {
		t.Errorf("console: %q", got)
	}
	if got := Filename("{date}", nil); len(got) != 10 {
		t.Errorf("default date: %q", got)
	}
}
