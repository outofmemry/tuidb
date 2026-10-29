package web

import (
	"strings"
	"testing"
)

func TestExplainCurrentStatement(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	x := e.ok("POST", "/api/sessions/"+id+"/explain", map[string]any{"text": "select 1; select * from t", "current": true, "offset": 15})
	if !strings.Contains(x["statement"].(string), "select * from t") || strings.Contains(x["statement"].(string), "select 1") {
		t.Fatalf("explain at the cursor: %v", x)
	}
}
