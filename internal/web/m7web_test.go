package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/ui"
)

func TestSharedState(t *testing.T) {
	e := newEnv(t, "")
	e.ok("PUT", "/api/state/groups?conn=c1", map[string]any{"groups": []map[string]any{{"name": "Core", "items": []string{"public.t1"}}}})
	g := e.ok("GET", "/api/state/groups?conn=c1", nil)["groups"].([]any)
	if len(g) != 1 || g[0].(map[string]any)["name"] != "Core" {
		t.Fatalf("groups = %v", g)
	}
	if code, _ := e.call("PUT", "/api/state/groups?conn=c1", map[string]any{"groups": []map[string]any{{"name": "a"}, {"name": "a"}}}, "tok"); code != 400 {
		t.Fatalf("duplicate group names: %d", code)
	}
	if code, _ := e.call("PUT", "/api/state/recent-files", map[string]any{"name": "q.sql"}, "tok"); code != 400 { // a browser only knows names
		t.Fatalf("a bare name must be refused: %d", code)
	}
	e.s.o.State.AddRecentFile("/home/me/dir/q.sql", 5)
	e.ok("PUT", "/api/state/recent-files", map[string]any{"name": "/home/me/dir/q.sql"})
	if rf := e.ok("GET", "/api/state/recent-files", nil); len(rf["files"].([]any)) != 1 {
		t.Fatalf("recent = %v", rf)
	}
	e.ok("PUT", "/api/state/connection-sort", map[string]any{"sort": "tag"})
	if cs := e.ok("GET", "/api/state/connection-sort", nil); cs["sort"] != "tag" || cs["default"] != "name" {
		t.Fatalf("sort = %v", cs)
	}
	if code, _ := e.call("PUT", "/api/state/connection-sort", map[string]any{"sort": "nope"}, "tok"); code != 400 {
		t.Fatalf("bad sort: %d", code)
	}
	e.ok("PUT", "/api/state/backup-options?conn=c1", map[string]any{"options": map[string]string{"backup.gzip": "true", "evil": "x"}})
	e.ok("PUT", "/api/state/backup-options?conn=c1", map[string]any{"options": map[string]string{"restore.gzip": "false"}})
	if o := e.ok("GET", "/api/state/backup-options?conn=c1", nil)["options"].(map[string]any); o["backup.gzip"] != "true" || o["evil"] != nil {
		t.Fatalf("options = %v", o)
	}
}

func TestOpenRecentFileOnlyListedSQL(t *testing.T) {
	e := newEnv(t, "")
	dir := t.TempDir()
	sql, txt := filepath.Join(dir, "a.sql"), filepath.Join(dir, "a.txt")
	os.WriteFile(sql, []byte("select 1;"), 0o600)
	os.WriteFile(txt, []byte("x"), 0o600)
	e.s.o.State.AddRecentFile(sql, 10)
	e.s.o.State.AddRecentFile(txt, 10)
	e.s.o.State.AddRecentFile("rel.sql", 10)
	if r := e.ok("POST", "/api/files/open", map[string]any{"path": sql}); r["text"] != "select 1;" || r["name"] != "a.sql" {
		t.Fatalf("open = %v", r)
	}
	for _, p := range []string{txt, "rel.sql", "/etc/passwd", filepath.Join(dir, "other.sql")} {
		if code, _ := e.call("POST", "/api/files/open", map[string]any{"path": p}, "tok"); code != 400 {
			t.Errorf("%q: %d, want 400", p, code)
		}
	}
}

func TestSettingsHidesSecrets(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) {
		c.AI.APIKey = "sk-secret"
		c.Web.Password = "hunter2"
		c.AI.APIKeyCmd = "echo sk-cmd"
		c.AI.BaseURL = "http://user:pw@localhost:11434/v1"
		c.MCP.Tokens = []config.MCPToken{{Name: "t", Value: "tokvalue"}}
	})
	r := e.ok("GET", "/api/settings", nil)
	txt := r["text"].(string)
	for _, secret := range []string{"sk-secret", "hunter2", "tokvalue", "echo sk-", "user:pw@"} {
		if strings.Contains(txt, secret) {
			t.Errorf("settings leak %q", secret)
		}
	}
	if !strings.Contains(txt, "[general]") || !strings.HasSuffix(r["path"].(string), "config.toml") || r["readOnly"] != true {
		t.Fatalf("settings = %v", r)
	}
}

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

