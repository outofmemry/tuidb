package web

import (
	"strings"
	"testing"
)

func TestLabelsAndSummaryTab(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	r := e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "-- tab: Totals\nselect 1;"})
	if it := r["results"].([]any)[0].(map[string]any); it["label"] != "Totals" {
		t.Fatalf("label = %v", it)
	}
	many := strings.Repeat("select 1;\n", 101) + "select boom;"
	r = e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": many})
	res := r["results"].([]any)
	if len(res) != 1 {
		t.Fatalf("more than 100 statements give one tab, got %d", len(res))
	}
	it := res[0].(map[string]any)
	if it["label"] != "Summary" || !strings.Contains(it["message"].(string), "102 statements run, 1 failed, 0 rows affected, 101 rows returned") {
		t.Fatalf("summary = %v", it)
	}
}
