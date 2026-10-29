package ui

import (
	"path/filepath"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/plugin"
)

func TestM13PluginBindingsAndDetail(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeTo(t, filepath.Join(config.ConfigDir(), "plugins", "hi.toml"), "name = \"hi\"\ncommand = \"echo\"\nkey = \"alt+P\"\n", 0o644)
	bs := pluginBindings()
	if len(bs) != 1 || bs[0].Action != "plugin:hi" || bs[0].Key != "alt+P" {
		t.Fatalf("bindings %+v", bs)
	}
}

func TestTUI3PluginDetail(t *testing.T) {
	if got := pluginDetail(plugin.Plugin{Output: "text", Key: "alt+P", Command: "echo"}); got != "text · alt+P · echo" {
		t.Fatalf("got %q", got)
	}
	if got := pluginDetail(plugin.Plugin{JS: true, Output: "sql", Command: "ignored"}); got != "js" {
		t.Fatalf("got %q", got)
	}
}

func TestM3PluginInsertsSQL(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := filepath.Join(config.ConfigDir(), "plugins")
	writeTo(t, filepath.Join(dir, "ins.toml"), "name = \"Insert\"\ncommand = \"printf\"\nargs = [\"select 42\"]\ncontexts = [\"editor\"]\noutput = \"sql\"\n", 0o644)
	writeTo(t, filepath.Join(dir, "grid.toml"), "name = \"GridOnly\"\ncommand = \"printf\"\nargs = [\"x\"]\ncontexts = [\"grid\"]\n", 0o644)
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.prefix("alt+p")
	h.want("Insert")
	h.wantNot("GridOnly") // filtered by the focused pane
	h.press("enter")
	if q := h.a.ws().tab().(*queryTab); q.ed.Text() != "select 42" {
		t.Fatalf("editor %q", q.ed.Text())
	}
}

func TestM3PluginTextOutputOpensAView(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeTo(t, filepath.Join(config.ConfigDir(), "plugins", "hi.toml"), "name = \"Hello\"\ncommand = \"printf\"\nargs = [\"hello there\"]\n", 0o644)
	h := newHarness(t, nil)
	h.connect()
	h.prefix("alt+p") // the sidebar has the focus; a plugin without contexts applies everywhere
	h.press("enter")
	h.want("hello there")
}
