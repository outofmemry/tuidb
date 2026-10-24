package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func testGrid() *Grid {
	g := &Grid{Editable: true, W: 60, H: 8}
	g.SetData([]gridCol{{Name: "id", PK: true}, {Name: "name"}, {Name: "note"}},
		[][]any{{int64(1), "Ann", nil}, {int64(2), "Bob", "x"}, {int64(3), "Cy", "y,z"}})
	return g
}

func TestGridNavigationAndClamp(t *testing.T) {
	g := testGrid()
	g.Move(10, 10, false)
	if g.row != 2 || g.col != 2 {
		t.Errorf("clamp: %d,%d", g.row, g.col)
	}
	g.Move(-10, -10, false)
	if g.row != 0 || g.col != 0 {
		t.Errorf("clamp low: %d,%d", g.row, g.col)
	}
	empty := &Grid{W: 40, H: 5}
	empty.Move(1, 1, false) // must not panic on no data
	empty.Page(1, false)
	if empty.AddRow() {
		t.Error("read-only grid must refuse to add rows")
	}
}

func TestGridEditTracksDirtyAndUndo(t *testing.T) {
	g := testGrid()
	g.GoCol(1, false)
	g.StartEdit()
	g.edit.Set("Annie")
	g.FinishEdit(true)
	if !g.Rows[0].Dirty[1] || g.Rows[0].Cur[1] != "Annie" || !g.HasPending() {
		t.Fatalf("%+v", g.Rows[0])
	}
	g.StartEdit()
	g.edit.Set("Ann") // back to the original value
	g.FinishEdit(true)
	if g.Rows[0].Dirty[1] || g.HasPending() {
		t.Errorf("editing back to the original must clear dirty: %+v", g.Rows[0])
	}
	g.SetCell(db.Null)
	if g.Rows[0].Cur[1] != db.Null || !g.Rows[0].Dirty[1] {
		t.Error("set NULL")
	}
	if !g.Undo() || g.Rows[0].Cur[1] != "Ann" {
		t.Errorf("undo: %+v", g.Rows[0])
	}
	if !g.Redo() || g.Rows[0].Cur[1] != db.Null {
		t.Errorf("redo: %+v", g.Rows[0])
	}
	g.DiscardPending()
	if g.HasPending() || g.Rows[0].Cur[1] != "Ann" {
		t.Error("discard")
	}
	g.edit = nil
	g.StartEdit()
	g.edit.Set("zzz")
	g.FinishEdit(false)
	if g.HasPending() {
		t.Error("esc must not save")
	}
}

func TestGridChanges(t *testing.T) {
	g := testGrid()
	ref := db.ObjRef{Schema: "public", Name: "t"}
	g.GoCol(1, false)
	g.SetCell("Annie")
	g.GoRow(1, false)
	g.DeleteRows()
	g.AddRow()
	g.GoCol(1, false)
	g.SetCell("New")
	ch := g.Changes(ref)
	if len(ch) != 3 {
		t.Fatalf("%d changes: %+v", len(ch), ch)
	}
	ch[0], ch[1] = ch[1], ch[0] // planner order is delete, update, insert
	if u := ch[0]; u.Kind != db.Update || len(u.Key) != 1 || u.Key[0].Col != "id" || u.Key[0].Val != int64(1) || u.Set[0].Val != "Annie" {
		t.Errorf("update %+v", u)
	}
	if d := ch[1]; d.Kind != db.Delete || d.Key[0].Val != int64(2) {
		t.Errorf("delete %+v", d)
	}
	if in := ch[2]; in.Kind != db.Insert || len(in.Set) != 1 || in.Set[0].Col != "name" {
		t.Errorf("insert must only set edited columns: %+v", in)
	}
	// A new row that is then deleted never reaches the server.
	g.DeleteRows()
	if n := len(g.Changes(ref)); n != 2 {
		t.Errorf("after deleting the new row: %d", n)
	}
	// Without a PK every original value identifies the row.
	np := &Grid{Editable: true, W: 40, H: 5}
	np.SetData([]gridCol{{Name: "a"}, {Name: "b"}}, [][]any{{int64(1), nil}})
	np.GoCol(0, false)
	np.SetCell("9")
	k := np.Changes(ref)[0].Key
	if len(k) != 2 || k[1].Val != nil {
		t.Errorf("no-pk key: %+v", k)
	}
}

