package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/config"
)

func TestM15SettingsEditorSavesAndReloads(t *testing.T) {
	h := newHarness(t, nil)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[general]\nkeep_alive = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.a.d.CfgPath = path
	h.a.run("settings")
	h.want("Settings")
	h.press("/")
	h.typeText("general.keep_alive")
	h.press("enter")
	h.press("enter") // flips the bool
	h.want("1 unsaved")
	h.press("s")
	h.want("saved 1 setting")
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "keep_alive = false") {
		t.Errorf("config.toml: %s", b)
	}
	if h.a.d.Cfg.General.KeepAlive {
		t.Error("the running config was not reloaded")
	}
	// a value the engine refuses stays in the dialog with its message
	h.press("/")
	h.press("ctrl+u")
	h.typeText("general.row_limit")
	h.press("enter")
	h.press("enter")
	h.press("ctrl+u")
	h.typeText("-5")
	h.press("enter")
	h.press("s")
	if !strings.Contains(h.screen(), "Settings") {
		t.Error("the dialog must stay open after a refused save")
	}
}

func TestM15SettingsKeyConflictShown(t *testing.T) {
	rebind := func(t *testing.T, cfg, key string) (*harness, string) {
		h := newHarness(t, nil)
		path := filepath.Join(t.TempDir(), "config.toml")
		os.WriteFile(path, []byte(cfg), 0o600)
		h.a.d.CfgPath = path
		loaded, err := config.Load(path) // the running config carries the file's [keys] entries
		if err != nil {
			t.Fatal(err)
		}
		h.a.d.Cfg = loaded
		h.a.run("settings")
		h.press("tab")
		h.want("Key bindings")
		h.press("/")
		h.typeText("rename_tab")
		h.press("enter")
		h.press("enter")
		h.want("Key for rename_tab")
		h.press("ctrl+u")
		h.typeText(key)
		h.press("enter")
		return h, path
	}
	// alt+x is cancel_queries' default: the builder would give it away silently (P2-79), so it is refused.
	h, path := rebind(t, "", "alt+x")
	h.want("takes the key from cancel_queries")
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "rename_tab") {
		t.Errorf("a refused binding was written: %s", b)
	}
	// two [keys] entries on one key is a real conflict too
	h, path = rebind(t, "[keys]\nnew_query_tab = \"alt+z\"\n", "alt+z")
	h.want("alt+z")
	h.want("rename_tab")
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "rename_tab") {
		t.Errorf("a clashing binding was written: %s", b)
	}
	// a free key still works
	h, path = rebind(t, "", "ctrl+alt+j")
	h.want("rename_tab rebound")
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "rename_tab") {
		t.Errorf("[keys] not written: %s", b)
	}
}

// P2-77: saving from the form warns before it drops the file's comments.
func TestM16SettingsSaveWarnsAboutComments(t *testing.T) {
	h := newHarness(t, nil)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("# my notes\n[general]\nkeep_alive = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.a.d.CfgPath = path
	h.a.run("settings")
	h.press("/")
	h.typeText("general.keep_alive")
	h.press("enter")
	h.press("enter")
	h.press("s")
	h.want("does not keep comments")
	h.press("n")
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "# my notes") {
		t.Fatalf("the comment is gone after no: %s", b)
	}
	h.press("s")
	h.press("y")
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "# my notes") {
		t.Errorf("expected the rewrite after yes: %s", b)
	}
}

func TestM7SettingsOpensTheConfigInTheEditor(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "myed --flag")
	cmd := editorCommand("/x/config.toml")
	if got := strings.Join(cmd.Args, " "); got != "myed --flag /x/config.toml" {
		t.Fatalf("args %q", got)
	}
	h := newHarness(t, nil)
	h.a.d.CfgPath = filepath.Join(t.TempDir(), "sub", "config.toml")
	if h.a.cfgPath() != h.a.d.CfgPath || h.a.settings() == nil {
		t.Fatal("settings must suspend the program for the editor")
	}
	if _, err := os.Stat(filepath.Dir(h.a.d.CfgPath)); err != nil {
		t.Fatal("the config folder is created for the editor")
	}
}
