package ui

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"tuidb/internal/db"
)

type rowState int

const (
	rowClean rowState = iota
	rowNew
	rowDeleted
)

type gridCol struct {
	Name  string
	Type  string
	PK    bool
	Width int
}

type gridRow struct {
	Orig  []any // values as loaded; nil for new rows
	Cur   []any // values with pending edits applied
	Dirty map[int]bool
	State rowState
	Meta  any // owner-specific data that travels with the row (structure grid keeps the original column name)
}

type gridSnapshot struct {
	rows     []*gridRow
	row, col int
}

// Grid is the data grid: browse, edit with pending changes, undo/redo, selection, copy.
type Grid struct {
	Cols     []gridCol
	Rows     []*gridRow
	Editable bool
	NewRow   func() []any // template for AddRow; nil = every cell DEFAULT
	W, H     int

	row, col             int
	anchorRow, anchorCol int
	selecting            bool
	top, left            int
	edit                 *lineEdit
	editStart            string // text the cell editor opened with
	undo, redo           []gridSnapshot
	SortCol              string
	SortDesc             bool
	RowOffset            int // first row number shown in the gutter (paging)
}

// SetData replaces the contents with a fresh result (drops pending edits and undo history).
func (g *Grid) SetData(cols []gridCol, rows [][]any) {
	old := map[string]int{}
	for _, c := range g.Cols {
		old[c.Name] = c.Width
	}
	g.Cols = cols
	g.Rows = make([]*gridRow, len(rows))
	for i, r := range rows {
		g.Rows[i] = &gridRow{Orig: r, Cur: append([]any(nil), r...), Dirty: map[int]bool{}}
	}
	for i := range g.Cols {
		if w, ok := old[g.Cols[i].Name]; ok && w > 0 {
			g.Cols[i].Width = w
		} else {
			g.Cols[i].Width = autoWidth(g.Cols[i], rows, i)
		}
	}
	// Keep the cursor when the same columns come back (reload after commit/sort); otherwise start over.
	if !sameNames(old, g.Cols) {
		g.col, g.left = 0, 0
	}
	g.col = clamp(g.col, 0, len(g.Cols)-1)
	g.row = clamp(g.row, 0, len(g.Rows)-1)
	g.top = clamp(g.top, 0, g.row)
	g.edit, g.selecting = nil, false
	g.undo, g.redo = nil, nil
}

func sameNames(old map[string]int, cols []gridCol) bool {
	if len(old) != len(cols) {
		return false
	}
	for _, c := range cols {
		if _, ok := old[c.Name]; !ok {
			return false
		}
	}
	return true
}

// ToTop moves the cursor and scroll to the first row (a new query, not a reload).
func (g *Grid) ToTop() { g.row, g.top, g.selecting = 0, 0, false }

func autoWidth(c gridCol, rows [][]any, i int) int {
	w := min(runeLen(c.Name)+2, 40)
	for r := 0; r < len(rows) && r < 100; r++ {
		if i < len(rows[r]) {
			w = max(w, min(width(cellText(rows[r][i])), 40))
		}
	}
	return max(w, 4)
}

func (g *Grid) cur() *gridRow {
	if g.row < 0 || g.row >= len(g.Rows) {
		return nil
	}
	return g.Rows[g.row]
}

func (g *Grid) Editing() bool { return g.edit != nil }

// ---- navigation -----------------------------------------------------------------------

func (g *Grid) Move(dr, dc int, extend bool) {
	g.setSelecting(extend)
	g.row = clamp(g.row+dr, 0, len(g.Rows)-1)
	g.col = clamp(g.col+dc, 0, len(g.Cols)-1)
	g.scroll()
}

func (g *Grid) GoRow(r int, extend bool) {
	g.setSelecting(extend)
	g.row = clamp(r, 0, len(g.Rows)-1)
	g.scroll()
}

func (g *Grid) GoCol(c int, extend bool) {
	g.setSelecting(extend)
	g.col = clamp(c, 0, len(g.Cols)-1)
	g.scroll()
}

func (g *Grid) Page(dir int, extend bool) { g.Move(dir*max(1, g.bodyH()-1), 0, extend) }

func (g *Grid) setSelecting(extend bool) {
	if extend && !g.selecting {
		g.anchorRow, g.anchorCol, g.selecting = g.row, g.col, true
	}
	if !extend {
		g.selecting = false
	}
}

