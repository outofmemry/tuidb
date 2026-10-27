package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/mcp"
)

func (f *fakeSess) Users(context.Context) ([]db.User, error) {
	return []db.User{{Name: "bob", Attrs: []string{"LOGIN"}}}, nil
}
func (f *fakeSess) PlanUser(c db.UserChange) ([]db.Statement, error) {
	return []db.Statement{{Text: "CREATE ROLE " + c.User.Name + " PASSWORD 'secret'", Display: "CREATE ROLE " + c.User.Name + " PASSWORD '***'"}}, nil
}

func TestUsersPlanGoesThroughApply(t *testing.T) {
	e := newEnv(t, "alert1")
	id := e.open()
	if u := e.ok("GET", "/api/sessions/"+id+"/users", nil); u["supported"] != true || len(u["users"].([]any)) != 1 {
		t.Fatalf("users: %v", u)
	}
	p := e.ok("POST", "/api/sessions/"+id+"/users/plan", map[string]any{"Kind": "create", "User": map[string]any{"Name": "amy"}})
	shown := p["statements"].([]any)[0].(map[string]any)["display"].(string)
	if strings.Contains(shown, "secret") || len(e.fake.applied) != 0 {
		t.Fatalf("plan must redact and not run: %v", p)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/apply", map[string]any{"planId": p["planId"]}, "tok"); code != 409 {
		t.Fatalf("apply must be gated: %d", code)
	}
}

func TestBackupFileConfined(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	for _, f := range []string{"/etc/passwd", "../x.sql", "a/b.sql", "/tmp/x.sql"} {
		if _, _, err := backupFile(f); err == nil {
			t.Fatalf("%q must be refused", f)
		}
	}
	p, ex, err := backupFile("x.sql")
	if err != nil || ex || filepath.Dir(p) != filepath.Join(config.DataDir(), "backups") {
		t.Fatalf("name inside the dir: %q %v %v", p, ex, err)
	}
	os.WriteFile(p, nil, 0o600)
	if _, ex, _ := backupFile("x.sql"); !ex {
		t.Fatal("an existing file must be reported")
	}
	os.Symlink("/etc/hosts", filepath.Join(filepath.Dir(p), "l.sql"))
	if _, _, err := backupFile("l.sql"); err == nil {
		t.Fatal("a symlink must be refused")
	}
}

func TestBackupDisplayRedactedAndRestoreGated(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "alert1")
	id := e.open()
	if m := e.ok("POST", "/api/sessions/"+id+"/backup", map[string]any{"File": "x.sql"}); m["tool"].(map[string]any)["Found"] == true && m["display"] == "" {
		t.Fatalf("a found tool must give a command: %v", m)
	}
	bin := t.TempDir() // stub tools so the test does not depend on a database client install
	for _, n := range []string{"pg_dump", "pg_restore", "psql"} {
		os.WriteFile(filepath.Join(bin, n), []byte("#!/bin/sh\n"), 0o755)
	}
	t.Setenv("PATH", bin)
	b := e.ok("POST", "/api/sessions/"+id+"/backup", map[string]any{"File": "x.sql"})
	if d := b["display"].(string); !strings.Contains(d, "pg_dump") || strings.Contains(d, "pw") || b["tool"].(map[string]any)["Name"] != "pg_dump" {
		t.Fatalf("backup: %v", b)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/backup", map[string]any{}, "tok"); code != 400 {
		t.Fatalf("file required: %d", code)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/restore", map[string]any{"File": "x.sql"}, "tok"); code != 400 {
		t.Fatalf("restoring a missing file must be refused: %d", code)
	}
	touchBackup(t, "x.sql")
	r := e.ok("POST", "/api/sessions/"+id+"/restore", map[string]any{"File": "x.sql"})
	if r["gate"].(map[string]any)["confirm"] != true {
		t.Fatalf("restore must show the gate: %v", r)
	}
	code, _ := e.call("POST", "/api/sessions/"+id+"/restore", map[string]any{"File": "x.sql", "Run": true}, "tok")
	if code != 409 {
		t.Fatalf("an unconfirmed restore must not start: %d", code)
	}
}

func TestDiagram(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	d := e.ok("GET", "/api/sessions/"+id+"/diagram", nil)
	tabs := d["tables"].([]any)
	if len(tabs) != 2 || len(tabs[0].(map[string]any)["columns"].([]any)) != 2 {
		t.Fatalf("diagram: %v", d)
	}
}

