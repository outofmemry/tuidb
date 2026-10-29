package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/board"
	"webtuidb/internal/config"
)

func TestBoardQueuesARunAndSurvivesNarrowPanes(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	v := &boardView{w: h.a.ws(), b: board.Board{Name: "b"}, running: true}
	if v.run() != nil || v.queued == nil {
		t.Fatal("a run during a run must be queued, not dropped")
	}
	for _, w := range []int{0, 1, 2, 3} {
		tile(h.a.th, &board.PanelResult{Panel: board.Panel{Title: "t", Kind: "table"}}, w, false)
	}
}

func TestM3BoardRunsItsPanels(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	writeTo(t, filepath.Join(config.DataDir(), "boards", "sales.toml"), `name = "Sales"
[[var]]
name = "who"
default = "x"
[[panel]]
title = "People"
kind = "table"
query = "select * from customers"
[[panel]]
title = "Total"
kind = "number"
query = "select 1"
`, 0o644)
	h := newHarness(t, nil)
	h.connect()
	h.prefix("m")
	h.want("Sales")
	h.press("enter")
	v := topView(h)
	for _, s := range []string{"Board Sales", "People", "ann@x.io", "Total", "who=x"} {
		if !strings.Contains(v, s) {
			t.Fatalf("board lacks %q:\n%s", s, v)
		}
	}
	h.press("esc")
	if len(h.a.overlays) != 0 {
		t.Fatal("Esc should close the board")
	}
}

func TestM3AddChartToBoard(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	h := newHarness(t, nil)
	h.connect()
	queryResult(h, "select * from customers")
	h.prefix("g")
	h.press("b")
	h.want("Add to Metrics Board")
	setField(t, h, "name", "cust")
	h.press("enter")
	if b, err := os.ReadFile(filepath.Join(config.DataDir(), "boards", "cust.toml")); err != nil || !strings.Contains(string(b), "select * from customers") {
		t.Fatalf("board file %q: %v", b, err)
	}
}

func TestM5BoardConnectionAndDuplicate(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	writeTo(t, filepath.Join(config.DataDir(), "boards", "sales.toml"), `name = "Sales"
connection = "elsewhere"
[[panel]]
title = "Total"
kind = "number"
query = "select 1"
`, 0o644)
	h := newHarness(t, nil)
	h.connect()
	h.prefix("m")
	h.press("enter")
	h.want("not open")
	h.press("esc")
	h.prefix("m")
	h.press("d")
	if _, err := os.Stat(filepath.Join(config.DataDir(), "boards", "Sales copy.toml")); err != nil {
		t.Fatalf("duplicate not saved: %v", err)
	}
}

func TestM7BoardButtonRunsItsTargetAndDependents(t *testing.T) {
	boardsDir(t)
	err := board.Save(board.Board{Name: "ops", Panels: []board.Panel{
		{Title: "Total", Kind: "number", Query: "select 1"},
		{Title: "Refresh", Kind: "button", Target: "Total"},
		{Title: "Detail", Kind: "table", Query: "select * from customers", RefreshOn: "Total"},
		{Title: "Other", Kind: "table", Query: "select 2"},
		{Title: "Load", Kind: "import", Table: "orders"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, nil)
	h.connect()
	h.run("metrics")
	h.press("enter")
	h.want("Board ops")
	h.want("[ ↵ ] run Total")
	before := len(h.fake.execs)
	h.press("tab", "enter") // the Refresh button
	ran := h.fake.execs[before:]
	if len(ran) != 2 || ran[0] != "select 1" || ran[1] != "select * from customers" {
		t.Fatalf("a button runs its target and the panels that refresh on it: %v", ran)
	}
	before = len(h.fake.execs)
	h.press("tab", "tab") // Other: runs alone
	h.press("enter")
	if ran := h.fake.execs[before:]; len(ran) != 1 || ran[0] != "select 2" {
		t.Fatalf("single panel: %v", ran)
	}
	h.press("tab", "enter") // Load: the import wizard aimed at orders
	h.want("Import data")
	if f := h.a.overlays[len(h.a.overlays)-1].(*formDlg); f.values()["table"] != "orders" {
		t.Fatalf("import form %v", f.values())
	}
}

func TestM7BoardsNewRenameDelete(t *testing.T) {
	boardsDir(t)
	h := newHarness(t, nil)
	h.connect()
	h.run("metrics")
	h.press("n")
	setField(t, h, "name", "sales")
	setField(t, h, "title", "Rows")
	setField(t, h, "query", "select 1")
	h.press("enter")
	if _, err := os.Stat(filepath.Join(board.Dir(), "sales.toml")); err != nil {
		t.Fatal(err)
	}
	h.want("sales")
	h.press("e")
	setField(t, h, "name", "revenue")
	h.press("enter")
	if _, err := os.Stat(filepath.Join(board.Dir(), "sales.toml")); err == nil {
		t.Fatal("the old file goes after a rename")
	}
	if b, err := board.Load(filepath.Join(board.Dir(), "revenue.toml")); err != nil || b.Name != "revenue" || len(b.Panels) != 1 {
		t.Fatalf("%+v %v", b, err)
	}
	h.want("revenue")
	h.press("x")
	setField(t, h, "confirm", "nope")
	h.press("enter")
	h.want("type “revenue”")
	setField(t, h, "confirm", "revenue")
	h.press("enter")
	if bs, _ := board.List(); len(bs) != 0 {
		t.Fatalf("boards left: %+v", bs)
	}
}