func TestToolPathsAreChecked(t *testing.T) {
	e := newEnv(t, "")
	conn := func(tools map[string]string) []map[string]any {
		return []map[string]any{{"Name": "c1", "Driver": "postgres", "Host": "h", "Database": "app", "Password": "pw", "Tools": tools}}
	}
	e.ok("PUT", "/api/connections", conn(map[string]string{"pg_dump": "/opt/pg16/bin/pg_dump-16"}))
	for _, bad := range []map[string]string{{"pg_dump": "/bin/sh"}, {"pg_dump": "pg_dump"}, {"sh": "/bin/sh"}} {
		if code, _ := e.call("PUT", "/api/connections", conn(bad), "tok"); code != 400 {
			t.Errorf("%v: %d, want 400", bad, code)
		}
	}
}

// the process list opens its own connection, once, and it is closed with the session
func TestProcessListUsesSideSession(t *testing.T) {
	e := newEnv(t, "")
	var opened atomic.Int32
	e.s.o.Connect = func(context.Context, config.Connection) (*ui.Opened, error) {
		opened.Add(1)
		return &ui.Opened{Sess: e.fake}, nil
	}
	id := e.open()
	for range 2 {
		if r := e.ok("GET", "/api/sessions/"+id+"/processes", nil); len(r["processes"].([]any)) != 1 {
			t.Fatalf("processes = %v", r)
		}
	}
	if opened.Load() != 2 {
		t.Fatalf("connections opened = %d, want the workspace's plus one side session", opened.Load())
	}
	e.ok("DELETE", "/api/sessions/"+id, nil)
}

func TestBoardEditing(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "")
	id := e.open()
	b := func(name string, panels ...map[string]any) map[string]any {
		return map[string]any{"Name": name, "RefreshSecs": 0, "Panels": panels}
	}
	n := map[string]any{"Title": "n", "Kind": "number", "Query": "select 1 as x"}
	btn := map[string]any{"Title": "go", "Kind": "button", "Target": "n"}
	imp := map[string]any{"Title": "load", "Kind": "import", "Table": "t"}
	dep := map[string]any{"Title": "d", "Kind": "table", "Query": "select 2 as x", "RefreshOn": "n"}
	e.ok("PUT", "/api/boards/new", b("one", n, btn, imp, dep))
	// button and import panels do not break a board run
	if r := e.ok("POST", "/api/sessions/"+id+"/boards/run", map[string]any{"Name": "one"}); len(r["panels"].([]any)) != 2 {
		t.Fatalf("run = %v", r)
	}
	p := e.ok("POST", "/api/sessions/"+id+"/boards/panel", map[string]any{"Name": "one", "Title": "n"})
	if p["panel"].(map[string]any)["number"] != "1" || p["dependents"].([]any)[0] != "d" {
		t.Fatalf("panel = %v", p)
	}
	for _, title := range []string{"go", "load", "missing"} {
		if code, _ := e.call("POST", "/api/sessions/"+id+"/boards/panel", map[string]any{"Name": "one", "Title": title}, "tok"); code != 400 {
			t.Errorf("panel %q: %d, want 400", title, code)
		}
	}
	e.ok("PUT", "/api/boards/one", b("two", n)) // rename: the old file goes
	if _, ok := boardNamed("one"); ok {
		t.Fatal("the old board file must be gone")
	}
	if _, ok := boardNamed("two"); !ok {
		t.Fatal("renamed board missing")
	}
	e.ok("PUT", "/api/boards/two", b("three", n))
	if code, _ := e.call("PUT", "/api/boards/three", b("three"), "tok"); code != 400 {
		t.Fatalf("a board needs a panel: %d", code)
	}
	if code, _ := e.call("PUT", "/api/boards/new", b("three", n), "tok"); code != 400 {
		t.Fatalf("a new board may not take an existing name: %d", code)
	}
	if code, _ := e.call("PUT", "/api/boards/three", b("three", map[string]any{"Title": "b", "Kind": "button", "Target": "zz"}), "tok"); code != 400 {
		t.Fatalf("a button needs a target: %d", code)
	}
	e.ok("DELETE", "/api/boards/three", nil)
	if _, ok := boardNamed("three"); ok {
		t.Fatal("delete did not remove the board")
	}
}

