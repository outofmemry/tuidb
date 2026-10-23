package board

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/db"
)

const example = `name = "Sales"
refresh_secs = 30
[[var]]
name = "region"
label = "Region"
default = "eu"
options = ["eu","us"]
[[panel]]
title = "Revenue"
kind = "line"
query = "select day, total from orders where region = $region"
chart = "x: day, y: total"
[[panel]]
title = "Count"
kind = "number"
query = "select count(*) from orders"
[[panel]]
title = "Bad"
kind = "table"
query = "select $nope"
`

type fake struct {
	db.Session
	got []string
}

func (f *fake) Caps() db.Capabilities { return db.Capabilities{Dialect: "postgres"} }
func (f *fake) Exec(_ context.Context, s string, _ int) (*db.Result, error) {
	f.got = append(f.got, s)
	if strings.Contains(s, "count(") {
		return &db.Result{Columns: []db.ResultColumn{{Name: "c"}}, Rows: [][]any{{int64(7)}}}, nil
	}
	if strings.Contains(s, "boom") {
		return nil, errors.New("boom")
	}
	return &db.Result{Columns: []db.ResultColumn{{Name: "day"}, {Name: "total"}}, Rows: [][]any{{"mon", int64(1)}, {"tue", int64(2)}}}, nil
}

func TestBoard(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	p := filepath.Join(t.TempDir(), "s.toml")
	os.WriteFile(p, []byte(example), 0o600)
	b, err := Load(p)
	if err != nil || b.Name != "Sales" || b.RefreshSecs != 30 || len(b.Vars) != 1 || b.Vars[0].Options[1] != "us" || len(b.Panels) != 3 {
		t.Fatalf("%v %+v", err, b)
	}
	b.Path = ""
	if err := Save(b); err != nil {
		t.Fatal(err)
	}
	l, _ := List()
	if len(l) != 1 || l[0].Name != "Sales" || len(l[0].Panels) != 3 {
		t.Fatalf("round trip: %+v", l)
	}
	f := &fake{}
	rs := Run(context.Background(), f, b, map[string]string{"region": "us"}, 100)
	if len(rs) != 3 || rs[0].Err != nil || len(rs[0].Series.Values) != 2 || !strings.Contains(f.got[0], "'us'") {
		t.Fatalf("line: %+v %v", rs[0], f.got)
	}
	if rs[1].Number != "7" || rs[2].Err == nil {
		t.Errorf("number/bad: %+v %+v", rs[1], rs[2])
	}
	if fb := FromChart("x", "select 1", db.ChartSpec{Kind: "line", X: "a", Y: "b"}); fb.Panels[0].Chart != "x: a, y: b" {
		t.Errorf("%+v", fb)
	}
}

func TestBoardGuards(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	rs := Run(context.Background(), &fake{}, Board{Panels: []Panel{{Kind: "table", Query: "delete from t"}}}, nil, 10)
	if rs[0].Err == nil {
		t.Error("write panel ran")
	}
	f := &fake{}
	rs = Run(context.Background(), f, Board{Vars: []Var{{Name: "n", Default: "-5"}}, Panels: []Panel{{Kind: "table", Query: "select 1 -$n"}}}, nil, 10)
	if rs[0].Err != nil || f.got[0] != "select 1 -(-5)" {
		t.Fatal(rs[0].Err, f.got)
	}
	out := filepath.Join(t.TempDir(), "evil.toml")
	if err := Save(Board{Name: "A", Path: out}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("wrote outside Dir")
	}
	if err := Save(Board{Name: "A"}); err == nil {
		t.Error("overwrote an existing board")
	}
}

func TestFromChartKeepsEverySeries(t *testing.T) {
	fb := FromChart("x", "select 1", db.ChartSpec{Kind: "bar", X: "a", Y: "b", Ys: []string{"b", "c"}})
	if got := fb.Panels[0].Chart; got != "x: a, y: b, c" {
		t.Errorf("chart %q", got)
	}
	if spec, _ := db.ParseChart("-- barchart " + fb.Panels[0].Chart); len(spec.Ys) != 2 {
		t.Errorf("round trip lost a series: %+v", spec)
	}
}

func TestSaveMethodRecordsPath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	b := Board{Name: "Sales"}
	if err := b.Save(); err != nil || b.Path == "" {
		t.Fatalf("%v path %q", err, b.Path)
	}
	if err := b.Save(); err != nil {
		t.Errorf("second save of the same board: %v", err)
	}
}

type mongoFake struct{ fake }

func (mongoFake) Caps() db.Capabilities { return db.Capabilities{Dialect: "mongo"} }

