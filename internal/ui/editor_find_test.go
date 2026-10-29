package ui

import (
	"testing"
)

func TestM2FindReplaceInEditor(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select a from a")
	h.prefix("alt+f")
	setField(t, h, "q", "A")
	setField(t, h, "r", "z")
	h.press("enter")
	if got := h.a.ws().tab().(*queryTab).ed.Text(); got != "select z from z" {
		t.Fatalf("got %q", got)
	}
}

func TestM6FindNextAndPrevious(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	q.ed.SetText("select foo; select foo; select foo")
	h.run("find_next") // no search text yet: asks
	setField(t, h, "q", "foo")
	h.press("enter")
	if q.ed.cur.col != 10 {
		t.Fatalf("first match ends at %d", q.ed.cur.col)
	}
	h.run("find_next")
	if q.ed.cur.col != 22 {
		t.Fatalf("second match ends at %d", q.ed.cur.col)
	}
	h.run("find_prev")
	if q.ed.cur.col != 10 || q.ed.Selection() != "foo" {
		t.Fatalf("previous match ends at %d sel %q", q.ed.cur.col, q.ed.Selection())
	}
	h.run("find_prev") // wraps to the last one
	if q.ed.cur.col != 34 {
		t.Fatalf("wrapped to %d", q.ed.cur.col)
	}
}
