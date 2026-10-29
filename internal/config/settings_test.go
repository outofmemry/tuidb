package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/keymap"
)

func TestSchemaDocsAndValues(t *testing.T) {
	c := Defaults()
	c.General.Hints = []string{"commit", "run_all"}
	seen := map[string]Field{}
	for _, f := range Schema(c) {
		seen[f.Key] = f
		if f.Doc == "" {
			t.Errorf("%s has no doc", f.Key)
		}
	}
	if f := seen["general.hints"]; f.Type != "list" || f.Value != "commit,run_all" {
		t.Errorf("hints = %+v", f)
	}
	if f := seen["general.connection_sort"]; f.Type != "enum" || !strings.Contains(strings.Join(f.Enum, ","), "recent") {
		t.Errorf("connection_sort = %+v", f)
	}
	if f := seen["table.decode_spatial"]; f.Type != "bool" || f.Value != "true" {
		t.Errorf("decode_spatial = %+v", f)
	}
	if _, ok := seen["security.passcode"]; ok {
		t.Error("the passcode hash must not be offered")
	}
}

func TestApplyValuesRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("[general]\nprefix = \"C-a\"\n[ai.providers.local]\nprovider = \"openai\"\nmodel = \"m\"\n[[tag]]\nname = \"x\"\ncolor = \"#fff\"\n[keys]\ncommit = \"x\"\n"), 0o644)
	err := ApplyValues(p, map[string]string{"general.row_limit": "50", "web.toolbar": "run_all, commit", "table.decode_spatial": "false", "general.connection_sort": "recent"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.General.RowLimit != 50 || c.General.Prefix != "C-a" || c.Table.DecodeSpatial || c.General.ConnectionSort != "recent" ||
		len(c.Web.Toolbar) != 2 || c.Web.Toolbar[1] != "commit" || c.AI.Providers["local"].Model != "m" || len(c.Tags) != 1 || c.KeyOverrides["commit"][0] != "x" {
		t.Fatalf("round trip lost data: %+v", c)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode())
	}
	before, _ := os.ReadFile(p)
	for bad, vals := range map[string]map[string]string{
		"not one of": {"table.filter_state": "nope"}, "whole number": {"general.row_limit": "x"}, "unknown setting": {"nope.key": "1"},
		"lock_after_mins": {"security.lock_after_mins": "-1"}, "true or false": {"table.alternating_rows": "maybe"},
		"query_timeout_secs must be >= 0": {"general.query_timeout_secs": "-5"}, // T-P3-16
	} {
		if err := ApplyValues(p, vals); err == nil || !strings.Contains(err.Error(), bad) {
			t.Errorf("%v: got %v, want %q", vals, err, bad)
		}
	}
	if after, _ := os.ReadFile(p); string(after) != string(before) {
		t.Error("a rejected change must not touch the file")
	}
}

func TestWriteKeysConflictAndReset(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := WriteKeys(p, map[string][]string{"commit": {"x"}, "run_all": {"M-r", "F5"}}); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil || c.KeyOverrides["commit"][0] != "x" || len(c.KeyOverrides["run_all"]) != 2 {
		t.Fatalf("%v %+v", err, c.KeyOverrides)
	}
	if err := WriteKeys(p, map[string][]string{"commit": {"x"}, "plugins": {"x"}}); err == nil || !strings.Contains(err.Error(), "both bound") {
		t.Fatalf("want a conflict, got %v", err)
	}
	if err := WriteKeys(p, map[string][]string{"nope": {"x"}}); err == nil {
		t.Fatal("unknown action must fail")
	}
	if err := WriteKeys(p, map[string][]string{}); err != nil {
		t.Fatal(err)
	}
	if c, _ := Load(p); len(c.KeyOverrides) != 0 {
		t.Fatalf("reset left %v", c.KeyOverrides)
	}
}

