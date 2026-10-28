package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/config"
)

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
