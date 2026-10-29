package ui

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func setChoice(t *testing.T, h *harness, key, val string) {
	t.Helper()
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	for _, fl := range f.fields {
		if fl.Key == key {
			fl.Value = val
			return
		}
	}
	t.Fatalf("no field %q", key)
}

func TestM5ConnectionsExportImport(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.Group = "work" })
	path := filepath.Join(t.TempDir(), "c.wtdb")
	h.press("x")
	setChoice(t, h, "scope", "work")
	setField(t, h, "path", path)
	setField(t, h, "pass", "s3cret")
	h.press("enter")
	b, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(b), "shop") {
		t.Fatalf("export must be encrypted: %v %q", err, b)
	}
	h.press("I")
	setField(t, h, "path", path)
	setField(t, h, "pass", "s3cret")
	h.press("enter")
	if n := len(h.a.d.Conns); n != 2 || h.a.d.Conns[1].Name != "shop (imported)" || h.a.d.Conns[1].Password != "" {
		t.Fatalf("conns after import: %+v", h.a.d.Conns)
	}
	if saved, _ := config.LoadConnections(h.a.d.ConnPath); len(saved) != 2 {
		t.Fatalf("import must be saved, file has %d", len(saved))
	}
}

func TestM5SourceConnectionsAreReadOnly(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.Source = "/srv/team.toml" })
	h.want("source")
	h.press("e")
	h.wantNot("Edit connection")
	h.press("d")
	if len(h.a.overlays) != 0 {
		t.Fatal("a source connection cannot be deleted")
	}
	h.press("c") // duplicate = local copy
	if saved, _ := config.LoadConnections(h.a.d.ConnPath); len(saved) != 1 || saved[0].Source != "" {
		t.Fatalf("only the copy is saved: %+v", saved)
	}
}

func TestM5CustomTagColour(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, cfg *config.Config) {
		c.Tag = "qa"
		cfg.Tags = []config.Tag{{Name: "qa", Color: "#123456"}}
	})
	if got := h.a.tagColor(h.a.d.Conns[0]); got != "#123456" {
		t.Fatalf("tag colour %q", got)
	}
	if !contains(h.a.tagNames(), "qa") {
		t.Fatal("custom tag missing from the picker")
	}
	h.connect()
	h.want("QA")
}

func TestM5OpenTargetFiltersTable(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) {
		c.Open = &config.OpenTarget{Table: "customers", Column: "name", Operation: "eq", Value: "Bob"}
	})
	h.connect()
	fs := h.fake.lastQuery.Filters
	if len(fs.Filters) != 1 || fs.Filters[0] != (db.Filter{Column: "name", Op: "=", Value: "Bob", Enabled: true}) {
		t.Fatalf("filters %+v", fs)
	}
	if h.a.ws().tab() == nil || h.a.ws().tab().Title() != "customers" {
		t.Fatal("the table must be open")
	}
}

func TestM5ExplainShowsTree(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select * from customers")
	h.run("explain")
	h.want("Explain")
	h.want("Hash Join")
	h.want("Seq Scan")
	if !strings.Contains(h.fake.execs[len(h.fake.execs)-1], "EXPLAIN (FORMAT JSON") {
		t.Fatalf("execs %v", h.fake.execs)
	}
}

func TestM5PinnedTabRunsOnItsOwnConnection(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.run("pin_connection")
	h.want("[pinned]")
	h.typeText("select 1")
	h.prefix("enter")
	if len(h.fake.pins) != 1 || len(h.fake.pins[0].execs) != 1 || h.fake.pins[0].execs[0] != "select 1" {
		t.Fatalf("pinned execs: %+v", h.fake.pins)
	}
	h.run("pin_connection")
	if !h.fake.pins[0].closed {
		t.Fatal("unpinning must release the connection")
	}
	h.wantNot("[pinned]")
}

func TestM5BulkUpdateThroughTheGate(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SafeMode = "alert2" })
	h.connect()
	h.openCustomers()
	h.run("bulk_update")
	h.want("filter the rows first")
	h.a.ws().tab().(*tableTab).filters = db.FilterSet{Filters: []db.Filter{{Column: "name", Op: "=", Value: "Bob", Enabled: true}}}
	h.a.ws().tab().(*tableTab).markApplied()
	h.run("bulk_update")
	setField(t, h, "set", "email=x@y.z ;; name=NULL")
	h.press("enter")
	h.want("Bulk update") // the preview comes first
	h.press("a")
	h.want("send to the server?") // the safe-mode gate sees the UPDATE
	if len(h.fake.applied) != 0 {
		t.Fatal("nothing is sent before confirming")
	}
	h.press("y")
	if len(h.fake.applied) != 1 || len(h.fake.applied[0]) != 1 || !strings.HasPrefix(h.fake.applied[0][0].Text, `UPDATE "public"."customers" SET "email" = $1, "name" = NULL WHERE`) {
		t.Fatalf("applied %+v", h.fake.applied)
	}
}