func TestPluginKeyOverride(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := WriteKeys(p, map[string][]string{"plugin:count": {"M-Q"}}); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p) // validates without knowing the plugin
	if err != nil {
		t.Fatal(err)
	}
	m, err := c.Keymap() // plugin not installed: override ignored
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range m.All() {
		if b.Action == "plugin:count" {
			t.Fatal("uninstalled plugin must not show")
		}
	}
	m, err = c.KeymapWith([]keymap.Binding{{Action: "plugin:count", Key: "alt+P", Context: keymap.Global, Desc: "plugin: count"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range m.All() {
		if b.Action == "plugin:count" && b.Key != "alt+Q" {
			t.Fatalf("override not applied: %+v", b)
		}
	}
	if err := WriteKeys(p, map[string][]string{"plugin:count": {"M-q"}, "commit": {"M-q"}}); err == nil {
		t.Fatal("alt+q is close_pane: conflict expected")
	}
}

func TestPasscodeAndPresets(t *testing.T) {
	h, err := HashPasscode("1234")
	if err != nil || !CheckPasscode(h, "1234") || CheckPasscode(h, "0000") || CheckPasscode("", "") {
		t.Fatalf("hash %q %v", h, err)
	}
	if _, err := HashPasscode(""); err == nil {
		t.Fatal("empty passcode")
	}
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("[security]\npasscode = \"plain\"\n"), 0o600)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "bcrypt") {
		t.Fatalf("plain passcode accepted: %v", err)
	}
	os.WriteFile(p, []byte("[security]\npasscode = \""+h+"\"\nlock_after_mins = 5\n"), 0o600)
	if c, err := Load(p); err != nil || c.Security.LockAfterMins != 5 {
		t.Fatalf("%v %+v", err, c.Security)
	}
	if n := len(PasswordCmdPresets()); n != 4 {
		t.Fatalf("%d presets", n)
	}
}

func TestInstalledThemes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	os.MkdirAll(filepath.Join(ThemesDir(), "nord"), 0o755)
	os.WriteFile(filepath.Join(ThemesDir(), "nord", "theme.toml"), []byte("[theme.colors]\nbg = \"#000000\"\n"), 0o644)
	os.WriteFile(filepath.Join(ThemesDir(), "flat.toml"), []byte("[theme.colors]\n"), 0o644)
	os.MkdirAll(filepath.Join(ThemesDir(), "empty"), 0o755)
	if got := strings.Join(InstalledThemes(), ","); got != "flat,nord" {
		t.Fatalf("installed = %q", got)
	}
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("[theme]\nname = \"nord\"\n"), 0o600)
	c, err := Load(p)
	if err != nil || !strings.HasSuffix(c.Theme.File, filepath.Join("nord", "theme.toml")) {
		t.Fatalf("%v %+v", err, c.Theme)
	}
	os.WriteFile(p, []byte("[theme]\nname = \"missing\"\n"), 0o600)
	if _, err := Load(p); err == nil {
		t.Fatal("unknown theme accepted")
	}
}

func TestHasComments(t *testing.T) {
	dir := t.TempDir()
	for body, want := range map[string]bool{
		"": false,
		"# Rewritten by webtuidb settings; x\n[a]\nb = 1\n": false,
		"# mine\n[a]\n":                     true,
		"[a]\nb = 1 # note\n":               true,
		"[a]\nb = \"x # not\"\n":            false,
		"[a]\nb = 'x # not'\n":              false,
		"[a]\nb = \"\"\"\nx # no\n\"\"\"\n": false,
		"[a]\nb = \"q\\\" # yes\"\n":        false,
	} {
		p := dir + "/c.toml"
		os.WriteFile(p, []byte(body), 0o600)
		if got := HasComments(p); got != want {
			t.Errorf("%q: %v want %v", body, got, want)
		}
	}
	if HasComments(dir + "/missing.toml") {
		t.Error("missing file")
	}
}

// NEW-3: a value the next start would reject is not saved, and the file is left as it was.
func TestApplyValuesRefusesWhatLoadRejects(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	orig := []byte("[general]\nprefix = \"C-a\"\n")
	os.WriteFile(p, orig, 0o644)
	if err := ApplyValues(p, map[string]string{"general.hints": "commmit"}); err == nil {
		t.Fatalf("typo saved: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != string(orig) {
		t.Fatalf("file changed: %q", b)
	}
}
