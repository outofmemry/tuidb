package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"webtuidb/internal/config"
)

func TestBoardKeepsConnectionPin(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "")
	n := map[string]any{"Title": "n", "Kind": "number", "Query": "select 1 as x"}
	e.ok("PUT", "/api/boards/new", map[string]any{"Name": "b", "Connection": "prod", "Panels": []any{n}})
	e.ok("PUT", "/api/boards/b", map[string]any{"Name": "b", "Connection": "", "Panels": []any{n}})
	b, ok := boardNamed("b")
	if !ok || b.Connection != "prod" || boardJSON(b)["connection"] != "prod" {
		t.Fatalf("pin lost: %+v", b)
	}
}

func TestBoardsPluginsMCP(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	e := newEnvCfg(t, "", func(c *config.Config) { c.MCP.Enabled = true })
	id := e.open()
	e.ok("POST", "/api/boards", map[string]any{"Name": "b1", "Query": "select 1 as x", "Kind": "bar", "X": "x", "Y": "x"})
	e.ok("POST", "/api/boards", map[string]any{"Name": "b1", "Query": "select 2 as x", "Kind": "bar"}) // appends, never overwrites
	r := e.ok("POST", "/api/sessions/"+id+"/boards/run", map[string]any{"Name": "b1"})
	if p := r["panels"].([]any); len(p) != 2 {
		t.Fatalf("both panels expected: %v", r)
	}
	e.ok("POST", "/api/boards", map[string]any{"Name": "w", "Query": "delete from t"})
	if code, _ := e.call("POST", "/api/sessions/"+id+"/boards/run", map[string]any{"Name": "w"}, "tok"); code != 400 {
		t.Fatalf("a write panel must be refused: %d", code)
	}

	pd := filepath.Join(cfgDir, "webtuidb", "plugins")
	os.MkdirAll(pd, 0o755)
	os.WriteFile(filepath.Join(pd, "echo.toml"), []byte("name = \"echo\"\ncommand = \"echo\"\nargs = [\"hi\"]\ncontexts = [\"editor\"]\n"), 0o644)
	pr := e.ok("POST", "/api/sessions/"+id+"/plugins/run", map[string]any{"Name": "echo"})
	if strings.TrimSpace(pr["text"].(string)) != "hi" || pr["output"] != "text" {
		t.Fatalf("plugin: %v", pr)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/plugins/run", map[string]any{"Name": "rm"}, "tok"); code != 400 {
		t.Fatalf("unknown plugin: %d", code)
	}

	if m := e.ok("GET", "/api/mcp/log", nil); m["enabled"] != true {
		t.Fatalf("mcp: %v", m)
	}
}

func TestBoardEditing(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "")
	id := e.open()
	b := func(name string, panels ...map[string]any) map[string]any {
		return map[string]any{"Name": name, "RefreshSecs": 0, "Panels": panels}
	}
	n := map[string]any{"Title": "n", "Kind": "number", "Query": "select 1 as x"}
	btn := map[string]any{"Title": "go", "Kind": "button", "Target": "n"}
	imp := map[string]any{"Title": "load", "Kind": "import", "Table": "t"}
	dep := map[string]any{"Title": "d", "Kind": "table", "Query": "select 2 as x", "RefreshOn": "n"}
	e.ok("PUT", "/api/boards/new", b("one", n, btn, imp, dep))
	// button and import panels do not break a board run
	if r := e.ok("POST", "/api/sessions/"+id+"/boards/run", map[string]any{"Name": "one"}); len(r["panels"].([]any)) != 2 {
		t.Fatalf("run = %v", r)
	}
	p := e.ok("POST", "/api/sessions/"+id+"/boards/panel", map[string]any{"Name": "one", "Title": "n"})
	if p["panel"].(map[string]any)["number"] != "1" || p["dependents"].([]any)[0] != "d" {
		t.Fatalf("panel = %v", p)
	}
	for _, title := range []string{"go", "load", "missing"} {
		if code, _ := e.call("POST", "/api/sessions/"+id+"/boards/panel", map[string]any{"Name": "one", "Title": title}, "tok"); code != 400 {
			t.Errorf("panel %q: %d, want 400", title, code)
		}
	}
	e.ok("PUT", "/api/boards/one", b("two", n)) // rename: the old file goes
	if _, ok := boardNamed("one"); ok {
		t.Fatal("the old board file must be gone")
	}
	if _, ok := boardNamed("two"); !ok {
		t.Fatal("renamed board missing")
	}
	e.ok("PUT", "/api/boards/two", b("three", n))
	if code, _ := e.call("PUT", "/api/boards/three", b("three"), "tok"); code != 400 {
		t.Fatalf("a board needs a panel: %d", code)
	}
	if code, _ := e.call("PUT", "/api/boards/new", b("three", n), "tok"); code != 400 {
		t.Fatalf("a new board may not take an existing name: %d", code)
	}
	if code, _ := e.call("PUT", "/api/boards/three", b("three", map[string]any{"Title": "b", "Kind": "button", "Target": "zz"}), "tok"); code != 400 {
		t.Fatalf("a button needs a target: %d", code)
	}
	e.ok("DELETE", "/api/boards/three", nil)
	if _, ok := boardNamed("three"); ok {
		t.Fatal("delete did not remove the board")
	}
}

func TestBoardSaveYClearsComment(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e := newEnv(t, "")
	e.ok("POST", "/api/boards", map[string]any{"Name": "yb", "Query": "-- barchart x: a, y: b, c\nselect 1 as a, 2 as b, 3 as c, 4 as d", "Y": "d"})
	b, err := findBoard("yb")
	if err != nil || len(b.Panels) != 1 {
		t.Fatalf("%v %v", b, err)
	}
	if c := b.Panels[0].Chart; !strings.Contains(c, "d") || strings.Contains(c, "b") || strings.Contains(c, "c,") {
		t.Fatalf("chart spec kept the comment's series: %q", c)
	}
}
