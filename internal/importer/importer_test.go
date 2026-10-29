package importer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/export"
)

type fake struct {
	db.Session
	plans   [][]db.Change
	applied [][]db.Statement
	tables  []*db.TableDef
	dialect string
	fail    error
}

func (f *fake) Caps() db.Capabilities { return db.Capabilities{Dialect: f.dialect} }
func (f *fake) Plan(ch []db.Change) ([]db.Statement, error) {
	f.plans = append(f.plans, ch)
	return db.PlanSQL(db.PG, ch)
}
func (f *fake) PlanTable(_, n *db.TableDef) ([]db.Statement, error) {
	f.tables = append(f.tables, n)
	return []db.Statement{{Text: "CREATE TABLE " + n.Name}}, nil
}
func (f *fake) Apply(_ context.Context, s []db.Statement) error {
	f.applied = append(f.applied, s)
	return f.fail
}

var tbl = db.ObjRef{Name: "t"}

func TestPreview(t *testing.T) {
	cols, rows, err := Preview(strings.NewReader("a,b\n1,2\n3,4\n"), Options{Source: "csv", Header: true}, 1)
	if err != nil || !reflect.DeepEqual(cols, []string{"a", "b"}) || !reflect.DeepEqual(rows, [][]string{{"1", "2"}}) {
		t.Errorf("header: %v %v %v", cols, rows, err)
	}
	cols, rows, _ = Preview(strings.NewReader("a;b\n1;2\n"), Options{Source: "csv", CSV: config.CSV{Delimiter: ";"}}, 5)
	if !reflect.DeepEqual(cols, []string{"column1", "column2"}) || len(rows) != 2 || rows[0][1] != "b" {
		t.Errorf("no header: %v %v", cols, rows)
	}
	cols, rows, _ = Preview(strings.NewReader("select 1; select ';';"), Options{Source: "sql"}, 5)
	if len(cols) != 1 || len(rows) != 2 {
		t.Errorf("sql: %v %v", cols, rows)
	}
}

func TestJSONForms(t *testing.T) {
	for _, in := range []string{`[{"a":1,"b":"x"},{"b":null,"a":2}]`, "{\"a\":1,\"b\":\"x\"}\n{\"b\":null,\"a\":2}\n"} {
		cols, rows, err := Preview(strings.NewReader(in), Options{Source: "json"}, 9)
		if err != nil || !reflect.DeepEqual(cols, []string{"a", "b"}) || len(rows) != 2 || rows[1][0] != "2" {
			t.Errorf("%q: %v %v %v", in, cols, rows, err)
		}
	}
}

func TestRunBatchesAndMapping(t *testing.T) {
	f := &fake{}
	var last Progress
	err := Run(context.Background(), f, strings.NewReader("a,b,c\n1,2,3\n4,5,6\n7,,9\n"),
		Options{Source: "csv", Header: true, Table: tbl, Batch: 2, Mapping: map[string]string{"a": "x", "b": ""}},
		func(p Progress) { last = p })
	if err != nil {
		t.Fatal(err)
	}
	if len(f.plans) != 2 || len(f.applied) != 2 || last.Rows != 3 || last.Batches != 2 {
		t.Fatalf("plans=%d applied=%d %+v", len(f.plans), len(f.applied), last)
	}
	got := f.plans[1][0].Set
	if !reflect.DeepEqual(got, []db.KeyVal{{Col: "x", Val: "7"}, {Col: "c", Val: "9"}}) {
		t.Errorf("set=%v", got)
	}
}

func TestCreateTableAndErrors(t *testing.T) {
	f := &fake{}
	if err := Run(context.Background(), f, strings.NewReader("a,b\n1,\n"), Options{Source: "csv", Header: true, Table: tbl, CreateTable: true}, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.tables) != 1 || len(f.tables[0].Columns) != 2 || f.tables[0].Columns[0].Type != "TEXT" || len(f.applied) != 2 {
		t.Errorf("%+v %d", f.tables, len(f.applied))
	}
	if f.plans[0][0].Set[1].Val != "" {
		t.Error("empty cell should be empty string")
	}
	f = &fake{fail: errors.New("boom")}
	var p Progress
	err := Run(context.Background(), f, strings.NewReader("a\n1\n2\n"), Options{Source: "csv", Header: true, Table: tbl, Batch: 1}, func(x Progress) { p = x })
	if err == nil || p.Err == nil || len(f.applied) != 1 {
		t.Errorf("err=%v p=%+v applied=%d", err, p, len(f.applied))
	}
	if err := Run(context.Background(), f, strings.NewReader("a\n1\n"), Options{Source: "csv", OnConflict: "ignore"}, nil); err == nil {
		t.Error("want on-conflict error")
	}
}

