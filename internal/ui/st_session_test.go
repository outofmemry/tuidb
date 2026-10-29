package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/db"
)

// T-P2-04 / T-P2-09: the sidebar's schema switch changes the session's current schema.
func TestFlowSchemaSwitchSetsSessionSchema(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.pump(h.a.ws().sb.pickSchema())
	h.pickLabel("app")
	if h.fake.schema != "app" || h.a.ws().sb.schema != "app" {
		t.Fatalf("session schema %q, sidebar %q", h.fake.schema, h.a.ws().sb.schema)
	}
	h.want("shop.app")
}

func runSQL(h *harness, sql string) {
	h.t.Helper()
	h.queryTab().ed.SetText(sql)
	h.prefix("enter")
}

// T-P1-03 / T-P2-05: BEGIN on an unpinned tab keeps one connection until COMMIT, the tab shows [tx], and closing it
// with the transaction open asks first.
func TestFlowBeginPinsUntilCommitAndCloseAsks(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	runSQL(h, "begin")
	if len(h.fake.pins) != 1 || q.pin == nil || !q.autoPin || !q.inTx {
		t.Fatalf("begin must pin: pins=%d pin=%v auto=%v tx=%v", len(h.fake.pins), q.pin, q.autoPin, q.inTx)
	}
	h.want("[tx]")
	h.want("until COMMIT or ROLLBACK")
	runSQL(h, "update customers set name = 'x' where id = 1")
	if p := h.fake.pins[0]; len(p.execs) != 2 || p.execs[1] != "update customers set name = 'x' where id = 1" {
		t.Fatalf("the update must run on the pinned connection: %v", p.execs)
	}
	runSQL(h, "commit")
	if q.pin != nil || q.inTx || !h.fake.pins[0].closed {
		t.Fatalf("commit must release the automatic pin: pin=%v tx=%v", q.pin, q.inTx)
	}
	h.wantNot("[tx]")
	runSQL(h, "begin")
	h.prefix("w")
	h.want("open transaction. Close it and roll back?")
	h.press("y")
	if !h.fake.pins[1].closed || len(h.a.ws().tabs) != 0 {
		t.Fatalf("closing must roll back: closed=%v tabs=%d", h.fake.pins[1].closed, len(h.a.ws().tabs))
	}
}

// T-P1-04: the "taking too long" prompt closes when the statement finishes, and its result is shown.
func TestFlowSlowPromptClosesWhenDone(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	_ = q.start([]db.Stmt{{Text: "select 1"}}) // not pumped: the statement "runs" until we deliver its result
	h.pump(q.Handle(queryTimeoutMsg{batch: q.batch}))
	h.want("taking too long")
	w := h.a.ws()
	h.a.Update(wsMsg{ws: w.id, tab: q.ID(), gen: w.gen, msg: queryDoneMsg{batch: q.batch, stmt: db.Stmt{Text: "select 1"}, res: &db.Result{Affected: 1}}})
	if len(h.a.overlays) != 0 || q.running || len(q.res) != 1 || q.res[0].isErr {
		t.Fatalf("prompt must close and the result show: overlays=%d running=%v res=%d", len(h.a.overlays), q.running, len(q.res))
	}
	h.wantNot("taking too long")
	h.wantNot("cancelled")
}

// T-P2-10: Enter in the Variables dialog saves the values and runs with them bound, even with query_params off;
// a later run with the values unbound says how to turn parameters on.
func TestFlowVariablesDialogRuns(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.queryTab().ed.SetText("select * from customers where id = :id")
	h.run("variables")
	h.want("Variables")
	h.typeText("2")
	h.press("enter")
	if n := len(h.fake.execs); n == 0 || strings.Contains(h.fake.execs[n-1], ":id") || !strings.Contains(h.fake.execs[n-1], "2") {
		t.Fatalf("the statement must run with :id bound: %v", h.fake.execs)
	}
	h.prefix("enter")
	h.want("Parameters are off")
}

// T-P2-11: a tab opened from a .sql file shows ● when edited and asks to save on close.
func TestFlowFileTabAsksToSave(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	path := filepath.Join(t.TempDir(), "q1.sql")
	if err := os.WriteFile(path, []byte("select 1;"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.pump(h.a.openSQLFile(path))
	q := h.queryTab()
	if strings.Contains(q.Title(), "●") {
		t.Fatal("an unchanged file is not marked")
	}
	h.typeText(" -- edited")
	if !strings.Contains(q.Title(), "●") {
		t.Fatalf("an edited file is marked: %q", q.Title())
	}
	h.prefix("w")
	h.want("Save changes to q1.sql?")
	h.pickLabel("Save")
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "-- edited") || len(h.a.ws().tabs) != 0 {
		t.Fatalf("save and close: file %q tabs %d", b, len(h.a.ws().tabs))
	}
}
