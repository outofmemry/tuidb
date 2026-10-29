package web

import (
	"context"
	"strings"
	"testing"
	"webtuidb/internal/config"
	"webtuidb/internal/ui"
)

func TestUsersPlanGoesThroughApply(t *testing.T) {
	e := newEnv(t, "alert1")
	id := e.open()
	if u := e.ok("GET", "/api/sessions/"+id+"/users", nil); u["supported"] != true || len(u["users"].([]any)) != 1 {
		t.Fatalf("users: %v", u)
	}
	p := e.ok("POST", "/api/sessions/"+id+"/users/plan", map[string]any{"Kind": "create", "User": map[string]any{"Name": "amy"}})
	shown := p["statements"].([]any)[0].(map[string]any)["display"].(string)
	if strings.Contains(shown, "secret") || len(e.fake.applied) != 0 {
		t.Fatalf("plan must redact and not run: %v", p)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/apply", map[string]any{"planId": p["planId"]}, "tok"); code != 409 {
		t.Fatalf("apply must be gated: %d", code)
	}
}

func TestSeveralObjectsOneAdminPlanAndSchemaOps(t *testing.T) {
	e := newEnv(t, "")
	e.s.o.Connect = func(context.Context, config.Connection) (*ui.Opened, error) {
		return &ui.Opened{Sess: &adminFake{e.fake}}, nil
	}
	id := e.open()
	p := e.ok("POST", "/api/sessions/"+id+"/plan/admin", map[string]any{"op": "drop", "refs": []map[string]any{{"name": "a", "kind": "table"}, {"name": "b", "kind": "table"}}})
	if len(p["statements"].([]any)) != 2 {
		t.Fatalf("plan = %v", p)
	}
	p = e.ok("POST", "/api/sessions/"+id+"/plan/admin", map[string]any{"op": "create_schema", "ref": map[string]any{"schema": "s1"}, "opts": map[string]string{"x": "y"}})
	if len(p["statements"].([]any)) != 1 {
		t.Fatalf("plan = %v", p)
	}
}