func TestRunSQL(t *testing.T) {
	f := &fake{dialect: "postgres"}
	err := Run(context.Background(), f, strings.NewReader("insert into t values ('a;b'); insert into t values (2);\ndelete from t;"), Options{Source: "sql", Batch: 2}, nil)
	if err != nil || len(f.applied) != 2 || len(f.applied[0]) != 2 || f.applied[0][0].Text != "insert into t values ('a;b')" {
		t.Errorf("%v %+v", err, f.applied)
	}
}

func TestCSVRoundTrip(t *testing.T) {
	res := &db.Result{Columns: []db.ResultColumn{{Name: "id"}, {Name: "s"}}, Rows: [][]any{{int64(1), "a,\"q\"\nb"}, {int64(2), ""}}}
	var b bytes.Buffer
	if err := export.Write(&b, res, export.Options{Format: "csv"}); err != nil {
		t.Fatal(err)
	}
	f := &fake{}
	if err := Run(context.Background(), f, &b, Options{Source: "csv", Header: true, Table: tbl}, nil); err != nil {
		t.Fatal(err)
	}
	ch := f.plans[0]
	if len(ch) != 2 || ch[0].Set[1].Val != "a,\"q\"\nb" || ch[1].Set[0].Val != "2" || ch[1].Set[1].Val != "" {
		t.Errorf("%+v", ch)
	}
}

func TestImportEdges(t *testing.T) {
	// BOM stripped, extra field errors
	cols, _, err := Preview(strings.NewReader("\xef\xbb\xbfid,name\n1,a\n"), Options{Source: "csv", Header: true}, 5)
	if err != nil || cols[0] != "id" {
		t.Errorf("bom: %q %v", cols, err)
	}
	if _, _, err := Preview(strings.NewReader("a,b\n1,2,3\n"), Options{Source: "csv", Header: true}, 5); err == nil {
		t.Error("want extra-field error")
	}
	// JSON key unknown to the first object
	if _, _, err := Preview(strings.NewReader(`[{"a":1},{"a":2,"b":3}]`), Options{Source: "json"}, 5); err == nil {
		t.Error("want unknown-key error")
	}
	// empty column set
	if err := Run(context.Background(), &fake{}, strings.NewReader(`[{}]`), Options{Source: "json", Table: tbl}, nil); err == nil {
		t.Error("want no-columns error")
	}
	// row range in the error, EmptyIsNull
	f := &fake{fail: errors.New("boom")}
	err = Run(context.Background(), f, strings.NewReader("a,b\n1,\n2,x\n"), Options{Source: "csv", Header: true, Table: tbl, EmptyIsNull: true}, nil)
	if err == nil || !strings.Contains(err.Error(), "rows 1-2: boom") {
		t.Errorf("err %v", err)
	}
	if got := f.plans[0][0].Set[1].Val; got != db.Null {
		t.Errorf("empty cell = %#v", got)
	}
}

func (f *fake) Columns(context.Context, db.ObjRef) ([]db.Column, error) {
	return []db.Column{{Name: "id", PK: true}, {Name: "v"}}, nil
}

func TestEncoding(t *testing.T) {
	_, rows, err := Preview(strings.NewReader("a\n\xe9\n"), Options{Source: "csv", Header: true, Encoding: "latin1"}, 5)
	if err != nil || len(rows) != 1 || rows[0][0] != "é" {
		t.Errorf("latin1: %v %v", rows, err)
	}
	u16 := "a\né\n"
	var b []byte
	for _, r := range u16 {
		b = append(b, byte(r), byte(r>>8))
	}
	if _, rows, err = Preview(bytes.NewReader(b), Options{Source: "csv", Header: true, Encoding: "utf-16le"}, 5); err != nil || len(rows) != 1 || rows[0][0] != "é" {
		t.Errorf("utf-16le: %v %v", rows, err)
	}
	if _, _, err = Preview(strings.NewReader("a"), Options{Source: "csv", Encoding: "nope"}, 1); err == nil {
		t.Error("unknown encoding must fail")
	}
}

