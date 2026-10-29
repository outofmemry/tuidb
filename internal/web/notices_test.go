package web

import (
	"strings"
	"testing"
)

func TestNoticesAndExplain(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	r := e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "do raise"})
	nid := r["results"].([]any)[0].(map[string]any)["noticeId"].(string)
	n := e.ok("GET", "/api/sessions/"+id+"/notices/"+nid, nil)
	ns := n["notices"].([]any)
	if n["message"] != "DO" || len(ns) != 2 || ns[0].(map[string]any)["severity"] != "NOTICE" || ns[1].(map[string]any)["message"] != "careful" {
		t.Fatalf("notices: %v", n)
	}
	if code, _ := e.call("GET", "/api/sessions/"+id+"/notices/zzz", nil, "tok"); code != 400 {
		t.Fatalf("unknown result: %d", code)
	}
	x := e.ok("POST", "/api/sessions/"+id+"/explain", map[string]any{"text": "select * from t"})
	nodes := x["nodes"].([]any)
	if len(nodes) != 1 || !strings.Contains(nodes[0].(map[string]any)["label"].(string), "Seq Scan") || !strings.HasPrefix(x["statement"].(string), "EXPLAIN") {
		t.Fatalf("explain: %v", x)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/explain", map[string]any{"text": "select 1; select 2"}, "tok"); code != 400 {
		t.Fatalf("one statement only: %d", code)
	}
}
