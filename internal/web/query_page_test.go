package web

import (
	"strings"
	"testing"
)

func TestExecPage(t *testing.T) {
	e, rf := richEnv(t, nil)
	id := e.open()
	r := e.ok("POST", "/api/sessions/"+id+"/exec/page", map[string]any{"text": "select 1", "limit": 1, "offset": 5})
	if r["supported"] != true || r["more"] != true || !strings.Contains(rf.execs[len(rf.execs)-1], "OFFSET 5") {
		t.Fatalf("page = %v execs=%v", r, rf.execs)
	}
	if r := e.ok("POST", "/api/sessions/"+id+"/exec/page", map[string]any{"text": "delete from t", "limit": 1}); r["supported"] != false {
		t.Fatalf("a write must fall back: %v", r)
	}
}
