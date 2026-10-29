package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/diagram"
)

func writeTo(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}

// topView renders the dialog on top of the stack by itself.
func topView(h *harness) string {
	return stripANSI(h.a.overlays[len(h.a.overlays)-1].View(h.a.th, 120, 36))
}

func TestM3UsersCreateGoesThroughPreview(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.usersOn, h.fake.ulist = true, []db.User{{Name: "ann", Attrs: []string{"LOGIN"}}}
	h.connect()
	h.prefix("u")
	h.want("Users (1)")
	h.want("ann")
	h.press("enter") // "New user"
	setField(t, h, "name", "bob")
	setField(t, h, "pw", "s3cret")
	h.press("enter")
	h.want("Attributes for bob")
	h.press("enter")
	h.want("Preview: 1 statement")
	h.want("CREATE ROLE bob PASSWORD '********'")
	h.wantNot("s3cret")
	if len(h.fake.applied) != 0 {
		t.Fatal("nothing may be sent before the preview is accepted")
	}
	h.press("enter")
	if len(h.fake.applied) != 1 || !strings.Contains(h.fake.applied[0][0].Text, "'s3cret'") {
		t.Fatalf("applied %+v", h.fake.applied)
	}
	h.want("Users (1)") // the list comes back
}

func TestM3UsersDropNeedsTheName(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.usersOn, h.fake.ulist = true, []db.User{{Name: "ann"}}
	h.connect()
	h.prefix("u")
	h.press("down", "enter") // ann
	h.pickLabel("Drop user")
	h.want("Drop user ann?")
	h.press("enter")
	h.want("type “ann”")
	setField(t, h, "confirm", "ann")
	h.press("enter", "enter")
	if len(h.fake.applied) != 1 || h.fake.applied[0][0].Text != "DROP ann" {
		t.Fatalf("applied %+v", h.fake.applied)
	}
}

func TestM3UsersNotSupported(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("u")
	h.want("not supported by postgres")
}

func fakeTool(t *testing.T, name string) {
	dir := t.TempDir()
	writeTo(t, filepath.Join(dir, name), "#!/bin/sh\necho dumping >&2\nprev=; for a in \"$@\"; do [ \"$prev\" = -f ] && : > \"$a\"; prev=$a; done\n", 0o755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestM3BackupShowsRedactedCommandAndStreams(t *testing.T) {
	fakeTool(t, "pg_dump")
	h := newHarness(t, nil)
	h.connect()
	h.prefix("b")
	h.want("Backup shop")
	setField(t, h, "file", filepath.Join(t.TempDir(), "out.sql"))
	h.press("enter")
	h.want("pg_dump")
	h.wantNot("PGPASSWORD")
	h.wait = time.Second // starting the tool takes longer than the harness's idle window
	h.press("enter")
	h.want("dumping")
	h.want("done")
}

func TestM3BackupToolMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	h := newHarness(t, nil)
	h.connect()
	h.prefix("b")
	h.want("pg_dump not found") // said before the form is filled in (T-P3-17)
	h.want("Backup shop")       // the form still opens: Ctrl+t inside it is where the path is set
}

func TestM3RestoreGoesThroughSafeMode(t *testing.T) {
	fakeTool(t, "psql")
	fakeTool(t, "pg_restore")
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SafeMode = "alert1" })
	h.connect()
	dump := filepath.Join(t.TempDir(), "in.sql")
	writeTo(t, dump, "select 1;", 0o644)
	h.prefix("B")
	setField(t, h, "file", dump)
	h.press("enter")
	h.want("Restore shop?")
	h.press("y")
	h.want("send to the server?") // the safe-mode gate, with the command line as the statement
	h.press("n")
	h.wantNot("dumping")
}

func TestM3DiagramRendersAndFilters(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("d")
	if _, ok := h.a.overlays[len(h.a.overlays)-1].(*paneDlg); !ok {
		t.Fatalf("no diagram:\n%s", h.screen())
	}
	if v := topView(h); !strings.Contains(v, "customers") || !strings.Contains(v, "orders") || !strings.Contains(v, "pk id") {
		t.Fatalf("diagram:\n%s", v)
	}
	h.press("/")
	setField(t, h, "f", "ord")
	h.press("enter")
	if v := topView(h); strings.Contains(v, "customers") || !strings.Contains(v, "orders") || !strings.Contains(v, "filter: ord") {
		t.Fatalf("filtered diagram:\n%s", v)
	}
	h.press("n") // hands over to the existing new-table flow
	h.want("New table")
}

func TestM3ColorHeadersTintsOnlyHeaderRows(t *testing.T) {
	tables := []diagram.Table{{Ref: db.ObjRef{Name: "a"}, Columns: customerCols}}
	text := diagram.Render(tables, 80, "name", "")
	lines := colorHeaders(text, map[string]int{"a": 1})
	var tinted int
	for _, l := range lines {
		if strings.Contains(l, "\x1b[") {
			tinted++
		}
	}
	if tinted != 1 || !strings.Contains(stripANSI(lines[1]), "| a") {
		t.Fatalf("lines %q", lines)
	}
}

