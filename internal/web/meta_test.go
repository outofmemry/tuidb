package web

import (
	"encoding/json"
	"strings"
	"testing"
	"webtuidb/internal/config"
)

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

func TestMetaMenusThemesVersion(t *testing.T) {
	e := newEnv(t, "")
	e.s.o.Version = "v9"
	m := e.ok("GET", "/api/meta", nil)
	if menus, _ := m["menus"].(map[string]any); len(menus["grid"].([]any)) == 0 {
		t.Fatalf("menus = %v", m["menus"])
	}
	if _, ok := m["themes"].([]any); !ok || m["version"] != "v9" {
		t.Fatalf("themes/version = %v %v", m["themes"], m["version"])
	}
	if _, ok := m["web"].(map[string]any)["toolbar"].([]any); !ok {
		t.Fatalf("web = %v", m["web"])
	}
	cfg := m["config"].(map[string]any)
	for sec, key := range map[string]string{"table": "filterDefaultColumn", "general": "newTabOpens", "editor": "closeBraces", "sidebar": "matching"} {
		if _, ok := cfg[sec].(map[string]any)[key]; !ok {
			t.Errorf("config.%s.%s missing", sec, key)
		}
	}
	for _, b := range m["keymap"].(map[string]any)["bindings"].([]any) {
		if b.(map[string]any)["Action"] == "" {
			t.Fatal("binding without an action")
		}
	}
}

func TestMetaExtrasAndSessionInfo(t *testing.T) {
	e, _ := richEnv(t, func(c *config.Config) {
		c.Tags = []config.Tag{{Name: "qa", Color: "#112233"}}
		c.MCP.Listen = "0.0.0.0:9000"
	})
	m := e.ok("GET", "/api/meta", nil)
	b, _ := json.Marshal(m)
	for _, want := range []string{`"qa"`, `#112233`, `"listen":"0.0.0.0:9000"`, `"fileBased":[]`, `"production"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("meta lacks %s: %s", want, b)
		}
	}
	for _, d := range m["drivers"].([]any) {
		if d == "sqlite" || d == "duckdb" || d == "cockroach" || d == "d1" {
			t.Errorf("removed driver %v still listed", d)
		}
	}
	info := e.ok("GET", "/api/sessions/"+e.open(), nil)
	if info["readOnly"] != false || info["pinnable"] != true {
		t.Errorf("info = %v", info)
	}
}