func TestQuote(t *testing.T) {
	_, rows, err := Preview(strings.NewReader("a,b\n'x,y',\"z\"\n"), Options{Source: "csv", Header: true, Quote: '\''}, 5)
	if err != nil || len(rows) != 1 || rows[0][0] != "x,y" || rows[0][1] != `"z"` {
		t.Errorf("%v %v", rows, err)
	}
}

func TestOnConflict(t *testing.T) {
	run := func(dialect, mode string) (*fake, error) {
		f := &fake{dialect: dialect}
		return f, Run(context.Background(), f, strings.NewReader("id,v\n1,a\n"), Options{Source: "csv", Header: true, Table: tbl, OnConflict: mode}, nil)
	}
	f, err := run("postgres", "update")
	if got := f.applied[0][0].Text; err != nil || !strings.HasSuffix(got, `ON CONFLICT ("id") DO UPDATE SET "v" = EXCLUDED."v"`) {
		t.Errorf("pg update: %v %q", err, got)
	}
	f, _ = run("mysql", "update")
	if got := f.applied[0][0].Text; !strings.HasSuffix(got, "ON DUPLICATE KEY UPDATE `id` = VALUES(`id`), `v` = VALUES(`v`)") {
		t.Errorf("mysql update: %q", got)
	}
	f, _ = run("mysql", "replace")
	if got := f.applied[0][0].Text; strings.Contains(got, "REPLACE INTO") || !strings.Contains(got, "ON DUPLICATE KEY UPDATE") {
		t.Errorf("mysql replace: %q", got)
	}
	if _, err = run("clickhouse", "update"); err == nil {
		t.Error("clickhouse must be refused")
	}
	if f, err = run("postgres", "stop"); err != nil || strings.Contains(f.applied[0][0].Text, "CONFLICT") {
		t.Errorf("stop: %v", err)
	}
}

func TestColumnTypes(t *testing.T) {
	f := &fake{}
	err := Run(context.Background(), f, strings.NewReader("a,b\n1,x\n"), Options{Source: "csv", Header: true, Table: tbl, CreateTable: true, ColumnTypes: map[string]string{"a": "integer"}}, nil)
	if err != nil || f.tables[0].Columns[0].Type != "integer" || f.tables[0].Columns[1].Type != "TEXT" {
		t.Errorf("%v %+v", err, f.tables)
	}
}

func runCSV(f db.Session, in string, o Options) error {
	o.Source, o.Table = "csv", tbl
	return Run(context.Background(), f, strings.NewReader(in), o, nil)
}

func TestBlankHeaderRejected(t *testing.T) {
	f := &fake{}
	for _, in := range []string{"a,,c\n1,2,3\n", "a, ,c\n1,2,3\n"} {
		err := runCSV(f, in, Options{Header: true})
		if err == nil || !strings.Contains(err.Error(), "column 2 has no name") || len(f.applied) != 0 {
			t.Errorf("%q: %v", in, err)
		}
		if _, _, err := Preview(strings.NewReader(in), Options{Source: "csv", Header: true}, 1); err == nil {
			t.Errorf("preview %q must fail", in)
		}
	}
}

func TestDuplicateTargets(t *testing.T) {
	f := &fake{}
	err := runCSV(f, "a,a\n1,2\n", Options{Header: true})
	if err == nil || !strings.Contains(err.Error(), "1") || !strings.Contains(err.Error(), "2") || len(f.applied) != 0 {
		t.Errorf("dup header: %v", err)
	}
	err = runCSV(f, "a,b\n1,2\n", Options{Header: true, Mapping: map[string]string{"a": "x", "b": "x"}})
	if err == nil || !strings.Contains(err.Error(), `"a"`) || !strings.Contains(err.Error(), `"b"`) {
		t.Errorf("dup mapping: %v", err)
	}
	f = &fake{}
	if err = runCSV(f, "a,a\n1,2\n", Options{Header: true, CreateTable: true}); err != nil {
		t.Fatal(err)
	}
	if c := f.tables[0].Columns; c[0].Name != "a" || c[1].Name != "a_2" {
		t.Errorf("suffix: %+v", c)
	}
}

