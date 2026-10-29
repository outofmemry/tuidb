// mouse.go: Mouse handling: hit testing, clicks, right clicks, dragging column widths.

package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// click handles a left click at screen cell (x, y): row 0 is the top bar, the body follows.
// shortcut: only the sidebar list and the table data grid react; tabs, dialogs, query results and split panes ignore clicks.
func (w *Workspace) click(x, y int) tea.Cmd {
	sbW, _, _, mainW, mainH := layoutOf(w.showSB, w.showDetail, w.showConsole, w.W, w.H)
	y-- // the top bar
	if y < 0 || y >= mainH || w.panes.split {
		return nil
	}
	switch {
	case x < sbW:
		w.focusSB = true
		rows := w.sb.rows()
		i := w.sb.top + (y - 1 - 2) // frame border, then the two header lines of the sidebar
		if i < 0 || i >= len(rows) {
			return nil
		}
		if i == w.sb.sel { // a second click on the selected item opens it
			return w.a.key(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
		w.sb.sel = i
	case sbW > 0 && x == sbW, x >= sbW+boolInt(sbW > 0)+mainW:
	default:
		t, ok := w.tab().(*tableTab)
		if !ok {
			return nil
		}
		mainX := sbW + boolInt(sbW > 0)
		if t.Click(x-(mainX+2), y-1) { // frame border and padding (x only)
			w.focusSB, w.focusDetail = false, false
		}
	}
	return nil
}

// Click moves the grid cursor to the cell under (x, y), relative to the tab's view; the filter bar sits above the grid.
func (t *tableTab) Click(x, y int) bool {
	if t.view != viewData {
		return false
	}
	bar := len(t.filterView(t.ws.a.th, t.grid.W))
	if !t.showFilters && len(t.filters.Active()) > 0 {
		bar = 1
	}
	return t.grid.Click(x, y-bar)
}

// Click puts the cursor on the cell at (x, y) of the grid view (y 0 is the header) and drops the selection.
func (g *Grid) Click(x, y int) bool {
	ri := g.top + y - 1
	if y < 1 || ri >= len(g.Rows) || x < 0 {
		return false
	}
	used := g.gutterW()
	for ci := g.left; ci < len(g.Cols); ci++ {
		used += g.Cols[ci].Width + 1
		if x < used {
			if x < used-g.Cols[ci].Width-1 { // the gutter
				return false
			}
			g.row, g.col, g.selecting = ri, ci, false
			g.scroll()
			return true
		}
	}
	return false
}

type mouseState struct {
	lastAt       time.Time
	lastX, lastY int
	drag         *colDrag
}

type colDrag struct {
	tab *tableTab
	col int
	x   int
}

const doubleClick = 450 * time.Millisecond

func (a *App) mouse(m tea.MouseMsg) tea.Cmd {
	w, mm := a.ws(), m.Mouse()
	if w == nil || !a.inWS || len(a.overlays) > 0 {
		a.term.mouse.drag = nil
		return nil
	}
	ms := &a.term.mouse
	switch m.(type) {
	case tea.MouseClickMsg:
		switch mm.Button {
		case tea.MouseLeft:
			double := time.Since(ms.lastAt) < doubleClick && ms.lastX == mm.X && ms.lastY == mm.Y
			ms.lastAt, ms.lastX, ms.lastY = time.Now(), mm.X, mm.Y
			return w.mouseClick(mm.X, mm.Y, double)
		case tea.MouseRight:
			return w.mouseRight(mm.X, mm.Y)
		}
	case tea.MouseMotionMsg:
		if d := ms.drag; d != nil && mm.Button == tea.MouseLeft {
			if dx := mm.X - d.x; dx != 0 {
				d.tab.grid.Resize(d.col, dx)
				d.x = mm.X
			}
		}
	case tea.MouseReleaseMsg:
		if d := ms.drag; d != nil {
			ms.drag = nil
			d.tab.saveWidths()
		}
	}
	return nil
}

// hit says which pane a screen cell is in and where inside its frame the cell is.
type hit struct {
	pane   string // top | sidebar | main | detail | console | none
	x, y   int    // relative to the pane's frame (0,0 is its top-left corner)
	sx     int    // the screen column
	mainW  int
	mainH  int
	conTop int
}

func (w *Workspace) hitTest(x, y int) hit {
	W, H := w.a.w, w.a.bodyH()
	sbW, detW, conH, mainW, mainH := layoutOf(w.showSB, w.showDetail, w.showConsole, W, H)
	y-- // the top bar
	h := hit{pane: "none", sx: x, mainW: mainW, mainH: mainH, conTop: mainH}
	switch {
	case y < 0:
		h.pane = "top"
	case conH > 0 && y >= mainH:
		h.pane, h.x, h.y = "console", x, y-mainH
	case y >= mainH:
	case x < sbW:
		h.pane, h.x, h.y = "sidebar", x, y
	case x == sbW && sbW > 0:
	case x < sbW+boolInt(sbW > 0)+mainW:
		h.pane, h.x, h.y = "main", x-(sbW+boolInt(sbW > 0)), y
	case detW > 0 && x > sbW+boolInt(sbW > 0)+mainW:
		h.pane, h.x, h.y = "detail", x-(sbW+boolInt(sbW > 0)+mainW+1), y
	}
	return h
}

func (w *Workspace) mouseClick(x, y int, double bool) tea.Cmd {
	h := w.hitTest(x, y)
	switch h.pane {
	case "console":
		w.focusConsole, w.focusSB, w.focusDetail = true, false, false
		ls := w.consoleLines()
		rows := w.consoleRows()
		top := max(0, len(ls)-rows)
		if w.focusConsole {
			top = clamp(w.con.top, 0, max(0, len(ls)-rows))
		}
		if i := top + h.y - 1; h.y >= 1 && i < len(ls) {
			w.con.sel = i
		}
	case "sidebar":
		w.focusSB, w.focusConsole, w.focusDetail = true, false, false
		rows := w.sb.rows()
		i := w.sb.top + (h.y - 1 - 2) // frame border, then the two header lines of the sidebar
		if i < 0 || i >= len(rows) {
			return nil
		}
		if i == w.sb.sel { // a second click on the selected item opens it
			return w.a.key(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
		w.sb.sel = i
	case "detail":
		if _, ok := w.tab().(detailer); ok {
			w.focusDetail, w.focusSB, w.focusConsole = true, false, false
		}
	case "main":
		if w.panes.split {
			return nil
		}
		w.focusConsole = false
		if h.y == 0 { // the tab bar sits on the frame's top edge
			if i := w.tabAt(h.x); i >= 0 {
				w.selectTab(i)
			}
			return nil
		}
		return w.clickTab(h, double)
	}
	return nil
}

func (w *Workspace) mouseRight(x, y int) tea.Cmd {
	h := w.hitTest(x, y)
	switch h.pane {
	case "sidebar":
		w.focusSB = true
		if i := w.sb.top + (h.y - 1 - 2); i >= 0 && i < len(w.sb.rows()) {
			w.sb.sel = i
			return w.sb.menu()
		}
	case "console":
		return w.consoleMenu()
	case "main":
		if w.panes.split {
			return nil
		}
		if h.y == 0 {
			if i := w.tabAt(h.x); i >= 0 {
				return w.tabMenu(i)
			}
			return nil
		}
		if t, ok := w.tab().(*tableTab); ok && t.view == viewData {
			gy := h.y - 1 - t.barLines()
			switch {
			case gy == 0:
				if col, edge := t.grid.HeaderAt(h.x - 2); col >= 0 && !edge {
					t.grid.col = col
					w.focusSB = false
					return w.a.run("column_menu")
				}
			case gy < 0 && t.showFilters: // the filter bar
				return t.filterBarMenu()
			case gy > 0:
				if row, col, ok := t.grid.CellAt(h.x-2, gy); ok {
					t.grid.row, t.grid.col = row, col
					t.grid.scroll()
					return t.gridMenu()
				}
			}
		}
		if t, ok := w.tab().(*queryTab); ok {
			if edH, resH := queryLayout(h.mainH - 2); resH > 0 && h.y-1 > edH {
				return t.resultMenu()
			}
		}
	}
	return nil
}

// consoleRows is the number of statement lines the console frame shows.
func (w *Workspace) consoleRows() int {
	_, _, conH, _, _ := layoutOf(w.showSB, w.showDetail, w.showConsole, w.a.w, w.a.bodyH())
	return max(1, conH-2-boolInt(w.con.searching))
}

// tabAt maps x (relative to the main frame's left edge) on the tab bar to an index into w.tabs, -1 for none.
func (w *Workspace) tabAt(x int) int {
	pos := 3 // "╭─" and the space before the title
	for _, l := range w.tabLabels(0) {
		if x >= pos && x < pos+width(l.text) {
			return l.idx
		}
		pos += width(l.text) + 1
	}
	return -1
}

// clickTab handles a click inside the active tab's body (h.x, h.y are relative to the main frame).
func (w *Workspace) clickTab(h hit, double bool) tea.Cmd {
	bx, by := h.x-2, h.y-1 // frame border and padding
	switch t := w.tab().(type) {
	case *tableTab:
		if t.view != viewData {
			w.focusSB = false
			return nil
		}
		gy := by - t.barLines()
		if gy < 0 || bx < 0 {
			return nil
		}
		w.focusSB, w.focusDetail = false, false
		g := t.grid
		if gy == 0 { // the header: grab a column's edge to resize it, click a name to sort by it
			col, edge := g.HeaderAt(bx)
			switch {
			case col < 0:
			case edge:
				w.a.term.mouse.drag = &colDrag{tab: t, col: col, x: h.sx}
			default:
				g.col = col
				name := g.Cols[col].Name
				return t.guard(func() tea.Cmd { return t.sortBy(name) })
			}
			return nil
		}
		row, col, ok := g.CellAt(bx, gy)
		if !ok {
			return nil
		}
		g.row, g.col, g.selecting = row, col, false
		g.scroll()
		if double {
			return w.a.key(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
	case *queryTab:
		edH, resH := queryLayout(h.mainH - 2)
		switch {
		case by < edH:
			w.focusSB, w.focusDetail = false, false
			t.clickEditor(bx, by, edH)
		case resH > 0 && by > edH:
			w.focusSB, w.focusDetail = false, false
			t.focusResults = len(t.res) > 0
		}
	}
	return nil
}

// queryLayout mirrors queryTab.View: the editor and results heights of a tab body H lines tall.
func queryLayout(H int) (edH, resH int) {
	resH = max(4, (H-1)*45/100)
	edH = H - 1 - resH
	if edH < 3 {
		edH, resH = max(1, H-1), 0
	}
	return
}

// clickEditor puts the cursor of the editor under (x, y); with a split editor the click also picks the pane.
func (t *queryTab) clickEditor(x, y, edH int) {
	t.focusResults = false
	if t.other == nil || edH < 5 {
		t.ed.ClickAt(x, y)
		return
	}
	hTop := (edH - 1) / 2
	if y == hTop { // the rule between the panes
		return
	}
	lower := y > hTop
	if lower != t.focusBottom {
		t.swapPane()
	}
	if lower {
		y -= hTop + 1
	}
	t.ed.ClickAt(x, y)
}

// barLines is the height of the filter bar (or pills) above the grid.
func (t *tableTab) barLines() int {
	n := len(t.filterView(t.ws.a.th, t.grid.W))
	if !t.showFilters && len(t.filters.Active()) > 0 {
		n = 1
	}
	return n
}

// saveWidths remembers the column widths of this table (State.TableState.ColWidths).
func (t *tableTab) saveWidths() {
	ts := t.ws.a.d.State.Table(t.stateKey())
	ts.ColWidths = map[string]int{}
	for _, c := range t.grid.Cols {
		ts.ColWidths[c.Name] = c.Width
	}
	t.ws.a.d.State.SetTable(t.stateKey(), ts)
}

// restoreWidths applies the saved widths to the columns that just arrived.
func (t *tableTab) restoreWidths() {
	for i := range t.grid.Cols {
		if w := t.ws.a.d.State.Table(t.stateKey()).ColWidths[t.grid.Cols[i].Name]; w > 0 {
			t.grid.Cols[i].Width = clamp(w, 4, 120)
		}
	}
}
