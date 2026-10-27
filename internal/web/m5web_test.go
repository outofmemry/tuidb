package web

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/config"
)

func TestSafeModeEndpoint(t *testing.T) {
	e := newEnv(t, "safe1")
	id := e.open()
	if code, _ := e.call("POST", "/api/sessions/"+id+"/safe-mode", map[string]any{"level": "silent"}, "tok"); code != 409 {
		t.Fatalf("leaving a password level without the password: %d", code)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/safe-mode", map[string]any{"level": "silent", "password": "pw"}, "tok"); code != 200 {
		t.Fatalf("with the password: %d", code)
	}
	if got := e.ok("GET", "/api/sessions/"+id, nil)["level"]; got != "silent" {
		t.Fatalf("level = %v", got)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/safe-mode", map[string]any{"level": "bogus"}, "tok"); code != 400 {
		t.Fatalf("bad level: %d", code)
	}
}

func TestWebThemeResolvesNamesAndFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "t.toml")
	os.WriteFile(f, []byte("[theme.colors]\nwarn = \"#111111\"\n"), 0o600)
	got := webTheme(config.Theme{Name: "sunset-light", File: f, Colors: map[string]string{"ok": "#222222"}})
	c := got["Colors"].(map[string]string)
	if got["Name"] != "light" || c["accent"] != webPalettes["sunset-light"]["accent"] || c["warn"] != "#111111" || c["ok"] != "#222222" {
		t.Fatalf("theme = %v", got)
	}
	if webTheme(config.Theme{Name: "auto"})["Name"] != "auto" {
		t.Fatal("auto must pass through to prefers-color-scheme")
	}
}

func TestConsoleReplacesBigStatements(t *testing.T) {
	var c consoleLog
	c.add("c1", strings.Repeat("x", 70<<10))
	if c.lines[0].Text != "<70 KB statement>" {
		t.Fatalf("got %q", c.lines[0].Text)
	}
}

func TestSharedDirsAreListed(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "team.sql"), []byte("select 1"), 0o600)
	e := newEnvCfg(t, "", func(c *config.Config) { c.Queries.SharedDirs = []string{dir} })
	b, _ := io.ReadAll(e.raw("GET", "/api/queries", nil, nil).Body)
	if !strings.Contains(string(b), `"Shared":true`) || !strings.Contains(string(b), "team") {
		t.Fatalf("queries: %s", b)
	}
}
