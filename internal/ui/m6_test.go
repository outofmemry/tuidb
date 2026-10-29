package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/export"
	"webtuidb/internal/safemode"
)

// pickLabel chooses the item of the top picker whose label contains label.
func (h *harness) pickLabel(label string) {
	h.t.Helper()
	p, ok := h.a.overlays[len(h.a.overlays)-1].(*pickerDlg)
	if !ok {
		h.t.Fatalf("no picker on top:\n%s", h.screen())
	}
	for i, it := range p.shown() {
		if strings.Contains(it.Label, label) {
			p.sel = i
			h.press("enter")
			return
		}
	}
	h.t.Fatalf("no item %q in %+v", label, p.shown())
}

func (h *harness) queryTab() *queryTab {
	h.t.Helper()
	q, ok := h.a.ws().tab().(*queryTab)
	if !ok {
		h.t.Fatalf("the active tab is not an editor:\n%s", h.screen())
	}
	return q
}

func TestM6SafeModeLoweringAsksForThePassword(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SafeMode = "safe1" })
	h.connect()
	h.run("safe_mode")
	h.press("enter") // Silent
	h.want("Enter the database password")
	setField(t, h, "pw", "nope")
	h.press("enter")
	h.want("wrong password")
	if h.a.ws().level != safemode.Safe1 {
		t.Fatal("a wrong password must not lower the level")
	}
	setField(t, h, "pw", "pw")
	h.press("enter")
	if h.a.ws().level != safemode.Silent || h.a.d.Conns[0].SafeMode != "silent" {
		t.Fatalf("level %v conn %q", h.a.ws().level, h.a.d.Conns[0].SafeMode)
	}
}

func TestM6SSHURLFillsTheForm(t *testing.T) {
	h := newHarness(t, nil)
	h.press("i")
	setField(t, h, "url", "postgresql+ssh://u:p@db.internal:5432/app?ssh_host=jump.example.com&ssh_user=bob&ssh_key=/k&ssh_port=2222&tLSMode=verify-ca")
	h.press("enter")
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	v := f.values()
	if v["ssh"] != "true" || v["ssh_host"] != "jump.example.com" || v["ssh_user"] != "bob" || v["ssh_key"] != "/k" || v["ssh_port"] != "2222" || v["ssl_mode"] != "verify-ca" || v["host"] != "db.internal" {
		t.Fatalf("form %+v", v)
	}
}

func TestM6GroupRenameAndDelete(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.Group = "work" })
	h.a.d.Conns = append(h.a.d.Conns, config.Connection{Name: "b", Driver: config.Postgres, Host: "h", Group: "work/sub"}, config.Connection{Name: "c", Driver: config.Postgres, Host: "h", Group: "other"})
	h.press("g")
	h.typeText("work")
	h.press("enter")
	h.pickLabel("Rename")
	setField(t, h, "name", "jobs")
	h.press("enter")
	if g := h.a.d.Conns[1].Group; g != "jobs/sub" || h.a.d.Conns[0].Group != "jobs" {
		t.Fatalf("groups %q %q", h.a.d.Conns[0].Group, g)
	}
	h.press("g")
	h.typeText("jobs")
	h.press("enter")
	h.pickLabel("Delete")
	setField(t, h, "confirm", "wrong")
	h.press("enter")
	h.want("type “jobs”")
	setField(t, h, "confirm", "jobs")
	h.press("enter")
	if len(h.a.d.Conns) != 1 || h.a.d.Conns[0].Name != "c" {
		t.Fatalf("left %+v", h.a.d.Conns)
	}
	if saved, _ := config.LoadConnections(h.a.d.ConnPath); len(saved) != 1 {
		t.Fatalf("saved %d", len(saved))
	}
}

func TestM6CloseOtherConnections(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.pump(h.a.openConnection(h.a.d.Conns[0]))
	if len(h.a.wss) != 2 {
		t.Fatalf("workspaces %d", len(h.a.wss))
	}
	h.run("close_other_connections")
	h.press("y")
	if len(h.a.wss) != 1 || h.a.active != 0 {
		t.Fatalf("workspaces %d active %d", len(h.a.wss), h.a.active)
	}
}

