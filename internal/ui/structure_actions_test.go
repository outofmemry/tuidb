package ui

import (
	"testing"
)

func TestM5MoveStructureColumn(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.reorder = true
	h.connect()
	h.openCustomers()
	h.prefix(")")
	h.want("column_name")
	tt := h.a.ws().tab().(*tableTab)
	if tt.structPending() {
		t.Fatal("nothing is pending yet")
	}
	h.press("alt+down")
	if got := cellStr(tt.structGrid.Rows[0].Cur[scName]); got != "name" {
		t.Fatalf("first column is %q after the move", got)
	}
	if !tt.Dirty() {
		t.Fatal("a reorder is a pending structure change")
	}
	h.press("alt+up")
	if tt.structPending() {
		t.Fatal("moving it back leaves nothing pending")
	}
	h.fake.reorder = false
}

func TestM8StructureRowFormEditsEveryAttribute(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix(")") // structure
	h.want("column_name")
	h.press("space")
	h.want("Column id")
	setField(t, h, "name", "ident")
	setField(t, h, "type", "bigint")
	setChoice(t, h, "null", "true")
	setField(t, h, "default", "7")
	h.press("enter")
	g := h.a.ws().tab().(*tableTab).structGrid
	r := g.Rows[0]
	if cellStr(r.Cur[scName]) != "ident" || cellStr(r.Cur[scType]) != "bigint" || cellStr(r.Cur[scNull]) != "YES" || cellStr(r.Cur[scDefault]) != "7" {
		t.Fatalf("row %v", r.Cur)
	}
	if len(r.Dirty) != 4 {
		t.Fatalf("four attributes changed: %v", r.Dirty)
	}
}
