package web

import (
	"testing"
	"time"
)

func TestPinnedTabRunsOnItsOwnConnection(t *testing.T) {
	e, rf := richEnv(t, nil)
	id := e.open()
	run := func(pin string) {
		e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select 1", "pin": pin})
	}
	run("tab1")
	run("tab1")
	if rf.pins != 1 || len(rf.pinned.execs) != 2 || len(rf.fakeSess.execs) != 0 {
		t.Fatalf("pins=%d pinned execs=%v pool execs=%v", rf.pins, rf.pinned.execs, rf.fakeSess.execs)
	}
	run("")
	if len(rf.fakeSess.execs) != 1 {
		t.Fatal("an unpinned run goes to the pool")
	}
	e.ok("DELETE", "/api/sessions/"+id+"/pin/tab1", nil)
	run("tab1")
	if rf.pins != 2 {
		t.Fatalf("after unpin the next pinned run pins again: %d", rf.pins)
	}
	plain := newEnv(t, "") // fakeSess is not a Pinner
	if code, _ := plain.call("POST", "/api/sessions/"+plain.open()+"/exec", map[string]any{"text": "select 1", "pin": "t"}, "tok"); code != 400 {
		t.Fatalf("pin on an engine without Pinner: %d", code)
	}
}

func TestIdlePinIsSwept(t *testing.T) {
	e, _ := richEnv(t, nil)
	id := e.open()
	e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select 1", "pin": "tab1"})
	ss := e.s.sessions[id]
	ss.sweepPins(time.Hour)
	if len(ss.pins) != 1 {
		t.Fatal("a fresh pin must stay")
	}
	time.Sleep(2 * time.Millisecond)
	ss.sweepPins(time.Millisecond)
	if len(ss.pins) != 0 {
		t.Fatal("an idle pin must be released")
	}
}
