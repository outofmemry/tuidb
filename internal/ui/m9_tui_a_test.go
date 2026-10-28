package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
	"webtuidb/internal/keymap"
)

func TestM9FindOperatorsAndColumn(t *testing.T) {
	g := testGrid() // names Ann, Bob, Cy; notes nil, x, "y,z"
	if !g.FindSpec(FindSpec{Text: "b", Op: FindPrefix}, 1) || g.row != 1 || g.col != 1 {
		t.Errorf("prefix: %d,%d", g.row, g.col)
	}
	g.GoRow(0, false)
	g.GoCol(0, false)
	if !g.FindSpec(FindSpec{Text: "y", Col: "name", Op: FindSuffix}, 1) || g.row != 2 {
		t.Errorf("suffix in name: %d,%d", g.row, g.col)
	}
	if g.FindSpec(FindSpec{Text: "x", Col: "name", Op: FindContains}, 1) {
		t.Error("x is only in the note column")
	}
	if !g.FindSpec(FindSpec{Text: "ann", Col: "name", Op: FindNotContains}, 1) || g.row != 1 {
		t.Errorf("not contains: %d", g.row)
	}
	if g.FindSpec(FindSpec{Text: "ANN", Op: FindContains, Case: true}, 1) {
		t.Error("match case")
	}
}

func TestM9ReplaceGoesThroughPendingEdits(t *testing.T) {
	g := testGrid()
	if n := g.Replace(FindSpec{Text: "B", Col: "name", Op: FindContains}, "Rob", true); n != 1 || g.Rows[1].Cur[1] != "RoboRob" || !g.Rows[1].Dirty[1] {
		t.Fatalf("replace all: %d %v", n, g.Rows[1].Cur)
	}
	if !g.Undo() || g.Rows[1].Cur[1] != "Bob" {
		t.Error("replace all is one undo step")
	}
	g.GoRow(2, false)
	g.GoCol(1, false)
	if n := g.Replace(FindSpec{Text: "c", Op: FindPrefix}, "S", false); n != 1 || g.Rows[2].Cur[1] != "Sy" {
		t.Errorf("replace one on a prefix: %d %v", n, g.Rows[2].Cur)
	}
	g.Editable = false
	if g.Replace(FindSpec{Text: "S"}, "x", true) != 0 {
		t.Error("read-only grid never replaces")
	}
}

func TestM9MultiRowEditStagesOnePerRow(t *testing.T) {
	g := testGrid()
	g.GoCol(1, false)
	g.GoRow(0, false)
	g.GoRow(2, true) // rows 1..3 selected
	g.MoveField(1)   // the detail pane walks fields without losing the rows
	g.MoveField(-1)
	if n := g.SetColumn("Z"); n != 3 {
		t.Fatalf("set column touched %d rows", n)
	}
	if len(g.Changes(db.ObjRef{Name: "t"})) != 3 {
		t.Error("one pending update per selected row")
	}
	g.Undo()
	if g.HasPending() {
		t.Error("one undo step clears all three")
	}
}

func TestM11PrettyMinify(t *testing.T) {
	g := &Grid{Editable: true, W: 40, H: 5}
	g.SetData([]gridCol{{Name: "id"}, {Name: "doc"}}, [][]any{{int64(1), `{"a":1,"b":[1,2]}`}, {int64(2), "plain"}})
	g.GoCol(1, false)
	g.GoRow(0, false)
	g.GoRow(1, true)
	if n := g.MapColumn(func(v any) (any, bool) { return reformatJSON(v, true) }); n != 1 || !strings.Contains(g.Rows[0].Cur[1].(string), "\n") {
		t.Fatalf("pretty: %d %v", n, g.Rows[0].Cur[1])
	}
	g.MapColumn(func(v any) (any, bool) { return reformatJSON(v, false) })
	if g.Rows[0].Cur[1] != `{"a":1,"b":[1,2]}` || g.Rows[1].Cur[1] != "plain" {
		t.Errorf("minify: %v", g.Rows[0].Cur[1])
	}
}

func TestM9QuickLookMinify(t *testing.T) {
	q := newQuickLook("doc", "{\n  \"a\": 1\n}")
	q.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if !q.raw || q.shown() != `{"a":1}` {
		t.Errorf("m shows the raw text minified: %q", q.shown())
	}
	q.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if !strings.Contains(q.shown(), "\n") {
		t.Error("m again pretty-prints")
	}
}

func TestM9EditableTableUsesEngineCheck(t *testing.T) {
	if _, tbl, ok := editableTable("select * from customers c where c.id in (select 1) order by 1", "postgres"); !ok || tbl != "customers" {
		t.Errorf("alias and subquery in WHERE are fine: %v %q", ok, tbl)
	}
	if _, _, ok := editableTable("select * from a join b on a.id = b.id", "postgres"); ok {
		t.Error("a join is not editable")
	}
}

func TestM11TriggerEditorMakesTheSameScript(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	ref := db.ObjRef{Schema: "public", Name: "customers", Kind: db.KindTable}
	tr := db.Trigger{Name: "audit", Timing: "AFTER", Event: "INSERT", Statement: "FOR EACH ROW EXECUTE FUNCTION f()"}
	d := h.a.ws().newTriggerEdit(ref, tr)
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	d.Update(tea.KeyPressMsg{Code: tea.KeyLeft}) // AFTER -> BEFORE
	got := d.trigger()
	if got.Timing != "BEFORE" || got.Name != "audit" || got.Event != "INSERT" {
		t.Fatalf("grid fields: %+v", got)
	}
	want := triggerSQL(h.a.ws().caps.Dialect, ref, got)
	n := len(h.a.ws().tabs)
	d.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if len(h.a.ws().tabs) != n+1 {
		t.Fatalf("ctrl+s should open the script tab (%d tabs)", len(h.a.ws().tabs))
	}
	tab, ok := h.a.ws().tab().(*queryTab)
	if !ok || !strings.Contains(tab.ed.Text(), want) || !strings.Contains(want, "BEFORE") {
		t.Errorf("the script tab does not hold the generated trigger SQL:\n tab: %q\nwant: %q", tab.ed.Text(), want)
	}
}

func TestM9ExportFileNamePattern(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	ws := h.a.ws()
	if got := ws.expandPath("/tmp/out/{table}-x.csv", "orders"); got != "/tmp/out/orders-x.csv" {
		t.Errorf("expandPath: %q", got)
	}
	if got := ws.expandPath("/tmp/{nope}/a.csv", "orders"); !strings.Contains(got, "/a.csv") {
		t.Errorf("only the file name is expanded: %q", got)
	}
}

func TestM12HelpListsBindingsInTableOrder(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.a.showHelp()
	hd := h.a.overlays[len(h.a.overlays)-1].(*helpDlg)
	var got []string
	for _, r := range hd.rows {
		if r.header == "" && r.ctx == keymap.Global {
			got = append(got, r.action)
		}
	}
	var want []string
	for _, b := range h.a.d.Keys.All() { // no-prefix bindings are listed too (M14 check 2)
		if b.Context == keymap.Global {
			want = append(want, b.Action)
		}
	}
	if len(got) == 0 || strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("help order %v, want the keymap table order %v", got, want)
	}
}
