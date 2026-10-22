package ui

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"tuidb/internal/config"
	"tuidb/internal/db"
)

func TestFlowConnectBrowse(t *testing.T) {
	h := newHarness(t, nil)
	h.want("shop") // welcome lists the connection
	h.want("connect")
	h.connect()
	h.want("shop.public") // sidebar shows where it is
	h.want("Tables")
	h.want("Views")
	h.openCustomers()
	h.want("Bob")
	h.want("rows 1–3 of 3")
	if h.a.ws().tab().Title() != "customers" {
		t.Errorf("tab %q", h.a.ws().tab().Title())
	}
}

func TestFlowEditPreviewCommit(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("l", "e") // second column, start editing "Ann"
	h.typeText("ie")
	h.press("enter")
	h.want("customers ●")
	h.prefix("P")
	h.want(`UPDATE "public"."customers" SET "name" = 'Annie' WHERE "id" = 1`)
	h.press("esc")
	if len(h.fake.applied) != 0 {
		t.Fatal("preview must not send anything")
	}
	h.prefix("s")
	if len(h.fake.applied) != 1 || !strings.HasPrefix(h.fake.applied[0][0].Text, `UPDATE "public"."customers" SET "name" = $1 WHERE "id" = $2`) {
		t.Fatalf("applied: %+v", h.fake.applied)
	}
	h.want("Annie")
	h.wantNot("customers ●")
	// history records the GUI statement
	hist, _ := h.a.d.Hist.List("shop", "", 0)
	if len(hist) != 1 || hist[0].Source != "gui" || !strings.Contains(hist[0].SQL, "Annie") {
		t.Errorf("history: %+v", hist)
	}
}

func TestFlowDiscardAndUndo(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("l", "e")
	h.typeText("zz")
	h.press("enter")
	h.want("Annzz")
	h.prefix("z")
	h.wantNot("Annzz")
	h.prefix("Z")
	h.want("Annzz")
	h.prefix("backspace")
	h.wantNot("Annzz")
	h.wantNot("customers ●")
}

func TestFlowAddAndDeleteRow(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix("i") // add_row (grid context)
	h.press("l", "e")
	h.typeText("Dee")
	h.press("enter")
	h.press("g", "j", "j") // first row, then down to Cy
	h.prefix("delete")
	h.prefix("s")
	if len(h.fake.applied) != 1 || len(h.fake.applied[0]) != 2 {
		t.Fatalf("applied: %+v", h.fake.applied)
	}
	var kinds []string
	for _, s := range h.fake.applied[0] {
		kinds = append(kinds, strings.Fields(s.Text)[0])
	}
	if strings.Join(kinds, ",") != "DELETE,INSERT" {
		t.Errorf("statements: %v", kinds)
	}
}

func TestFlowSafeModeAlert2(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SafeMode = "alert2" })
	h.connect()
	h.want("Alert Mode 2")
	h.openCustomers()
	h.press("l", "e")
	h.typeText("x")
	h.press("enter")
	h.prefix("s")
	h.want("Alert Mode 2: send to the server?")
	h.want("UPDATE")
	if len(h.fake.applied) != 0 {
		t.Fatal("nothing may be sent before confirming")
	}
	h.press("n")
	if len(h.fake.applied) != 0 || len(h.a.overlays) != 0 {
		t.Fatal("n cancels")
	}
	h.prefix("s")
	h.press("y")
	if len(h.fake.applied) != 1 {
		t.Fatalf("y sends: %+v", h.fake.applied)
	}
}

func TestFlowSafeMode1NeedsPassword(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SafeMode = "safe1" })
	h.connect()
	h.openCustomers()
	h.press("l", "e")
	h.typeText("x")
	h.press("enter")
	h.prefix("s")
	h.want("enter the database password")
	h.typeText("nope")
	h.press("enter")
	h.want("wrong password")
	if len(h.fake.applied) != 0 {
		t.Fatal("wrong password must not send")
	}
	h.press("ctrl+u")
	h.typeText("pw")
	h.press("enter")
	if len(h.fake.applied) != 1 {
		t.Fatalf("correct password sends: %+v", h.fake.applied)
	}
	h.wantNot("pw") // the password is never echoed
}

func TestFlowSafeModeReadOnlyPassesAlert2(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SafeMode = "alert2" })
	h.connect()
	h.prefix("e")
	h.typeText("select 1")
	h.prefix("alt+enter")
	h.want("2 rows")
	if len(h.a.overlays) != 0 {
		t.Fatal("SELECT must not prompt in Alert Mode 2")
	}
	h.press("ctrl+a")
	h.typeText("delete from t")
	h.prefix("alt+enter")
	h.want("Alert Mode 2: send to the server?")
}

func TestFlowRebindFromConfig(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) {
		c.KeyOverrides = map[string][]string{"commit": {"x"}}
		c.General.Prefix = "C-a"
	})
	h.connect()
	h.openCustomers()
	h.press("l", "e")
	h.typeText("q")
	h.press("enter")
	h.press("ctrl+d", "x") // old prefix does nothing
	if len(h.fake.applied) != 0 {
		t.Fatal("old prefix must not work")
	}
	h.press("ctrl+a", "x")
	if len(h.fake.applied) != 1 {
		t.Fatalf("rebound commit: %+v", h.fake.applied)
	}
}

func TestFlowQueryRunAndCancel(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select 1; boom; select 2")
	h.prefix("alt+enter")
	h.want("Query 1")
	h.want("✗ Query 2")
	h.want("Query 3")
	h.want("syntax error near boom") // errors do not stop the batch by default
	if len(h.fake.execs) != 3 {
		t.Errorf("execs %v", h.fake.execs)
	}
	// ReturnOnError stops at the first failure.
	h2 := newHarness(t, func(_ *config.Connection, c *config.Config) { c.Editor.ReturnOnError = true })
	h2.connect()
	h2.prefix("e")
	h2.typeText("select 1; boom; select 2")
	h2.prefix("alt+enter")
	if len(h2.fake.execs) != 2 {
		t.Errorf("return on error: %v", h2.fake.execs)
	}
	// Run current picks the statement under the cursor.
	h3 := newHarness(t, nil)
	h3.connect()
	h3.prefix("e")
	h3.typeText("select 1; select 22")
	h3.prefix("enter")
	if len(h3.fake.execs) != 1 || h3.fake.execs[0] != "select 22" {
		t.Errorf("run current: %v", h3.fake.execs)
	}
}