func TestM3BoardRunsItsPanels(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	writeTo(t, filepath.Join(config.DataDir(), "boards", "sales.toml"), `name = "Sales"
[[var]]
name = "who"
default = "x"
[[panel]]
title = "People"
kind = "table"
query = "select * from customers"
[[panel]]
title = "Total"
kind = "number"
query = "select 1"
`, 0o644)
	h := newHarness(t, nil)
	h.connect()
	h.prefix("m")
	h.want("Sales")
	h.press("enter")
	v := topView(h)
	for _, s := range []string{"Board Sales", "People", "ann@x.io", "Total", "who=x"} {
		if !strings.Contains(v, s) {
			t.Fatalf("board lacks %q:\n%s", s, v)
		}
	}
	h.press("esc")
	if len(h.a.overlays) != 0 {
		t.Fatal("Esc should close the board")
	}
}

func TestM3AddChartToBoard(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	h := newHarness(t, nil)
	h.connect()
	queryResult(h, "select * from customers")
	h.prefix("g")
	h.press("b")
	h.want("Add to Metrics Board")
	setField(t, h, "name", "cust")
	h.press("enter")
	if b, err := os.ReadFile(filepath.Join(config.DataDir(), "boards", "cust.toml")); err != nil || !strings.Contains(string(b), "select * from customers") {
		t.Fatalf("board file %q: %v", b, err)
	}
}

func twoTabs(h *harness) *Workspace {
	h.connect()
	h.openCustomers()
	h.prefix("e")
	return h.a.ws()
}

func TestM3SplitRightAndNextPane(t *testing.T) {
	h := newHarness(t, nil)
	w := twoTabs(h)
	h.prefix(`"`)
	if !w.m3.split || w.pane(w.tabs[1]) != 1 || w.m3.pane != 1 {
		t.Fatalf("split state %+v", w.m3)
	}
	h.want("customers")
	h.want("SQL")
	if _, ok := w.tab().(*queryTab); !ok {
		t.Fatal("the moved tab keeps the focus")
	}
	h.prefix("alt+]")
	if _, ok := w.tab().(*tableTab); !ok || w.m3.pane != 0 {
		t.Fatalf("next_pane did not focus the left group: %T pane %d", w.tab(), w.m3.pane)
	}
	h.prefix("alt+[")
	if _, ok := w.tab().(*queryTab); !ok {
		t.Fatal("prev_pane did not come back")
	}
	h.prefix(`"`) // moving the only tab of the right group back closes the split
	if w.m3.split {
		t.Fatalf("split should be closed: %+v", w.m3)
	}
}

func TestM3SplitNeedsTwoTabsAndNewTabsOpenInFocusedGroup(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix(`"`)
	h.want("open a second tab")
	h.prefix("e")
	h.prefix(`"`)
	h.prefix("e") // a new tab opens in the focused (right) group
	if w := h.a.ws(); w.pane(w.tabs[2]) != 1 || len(w.groupIdx(1)) != 2 {
		t.Fatalf("groups %v / %v", w.groupIdx(0), w.groupIdx(1))
	}
	h.prefix("w") // close_tab keeps the split while both groups have tabs
	if !h.a.ws().m3.split {
		t.Fatal("split closed too early")
	}
}

func TestM3VimModeDeletesAChar(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("abc")
	h.prefix("v")
	h.want("[N]")
	h.press("0", "x")
	q := h.a.ws().tab().(*queryTab)
	if q.ed.Text() != "bc" {
		t.Fatalf("text %q", q.ed.Text())
	}
	h.typeText("z") // unknown normal-mode letters must not insert
	h.press("i", "q", "esc")
	if q.ed.Text() != "qbc" {
		t.Fatalf("insert mode: %q", q.ed.Text())
	}
	h.press("o")
	h.typeText("n")
	h.press("esc", "g", "g", "d", "d", "p")
	if q.ed.Text() != "n\nqbc" {
		t.Fatalf("dd p: %q", q.ed.Text())
	}
	h.press("u")
	h.prefix("v")
	h.wantNot("[N]")
}

func TestM3EditorOccurrencesAndInvisibles(t *testing.T) {
	e := NewEditor("postgres", EditorOpts{})
	e.SetText("foo bar\tfoo")
	e.W, e.H = 40, 2
	if msg := e.extra("select_all_occurrences"); !strings.Contains(msg, "2 occurrence(s) of \"foo\"") {
		t.Fatalf("msg %q", msg)
	}
	e.marks, e.anchor, e.cur = "", nil, pos{}
	if e.extra("select_next_occurrence"); e.Selection() != "foo" {
		t.Fatalf("first press selects the word, got %q", e.Selection())
	}
	e.extra("select_next_occurrence")
	if e.cur.col != 11 {
		t.Fatalf("second press jumps to the next match: %+v", e.cur)
	}
	e.extra("invisibles")
	th := NewTheme(config.Theme{})
	if v := stripANSI(e.View(th, true)); !strings.Contains(v, "foo·bar→foo") {
		t.Fatalf("view %q", v)
	}
}