func (g *Grid) ClearSelection() { g.selecting = false }

func (g *Grid) bodyH() int { return max(1, g.H-1) } // minus header

func (g *Grid) gutterW() int { return max(3, len(fmt.Sprint(g.RowOffset+len(g.Rows)))) + 1 }

func (g *Grid) scroll() {
	b := g.bodyH()
	if g.row < g.top {
		g.top = g.row
	}
	if g.row >= g.top+b {
		g.top = g.row - b + 1
	}
	if g.col < g.left {
		g.left = g.col
	}
	for g.left < g.col && !g.colFits(g.col) {
		g.left++
	}
}

// colFits reports whether column c is fully visible when scrolled to g.left.
func (g *Grid) colFits(c int) bool {
	used := g.gutterW()
	for i := g.left; i < len(g.Cols); i++ {
		used += g.Cols[i].Width + 1
		if i == c {
			return used <= g.W
		}
	}
	return false
}

// selection returns the inclusive rectangle (cursor alone when not selecting).
func (g *Grid) selection() (r0, c0, r1, c1 int) {
	if !g.selecting {
		return g.row, g.col, g.row, g.col
	}
	return min(g.anchorRow, g.row), min(g.anchorCol, g.col), max(g.anchorRow, g.row), max(g.anchorCol, g.col)
}

func (g *Grid) inSelection(r, c int) bool {
	if !g.selecting {
		return false
	}
	r0, c0, r1, c1 := g.selection()
	return r >= r0 && r <= r1 && c >= c0 && c <= c1
}

// ---- editing --------------------------------------------------------------------------

func (g *Grid) snapshot() {
	s := gridSnapshot{row: g.row, col: g.col, rows: make([]*gridRow, len(g.Rows))}
	for i, r := range g.Rows {
		c := *r
		c.Cur = append([]any(nil), r.Cur...)
		c.Dirty = map[int]bool{}
		for k, v := range r.Dirty {
			c.Dirty[k] = v
		}
		s.rows[i] = &c
	}
	g.undo = append(g.undo, s)
	if len(g.undo) > 50 {
		g.undo = g.undo[1:]
	}
	g.redo = nil
}

func (g *Grid) restore(s gridSnapshot) {
	g.Rows, g.row, g.col = s.rows, s.row, s.col
	g.row = clamp(g.row, 0, len(g.Rows)-1)
	g.selecting, g.edit = false, nil // the old anchor may point past the restored rows
	g.scroll()
}

func (g *Grid) Undo() bool {
	if len(g.undo) == 0 {
		return false
	}
	cur := g.undo[len(g.undo)-1]
	g.undo = g.undo[:len(g.undo)-1]
	g.snapshotTo(&g.redo)
	g.restore(cur)
	return true
}

func (g *Grid) Redo() bool {
	if len(g.redo) == 0 {
		return false
	}
	nxt := g.redo[len(g.redo)-1]
	g.redo = g.redo[:len(g.redo)-1]
	g.snapshotTo(&g.undo)
	g.restore(nxt)
	return true
}

func (g *Grid) snapshotTo(dst *[]gridSnapshot) {
	keepUndo, keepRedo := g.undo, g.redo
	g.snapshot()
	s := g.undo[len(g.undo)-1]
	g.undo, g.redo = keepUndo, keepRedo
	*dst = append(*dst, s)
}

// sameValue compares for dirty tracking: NULL-ness and text.
func sameValue(a, b any) bool {
	if isSpecial(a) || isSpecial(b) {
		return fmt.Sprintf("%T:%v", a, a) == fmt.Sprintf("%T:%v", b, b) || (db.IsNull(a) && db.IsNull(b))
	}
	return db.FormatValue(a) == db.FormatValue(b)
}

// SetCell writes v into the cursor cell (pending until commit). Returns false when not editable.
func (g *Grid) SetCell(v any) bool {
	r := g.cur()
	if r == nil || !g.Editable || r.State == rowDeleted || g.col >= len(g.Cols) {
		return false
	}
	g.snapshot()
	r.Cur[g.col] = v
	if r.State == rowNew || !sameValue(v, r.Orig[g.col]) {
		r.Dirty[g.col] = true
	} else {
		delete(r.Dirty, g.col)
	}
	return true
}