func TestFlowQueryCancel(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select sleep")
	tab := h.a.ws().tab().(*queryTab)
	_, cmd := h.a.Update(keyMsg("ctrl+d"))
	_, cmd = h.a.Update(keyMsg("alt+enter"))
	done := make(chan struct{})
	go func() { cmd(); close(done) }()
	for i := 0; i < 100 && !tab.running; i++ {
		_ = i
	}
	if !tab.running {
		t.Fatal("tab should be running")
	}
	h.prefix(".") // cancel_query in the editor context
	<-done
	if tab.running {
		t.Error("still running after cancel")
	}
	h.want("cancelled")
}

func TestFlowWelcomeCreatesConnection(t *testing.T) {
	h := newHarness(t, nil)
	h.press("n")
	h.want("New connection")
	h.typeText("analytics")
	h.press("tab", "right") // driver: postgres -> mysql
	h.press("enter")
	if len(h.a.d.Conns) != 2 {
		t.Fatalf("conns: %+v\n%s", h.a.d.Conns, h.screen())
	}
	c := h.a.d.Conns[1]
	if c.Name != "analytics" || c.Driver != config.MySQL || c.Host != "127.0.0.1" {
		t.Errorf("%+v", c)
	}
	saved, err := config.LoadConnections(h.a.d.ConnPath)
	if err != nil || len(saved) != 2 {
		t.Fatalf("saved: %+v %v", saved, err)
	}
	if st, _ := os.Stat(h.a.d.ConnPath); st.Mode().Perm() != 0o600 {
		t.Errorf("perms %v", st.Mode().Perm())
	}
	// duplicate names are rejected inside the form
	h.press("n")
	h.typeText("analytics")
	h.press("enter")
	h.want("already exists")
}

func TestFlowOpenAnythingAndHelp(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("p")
	h.want("Open anything")
	h.typeText("rec")
	h.press("enter")
	if h.a.ws().tab() == nil || h.a.ws().tab().Title() != "recent" {
		t.Fatalf("opened %v\n%s", h.a.ws().tab(), h.screen())
	}
	h.prefix("?")
	h.want("Key bindings")
	h.want("C-d")
	h.press("esc")
}

func TestFlowUnknownKeyAfterPrefixAndPrefixTwice(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.press("ctrl+d")
	h.want("PREFIX")
	h.want("press a key")
	h.press("~")
	h.wantNot("PREFIX")
	h.want("no binding")
	h.press("ctrl+d", "esc")
	h.wantNot("PREFIX")
}

func TestFlowQuitWithPendingChangesAsks(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("l", "e")
	h.typeText("x")
	h.press("enter")
	h.prefix("q")
	h.want("Quit with uncommitted changes?")
	h.press("n")
	if h.a.quit {
		t.Fatal("n keeps running")
	}
	h.prefix("q")
	h.press("y")
	if !h.a.quit {
		t.Fatal("y quits")
	}
}

func TestFlowFilterBar(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix("f")
	h.want("Filters (AND)")
	h.typeText("2")
	h.press("enter")
	q := h.fake.lastQuery
	if len(q.Filters.Active()) != 1 || q.Filters.Filters[0].Column != "id" || q.Filters.Filters[0].Op != "=" || q.Filters.Filters[0].Value != "2" {
		t.Fatalf("query filters: %+v", q.Filters)
	}
	h.want("1 filter(s)")
	// add a second filter, switch to OR, then clear everything
	h.press("alt+n")
	h.press("alt+o")
	h.want("Filters (OR)")
	h.press("alt+c")
	if len(h.fake.lastQuery.Filters.Filters) != 0 {
		t.Errorf("cleared: %+v", h.fake.lastQuery.Filters)
	}
	h.wantNot("Filters (")
	// filters persist per table in state.json
	h.prefix("f")
	h.typeText("7")
	h.press("enter")
	ts := h.a.d.State.Table(h.a.ws().tab().(*tableTab).stateKey())
	if len(ts.Filters.Filters) != 1 || ts.Filters.Filters[0].Value != "7" {
		t.Errorf("state: %+v", ts)
	}
}

func TestFlowFilterEscHidesButKeepsFilter(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix("f")
	h.typeText("1")
	h.press("enter")
	h.press("esc")
	h.wantNot("Filters (")
	if n := len(h.a.ws().tab().(*tableTab).filters.Active()); n != 1 {
		t.Errorf("filter should stay applied, got %d active", n)
	}
	h.want("1 filter(s)")
}

func TestFlowQuickFilter(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("j", "l") // Bob's name
	h.prefix("x")
	h.want("Quick filter: name")
	h.press("down", "enter") // "<> Bob"
	f := h.fake.lastQuery.Filters.Filters
	if len(f) != 1 || f[0].Column != "name" || f[0].Op != "<>" || f[0].Value != "Bob" {
		t.Fatalf("%+v", f)
	}
}

func TestFlowColumnsPicker(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix("alt+f")
	h.want("Visible columns")
	h.press("down", "space", "enter") // hide "name"
	cols := h.fake.lastQuery.Columns
	if strings.Join(cols, ",") != "id,email" {
		t.Fatalf("visible columns sent to the driver: %v", cols)
	}
	h.want("1 hidden col(s)")
	h.prefix("alt+f")
	h.press("a", "enter") // select all again
	if len(h.fake.lastQuery.Columns) != 0 {
		t.Errorf("all visible means no projection: %v", h.fake.lastQuery.Columns)
	}
}

func TestFlowSortAndPaging(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.General.RowLimit = 2 })
	h.connect()
	h.openCustomers()
	h.want("rows 1–2 of 3")
	h.prefix("right") // next_page
	if h.fake.lastQuery.Offset != 2 {
		t.Fatalf("offset %d", h.fake.lastQuery.Offset)
	}
	h.want("rows 3–3 of 3")
	h.prefix("left")
	if h.fake.lastQuery.Offset != 0 {
		t.Errorf("offset %d", h.fake.lastQuery.Offset)
	}
	h.press("l", "s") // sort by name asc
	if s := h.fake.lastQuery.Sort; len(s) != 1 || s[0].Column != "name" || s[0].Desc {
		t.Errorf("sort %+v", s)
	}
	h.press("s")
	if s := h.fake.lastQuery.Sort; len(s) != 1 || !s[0].Desc {
		t.Errorf("sort desc %+v", s)
	}
	h.press("s")
	if len(h.fake.lastQuery.Sort) != 0 {
		t.Errorf("third press clears: %+v", h.fake.lastQuery.Sort)
	}
}

