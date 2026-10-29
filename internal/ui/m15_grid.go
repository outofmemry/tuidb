package ui

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
	"webtuidb/internal/menus"
)

// ---- cell display: NULL, empty, DEFAULT, spatial -------------------------------------------------------

// cellDisplay is the text a grid cell shows: NULL, two quotes for the empty string, DEFAULT for a pending
// default, otherwise the usual one-line text. The Null style (dim italic) colours NULL, DEFAULT and
// expressions through isSpecial, and the empty string through specialCell. Geometry is Grid.cellString's job.
func cellDisplay(v any, w int) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case string:
		if x == "" {
			return "''"
		}
	default:
		if v == db.Empty {
			return "''"
		}
	}
	return gridCellText(v, w)
}

// specialCell says whether a cell is drawn in the Null style (dim italic): NULL, the empty string, DEFAULT, expressions.
func specialCell(v any) bool {
	if s, ok := v.(string); ok && s == "" {
		return true
	}
	return isSpecial(v)
}

type wktEntry struct {
	src []byte
	txt string
	ok  bool
}

// wktCell decodes geometry bytes once per cell: the entry is reused while the cell still holds the same slice.
func (g *Grid) wktCell(r *gridRow, c int, b []byte) (string, bool) {
	if len(b) < 5 {
		return "", false
	}
	k := wktKey{r, c}
	if e, ok := g.wktCache[k]; ok && len(e.src) == len(b) && &e.src[0] == &b[0] {
		return e.txt, e.ok
	}
	if g.wktCache == nil {
		g.wktCache = map[wktKey]wktEntry{}
	}
	if len(g.wktCache) > 5000 {
		g.wktCache = map[wktKey]wktEntry{}
	}
	txt, ok := db.WKT(b)
	g.wktCache[k] = wktEntry{b, txt, ok}
	return txt, ok
}

type wktKey struct {
	r *gridRow
	c int
}

// cellString is the grid's text for the cell (r, c).
func (g *Grid) cellString(r *gridRow, c int, w int) string {
	v := r.Cur[c]
	if b, ok := v.([]byte); ok && g.DecodeSpatial {
		if s, ok := g.wktCell(r, c, b); ok {
			return s
		}
	}
	return cellDisplay(v, w)
}

// ---- hit tests for the mouse (TUI-C routes the clicks) ------------------------------------------------

// HeaderAt maps a screen column x (0 = the grid's left edge) to the column header under it. edge is true
// when x sits on the separator that closes the column, the place to grab for a resize drag.
func (g *Grid) HeaderAt(x int) (col int, edge bool) {
	pos := g.gutterW() // the separator before the first visible column
	for i := g.left; i < len(g.Cols) && pos < g.W; i++ {
		if x == pos && i > g.left {
			return i - 1, true
		}
		end := pos + g.Cols[i].Width
		if x > pos && x <= end {
			return i, false
		}
		pos = end + 1
	}
	if x == pos && pos > g.gutterW() && g.left < len(g.Cols) {
		return min(len(g.Cols)-1, g.lastVisible()), true
	}
	return -1, false
}

func (g *Grid) lastVisible() int {
	pos, last := g.gutterW(), g.left
	for i := g.left; i < len(g.Cols) && pos < g.W; i++ {
		last = i
		pos += g.Cols[i].Width + 1
	}
	return last
}

// Resize widens (or narrows) a column by dx cells.
func (g *Grid) Resize(col, dx int) {
	if col >= 0 && col < len(g.Cols) {
		c := &g.Cols[col]
		c.Width = clamp(c.Width+dx, 4, 120)
	}
}

// CellAt maps a screen cell (x, y relative to the grid's top-left, y = 0 the header) to a row and column.
func (g *Grid) CellAt(x, y int) (row, col int, ok bool) {
	if y < 1 {
		return 0, 0, false
	}
	row = g.top + y - 1
	if row >= len(g.Rows) {
		return 0, 0, false
	}
	col, edge := g.HeaderAt(x)
	if col < 0 || edge {
		// a click on a separator belongs to the cell on its right
		c, e := g.HeaderAt(x + 1)
		if c < 0 || e {
			return 0, 0, false
		}
		col = c
	}
	return row, col, true
}

// ---- local sort (query results sort in place; the data tab sorts on the server) ------------------------

// SortLocal orders the loaded rows by a column: dir 1 ascending, -1 descending, 0 back to the loaded order.
func (g *Grid) SortLocal(name string, dir int) {
	ci := -1
	for i, c := range g.Cols {
		if c.Name == name {
			ci = i
		}
	}
	if ci < 0 {
		return
	}
	if dir == 0 {
		if g.unsorted != nil {
			have := map[*gridRow]bool{}
			for _, r := range g.Rows {
				have[r] = true
			}
			var out []*gridRow
			for _, r := range g.unsorted {
				if have[r] {
					out = append(out, r)
					delete(have, r)
				}
			}
			for _, r := range g.Rows { // rows added since the sort go last
				if have[r] {
					out = append(out, r)
				}
			}
			g.Rows = out
		}
		g.SortCol, g.SortDesc, g.unsorted, g.Sorts = "", false, nil, nil
		return
	}
	if g.unsorted == nil {
		g.unsorted = append([]*gridRow(nil), g.Rows...)
	}
	cur := g.cur()
	sort.SliceStable(g.Rows, func(i, j int) bool {
		c := compareCells(g.Rows[i].Cur[ci], g.Rows[j].Cur[ci])
		if dir < 0 {
			return c > 0
		}
		return c < 0
	})
	g.SortCol, g.SortDesc, g.Sorts = name, dir < 0, nil
	for i, r := range g.Rows { // the cursor stays on the same row
		if r == cur {
			g.row = i
		}
	}
	g.scroll()
}