func (g *Grid) StartEdit() bool {
	r := g.cur()
	if r == nil || !g.Editable || r.State == rowDeleted {
		return false
	}
	text := ""
	if v := r.Cur[g.col]; !isSpecial(v) {
		text = db.FormatValue(v)
	}
	e := newLine(text)
	g.edit, g.editStart = &e, text
	return true
}

func (g *Grid) FinishEdit(save bool) {
	if g.edit == nil {
		return
	}
	text := g.edit.Value()
	g.edit = nil
	r := g.cur()
	// Unchanged text is not an edit: opening and closing the editor on a NULL or DEFAULT cell
	// must not turn it into an empty string (use Set Value > EMPTY for that).
	if !save || r == nil || g.col >= len(r.Cur) || text == g.editStart {
		return
	}
	g.SetCell(text)
}

func (g *Grid) AddRow() bool {
	if !g.Editable {
		return false
	}
	g.snapshot()
	cur := make([]any, len(g.Cols))
	dirty := map[int]bool{}
	if g.NewRow != nil {
		cur = g.NewRow()
		for i := range cur {
			dirty[i] = true
		}
	} else {
		for i := range cur {
			cur[i] = db.Default
		}
	}
	at := min(g.row+1, len(g.Rows))
	if len(g.Rows) == 0 {
		at = 0
	}
	g.Rows = append(g.Rows[:at], append([]*gridRow{{Cur: cur, Dirty: dirty, State: rowNew}}, g.Rows[at:]...)...)
	g.row, g.selecting = at, false
	g.scroll()
	return true
}

func (g *Grid) selectedRows() []int {
	r0, _, r1, _ := g.selection()
	var out []int
	for i := r0; i <= r1 && i < len(g.Rows); i++ {
		out = append(out, i)
	}
	return out
}

func (g *Grid) DuplicateRows() bool {
	if !g.Editable || len(g.Rows) == 0 {
		return false
	}
	idx := g.selectedRows()
	g.snapshot()
	var dup []*gridRow
	for _, i := range idx {
		src := g.Rows[i]
		d := &gridRow{Cur: append([]any(nil), src.Cur...), Dirty: map[int]bool{}, State: rowNew}
		for c := range d.Cur {
			d.Dirty[c] = true
		}
		dup = append(dup, d)
	}
	at := idx[len(idx)-1] + 1
	g.Rows = append(g.Rows[:at], append(dup, g.Rows[at:]...)...)
	g.row, g.selecting = at, false
	g.scroll()
	return true
}

func (g *Grid) DeleteRows() bool {
	if !g.Editable || len(g.Rows) == 0 {
		return false
	}
	idx := g.selectedRows()
	g.snapshot()
	del := map[int]bool{}
	for _, i := range idx {
		del[i] = true
	}
	var kept []*gridRow
	for i, r := range g.Rows {
		switch {
		case !del[i]:
			kept = append(kept, r)
		case r.State == rowNew: // never reached the server: just drop it
		default:
			r.State = rowDeleted
			kept = append(kept, r)
		}
	}
	g.Rows = kept
	g.row, g.selecting = clamp(g.row, 0, len(g.Rows)-1), false
	g.scroll()
	return true
}

func (g *Grid) HasPending() bool {
	for _, r := range g.Rows {
		if r.State != rowClean || len(r.Dirty) > 0 {
			return true
		}
	}
	return false
}