func TestGridDuplicateAndSelection(t *testing.T) {
	g := testGrid()
	g.Move(1, 0, true) // select rows 0..1
	g.DuplicateRows()
	if len(g.Rows) != 5 || g.Rows[2].State != rowNew || g.Rows[2].Cur[1] != "Ann" || g.Rows[3].Cur[1] != "Bob" {
		t.Fatalf("%d rows", len(g.Rows))
	}
	ch := g.Changes(db.ObjRef{Name: "t"})
	if len(ch) != 2 || ch[0].Kind != db.Insert || len(ch[0].Set) != 3 {
		t.Errorf("duplicates insert every column: %+v", ch)
	}
}

func TestGridCopyFormats(t *testing.T) {
	g := testGrid()
	g.Move(2, 0, true)
	for f, want := range map[CopyFormat]string{
		FmtCSV:       "1,Ann,NULL\n2,Bob,x\n3,Cy,\"y,z\"",
		FmtCSVHeader: "id,name,note\n1,Ann,NULL",
		FmtPlain:     "1\tAnn\tNULL\n2\tBob\tx\n3\tCy\ty,z",
	} {
		got := g.CopyRows(f, "t")
		if !strings.HasPrefix(got, want) {
			t.Errorf("%s:\n got  %q\n want %q", f, got, want)
		}
	}
	if got := g.CopyRows(FmtInsert, "t"); !strings.HasPrefix(got, `INSERT INTO "t" ("id", "name", "note") VALUES ('1', 'Ann', NULL);`) {
		// ints print without quotes
		if !strings.HasPrefix(got, `INSERT INTO "t" ("id", "name", "note") VALUES (1, 'Ann', NULL);`) {
			t.Errorf("insert: %q", got)
		}
	}
	if got := g.CopyRows(FmtMarkdown, "t"); !strings.HasPrefix(got, "| id | name | note |\n| --- | --- | --- |\n| 1 | Ann | NULL |") {
		t.Errorf("markdown: %q", got)
	}
	if got := g.CopyRows(FmtJSON, "t"); !strings.Contains(got, `"name": "Ann"`) || !strings.Contains(got, `"note": null`) {
		t.Errorf("json: %q", got)
	}
	if got := g.CopyRows(FmtHTML, "t"); !strings.Contains(got, "<td>Ann</td>") {
		t.Errorf("html: %q", got)
	}
	g.ClearSelection()
	g.GoRow(0, false)
	g.GoCol(1, false)
	if got := g.CopyCells(FmtPlain, "t"); got != "Ann" {
		t.Errorf("cell: %q", got)
	}
	if got := g.CopyColumn(); got != "Ann\nBob\nCy" {
		t.Errorf("column: %q", got)
	}
}

func TestGridViewFitsDimensions(t *testing.T) {
	th := NewTheme(configTheme())
	g := testGrid()
	g.W, g.H = 40, 6
	for _, focused := range []bool{true, false} {
		lines := strings.Split(g.View(th, focused), "\n")
		if len(lines) != 6 {
			t.Fatalf("height %d", len(lines))
		}
		for i, l := range lines {
			if w := width(l); w != 40 {
				t.Errorf("line %d width %d: %q", i, w, l)
			}
		}
	}
	g.GoCol(2, false)
	g.StartEdit()
	g.edit.Set("a very long value that is way wider than the cell itself!!")
	for _, l := range strings.Split(g.View(th, true), "\n") {
		if width(l) != 40 {
			t.Errorf("editing overflow %d", width(l))
		}
	}
	// Narrow windows must not panic.
	g.W, g.H = 8, 2
	_ = g.View(th, true)
	g.W, g.H = 4, 1
	if g.View(th, true) != "" {
		t.Error("too small renders nothing")
	}
}

func TestGridEditingNullCellWithoutChangeKeepsNull(t *testing.T) {
	g := testGrid()
	g.GoCol(2, false) // Ann's note is NULL
	g.StartEdit()
	g.FinishEdit(true)
	if g.HasPending() || g.Rows[0].Cur[2] != nil {
		t.Fatalf("opening and closing the editor must leave NULL alone: %+v", g.Rows[0])
	}
	g.StartEdit()
	g.edit.Set("hi")
	g.FinishEdit(true)
	if g.Rows[0].Cur[2] != "hi" {
		t.Fatal("a real edit still works")
	}
	// DEFAULT cells of a new row behave the same
	g.AddRow()
	g.GoCol(1, false)
	g.StartEdit()
	g.FinishEdit(true)
	if g.Rows[g.row].Cur[1] != db.Default {
		t.Errorf("DEFAULT must survive an untouched edit: %v", g.Rows[g.row].Cur[1])
	}
}

