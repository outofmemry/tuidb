package web

import (
	"testing"
)

func TestSafeModeChangeWithoutPasswordNeedsConfirm(t *testing.T) {
	e := newEnv(t, "safe1")
	id := e.open()
	e.s.sessions[id].o.Password = ""
	if code, out := e.call("POST", "/api/sessions/"+id+"/safe-mode", map[string]any{"level": "silent"}, "tok"); code != 409 || out["gate"] == nil {
		t.Fatalf("got %d %v", code, out)
	}
	e.ok("POST", "/api/sessions/"+id+"/safe-mode", map[string]any{"level": "silent", "confirmed": true})
}

func TestSafeModeEndpoint(t *testing.T) {
	e := newEnv(t, "safe1")
	id := e.open()
	if code, _ := e.call("POST", "/api/sessions/"+id+"/safe-mode", map[string]any{"level": "silent"}, "tok"); code != 409 {
		t.Fatalf("leaving a password level without the password: %d", code)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/safe-mode", map[string]any{"level": "silent", "password": "pw"}, "tok"); code != 200 {
		t.Fatalf("with the password: %d", code)
	}
	if got := e.ok("GET", "/api/sessions/"+id, nil)["level"]; got != "silent" {
		t.Fatalf("level = %v", got)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/safe-mode", map[string]any{"level": "bogus"}, "tok"); code != 400 {
		t.Fatalf("bad level: %d", code)
	}
}