func TestAssistantProvider(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) {
		c.AI.Providers = map[string]config.AIProvider{"local": {Provider: "openai", BaseURL: "http://127.0.0.1:1/v1"}}
	})
	if m := e.ok("GET", "/api/meta", nil); m["ai"].(map[string]any)["providers"].([]any)[0] != "local" {
		t.Fatalf("meta ai = %v", m["ai"])
	}
	id := e.open()
	msgs := []map[string]any{{"role": "user", "content": "hi"}}
	if code, out := e.call("POST", "/api/sessions/"+id+"/assistant", map[string]any{"messages": msgs, "provider": "nope"}, "tok"); code != 400 || !strings.Contains(out["error"].(string), "unknown ai provider") {
		t.Fatalf("unknown provider: %d %v", code, out)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/assistant", map[string]any{"messages": msgs, "provider": "local"}, "tok"); code != 400 { // reaches the (dead) endpoint, which fails as a 400
		t.Fatalf("known provider: %d", code)
	}
}

type adminFake struct{ *fakeSess }

func (f *adminFake) PlanAdmin(_ db.AdminOp, ref db.ObjRef, _ map[string]string) ([]db.Statement, error) {
	return []db.Statement{{Text: "CREATE DATABASE " + ref.Name}}, nil
}

func TestRestoreIntoNewDatabase(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "alert1")
	e.s.o.Connect = func(context.Context, config.Connection) (*ui.Opened, error) {
		return &ui.Opened{Sess: &adminFake{e.fake}}, nil
	}
	bin := t.TempDir()
	tool := filepath.Join(bin, "pg_restore-16")
	os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	e.ok("PUT", "/api/connections", []map[string]any{{"Name": "c1", "Driver": "postgres", "Host": "h", "Database": "app", "Password": "pw", "SafeMode": "alert1", "Tools": map[string]string{"pg_restore": tool}}})
	id := e.open()
	body := map[string]any{"File": "x.dump", "Format": "custom", "NewDatabase": "fresh"}
	touchBackup(t, "x.dump")
	r := e.ok("POST", "/api/sessions/"+id+"/restore", body)
	if d := r["display"].(string); !strings.Contains(d, tool) { // the target database travels in the environment, not in the shown command
		t.Fatalf("display = %q", d)
	}
	if len(r["create"].([]any)) != 1 || r["gate"].(map[string]any)["confirm"] != true {
		t.Fatalf("review = %v", r)
	}
	body["Run"] = true
	if code, _ := e.call("POST", "/api/sessions/"+id+"/restore", body, "tok"); code != 409 || len(e.fake.applied) != 0 {
		t.Fatalf("an unconfirmed restore must not create anything: %d %v", code, e.fake.applied)
	}
	body["Confirmed"] = true
	e.ok("POST", "/api/sessions/"+id+"/restore", body)
	if len(e.fake.applied) != 1 || e.fake.applied[0][0].Text != "CREATE DATABASE fresh" {
		t.Fatalf("applied = %v", e.fake.applied)
	}
	if o := e.s.o.State.BackupOptions("c1"); o["restore.format"] != "custom" || o["backup.file"] != "" || o["restore.file"] != "" {
		t.Fatalf("remembered options = %v", o)
	}
}

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

func TestChartSeries(t *testing.T) {
	e := newEnv(t, "")
	s := func(v string) *string { return &v }
	r := e.ok("POST", "/api/chart", map[string]any{"Stmt": "-- barchart x: k, y: a, b", "Columns": []string{"k", "a", "b"},
		"Cells": [][]*string{{s("x"), s("1"), s("2")}, {s("y"), s("3"), s("4")}}})
	series := r["series"].([]any)
	if len(series) != 2 || series[1].(map[string]any)["name"] != "b" {
		t.Fatalf("series = %v", r)
	}
}

func TestMCPGetsStores(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) { c.MCP.Enabled = true })
	if e.s.mcp == nil || e.s.mcp.Queries == nil || e.s.mcp.Hist == nil || !e.s.mcp.Boards {
		t.Fatal("the MCP server must get the saved queries, the history and the boards")
	}
}

