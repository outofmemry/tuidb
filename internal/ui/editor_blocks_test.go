package ui

import (
	"strings"
	"testing"
)

func TestM6BlockSelectCopyAndDelete(t *testing.T) {
	e := NewEditor("postgres", EditorOpts{})
	e.SetText("abcd\nefgh\nijkl")
	e.cur = pos{0, 1}
	e.toggleBlock()
	e.Handle(keyMsg("right"))
	e.Handle(keyMsg("right"))
	e.Handle(keyMsg("down"))
	if got := e.Selection(); got != "bc\nfg" {
		t.Fatalf("block %q", got)
	}
	e.Handle(keyMsg("backspace"))
	if e.Text() != "ad\neh\nijkl" || e.blk != nil {
		t.Fatalf("after delete %q", e.Text())
	}
	// through the tab: V starts the mode, ctrl+c would copy Selection()
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	q.ed.SetText("abcd\nefgh")
	h.run("block_select")
	h.press("shift+right", "down")
	if q.ed.Selection() != "a\ne" || !strings.Contains(q.Title(), "BLOCK") {
		t.Fatalf("selection %q title %q", q.ed.Selection(), q.Title())
	}
	h.press("esc")
	if q.ed.blk != nil {
		t.Fatal("Esc ends block mode")
	}
}

func TestReview678BlockClearedOnUndo(t *testing.T) {
	e := NewEditor("postgres", EditorOpts{})
	e.SetText("a\nb\nc\nd")
	e.cur = pos{3, 0}
	e.Handle(keyMsg("enter"))
	e.toggleBlock()
	e.Undo()
	if e.blk != nil {
		t.Fatal("undo must leave block mode")
	}
}