func chatServer(t *testing.T, reply string, body *string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*body = string(b)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": reply}}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestM3AssistantRunsASQLBlockInAQueryTab(t *testing.T) {
	var req string
	srv := chatServer(t, "Try:\n```sql\nselect * from customers\n```\nDone.", &req)
	h := newHarness(t, func(_ *config.Connection, c *config.Config) {
		c.AI = config.AI{Provider: "openai", Model: "m", BaseURL: srv.URL, APIKey: "k"}
	})
	h.connect()
	h.prefix("A")
	h.typeText("list @customers")
	h.press("enter")
	h.want("select * from customers")
	h.want("copy or run the 1 SQL block")
	if !strings.Contains(req, "CREATE TABLE customers") {
		t.Fatalf("the @mention did not put the DDL in the system prompt: %s", req)
	}
	h.press("ctrl+o")
	h.want("SQL from the assistant")
	h.press("enter")
	if q, ok := h.a.ws().tab().(*queryTab); !ok || q.ed.Text() != "select * from customers" {
		t.Fatalf("no query tab with the block:\n%s", h.screen())
	}
	if len(h.fake.execs) != 1 || h.fake.execs[0] != "select * from customers" {
		t.Fatalf("execs %v", h.fake.execs)
	}
	h.prefix("A") // the same tab again, history intact
	h.want("list @customers")
}

func TestM3AssistantNeedsAKeyAndAsksBeforeWrites(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("A")
	h.typeText("hi")
	h.press("enter")
	h.want("needs an API key")
	h.a.ws().runSQL("delete from customers")
	h.want("changes data")
	if len(h.fake.execs) != 0 {
		t.Fatal("nothing may run before the question is answered")
	}
}

func TestM3PluginInsertsSQL(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := filepath.Join(config.ConfigDir(), "plugins")
	writeTo(t, filepath.Join(dir, "ins.toml"), "name = \"Insert\"\ncommand = \"printf\"\nargs = [\"select 42\"]\ncontexts = [\"editor\"]\noutput = \"sql\"\n", 0o644)
	writeTo(t, filepath.Join(dir, "grid.toml"), "name = \"GridOnly\"\ncommand = \"printf\"\nargs = [\"x\"]\ncontexts = [\"grid\"]\n", 0o644)
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.prefix("alt+p")
	h.want("Insert")
	h.wantNot("GridOnly") // filtered by the focused pane
	h.press("enter")
	if q := h.a.ws().tab().(*queryTab); q.ed.Text() != "select 42" {
		t.Fatalf("editor %q", q.ed.Text())
	}
}

func TestM3PluginTextOutputOpensAView(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeTo(t, filepath.Join(config.ConfigDir(), "plugins", "hi.toml"), "name = \"Hello\"\ncommand = \"printf\"\nargs = [\"hello there\"]\n", 0o644)
	h := newHarness(t, nil)
	h.connect()
	h.prefix("alt+p") // the sidebar has the focus; a plugin without contexts applies everywhere
	h.press("enter")
	h.want("hello there")
}

func TestM3GoBackAndForward(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	cust := db.ObjRef{Schema: "public", Name: "customers", Kind: db.KindTable}
	ord := db.ObjRef{Schema: "public", Name: "orders", Kind: db.KindTable}
	h.pump(w.openObject(cust, false))
	h.pump(w.openObject(ord, false))
	if w.tab().(*tableTab).ref != ord {
		t.Fatal("orders should be open")
	}
	h.prefix("<")
	if w.tab().(*tableTab).ref != cust {
		t.Fatalf("back should reopen customers, got %v", w.tab().(*tableTab).ref)
	}
	h.prefix(">")
	if w.tab().(*tableTab).ref != ord {
		t.Fatal("forward should reopen orders")
	}
	h.prefix(">")
	h.want("nothing to go forward to")
	for i := 0; i < 60; i++ { // the history is capped
		w.m3.visit(db.ObjRef{Name: string(rune('a' + i%26)), Schema: string(rune('A' + i))})
	}
	if len(w.m3.hist) != 50 {
		t.Fatalf("history %d", len(w.m3.hist))
	}
}

func TestM3BootstrapRunsAfterConnect(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.Bootstrap = []string{"SET search_path = app", "boom"} })
	h.press("enter")
	if len(h.fake.execs) != 2 || h.fake.execs[0] != "SET search_path = app" {
		t.Fatalf("execs %v", h.fake.execs)
	}
	h.want("Bootstrap command failed") // the failing one is shown...
	h.press("esc")
	h.want("customers") // ...and the workspace still loaded
}

func TestM3MCPActivityWithoutServer(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("p") // open_anything
	h.typeText("MCP")
	h.press("enter")
	h.want("MCP server is off")
}