func TestAlterColumnPlansARename(t *testing.T) {
	e, rf := richEnv(t, nil)
	id := e.open()
	ref := map[string]any{"schema": "public", "name": "t", "kind": "table"}
	e.ok("POST", "/api/sessions/"+id+"/plan/table", map[string]any{"ref": ref, "op": "alter_column", "column": "name", "col": map[string]any{"Name": "title", "Type": "varchar(40)", "Nullable": false, "Default": "'x'", "HasDef": true}})
	c := rf.nw.Columns[1]
	if c.Name != "title" || c.OrigName != "name" || c.Type != "varchar(40)" || c.Nullable || !c.HasDef || c.Default != "'x'" || rf.old.Columns[1].Name != "name" {
		t.Fatalf("new column = %+v, old = %+v", c, rf.old.Columns[1])
	}
	for _, bad := range []map[string]any{{"Name": "id", "Type": "int"}, {"Name": "", "Type": "int"}, {"Name": "z", "Type": ""}} {
		if code, _ := e.call("POST", "/api/sessions/"+id+"/plan/table", map[string]any{"ref": ref, "op": "alter_column", "column": "name", "col": bad}, "tok"); code != 400 {
			t.Errorf("%v: %d, want 400", bad, code)
		}
	}
}

func TestSSHURLRoundTrip(t *testing.T) {
	e := newEnv(t, "")
	c := e.ok("POST", "/api/connections/parse", map[string]any{"url": "postgresql+ssh://u:p@db:5432/app?ssh_host=bastion&ssh_user=me&ssh_key=/k&ssh_port=2222&tLSMode=require"})
	ssh, _ := c["SSH"].(map[string]any)
	if ssh["Host"] != "bastion" || ssh["User"] != "me" || ssh["Key"] != "/k" || ssh["Port"] != float64(2222) || c["SSLMode"] != "require" {
		t.Fatalf("parsed = %v", c)
	}
	u := e.ok("POST", "/api/connections/format", c)["url"].(string)
	if !strings.Contains(u, "+ssh") || !strings.Contains(u, "ssh_host=bastion") || !strings.Contains(u, "2222") {
		t.Fatalf("formatted = %q", u)
	}
}

func TestSeveralObjectsOneAdminPlanAndSchemaOps(t *testing.T) {
	e := newEnv(t, "")
	e.s.o.Connect = func(context.Context, config.Connection) (*ui.Opened, error) {
		return &ui.Opened{Sess: &adminFake{e.fake}}, nil
	}
	id := e.open()
	p := e.ok("POST", "/api/sessions/"+id+"/plan/admin", map[string]any{"op": "drop", "refs": []map[string]any{{"name": "a", "kind": "table"}, {"name": "b", "kind": "table"}}})
	if len(p["statements"].([]any)) != 2 {
		t.Fatalf("plan = %v", p)
	}
	p = e.ok("POST", "/api/sessions/"+id+"/plan/admin", map[string]any{"op": "create_schema", "ref": map[string]any{"schema": "s1"}, "opts": map[string]string{"x": "y"}})
	if len(p["statements"].([]any)) != 1 {
		t.Fatalf("plan = %v", p)
	}
}

func TestParamFormatOverrideAndCurrentStatement(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	text := "select :a; select ${b} from t"
	r := e.ok("POST", "/api/params", map[string]any{"text": text, "dialect": "postgres", "paramFormat": "brace"})
	if p := r["params"].([]any); len(p) != 1 || p[0] != "b" {
		t.Fatalf("brace params = %v", p)
	}
	r = e.ok("POST", "/api/params", map[string]any{"text": text, "dialect": "postgres", "paramFormat": "colon", "current": true, "offset": 3})
	if p := r["params"].([]any); len(p) != 1 || p[0] != "a" {
		t.Fatalf("params of the first statement = %v", p)
	}
	code, out := e.call("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select ${b}", "paramFormat": "brace"}, "tok")
	if code != 422 || out["params"].([]any)[0] != "b" {
		t.Fatalf("exec with an overriding format: %d %v", code, out)
	}
	e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select ${b}", "paramFormat": "brace", "vars": map[string]string{"b": "1"}})
	if last := e.fake.execs[len(e.fake.execs)-1]; last != "select 1" {
		t.Fatalf("ran %q", last)
	}
}