func TestGridStaleSelectionAfterUndoDoesNotPanic(t *testing.T) {
	g := testGrid()
	g.Move(2, 0, true) // select rows 0..2
	g.AddRow()
	g.AddRow()
	g.Undo()
	g.Undo()
	_ = g.CopyRows(FmtCSV, "t") // used to index past the restored rows
	g.AddRow()
	g.Move(1, 0, true)
	g.DiscardPending()
	_ = g.CopyRows(FmtCSV, "t")
	g.DeleteRows()
	g.Rows = nil
	g.row = 0
	g.StartEdit() // no rows
	g.FinishEdit(true)
}

func TestParseClip(t *testing.T) {
	for in, want := range map[string][][]string{
		"1,Ann,NULL\n2,Bob,x":           {{"1", "Ann", "NULL"}, {"2", "Bob", "x"}},
		"1\tAnn\t\n2\tBob\ty,z":         {{"1", "Ann", ""}, {"2", "Bob", "y,z"}},
		`1,"a,b","say ""hi"""` + "\r\n": {{"1", "a,b", `say "hi"`}},
		"single":                        {{"single"}},
		"":                              nil,
	} {
		got := ParseClip(in)
		if len(got) != len(want) {
			t.Errorf("%q: %v", in, got)
			continue
		}
		for i := range want {
			if strings.Join(got[i], "|") != strings.Join(want[i], "|") {
				t.Errorf("%q row %d: %q want %q", in, i, got[i], want[i])
			}
		}
	}
}

func TestGridPasteAndFind(t *testing.T) {
	g := testGrid()
	g.GoRow(1, false)
	if !g.PasteRows([][]string{{"9", "Zed", "NULL"}, {"10", "Yan"}}) || len(g.Rows) != 5 || g.Rows[2].State != rowNew {
		t.Fatalf("rows: %d", len(g.Rows))
	}
	if g.Rows[2].Cur[2] != "NULL" || g.Rows[3].Cur[2] != db.Default || g.Rows[3].Dirty[2] {
		t.Errorf("pasted text stays text (the word NULL included); uncovered columns stay DEFAULT: %+v", g.Rows[3])
	}
	if n := len(g.Changes(db.ObjRef{Name: "t"})); n != 2 {
		t.Errorf("two inserts: %d", n)
	}
	g2 := testGrid()
	g2.GoRow(1, false)
	g2.GoCol(1, false)
	g2.PasteCells([][]string{{"B1", "b2"}, {"C1", "c2"}, {"over", "the", "edge"}})
	if g2.Rows[1].Cur[1] != "B1" || g2.Rows[1].Cur[2] != "b2" || g2.Rows[2].Cur[1] != "C1" || !g2.Rows[1].Dirty[1] {
		t.Errorf("cells: %+v %+v", g2.Rows[1], g2.Rows[2])
	}
	if len(g2.Rows) != 3 {
		t.Error("pasting cells never adds rows (the third line is clipped)")
	}
	g2.PasteCells([][]string{{"Bob"}}) // pasting the original value back is not a change
	if g2.Rows[1].Dirty[1] {
		t.Error("original value pasted back must clear dirty")
	}
	ro := testGrid()
	ro.Editable = false
	if ro.PasteRows([][]string{{"1"}}) || ro.PasteCells([][]string{{"1"}}) {
		t.Error("read-only grids refuse paste")
	}

	f := testGrid()
	if !f.Find("bob", 1) || f.row != 1 || f.col != 1 {
		t.Errorf("find: %d,%d", f.row, f.col)
	}
	if !f.Find("bob", 1) || f.row != 1 { // wraps around to the same single match
		t.Error("wrap")
	}
	if f.Find("nope", 1) || (&Grid{}).Find("x", 1) {
		t.Error("no match, no data")
	}
	f.GoRow(0, false)
	f.GoCol(0, false)
	if !f.Find("y", -1) || f.row != 2 {
		t.Errorf("backwards from the top wraps to the last match: %d,%d", f.row, f.col)
	}
}

func benchGrid() *Grid {
	cols := make([]gridCol, 20)
	for c := range cols {
		cols[c] = gridCol{Name: fmt.Sprintf("column_%d", c)}
	}
	rows := make([][]any, 300)
	for r := range rows {
		row := make([]any, 20)
		for c := range row {
			switch c % 4 {
			case 0:
				row[c] = int64(r * c)
			case 1:
				row[c] = fmt.Sprintf("some text value %d-%d", r, c)
			case 2:
				row[c] = nil
			default:
				row[c] = float64(r) / 7
			}
		}
		rows[r] = row
	}
	g := &Grid{W: 120, H: 40}
	g.SetData(cols, rows)
	return g
}

