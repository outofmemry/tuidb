package web

import (
	"testing"
)

func TestNegativeLimitIsClamped(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	ss := e.s.sessions[id]
	if got := ss.clampLimit(-1); got != ss.rowLimit() {
		t.Fatalf("clampLimit(-1) = %d, want %d", got, ss.rowLimit())
	}
	if got := ss.clampLimit(1 << 30); ss.rowLimit() > 0 && got != ss.rowLimit() {
		t.Fatalf("clampLimit(huge) = %d", got)
	}
}

func TestReapSkipsBusyAndJobs(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	ss := e.s.sessions[id]
	ss.mu.Lock()
	ss.busy = 1
	ss.mu.Unlock()
	e.s.reapOnce(-1)
	if e.s.sessions[id] == nil {
		t.Fatal("a session with a running handler was reaped")
	}
	ss.mu.Lock()
	ss.busy = 0
	ss.mu.Unlock()
	e.s.reapOnce(-1)
	if e.s.sessions[id] != nil {
		t.Fatal("an idle session should be reaped")
	}
}
