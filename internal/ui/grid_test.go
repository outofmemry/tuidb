package ui

import (
	"strings"
	"testing"

	"tuidb/internal/db"
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
	if g.Rows[2].Cur[2] != db.Null || g.Rows[3].Cur[2] != db.Default || g.Rows[3].Dirty[2] {
		t.Errorf("NULL maps to NULL; uncovered columns stay DEFAULT and are not inserted: %+v", g.Rows[3])
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