func TestFlowStructureEditCommit(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix(")") // show_structure
	h.want("column_name")
	h.want("data_type")
	h.want("email")
	// rename "name" -> "full_name": row 2, column_name cell
	h.press("j", "l", "enter")
	h.press("ctrl+u")
	h.typeText("full_name")
	h.press("enter")
	h.want("customers ●")
	// toggle nullability of email (row 3): move to is_nullable and press Enter
	h.press("j", "l", "l", "enter")
	// add a new column
	h.prefix("i")
	h.prefix("s")
	if len(h.fake.applied) != 1 || h.fake.applied[0][0].Text != "ALTER TABLE customers" {
		t.Fatalf("applied: %+v\n%s", h.fake.applied, h.screen())
	}
	old, nw := h.fake.lastOld, h.fake.lastNew
	if old == nil || len(old.Columns) != 3 || old.Columns[1].OrigName != "name" {
		t.Fatalf("old def: %+v", old)
	}
	if len(nw.Columns) != 4 {
		t.Fatalf("new def should have the added column: %+v", nw.Columns)
	}
	if nw.Columns[1].Name != "full_name" || nw.Columns[1].OrigName != "name" {
		t.Errorf("rename must keep OrigName: %+v", nw.Columns[1])
	}
	if nw.Columns[2].Nullable != !old.Columns[2].Nullable {
		t.Errorf("nullable toggle: old %v new %v", old.Columns[2].Nullable, nw.Columns[2].Nullable)
	}
	if nw.Columns[3].OrigName != "" || nw.Columns[3].Name != "new_column" {
		t.Errorf("added column: %+v", nw.Columns[3])
	}
	if strings.Join(nw.PrimaryKey, ",") != "id" {
		t.Errorf("pk: %v", nw.PrimaryKey)
	}
}

func TestFlowStructureValidatesBeforePlanning(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix(")")
	h.press("j", "l", "enter", "ctrl+u", "enter") // empty column name
	h.prefix("s")
	h.want("every column needs a name")
	if h.fake.lastNew != nil {
		t.Error("PlanTable must not be called with an invalid definition")
	}
}

func TestFlowNewTable(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.press("N")
	h.want("New table")
	h.typeText("widgets")
	h.press("enter")
	h.want("new table widgets")
	h.prefix("s")
	if h.fake.lastOld != nil || h.fake.lastNew == nil || h.fake.lastNew.Name != "widgets" {
		t.Fatalf("old=%v new=%+v", h.fake.lastOld, h.fake.lastNew)
	}
	c := h.fake.lastNew.Columns
	if len(c) != 1 || c[0].Name != "id" || strings.Join(h.fake.lastNew.PrimaryKey, ",") != "id" {
		t.Errorf("new table starts with an id primary key: %+v pk=%v", c, h.fake.lastNew.PrimaryKey)
	}
	if len(h.fake.applied) != 1 {
		t.Errorf("applied %+v", h.fake.applied)
	}
}

func TestFlowIndexEdit(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix("alt+i") // show_index
	h.prefix("i")     // add an index row
	h.press("enter")  // edit the index name
	h.press("ctrl+u")
	h.typeText("ix_email")
	h.press("enter", "l", "l", "enter")
	h.typeText("email")
	h.press("enter")
	h.prefix("s")
	nw := h.fake.lastNew
	if nw == nil || len(nw.Indexes) != 1 || nw.Indexes[0].Name != "ix_email" || strings.Join(nw.Indexes[0].Columns, ",") != "email" {
		t.Fatalf("%+v\n%s", nw, h.screen())
	}
}

func TestFlowCloseDirtyTabAsks(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("l", "e")
	h.typeText("x")
	h.press("enter")
	h.prefix("w")
	h.want("Close customers with unsaved changes?")
	h.press("n")
	if len(h.a.ws().tabs) != 1 {
		t.Fatal("n keeps the tab")
	}
	h.prefix("w")
	h.press("y")
	if len(h.a.ws().tabs) != 0 {
		t.Fatal("y closes the tab")
	}
}

func TestFlowDatabaseManagement(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("k") // open_database
	h.want("Open database")
	h.want("New database…")
	h.press("enter") // the first row is "New database…"
	h.want("New database")
	h.typeText("analytics")
	h.press("enter")
	if len(h.fake.applied) != 1 || h.fake.applied[0][0].Text != "-- create_database analytics" {
		t.Fatalf("applied: %+v\n%s", h.fake.applied, h.screen())
	}
	// the list is shown again after a change; drop needs the name typed back
	h.want("Open database")
	h.press("down", "down", "alt+enter") // "other" -> menu
	h.press("down", "down", "enter")     // Drop…
	h.want("Drop other?")
	h.typeText("oops")
	h.press("enter")
	h.want(`type "other" to confirm`)
	h.press("ctrl+u")
	h.typeText("other")
	h.press("enter")
	if len(h.fake.applied) != 2 || h.fake.applied[1][0].Text != "-- drop_database other" {
		t.Fatalf("applied: %+v", h.fake.applied)
	}
	h.want("Open database") // refreshed again; the open database is protected
	h.press("down", "alt+enter", "down", "down", "enter")
	h.want("cannot be dropped")
}

// ---- regressions from the independent review ------------------------------------------------

func TestFlowEditingNullCellKeepsNull(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("j", "l", "l", "enter", "ctrl+s") // Bob's NULL email: open the popover and save it untouched
	h.wantNot("customers ●")
	h.prefix("s")
	h.want("no pending changes")
	if len(h.fake.applied) != 0 {
		t.Fatalf("an untouched edit must not write: %+v", h.fake.applied)
	}
}

func TestFlowHiddenKeyColumnMakesGridReadOnly(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix("alt+f")
	h.press("space", "enter") // hide "id", the primary key
	h.press("enter")          // editing is refused; a read-only cell opens Quick Look instead
	h.want("Quick look")
	h.press("esc")
	h.wantNot("customers ●")
	h.prefix("i")
	h.want("read-only")
	if len(h.a.ws().tab().(*tableTab).grid.Rows) != 3 {
		t.Error("no row may be added to a grid without its key")
	}
}

