package web

import (
	"context"
	"sync/atomic"
	"testing"
	"webtuidb/internal/config"
	"webtuidb/internal/ui"
)

func TestProcessesKillGated(t *testing.T) {
	e := newEnv(t, "alert2")
	id := e.open()
	p := e.ok("GET", "/api/sessions/"+id+"/processes", nil)
	if p["supported"] != true || len(p["processes"].([]any)) != 1 {
		t.Fatalf("processes: %v", p)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/processes/kill", map[string]any{"ID": "7"}, "tok"); code != 409 || len(e.fake.killed) != 0 {
		t.Fatalf("an unconfirmed kill must be refused: %d", code)
	}
	e.ok("POST", "/api/sessions/"+id+"/processes/kill", map[string]any{"ID": "7", "Confirmed": true})
	if len(e.fake.killed) != 1 {
		t.Fatal("kill did not run")
	}
}

// the process list opens its own connection, once, and it is closed with the session
func TestProcessListUsesSideSession(t *testing.T) {
	e := newEnv(t, "")
	var opened atomic.Int32
	e.s.o.Connect = func(context.Context, config.Connection) (*ui.Opened, error) {
		opened.Add(1)
		return &ui.Opened{Sess: e.fake}, nil
	}
	id := e.open()
	for range 2 {
		if r := e.ok("GET", "/api/sessions/"+id+"/processes", nil); len(r["processes"].([]any)) != 1 {
			t.Fatalf("processes = %v", r)
		}
	}
	if opened.Load() != 2 {
		t.Fatalf("connections opened = %d, want the workspace's plus one side session", opened.Load())
	}
	e.ok("DELETE", "/api/sessions/"+id, nil)
}