// compareCells orders NULLs first, numbers numerically, everything else as text.
func compareCells(a, b any) int {
	an, bn := a == nil || a == db.Null, b == nil || b == db.Null
	switch {
	case an && bn:
		return 0
	case an:
		return -1
	case bn:
		return 1
	}
	sa, sb := plain(a), plain(b)
	if fa, err := strconv.ParseFloat(sa, 64); err == nil {
		if fb, err := strconv.ParseFloat(sb, 64); err == nil {
			switch {
			case fa < fb:
				return -1
			case fa > fb:
				return 1
			}
			return 0
		}
	}
	return strings.Compare(strings.ToLower(sa), strings.ToLower(sb))
}

// ---- copy as ------------------------------------------------------------------------------------------

// pushCopyAs asks for one of the formats, then runs do with it.
func (a *App) pushCopyAs(title string, formats []CopyFormat, do func(CopyFormat) tea.Cmd) {
	var items []pickItem
	for _, f := range formats {
		items = append(items, pickItem{Label: string(f), Value: f})
	}
	a.push(&pickerDlg{title: title, items: items, onPick: func(it pickItem, _ bool) tea.Cmd { return do(it.Value.(CopyFormat)) }})
}

// structFormats are the copy-as formats that make sense for column / index definitions.
var structFormats = []CopyFormat{FmtCSVHeader, FmtJSON, FmtMarkdown, FmtPlain, FmtHTML}

// ---- the column menu (menus.Header) ---------------------------------------------------------------------

// colMenu holds what the column menu needs from its owner; a nil hook answers with a note.
type colMenu struct {
	a       *App
	g       *Grid
	sort    func(name string, dir int) tea.Cmd
	several func() tea.Cmd
	filter  func(name string) tea.Cmd
	hide    func() tea.Cmd
	showAll func() tea.Cmd
	def     func(name string) string // the column definition; "" = name and type from the grid
	copy    func(text, what string) tea.Cmd
}

func (m colMenu) open() tea.Cmd {
	if m.g.col >= len(m.g.Cols) {
		return m.a.say("no column here")
	}
	name := m.g.Cols[m.g.col].Name
	m.a.pushMenu("Column "+name, menus.Header, menus.Env{Caps: capsMap(db.Capabilities{})}, func(it menus.Item) tea.Cmd { return m.run(it.Action, name) })
	return nil
}

func (m colMenu) run(act, name string) tea.Cmd {
	note := func(what string) tea.Cmd { return m.a.say("%s: %s", what, "not available here") }
	switch act {
	case "sort_asc", "sort_desc", "sort_none":
		if m.sort == nil {
			return note("sort")
		}
		return m.sort(name, map[string]int{"sort_asc": 1, "sort_desc": -1, "sort_none": 0}[act])
	case "sort_columns":
		if m.several == nil {
			return m.a.say("sorting by several columns: use the table's data tab (results sort by one column)")
		}
		return m.several()
	case "filter_column":
		if m.filter == nil {
			return note("filter")
		}
		return m.filter(name)
	case "hide_column":
		if m.hide == nil {
			return m.a.say("hiding columns: use the table's data tab")
		}
		return m.hide()
	case "show_all_columns":
		if m.showAll == nil {
			return m.a.say("showing columns: use the table's data tab")
		}
		return m.showAll()
	case "copy_name":
		return m.copy(name, "column name")
	case "copy_column":
		return m.copy(m.g.CopyColumn(), "column values")
	case "copy_definition":
		def := ""
		if m.def != nil {
			def = m.def(name)
		}
		if def == "" {
			def = name + " " + m.g.Cols[m.g.col].Type
		}
		return m.copy(strings.TrimSpace(def), "column definition")
	}
	return nil
}

// columnDefinition is "name type NOT NULL DEFAULT x PRIMARY KEY" for a loaded column.
func columnDefinition(c db.Column) string {
	parts := []string{c.Name, c.Type}
	if !c.Nullable {
		parts = append(parts, "NOT NULL")
	}
	if c.HasDef || c.Default != "" {
		parts = append(parts, "DEFAULT "+c.Default)
	}
	if c.PK {
		parts = append(parts, "PRIMARY KEY")
	}
	if c.Extra != "" {
		parts = append(parts, c.Extra)
	}
	return strings.Join(parts, " ")
}