func TestM5FakeRows(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.run("fake_rows")
	setField(t, h, "n", "3")
	h.press("enter")
	h.press("a") // preview, then apply
	if len(h.fake.applied) != 1 || len(h.fake.applied[0]) != 3 || !strings.HasPrefix(h.fake.applied[0][0].Text, "INSERT") {
		t.Fatalf("applied %+v", h.fake.applied)
	}
}

func TestM5AliasAwareComplete(t *testing.T) {
	if got := aliasesIn("select c.id from public.customers c join orders as o on o.cid = c.id where x"); got["c"] != "public.customers" || got["o"] != "orders" || got["where"] != "" {
		t.Fatalf("aliases %v", got)
	}
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.a.ws().tab().(*queryTab)
	q.cols["customers"] = []string{"id", "name"}
	q.ed.SetText("select c.na from customers c")
	q.ed.move(0, 11, false)
	got := q.complete("c.na")
	if len(got) == 0 || got[0].Text != "name" {
		t.Fatalf("complete %+v", got)
	}
}

// T-P1-02: accepting a completion after schema. or alias. keeps the qualifier, and alias. resolves with its schema.
func TestM18CompletionKeepsQualifier(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.a.ws().tab().(*queryTab)
	q.cols["app.customers"] = []string{"id", "city"}
	q.cols["customers"] = []string{"id", "meta"} // the sidebar schema's customers: must not be offered for app.customers c
	q.ed.SetText("select c.ci from app.customers c")
	q.ed.move(0, 12, false)
	if got := q.complete("c.ci"); len(got) == 0 || got[0].Text != "city" {
		t.Fatalf("alias of app.customers: %+v", got)
	}
	q.ed.SetText("select count(*) from public.cus")
	q.ed.move(0, 31, false)
	if !q.ed.openComplete() {
		t.Fatal("no completion for public.cus")
	}
	q.ed.acceptComplete()
	if got := strings.TrimSpace(q.ed.Text()); got != "select count(*) from public.customers" {
		t.Fatalf("schema. dropped: %q", got)
	}
	q.ed.SetText("select c. from app.customers c")
	q.ed.move(0, 9, false)
	if !q.ed.openComplete() {
		t.Fatal("no completion for c.")
	}
	q.ed.acceptComplete()
	if got := q.ed.Text(); !strings.HasPrefix(got, "select c.id") {
		t.Fatalf("alias dropped: %q", got)
	}
}

func TestM5EditorTransforms(t *testing.T) {
	e := NewEditor("postgres", EditorOpts{})
	e.SetText("select name from t")
	e.anchor, e.cur = &pos{0, 7}, pos{0, 11}
	e.transform("upper_case")
	if e.Text() != "select NAME from t" || e.Selection() != "NAME" {
		t.Fatalf("%q sel %q", e.Text(), e.Selection())
	}
	e.transform("capitalize")
	if e.Text() != "select Name from t" {
		t.Fatalf("%q", e.Text())
	}
	e.transform("lower_case")
	if e.Text() != "select name from t" {
		t.Fatalf("%q", e.Text())
	}
}

func TestM5TransformWordUnderCursor(t *testing.T) {
	e := NewEditor("postgres", EditorOpts{})
	e.SetText("select name from t")
	e.cur, e.anchor = pos{0, 9}, nil
	e.transform("upper_case")
	if e.Text() != "select NAME from t" {
		t.Fatalf("%q", e.Text())
	}
	e.SetText("a  b")
	e.cur, e.anchor = pos{0, 2}, nil // between two spaces: no word
	if msg := e.transform("upper_case"); msg == "" || e.Text() != "a  b" {
		t.Fatalf("%q %q", msg, e.Text())
	}
}

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

// at finds the screen cell of the first occurrence of s.
func (h *harness) at(s string) (x, y int) {
	h.t.Helper()
	for y, l := range strings.Split(h.screen(), "\n") {
		if i := strings.Index(l, s); i >= 0 {
			return len([]rune(l[:i])), y
		}
	}
	h.t.Fatalf("%q is not on screen:\n%s", s, h.screen())
	return
}

