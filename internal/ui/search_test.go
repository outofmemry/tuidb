package ui

import (
	"testing"
)

func TestM2SearchOpensFilteredTable(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("/")
	h.typeText("Ann")
	h.press("enter")
	h.want("customers")
	h.press("enter")
	tt, ok := h.a.ws().tab().(*tableTab)
	if !ok || len(tt.filters.Filters) != 1 || tt.filters.Filters[0].Value != "Ann" || !tt.showFilters {
		t.Fatalf("tab %#v", h.a.ws().tab())
	}
	if len(h.fake.lastQuery.Filters.Filters) != 1 {
		t.Errorf("rows were not loaded with the filter: %+v", h.fake.lastQuery)
	}
}

func TestM5SearchForField(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.run("toggle_row_detail")
	h.run("focus_right")
	h.press("/")
	setField(t, h, "q", "emai")
	h.press("enter")
	if c := h.a.ws().tab().(*tableTab).grid.col; c != 2 {
		t.Fatalf("cursor on column %d, want 2 (email)", c)
	}
}
