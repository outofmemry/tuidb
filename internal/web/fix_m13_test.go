package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func writePlugin(t *testing.T, name, body string) {
	t.Helper()
	dir := filepath.Join(config.ConfigDir(), "plugins")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600)
}

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

// R-002: a plugin key that clashes with a default never breaks /api/meta; it is reported.
func TestMetaSurvivesPluginKeyConflict(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writePlugin(t, "clash.js", "// key: alt+J\n1\n") // alt+J is the jobs shortcut
	writePlugin(t, "fine.js", "// key: ctrl+alt+x\n1\n")
	e := newEnv(t, "")
	m := e.ok("GET", "/api/meta", nil)
	conf, _ := m["pluginConflicts"].([]any)
	if len(conf) != 1 || !strings.Contains(conf[0].(string), "plugin:clash") {
		t.Fatalf("pluginConflicts = %v", m["pluginConflicts"])
	}
	var fine, clash bool
	for _, b := range m["keymap"].(map[string]any)["bindings"].([]any) {
		switch b.(map[string]any)["Action"] {
		case "plugin:fine":
			fine = true
		case "plugin:clash":
			clash = true
		}
	}
	if !fine || clash {
		t.Fatalf("fine=%v clash=%v", fine, clash)
	}
}

// R-004: the lock passcode hash is never sent by GET /api/settings.
func TestSettingsHidesPasscodeHash(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) {
		c.Security.Passcode = "$2a$10$abcdefghijklmnopqrstuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuu"
	})
	out := e.ok("GET", "/api/settings", nil)
	if strings.Contains(out["text"].(string), "$2a$") {
		t.Fatalf("hash leaked: %v", out["text"])
	}
}

// R-005: the settings editor writes the file given with -config, not the default one.
func TestSettingsUseConfigPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e := newEnv(t, "")
	other := filepath.Join(t.TempDir(), "other.toml")
	e.s.o.CfgPath = other
	e.ok("PUT", "/api/settings", map[string]any{"values": map[string]string{"table.alternating_rows": "true"}})
	if b, err := os.ReadFile(other); err != nil || !strings.Contains(string(b), "alternating_rows") {
		t.Fatalf("-config file = %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(config.ConfigDir(), "config.toml")); err == nil {
		t.Fatal("the default config.toml was written")
	}
	if got := e.ok("GET", "/api/settings/schema", nil)["path"]; got != other {
		t.Fatalf("schema path = %v", got)
	}
}

// R-006: path-valued settings cannot be set from the browser.
func TestSettingsRefusePathKeys(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e := newEnv(t, "")
	for _, k := range []string{"queries.shared_dirs", "theme.file", "web.tls_cert", "web.tls_key", "mcp.tls_cert", "mcp.tls_key"} {
		code, out := e.call("PUT", "/api/settings", map[string]any{"values": map[string]string{k: "/etc"}}, "tok")
		if code != http.StatusBadRequest || !strings.Contains(out["error"].(string), "config.toml") {
			t.Errorf("%s: %d %v", k, code, out)
		}
	}
}

// R-010: parallel wrong passcodes are serialised, so the 1 s delay adds up instead of overlapping.
func TestPasscodeAttemptsSerialised(t *testing.T) {
	hash, _ := config.HashPasscode("1234")
	e := newEnvCfg(t, "", func(c *config.Config) { c.Security.Passcode = hash })
	for i := 0; i < 3; i++ {
		e.ok("POST", "/api/passcode/check", map[string]any{"passcode": "no"})
	}
	st := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.call("POST", "/api/passcode/check", map[string]any{"passcode": "no"}, "tok")
		}()
	}
	wg.Wait()
	if d := time.Since(st); d < 2800*time.Millisecond {
		t.Fatalf("3 parallel attempts took %v; they must queue (1 s each)", d)
	}
}

// R-010: the per-address table is bounded and expires.
func TestPasscodeTableBounded(t *testing.T) {
	var p passFails
	for i := 0; i < passMaxIPs*2; i++ {
		p.entry(string(rune('a'+i%26)) + string(rune(i)))
	}
	if len(p.n) > passMaxIPs {
		t.Fatalf("%d entries", len(p.n))
	}
	e := p.entry("x")
	e.fails = 5
	e.last.Store(time.Now().Add(-2 * passFailExpiry).UnixNano())
	if e2 := p.entry("y"); e2 == nil || p.n["x"] != nil {
		t.Fatal("an idle address must expire")
	}
}

// R-011: concurrent chat requests share one store without a race (run with -race).
func TestChatsStoreOnce(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s := New(Options{Cfg: config.Defaults(), Token: "t"})
	var n atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.chats() != nil {
				n.Add(1)
			}
		}()
	}
	wg.Wait()
	if n.Load() != 8 || s.chats() != s.chatStore {
		t.Fatal("chats()")
	}
}

// R-008: a panic in a handler is a 500 for that request, not a dead server, and a panicking job ends as failed.
func TestHandlerAndJobPanicsAreContained(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "")
	req, _ := http.NewRequest("GET", "/x", nil)
	if _, err := e.s.call(func(*http.Request) (any, error) { panic("boom") }, req); err == nil || !strings.Contains(err.Error(), "internal error") {
		t.Fatalf("handler panic: %v", err)
	}
	id := e.open()
	ss := e.s.sessions[id]
	j := &job{kind: "x", title: "x"}
	jid := ss.startJob(j, func(context.Context) error { panic("job boom") })
	if r := waitJob(t, e, id, jid); r["error"] == nil || r["error"] == "" || !strings.Contains(r["error"].(string), "internal error") {
		t.Fatalf("job = %v", r)
	}
}
