package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func TestPruneHiddenDropsVanishedColumns(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	tt := h.a.ws().tab().(*tableTab)
	tt.hidden = map[string]bool{"gone": true}
	tt.pruneHidden()
	if len(tt.hidden) != 0 {
		t.Fatal("stale hidden column kept")
	}
}

func TestM4SortByTwoColumns(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.run("sort_columns")
	h.want("Sort by")
	h.press("down", "space", "down", "space", "enter") // name, email
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	f.fields[1].Value = "desc"
	h.press("enter")
	want := []db.Sort{{Column: "name"}, {Column: "email", Desc: true}}
	if got := h.fake.lastQuery.Sort; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("sort %+v, want %+v", got, want)
	}
}

func TestM4HideAndShowColumns(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("right") // the name column
	h.run("hide_column")
	if got := h.fake.lastQuery.Columns; strings.Join(got, ",") != "id,email" {
		t.Fatalf("columns %v", got)
	}
	h.run("show_all_columns")
	if got := h.fake.lastQuery.Columns; len(got) != 0 {
		t.Fatalf("columns %v after show all", got)
	}
}

func TestM4FilterColumnsSortedByName(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.Table.FilterColumnSort = "name" })
	h.connect()
	h.openCustomers()
	tab := h.a.ws().tab().(*tableTab)
	var names []string
	for _, c := range tab.filterColumns() {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "email,id,name" {
		t.Fatalf("filter columns %v", names)
	}
}

// T-P3-07: every key of a multi-column sort is marked with its direction and order.
func TestFlowGridMultiSortMarks(t *testing.T) {
	th := NewTheme(configTheme())
	g := testGrid()
	for i := range g.Cols {
		g.Cols[i].Width = 12
	}
	g.Sorts = []db.Sort{{Column: "name"}, {Column: "note", Desc: true}}
	head := stripANSI(strings.Split(g.View(th, true), "\n")[0])
	if !strings.Contains(head, "name ▲1") || !strings.Contains(head, "note ▼2") {
		t.Fatalf("header: %q", head)
	}
	g.Sorts, g.SortCol = []db.Sort{{Column: "name"}}, "name" // a single key keeps the bare arrow
	if head := stripANSI(strings.Split(g.View(th, true), "\n")[0]); !strings.Contains(head, "name ▲") || strings.Contains(head, "▲1") {
		t.Fatalf("single sort header: %q", head)
	}
}
