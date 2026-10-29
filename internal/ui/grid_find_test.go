package ui

import (
	"testing"
)

// P2-63: the results find form offers Replace once the result is editable.
func TestTUI1ResultFindOffersReplace(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	q.ed.SetText("select 1")
	h.run("run_current")
	if len(q.res) == 0 || q.res[0].grid == nil {
		t.Fatalf("no result grid:\n%s", h.screen())
	}
	q.res[0].grid.Editable = true // the result maps to one table (makeEditable)
	q.focusResults = true
	h.run("find")
	f, ok := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	if !ok {
		t.Fatalf("no find form:\n%s", h.screen())
	}
	for _, fl := range f.fields {
		if fl.Key == "with" {
			return
		}
	}
	t.Fatal("no Replace field")
}

func TestM4ResultSearchMatchCase(t *testing.T) {
	g := &Grid{}
	g.SetData([]gridCol{{Name: "n"}}, [][]any{{"alpha"}, {"ALPHA"}})
	if !g.FindCase("ALPHA", 1, true) || g.row != 1 {
		t.Fatalf("a case-sensitive search must skip row 0, at row %d", g.row)
	}
	g.row = 1
	if g.FindCase("alpha", 1, true) && g.row == 1 {
		t.Fatal("alpha must not match ALPHA when case matters")
	}
	if !g.FindCase("alpha", -1, false) {
		t.Fatal("a case-insensitive search must match")
	}
}

func TestM4SearchPromptTogglesMatchCase(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	queryResult(h, "select * from customers")
	h.press("/")
	h.want("match case off")
	h.press("alt+c")
	h.want("match case on")
}

func TestReplaceInFoldedLengthAndSkips(t *testing.T) {
	// Kelvin sign lowercases to a shorter byte sequence; prefix/suffix must not slice out of range.
	k := "K" + "x"
	for _, op := range []string{FindPrefix, FindSuffix} {
		s := FindSpec{Text: "kx", Op: op}
		if op == FindSuffix {
			k = "a" + "K" + "x"
			s.Text = "kx"
		}
		got, ok := s.replaceIn(k, "Z")
		if want := map[string]string{FindPrefix: "Z", FindSuffix: "aZ"}[op]; !ok || got != want {
			t.Errorf("%s: %q %v, want %q", op, got, ok, want)
		}
	}
	if _, ok := (FindSpec{Text: "kxy", Op: FindPrefix}).replaceIn("Kx", "Z"); ok {
		t.Error("longer text must not match")
	}

	g := &Grid{Editable: true, Cols: []gridCol{{Name: "id", Type: "int", PK: true}, {Name: "n", Type: "text"}}}
	g.SetData(g.Cols, [][]any{{"1a", "1b"}})
	g.Editable = true
	n, sk := g.ReplaceSkip(FindSpec{Text: "1", Op: FindContains}, "2", true)
	if n != 1 || sk != 1 || g.Rows[0].Cur[0] != "1a" || g.Rows[0].Cur[1] != "2b" {
		t.Errorf("any column: n=%d skipped=%d %v", n, sk, g.Rows[0].Cur)
	}
	if n, _ := g.ReplaceSkip(FindSpec{Text: "1", Col: "id", Op: FindContains}, "9", true); n != 1 {
		t.Errorf("a chosen column is edited: %d", n)
	}
}

func TestFindSuffixMultibyte(t *testing.T) {
	for _, c := range []struct{ cell, suf string }{{"xé", "é"}, {"a日本", "日本"}} {
		if n, ok := (FindSpec{Text: c.suf, Op: FindSuffix, Case: true}).endLen(c.cell, false); !ok || n != len(c.suf) {
			t.Errorf("%q suffix %q: %d %v", c.cell, c.suf, n, ok)
		}
	}
}

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
