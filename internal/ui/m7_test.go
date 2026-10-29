package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/board"
	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func TestM7SettingsOpensTheConfigInTheEditor(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "myed --flag")
	cmd := editorCommand("/x/config.toml")
	if got := strings.Join(cmd.Args, " "); got != "myed --flag /x/config.toml" {
		t.Fatalf("args %q", got)
	}
	h := newHarness(t, nil)
	h.a.d.CfgPath = filepath.Join(t.TempDir(), "sub", "config.toml")
	if h.a.cfgPath() != h.a.d.CfgPath || h.a.settings() == nil {
		t.Fatal("settings must suspend the program for the editor")
	}
	if _, err := os.Stat(filepath.Dir(h.a.d.CfgPath)); err != nil {
		t.Fatal("the config folder is created for the editor")
	}
}

func TestM7ProcessListOpensItsOwnSession(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.procs = []db.ProcessInfo{{ID: "7", User: "ann", DB: "shop", State: "active", Query: "select pg_sleep(9)", Elapsed: 3 * time.Second}}
	h.connect()
	n := len(h.connected)
	h.run("process_list")
	h.want("Processes")
	h.want("pg_sleep")
	if len(h.connected) != n+1 || h.a.ws().side == nil {
		t.Fatalf("connects %d -> %d, side %v", n, len(h.connected), h.a.ws().side)
	}
	h.press("esc")
	h.run("process_list")
	if len(h.connected) != n+1 {
		t.Fatal("the process-list session is kept, not reopened")
	}
}

func TestM7CancelOpensTheProcessListWhileBusy(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select sleep")
	tab := h.queryTab()
	h.a.Update(keyMsg("ctrl+d"))
	_, cmd := h.a.Update(keyMsg("alt+enter"))
	go cmd()
	if !tab.running {
		t.Fatal("the query should be running")
	}
	n := len(h.connected)
	h.run("process_list") // the same key: cancel while something runs, and the list opens on the side session
	if tab.running {
		t.Fatal("cancelled")
	}
	h.want("Processes")
	if len(h.connected) != n+1 {
		t.Fatalf("connects %d -> %d", n, len(h.connected))
	}
}

func TestM7BackupToolPathAndRememberedOptions(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	tool := filepath.Join(t.TempDir(), "my-pg-dump")
	writeTo(t, tool, "#!/bin/sh\nexit 0\n", 0o755)
	h := newHarness(t, nil)
	h.connect()
	h.prefix("b")
	h.press("ctrl+t")
	h.want("Tool paths for shop")
	setField(t, h, "pg_dump", tool)
	h.press("enter")
	if h.a.d.Conns[0].Tools["pg_dump"] != tool {
		t.Fatalf("tools %v", h.a.d.Conns[0].Tools)
	}
	if saved, _ := config.LoadConnections(h.a.d.ConnPath); len(saved) != 1 || saved[0].Tools["pg_dump"] != tool {
		t.Fatalf("saved %+v", saved)
	}
	out := filepath.Join(t.TempDir(), "x.sql")
	setField(t, h, "file", out)
	setChoice(t, h, "gzip", "true")
	h.press("enter")
	h.want("my-pg-dump")
	h.press("esc")
	mem := h.a.d.State.BackupOptions("shop")
	if mem["backup.file"] != out || mem["backup.gzip"] != "true" {
		t.Fatalf("remembered %v", mem)
	}
	h.prefix("b")
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	if v := f.values(); v["file"] != out || v["gzip"] != "true" {
		t.Fatalf("form %v", v)
	}
}

func TestM7RestoreIntoANewDatabase(t *testing.T) {
	fakeTool(t, "psql")
	fakeTool(t, "pg_restore")
	h := newHarness(t, nil)
	h.connect()
	dump := filepath.Join(t.TempDir(), "in.sql")
	writeTo(t, dump, "select 1;", 0o644)
	h.prefix("B")
	setField(t, h, "file", dump)
	setField(t, h, "newdb", "fresh")
	h.press("enter")
	h.want("First creates database fresh")
	h.press("y")
	if len(h.fake.applied) != 1 || h.fake.applied[0][0].Text != "-- create_database fresh" {
		t.Fatalf("applied %+v", h.fake.applied)
	}
	h.want("Restore in.sql") // then the restore runs against it
}

func TestM7ColumnLevelGrants(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.usersOn = true
	h.fake.ulist = []db.User{{Name: "ann", Grants: []db.Grant{{Object: "public.customers", Privs: []string{"SELECT"}, Columns: []string{"id", "name"}}}}}
	h.connect()
	h.prefix("u")
	h.press("down", "enter")
	h.pickLabel("Details")
	h.want("SELECT (id, name)")
	h.press("esc")
	h.prefix("u")
	h.press("down", "enter")
	h.pickLabel("Grant privileges")
	setField(t, h, "cols", "email, name")
	h.press("enter")
	h.press("enter") // apply the preview; the user list comes back
	last := h.fake.userChanges[len(h.fake.userChanges)-1]
	if last.Kind != "grant" || strings.Join(last.Grant.Columns, ",") != "email,name" {
		t.Fatalf("change %+v", last)
	}
	h.press("down", "enter")
	h.pickLabel("Revoke privileges")
	h.want("SELECT (id, name)")
	h.press("enter")
	if f := h.a.overlays[len(h.a.overlays)-1].(*formDlg); f.values()["cols"] != "id, name" {
		t.Fatalf("revoke form %v", f.values())
	}
}

func boardsDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

func TestM7BoardButtonRunsItsTargetAndDependents(t *testing.T) {
	boardsDir(t)
	err := board.Save(board.Board{Name: "ops", Panels: []board.Panel{
		{Title: "Total", Kind: "number", Query: "select 1"},
		{Title: "Refresh", Kind: "button", Target: "Total"},
		{Title: "Detail", Kind: "table", Query: "select * from customers", RefreshOn: "Total"},
		{Title: "Other", Kind: "table", Query: "select 2"},
		{Title: "Load", Kind: "import", Table: "orders"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, nil)
	h.connect()
	h.run("metrics")
	h.press("enter")
	h.want("Board ops")
	h.want("[ ↵ ] run Total")
	before := len(h.fake.execs)
	h.press("tab", "enter") // the Refresh button
	ran := h.fake.execs[before:]
	if len(ran) != 2 || ran[0] != "select 1" || ran[1] != "select * from customers" {
		t.Fatalf("a button runs its target and the panels that refresh on it: %v", ran)
	}
	before = len(h.fake.execs)
	h.press("tab", "tab") // Other: runs alone
	h.press("enter")
	if ran := h.fake.execs[before:]; len(ran) != 1 || ran[0] != "select 2" {
		t.Fatalf("single panel: %v", ran)
	}
	h.press("tab", "enter") // Load: the import wizard aimed at orders
	h.want("Import data")
	if f := h.a.overlays[len(h.a.overlays)-1].(*formDlg); f.values()["table"] != "orders" {
		t.Fatalf("import form %v", f.values())
	}
}

func TestM7BoardsNewRenameDelete(t *testing.T) {
	boardsDir(t)
	h := newHarness(t, nil)
	h.connect()
	h.run("metrics")
	h.press("n")
	setField(t, h, "name", "sales")
	setField(t, h, "title", "Rows")
	setField(t, h, "query", "select 1")
	h.press("enter")
	if _, err := os.Stat(filepath.Join(board.Dir(), "sales.toml")); err != nil {
		t.Fatal(err)
	}
	h.want("sales")
	h.press("e")
	setField(t, h, "name", "revenue")
	h.press("enter")
	if _, err := os.Stat(filepath.Join(board.Dir(), "sales.toml")); err == nil {
		t.Fatal("the old file goes after a rename")
	}
	if b, err := board.Load(filepath.Join(board.Dir(), "revenue.toml")); err != nil || b.Name != "revenue" || len(b.Panels) != 1 {
		t.Fatalf("%+v %v", b, err)
	}
	h.want("revenue")
	h.press("x")
	setField(t, h, "confirm", "nope")
	h.press("enter")
	h.want("type “revenue”")
	setField(t, h, "confirm", "revenue")
	h.press("enter")
	if bs, _ := board.List(); len(bs) != 0 {
		t.Fatalf("boards left: %+v", bs)
	}
}

func TestM7ProviderPickerPerChat(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) {
		c.AI.Provider, c.AI.Model = "anthropic", "claude-x"
		c.AI.Providers = map[string]config.AIProvider{"local": {Provider: "openai", Model: "llama", BaseURL: "http://localhost:11434/v1"}}
	})
	h.connect()
	h.run("assistant")
	at := h.a.ws().tab().(*assistantTab)
	if at.aiCfg().Model != "claude-x" {
		t.Fatalf("default model %q", at.aiCfg().Model)
	}
	h.press("ctrl+p")
	p := h.a.overlays[len(h.a.overlays)-1].(*pickerDlg)
	if !strings.Contains(p.items[0].Label, "default") || p.items[1].Label != "local" {
		t.Fatalf("default comes first: %+v", p.items)
	}
	h.pickLabel("local")
	if c := at.aiCfg(); c.Model != "llama" || c.Provider != "openai" || at.Title() != "AI assistant · local" {
		t.Fatalf("cfg %+v title %q", c, at.Title())
	}
}

func TestM7MCPServerGetsTheStores(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.MCP.Enabled, c.MCP.Listen = true, "127.0.0.1:0" })
	if h.a.mcp == nil || h.a.mcp.Queries != h.a.d.Queries || h.a.mcp.Hist != h.a.d.Hist || !h.a.mcp.Boards {
		t.Fatal("the new MCP tools need the saved queries, the history and the boards")
	}
	h.connect()
	h.run("mcp_log")
	h.want("Tools: list_connections")
}
