package web

import (
	"strings"
	"testing"
	"webtuidb/internal/config"
)

func TestVarsEmptyObjectStillPrompts(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	code, out := e.call("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select :id", "vars": map[string]string{}, "paramFormat": "colon"}, "tok")
	if code != 422 || out["params"] == nil {
		t.Fatalf("got %d %v", code, out)
	}
}

func TestReadOnlyConnectionRefusesWrites(t *testing.T) {
	e := newEnv(t, "")
	e.s.o.Conns[0].ReadOnly = true
	id := e.open()
	rows := e.ok("POST", "/api/sessions/"+id+"/rows", map[string]any{"ref": map[string]any{"name": "t", "kind": "table"}})
	if rows["editable"] == true {
		t.Fatal("rows of a read-only connection must not be editable")
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "delete from t"}, "tok"); code != 403 {
		t.Fatalf("exec write: %d", code)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/tools/bulk-update", map[string]any{"ref": map[string]any{"name": "t"}, "set": []any{map[string]any{"Col": "a", "Val": "1"}}}, "tok"); code == 200 {
		t.Fatalf("bulk update accepted")
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select 1"}, "tok"); code != 200 {
		t.Fatalf("read refused: %d", code)
	}
}

func TestLogOriginalValues(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) { c.General.LogOriginalValues = true })
	id := e.open()
	rows := e.ok("POST", "/api/sessions/"+id+"/rows", map[string]any{"ref": map[string]string{"Name": "t", "Kind": "table"}})
	p := e.ok("POST", "/api/sessions/"+id+"/plan", map[string]any{"resultId": rows["resultId"], "deletes": []int{0}})
	e.ok("POST", "/api/sessions/"+id+"/apply", map[string]any{"planId": p["planId"], "confirmed": true})
	h, _ := e.hist.List("c1", "", 0)
	if len(h) != 1 || !strings.Contains(h[0].Original, "delete") {
		t.Fatalf("history must carry the pre-image: %+v", h)
	}
}

// W-P2-01/02: the row limit is in /api/meta, and /rows says when the clamp cut the client's request short.
func TestRowLimitExposed(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) { c.General.RowLimit = 3 })
	if m := e.ok("GET", "/api/meta", nil); m["rowLimit"] != float64(3) {
		t.Errorf("meta rowLimit %v", m["rowLimit"])
	}
	id := e.open()
	ref := map[string]any{"name": "t", "kind": "table"}
	for _, c := range []struct {
		limit int
		want  bool
	}{{0, true}, {1000, true}, {3, false}} {
		if out := e.ok("POST", "/api/sessions/"+id+"/rows", map[string]any{"ref": ref, "limit": c.limit}); out["truncated"] != c.want {
			t.Errorf("limit %d: truncated %v", c.limit, out["truncated"])
		}
	}
}
