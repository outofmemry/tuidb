package ui

import (
	"testing"
)

func TestM6CloseOtherConnections(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.pump(h.a.openConnection(h.a.d.Conns[0]))
	if len(h.a.wss) != 2 {
		t.Fatalf("workspaces %d", len(h.a.wss))
	}
	h.run("close_other_connections")
	h.press("y")
	if len(h.a.wss) != 1 || h.a.active != 0 {
		t.Fatalf("workspaces %d active %d", len(h.a.wss), h.a.active)
	}
}
