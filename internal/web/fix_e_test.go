package web

import (
	"strings"
	"testing"
)

func TestQueryUpdateAndDeleteGuards(t *testing.T) {
	e := newEnv(t, "")
	e.ok("POST", "/api/queries", map[string]any{"folder": "reports", "name": "daily", "sql": "select 1"})
	p := "/api/queries/reports/daily.sql"
	if code, _ := e.call("PUT", p, nil, "tok"); code != 400 {
		t.Fatalf("empty body: %d", code)
	}
	if code, _ := e.call("PUT", p, map[string]any{"sqll": "x"}, "tok"); code != 400 {
		t.Fatalf("missing sql: %d", code)
	}
	e.ok("PUT", p, map[string]any{"sql": "select 2"})
	if code, _ := e.call("DELETE", "/api/queries/reports/", nil, "tok"); code != 400 {
		t.Fatalf("folder delete: %d", code)
	}
	req := e.raw("PUT", p, strings.NewReader(`{"sql":"select 3"}`), map[string]string{"If-Match": `"stale"`})
	if req.StatusCode != 409 {
		t.Fatalf("stale If-Match: %d", req.StatusCode)
	}
	e.ok("DELETE", p, nil)
}

func TestHistoryClearNeedsAll(t *testing.T) {
	e := newEnv(t, "")
	if code, _ := e.call("DELETE", "/api/history?conn=c1", nil, "tok"); code != 400 {
		t.Fatalf("clear without all: %d", code)
	}
}

func TestTableStateMerges(t *testing.T) {
	e := newEnv(t, "")
	q := "/api/state/table?conn=c1&table=t"
	if code, _ := e.call("PUT", q, nil, "tok"); code != 400 {
		t.Fatalf("empty body: %d", code)
	}
	e.ok("PUT", q, map[string]any{"widths": map[string]int{"a": 9}, "hidden": []string{"x"}})
	e.ok("PUT", q, map[string]any{"show_filters": true})
	got := e.ok("GET", q, nil)
	if got["show_filters"] != true || got["widths"] == nil || got["hidden"] == nil {
		t.Fatalf("partial PUT lost fields: %v", got)
	}
}

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

func TestNegativeLimitIsClamped(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	ss := e.s.sessions[id]
	if got := ss.clampLimit(-1); got != ss.rowLimit() {
		t.Fatalf("clampLimit(-1) = %d, want %d", got, ss.rowLimit())
	}
	if got := ss.clampLimit(1 << 30); ss.rowLimit() > 0 && got != ss.rowLimit() {
		t.Fatalf("clampLimit(huge) = %d", got)
	}
}

func TestExprNeedsMacroOrAdvanced(t *testing.T) {
	var v Val
	if err := v.UnmarshalJSON([]byte(`{"$expr":"(select pg_sleep(300))"}`)); err == nil {
		t.Fatal("arbitrary expression accepted")
	}
	if err := v.UnmarshalJSON([]byte(`{"$expr":"(select 1)","$advanced":"true"}`)); err != nil {
		t.Fatal(err)
	}
	if err := v.UnmarshalJSON([]byte(`{"$expr":"now()"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestSafeModeChangeWithoutPasswordNeedsConfirm(t *testing.T) {
	e := newEnv(t, "safe1")
	id := e.open()
	e.s.sessions[id].o.Password = ""
	if code, out := e.call("POST", "/api/sessions/"+id+"/safe-mode", map[string]any{"level": "silent"}, "tok"); code != 409 || out["gate"] == nil {
		t.Fatalf("got %d %v", code, out)
	}
	e.ok("POST", "/api/sessions/"+id+"/safe-mode", map[string]any{"level": "silent", "confirmed": true})
}

func TestBoardKeepsConnectionPin(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "")
	n := map[string]any{"Title": "n", "Kind": "number", "Query": "select 1 as x"}
	e.ok("PUT", "/api/boards/new", map[string]any{"Name": "b", "Connection": "prod", "Panels": []any{n}})
	e.ok("PUT", "/api/boards/b", map[string]any{"Name": "b", "Connection": "", "Panels": []any{n}})
	b, ok := boardNamed("b")
	if !ok || b.Connection != "prod" || boardJSON(b)["connection"] != "prod" {
		t.Fatalf("pin lost: %+v", b)
	}
}

func TestReapSkipsBusyAndJobs(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	ss := e.s.sessions[id]
	ss.mu.Lock()
	ss.busy = 1
	ss.mu.Unlock()
	e.s.reapOnce(-1)
	if e.s.sessions[id] == nil {
		t.Fatal("a session with a running handler was reaped")
	}
	ss.mu.Lock()
	ss.busy = 0
	ss.mu.Unlock()
	e.s.reapOnce(-1)
	if e.s.sessions[id] != nil {
		t.Fatal("an idle session should be reaped")
	}
}
