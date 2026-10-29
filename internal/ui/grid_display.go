// grid_display.go: How the grid shows a cell (special values, WKT, header colours), sizing, hit testing and local sort.

package ui

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"webtuidb/internal/db"
)

var clusterColors = []string{"39", "208", "41", "170", "220", "203"}

var headerRe = regexp.MustCompile(`\| [^|]+ +\|`)

// colorHeaders tints each table's header row (the line after a box's top rule) by its FK cluster, or by the
// colour override (ANSI code per table name) when there is one.
func colorHeaders(text string, byName map[string]int, override ...map[string]string) []string {
	var ov map[string]string
	if len(override) > 0 {
		ov = override[0]
	}
	lines := strings.Split(text, "\n")
	start, hdr := true, -1
	for i, l := range lines {
		if l == "" {
			start = true
			continue
		}
		if start {
			start, hdr = false, i+1
		}
		if i != hdr {
			continue
		}
		lines[i] = headerRe.ReplaceAllStringFunc(l, func(m string) string {
			name := strings.TrimSpace(m[1 : len(m)-1])
			col := ""
			if c, ok := byName[name]; ok {
				col = clusterColors[c%len(clusterColors)]
			}
			if c := ov[name]; c != "" {
				col = c
			}
			if col == "" {
				return m
			}
			return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("16")).Background(lipgloss.Color(col)).Render(m)
		})
	}
	return lines
}

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