// columnInfo is the "type · nullable · default · key" line the row detail shows for a column (M15.6).
func columnInfo(c db.Column) string {
	parts := []string{c.Type, map[bool]string{true: "nullable", false: "not null"}[c.Nullable]}
	if c.HasDef || c.Default != "" {
		parts = append(parts, "default "+c.Default)
	} else {
		parts = append(parts, "no default")
	}
	switch {
	case c.PK:
		parts = append(parts, "primary key")
	case c.FK != nil:
		parts = append(parts, "foreign key → "+c.FK.RefTable)
	default:
		parts = append(parts, "no key")
	}
	return strings.Join(parts, " · ")
}

// tableColumnMenu is column_menu on the data tab: sort / filter / hide act through the tab's own state.
func (t *tableTab) columnMenu() tea.Cmd {
	g := t.grid
	m := colMenu{a: t.ws.a, g: g, copy: t.copy,
		sort: func(name string, dir int) tea.Cmd {
			return t.guard(func() tea.Cmd {
				t.sort = nil
				if dir != 0 {
					t.sort = []db.Sort{{Column: name, Desc: dir < 0}}
				}
				t.offset, t.toTop = 0, true
				t.saveState()
				return t.load()
			})
		},
		several: t.sortColumns,
		filter:  t.filterWithColumn,
		hide:    t.hideColumn,
		showAll: t.showAllColumns,
		def: func(name string) string {
			for _, c := range t.cols {
				if c.Name == name {
					return columnDefinition(c)
				}
			}
			return ""
		},
	}
	return m.open()
}

// filterWithColumn opens the filter bar on a new filter for the column (reusing an empty first one).
func (t *tableTab) filterWithColumn(name string) tea.Cmd {
	t.openFilters()
	fs := &t.filters.Filters
	i := len(*fs) - 1
	if f := (*fs)[i]; f.Value != "" || len(*fs) > 1 && f.Column != name {
		*fs = append(*fs, t.defaultFilter())
		i++
	}
	(*fs)[i].Column = name
	t.fb.sel, t.fb.field = i, ffValue
	t.syncEdit()
	return nil
}

// resultColumnMenu is column_menu on a query result: sorting is local, "filter" opens Find on that column.
func (t *queryTab) resultColumnMenu(rv *resultView) tea.Cmd {
	g := rv.grid
	if g == nil {
		return t.ws.a.say("nothing here: this result has no rows")
	}
	m := colMenu{a: t.ws.a, g: g, copy: t.copyOut,
		sort:   func(name string, dir int) tea.Cmd { g.SortLocal(name, dir); return nil },
		filter: func(name string) tea.Cmd { rv.find.Col = name; return t.findPrompt(rv) },
	}
	return m.open()
}

// ---- structure grid: copy as and the row menu -------------------------------------------------------------

// structCopyAs copies the column (or index) definitions as CSV / JSON / Markdown...; selected rows, else all.
func (t *tableTab) structCopyAs() tea.Cmd {
	g := t.editGrid()
	if g == nil || len(g.Rows) == 0 {
		return t.ws.a.say("the structure is still loading")
	}
	what := map[bool]string{true: "index definitions", false: "column definitions"}[t.view == viewIndex]
	t.ws.a.pushCopyAs("Copy "+what+" as", structFormats, func(f CopyFormat) tea.Cmd {
		r0, r1 := 0, len(g.Rows)-1
		if g.selecting {
			r0, _, r1, _ = g.selection()
		}
		return t.copy(g.FormatRect(r0, 0, r1, len(g.Cols)-1, f, t.ref.Name), what)
	})
	return nil
}

// structMenu is the structure editor's row menu (menus.Structure).
func (t *tableTab) structMenu() tea.Cmd {
	g := t.editGrid()
	if g == nil {
		return t.ws.a.say("the structure is still loading")
	}
	t.ws.a.pushMenu("Structure", menus.Structure, menus.Env{Caps: capsMap(t.ws.caps), Editable: g.Editable}, func(it menus.Item) tea.Cmd {
		switch it.Action {
		case "add_row":
			return t.structAdd(g)
		case "delete_rows":
			g.DeleteRows()
		case "set_value":
			return t.structSetValue()
		case "move_up":
			return t.moveColumn(g, -1)
		case "move_down":
			return t.moveColumn(g, 1)
		case "copy_rows_as":
			return t.structCopyAs()
		}
		return nil
	})
	return nil
}

// ---- binary quick look: image and PDF headers ---------------------------------------------------------

// binaryInfo is the one-line header above a binary value: size, and what the bytes are when they are an image or a PDF.
func binaryInfo(b []byte, sniff bool) string {
	s := fmt.Sprintf("%d bytes", len(b))
	if !sniff {
		return s
	}
	if mime, w, h, ok := db.SniffImage(b); ok {
		return fmt.Sprintf("%s %d×%d, %s", mime, w, h, s)
	}
	if db.IsPDF(b) {
		return "PDF, " + s
	}
	return s
}

// base64Lines is raw as base64 wrapped at 76 columns.
func base64Lines(raw []byte) string {
	s := base64.StdEncoding.EncodeToString(raw)
	var out []string
	for len(s) > 76 {
		out, s = append(out, s[:76]), s[76:]
	}
	return strings.Join(append(out, s), "\n")
}
