package ui

import (
	"testing"
)

func TestM6CustomGroups(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.press("m")
	h.pickLabel("Add to group")
	h.pickLabel("New group")
	setField(t, h, "name", "Core")
	h.press("enter")
	if gs := h.a.d.State.Groups("shop"); len(gs) != 1 || gs[0].Name != "Core" || len(gs[0].Items) != 1 || gs[0].Items[0] != "public.customers" {
		t.Fatalf("groups %+v", gs)
	}
	h.want("▣ Core")
	var group *sbRow
	for _, r := range h.a.ws().sb.rows() {
		if r.group == "Core" {
			r := r
			group = &r
		}
	}
	if group == nil {
		t.Fatal("the group section lists the table")
	}
	h.a.ws().sb.sel = indexOfRow(h.a.ws().sb.rows(), "Core")
	h.press("m")
	h.pickLabel("Remove from group")
	if gs := h.a.d.State.Groups("shop"); len(gs) != 0 {
		t.Fatalf("an empty group goes away: %+v", gs)
	}
	h.wantNot("▣ Core")
}
