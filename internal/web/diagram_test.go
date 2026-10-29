package web

import (
	"testing"
)

func TestDiagram(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	d := e.ok("GET", "/api/sessions/"+id+"/diagram", nil)
	tabs := d["tables"].([]any)
	if len(tabs) != 2 || len(tabs[0].(map[string]any)["columns"].([]any)) != 2 {
		t.Fatalf("diagram: %v", d)
	}
}
