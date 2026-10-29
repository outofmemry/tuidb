package web

import (
	"strings"
	"testing"
)

func TestFiltersSQLAndColor(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	ref := map[string]any{"schema": "public", "name": "t", "kind": "table"}
	r := e.ok("POST", "/api/sessions/"+id+"/filters/sql", map[string]any{"ref": ref, "filters": map[string]any{"Filters": []map[string]any{{"Column": "name", "Op": "=", "Value": "bob", "Enabled": true}}}})
	if w := r["where"].(string); !strings.Contains(w, "name") || !strings.Contains(w, "'bob'") {
		t.Fatalf("where = %q", w)
	}
	if info := e.ok("POST", "/api/sessions/"+id+"/color", map[string]any{"color": "#aabbcc"}); info["color"] != "#aabbcc" {
		t.Fatalf("info = %v", info)
	}
	if cs, _ := e.s.findConn("c1"); cs.Color != "#aabbcc" {
		t.Fatalf("saved colour = %q", cs.Color)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/color", map[string]any{"color": "red; x"}, "tok"); code != 400 {
		t.Fatalf("bad colour: %d", code)
	}
	if i := e.ok("GET", "/api/sessions/"+id, nil); i["host"] != "h" || i["user"] == nil {
		t.Fatalf("details = %v", i)
	}
}