// Changes turns the pending edits into db.Changes for table ref. Rows are identified by their PK
// columns' original values, or by every original value when the table has no primary key
// (so duplicate rows in a PK-less table are all affected, as in TablePlus).
func (g *Grid) Changes(ref db.ObjRef) []db.Change {
	var keyCols []int
	for i, c := range g.Cols {
		if c.PK {
			keyCols = append(keyCols, i)
		}
	}
	if len(keyCols) == 0 {
		for i := range g.Cols {
			keyCols = append(keyCols, i)
		}
	}
	var deletes, updates, inserts []db.Change
	for _, r := range g.Rows {
		key := func() []db.KeyVal {
			kv := make([]db.KeyVal, len(keyCols))
			for i, c := range keyCols {
				kv[i] = db.KeyVal{Col: g.Cols[c].Name, Val: r.Orig[c]}
			}
			return kv
		}
		switch {
		case r.State == rowNew:
			var set []db.KeyVal
			for c := range g.Cols {
				if r.Dirty[c] {
					set = append(set, db.KeyVal{Col: g.Cols[c].Name, Val: r.Cur[c]})
				}
			}
			inserts = append(inserts, db.Change{Kind: db.Insert, ObjRef: ref, Set: set})
		case r.State == rowDeleted:
			deletes = append(deletes, db.Change{Kind: db.Delete, ObjRef: ref, Key: key()})
		case len(r.Dirty) > 0:
			var set []db.KeyVal
			for c := range g.Cols {
				if r.Dirty[c] {
					set = append(set, db.KeyVal{Col: g.Cols[c].Name, Val: r.Cur[c]})
				}
			}
			updates = append(updates, db.Change{Kind: db.Update, ObjRef: ref, Key: key(), Set: set})
		}
	}
	// deletes first, inserts last: a key freed by a delete or update can be reused by an insert
	return append(append(deletes, updates...), inserts...)
}

func (g *Grid) DiscardPending() {
	var kept []*gridRow
	for _, r := range g.Rows {
		if r.State == rowNew {
			continue
		}
		r.State = rowClean
		r.Cur = append([]any(nil), r.Orig...)
		r.Dirty = map[int]bool{}
		kept = append(kept, r)
	}
	g.Rows, g.undo, g.redo, g.edit, g.selecting = kept, nil, nil, nil, false
	g.row = clamp(g.row, 0, len(g.Rows)-1)
}

// ---- copy -----------------------------------------------------------------------------

type CopyFormat string

const (
	FmtPlain     CopyFormat = "Plain Text"
	FmtJSON      CopyFormat = "JSON"
	FmtHTML      CopyFormat = "HTML"
	FmtMarkdown  CopyFormat = "Markdown Table"
	FmtCSV       CopyFormat = "CSV"
	FmtCSVHeader CopyFormat = "CSV with Header"
	FmtInsert    CopyFormat = "INSERT Statement"
)

var CopyFormats = []CopyFormat{FmtPlain, FmtJSON, FmtHTML, FmtMarkdown, FmtCSV, FmtCSVHeader, FmtInsert}

func jsonValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		return fmt.Sprintf("\\x%x", x)
	case string, bool, int64, uint64, float64:
		return x
	default:
		if v == db.Null || v == db.Default {
			return nil
		}
		return db.FormatValue(v)
	}
}

func plain(v any) string {
	if v == nil || v == db.Null {
		return "NULL"
	}
	if v == db.Empty {
		return ""
	}
	return db.FormatValue(v)
}