func TestFlowPendingEditsAskBeforeBeingDropped(t *testing.T) {
	for _, tc := range []struct {
		name string
		do   func(h *harness)
	}{
		{"sort", func(h *harness) { h.press("s") }},
		{"next page", func(h *harness) { h.prefix("right") }},
		{"filter", func(h *harness) { h.prefix("f"); h.typeText("1"); h.press("enter") }},
		{"columns", func(h *harness) { h.prefix("alt+f"); h.press("down", "space", "enter") }},
	} {
		h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.General.RowLimit = 2 })
		h.connect()
		h.openCustomers()
		h.press("l", "e")
		h.typeText("Q")
		h.press("enter")
		tc.do(h)
		h.want("Discard pending changes?")
		h.press("n")
		if !h.a.ws().tab().Dirty() {
			t.Errorf("%s: n must keep the edit", tc.name)
		}
		tc.do(h)
		h.press("y")
		if h.a.ws().tab().Dirty() {
			t.Errorf("%s: y discards", tc.name)
		}
	}
}

func TestFlowSwitchingDatabaseWithUnsavedChangesAsks(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("l", "e")
	h.typeText("Q")
	h.press("enter")
	h.a.ws().openDatabase("other")
	h.pump(nil)
	h.want("closes all tabs")
	h.press("n")
	if len(h.a.ws().tabs) != 1 || h.fake.curDB == "other" {
		t.Fatal("n must keep the tab and the database")
	}
}

func TestFlowNoSecondCommitOrEditWhileApplying(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("l", "e")
	h.typeText("Q")
	h.press("enter")
	h.a.ws().applying = true // a commit is in flight
	h.prefix("s")
	h.want("still being sent")
	h.prefix("i")
	h.press("enter")
	if len(h.a.ws().tab().(*tableTab).grid.Rows) != 3 || len(h.fake.applied) != 0 {
		t.Fatal("nothing may change while statements are in flight")
	}
	h.a.ws().applying = false
	h.prefix("s")
	if len(h.fake.applied) != 1 {
		t.Fatalf("once idle the commit goes through exactly once: %+v", h.fake.applied)
	}
}

func TestFlowReconnectComesBackToCurrentDatabase(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.a.ws().openDatabase("other")
	h.pump(h.a.ws().openDatabase("other"))
	h.prefix("R")
	h.press("y")
	last := h.connected[len(h.connected)-1]
	if last.Database != "other" {
		t.Fatalf("reconnect used database %q, want the open one", last.Database)
	}
}

func TestFlowPrefixActionWhileEditingSavesTheEditFirst(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("l", "e")
	h.typeText("Zed") // editor still open on Ann's name
	h.prefix("i")     // add_row
	g := h.a.ws().tab().(*tableTab).grid
	if g.Editing() {
		t.Error("the editor must have closed")
	}
	if got := g.Rows[0].Cur[1]; got != "AnnZed" {
		t.Errorf("Ann's row keeps the edit: %v", got)
	}
	if g.Rows[1].State != rowNew || g.Rows[1].Cur[1] != "DEFAULT" && g.Rows[1].Cur[1] == "AnnZed" {
		t.Errorf("the new row must not receive the half-typed text: %+v", g.Rows[1].Cur)
	}
}

func TestFlowStructureViewBeforeLoadDoesNotPanic(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	tab := h.a.ws().tab().(*tableTab)
	tab.view, tab.structGrid, tab.idxGrid, tab.structLoaded = viewStructure, nil, nil, false
	for _, a := range []string{"add_row", "delete_rows", "set_value"} {
		tab.Action(a)
	}
	tab.Do("undo")
	tab.Do("redo")
	tab.Input(keyMsg("enter"))
	tab.Input(keyMsg("i"))
	_ = tab.View(NewTheme(configTheme()), 80, 20)
	h.prefix("alt+r") // reload_tab in a structure view reloads the structure
	if !tab.structLoaded || tab.structGrid == nil {
		t.Errorf("reload in the structure view must load the structure: loaded=%v", tab.structLoaded)
	}
}

func TestFlowClosingQueryTabCancelsStatement(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select sleep")
	tab := h.a.ws().tab().(*queryTab)
	_, cmd := h.a.Update(keyMsg("ctrl+d"))
	_, cmd = h.a.Update(keyMsg("alt+enter"))
	done := make(chan struct{})
	go func() { cmd(); close(done) }()
	for i := 0; i < 1000 && !tab.running; i++ {
		time.Sleep(time.Millisecond)
	}
	h.a.ws().closeTab(h.a.ws().cur)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("closing the tab must cancel the running statement")
	}
}

func TestFlowSafeModeChoiceIsSavedWithTheConnection(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("S")
	h.press("down", "enter") // Alert Mode 1
	h.want("Alert Mode 1")
	saved, err := config.LoadConnections(h.a.d.ConnPath)
	if err != nil || len(saved) != 1 || saved[0].SafeMode != "alert1" {
		t.Fatalf("saved: %+v %v", saved, err)
	}
}

func TestFlowKeylessTableWarnsBeforeUpdate(t *testing.T) {
	h := newHarness(t, nil)
	noPK := make([]db.Column, len(customerCols))
	copy(noPK, customerCols)
	for i := range noPK {
		noPK[i].PK = false
	}
	saved := customerCols
	customerCols = noPK
	defer func() { customerCols = saved }()
	h.connect()
	h.openCustomers()
	h.press("l", "e")
	h.typeText("Q")
	h.press("enter")
	h.prefix("s")
	h.want("has no primary key")
	if len(h.fake.applied) != 0 {
		t.Fatal("nothing is sent before the warning is accepted")
	}
	h.press("n")
	h.prefix("s")
	h.press("y")
	if len(h.fake.applied) != 1 {
		t.Fatalf("accepting sends it: %+v", h.fake.applied)
	}
}

