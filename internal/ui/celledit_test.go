package ui

import (
	"strings"
	"testing"
)

// P2-67: the row-detail edit toast counts the rows SetColumn changed and says nothing for an unchanged value.
func TestTUI1FieldEditToast(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	g := h.a.ws().tab().(*tableTab).grid
	g.GoCol(1, false)
	h.pump(editFieldInPopover(h.a.ws(), g))
	h.press("ctrl+a")
	h.typeText("Zed")
	h.press("ctrl+s")
	h.want("staged name in 1 row(s)")
	if strings.Contains(h.screen(), `"name"`) {
		t.Fatal("the toast quotes the column name")
	}
}