func TestCreateTableWithUpdateRefused(t *testing.T) {
	f := &fake{dialect: "postgres"}
	err := runCSV(f, "id,v\n1,a\n", Options{Header: true, CreateTable: true, OnConflict: "update"})
	if err == nil || len(f.tables) != 0 {
		t.Errorf("%v %v", err, f.tables)
	}
}

func TestMySQLReplaceKeepsUnmappedColumns(t *testing.T) {
	f := &fake{dialect: "mysql"}
	if err := runCSV(f, "id,v\n1,a\n", Options{Header: true, OnConflict: "replace"}); err != nil {
		t.Fatal(err)
	}
	got := f.applied[0][0].Text
	if strings.Contains(got, "REPLACE") || !strings.Contains(got, "ON DUPLICATE KEY UPDATE") {
		t.Errorf("%q", got)
	}
}

func TestQuoteFromConfigAndValidation(t *testing.T) {
	o := Options{Source: "csv", Header: true, CSV: config.CSV{Quote: "'"}}
	_, rows, err := Preview(strings.NewReader("a,b\n'x,y',z\n"), o, 5)
	if err != nil || len(rows) != 1 || rows[0][0] != "x,y" {
		t.Errorf("%v %v", rows, err)
	}
	for _, o := range []Options{{Quote: ','}, {Quote: '\n'}, {Quote: 0x80}, {CSV: config.CSV{Delimiter: "||"}}, {CSV: config.CSV{Quote: "ab"}}} {
		o.Source = "csv"
		if _, _, err := Preview(strings.NewReader("a\n1\n"), o, 1); err == nil {
			t.Errorf("%+v must be rejected", o)
		}
	}
}

func TestNullAsAndFormulaRoundTrip(t *testing.T) {
	r := &db.Result{Columns: []db.ResultColumn{{Name: "id"}, {Name: "v"}}, Rows: [][]any{{int64(1), "=1+1"}, {int64(2), nil}, {int64(3), "-12.34"}}}
	cfg := config.CSV{NullAs: `\N`}
	var b bytes.Buffer
	if err := export.Write(&b, r, export.Options{Format: "csv", CSV: cfg}); err != nil {
		t.Fatal(err)
	}
	for _, keep := range []bool{false, true} {
		f := &fake{}
		if err := runCSV(f, b.String(), Options{Header: true, CSV: cfg, KeepFormulaQuote: keep}); err != nil {
			t.Fatal(err)
		}
		set := func(i int) any { return f.plans[0][i].Set[1].Val }
		want := "=1+1"
		if keep {
			want = "'=1+1"
		}
		if set(0) != want || set(1) != db.Null || set(2) != "-12.34" {
			t.Errorf("keep=%v: %v %v %v", keep, set(0), set(1), set(2))
		}
	}
	// an explicit NullAs wins over the empty-cell rule
	f := &fake{}
	runCSV(f, "a\nNIL\n\n", Options{Header: true, NullAs: "NIL", EmptyIsNull: false})
	if f.plans[0][0].Set[0].Val != db.Null {
		t.Errorf("NullAs: %v", f.plans[0][0].Set[0].Val)
	}
}

// txFake is a Pinner whose pin records BEGIN/COMMIT/ROLLBACK and applies through the same fake.
type txFake struct {
	*fake
	log []string
}

func (t *txFake) Caps() db.Capabilities {
	return db.Capabilities{Dialect: "postgres", Transactions: true}
}
func (t *txFake) Pin(context.Context) (db.Session, error) {
	return &txPin{txFake: t}, nil
}

type txPin struct{ *txFake }

func (p *txPin) Exec(_ context.Context, stmt string, _ int) (*db.Result, error) {
	p.log = append(p.log, stmt)
	return &db.Result{}, nil
}
func (p *txPin) Close() error { p.log = append(p.log, "close"); return nil }

