package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/ui"
)

// richFake adds what the wave C endpoints need on top of fakeSess: foreign keys, column order and pinning.
type richFake struct {
	*fakeSess
	old, nw *db.TableDef
	pinned  *fakeSess
	pins    int
}

func (f *richFake) Caps() db.Capabilities {
	c := f.fakeSess.Caps()
	c.ForeignKeys, c.ReorderColumns = true, true
	return c
}
func (f *richFake) ForeignKeys(context.Context, db.ObjRef) ([]db.ForeignKey, error) { return nil, nil }
func (f *richFake) PlanTable(old, nw *db.TableDef) ([]db.Statement, error) {
	f.old, f.nw = old, nw
	return []db.Statement{{Text: "ALTER TABLE t"}}, nil
}
func (f *richFake) Pin(context.Context) (db.Session, error) { f.pins++; return f.pinned, nil }

func richEnv(t *testing.T, mutate func(*config.Config)) (*env, *richFake) {
	e := newEnvCfg(t, "", mutate)
	rf := &richFake{fakeSess: e.fake, pinned: &fakeSess{}}
	e.s.o.Connect = func(_ context.Context, c config.Connection) (*ui.Opened, error) { return &ui.Opened{Sess: rf}, nil }
	return e, rf
}

func TestMetaExtrasAndSessionInfo(t *testing.T) {
	e, _ := richEnv(t, func(c *config.Config) {
		c.Tags = []config.Tag{{Name: "qa", Color: "#112233"}}
		c.MCP.Listen = "0.0.0.0:9000"
	})
	m := e.ok("GET", "/api/meta", nil)
	b, _ := json.Marshal(m)
	for _, want := range []string{`"qa"`, `#112233`, `"listen":"0.0.0.0:9000"`, `"fileBased":[]`, `"production"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("meta lacks %s: %s", want, b)
		}
	}
	for _, d := range m["drivers"].([]any) {
		if d == "sqlite" || d == "duckdb" || d == "cockroach" || d == "d1" {
			t.Errorf("removed driver %v still listed", d)
		}
	}
	info := e.ok("GET", "/api/sessions/"+e.open(), nil)
	if info["readOnly"] != false || info["pinnable"] != true {
		t.Errorf("info = %v", info)
	}
}

func TestPlanTableEdit(t *testing.T) {
	e, rf := richEnv(t, nil)
	id := e.open()
	ref := map[string]any{"schema": "public", "name": "t", "kind": "table"}
	e.ok("POST", "/api/sessions/"+id+"/plan/table", map[string]any{"ref": ref, "op": "move_column", "column": "name", "to": 0})
	if got := rf.nw.Columns; got[0].Name != "name" || got[1].Name != "id" || rf.old.Columns[0].Name != "id" || got[0].OrigName != "name" || got[1].OrigName != "id" {
		t.Fatalf("columns after move = %v, before = %v", got, rf.old.Columns)
	}
	e.ok("POST", "/api/sessions/"+id+"/plan/table", map[string]any{"ref": ref, "op": "add_fk", "fk": map[string]any{"Columns": []string{"id"}, "RefTable": "u", "RefColumns": []string{"id"}}})
	if len(rf.nw.ForeignKeys) != 1 || rf.nw.ForeignKeys[0].Name != "fk_t_id" || rf.nw.ForeignKeys[0].OnDelete != "NO ACTION" || len(rf.old.ForeignKeys) != 0 {
		t.Fatalf("fk = %+v", rf.nw.ForeignKeys)
	}
	for _, bad := range []map[string]any{
		{"ref": ref, "op": "move_column", "column": "nope", "to": 0},
		{"ref": ref, "op": "move_column", "column": "id", "to": 0},
		{"ref": ref, "op": "add_fk", "fk": map[string]any{"Columns": []string{"zzz"}, "RefTable": "u", "RefColumns": []string{"id"}}},
		{"ref": ref, "op": "add_fk", "fk": map[string]any{"Columns": []string{"id"}, "RefColumns": []string{"id"}}},
		{"ref": ref, "op": "bogus"},
	} {
		if code, _ := e.call("POST", "/api/sessions/"+id+"/plan/table", bad, "tok"); code != 400 {
			t.Errorf("%v: %d, want 400", bad, code)
		}
	}
}

func TestPinnedTabRunsOnItsOwnConnection(t *testing.T) {
	e, rf := richEnv(t, nil)
	id := e.open()
	run := func(pin string) {
		e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select 1", "pin": pin})
	}
	run("tab1")
	run("tab1")
	if rf.pins != 1 || len(rf.pinned.execs) != 2 || len(rf.fakeSess.execs) != 0 {
		t.Fatalf("pins=%d pinned execs=%v pool execs=%v", rf.pins, rf.pinned.execs, rf.fakeSess.execs)
	}
	run("")
	if len(rf.fakeSess.execs) != 1 {
		t.Fatal("an unpinned run goes to the pool")
	}
	e.ok("DELETE", "/api/sessions/"+id+"/pin/tab1", nil)
	run("tab1")
	if rf.pins != 2 {
		t.Fatalf("after unpin the next pinned run pins again: %d", rf.pins)
	}
	plain := newEnv(t, "") // fakeSess is not a Pinner
	if code, _ := plain.call("POST", "/api/sessions/"+plain.open()+"/exec", map[string]any{"text": "select 1", "pin": "t"}, "tok"); code != 400 {
		t.Fatalf("pin on an engine without Pinner: %d", code)
	}
}

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

func TestExportOptionsAndFileName(t *testing.T) {
	e, _ := richEnv(t, func(c *config.Config) { c.Export.Filename = "{connection}-{table}" })
	id := e.open()
	resp := e.post("/api/sessions/"+id+"/export", map[string]any{"mode": "query", "format": "csv", "text": "select 1", "nullAs": "∅"})
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "c1-result.csv") {
		t.Fatalf("pattern name: %q", cd)
	}
	resp = e.post("/api/sessions/"+id+"/export", map[string]any{"mode": "query", "format": "csv", "text": "select 1", "fileName": "a/b.csv"})
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "a_b.csv") {
		t.Fatalf("typed name: %q", cd)
	}
}

func TestAssistantModelsAndBoardDuplicate(t *testing.T) {
	prov := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "no", 404)
			return
		}
		io.WriteString(w, `{"data":[{"id":"b"},{"id":"a"}]}`)
	}))
	defer prov.Close()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e, _ := richEnv(t, func(c *config.Config) {
		c.AI = config.AI{Provider: "openai", APIKey: "k", BaseURL: prov.URL, Model: "a"}
	})
	m := e.ok("GET", "/api/assistant/models", nil)
	if l := m["models"].([]any); len(l) != 2 || l[0] != "a" || m["current"] != "a" {
		t.Fatalf("models = %v", m)
	}
	e.ok("POST", "/api/boards", map[string]any{"Name": "b1", "Query": "select 1 as x", "Kind": "bar", "X": "x", "Y": "x"})
	e.ok("POST", "/api/boards/duplicate", map[string]any{"Name": "b1", "NewName": "b2"})
	if code, _ := e.call("POST", "/api/boards/duplicate", map[string]any{"Name": "b1", "NewName": "b2"}, "tok"); code != 400 {
		t.Fatalf("an existing board must not be overwritten: %d", code)
	}
	resp := e.raw("GET", "/api/boards", nil, nil)
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), `"b2"`) {
		t.Fatalf("boards = %s", b)
	}
}

func TestCheckExpr(t *testing.T) {
	for _, ok := range []string{"NOW()", "now() + interval '1 day'", "nextval('public.seq'::regclass)", "'it''s;fine'", `"a;b"`, "gen_random_uuid()"} {
		if err := checkExpr(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "1); DROP TABLE t; --", "1; select 2", "a -- b", "a /* b */", "(1", "'open", "$$x$$"} {
		if checkExpr(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestExplainCurrentStatement(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	x := e.ok("POST", "/api/sessions/"+id+"/explain", map[string]any{"text": "select 1; select * from t", "current": true, "offset": 15})
	if !strings.Contains(x["statement"].(string), "select * from t") || strings.Contains(x["statement"].(string), "select 1") {
		t.Fatalf("explain at the cursor: %v", x)
	}
}

func TestIdlePinIsSwept(t *testing.T) {
	e, _ := richEnv(t, nil)
	id := e.open()
	e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select 1", "pin": "tab1"})
	ss := e.s.sessions[id]
	ss.sweepPins(time.Hour)
	if len(ss.pins) != 1 {
		t.Fatal("a fresh pin must stay")
	}
	time.Sleep(2 * time.Millisecond)
	ss.sweepPins(time.Millisecond)
	if len(ss.pins) != 0 {
		t.Fatal("an idle pin must be released")
	}
}

func TestAssistantModelsDropsKeyOnCrossHostRedirect(t *testing.T) {
	var leaked string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("x-api-key")
		io.WriteString(w, `{"data":[{"id":"a"}]}`)
	}))
	defer other.Close()
	prov := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/v1/models", http.StatusFound)
	}))
	defer prov.Close()
	e, _ := richEnv(t, func(c *config.Config) {
		c.AI = config.AI{Provider: "anthropic", APIKey: "secret", BaseURL: prov.URL}
	})
	e.ok("GET", "/api/assistant/models", nil)
	if leaked != "" {
		t.Fatalf("x-api-key leaked to another host: %q", leaked)
	}
}