func TestBoardsPluginsMCP(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	e := newEnvCfg(t, "", func(c *config.Config) { c.MCP.Enabled = true })
	id := e.open()
	e.ok("POST", "/api/boards", map[string]any{"Name": "b1", "Query": "select 1 as x", "Kind": "bar", "X": "x", "Y": "x"})
	e.ok("POST", "/api/boards", map[string]any{"Name": "b1", "Query": "select 2 as x", "Kind": "bar"}) // appends, never overwrites
	r := e.ok("POST", "/api/sessions/"+id+"/boards/run", map[string]any{"Name": "b1"})
	if p := r["panels"].([]any); len(p) != 2 {
		t.Fatalf("both panels expected: %v", r)
	}
	e.ok("POST", "/api/boards", map[string]any{"Name": "w", "Query": "delete from t"})
	if code, _ := e.call("POST", "/api/sessions/"+id+"/boards/run", map[string]any{"Name": "w"}, "tok"); code != 400 {
		t.Fatalf("a write panel must be refused: %d", code)
	}

	pd := filepath.Join(cfgDir, "webtuidb", "plugins")
	os.MkdirAll(pd, 0o755)
	os.WriteFile(filepath.Join(pd, "echo.toml"), []byte("name = \"echo\"\ncommand = \"echo\"\nargs = [\"hi\"]\ncontexts = [\"editor\"]\n"), 0o644)
	pr := e.ok("POST", "/api/sessions/"+id+"/plugins/run", map[string]any{"Name": "echo"})
	if strings.TrimSpace(pr["text"].(string)) != "hi" || pr["output"] != "text" {
		t.Fatalf("plugin: %v", pr)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/plugins/run", map[string]any{"Name": "rm"}, "tok"); code != 400 {
		t.Fatalf("unknown plugin: %d", code)
	}

	if m := e.ok("GET", "/api/mcp/log", nil); m["enabled"] != true {
		t.Fatalf("mcp: %v", m)
	}
}

func TestAssistant(t *testing.T) {
	var sys string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 1<<16)
		n, _ := r.Body.Read(b)
		sys = string(b[:n])
		w.Write([]byte(`{"choices":[{"message":{"content":"Try:\n` + "```sql\\nselect 1\\n```" + `"}}]}`))
	}))
	defer llm.Close()
	e := newEnvCfg(t, "", func(c *config.Config) {
		c.AI = config.AI{Provider: "openai", Model: "m", APIKey: "k", BaseURL: llm.URL}
	})
	id := e.open()
	r := e.ok("POST", "/api/sessions/"+id+"/assistant", map[string]any{"messages": []map[string]string{{"role": "user", "content": "count rows in @t1"}}})
	bl := r["blocks"].([]any)
	if len(bl) != 1 || bl[0].(map[string]any)["sql"] != "select 1" || bl[0].(map[string]any)["readOnly"] != true || !strings.Contains(sys, "CREATE TABLE t") {
		t.Fatalf("assistant: %v / %s", r, sys)
	}
	e2 := newEnv(t, "")
	id2 := e2.open()
	if code, _ := e2.call("POST", "/api/sessions/"+id2+"/assistant", map[string]any{"messages": []map[string]string{{"role": "user", "content": "hi"}}}, "tok"); code != 400 {
		t.Fatalf("no key must be a clear 400: %d", code)
	}
}

func TestMcpApprovalRoundTrip(t *testing.T) {
	e := newEnvCfg(t, "alert1", func(c *config.Config) { c.MCP.Enabled = true })
	got := make(chan bool, 1)
	go func() {
		ok, _ := e.s.mcp.Approver(context.Background(), mcp.Approval{Token: "t", Connection: "c1", SQL: "DELETE FROM x"})
		got <- ok
	}()
	var id string
	for i := 0; i < 200 && id == ""; i++ {
		if l := e.s.ap.list(); len(l) == 1 {
			id = l[0].ID
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if id == "" {
		t.Fatal("approval never listed")
	}
	e.ok("POST", "/api/mcp/approvals/"+id, map[string]any{"Approve": true})
	if !<-got {
		t.Fatal("approver should return true")
	}
	if code, _ := e.call("POST", "/api/mcp/approvals/"+id, map[string]any{}, "tok"); code != 400 {
		t.Fatalf("second answer: %d", code)
	}
}

func TestFoldsAndSplit(t *testing.T) {
	e := newEnv(t, "alert1")
	if code, _ := e.call("PUT", "/api/state/folds?key=c1/tab1", []int{1, 3}, "tok"); code != 200 {
		t.Fatalf("put: %d", code)
	}
	if got := e.s.o.State.Folds("c1/tab1"); len(got) != 2 || got[1] != 3 {
		t.Fatalf("folds: %v", got)
	}
	if code, _ := e.call("GET", "/api/state/folds", nil, "tok"); code != 400 {
		t.Fatalf("key required: %d", code)
	}
}

func touchBackup(t *testing.T, name string) {
	t.Helper()
	dir := filepath.Join(config.DataDir(), "backups")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, name), []byte("-- dump\n"), 0o600)
}

func TestBackupRefusedOnReadOnlyConnection(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "")
	e.ok("PUT", "/api/connections", []map[string]any{{"Name": "c1", "Driver": "postgres", "Host": "h", "Database": "app", "Password": "pw", "ReadOnly": true}})
	id := e.open()
	touchBackup(t, "x.sql")
	for _, op := range []string{"restore", "backup"} {
		if code, _ := e.call("POST", "/api/sessions/"+id+"/"+op, map[string]any{"File": "x.sql"}, "tok"); code != 400 {
			t.Errorf("%s on a read-only connection: %d", op, code)
		}
	}
}
