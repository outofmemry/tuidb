package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/db"
)

func TestInlineArgsKeepsOrderForQuestionMarkDialects(t *testing.T) {
	d, _ := db.DialectFor("mysql")
	if got := inlineArgs(d, "`a` = ? AND `b` = ?", []any{1, 2}); got != "`a` = "+d.Literal(1)+" AND `b` = "+d.Literal(2) {
		t.Errorf("mysql: %s", got)
	}
	p, _ := db.DialectFor("postgres")
	if got := inlineArgs(p, `"a" = $1 AND "b" = $2`, []any{"x?", "y"}); got != `"a" = `+p.Literal("x?")+` AND "b" = `+p.Literal("y") {
		t.Errorf("postgres: %s", got)
	}
}

// P2-65: Alt+c changes nothing until the unsaved-changes prompt is answered.
func TestTUI1ClearFiltersWaitsForGuard(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("l", "e")
	h.typeText("Q")
	h.press("enter")
	tab := h.a.ws().tab().(*tableTab)
	tab.filters.Filters = []db.Filter{{Enabled: true, Column: "name", Op: "=", Value: "x"}}
	tab.showFilters = true
	h.pump(tab.clearFilters())
	h.want("Discard pending changes?")
	h.press("n")
	if len(tab.filters.Filters) != 1 || !tab.showFilters {
		t.Fatalf("cancelled, but filters=%v bar=%v", tab.filters.Filters, tab.showFilters)
	}
}

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

func TestM6FilterSQLPopover(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	tt := h.a.ws().tab().(*tableTab)
	tt.filters = db.FilterSet{Filters: []db.Filter{{Column: "name", Op: "=", Value: "Bob", Enabled: true}}}
	tt.markApplied() // the popover shows the applied filter (T-P2-03)
	h.run("filter_sql")
	h.want("Filter SQL")
	h.want(`WHERE "name" = 'Bob'`)
}

// T-P1-01: following a foreign key loads the referenced table filtered to the row, not every row.
func TestFlowFollowFKAppliesTheFilter(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	tab := h.a.ws().tab().(*tableTab)
	tab.cols = append([]db.Column(nil), tab.cols...) // do not touch the shared metadata
	tab.cols[1].FK = &db.ForeignKey{Columns: []string{"name"}, RefTable: "orders", RefColumns: []string{"id"}}
	tab.grid.GoCol(1, false)
	h.pump(tab.followFK())
	q := h.fake.lastQuery
	if q.ObjRef.Name != "orders" || len(q.Filters.Active()) != 1 || q.Filters.Filters[0].Column != "id" || q.Filters.Filters[0].Value != "Ann" {
		t.Fatalf("follow FK loaded %s with %+v", q.ObjRef.Name, q.Filters)
	}
}

// T-P2-03: the collapsed summary, the Filter SQL popover and the saved state show the applied filter.
func TestFlowFilterSummaryShowsApplied(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix("f")
	h.typeText("2")
	h.press("enter")
	h.press("backspace")
	h.typeText("9") // edited, not applied
	h.press("esc")
	h.want("id = 2")
	h.want("not applied")
	h.wantNot("id = 9")
	tab := h.a.ws().tab().(*tableTab)
	h.pump(tab.showFilterSQL())
	d := h.a.overlays[len(h.a.overlays)-1].(*textViewDlg)
	if !strings.Contains(d.lines[0], "2") || strings.Contains(d.lines[0], "9") || !strings.Contains(strings.Join(d.lines, "\n"), "not applied") {
		t.Fatalf("filter SQL: %q", d.lines)
	}
	h.press("esc")
	if ts := h.a.d.State.Table(tab.stateKey()); len(ts.Filters.Filters) != 1 || ts.Filters.Filters[0].Value != "2" {
		t.Fatalf("state keeps the unapplied edit: %+v", ts.Filters)
	}
	h.prefix("f")
	h.press("enter") // applying clears the mark
	h.press("esc")
	h.want("id = 9")
	h.wantNot("not applied")
}

// T-P3-15: Enter applies from the operator field, Raw SQL has no operator, the focus is bracketed.
func TestFlowFilterBarEnterAppliesAndRawHidesOp(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix("f")
	h.typeText("2")
	h.press("shift+tab")
	h.want("[= ▾]") // visible without colour
	h.press("enter")
	h.wantNot("Filter operator")
	if f := h.fake.lastQuery.Filters.Active(); len(f) != 1 || f[0].Value != "2" {
		t.Fatalf("enter on the operator field must apply: %+v", f)
	}
	h.want("Space  pick")
	tab := h.a.ws().tab().(*tableTab)
	tab.filters.Filters[0].Column = db.RawColumn
	tab.fb.field = ffColumn
	h.wantNot("= ▾")
	h.press("tab")
	if tab.fb.field != ffValue {
		t.Fatalf("tab from Raw SQL lands on %d, want the value", tab.fb.field)
	}
	h.press("shift+tab")
	if tab.fb.field != ffColumn {
		t.Fatalf("shift+tab from the value lands on %d, want the column", tab.fb.field)
	}
}
