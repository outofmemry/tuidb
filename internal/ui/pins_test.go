package ui

import (
	"testing"
)

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
