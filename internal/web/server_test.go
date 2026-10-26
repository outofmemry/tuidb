package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/store"
	"webtuidb/internal/ui"
)

// fakeSess implements just the Session methods the API touches; the rest panic if called.
type fakeSess struct {
	db.Session
	applied [][]db.Statement
	changes []db.Change
	execs   []string
	killed  []string
	capsFn  func() db.Capabilities // overrides Caps (the M14 capability gating test)
}

func (f *fakeSess) Caps() db.Capabilities {
	if f.capsFn != nil {
		return f.capsFn()
	}
	return db.Capabilities{Dialect: "postgres", Schemas: true, Editor: "sql", Transactions: true, ProcessList: true, Users: true}
}
func (f *fakeSess) Objects(_ context.Context, schema string) ([]db.Object, error) {
	if schema == "other" {
		return []db.Object{{ObjRef: db.ObjRef{Schema: "other", Name: "t1", Kind: db.KindTable}}, {ObjRef: db.ObjRef{Schema: "other", Name: "t3", Kind: db.KindTable}}}, nil
	}
	return []db.Object{{ObjRef: db.ObjRef{Schema: "public", Name: "t1", Kind: db.KindTable}}, {ObjRef: db.ObjRef{Schema: "public", Name: "t2", Kind: db.KindTable}}}, nil
}
func (f *fakeSess) Schemas(context.Context) ([]string, error) {
	return []string{"public", "other", "pg_catalog"}, nil
}
func (f *fakeSess) Count(_ context.Context, q db.RowQuery, _ bool) (db.Count, error) {
	if q.ObjRef.Name == "t2" {
		return db.Count{N: 2, Exact: true}, nil
	}
	return db.Count{Exact: true}, nil
}
func (f *fakeSess) DDL(context.Context, db.ObjRef) (string, error) {
	return "CREATE TABLE t (id int);", nil
}
func (f *fakeSess) Processes(context.Context) ([]db.ProcessInfo, error) {
	return []db.ProcessInfo{{ID: "7", User: "u", DB: "app", State: "active", Query: "select 1"}}, nil
}
func (f *fakeSess) Kill(_ context.Context, id string) error {
	f.killed = append(f.killed, id)
	return nil
}
func (f *fakeSess) ServerInfo() string                          { return "fake 1" }
func (f *fakeSess) Ping(context.Context) error                  { return nil }
func (f *fakeSess) Close() error                                { return nil }
func (f *fakeSess) CurrentDatabase() string                     { return "app" }
func (f *fakeSess) CurrentSchema() string                       { return "public" }
func (f *fakeSess) Databases(context.Context) ([]string, error) { return []string{"app"}, nil }
func (f *fakeSess) Columns(_ context.Context, ref db.ObjRef) ([]db.Column, error) {
	return []db.Column{{Name: "id", Type: "int", PK: true}, {Name: "name", Type: "text", Nullable: true}}, nil
}
func (f *fakeSess) Indexes(context.Context, db.ObjRef) ([]db.Index, error) { return nil, nil }
func (f *fakeSess) PlanTable(old, nw *db.TableDef) ([]db.Statement, error) {
	if old == nil {
		return []db.Statement{{Text: "CREATE " + nw.Name}}, nil
	}
	return nil, nil
}
func (f *fakeSess) Rows(_ context.Context, q db.RowQuery) (*db.Result, error) {
	if q.ObjRef.Name == "d2" {
		return &db.Result{Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}}, Rows: [][]any{{int64(1), "ann2"}, {int64(2), nil}}, HasRows: true}, nil
	}
	return &db.Result{
		Columns: []db.ResultColumn{{Name: "id", Type: "int"}, {Name: "name", Type: "text"}},
		Rows:    [][]any{{int64(1), "ann"}, {int64(2), nil}, {int64(3), []byte{0xde, 0xad}}},
		HasRows: true,
	}, nil
}
func (f *fakeSess) Exec(_ context.Context, stmt string, limit int) (*db.Result, error) {
	f.execs = append(f.execs, stmt)
	if strings.Contains(stmt, "boom") {
		return nil, fmt.Errorf("boom")
	}
	if strings.HasPrefix(stmt, "EXPLAIN") {
		return &db.Result{Columns: []db.ResultColumn{{Name: "QUERY PLAN"}}, Rows: [][]any{{`[{"Plan":{"Node Type":"Seq Scan","Relation Name":"t","Total Cost":1.5,"Plan Rows":3}}]`}}, HasRows: true}, nil
	}
	if strings.Contains(stmt, "raise") {
		return &db.Result{Message: "DO\nNOTICE: hello\nWARNING: careful"}, nil
	}
	if stmt == "select * from t1" {
		return &db.Result{Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}}, Rows: [][]any{{int64(1), "ann"}}, HasRows: true}, nil
	}
	if strings.HasPrefix(strings.ToLower(stmt), "select") {
		return &db.Result{Columns: []db.ResultColumn{{Name: "x"}}, Rows: [][]any{{int64(1)}}, HasRows: true}, nil
	}
	return &db.Result{Affected: 1, Message: "1 row affected"}, nil
}
func (f *fakeSess) Plan(ch []db.Change) ([]db.Statement, error) {
	f.changes = ch
	var out []db.Statement
	for _, c := range ch {
		out = append(out, db.Statement{Text: fmt.Sprintf("kind%d %s", c.Kind, c.Name)})
	}
	return out, nil
}
func (f *fakeSess) Apply(_ context.Context, s []db.Statement) error {
	f.applied = append(f.applied, s)
	return nil
}

