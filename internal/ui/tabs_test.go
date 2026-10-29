package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/config"
)

func TestM15MoveTab(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.a.run("new_query_tab")
	w := h.a.ws()
	if len(w.tabs) != 2 {
		t.Fatalf("%d tabs", len(w.tabs))
	}
	first := w.tabs[0]
	h.a.run("move_tab_left")
	if w.tabs[1] != first || w.cur != 0 {
		t.Errorf("after move left: cur=%d tabs=%v", w.cur, w.tabs)
	}
	h.a.run("move_tab_right")
	if w.tabs[0] != first || w.cur != 1 {
		t.Errorf("after move right: cur=%d", w.cur)
	}
	h.a.run("move_tab_right") // already last
	if w.cur != 1 || !strings.Contains(h.a.toast, "last") {
		t.Errorf("edge: cur=%d toast=%q", w.cur, h.a.toast)
	}
}

func TestM4TabMenu(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix("e")
	h.prefix("e")
	if n := len(h.a.ws().tabs); n != 3 {
		t.Fatalf("%d tabs, want 3", n)
	}
	h.prefix("alt+n") // rename the active (second editor) tab
	setField(t, h, "name", "scratch")
	h.press("enter")
	h.want("scratch")
	h.press("ctrl+d", "alt+p") // unrelated prefix key must not disturb the name
	h.press("esc")
	if got := tabNames(h); got[2] != "scratch" {
		t.Fatalf("names %v", got)
	}
	h.a.ws().selectTab(1)
	h.run("close_tabs_right")
	if n := len(h.a.ws().tabs); n != 2 {
		t.Fatalf("close_tabs_right left %d tabs", n)
	}
	h.run("close_other_tabs")
	if got := tabNames(h); len(got) != 1 {
		t.Fatalf("close_other_tabs left %v", got)
	}
	h.run("close_all_tabs")
	if n := len(h.a.ws().tabs); n != 0 {
		t.Fatalf("close_all_tabs left %d tabs", n)
	}
}

func TestM4CloseOthersAsksAboutUnsavedEdits(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.a.ws().tab().(*tableTab).grid.SetCell("zed") // a pending edit: the table tab is dirty
	h.prefix("e")
	h.run("close_other_tabs")
	h.want("unsaved changes")
	h.press("esc")
	if n := len(h.a.ws().tabs); n != 2 {
		t.Fatalf("declining must keep both tabs, have %d", n)
	}
}

func TestM4AutoSaveTabsRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want int
	}{{"first", 1}, {"all", 2}, {"none", 0}} {
		h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.Editor.AutoSaveTabs = tc.mode })
		h.connect()
		h.prefix("e")
		h.typeText("select 1")
		h.prefix("e")
		h.typeText("select 2")
		h.run("disconnect")
		h.press("enter")
		if h.a.ws() != nil {
			t.Fatalf("%s: still connected", tc.mode)
		}
		if got := len(h.a.d.State.OpenTabs("shop")); got != tc.want {
			t.Fatalf("%s: stored %d tabs, want %d", tc.mode, got, tc.want)
		}
		h.a.inWS = false
		h.connect()
		var texts []string
		for _, tb := range h.a.ws().tabs {
			if q, ok := tb.(*queryTab); ok {
				texts = append(texts, q.ed.Text())
			}
		}
		if len(texts) != tc.want || tc.want > 0 && texts[0] != "select 1" {
			t.Fatalf("%s: restored %v", tc.mode, texts)
		}
	}
}
