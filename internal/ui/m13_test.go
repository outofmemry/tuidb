package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/ai"
	"webtuidb/internal/board"
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/store"
)

func TestM13DiagramAddForeignKeyPlansAndPreviews(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("d")
	h.want("a: add FK")
	h.press("a")
	h.want("Add foreign key")
	setChoice(t, h, "table", "orders")
	setField(t, h, "cols", "id")
	setChoice(t, h, "ref", "customers")
	setField(t, h, "refcols", "id")
	setChoice(t, h, "del", "CASCADE")
	h.press("enter")
	h.want("Preview: 1 statement")
	nd := h.fake.lastNew
	if nd == nil || len(nd.ForeignKeys) != 1 || nd.ForeignKeys[0].RefTable != "customers" || nd.ForeignKeys[0].OnDelete != "CASCADE" {
		t.Fatalf("planned %+v", nd)
	}
	if len(h.fake.applied) != 0 {
		t.Fatal("nothing is sent before the preview is accepted")
	}
	h.press("enter")
	if len(h.fake.applied) != 1 {
		t.Fatalf("applied %+v", h.fake.applied)
	}
}

func TestM13DiagramColourIsRemembered(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("d")
	h.press("c")
	h.pickLabel("customers")
	h.pickLabel("red")
	if got := h.a.d.State.DiagramColors("shop")["public.customers"]; got != "203" {
		t.Fatalf("colours %v", got)
	}
}

func TestM13MySQLLimitsChange(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.dialect = "mysql"
	h.fake.usersOn, h.fake.ulist = true, []db.User{{Name: "ann", Host: "%", Limits: map[string]int{"MAX_USER_CONNECTIONS": 2}}}
	h.connect()
	h.prefix("u")
	h.press("down", "enter")
	h.pickLabel("Resource limits")
	h.want("Resource limits of ann@%")
	setField(t, h, "MAX_QUERIES_PER_HOUR", "500")
	h.press("enter")
	var got *db.UserChange
	for i := range h.fake.userChanges {
		if h.fake.userChanges[i].Kind == "limits" {
			got = &h.fake.userChanges[i]
		}
	}
	if got == nil || got.User.Limits["MAX_QUERIES_PER_HOUR"] != 500 || got.User.Limits["MAX_USER_CONNECTIONS"] != 2 {
		t.Fatalf("changes %+v", h.fake.userChanges)
	}
}

func TestM13ChecklistUncheckAll(t *testing.T) {
	c := &checklistDlg{items: []checkItem{{Label: "a", Checked: true}, {Label: "b"}}}
	c.Update(keyMsg("A"))
	if c.items[0].Checked || c.items[1].Checked {
		t.Fatal("A unchecks all")
	}
	c.Update(keyMsg("a"))
	if !c.items[0].Checked || !c.items[1].Checked {
		t.Fatal("a checks all")
	}
}

