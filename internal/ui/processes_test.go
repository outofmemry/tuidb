package ui

import (
	"strings"
	"testing"
	"time"

	"webtuidb/internal/db"
)

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

func TestM2ProcessListKills(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.procs = []db.ProcessInfo{{ID: "42", User: "app", DB: "shop", State: "active", Elapsed: time.Second, Query: "select pg_sleep(99)"}}
	h.connect()
	h.prefix(".")
	h.want("Processes")
	h.want("pg_sleep")
	h.press("k")
	h.want("Kill process 42")
	h.press("y")
	if len(h.fake.killed) != 1 || h.fake.killed[0] != "42" {
		t.Fatalf("killed %v\n%s", h.fake.killed, h.screen())
	}
	h.want("no processes")
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
