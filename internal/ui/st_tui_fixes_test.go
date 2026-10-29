package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

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

// T-P3-08 + T-P3-22: a preview shows tabs as → and applies with Enter; the bulk update says how many rows match.
func TestFlowBulkUpdatePreviewEnterAndCount(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SafeMode = "alert2" })
	h.connect()
	h.pump(h.a.ws().previewStatements("Fix", []db.Statement{{Text: "UPDATE t SET a = 'multi\nline\tnote'"}}, nil))
	h.want("line→note")
	h.press("esc")
	h.openCustomers()
	h.prefix("f")
	h.typeText("2")
	h.press("enter", "esc")
	h.run("bulk_update")
	h.want("3 rows match the filter") // the fake counts every row
	setField(t, h, "set", "email=x@y.z")
	h.press("enter") // the preview
	h.press("enter") // applies it, through the safe-mode gate
	h.want("send to the server?")
}

// T-P3-04: a validation error keeps the dialog open with what was typed.
func TestFlowDialogsKeepInputOnError(t *testing.T) {
	h := newHarness(t, nil)
	h.press("x") // export connections
	setField(t, h, "pass", "")
	setChoice(t, h, "pw", "true")
	h.press("enter")
	h.want("include passwords needs a passphrase")
	if _, ok := h.a.overlays[len(h.a.overlays)-1].(*formDlg); !ok {
		t.Fatal("the export form closed on the error")
	}
	h.press("esc")
	h.connect()
	h.openCustomers()
	h.prefix("L")
	h.press("ctrl+u")
	h.typeText("0")
	h.press("enter")
	h.want("limit must be 1 or more")
	if f, ok := h.a.overlays[len(h.a.overlays)-1].(*formDlg); !ok || f.values()["limit"] != "0" {
		t.Fatal("the page settings form closed on the error")
	}
	h.press("esc")
	h.pump(h.a.ws().importWizard(db.ObjRef{Name: "customers", Kind: db.KindTable}, false))
	setField(t, h, "path", "/nonexistent/x.csv")
	setField(t, h, "nullas", "NA")
	h.press("enter")
	h.want("cannot read that file")
	h.press("esc")
	h.pump(h.a.ws().importWizard(db.ObjRef{Name: "customers", Kind: db.KindTable}, false))
	if v := h.a.overlays[len(h.a.overlays)-1].(*formDlg).values(); v["path"] != "/nonexistent/x.csv" || v["nullas"] != "NA" {
		t.Fatalf("the import dialog forgot its answers: %v", v)
	}
	if savedQueryName(map[string]string{"name": "reports/ "}) == nil || savedQueryName(map[string]string{"name": "q"}) != nil {
		t.Fatal("savedQueryName")
	}
}

// T-P3-05: Esc on a changed connection form asks first; an unchanged one closes at once.
func TestFlowConnectionFormAsksBeforeDiscarding(t *testing.T) {
	h := newHarness(t, nil)
	h.press("n")
	h.press("esc")
	if len(h.a.overlays) != 0 {
		t.Fatal("an unchanged form closes without asking")
	}
	h.press("n")
	h.typeText("zz")
	h.press("esc")
	h.want("Discard changes to this connection?")
	h.press("n") // keep editing
	if f, ok := h.a.overlays[len(h.a.overlays)-1].(*formDlg); !ok || f.values()["name"] != "zz" {
		t.Fatal("cancelling the discard must bring the form back with the edits")
	}
	h.press("esc", "y")
	if len(h.a.overlays) != 0 || len(h.a.d.Conns) != 1 {
		t.Fatalf("discard: overlays %d conns %d", len(h.a.overlays), len(h.a.d.Conns))
	}
}