func TestM5MouseClickSelects(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	x, y := h.at("Cy")
	_, cmd := h.a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	h.pump(cmd)
	g := h.a.ws().tab().(*tableTab).grid
	if g.row != 2 || g.col != 1 {
		t.Fatalf("cursor at row %d col %d, want 2/1", g.row, g.col)
	}
	if h.a.ws().focusSB {
		t.Fatal("a click in the grid moves the focus there")
	}
	x, y = h.at("orders")
	_, cmd = h.a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	h.pump(cmd)
	w := h.a.ws()
	if r, ok := w.sb.current(); !w.focusSB || !ok || r.obj.Name != "orders" {
		t.Fatalf("sidebar selection %+v focus %v", r, w.focusSB)
	}
}

func TestM5Ansi16Fallback(t *testing.T) {
	got := ansi16(map[string]string{"a": "#f7768e", "b": "#000000", "c": "red"})
	if got["b"] != "0" || got["c"] != "red" || got["a"] != "9" {
		t.Fatalf("%v", got)
	}
	t.Setenv("COLORTERM", "")
	t.Setenv("TERM", "xterm")
	t.Setenv("WEBTUIDB_COLORS", "")
	if !limitedColors() {
		t.Fatal("plain xterm without COLORTERM is a 16-color terminal")
	}
	t.Setenv("COLORTERM", "truecolor")
	if limitedColors() {
		t.Fatal("truecolor must keep the palette")
	}
}

func TestM5BoardConnectionAndDuplicate(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	writeTo(t, filepath.Join(config.DataDir(), "boards", "sales.toml"), `name = "Sales"
connection = "elsewhere"
[[panel]]
title = "Total"
kind = "number"
query = "select 1"
`, 0o644)
	h := newHarness(t, nil)
	h.connect()
	h.prefix("m")
	h.press("enter")
	h.want("not open")
	h.press("esc")
	h.prefix("m")
	h.press("d")
	if _, err := os.Stat(filepath.Join(config.DataDir(), "boards", "Sales copy.toml")); err != nil {
		t.Fatalf("duplicate not saved: %v", err)
	}
}

func TestM5Diffs(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.run("schema_diff")
	setField(t, h, "to", "app")
	h.press("enter")
	if q, ok := h.a.ws().tab().(*queryTab); !ok || !strings.Contains(q.ed.Text(), "ALTER TABLE customers;") { // the fake plans an ALTER for every table
		t.Fatalf("schema diff must land in an editor tab:\n%s", h.screen())
	}
	h.run("data_diff")
	h.press("enter") // customers -> orders: the fake serves the same rows for both
	h.want("nothing to change")
}

func TestM5ConnectionDetailsAndStackLabel(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.General.ShowConnectionName = true })
	h.connect()
	h.run("connection_details")
	h.want("Connection details")
	h.want("Fake 1.0")
	if got := h.a.stackLabel(h.a.ws()); got != "shop" {
		t.Fatalf("label %q", got)
	}
}

func TestVimNormalModeIgnoresEditingKeys(t *testing.T) {
	e := NewEditor("postgres", EditorOpts{})
	e.SetText("select 1")
	e.vim.on, e.vim.insert = true, false
	e.cur = pos{0, 3}
	for _, c := range []rune{tea.KeyEnter, tea.KeyTab, tea.KeyBackspace, tea.KeyDelete} {
		e.Handle(tea.KeyPressMsg{Code: c})
		if e.Text() != "select 1" {
			t.Fatalf("key %v edited the buffer: %q", c, e.Text())
		}
	}
}

func TestCleanReplyStripsControlSequences(t *testing.T) {
	in := "ok\x1b]52;c;ZXZpbA==\x07 \x1b[2Jfine\r\nline\x00two\ttab"
	if got := cleanReply(in); got != "ok fine\nlinetwo\ttab" {
		t.Fatalf("%q", got)
	}
}

func TestAssistantFailureAfterQuestionDeleted(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	at := &assistantTab{ws: w, id: w.newID(), running: true}
	w.m3.chat = nil // the messages dialog deleted the only message
	at.sendFailed("hi", errors.New("boom"), false)
	if len(w.m3.chat) != 0 || at.running || at.in.Value() != "hi" {
		t.Fatalf("chat %v running %v input %q", w.m3.chat, at.running, at.in.Value())
	}
}

func TestQueryRerunWithPinnedResultKeepsIndexInRange(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.a.ws().tab().(*queryTab)
	q.res = []*resultView{{pinned: true}}
	q.override = 100
	q.start([]db.Stmt{{Text: "select 1"}})
	if q.rc != 0 || q.rc >= len(q.res) || q.override != 0 {
		t.Fatalf("rc %d res %d override %d", q.rc, len(q.res), q.override)
	}
	q.resultsView(h.a.th, 40, 10) // used to index t.res[len(t.res)]
}