func TestM13ExporterPanelExportsItsRows(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	err := board.Save(board.Board{Name: "ops", Panels: []board.Panel{
		{Title: "Rows", Kind: "exporter", Query: "select * from customers", Format: "json"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, nil)
	h.connect()
	h.run("metrics")
	h.press("enter")
	h.want("export json")
	h.press("enter")
	h.want("Export Rows")
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	if f.values()["format"] != "json" {
		t.Fatalf("format not preselected: %v", f.values())
	}
	out := filepath.Join(t.TempDir(), "rows.json")
	setField(t, h, "path", out)
	h.wait = time.Second
	h.press("enter")
	b, err := os.ReadFile(out)
	if err != nil || !strings.Contains(string(b), "Ann") {
		t.Fatalf("export file %q: %v", b, err)
	}
}

func TestM13InputPanelSetsItsVariable(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	err := board.Save(board.Board{Name: "ops", Panels: []board.Panel{
		{Title: "Who", Kind: "input", Var: "who", Default: "x"},
		{Title: "People", Kind: "table", Query: "select * from customers where name = '$who'"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, nil)
	h.connect()
	h.run("metrics")
	h.press("enter")
	h.want("$who = x")
	h.press("enter")
	setField(t, h, "v", "ann")
	h.press("enter")
	h.want("$who = ann")
}

func TestM13NewChatSavesTheOldOne(t *testing.T) {
	h := newHarness(t, nil)
	chats := &store.Chats{Dir: t.TempDir()}
	h.a.d.Chats = chats
	h.connect()
	w := h.a.ws()
	w.m3.chat = append(w.m3.chat, aiMsg("user", "how many customers?"), aiMsg("assistant", "three"))
	h.run("assistant_new")
	list, _ := chats.List("shop")
	if len(list) != 1 || list[0].Title != "how many customers?" || list[0].Count != 2 {
		t.Fatalf("saved chats %+v", list)
	}
	if len(w.m3.chat) != 0 || w.m3.curChat != nil {
		t.Fatal("the new chat starts empty")
	}
	// history loads it back, clear keeps the id but empties the messages
	h.run("assistant_history")
	h.want("how many customers?")
	h.press("enter")
	if len(w.m3.chat) != 2 || w.m3.curChat == nil {
		t.Fatalf("loaded %d messages", len(w.m3.chat))
	}
	id := w.m3.curChat.ID
	h.run("assistant_clear")
	got, err := chats.Load("shop", id)
	if err != nil || len(got.Messages) != 0 || len(w.m3.chat) != 0 {
		t.Fatalf("cleared chat %+v %v", got, err)
	}
}

func TestM13ClosePaneKeepsEveryTab(t *testing.T) {
	h := newHarness(t, nil)
	w := twoTabs(h)
	h.prefix(`"`)
	if !w.m3.split {
		t.Fatal("split first")
	}
	h.run("close_pane")
	if w.m3.split || len(w.tabs) != 2 || len(w.groupIdx(0)) != 2 {
		t.Fatalf("after close_pane: split %v tabs %d", w.m3.split, len(w.tabs))
	}
	h.run("close_pane")
	h.want("one pane")
}

func TestM13ProcessListMarksAndKillsSeveral(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.procs = []db.ProcessInfo{
		{ID: "7", User: "ann", DB: "shop", Query: "select 1", Elapsed: 5 * time.Second},
		{ID: "8", User: "bob", DB: "shop", Query: "select 2", Elapsed: 4 * time.Second},
	}
	h.connect()
	h.run("process_list")
	h.want("select 2")
	h.press("space", "space")
	h.want("2 marked")
	h.press("k")
	h.want("Kill 2 processes?")
	h.press("y")
	if len(h.fake.killed) != 2 {
		t.Fatalf("killed %v", h.fake.killed)
	}
}

func TestM13ProcessListSortReverses(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.procs = []db.ProcessInfo{{ID: "7", User: "ann"}, {ID: "8", User: "bob"}}
	h.connect()
	h.run("process_list")
	h.press("s")
	h.pickLabel("user")
	h.want("sorted by user ↑")
	h.press("s")
	h.pickLabel("user")
	h.want("sorted by user ↓")
	h.press("q")
	if len(h.a.overlays) != 0 {
		t.Fatal("q dismisses the process list")
	}
}

func TestM13ProcessAsFormats(t *testing.T) {
	ps := []db.ProcessInfo{{ID: "7", User: "ann", Query: "select  1"}}
	if got := processAs(ps, FmtCSVHeader); !strings.HasPrefix(got, "id,user,db,state,elapsed,query\n7,ann,") || !strings.HasSuffix(got, "select 1") {
		t.Fatalf("csv %q", got)
	}
	if got := processAs(ps, FmtJSON); !strings.Contains(got, `"user": "ann"`) {
		t.Fatalf("json %q", got)
	}
	if got := processAs(ps, FmtMarkdown); !strings.Contains(got, "| id | user |") || !strings.Contains(got, "| 7 | ann |") {
		t.Fatalf("md %q", got)
	}
}

func TestM13ToggleExtraAddsAndRemovesFlags(t *testing.T) {
	got := toggleExtra("--clean -j 4", []string{"--clean", "-j 4", "--if-exists"}, []bool{false, true, true})
	if got != "-j 4 --if-exists" {
		t.Fatalf("got %q", got)
	}
}

func TestM13PluginBindingsAndDetail(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeTo(t, filepath.Join(config.ConfigDir(), "plugins", "hi.toml"), "name = \"hi\"\ncommand = \"echo\"\nkey = \"alt+P\"\n", 0o644)
	bs := pluginBindings()
	if len(bs) != 1 || bs[0].Action != "plugin:hi" || bs[0].Key != "alt+P" {
		t.Fatalf("bindings %+v", bs)
	}
}

func aiMsg(role, content string) ai.Message { return ai.Message{Role: role, Content: content} }

func TestM13PostgresConnectionLimit(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.usersOn, h.fake.ulist = true, []db.User{{Name: "ann", Limits: map[string]int{"CONNECTION_LIMIT": -1}}}
	h.connect()
	h.prefix("u")
	h.press("down", "enter")
	h.pickLabel("Connection limit")
	h.want("Connection limit of ann")
	setField(t, h, "n", "7")
	h.press("enter")
	var got *db.UserChange
	for i := range h.fake.userChanges {
		if h.fake.userChanges[i].Kind == "limits" {
			got = &h.fake.userChanges[i]
		}
	}
	if got == nil || got.User.Limits["CONNECTION_LIMIT"] != 7 {
		t.Fatalf("changes %+v", h.fake.userChanges)
	}
}
