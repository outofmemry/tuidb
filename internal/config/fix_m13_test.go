package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/keymap"
)

// R-006: a half-set TLS pair is a config error, so a settings save cannot leave a file the next start rejects.
func TestTLSPairValidated(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("[web]\ntls_cert = \"/x.pem\"\n"), 0o600)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "tls_cert and web.tls_key") {
		t.Fatalf("web: %v", err)
	}
	os.WriteFile(p, []byte("[mcp]\ntls_key = \"/x.key\"\n"), 0o600)
	if _, err := Load(p); err == nil {
		t.Fatal("mcp pair accepted")
	}
	os.WriteFile(p, []byte("[web]\ntls_cert = \"/x.pem\"\ntls_key = \"/x.key\"\n"), 0o600)
	if _, err := Load(p); err != nil {
		t.Fatalf("a full pair: %v", err)
	}
	// ApplyValues refuses the half pair and leaves the file as it was
	os.WriteFile(p, []byte("[general]\nrow_limit = 5\n"), 0o600)
	if err := ApplyValues(p, map[string]string{"web.tls_cert": "/x.pem"}); err == nil {
		t.Fatal("ApplyValues wrote a half pair")
	}
}

// R-002: KeymapChecked skips the plugin shortcut that clashes; WriteKeys refuses a new key that takes a plugin's.
func TestKeymapCheckedAndWriteKeysSeePlugins(t *testing.T) {
	plug := []keymap.Binding{{Action: "plugin:a", Key: "ctrl+alt+x", Context: keymap.Global, Desc: "plugin: a"}, {Action: "plugin:b", Key: "alt+J", Context: keymap.Global, Desc: "plugin: b"}}
	m, conflicts, err := Defaults().KeymapChecked(plug)
	if err != nil || len(conflicts) != 1 || !strings.Contains(conflicts[0], "plugin:b") || m == nil {
		t.Fatalf("%v %v", conflicts, err)
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := WriteKeys(p, map[string][]string{"commit": {"ctrl+alt+x"}}, plug...); err == nil || !strings.Contains(err.Error(), "plugin:a") {
		t.Fatalf("a key taken from a plugin: %v", err)
	}
	if err := WriteKeys(p, map[string][]string{"commit": {"ctrl+alt+y"}}, plug...); err != nil {
		t.Fatalf("a free key: %v", err)
	}
}

// P1-05 / P1-06: plugin overrides never stop the config from loading or the keymap from building.
func TestPluginKeyOverrides(t *testing.T) {
	c := Defaults()
	c.KeyOverrides = map[string][]string{"plugin:x": {"M-Q"}}
	plug := []keymap.Binding{{Action: "plugin:x", Key: "alt+P", Context: keymap.Global, Desc: "plugin: x"}}
	if m, _, err := c.KeymapChecked(plug); err != nil || m == nil {
		t.Fatalf("installed plugin override: %v", err)
	}
	c.KeyOverrides = map[string][]string{"plugin:gone": {"-n M-q"}}
	if err := c.validate(); err != nil {
		t.Fatalf("uninstalled plugin with -n spelling: %v", err)
	}
}

func TestR4ConfigSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	p := filepath.Join(dir, "config.toml")
	// P2-12: theme.file is ~-expanded
	os.WriteFile(p, []byte("[theme]\nfile = \"~/mine.toml\"\n"), 0o600)
	c, err := Load(p)
	if err != nil || c.Theme.File != filepath.Join(dir, "mine.toml") {
		t.Fatalf("theme.file %q err=%v", c.Theme.File, err)
	}
	// P2-11: a passcode hash alone makes the file 0600
	os.WriteFile(p, []byte("[security]\npasscode = \"$2a$10$abcdefghijklmnopqrstuv\"\n"), 0o644)
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm()&0o077 != 0 {
		t.Fatalf("mode %v", st.Mode())
	}
	// P2-13: unknown hint / toolbar names are errors naming the entry
	os.WriteFile(p, []byte("[general]\nhints = [\"commmit\"]\n"), 0o600)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "commmit") {
		t.Fatalf("hints typo: %v", err)
	}
	os.WriteFile(p, []byte("[web]\ntoolbar = [\"history\", \"commit\", \"plugin:x\", \"nope\"]\n"), 0o600)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("toolbar typo: %v", err)
	}
	os.WriteFile(p, []byte("[web]\ntoolbar = [\"history\", \"commit\", \"plugin:x\"]\n"), 0o600)
	if _, err := Load(p); err != nil {
		t.Fatalf("valid toolbar: %v", err)
	}
}