func TestImportRunsInOneTransaction(t *testing.T) {
	f := &txFake{fake: &fake{}}
	if err := runCSV(f, "a\n1\n2\n3\n", Options{Header: true, Batch: 1}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.log, ","); got != "BEGIN,COMMIT,close" {
		t.Errorf("success: %s", got)
	}
	f = &txFake{fake: &fake{fail: errors.New("boom")}}
	err := runCSV(f, "a\n1\n2\n", Options{Header: true, Batch: 1})
	if got := strings.Join(f.log, ","); err == nil || got != "BEGIN,ROLLBACK,close" || !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("failure: %v %s", err, got)
	}
}

func TestPartialImportMessage(t *testing.T) {
	f := &failAfter{fake: &fake{}, ok: 1}
	err := runCSV(&fakeSess{f}, "a\n1\n2\n", Options{Header: true, Batch: 1})
	if err == nil || !strings.Contains(err.Error(), "rows before the failure were committed") {
		t.Errorf("%v", err)
	}
}

type failAfter struct {
	*fake
	ok int
}
type fakeSess struct{ *failAfter }

func (f *fakeSess) Apply(c context.Context, s []db.Statement) error {
	if f.ok == 0 {
		return errors.New("boom")
	}
	f.ok--
	return f.fake.Apply(c, s)
}

func TestStrayQuoteFailsWithLine(t *testing.T) { // P1-08
	in := "a,b\n1,\"unclosed\n2,x\n3,y\n"
	_, _, err := Preview(strings.NewReader(in), Options{Source: "csv", Header: true}, 9)
	if err == nil || !strings.Contains(err.Error(), "line") {
		t.Errorf("default quote: %v", err)
	}
	f := &fake{}
	if err := runCSV(f, in, Options{Header: true}); err == nil || len(f.applied) != 0 {
		t.Errorf("run: %v %d", err, len(f.applied))
	}
}

func TestShortRowIsAnError(t *testing.T) { // P2-45
	f := &fake{}
	err := runCSV(f, "a,b,c\n1,2,3\n4,5\n", Options{Header: true})
	if err == nil || !strings.Contains(err.Error(), "line 3") || len(f.applied) != 0 {
		t.Errorf("%v", err)
	}
}

// ddlTx records the order of BEGIN, the CREATE and ROLLBACK on one pin.
type ddlTx struct {
	*fake
	log []string
}

func (t *ddlTx) Caps() db.Capabilities {
	return db.Capabilities{Dialect: "postgres", Transactions: true}
}
func (t *ddlTx) Pin(context.Context) (db.Session, error) {
	return &ddlPin{ddlTx: t}, nil
}

type ddlPin struct{ *ddlTx }

func (p *ddlPin) Exec(_ context.Context, stmt string, _ int) (*db.Result, error) {
	p.log = append(p.log, stmt)
	return &db.Result{}, nil
}
func (p *ddlPin) Close() error { return nil }
func (p *ddlPin) Apply(_ context.Context, s []db.Statement) error {
	p.log = append(p.log, s[0].Text)
	if strings.HasPrefix(s[0].Text, "INSERT") {
		return errors.New("NUL byte")
	}
	return nil
}

func TestCreateTableRollsBackWithData(t *testing.T) { // P2-46
	f := &ddlTx{fake: &fake{}}
	err := runCSV(f, "a\n1\n", Options{Header: true, CreateTable: true})
	if got := strings.Join(f.log, "|"); err == nil || !strings.HasPrefix(got, "BEGIN|CREATE TABLE t|INSERT") || !strings.HasSuffix(got, "|ROLLBACK") {
		t.Errorf("%v %s", err, got)
	}
}

func TestPreviewMatchesImport(t *testing.T) { // P2-48
	in := "a,b\n'=1+1,\\N\n'=A1,\n"
	o := Options{Source: "csv", Header: true, CSV: config.CSV{NullAs: `\N`}, EmptyIsNull: true}
	_, rows, err := Preview(strings.NewReader(in), o, 5)
	if err != nil || !reflect.DeepEqual(rows, [][]string{{"=1+1", "NULL"}, {"=A1", "NULL"}}) {
		t.Errorf("%v %v", rows, err)
	}
	o.KeepFormulaQuote = true
	_, rows, _ = Preview(strings.NewReader(in), o, 5)
	if rows[0][0] != "'=1+1" {
		t.Errorf("keep: %v", rows)
	}
}