// One iteration = 100 cursor steps, each followed by a frame, as Bubble Tea renders after every key.
func benchMoves(b *testing.B, dr, dc int) {
	th := NewTheme(configTheme())
	for b.Loop() {
		g := benchGrid()
		for range 100 {
			g.Move(dr, dc, false)
			_ = g.View(th, true)
		}
	}
}

func BenchmarkGridDown(b *testing.B)  { benchMoves(b, 1, 0) }
func BenchmarkGridRight(b *testing.B) { benchMoves(b, 0, 1) }

func TestDetailMove(t *testing.T) {
	g := &Grid{}
	g.SetData([]gridCol{{Name: "a"}, {Name: "b"}, {Name: "c"}}, [][]any{{1, 2, 3}})
	for _, k := range []string{"j", "down", "tab"} {
		if !detailMove(g, k) {
			t.Fatalf("%s should move", k)
		}
	}
	if g.col != 2 {
		t.Fatalf("col = %d, want 2 after three moves down", g.col)
	}
	detailMove(g, "j") // clamps at the last field
	if g.col != 2 {
		t.Fatalf("col = %d, want 2 at the end", g.col)
	}
	detailMove(g, "home")
	if g.col != 0 || detailMove(g, "enter") {
		t.Fatalf("home should go to 0 and enter is not a move (col=%d)", g.col)
	}
}

func TestCopyCellsAsAllFormats(t *testing.T) {
	g := testGrid()
	g.GoRow(0, false)
	g.GoCol(0, false)
	g.GoRow(1, true)
	g.GoCol(1, true) // ids and names of the first two rows
	if got := g.CopyCells(FmtCSVHeader, "t"); got != "id,name\n1,Ann\n2,Bob" {
		t.Errorf("csv: %q", got)
	}
	for _, f := range CopyFormats {
		if g.CopyCells(f, "t") == "" {
			t.Errorf("%s copied nothing", f)
		}
	}
	if got := g.CopyCells(FmtMarkdown, "t"); !strings.Contains(got, "| id | name |") || strings.Contains(got, "note") {
		t.Errorf("markdown covers only the selected columns: %q", got)
	}
	if got := g.CopyColumn(); got != "2\n"+"Bob\n"+"Cy" && got != "Ann\nBob\nCy" {
		t.Errorf("copy_column: %q", got)
	}
}

func TestColumnMenuSortAndLocalSort(t *testing.T) {
	g := testGrid()
	g.GoCol(1, false)
	var gotName string
	var gotDir int
	m := colMenu{g: g, sort: func(name string, dir int) tea.Cmd { gotName, gotDir = name, dir; g.SortLocal(name, dir); return nil }}
	m.run("sort_desc", "name")
	if gotName != "name" || gotDir != -1 || g.Rows[0].Cur[1] != "Cy" || !g.SortDesc || g.SortCol != "name" {
		t.Fatalf("desc: %s %d %v", gotName, gotDir, g.Rows[0].Cur)
	}
	m.run("sort_asc", "name")
	if g.Rows[0].Cur[1] != "Ann" || g.SortDesc {
		t.Fatalf("asc: %v", g.Rows[0].Cur)
	}
	m.run("sort_none", "name")
	if g.SortCol != "" || g.Rows[2].Cur[1] != "Cy" || g.Rows[0].Cur[1] != "Ann" {
		t.Fatalf("none restores the loaded order: %v", g.Rows[0].Cur)
	}
	g.SortLocal("id", -1) // numbers sort as numbers
	if g.Rows[0].Cur[0] != int64(3) {
		t.Fatalf("numeric: %v", g.Rows[0].Cur)
	}
}

func TestCellDisplayNullEmptyDefault(t *testing.T) {
	for v, want := range map[string]string{"nil": "NULL", "empty": "''", "emptyval": "''", "default": "DEFAULT", "text": "abc"} {
		var in any
		switch v {
		case "nil":
		case "empty":
			in = ""
		case "emptyval":
			in = db.Empty
		case "default":
			in = db.Default
		case "text":
			in = "abc"
		}
		if got := cellDisplay(in, 10); got != want {
			t.Errorf("%s: %q, want %q", v, got, want)
		}
	}
	if !specialCell("") || !specialCell(nil) || specialCell("x") {
		t.Error("NULL, '' and DEFAULT draw dim italic, ordinary text does not")
	}
}