// A saved filter that makes the load fail must stay visible and clearable, not trap the user on an error.
func TestFlowFailingFilterStaysVisibleAndClearable(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.failRows = errors.New(`invalid input syntax for type integer: "Ann"`)
	h.connect()
	h.openCustomers()
	h.prefix("f")
	h.typeText("Ann")
	h.press("enter")
	h.want("invalid input syntax")
	h.want("Filters (AND)") // the bar is shown even though the load failed
	h.want("clear all (in the bar)")
	// the structure view is not blocked by the failed row load
	h.prefix(")")
	h.wantNot("invalid input syntax")
	h.want("column_name")
	h.prefix("(")
	h.press("alt+c") // clear all filters
	h.want("ann@x.io")
	h.wantNot("invalid input syntax")
}

// ---- speed: parallel loads and the metadata cache ---------------------------------------

func TestFlowColumnMetadataIsCachedPerWorkspace(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	if n := h.fake.colCalls.Load(); n != 1 {
		t.Fatalf("opening a table fetches columns once, got %d", n)
	}
	h.prefix(")") // structure reuses the cached columns
	h.want("column_name")
	h.prefix("(")
	h.prefix("w") // close, then reopen
	h.prefix("alt+h")
	h.press("enter")
	h.want("ann@x.io")
	if n := h.fake.colCalls.Load(); n != 1 {
		t.Errorf("structure view and reopening must reuse cached columns, got %d fetches", n)
	}
	h.prefix("r") // reload drops the cache
	h.prefix("alt+h")
	h.press("enter")
	if n := h.fake.colCalls.Load(); n < 2 {
		t.Errorf("reload must refetch metadata, got %d fetches", n)
	}
}

func TestFlowRowsBeforeColumnsStayReadOnlyUntilKeyKnown(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	tab := newTableTab(h.a.ws(), 99, db.ObjRef{Schema: "public", Name: "customers", Kind: db.KindTable})
	h.a.ws().tabs = append(h.a.ws().tabs, tab)
	tab.seq = 1
	res, _ := h.fake.Rows(nil, db.RowQuery{})
	tab.Handle(tableLoadedMsg{seq: 1, res: res}) // rows first
	if tab.grid.Editable || tab.grid.Cols[0].PK {
		t.Fatal("without column metadata the key is unknown: read-only, no key icon")
	}
	tab.Handle(colsMetaMsg{ref: tab.ref, cols: customerCols, seq: 1})
	if !tab.grid.Editable || !tab.grid.Cols[0].PK {
		t.Errorf("once the columns arrive the grid becomes editable with its key: editable=%v pk=%v", tab.grid.Editable, tab.grid.Cols[0].PK)
	}
	if len(tab.grid.Rows) != 3 {
		t.Error("applying metadata must not drop the loaded rows")
	}
}

func TestFlowHiddenColumnsWaitForMetadataOnColdOpen(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	ref := db.ObjRef{Schema: "public", Name: "customers", Kind: db.KindTable}
	// a previous session hid "email"
	tab := newTableTab(h.a.ws(), 98, ref)
	tab.hidden = map[string]bool{"email": true}
	tab.saveState()
	h.press("enter") // open customers cold: no cached columns
	if got := strings.Join(h.fake.lastQuery.Columns, ","); got != "id,name" {
		t.Fatalf("the projection must use the real column list: %q", got)
	}
}

func TestFlowSidebarPrefetchFillsCache(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	ws := h.a.ws()
	ref := db.ObjRef{Schema: "public", Name: "orders", Kind: db.KindTable}
	ws.sb.prefetchSeq = 7
	h.pump(ws.handle(wsMsg{ws: ws.id, tab: -1, msg: prefetchMsg{seq: 6, ref: ref}}))
	if _, ok := ws.meta[ref]; ok {
		t.Fatal("a stale prefetch (cursor moved on) must not fetch")
	}
	h.pump(ws.handle(wsMsg{ws: ws.id, tab: -1, msg: prefetchMsg{seq: 7, ref: ref}}))
	if _, ok := ws.meta[ref]; !ok {
		t.Fatal("prefetch must fill the metadata cache")
	}
}

func TestFlowHelpSearch(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix("?")
	h.want("Key bindings")
	h.want("Grid")
	h.want("Global")
	h.press("/")
	h.typeText("comit") // fuzzy: typo still finds it
	h.want("commit changes")
	h.wantNot("open anything")
	h.press("ctrl+u")
	h.typeText("M-f") // search by key
	h.want("choose visible columns")
	h.press("esc") // clears the search: the full list is back
	d := h.a.overlays[len(h.a.overlays)-1].(*helpDlg)
	if len(d.shown()) != len(d.rows) {
		t.Fatalf("clearing the search shows everything again: %d of %d", len(d.shown()), len(d.rows))
	}
	if d.rows[0].header != "Grid" || d.rows[1].ctx != "grid" {
		t.Errorf("the focused pane's own bindings come first: %+v %+v", d.rows[0], d.rows[1])
	}
	h.press("esc", "/")
	h.typeText("filters")
	h.press("enter") // runs the selected command
	if len(h.a.overlays) != 0 {
		t.Fatal("running a command closes the help")
	}
	h.want("Filters (AND)")
	h.prefix("?")
	h.press("esc")
	if len(h.a.overlays) != 0 {
		t.Error("esc closes the help when not searching")
	}
}

// ---- the last M1 items ------------------------------------------------------------------

func TestFlowExactCountAndPageSettings(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix("L")
	h.want("Rows per page and offset")
	h.press("ctrl+u")
	h.typeText("2")
	h.press("tab", "ctrl+u")
	h.typeText("1")
	h.press("enter")
	if q := h.fake.lastQuery; q.Limit != 2 || q.Offset != 1 {
		t.Fatalf("limit/offset sent to the driver: %+v", q)
	}
	h.want("rows 2–3 of")
	h.prefix("L")
	h.press("ctrl+u")
	h.typeText("0")
	h.press("enter")
	h.want("limit must be 1 or more")
	h.press("esc")
	h.prefix("#") // count_exact
	h.want("already exact")
}

func TestFlowTestConnectionFromTheForm(t *testing.T) {
	h := newHarness(t, nil)
	h.press("n")
	h.typeText("probe")
	h.press("ctrl+t")
	h.want("connection works (Fake 1.0)")
	if len(h.a.d.Conns) != 1 {
		t.Error("testing must not save the connection")
	}
	last := h.connected[len(h.connected)-1]
	if last.Name != "probe" || last.Driver != config.Postgres {
		t.Errorf("tested with %+v", last)
	}
	h.press("esc")
	h.press("n")
	h.press("ctrl+t") // no name: the same validation as saving
	h.want("needs a name")
}