type env struct {
	t    *testing.T
	srv  *httptest.Server
	fake *fakeSess
	hist *store.History
	s    *Server
}

func newEnv(t *testing.T, safe string) *env { return newEnvCfg(t, safe, nil) }

func newEnvCfg(t *testing.T, safe string, mutate func(*config.Config)) *env {
	t.Helper()
	fake := &fakeSess{}
	dir := t.TempDir()
	st, err := store.LoadState(dir + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	if mutate != nil {
		mutate(&cfg)
	}
	s := New(Options{
		Cfg: cfg, ConnPath: dir + "/connections.toml", State: st,
		Hist: &store.History{Dir: dir + "/history"}, Queries: &store.Queries{Root: dir + "/queries"},
		Token: "tok",
		Conns: []config.Connection{{Name: "c1", Driver: config.Postgres, Host: "h", Database: "app", Password: "pw", SafeMode: safe}},
		Connect: func(ctx context.Context, c config.Connection) (*ui.Opened, error) {
			return &ui.Opened{Sess: fake, Password: c.Password}, nil
		},
	})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &env{t: t, srv: ts, fake: fake, hist: s.o.Hist, s: s}
}

func (e *env) call(method, path string, body any, token string) (int, map[string]any) {
	e.t.Helper()
	var rd bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = *bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, &rd)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (e *env) ok(method, path string, body any) map[string]any {
	e.t.Helper()
	code, out := e.call(method, path, body, "tok")
	if code != 200 {
		e.t.Fatalf("%s %s: %d %v", method, path, code, out)
	}
	return out
}

func (e *env) open() string {
	return e.ok("POST", "/api/sessions", map[string]any{"name": "c1"})["id"].(string)
}

func TestAuth(t *testing.T) {
	e := newEnv(t, "")
	for _, tok := range []string{"", "wrong"} {
		if code, _ := e.call("GET", "/api/connections", nil, tok); code != 401 {
			t.Fatalf("token %q: got %d, want 401", tok, code)
		}
	}
	if code, _ := e.call("GET", "/api/connections", nil, "tok"); code != 200 {
		t.Fatalf("good token rejected: %d", code)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/connections", nil)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Origin", "http://evil.example")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 403 {
		t.Fatalf("foreign origin: got %d, want 403", resp.StatusCode)
	}
}

func TestRowsFormatsCells(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	out := e.ok("POST", "/api/sessions/"+id+"/rows", map[string]any{"ref": map[string]any{"name": "t", "kind": "table"}})
	cells := out["cells"].([]any)
	if cells[1].([]any)[1] != nil {
		t.Fatalf("SQL NULL must be JSON null, got %v", cells[1])
	}
	if got := cells[2].([]any)[1]; got != `\xdead` {
		t.Fatalf("bytes rendered as %v", got)
	}
	if out["editable"] != true || out["resultId"] == "" {
		t.Fatalf("table rows should be editable and cached: %v", out)
	}
	cols := out["columns"].([]any)
	if cols[0].(map[string]any)["pk"] != true {
		t.Fatalf("PK flag missing: %v", cols)
	}
}

func TestPlanApplyAndGate(t *testing.T) {
	e := newEnv(t, "safe2")
	id := e.open()
	rows := e.ok("POST", "/api/sessions/"+id+"/rows", map[string]any{"ref": map[string]any{"name": "t", "kind": "table"}})
	plan := e.ok("POST", "/api/sessions/"+id+"/plan", map[string]any{
		"resultId": rows["resultId"],
		"updates":  []any{map[string]any{"row": 0, "set": map[string]any{"name": map[string]any{"$": "default"}}}},
		"deletes":  []any{1},
		"inserts":  []any{map[string]any{"name": map[string]any{"$expr": "NOW()"}}},
	})
	ch := e.fake.changes
	if len(ch) != 3 || ch[0].Kind != db.Delete || ch[1].Kind != db.Update || ch[2].Kind != db.Insert {
		t.Fatalf("want delete, update, insert; got %+v", ch)
	}
	if ch[1].Set[0].Val != db.Default || ch[2].Set[0].Val != db.Expr("NOW()") {
		t.Fatalf("special values lost: %+v", ch)
	}
	if ch[1].Key[0].Col != "id" || ch[1].Key[0].Val != int64(1) {
		t.Fatalf("key must be the original PK value: %+v", ch[1].Key)
	}
	pid := plan["planId"]
	// Safe Mode 2 + a write: password required, and the server enforces it
	code, out := e.call("POST", "/api/sessions/"+id+"/apply", map[string]any{"planId": pid}, "tok")
	if code != 409 || out["gate"].(map[string]any)["password"] != true {
		t.Fatalf("apply without password: %d %v", code, out)
	}
	code, _ = e.call("POST", "/api/sessions/"+id+"/apply", map[string]any{"planId": pid, "password": "nope"}, "tok")
	if code != 409 || len(e.fake.applied) != 0 {
		t.Fatalf("wrong password must not apply (code %d, applied %d)", code, len(e.fake.applied))
	}
	e.ok("POST", "/api/sessions/"+id+"/apply", map[string]any{"planId": pid, "password": "pw"})
	if len(e.fake.applied) != 1 {
		t.Fatal("not applied")
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/apply", map[string]any{"planId": pid, "password": "pw"}, "tok"); code != 400 {
		t.Fatalf("a plan must apply only once, got %d", code)
	}
}

func TestNoPKRejectsUpdateDeleteButAllowsInsert(t *testing.T) {
	c := &cached{
		ref: db.ObjRef{Name: "t"}, cols: []db.Column{{Name: "a"}, {Name: "b"}},
		res: &db.Result{Columns: []db.ResultColumn{{Name: "a"}, {Name: "b"}}, Rows: [][]any{{"x", nil}}},
	}
	if _, err := buildChanges(c, editReq{Deletes: []int{0}}); err == nil {
		t.Fatal("delete without a PK accepted")
	}
	if _, err := buildChanges(c, editReq{Inserts: []map[string]Val{{"a": {V: "1"}}}}); err != nil {
		t.Fatalf("insert needs no key: %v", err)
	}
}

func TestHiddenPKOnlyBlocksKeyedChanges(t *testing.T) {
	c := &cached{
		ref: db.ObjRef{Name: "t"}, cols: []db.Column{{Name: "id", PK: true}, {Name: "n"}},
		res: &db.Result{Columns: []db.ResultColumn{{Name: "n"}}, Rows: [][]any{{"x"}}},
	}
	if _, err := buildChanges(c, editReq{Inserts: []map[string]Val{{"n": {V: "1"}}}}); err != nil {
		t.Fatalf("insert-only plan with hidden PK rejected: %v", err)
	}
	if _, err := buildChanges(c, editReq{Deletes: []int{0}}); err == nil {
		t.Fatal("delete with hidden PK accepted")
	}
}

func TestUpdateRowRequiredAndKeyVerified(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	rows := e.ok("POST", "/api/sessions/"+id+"/rows", map[string]any{"ref": map[string]any{"name": "t", "kind": "table"}})
	plan := func(body map[string]any) int {
		body["resultId"] = rows["resultId"]
		code, _ := e.call("POST", "/api/sessions/"+id+"/plan", body, "tok")
		return code
	}
	set := map[string]any{"name": "z"}
	if code := plan(map[string]any{"updates": []any{map[string]any{"set": set}}}); code != 400 {
		t.Fatalf("update without row accepted: %d", code)
	}
	// the client saw id=2 at row 0, but the snapshot now has id=1 there
	if code := plan(map[string]any{"updates": []any{map[string]any{"row": 0, "key": map[string]any{"id": "2"}, "set": set}}}); code != 400 {
		t.Fatalf("mismatched key accepted: %d", code)
	}
	if code := plan(map[string]any{"deletes": []any{0}, "deleteKeys": []any{map[string]any{"id": "2"}}}); code != 400 {
		t.Fatalf("mismatched delete key accepted: %d", code)
	}
	if code := plan(map[string]any{"updates": []any{map[string]any{"row": 0, "key": map[string]any{"id": "1"}, "set": set}}}); code != 200 {
		t.Fatalf("matching key rejected: %d", code)
	}
	if code := plan(map[string]any{"inserts": []any{map[string]any{"name": map[string]any{"$expr": "1); DROP TABLE t; --"}}}}); code != 400 {
		t.Fatalf("arbitrary $expr accepted: %d", code)
	}
}

func TestDeleteQueriesRootAndEmptyConnections(t *testing.T) {
	e := newEnv(t, "")
	for _, p := range []string{"/api/queries/", "/api/queries/a/..", "/api/queries/."} {
		if code, _ := e.call("DELETE", p, nil, "tok"); code != 400 {
			t.Fatalf("DELETE %s: got %d, want 400", p, code)
		}
	}
	if code, _ := e.call("PUT", "/api/connections", nil, "tok"); code != 400 { // empty body
		t.Fatalf("empty PUT accepted: %d", code)
	}
	req, _ := http.NewRequest("PUT", e.srv.URL+"/api/connections", strings.NewReader("null"))
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 400 {
		t.Fatalf("null PUT accepted: %v %v", resp, err)
	}
}

func TestExecSplitsGatesAndRecordsHistory(t *testing.T) {
	e := newEnv(t, "alert2")
	id := e.open()
	// read-only batch passes straight through
	out := e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select 1; select 2"})
	if n := len(out["results"].([]any)); n != 2 {
		t.Fatalf("want 2 results, got %d", n)
	}
	// a write needs confirmation, and nothing runs before it
	before := len(e.fake.execs)
	code, g := e.call("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "delete from t"}, "tok")
	if code != 409 || g["gate"].(map[string]any)["confirm"] != true || len(e.fake.execs) != before {
		t.Fatalf("unconfirmed write: %d %v", code, g)
	}
	out = e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "delete from t; select boom; select 3", "confirmed": true})
	res := out["results"].([]any)
	if len(res) != 3 || res[1].(map[string]any)["error"] != "boom" || res[2].(map[string]any)["error"] != nil {
		t.Fatalf("later statements must still run: %v", res)
	}
	h, _ := e.hist.List("c1", "", 0)
	if len(h) != 4 { // select 1, select 2, delete, select 3
		t.Fatalf("history has %d entries, want 4 successful statements", len(h))
	}
}