func TestGridRendersWKT(t *testing.T) {
	pt := []byte{1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0, 0, 0, 0, 0, 0, 0, 0x40} // POINT(1 2), little endian
	g := &Grid{W: 60, H: 4, DecodeSpatial: true}
	g.SetData([]gridCol{{Name: "geom"}}, [][]any{{pt}})
	if got := g.cellString(g.Rows[0], 0, 20); got != "POINT(1 2)" {
		t.Fatalf("wkt: %q", got)
	}
	g.DecodeSpatial = false
	if got := g.cellString(g.Rows[0], 0, 20); strings.Contains(got, "POINT") {
		t.Fatalf("decode_spatial off must not decode: %q", got)
	}
}

func TestGridHitTests(t *testing.T) {
	g := testGrid() // id, name, note
	g.W, g.H = 60, 8
	gw := g.gutterW()
	g.Cols[0].Width, g.Cols[1].Width = 4, 6
	if c, edge := g.HeaderAt(gw + 2); c != 0 || edge {
		t.Errorf("inside id: %d %v", c, edge)
	}
	if c, edge := g.HeaderAt(gw + 5); c != 0 || !edge { // the separator after id
		t.Errorf("edge of id: %d %v", c, edge)
	}
	if c, _ := g.HeaderAt(gw + 7); c != 1 {
		t.Errorf("inside name: %d", c)
	}
	if r, c, ok := g.CellAt(gw+7, 2); !ok || r != 1 || c != 1 {
		t.Errorf("cell: %d %d %v", r, c, ok)
	}
	if _, _, ok := g.CellAt(gw+2, 9); ok {
		t.Error("below the rows is not a cell")
	}
	g.Resize(0, 3)
	if g.Cols[0].Width != 7 {
		t.Errorf("resize: %d", g.Cols[0].Width)
	}
}

func TestEscDeselectsAndTabEditsNextCell(t *testing.T) {
	g := testGrid()
	g.Move(1, 0, true)
	if !g.selecting {
		t.Fatal("shift-move selects")
	}
	g.ClearSelection()
	if g.selecting {
		t.Fatal("esc deselects")
	}
	g.GoRow(0, false)
	g.GoCol(1, false)
	g.StartEdit()
	g.edit.Set("Annie")
	g.EditKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if g.Rows[0].Cur[1] != "Annie" || g.col != 2 || !g.Editing() {
		t.Fatalf("tab saves and edits the next cell: %v col %d editing %v", g.Rows[0].Cur, g.col, g.Editing())
	}
	g.EditKey(tea.KeyPressMsg{Code: tea.KeyTab}) // last column: wraps to the next row
	if g.row != 1 || g.col != 0 || !g.Editing() {
		t.Fatalf("wrap: row %d col %d", g.row, g.col)
	}
	g.EditKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if g.Editing() {
		t.Fatal("esc closes the editor")
	}
}

func TestCellEditPopoverTab(t *testing.T) {
	var saved string
	var moved int
	d := newCellEdit("name", "Ann", func(s string) tea.Cmd { saved = s; return nil })
	d.next = func(dir int) tea.Cmd { moved = dir; return nil }
	d.ed.SetText("Annie")
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if saved != "Annie" || moved != 1 {
		t.Fatalf("tab: saved %q moved %d", saved, moved)
	}
	d.ed.SetText("a\nb") // multi-line text keeps Tab as indentation
	moved = 0
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if moved != 0 {
		t.Fatal("tab in multi-line text must not leave the popover")
	}
}

func TestThemeSurfaceKeys(t *testing.T) {
	for name, p := range map[string]map[string]string{"dark": darkPalette, "light": lightPalette} {
		for _, k := range []string{"editor_bg", "window_bg", "sidebar_bg", "tabs_bg", "tab_active_bg", "notification_bg", "grid_line", "cell_selection"} {
			if p[k] == "" {
				t.Errorf("%s lacks %s", name, k)
			}
		}
	}
	for name, p := range namedPalettes {
		for _, k := range []string{"editor_bg", "window_bg", "sidebar_bg", "tabs_bg", "tab_active_bg", "notification_bg", "grid_line", "cell_selection"} {
			if p[k] == "" {
				t.Errorf("%s lacks %s", name, k)
			}
		}
	}
	pal := palette(config.Theme{Name: "dark", Colors: map[string]string{"sel_cell": "#123456"}})
	if pal["cell_selection"] != "#123456" {
		t.Errorf("overriding sel_cell must carry to cell_selection: %q", pal["cell_selection"])
	}
	if !strings.Contains(ThemeTOML(config.Theme{Name: "dark"}), `"editor_bg"`) {
		t.Error("theme export lists the new keys")
	}
}
