package ui

import (
	"testing"

	"webtuidb/internal/config"
)

func TestConnectIsGuardedAndCancellable(t *testing.T) {
	h := newHarness(t, nil)
	c := h.a.d.Conns[0]
	h.a.openConnection(c)
	if len(h.a.pending) != 1 {
		t.Fatal("the attempt is not tracked")
	}
	if cmd := h.a.openConnection(c); cmd == nil && len(h.a.pending) != 1 {
		t.Fatal("a second attempt must be ignored")
	}
	if h.a.cancelConnecting() != 1 {
		t.Fatal("cancel did not see the attempt")
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
