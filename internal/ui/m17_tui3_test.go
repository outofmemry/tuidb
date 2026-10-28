package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/db"
	"webtuidb/internal/plugin"
)

func TestTUI3ClearFiltersAsksOnce(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	tab := h.a.ws().tab().(*tableTab)
	tab.filters.Filters = []db.Filter{{Column: "id", Op: "=", Value: "1", Enabled: true}}
	tab.showFilters = true
	tab.grid.Rows = []*gridRow{{Cur: []any{1}, Orig: []any{1}, Dirty: map[int]bool{}}}
	tab.grid.Cols = []gridCol{{Name: "id"}}
	tab.grid.Editable = true
	tab.grid.SetCell(int64(9))
	if !tab.grid.HasPending() {
		t.Fatal("no pending edit")
	}
	prompts := func() int {
		n := 0
		for _, o := range h.a.overlays {
			if _, ok := o.(*confirmDlg); ok {
				n++
			}
		}
		return n
	}
	h.pump(tab.clearFilters())
	if prompts() != 1 {
		t.Fatalf("%d prompts", prompts())
	}
	h.press("esc") // No
	if len(tab.filters.Filters) != 1 || !tab.showFilters || !tab.grid.HasPending() {
		t.Fatal("declining must change nothing")
	}
	h.pump(tab.clearFilters())
	h.press("y") // Discard
	if prompts() != 0 {
		t.Fatalf("%d prompts left after Yes", prompts())
	}
	if len(tab.filters.Filters) != 0 || tab.showFilters {
		t.Fatal("filters not cleared")
	}
}

func TestTUI3SetColumnSkipsUnchanged(t *testing.T) {
	g := &Grid{Cols: []gridCol{{Name: "a"}}, Editable: true}
	g.Rows = []*gridRow{
		{Cur: []any{"x"}, Orig: []any{"x"}, Dirty: map[int]bool{}},
		{Cur: []any{"y"}, Orig: []any{"y"}, Dirty: map[int]bool{}},
	}
	g.anchorRow, g.row, g.selecting = 0, 1, true
	if n := g.SetColumn("x"); n != 1 {
		t.Fatalf("changed %d, want 1", n)
	}
	g.Undo()
	g.Rows[1].Cur[0] = "x"
	if n := g.SetColumn("x"); n != 0 {
		t.Fatalf("changed %d for an unchanged value", n)
	}
	if g.HasPending() {
		t.Fatal("an unchanged value staged something")
	}
}

func TestTUI3PluginDetail(t *testing.T) {
	if got := pluginDetail(plugin.Plugin{Output: "text", Key: "alt+P", Command: "echo"}); got != "text · alt+P · echo" {
		t.Fatalf("got %q", got)
	}
	if got := pluginDetail(plugin.Plugin{JS: true, Output: "sql", Command: "ignored"}); got != "js" {
		t.Fatalf("got %q", got)
	}
}

func TestTUI3ImportNullAsHint(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.run("import")
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	for _, fl := range f.fields {
		if fl.Key == "nullas" {
			if !strings.Contains(fl.Hint, "[csv] null_as") {
				t.Fatalf("hint %q", fl.Hint)
			}
			return
		}
	}
	t.Fatal("no nullas field")
}