func TestFlowReopensWorkspacesOnStart(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.a.ws().openDatabase("other")
	h.pump(h.a.ws().openDatabase("other"))
	h.prefix("q") // quit saves the session
	if got := h.a.d.State.LastSession(); len(got) != 1 || got[0].Connection != "shop" || got[0].Database != "other" {
		t.Fatalf("saved session: %+v", got)
	}
	h2 := newHarness(t, nil)
	h2.a.d.State.SetLastSession(h.a.d.State.LastSession())
	h2.connected = nil
	h2.pump(h2.a.Init())
	if h2.a.ws() == nil || len(h2.connected) != 1 || h2.connected[0].Database != "other" {
		t.Fatalf("reopen: ws=%v connected=%+v", h2.a.ws(), h2.connected)
	}
	h3 := newHarness(t, func(_ *config.Connection, c *config.Config) { c.General.ReopenWorkspaces = false })
	h3.a.d.State.SetLastSession(h.a.d.State.LastSession())
	h3.pump(h3.a.Init())
	if h3.a.ws() != nil {
		t.Error("reopen_workspaces = false must start on the connection list")
	}
}

func TestFlowQueryTimeoutOffersCancel(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select sleep")
	tab := h.a.ws().tab().(*queryTab)
	_, cmd := h.a.Update(keyMsg("ctrl+d"))
	_, cmd = h.a.Update(keyMsg("alt+enter"))
	done := make(chan struct{})
	go func() { cmd(); close(done) }()
	for i := 0; i < 1000 && !tab.running; i++ {
		time.Sleep(time.Millisecond)
	}
	h.pump(tab.Handle(queryTimeoutMsg{batch: tab.batch}))
	h.want("The server is taking too long")
	h.press("down", "enter") // Cancel the query
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelling from the prompt must stop the statement")
	}
	if tab.running {
		t.Error("still running")
	}
	// a stale timer (the batch already finished) must stay silent
	h.pump(tab.Handle(queryTimeoutMsg{batch: tab.batch - 5}))
	h.wantNot("taking too long")
}

func TestFlowKeepAliveReportsLostConnectionOnce(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	ws := h.a.ws()
	fail := errors.New("broken pipe")
	cmd, _ := ws.handleGlobal(pingMsg{err: fail})
	h.pump(cmd)
	h.want("lost connection to shop")
	h.a.toast = ""
	if cmd, _ := ws.handleGlobal(pingMsg{err: fail}); cmd != nil {
		t.Error("the loss is announced once, not every 30 seconds")
	}
	cmd, _ = ws.handleGlobal(pingMsg{})
	h.pump(cmd)
	h.want("reachable again")
}

func TestFlowPasteRowsAndCells(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix("v") // paste_rows waits for the clipboard
	h.a.Update(tea.ClipboardMsg{Content: "7,Dee,dee@x.io\n8,Eve,NULL"})
	g := h.a.ws().tab().(*tableTab).grid
	if len(g.Rows) != 5 || g.Rows[1].Cur[1] != "Dee" || g.Rows[2].Cur[2] != "NULL" {
		t.Fatalf("rows pasted after the cursor: %d", len(g.Rows))
	}
	h.want("customers ●")
	h.prefix("backspace")
	h.press("g")
	h.press("l")
	h.prefix("V")
	h.a.Update(tea.ClipboardMsg{Content: "Zoe"})
	if g.Rows[0].Cur[1] != "Zoe" {
		t.Errorf("cell pasted at the cursor: %v", g.Rows[0].Cur[1])
	}
	h.prefix("V")
	h.a.Update(tea.ClipboardMsg{Content: ""})
	h.want("nothing pasted")
}

func TestFlowFindInGridAndPinResult(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("/")
	h.typeText("cy")
	h.press("enter")
	if g := h.a.ws().tab().(*tableTab).grid; g.row != 2 {
		t.Fatalf("found Cy on row %d", g.row)
	}
	h.press("N")
	if g := h.a.ws().tab().(*tableTab).grid; g.row != 2 { // the only match: wraps to itself
		t.Error("N wraps")
	}
	h.press("/")
	h.typeText("zzz")
	h.press("enter")
	h.want("not in the loaded rows")

	h.prefix("e")
	h.typeText("select 1")
	h.prefix("alt+enter")
	h.prefix("alt+j") // focus results
	h.press("p")
	h.want("pinned")
	h.want("◆ Query 1")
	h.prefix("alt+k")
	h.press("ctrl+a")
	h.typeText("select 2")
	h.prefix("alt+enter")
	h.want("◆ Query 1")
	h.want("Query 2")
}

func TestFlowEditFunctionAndTriggerDefinitions(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	// the function lives in the sidebar: Enter opens its definition as editable SQL
	h.press("/")
	h.typeText("order_count")
	h.press("enter", "enter")
	tab, ok := h.a.ws().tab().(*queryTab)
	if !ok || !strings.Contains(tab.ed.Text(), "CREATE OR REPLACE FUNCTION order_count()") || !strings.Contains(tab.ed.Text(), "Edit it, then run all") {
		t.Fatalf("function definition tab: %v", h.screen())
	}
	if tab.Title() != "order_count" {
		t.Errorf("title %q", tab.Title())
	}
	// triggers of a table: pick one, get DROP + CREATE; or start a new one
	h.press("ctrl+h")
	h.press("esc", "/")
	h.typeText("customers")
	h.press("enter", "enter")
	h.prefix("alt+t")
	h.want("Triggers on customers")
	h.want("New trigger")
	h.press("down", "enter")
	trig := h.a.ws().tab().(*queryTab)
	if !strings.Contains(trig.ed.Text(), `DROP TRIGGER IF EXISTS "audit" ON "public"."customers";`) || !strings.Contains(trig.ed.Text(), "EXECUTE FUNCTION log_order()") {
		t.Errorf("trigger text: %q", trig.ed.Text())
	}
	h.prefix("2") // back to the customers tab
	h.prefix("alt+t")
	h.press("enter") // New trigger…
	if nt := h.a.ws().tab().(*queryTab); !strings.Contains(nt.ed.Text(), "CREATE TRIGGER my_trigger") {
		t.Errorf("template: %q", nt.ed.Text())
	}
	h.prefix("alt+t") // from a SQL tab there is no table
	h.want("open a table first")
}