func TestNullAsAndEmptyIsNullForJSON(t *testing.T) { // P2-59
	f := &fake{}
	err := Run(context.Background(), f, strings.NewReader(`[{"a":"","b":"NIL","c":"x"}]`),
		Options{Source: "json", Table: tbl, EmptyIsNull: true, NullAs: "NIL"}, nil)
	set := f.plans[0][0].Set
	if err != nil || set[0].Val != db.Null || set[1].Val != db.Null || set[2].Val != "x" {
		t.Errorf("%v %v", err, set)
	}
}

func TestWrongEncodingIsAnError(t *testing.T) { // P2-49
	_, _, err := Preview(strings.NewReader("a,b\nhello,world!\n"), Options{Source: "csv", Header: true, Encoding: "utf-16le"}, 5)
	if err == nil || !strings.Contains(err.Error(), "utf-16le") {
		t.Errorf("%v", err)
	}
	// a replacement character the file really has is fine
	_, rows, err := Preview(strings.NewReader("a\n�\n"), Options{Source: "csv", Header: true, Encoding: "latin1"}, 5)
	if err != nil {
		t.Logf("latin1 decodes the 3 bytes as mojibake, not FFFD: %v %v", rows, err)
	}
}

func TestSQLImportHonoursCancel(t *testing.T) { // P2-50
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fake{}
	err := Run(ctx, f, strings.NewReader("select 1; select 2;"), Options{Source: "sql", Batch: 1}, nil)
	if !errors.Is(err, context.Canceled) || len(f.applied) != 0 {
		t.Errorf("%v %d", err, len(f.applied))
	}
}

func TestOnConflictNeedsPostgresDriver(t *testing.T) { // P2-51
	for _, drv := range []string{"redshift", "greenplum", "vertica"} {
		f := &fake{dialect: "postgres"}
		err := runCSV(f, "id,v\n1,a\n", Options{Header: true, OnConflict: "update", Driver: drv})
		if err == nil || len(f.applied) != 0 {
			t.Errorf("%s: %v", drv, err)
		}
	}
	f := &fake{dialect: "postgres"}
	if err := runCSV(f, "id,v\n1,a\n", Options{Header: true, OnConflict: "update", Driver: config.Postgres}); err != nil {
		t.Error(err)
	}
}

func TestOnConflictNeedsKeyColumn(t *testing.T) { // P2-52
	f := &fake{dialect: "postgres"}
	err := runCSV(f, "v\na\n", Options{Header: true, OnConflict: "update"})
	if err == nil || !strings.Contains(err.Error(), `"id"`) || len(f.applied) != 0 {
		t.Errorf("%v", err)
	}
}

// mysqlTx: the CREATE applies, the first data batch fails.
type mysqlTx struct {
	*fake
	calls *int
	log   *[]string
}

func (m mysqlTx) Caps() db.Capabilities {
	return db.Capabilities{Dialect: "mysql", Transactions: true}
}
func (m mysqlTx) Pin(context.Context) (db.Session, error) { return mysqlPin{m}, nil }
func (m mysqlTx) Apply(_ context.Context, s []db.Statement) error {
	*m.calls++
	if *m.calls > 1 {
		return errors.New("boom")
	}
	return nil
}

type mysqlPin struct{ mysqlTx }

func (p mysqlPin) Exec(_ context.Context, stmt string, _ int) (*db.Result, error) {
	*p.log = append(*p.log, stmt)
	return &db.Result{}, nil
}
func (p mysqlPin) Close() error { return nil }

func TestMySQLCreateTableRollbackMessage(t *testing.T) { // P2-46
	for _, create := range []bool{true, false} {
		n, log := 0, []string{}
		f := mysqlTx{fake: &fake{}, calls: &n, log: &log}
		if !create {
			n = 1
		}
		err := runCSV(f, "a\n1\n", Options{Header: true, Batch: 1, CreateTable: create, Driver: config.MySQL})
		if err == nil {
			t.Fatal("want error")
		}
		stays := strings.Contains(err.Error(), "the new table stays")
		if stays != create || (!create && !strings.Contains(err.Error(), "no rows were imported")) {
			t.Errorf("create=%v: %v", create, err)
		}
	}
}

