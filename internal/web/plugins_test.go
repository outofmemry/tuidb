package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/ui"
)

// pinSess is a session that lends a pinned connection and records what ran on it.
type pinSess struct {
	*fakeSess
	mu  sync.Mutex
	log []string
}

type pinned struct {
	*fakeSess
	p *pinSess
}

func (p *pinSess) Pin(context.Context) (db.Session, error) { return &pinned{p.fakeSess, p}, nil }

func (c *pinned) Exec(ctx context.Context, stmt string, limit int) (*db.Result, error) {
	c.p.mu.Lock()
	c.p.log = append(c.p.log, stmt)
	c.p.mu.Unlock()
	return c.fakeSess.Exec(ctx, stmt, limit)
}

func (c *pinned) Close() error { return nil }

// R-007: a plugin query runs inside a read-only transaction that is rolled back, and a side-effecting SELECT is refused.
func TestPluginQueryReadOnlyTransaction(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writePlugin(t, "ok.js", "query('select 1 as x')\n")
	writePlugin(t, "bad.js", "query(\"select pg_terminate_backend(1)\")\n")
	e := newEnv(t, "")
	ps := &pinSess{fakeSess: e.fake}
	e.s.o.Connect = func(ctx context.Context, c config.Connection) (*ui.Opened, error) { return &ui.Opened{Sess: ps}, nil }
	id := e.open()
	e.ok("POST", "/api/sessions/"+id+"/plugins/run", map[string]any{"name": "ok"})
	ps.mu.Lock()
	log := strings.Join(ps.log, "|")
	ps.mu.Unlock()
	if log != "BEGIN READ ONLY|select 1 as x|ROLLBACK" {
		t.Fatalf("statements on the pinned connection: %q", log)
	}
	code, out := e.call("POST", "/api/sessions/"+id+"/plugins/run", map[string]any{"name": "bad"}, "tok")
	if code != 400 || !strings.Contains(out["error"].(string), "pg_terminate_backend") {
		t.Fatalf("a side-effecting SELECT: %d %v", code, out)
	}
}

// R-007: without a pinnable session and without read_only, the query is refused rather than run unguarded.
func TestPluginQueryRefusedWithoutReadOnlyGuarantee(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writePlugin(t, "ok.js", "query('select 1')\n")
	e := newEnv(t, "")
	id := e.open()
	code, out := e.call("POST", "/api/sessions/"+id+"/plugins/run", map[string]any{"name": "ok"}, "tok")
	if code != 400 || !strings.Contains(out["error"].(string), "read-only") {
		t.Fatalf("%d %v", code, out)
	}
}

func TestPluginRunEffectsAndReadOnlyHost(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := filepath.Join(config.ConfigDir(), "plugins")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "demo.js"), []byte("// key: alt+P\ncopy('hi'); notify('n');\nvar r = query('select 1 as x');\nr.rows[0][0]\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "bad.js"), []byte("query('delete from t')\n"), 0o600)
	e := newEnv(t, "")
	e.s.o.Conns[0].ReadOnly = true // the fake session cannot pin a connection, so the server must enforce read-only itself
	id := e.open()
	out := e.ok("POST", "/api/sessions/"+id+"/plugins/run", map[string]any{"name": "demo", "context": "table"})
	fx := out["effects"].([]any)
	if out["text"] != "1" || len(fx) != 2 || fx[0].(map[string]any)["kind"] != "copy" || fx[0].(map[string]any)["text"] != "hi" {
		t.Fatalf("out = %v", out)
	}
	code, o := e.call("POST", "/api/sessions/"+id+"/plugins/run", map[string]any{"name": "bad"}, "tok")
	if code != 400 || !strings.Contains(o["error"].(string), "read-only") {
		t.Fatalf("a write from a plugin: %d %v", code, o)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/plugins", nil)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list []map[string]any
	json.NewDecoder(resp.Body).Decode(&list)
	var demo map[string]any
	for _, p := range list {
		if p["name"] == "demo" {
			demo = p
		}
	}
	if demo == nil || demo["key"] != "alt+P" || demo["js"] != true {
		t.Fatalf("plugins = %v", list)
	}
	m := e.ok("GET", "/api/meta", nil)
	var bound bool
	for _, b := range m["keymap"].(map[string]any)["bindings"].([]any) {
		bound = bound || b.(map[string]any)["Action"] == "plugin:demo"
	}
	if !bound {
		t.Fatal("the plugin shortcut is not in the keymap")
	}
}
