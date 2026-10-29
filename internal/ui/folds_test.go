package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/config"
)

func TestEditorFoldHidesLinesAndPersists(t *testing.T) {
	e := newEd()
	e.SetText("select 1,\n 2,\n 3;\nselect 4")
	var saved []int
	e.OnFolds = func(f []int) { saved = f }
	e.cur = pos{1, 0}
	e.ToggleFold()
	if e.cur.row != 0 || len(saved) != 1 || saved[0] != 0 {
		t.Fatalf("cur=%v saved=%v", e.cur, saved)
	}
	e.W, e.H = 40, 5
	v := stripANSI(e.View(NewTheme(config.Defaults().Theme), true))
	if !strings.Contains(v, "… (2 lines)") || strings.Contains(v, " 3;") {
		t.Errorf("folded view:\n%s", v)
	}
	e.move(1, 0, false) // the hidden lines are skipped
	if e.cur.row != 3 {
		t.Errorf("cursor on %d", e.cur.row)
	}
	e2 := newEd()
	e2.SetText(e.Text())
	e2.SetFolded(saved)
	if len(e2.Folded()) != 1 {
		t.Error("folds not restored")
	}
	e2.Insert("x") // shortcut: an edit unfolds
	if len(e2.Folded()) != 0 {
		t.Error("edit should unfold")
	}
}