// FormatRect renders rows r0..r1, cols c0..c1 in the given format.
func (g *Grid) FormatRect(r0, c0, r1, c1 int, f CopyFormat, table string) string {
	names := make([]string, 0, c1-c0+1)
	for c := c0; c <= c1; c++ {
		names = append(names, g.Cols[c].Name)
	}
	cell := func(r, c int) any { return g.Rows[r].Cur[c] }
	var b strings.Builder
	switch f {
	case FmtCSV, FmtCSVHeader:
		w := csv.NewWriter(&b)
		if f == FmtCSVHeader {
			w.Write(names)
		}
		for r := r0; r <= r1; r++ {
			rec := make([]string, 0, len(names))
			for c := c0; c <= c1; c++ {
				rec = append(rec, plain(cell(r, c)))
			}
			w.Write(rec)
		}
		w.Flush()
	case FmtJSON:
		var docs []map[string]any
		for r := r0; r <= r1; r++ {
			d := map[string]any{}
			for c := c0; c <= c1; c++ {
				d[g.Cols[c].Name] = jsonValue(cell(r, c))
			}
			docs = append(docs, d)
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		enc.Encode(docs)
		return strings.TrimRight(buf.String(), "\n")
	case FmtHTML:
		b.WriteString("<table>\n<tr>")
		for _, n := range names {
			b.WriteString("<th>" + html.EscapeString(n) + "</th>")
		}
		b.WriteString("</tr>\n")
		for r := r0; r <= r1; r++ {
			b.WriteString("<tr>")
			for c := c0; c <= c1; c++ {
				b.WriteString("<td>" + html.EscapeString(plain(cell(r, c))) + "</td>")
			}
			b.WriteString("</tr>\n")
		}
		b.WriteString("</table>")
	case FmtMarkdown:
		esc := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ") }
		b.WriteString("| " + strings.Join(names, " | ") + " |\n|" + strings.Repeat(" --- |", len(names)) + "\n")
		for r := r0; r <= r1; r++ {
			b.WriteString("|")
			for c := c0; c <= c1; c++ {
				b.WriteString(" " + esc(plain(cell(r, c))) + " |")
			}
			b.WriteString("\n")
		}
	case FmtInsert:
		d := db.PG // generic quoting; the pasted statement is meant to be edited anyway
		qn := make([]string, len(names))
		for i, n := range names {
			qn[i] = d.Quote(n)
		}
		for r := r0; r <= r1; r++ {
			vals := make([]string, 0, len(names))
			for c := c0; c <= c1; c++ {
				vals = append(vals, d.Literal(cell(r, c)))
			}
			fmt.Fprintf(&b, "INSERT INTO %s (%s) VALUES (%s);\n", d.Quote(table), strings.Join(qn, ", "), strings.Join(vals, ", "))
		}
	default: // plain: tab separated
		for r := r0; r <= r1; r++ {
			for c := c0; c <= c1; c++ {
				if c > c0 {
					b.WriteByte('\t')
				}
				b.WriteString(plain(cell(r, c)))
			}
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// CopyRows is the selected rows with all columns; CopyCells is just the selected rectangle.
func (g *Grid) CopyRows(f CopyFormat, table string) string {
	if len(g.Rows) == 0 {
		return ""
	}
	r0, _, r1, _ := g.selection()
	return g.FormatRect(r0, 0, r1, len(g.Cols)-1, f, table)
}

func (g *Grid) CopyCells(f CopyFormat, table string) string {
	if len(g.Rows) == 0 {
		return ""
	}
	r0, c0, r1, c1 := g.selection()
	return g.FormatRect(r0, c0, r1, c1, f, table)
}

func (g *Grid) CopyColumn() string {
	var vals []string
	for _, r := range g.Rows {
		vals = append(vals, plain(r.Cur[g.col]))
	}
	return strings.Join(vals, "\n")
}

// CursorValue is the raw value under the cursor.
func (g *Grid) CursorValue() (any, bool) {
	if r := g.cur(); r != nil && g.col < len(r.Cur) {
		return r.Cur[g.col], true
	}
	return nil, false
}

// ---- render ---------------------------------------------------------------------------

func isNumber(v any) bool {
	switch v.(type) {
	case int64, uint64, float64, int, int32:
		return true
	}
	return false
}

func (g *Grid) View(th Theme, focused bool) string {
	if g.W < 8 || g.H < 2 {
		return ""
	}
	gw := g.gutterW()
	var hdr strings.Builder
	hdr.WriteString(th.HeaderCell.Render(fit("", gw)))
	for i := g.left; i < len(g.Cols); i++ {
		c := g.Cols[i]
		name := c.Name
		if c.PK {
			name = "⚷" + name
		}
		if g.SortCol == c.Name {
			if g.SortDesc {
				name += " ▼"
			} else {
				name += " ▲"
			}
		}
		hdr.WriteString(th.HeaderCell.Render("│" + fit(name, c.Width)))
		if width(hdr.String()) >= g.W {
			break
		}
	}
	head := ansi.Truncate(hdr.String(), g.W, "")
	if pad := g.W - width(head); pad > 0 {
		head += th.HeaderCell.Render(strings.Repeat(" ", pad))
	}
	lines := []string{head}
	for vi := 0; vi < g.bodyH(); vi++ {
		ri := g.top + vi
		if ri >= len(g.Rows) {
			lines = append(lines, strings.Repeat(" ", g.W))
			continue
		}
		r := g.Rows[ri]
		base := th.Cell
		switch {
		case r.State == rowDeleted:
			base = th.Deleted
		case r.State == rowNew:
			base = th.New
		case ri%2 == 1:
			base = th.AltRow
		}
		var sb strings.Builder
		sb.WriteString(th.Dim.Render(padLeft(fmt.Sprint(g.RowOffset+ri+1), gw-1) + " "))
		for ci := g.left; ci < len(g.Cols); ci++ {
			c := g.Cols[ci]
			v := r.Cur[ci]
			text := cellText(v)
			if g.edit != nil && ri == g.row && ci == g.col {
				sb.WriteString(th.Border.Render("│") + g.edit.View(c.Width, th, true))
				continue
			}
			cell := fit(text, c.Width)
			if isNumber(v) {
				cell = padLeft(fit(text, c.Width), c.Width)
				if t := fit(text, c.Width); strings.TrimSpace(t) != "" {
					cell = padLeft(strings.TrimRight(t, " "), c.Width)
				}
			}
			st := base
			switch {
			case ri == g.row && ci == g.col && focused:
				st = th.SelCell
			case g.inSelection(ri, ci):
				st = th.SelRow
			case r.Dirty[ci] && r.State == rowClean:
				st = th.Modified
			case isSpecial(v):
				st = base.Inherit(th.Null)
			}
			sb.WriteString(th.Border.Render("│") + st.Render(cell))
			if width(sb.String()) >= g.W {
				break
			}
		}
		line := ansi.Truncate(sb.String(), g.W, "")
		if pad := g.W - width(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// ParseClip splits clipboard text into rows of fields: tab separated if there is any tab
// (what "copy cells" produces), otherwise CSV (what "copy rows" produces).
func ParseClip(text string) [][]string {
	text = strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if text == "" {
		return nil
	}
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord, r.LazyQuotes = -1, true
	if strings.Contains(text, "\t") {
		r.Comma = '\t'
		r.LazyQuotes = true
	}
	recs, err := r.ReadAll()
	if err != nil {
		return [][]string{{text}}
	}
	return recs
}

func clipValue(s string) any {
	if s == "NULL" {
		return db.Null
	}
	return s
}

// PasteRows inserts the parsed rows as new rows after the cursor. Values go to columns in order;
// columns the clipboard does not cover stay DEFAULT and are left out of the INSERT.
func (g *Grid) PasteRows(data [][]string) bool {
	if !g.Editable || len(data) == 0 {
		return false
	}
	g.snapshot()
	var add []*gridRow
	for _, rec := range data {
		cur := make([]any, len(g.Cols))
		dirty := map[int]bool{}
		for c := range cur {
			cur[c] = db.Default
			if c < len(rec) {
				cur[c], dirty[c] = clipValue(rec[c]), true
			}
		}
		add = append(add, &gridRow{Cur: cur, Dirty: dirty, State: rowNew})
	}
	at := min(g.row+1, len(g.Rows))
	g.Rows = append(g.Rows[:at], append(add, g.Rows[at:]...)...)
	g.row, g.selecting = at, false
	g.scroll()
	return true
}

// PasteCells writes the parsed block into existing cells starting at the cursor (clipped at the grid edge).
func (g *Grid) PasteCells(data [][]string) bool {
	if !g.Editable || len(data) == 0 || len(g.Rows) == 0 {
		return false
	}
	g.snapshot()
	for i, rec := range data {
		r := g.row + i
		if r >= len(g.Rows) || g.Rows[r].State == rowDeleted {
			continue
		}
		for j, f := range rec {
			c := g.col + j
			if c >= len(g.Cols) {
				break
			}
			row := g.Rows[r]
			v := clipValue(f)
			row.Cur[c] = v
			if row.State == rowNew || !sameValue(v, row.Orig[c]) {
				row.Dirty[c] = true
			} else {
				delete(row.Dirty, c)
			}
		}
	}
	return true
}

// Find moves the cursor to the next (dir=1) or previous (dir=-1) cell whose text contains text.
func (g *Grid) Find(text string, dir int) bool {
	if text == "" || len(g.Rows) == 0 || len(g.Cols) == 0 {
		return false
	}
	text = strings.ToLower(text)
	n := len(g.Rows) * len(g.Cols)
	pos := g.row*len(g.Cols) + g.col
	for i := 1; i <= n; i++ {
		p := ((pos+dir*i)%n + n) % n
		r, c := p/len(g.Cols), p%len(g.Cols)
		if strings.Contains(strings.ToLower(cellText(g.Rows[r].Cur[c])), text) {
			g.row, g.col, g.selecting = r, c, false
			g.scroll()
			return true
		}
	}
	return false
}