func TestM6SplitDownStacksTwoEditors(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select 1")
	h.run("split_down")
	q := h.queryTab()
	if q.other == nil || !q.focusBottom || q.ed.Text() != "" || q.other.Text() != "select 1" {
		t.Fatalf("split: other=%v bottom=%v ed=%q", q.other != nil, q.focusBottom, q.ed.Text())
	}
	h.typeText("select 2")
	h.run("next_pane")
	if q.ed.Text() != "select 1" || q.other.Text() != "select 2" {
		t.Fatalf("after next_pane: %q / %q", q.ed.Text(), q.other.Text())
	}
	h.want("select 2") // both editors are drawn
	h.want("select 1")
	h.run("split_down") // the unfocused one has text: asks
	h.press("y")
	if q.other != nil || q.ed.Text() != "select 1" {
		t.Fatal("split_down again closes the other pane")
	}
}

func TestM6BlockSelectCopyAndDelete(t *testing.T) {
	e := NewEditor("postgres", EditorOpts{})
	e.SetText("abcd\nefgh\nijkl")
	e.cur = pos{0, 1}
	e.toggleBlock()
	e.Handle(keyMsg("right"))
	e.Handle(keyMsg("right"))
	e.Handle(keyMsg("down"))
	if got := e.Selection(); got != "bc\nfg" {
		t.Fatalf("block %q", got)
	}
	e.Handle(keyMsg("backspace"))
	if e.Text() != "ad\neh\nijkl" || e.blk != nil {
		t.Fatalf("after delete %q", e.Text())
	}
	// through the tab: V starts the mode, ctrl+c would copy Selection()
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	q.ed.SetText("abcd\nefgh")
	h.run("block_select")
	h.press("shift+right", "down")
	if q.ed.Selection() != "a\ne" || !strings.Contains(q.Title(), "BLOCK") {
		t.Fatalf("selection %q title %q", q.ed.Selection(), q.Title())
	}
	h.press("esc")
	if q.ed.blk != nil {
		t.Fatal("Esc ends block mode")
	}
}

func TestM6FindNextAndPrevious(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	q.ed.SetText("select foo; select foo; select foo")
	h.run("find_next") // no search text yet: asks
	setField(t, h, "q", "foo")
	h.press("enter")
	if q.ed.cur.col != 10 {
		t.Fatalf("first match ends at %d", q.ed.cur.col)
	}
	h.run("find_next")
	if q.ed.cur.col != 22 {
		t.Fatalf("second match ends at %d", q.ed.cur.col)
	}
	h.run("find_prev")
	if q.ed.cur.col != 10 || q.ed.Selection() != "foo" {
		t.Fatalf("previous match ends at %d sel %q", q.ed.cur.col, q.ed.Selection())
	}
	h.run("find_prev") // wraps to the last one
	if q.ed.cur.col != 34 {
		t.Fatalf("wrapped to %d", q.ed.cur.col)
	}
}

func TestM6MongoMethodSuggestions(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.dialect = "mongo"
	h.connect()
	h.prefix("e")
	got := h.queryTab().complete("db.users.fi")
	names := map[string]bool{}
	for _, c := range got {
		names[c.Text] = true
	}
	if !names["find"] || !names["findOne"] {
		t.Fatalf("suggestions %+v", got)
	}
	if len(mongoMethodCandidates("db.users")) != len(mongoMethods) || mongoMethodCandidates("users") != nil {
		t.Fatal("methods follow db.<collection>. only")
	}
}

func TestM6OpenFileRecentAndViewer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.sql")
	if err := os.WriteFile(path, []byte("select 42 from dual;"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, nil)
	h.run("open_file") // welcome: the read-only viewer
	setField(t, h, "path", path)
	h.press("enter")
	h.want("SQL viewer")
	h.want("select 42")
	h.press("esc")
	if rf := h.a.d.State.RecentFiles(); len(rf) != 1 || rf[0] != path {
		t.Fatalf("recent %v", rf)
	}
	h.connect()
	h.run("open_recent")
	h.want("q.sql")
	h.press("enter")
	if q := h.queryTab(); q.ed.Text() != "select 42 from dual;" || q.Title() != "q.sql" {
		t.Fatalf("tab %q %q", q.Title(), q.ed.Text())
	}
}

func TestM6ChartWithSeveralYColumns(t *testing.T) {
	res := &db.Result{HasRows: true, Columns: []db.ResultColumn{{Name: "m", Type: "text"}, {Name: "a", Type: "int"}, {Name: "b", Type: "int"}},
		Rows: [][]any{{"jan", int64(3), int64(5)}, {"feb", int64(4), int64(1)}}}
	rv := &resultView{raw: res, stmt: "-- barchart x: m, y: a, b\nselect 1"}
	h := newHarness(t, nil)
	out := stripANSI(strings.Join(chartLines(h.a.th, rv, 80, 20), "\n"))
	for _, want := range []string{"█ a", "▓ b", "jan", "feb", "5"} {
		if !strings.Contains(out, want) {
			t.Fatalf("chart lacks %q:\n%s", want, out)
		}
	}
}

