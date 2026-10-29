package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"webtuidb/internal/config"
)

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

func TestSettingsSchemaPutAndKeys(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e := newEnv(t, "")
	fields := e.ok("GET", "/api/settings/schema", nil)["fields"].([]any)
	if len(fields) == 0 || fields[0].(map[string]any)["key"] == "" {
		t.Fatalf("fields = %v", fields)
	}
	if code, out := e.call("PUT", "/api/settings", map[string]any{"values": map[string]string{"table.alternating_rows": "maybe"}}, "tok"); code != 400 {
		t.Fatalf("a value that does not fit its type: %d %v", code, out)
	}
	e.ok("PUT", "/api/settings", map[string]any{"values": map[string]string{"table.alternating_rows": "true"}})
	if b, err := os.ReadFile(filepath.Join(config.ConfigDir(), "config.toml")); err != nil || !strings.Contains(string(b), "alternating_rows") {
		t.Fatalf("config.toml = %q %v", b, err)
	}
	if m := e.ok("GET", "/api/meta", nil); m["config"].(map[string]any)["table"].(map[string]any)["alternatingRows"] != true {
		t.Fatalf("meta did not pick the saved setting up: %v", m["config"])
	}
	e.ok("PUT", "/api/settings/keys", map[string]any{"keys": map[string]any{"next_tab": []string{"ctrl+alt+j"}}})
	code, out := e.call("PUT", "/api/settings/keys", map[string]any{"keys": map[string]any{"next_tab": []string{"ctrl+alt+j"}, "prev_tab": []string{"ctrl+alt+j"}}}, "tok")
	if code != 409 || out["error"] == "" {
		t.Fatalf("a keymap conflict is a 409: %d %v", code, out)
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

// P2-39: path settings are flagged read-only in the schema.
func TestSettingsSchemaReadOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e := newEnv(t, "")
	seen := map[string]bool{}
	for _, f := range e.ok("GET", "/api/settings/schema", nil)["fields"].([]any) {
		m := f.(map[string]any)
		k := m["key"].(string)
		ro := m["readOnly"] == true
		want := false
		for _, p := range pathSettings {
			want = want || p == k
		}
		if ro != want {
			t.Errorf("%s readOnly = %v, want %v", k, ro, want)
		}
		seen[k] = ro
	}
	if !seen["theme.file"] || !seen["web.tls_cert"] {
		t.Errorf("path settings missing or editable: %v", seen)
	}
}

func TestSettingsWarnCommentsAndKeyTaken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e := newEnv(t, "")
	path := filepath.Join(config.ConfigDir(), "config.toml")
	os.MkdirAll(filepath.Dir(path), 0o755)
	vals := map[string]any{"values": map[string]string{"table.alternating_rows": "true"}}
	if out := e.ok("PUT", "/api/settings", vals); out["warning"] != nil {
		t.Fatalf("no comments, no warning: %v", out["warning"])
	}
	os.WriteFile(path, []byte("[table]\nalternating_rows = true # keep me\n"), 0o600)
	if out := e.ok("PUT", "/api/settings", vals); out["warning"] != "comments in config.toml were not kept" {
		t.Fatalf("settings warning = %v", out["warning"])
	}
	os.WriteFile(path, []byte("# mine\n"), 0o600)
	if out := e.ok("PUT", "/api/settings/keys", map[string]any{"keys": map[string]any{"next_tab": []string{"ctrl+alt+j"}}}); out["warning"] != "comments in config.toml were not kept" {
		t.Fatalf("keys warning = %v", out)
	}
	// "p" is the default key of preview: giving it to commit would silently remove preview
	code, out := e.call("PUT", "/api/settings/keys", map[string]any{"keys": map[string]any{"commit": []string{"p"}}}, "tok")
	if msg, _ := out["error"].(string); code != 409 || !strings.Contains(msg, "takes the key from") {
		t.Fatalf("taking a default's key must be a 409: %d %v", code, out)
	}
}
