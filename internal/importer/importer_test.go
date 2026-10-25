package importer

import (
	"bytes"
	"context"
	"errors"
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