func TestFlowQuickLookOpensJSONTree(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	g := h.a.ws().tab().(*tableTab).grid
	g.Rows[0].Cur[2] = `{"a": [1, 2], "b": {"c": null}}`
	g.GoCol(2, false)
	h.prefix("enter") // quick_look
	h.want("Quick look: email")
	h.want("{2 fields}")
	h.press("r")
	h.want(`"a": [`)
}

func TestFlowEditAndCommitAQueryResult(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select * from customers")
	h.prefix("alt+enter")
	h.want("Ann")
	h.prefix("alt+j") // focus the results
	h.want("edit")    // the hint says the result is editable
	h.press("l", "e")
	h.typeText("Q")
	h.press("enter")
	h.want("SQL 1 ●") // the tab shows pending changes
	h.prefix("P")
	h.want(`UPDATE "public"."customers" SET "name" = 'AnnQ' WHERE "id" = 1`)
	h.press("esc")
	h.prefix("s")
	if len(h.fake.applied) != 1 {
		t.Fatalf("applied: %+v", h.fake.applied)
	}
	h.want("AnnQ") // the query was run again
	h.wantNot("SQL 1 ●")
	// add a row and discard
	h.prefix("i")
	h.want("SQL 1 ●")
	h.prefix("backspace")
	h.wantNot("SQL 1 ●")
}

func TestFlowQueryResultStaysReadOnlyWithoutItsKey(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select name from customers") // the primary key is not selected
	h.prefix("alt+enter")
	h.prefix("alt+j")
	h.press("enter") // quick look instead of an editor
	h.want("Quick look")
	h.press("esc")
	h.prefix("i")
	h.want("is not available here") // add_row does nothing on a read-only result
}

func TestFlowDefaultSafeModeAppliesToConnectionsWithoutOne(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, cfg *config.Config) {
		c.SafeMode = ""
		cfg.General.DefaultSafeMode = "alert2"
	})
	h.connect()
	h.want("Alert Mode 2")
	h2 := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SafeMode = "Safe-2" })
	h2.connect()
	h2.want("Safe Mode 2") // an invalid level fails closed
}

func TestFlowInterruptSavesSessionAndCloses(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.a.Update(tea.InterruptMsg{})
	if !h.a.quit || len(h.a.d.State.LastSession()) != 1 {
		t.Fatal("SIGINT/Ctrl+C must save the session and tear down")
	}
}

// ---- second review round ------------------------------------------------------------------

func TestReviewStaleGenerationMessagesAreDropped(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	ws := h.a.ws()
	ref := db.ObjRef{Schema: "public", Name: "customers", Kind: db.KindTable}
	old := ws.gen
	ws.forget(nil) // what a database switch or reconnect does
	ws.handle(wsMsg{ws: ws.id, tab: -1, gen: old, msg: colsMetaMsg{ref: ref, cols: customerCols}})
	if _, ok := ws.meta[ref]; ok {
		t.Fatal("metadata from a previous session must not enter the cache")
	}
	ws.handle(wsMsg{ws: ws.id, tab: -1, gen: ws.gen, msg: colsMetaMsg{ref: ref, cols: customerCols}})
	if _, ok := ws.meta[ref]; !ok {
		t.Fatal("current-generation metadata is cached")
	}
}

func TestReviewLoadsAreCancelledAndTabsClose(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	tab := h.a.ws().tab().(*tableTab)
	h.prefix("alt+r")
	if len(tab.cancels) == 0 {
		t.Fatal("loads must be cancellable")
	}
	h.prefix("alt+r") // a second load cancels the first
	h.prefix("w")
	if !tab.closed {
		t.Error("closing the tab marks it closed so late callbacks are no-ops")
	}
}

func TestReviewCommitTwiceSendsOnce(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("l", "e")
	h.typeText("Q")
	h.press("enter")
	tab := h.a.ws().tab().(*tableTab)
	_, cmd := h.a.Update(keyMsg("ctrl+d"))
	_, cmd = h.a.Update(keyMsg("s")) // commit sent, nothing landed yet
	if !h.a.ws().applying {
		t.Fatal("applying flag")
	}
	h.prefix("s")
	h.want("still being sent")
	h.pump(cmd)
	if len(h.fake.applied) != 1 {
		t.Fatalf("applied %d times", len(h.fake.applied))
	}
	if tab.grid.HasPending() {
		t.Error("committed rows are no longer pending")
	}
}

func TestReviewGridReadOnlyWhileLoading(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	tab := h.a.ws().tab().(*tableTab)
	tab.load() // not pumped: still loading
	if tab.grid.Editable {
		t.Fatal("edits typed during a reload would be thrown away: the grid is read-only until it lands")
	}
}

func TestReviewRunBlockedWhileApplyingAndCancelFromSidebar(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select 1")
	h.a.ws().applying = true
	h.prefix("alt+enter")
	h.want("still being sent")
	h.a.ws().applying = false
	tab := h.a.ws().tab().(*queryTab)
	tab.running = true
	tab.cancel = func() {}
	h.prefix("alt+h") // sidebar focused
	h.prefix(".")
	if tab.running {
		t.Error("cancel must reach the running tab from any pane")
	}
}

func TestReviewResultCommitKeepsOtherResultsAndRerunAsks(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select * from customers; select * from customers")
	h.prefix("alt+enter")
	tab := h.a.ws().tab().(*queryTab)
	if len(tab.res) != 2 {
		t.Fatalf("results %d", len(tab.res))
	}
	h.prefix("alt+j")
	h.press("l", "e")
	h.typeText("Q")
	h.press("enter") // edit in result 2 (focused)
	h.prefix("s")
	if len(h.fake.applied) != 1 || len(tab.res) != 2 {
		t.Fatalf("commit refreshes one result in place: applied=%d results=%d", len(h.fake.applied), len(tab.res))
	}
	h.press("l", "e")
	h.typeText("Z")
	h.press("enter")
	h.prefix("alt+k")
	h.prefix("alt+enter") // re-running with pending result edits asks first
	h.want("Discard pending result edits?")
	h.press("n")
	if !tab.Dirty() {
		t.Error("n keeps the edits")
	}
}