func TestExecReturnOnError(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) { c.Editor.ReturnOnError = true })
	id := e.open()
	out := e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select 1; select boom; select 3"})
	if res := out["results"].([]any); len(res) != 2 {
		t.Fatalf("must stop at the first error: %v", res)
	}
}

func TestPutConnectionsValidates(t *testing.T) {
	e := newEnv(t, "")
	code, _ := e.call("PUT", "/api/connections", []config.Connection{{Name: "x", Driver: "nope"}}, "tok")
	if code != 400 {
		t.Fatalf("invalid driver accepted: %d", code)
	}
	dup := config.Connection{Name: "d", Driver: config.Postgres, Host: "h", Database: "app"}
	if code, _ := e.call("PUT", "/api/connections", []config.Connection{dup, dup}, "tok"); code != 400 {
		t.Fatalf("duplicate names accepted: %d", code)
	}
	e.ok("PUT", "/api/connections", []config.Connection{dup})
}

func TestStaticFallback(t *testing.T) {
	e := newEnv(t, "")
	resp, err := http.Get(e.srv.URL + "/s/anything")
	if err != nil {
		t.Fatal(err)
	}
	// no index.html is embedded in a source checkout: a clear 503, not a crash or a 404
	if resp.StatusCode != 503 && resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestConnURL(t *testing.T) {
	e := newEnv(t, "")
	for _, tc := range []struct {
		url, driver, host, user, pw, db string
		port                            float64
	}{
		{"postgresql://u:p%40w@h:5432/d?sslmode=require", "postgres", "h", "u", "p@w", "d", 5432},
		{"mysql://root:s@db.local:3307/shop", "mysql", "db.local", "root", "s", "shop", 3307},
		{"redshift://u:p@rh:5439/dw", "redshift", "rh", "u", "p", "dw", 5439},
		{"mongodb://m:q@mh:27017/logs", "mongodb", "mh", "m", "q", "logs", 27017},
	} {
		out := e.ok("POST", "/api/connections/parse", map[string]string{"url": tc.url})
		if out["Driver"] != tc.driver || out["Host"] != tc.host || out["User"] != tc.user || out["Password"] != tc.pw || out["Database"] != tc.db {
			t.Errorf("%s -> %v", tc.url, out)
		}
		if p, _ := out["Port"].(float64); p != tc.port {
			t.Errorf("%s port %v", tc.url, out["Port"])
		}
		back := e.ok("POST", "/api/connections/format", out)["url"].(string)
		again := e.ok("POST", "/api/connections/parse", map[string]string{"url": back})
		if again["Host"] != out["Host"] || again["Password"] != out["Password"] || again["Database"] != out["Database"] {
			t.Errorf("round trip %s -> %s -> %v", tc.url, back, again)
		}
	}
	code, out := e.call("POST", "/api/connections/parse", map[string]string{"url": "not a url:pw@@"}, "tok")
	if code != 400 || out["error"] == nil || strings.Contains(fmt.Sprint(out["error"]), "pw@@") {
		t.Errorf("garbage: %d %v", code, out)
	}
	for _, p := range []string{"parse", "format"} {
		if code, _ := e.call("POST", "/api/connections/"+p, map[string]string{"url": "mysql://h/d"}, ""); code != 401 {
			t.Errorf("%s without token: %d", p, code)
		}
	}
}

func TestMetaKeymap(t *testing.T) {
	e := newEnv(t, "")
	km := e.ok("GET", "/api/meta", nil)["keymap"].(map[string]any)
	if km["prefix"] != "ctrl+d" {
		t.Fatalf("prefix = %v, want ctrl+d", km["prefix"])
	}
	var found bool
	for _, b := range km["bindings"].([]any) {
		m := b.(map[string]any)
		if m["Action"] == "help" && m["Key"] == "?" && m["Context"] == "global" {
			found = true
		}
	}
	if !found {
		t.Fatal("help binding missing from meta keymap")
	}
}