func TestMongoPanelRefusesUnboundVar(t *testing.T) {
	b := Board{Vars: []Var{{Name: "region", Default: "eu"}}, Panels: []Panel{
		{Kind: "table", Query: `db.o.find({region: "$region"})`},
		{Kind: "table", Query: `db.o.aggregate([{$match: {a: 1}}])`}}}
	rs := Run(context.Background(), &mongoFake{}, b, nil, 10)
	if rs[0].Err == nil || rs[1].Err != nil {
		t.Errorf("var panel err %v, operator panel err %v", rs[0].Err, rs[1].Err)
	}
}

func TestBoardButtonsAndDependents(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	b := Board{Name: "B", Panels: []Panel{
		{Title: "go", Kind: "button", Target: "n"},
		{Title: "imp", Kind: "import", Table: "t"},
		{Title: "n", Kind: "number", Query: "select count(*) from t", RefreshOn: "go"},
	}}
	f := &fake{}
	if rs := Run(context.Background(), f, b, nil, 10); len(rs) != 1 || rs[0].Number != "7" {
		t.Fatalf("%+v", rs)
	}
	if d := Dependents(b, "go"); len(d) != 1 || d[0].Title != "n" {
		t.Fatal(d)
	}
	if r := RunPanel(context.Background(), f, b, b.Panels[2], nil, 10); r.Number != "7" {
		t.Fatal(r)
	}
	if err := Save(b); err != nil {
		t.Fatal(err)
	}
	l, _ := List()
	if len(l) != 1 || l[0].Panels[0].Target != "n" || l[0].Panels[1].Table != "t" || l[0].Panels[2].RefreshOn != "go" {
		t.Fatalf("%+v", l)
	}
}

func TestInputDropdownExporter(t *testing.T) {
	b := Board{Panels: []Panel{
		{Title: "Region", Kind: "dropdown", Var: "region", Options: []string{"eu", "us"}, Default: "eu"},
		{Title: "Pick", Kind: "dropdown", Var: "day", Query: "select day from orders"},
		{Title: "N", Kind: "input", Var: "n", Default: "5"},
		{Title: "Out", Kind: "exporter", Format: "csv", Query: "select day, total from orders where n = $n and r = $region"},
	}}
	f := &fake{}
	if got := Run(context.Background(), f, b, nil, 10); len(got) != 0 || len(f.got) != 0 {
		t.Fatalf("input/dropdown/exporter must be skipped: %v %v", got, f.got)
	}
	res, err := ExportPanel(context.Background(), f, b, b.Panels[3], map[string]string{"region": "us"}, 10)
	if err != nil || len(res.Rows) != 2 || len(f.got) != 1 || !strings.Contains(f.got[0], "n = '5'") && !strings.Contains(f.got[0], "n = 5") || !strings.Contains(f.got[0], "us") {
		t.Fatalf("%v %v %v", err, res, f.got)
	}
	if o, _, _ := DropdownOptions(context.Background(), f, b, b.Panels[0], nil); len(o) != 2 || o[1] != "us" {
		t.Fatal(o)
	}
	if o, _, err := DropdownOptions(context.Background(), f, b, b.Panels[1], nil); err != nil || len(o) != 2 || o[0] != "mon" {
		t.Fatal(o, err)
	}
}

func TestDropdownSkipsNullAndFormatsBytes(t *testing.T) { // P2-55, P2-56
	f := &optFake{rows: [][]any{{"a"}, {nil}, {[]byte("hi")}, {int64(3)}}}
	b := Board{Panels: []Panel{{Kind: "dropdown", Var: "v", Query: "select x"}}}
	o, cut, err := DropdownOptions(context.Background(), f, b, b.Panels[0], nil)
	if cut || err != nil || strings.Join(o, "|") != "a|\\x6869|3" {
		t.Errorf("%v %q", err, o)
	}
	if DropdownLimit <= 500 || f.limit != DropdownLimit {
		t.Errorf("limit %d / %d", DropdownLimit, f.limit)
	}
}

type optFake struct {
	fake
	rows  [][]any
	limit int
}

func (f *optFake) Exec(_ context.Context, _ string, limit int) (*db.Result, error) {
	f.limit = limit
	return &db.Result{Columns: []db.ResultColumn{{Name: "x"}}, Rows: f.rows}, nil
}

func TestDropdownTruncatedCountsRawRows(t *testing.T) { // NULLs are skipped, but the limit is judged on the raw rows
	rows := make([][]any, DropdownLimit)
	for i := range rows {
		rows[i] = []any{nil}
	}
	rows[0] = []any{"a"}
	f := &optFake{rows: rows}
	b := Board{Panels: []Panel{{Kind: "dropdown", Var: "v", Query: "select x"}}}
	o, cut, err := DropdownOptions(context.Background(), f, b, b.Panels[0], nil)
	if err != nil || len(o) != 1 || !cut {
		t.Errorf("%v %d %v", err, len(o), cut)
	}
}
