package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/menus"
)

func TestM15MouseSelectsTab(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.a.run("new_query_tab")
	w := h.a.ws()
	if w.cur != 1 {
		t.Fatalf("cur %d", w.cur)
	}
	sbW, _, _, _, _ := layoutOf(w.showSB, w.showDetail, w.showConsole, h.a.w, h.a.bodyH())
	x := sbW + 1 + 3 + 2 // inside the first label
	h.a.Update(tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseLeft})
	if w.cur != 0 {
		t.Fatalf("clicking the first tab left cur=%d", w.cur)
	}
	// right click opens the tab menu
	h.a.Update(tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseRight})
	if got, want := h.menuLabels(), menus.Labels(menus.Tab, menus.Env{}); !sameLabels(got, want) {
		t.Errorf("right click menu %v", got)
	}
}

func TestM15MouseSidebarAndGrid(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	rows := w.sb.rows()
	if len(rows) < 3 {
		t.Fatalf("rows %d", len(rows))
	}
	w.focusSB = false
	// a click on the sidebar's third visible row selects it and gives the sidebar the focus
	h.a.Update(tea.MouseClickMsg{X: 4, Y: 1 + 1 + 2 + 2, Button: tea.MouseLeft})
	if !w.focusSB || w.sb.sel != 2 {
		t.Errorf("sidebar click: focus=%v sel=%d", w.focusSB, w.sb.sel)
	}
	w.sb.sel = 1 // customers
	h.press("enter")
	h.want("ann@x.io")
	t0 := w.tab().(*tableTab)
	sbW, _, _, _, _ := layoutOf(w.showSB, w.showDetail, w.showConsole, h.a.w, h.a.bodyH())
	gx := sbW + 1 + 2 // grid column 0
	// row 3 of the grid (header is the first line under the tab bar and the filter bar)
	h.a.Update(tea.MouseClickMsg{X: gx + t0.grid.gutterW() + 12, Y: 1 + 1 + t0.barLines() + 1 + 2, Button: tea.MouseLeft})
	if t0.grid.row != 2 {
		t.Errorf("grid click: row=%d", t0.grid.row)
	}
}

func TestM15HeaderClickSortsAndDragResizes(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	w := h.a.ws()
	t0 := w.tab().(*tableTab)
	sbW, _, _, _, _ := layoutOf(w.showSB, w.showDetail, w.showConsole, h.a.w, h.a.bodyH())
	gx := sbW + 1 + 2
	hy := 1 + 1 + t0.barLines() // top bar, frame edge, filter bar
	col1 := t0.grid.gutterW() + 1 + t0.grid.Cols[0].Width + 1 + 2
	h.a.Update(tea.MouseClickMsg{X: gx + col1, Y: hy, Button: tea.MouseLeft})
	if len(t0.sort) != 1 || t0.sort[0].Column != t0.grid.Cols[1].Name {
		t.Errorf("header click sort: %+v", t0.sort)
	}
	// drag the right edge of column 0
	edge := gx + t0.grid.gutterW() + 1 + t0.grid.Cols[0].Width
	before := t0.grid.Cols[0].Width
	h.a.Update(tea.MouseClickMsg{X: edge, Y: hy, Button: tea.MouseLeft})
	h.a.Update(tea.MouseMotionMsg{X: edge + 3, Y: hy, Button: tea.MouseLeft})
	h.a.Update(tea.MouseReleaseMsg{X: edge + 3, Y: hy, Button: tea.MouseLeft})
	if got := t0.grid.Cols[0].Width; got != before+3 {
		t.Errorf("drag: width %d -> %d", before, got)
	}
	if got := h.a.d.State.Table(t0.stateKey()).ColWidths[t0.grid.Cols[0].Name]; got != before+3 {
		t.Errorf("width not saved: %d", got)
	}
}

func TestM5MouseClickSelects(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	x, y := h.at("Cy")
	_, cmd := h.a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	h.pump(cmd)
	g := h.a.ws().tab().(*tableTab).grid
	if g.row != 2 || g.col != 1 {
		t.Fatalf("cursor at row %d col %d, want 2/1", g.row, g.col)
	}
	if h.a.ws().focusSB {
		t.Fatal("a click in the grid moves the focus there")
	}
	x, y = h.at("orders")
	_, cmd = h.a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	h.pump(cmd)
	w := h.a.ws()
	if r, ok := w.sb.current(); !w.focusSB || !ok || r.obj.Name != "orders" {
		t.Fatalf("sidebar selection %+v focus %v", r, w.focusSB)
	}
}