func TestReviewDangerConfirmNeedsY(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("l", "e")
	h.typeText("x")
	h.press("enter")
	h.prefix("w")
	h.want("Close customers with unsaved changes?")
	h.press("enter")
	if len(h.a.ws().tabs) != 1 {
		t.Fatal("Enter on a destructive confirm must cancel")
	}
	h.prefix("w")
	h.press("y")
	if len(h.a.ws().tabs) != 0 {
		t.Fatal("y confirms")
	}
}

func TestReviewDropTableNeedsTheNameTyped(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("delete") // delete_item in the sidebar context
	h.want("Delete customers?")
	h.press("enter")
	h.want(`type "customers" to confirm`)
	if len(h.fake.applied) != 0 {
		t.Fatal("nothing may run before the name is typed")
	}
	h.typeText("customers")
	h.press("enter")
	if len(h.fake.applied) != 1 || h.fake.applied[0][0].Text != "-- drop customers" {
		t.Fatalf("applied %+v", h.fake.applied)
	}
}

func TestReviewAsyncResultsWaitForOpenDialogs(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("?") // a dialog is open
	ws := h.a.ws()
	h.a.Update(wsMsg{ws: ws.id, tab: -1, gen: ws.gen, msg: colsMetaMsg{ref: db.ObjRef{Name: "x"}, cols: customerCols}})
	if _, ok := ws.meta[db.ObjRef{Name: "x"}]; ok {
		t.Fatal("a result must wait while a dialog is open")
	}
	h.press("esc") // closing the dialog replays it
	if _, ok := ws.meta[db.ObjRef{Name: "x"}]; !ok {
		t.Fatal("the deferred result is applied once the dialog closes")
	}
}

func TestReviewPrefixResetWhenDialogOpens(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("ctrl+d") // prefix pending...
	h.a.push(&textViewDlg{title: "async", lines: []string{"x"}})
	h.press("esc")
	h.press("D") // ...must not be read as prefix+D (duplicate rows); a plain D does nothing
	if len(h.a.ws().tab().(*tableTab).grid.Rows) != 3 {
		t.Fatal("a stale prefix survived the dialog")
	}
}

func TestReviewCopyURLLeavesPasswordOut(t *testing.T) {
	h := newHarness(t, nil)
	h.press("u")
	h.want("without the password")
	h.press("U")
	h.want("Copy the URL with the password")
}

func TestReviewSafeMode1WithoutStoredPasswordConfirms(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SafeMode = "safe1"; c.Password = "" })
	h.connect()
	h.openCustomers()
	h.press("l", "e")
	h.typeText("x")
	h.press("enter")
	h.prefix("s")
	h.want("no stored password")
	h.press("enter") // danger: Enter cancels
	if len(h.fake.applied) != 0 {
		t.Fatal("an empty password must not pass Safe Mode")
	}
}

func TestReviewShortTerminalWithConsoleDoesNotPanic(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("ctrl+c")
	for _, hh := range []int{6, 8, 12, 14} {
		h.a.Update(tea.WindowSizeMsg{Width: 60, Height: hh})
		_ = h.a.render()
	}
}

func TestReviewDefaultSortAndStripesSettings(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) {
		c.Table.DefaultSort = "pk_desc"
		c.Table.AlternatingRows = false
		c.Table.FilterState = "show"
	})
	h.connect()
	h.openCustomers()
	if s := h.fake.lastQuery.Sort; len(s) != 1 || s[0].Column != "id" || !s[0].Desc {
		t.Errorf("default_sort = pk_desc: %+v", s)
	}
	h.want("Filters (AND)")
	if !h.a.ws().tab().(*tableTab).grid.NoStripes {
		t.Error("alternating_rows = false")
	}
}

func TestReviewQueryResultsSurviveAReload(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("r") // bumps the workspace generation
	h.prefix("e")
	h.typeText("select 1")
	h.prefix("alt+enter")
	h.want("Query 1") // the result carries the current generation and is not dropped as stale
}

func TestFlowEnterOpensPopoverEditorForJSON(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	g := h.a.ws().tab().(*tableTab).grid
	g.Rows[0].Cur[2] = `{"tier":"gold","tags":["a","b"]}`
	g.GoCol(2, false)
	h.press("enter")
	h.want("Edit email")
	h.want(`"tier": "gold"`) // pretty-printed for editing
	h.press("ctrl+s")        // unchanged text: nothing becomes dirty
	if g.HasPending() {
		t.Fatal("saving unchanged JSON must not mark the cell")
	}
	h.press("enter")
	h.press("ctrl+a", "x") // replace all with x
	h.press("enter")       // Enter inside the popover is a newline, not save
	h.typeText("y")
	h.press("ctrl+s")
	if got := g.Rows[0].Cur[2]; got != "x\ny" || !g.HasPending() {
		t.Fatalf("saved %q", got)
	}
	h.press("enter")
	h.typeText("zzz")
	h.press("esc")
	if g.Rows[0].Cur[2] != "x\ny" {
		t.Fatal("esc cancels")
	}
	h.press("e") // inline editing is still there
	if !g.Editing() {
		t.Fatal("e edits inline")
	}
	h.press("esc")
}

func TestReviewCloseCancelsEveryLoadAndDDLBatchesAreNotCalledAtomic(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	tab := h.a.ws().tab().(*tableTab)
	tab.cols = nil
	delete(h.a.ws().meta, tab.ref)
	tab.load() // rows, count and columns are all in flight
	if n := len(tab.cancels); n != 3 {
		t.Fatalf("every load must be cancellable, got %d cancel funcs", n)
	}
	called := 0
	for i := range tab.cancels {
		orig := tab.cancels[i]
		tab.cancels[i] = func() { called++; orig() }
	}
	tab.Close()
	if called != 3 {
		t.Fatalf("Close cancelled %d of 3 loads", called)
	}
	if hasDDL([]db.Statement{{Text: `UPDATE "t" SET a = 1`}}) || !hasDDL([]db.Statement{{Text: "update t set a=1"}, {Text: "ALTER TABLE t ADD b int"}}) || !hasDDL([]db.Statement{{Text: "CREATE INDEX CONCURRENTLY i ON t(a)"}}) {
		t.Error("hasDDL")
	}
}