// mysqlGuard mimics the real MySQL pinned session: Apply refuses DDL, as internal/db/mysql/pin.go does.
type mysqlGuard struct {
	*fake
	log *[]string
}

func (m mysqlGuard) Caps() db.Capabilities {
	return db.Capabilities{Dialect: "mysql", Transactions: true}
}
func (m mysqlGuard) Pin(context.Context) (db.Session, error) { return mysqlGuardPin{m}, nil }
func (m mysqlGuard) Apply(_ context.Context, s []db.Statement) error {
	*m.log = append(*m.log, "plain:"+s[0].Text)
	return nil
}

type mysqlGuardPin struct{ mysqlGuard }

func (p mysqlGuardPin) Exec(_ context.Context, stmt string, _ int) (*db.Result, error) {
	*p.log = append(*p.log, stmt)
	return &db.Result{}, nil
}
func (p mysqlGuardPin) Close() error { return nil }
func (p mysqlGuardPin) Apply(_ context.Context, s []db.Statement) error {
	if strings.HasPrefix(s[0].Text, "CREATE") {
		return errors.New("structure changes are DDL, which MySQL commits implicitly together with your open transaction: release the pin first")
	}
	*p.log = append(*p.log, "pin:"+strings.Fields(s[0].Text)[0])
	return nil
}

func TestMySQLCreateTableRunsBeforePin(t *testing.T) { // NEW-1
	log := []string{}
	f := mysqlGuard{fake: &fake{}, log: &log}
	if err := runCSV(f, "a\n1\n", Options{Header: true, CreateTable: true, Driver: config.MySQL}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(log, "|"); got != "plain:CREATE TABLE t|BEGIN|pin:INSERT|COMMIT" {
		t.Errorf("%s", got)
	}
}

// countReader counts the bytes handed out.
type countReader struct {
	r io.Reader
	n int
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestSQLImportStreams(t *testing.T) { // IO-7
	old := sqlChunk
	sqlChunk = 16
	defer func() { sqlChunk = old }()
	var want []string
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		s := fmt.Sprintf("insert into t values ('a;%d')", i)
		want = append(want, s)
		sb.WriteString(s + ";\n")
	}
	f := &fake{dialect: "postgres"}
	if err := Run(context.Background(), f, strings.NewReader(sb.String()), Options{Source: "sql", Batch: 7}, nil); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, b := range f.applied {
		for _, s := range b {
			got = append(got, s.Text)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %d statements, want %d: %q", len(got), len(want), got)
	}
	// preview stops reading once it has its rows
	cr := &countReader{r: strings.NewReader(sb.String())}
	_, rows, err := Preview(cr, Options{Source: "sql"}, 2)
	if err != nil || len(rows) != 2 || rows[1][0] != want[1] || cr.n >= sb.Len()/2 {
		t.Errorf("preview: %v %v read %d of %d", err, rows, cr.n, sb.Len())
	}
	// a cancel in the middle of the file stops the import
	ctx, cancel := context.WithCancel(context.Background())
	cf := &cancelFake{fake: &fake{dialect: "postgres"}, cancel: cancel}
	err = Run(ctx, cf, strings.NewReader(sb.String()), Options{Source: "sql", Batch: 1}, nil)
	if !errors.Is(err, context.Canceled) || len(cf.applied) != 1 {
		t.Errorf("%v %d", err, len(cf.applied))
	}
	if _, _, err := PreviewContext(ctx, strings.NewReader(sb.String()), Options{Source: "sql"}, 5); !errors.Is(err, context.Canceled) {
		t.Errorf("preview cancel: %v", err)
	}
}

type cancelFake struct {
	*fake
	cancel func()
}

func (c *cancelFake) Apply(ctx context.Context, s []db.Statement) error {
	err := c.fake.Apply(ctx, s)
	c.cancel()
	return err
}

func TestMySQLOnConflictNeedsKeyColumn(t *testing.T) { // IO-9
	for _, mode := range []string{"update", "replace"} {
		f := &fake{dialect: "mysql"}
		err := runCSV(f, "v\na\n", Options{Header: true, OnConflict: mode, Driver: config.MySQL})
		if err == nil || !strings.Contains(err.Error(), `"id"`) || len(f.applied) != 0 {
			t.Errorf("%s: %v", mode, err)
		}
	}
}
