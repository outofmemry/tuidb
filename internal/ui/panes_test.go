package ui

import (
	"testing"

	"webtuidb/internal/db"
)

func TestM13ClosePaneKeepsEveryTab(t *testing.T) {
	h := newHarness(t, nil)
	w := twoTabs(h)
	h.prefix(`"`)
	if !w.panes.split {
		t.Fatal("split first")
	}
	h.run("close_pane")
	if w.panes.split || len(w.tabs) != 2 || len(w.groupIdx(0)) != 2 {
		t.Fatalf("after close_pane: split %v tabs %d", w.panes.split, len(w.tabs))
	}
	h.run("close_pane")
	h.want("one pane")
}

func TestM3SplitRightAndNextPane(t *testing.T) {
	h := newHarness(t, nil)
	w := twoTabs(h)
	h.prefix(`"`)
	if !w.panes.split || w.pane(w.tabs[1]) != 1 || w.panes.pane != 1 {
		t.Fatalf("split state %+v", w.panes)
	}
	h.want("customers")
	h.want("SQL")
	if _, ok := w.tab().(*queryTab); !ok {
		t.Fatal("the moved tab keeps the focus")
	}
	h.prefix("alt+]")
	if _, ok := w.tab().(*tableTab); !ok || w.panes.pane != 0 {
		t.Fatalf("next_pane did not focus the left group: %T pane %d", w.tab(), w.panes.pane)
	}
	h.prefix("alt+[")
	if _, ok := w.tab().(*queryTab); !ok {
		t.Fatal("prev_pane did not come back")
	}
	h.prefix(`"`) // moving the only tab of the right group back closes the split
	if w.panes.split {
		t.Fatalf("split should be closed: %+v", w.panes)
	}
}

func TestM3SplitNeedsTwoTabsAndNewTabsOpenInFocusedGroup(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix(`"`)
	h.want("open a second tab")
	h.prefix("e")
	h.prefix(`"`)
	h.prefix("e") // a new tab opens in the focused (right) group
	if w := h.a.ws(); w.pane(w.tabs[2]) != 1 || len(w.groupIdx(1)) != 2 {
		t.Fatalf("groups %v / %v", w.groupIdx(0), w.groupIdx(1))
	}
	h.prefix("w") // close_tab keeps the split while both groups have tabs
	if !h.a.ws().panes.split {
		t.Fatal("split closed too early")
	}
}

func TestM3GoBackAndForward(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	cust := db.ObjRef{Schema: "public", Name: "customers", Kind: db.KindTable}
	ord := db.ObjRef{Schema: "public", Name: "orders", Kind: db.KindTable}
	h.pump(w.openObject(cust, false))
	h.pump(w.openObject(ord, false))
	if w.tab().(*tableTab).ref != ord {
		t.Fatal("orders should be open")
	}
	h.prefix("<")
	if w.tab().(*tableTab).ref != cust {
		t.Fatalf("back should reopen customers, got %v", w.tab().(*tableTab).ref)
	}
	h.prefix(">")
	if w.tab().(*tableTab).ref != ord {
		t.Fatal("forward should reopen orders")
	}
	h.prefix(">")
	h.want("nothing to go forward to")
	for i := 0; i < 60; i++ { // the history is capped
		w.panes.visit(db.ObjRef{Name: string(rune('a' + i%26)), Schema: string(rune('A' + i))})
	}
	if len(w.panes.hist) != 50 {
		t.Fatalf("history %d", len(w.panes.hist))
	}
}

func TestM6SplitDownStacksTwoEditors(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select 1")
	h.run("split_down")
	q := h.queryTab()
	if q.other == nil || !q.focusBottom || q.ed.Text() != "" || q.other.Text() != "select 1" {
		t.Fatalf("split: other=%v bottom=%v ed=%q", q.other != nil, q.focusBottom, q.ed.Text())
	}
	h.typeText("select 2")
	h.run("next_pane")
	if q.ed.Text() != "select 1" || q.other.Text() != "select 2" {
		t.Fatalf("after next_pane: %q / %q", q.ed.Text(), q.other.Text())
	}
	h.want("select 2") // both editors are drawn
	h.want("select 1")
	h.run("split_down") // the unfocused one has text: asks
	h.press("y")
	if q.other != nil || q.ed.Text() != "select 1" {
		t.Fatal("split_down again closes the other pane")
	}
}

func TestReview678SplitClearsSavedAsAndDirtyCountsOtherPane(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	q.savedAs, q.lastAuto = "/x/report.sql", "a"
	h.typeText("select 1")
	h.run("split_down")
	if q.savedAs != "" || q.lastAuto != "" {
		t.Fatal("split must detach the saved query")
	}
	if !q.Dirty() {
		t.Fatal("text in the other pane counts as unsaved")
	}
}