func TestM6SchemaCreateThroughTheGate(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SafeMode = "alert1" })
	h.connect()
	h.run("new_schema")
	setField(t, h, "name", "audit")
	h.press("enter")
	h.want("send to the server?")
	if len(h.fake.applied) != 0 {
		t.Fatal("nothing is sent before confirming")
	}
	h.press("y")
	if len(h.fake.applied) != 1 || h.fake.applied[0][0].Text != "-- create_schema audit" {
		t.Fatalf("applied %+v", h.fake.applied)
	}
}

func TestM6SchemaDropNeedsTheName(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.press("m")
	h.pickLabel("Drop schema")
	h.pickLabel("app")
	setField(t, h, "confirm", "app")
	h.press("enter")
	if len(h.fake.applied) != 1 || h.fake.applied[0][0].Text != "-- drop_schema app" {
		t.Fatalf("applied %+v", h.fake.applied)
	}
}

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

func indexOfRow(rows []sbRow, group string) int {
	for i, r := range rows {
		if r.group == group {
			return i
		}
	}
	return 0
}

func TestM6NewDatabaseSendsEncodingAndCollation(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.run("open_database")
	h.pickLabel("New database")
	setField(t, h, "name", "fresh")
	setField(t, h, "encoding", "UTF8")
	setField(t, h, "collation", "C")
	h.press("enter")
	last := h.fake.adminOpts[len(h.fake.adminOpts)-1]
	if last["encoding"] != "UTF8" || last["collation"] != "C" {
		t.Fatalf("opts %v", last)
	}
}

func TestM6ExportSeveralTablesAcrossSchemas(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.Export.Filename = "{table}" })
	h.connect()
	h.run("export")
	setChoice(t, h, "many", "true")
	h.press("enter")
	h.want("Export which tables?")
	h.press("down", "down", "space") // public.customers is ticked already; add the third entry (app.customers)
	h.press("enter")
	setField(t, h, "folder", dir)
	h.press("enter")
	for _, name := range []string{"public.customers.csv", "app.customers.csv"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			ents, _ := os.ReadDir(dir)
			t.Fatalf("%v (have %v)", err, ents)
		}
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

func TestM6QuickLookSaveAs(t *testing.T) {
	h := newHarness(t, nil)
	q := h.a.newQuickLookValue("blob", []byte{0xde, 0xad, 0xbe, 0xef}, nil)
	h.a.push(q)
	h.press("s")
	path := filepath.Join(t.TempDir(), "cell.bin")
	setField(t, h, "path", path)
	h.press("enter")
	if b, err := os.ReadFile(path); err != nil || string(b) != "\xde\xad\xbe\xef" {
		t.Fatalf("%v %x", err, b)
	}
}

func TestM6CSVQuoteModeAndWarning(t *testing.T) {
	old := copyCSV
	defer func() { copyCSV = old }()
	copyCSV = config.CSV{Delimiter: ",", Quote: `"`, LineBreak: "\n", QuoteMode: "always"}
	if got := csvLine([]string{"a", "", "b c"}); got != `"a",,"b c"`+"\n" {
		t.Fatalf("always: %q", got)
	}
	copyCSV.QuoteMode = "needed"
	if got := csvLine([]string{"a", "b,c"}); got != `a,"b,c"`+"\n" {
		t.Fatalf("needed: %q", got)
	}
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.CSV.Delimiter, c.CSV.Decimal = ",", "," })
	h.connect()
	h.openCustomers()
	h.run("export")
	h.want("delimiter and decimal separator")
	if export.CheckCSV(h.a.d.Cfg.CSV) == "" {
		t.Fatal("the config must trigger the warning")
	}
}

func TestReview678SplitClearsSavedAsAndDirtyCountsOtherPane(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	q.savedAs, q.lastAuto = "/x/report.sql", "a"
	h.typeText("select 1")
	h.run("split_down")
	if q.savedAs != "" || q.lastAuto != "" {
		t.Fatal("split must detach the saved query")
	}
	if !q.Dirty() {
		t.Fatal("text in the other pane counts as unsaved")
	}
}

func TestReview678BlockClearedOnUndo(t *testing.T) {
	e := NewEditor("postgres", EditorOpts{})
	e.SetText("a\nb\nc\nd")
	e.cur = pos{3, 0}
	e.Handle(keyMsg("enter"))
	e.toggleBlock()
	e.Undo()
	if e.blk != nil {
		t.Fatal("undo must leave block mode")
	}
}
