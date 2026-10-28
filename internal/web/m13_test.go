package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/config"
)

func waitJob(t *testing.T, e *env, id, jid string) map[string]any {
	t.Helper()
	for i := 0; i < 200; i++ {
		j := e.ok("GET", "/api/sessions/"+id+"/jobs/"+jid, nil)
		if j["done"] == true {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return nil
}

func TestJobsListCancelDismiss(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	ss := e.s.sessions[id]
	j := &job{kind: "import", title: "Import into t"}
	jid := ss.startJob(j, func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	l := e.ok("GET", "/api/sessions/"+id+"/jobs", nil)["jobs"].([]any)
	if len(l) != 1 {
		t.Fatalf("jobs = %v", l)
	}
	row := l[0].(map[string]any)
	if row["id"] != jid || row["kind"] != "import" || row["title"] != "Import into t" || row["done"] != false || row["progress"].(float64) != -1 {
		t.Fatalf("row = %v", row)
	}
	e.ok("POST", "/api/sessions/"+id+"/jobs/"+jid+"/cancel", nil)
	waitJob(t, e, id, jid)
	row = e.ok("GET", "/api/sessions/"+id+"/jobs", nil)["jobs"].([]any)[0].(map[string]any)
	if row["done"] != true || row["err"] == "" {
		t.Fatalf("a cancelled job is listed as failed: %v", row)
	}
	e.ok("DELETE", "/api/sessions/"+id+"/jobs/"+jid, nil) // finished: dismissed
	if l := e.ok("GET", "/api/sessions/"+id+"/jobs", nil)["jobs"].([]any); len(l) != 0 {
		t.Fatalf("dismissed job still listed: %v", l)
	}
}

func TestBoardExportJobAndDropdownOptions(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "")
	id := e.open()
	panels := []map[string]any{
		{"Title": "n", "Kind": "number", "Query": "select 1 as x"},
		{"Title": "dl", "Kind": "exporter", "Query": "select 1 as x", "Format": "csv"},
		{"Title": "pick", "Kind": "dropdown", "var": "v", "options": []string{"a", "b"}, "default": "a"},
		{"Title": "who", "Kind": "input", "var": "w"},
	}
	e.ok("PUT", "/api/boards/new", map[string]any{"Name": "b1", "Panels": panels})
	if code, _ := e.call("PUT", "/api/boards/b1", map[string]any{"Name": "b1", "Panels": []map[string]any{{"Title": "x", "Kind": "exporter", "Query": "select 1", "Format": "pdf"}}}, "tok"); code != 400 {
		t.Fatalf("bad exporter format: %d", code)
	}
	bs := e.ok("GET", "/api/boards", nil)
	_ = bs
	opts := e.ok("GET", "/api/sessions/"+id+"/boards/b1/options?panel=pick", nil)["options"].([]any)
	if len(opts) != 2 || opts[1] != "b" {
		t.Fatalf("options = %v", opts)
	}
	if code, _ := e.call("GET", "/api/sessions/"+id+"/boards/b1/options?panel=n", nil, "tok"); code != 400 {
		t.Fatalf("options of a number panel: %d", code)
	}
	// the run skips the panels the UIs handle
	if r := e.ok("POST", "/api/sessions/"+id+"/boards/run", map[string]any{"Name": "b1"}); len(r["panels"].([]any)) != 1 {
		t.Fatalf("run = %v", r)
	}
	jid := e.ok("POST", "/api/sessions/"+id+"/boards/b1/export", map[string]any{"panel": "dl", "format": "csv"})["job"].(string)
	waitJob(t, e, id, jid)
	row := e.ok("GET", "/api/sessions/"+id+"/jobs", nil)["jobs"].([]any)[0].(map[string]any)
	if row["kind"] != "export" || !strings.HasSuffix(row["file"].(string), ".csv") || row["err"] != "" {
		t.Fatalf("row = %v", row)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/sessions/"+id+"/jobs/"+jid+"/file", nil)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	if got := strings.ReplaceAll(string(buf[:n]), "\r\n", "\n"); got != "x\n1\n" {
		t.Fatalf("file = %q", got)
	}
}

func TestChatsCRUDAndSearch(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "")
	id := e.open()
	base := "/api/sessions/" + id + "/chats"
	c := e.ok("POST", base, nil)
	cid := c["ID"].(string)
	e.ok("PUT", base+"/"+cid, map[string]any{"Messages": []map[string]any{{"Role": "user", "Content": "How many Orders per day?"}, {"Role": "assistant", "Content": "select 1"}}})
	got := e.ok("GET", base+"/"+cid, nil)
	if got["Title"] != "How many Orders per day?" || len(got["Messages"].([]any)) != 2 {
		t.Fatalf("chat = %v", got)
	}
	if l := e.ok("GET", base+"?q=orders", nil)["chats"].([]any); len(l) != 1 {
		t.Fatalf("search hit = %v", l)
	}
	if l := e.ok("GET", base+"?q=zebra", nil)["chats"].([]any); len(l) != 0 {
		t.Fatalf("search miss = %v", l)
	}
	if code, _ := e.call("GET", base+"/..%2f..%2fx", nil, "tok"); code == 200 {
		t.Fatal("a path in the chat id must be refused")
	}
	e.ok("DELETE", base+"/"+cid, nil)
	if code, _ := e.call("GET", base+"/"+cid, nil, "tok"); code != 404 {
		t.Fatalf("deleted chat: %d", code)
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

func TestPasscodeCheck(t *testing.T) {
	h, err := config.HashPasscode("1234")
	if err != nil {
		t.Fatal(err)
	}
	e := newEnvCfg(t, "", func(c *config.Config) { c.Security.Passcode = h; c.Security.LockAfterMins = 5 })
	if out := e.ok("POST", "/api/passcode/check", map[string]any{"passcode": "nope"}); out["ok"] != false {
		t.Fatalf("wrong passcode: %v", out)
	}
	out := e.ok("POST", "/api/passcode/check", map[string]any{"passcode": "1234"})
	if out["ok"] != true || len(out) != 1 {
		t.Fatalf("right passcode: %v", out)
	}
	m := e.ok("GET", "/api/meta", nil)
	sec := m["security"].(map[string]any)
	if sec["passcode"] != true || sec["lockAfterMins"].(float64) != 5 {
		t.Fatalf("security = %v", sec)
	}
	// three failures slow the fourth attempt down
	for i := 0; i < 3; i++ {
		e.ok("POST", "/api/passcode/check", map[string]any{"passcode": "x"})
	}
	start := time.Now()
	e.ok("POST", "/api/passcode/check", map[string]any{"passcode": "x"})
	if time.Since(start) < 900*time.Millisecond {
		t.Fatal("no delay after 3 failures")
	}
}

func TestRevealOnlyFromLoopback(t *testing.T) {
	e := newEnv(t, "")
	req := httptest.NewRequest("POST", "/api/reveal", strings.NewReader(`{"path":"/tmp"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.9:5555"
	_, err := e.s.reveal(req)
	ae, ok := err.(*apiErr)
	if !ok || ae.Code != 403 || ae.Msg != "reveal works only on the machine running webtuidb" {
		t.Fatalf("err = %v", err)
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

func TestPluginRunEffectsAndReadOnlyHost(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := filepath.Join(config.ConfigDir(), "plugins")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "demo.js"), []byte("// key: alt+P\ncopy('hi'); notify('n');\nvar r = query('select 1 as x');\nr.rows[0][0]\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "bad.js"), []byte("query('delete from t')\n"), 0o600)
	e := newEnv(t, "")
	e.s.o.Conns[0].ReadOnly = true // the fake session cannot pin a connection, so the server must enforce read-only itself
	id := e.open()
	out := e.ok("POST", "/api/sessions/"+id+"/plugins/run", map[string]any{"name": "demo", "context": "table"})
	fx := out["effects"].([]any)
	if out["text"] != "1" || len(fx) != 2 || fx[0].(map[string]any)["kind"] != "copy" || fx[0].(map[string]any)["text"] != "hi" {
		t.Fatalf("out = %v", out)
	}
	code, o := e.call("POST", "/api/sessions/"+id+"/plugins/run", map[string]any{"name": "bad"}, "tok")
	if code != 400 || !strings.Contains(o["error"].(string), "read-only") {
		t.Fatalf("a write from a plugin: %d %v", code, o)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/plugins", nil)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list []map[string]any
	json.NewDecoder(resp.Body).Decode(&list)
	var demo map[string]any
	for _, p := range list {
		if p["name"] == "demo" {
			demo = p
		}
	}
	if demo == nil || demo["key"] != "alt+P" || demo["js"] != true {
		t.Fatalf("plugins = %v", list)
	}
	m := e.ok("GET", "/api/meta", nil)
	var bound bool
	for _, b := range m["keymap"].(map[string]any)["bindings"].([]any) {
		bound = bound || b.(map[string]any)["Action"] == "plugin:demo"
	}
	if !bound {
		t.Fatal("the plugin shortcut is not in the keymap")
	}
}

func TestSupportedActionsParsesWorkspace(t *testing.T) {
	b, err := os.ReadFile("../../web/src/routes/Workspace.tsx")
	if err != nil {
		t.Skip("web sources not present")
	}
	l := SupportedActions(b)
	has := map[string]bool{}
	for _, a := range l {
		has[a] = true
	}
	for _, a := range []string{"help", "commit", "tab_1", "tab_9", "close_pane"} {
		if !has[a] {
			t.Errorf("%s missing from %d parsed actions", a, len(l))
		}
	}
	if got := SupportedActions([]byte("const SUPPORTED = new Set(['a', 'b_c', ...Array.from({ length: 2 }, (_, i) => `t_${i + 1}`)])")); strings.Join(got, ",") != "t_1,t_2,a,b_c" {
		t.Errorf("got %v", got)
	}
}

func TestConnectionsCarryLastConnected(t *testing.T) {
	e := newEnv(t, "")
	list := func() string {
		req, _ := http.NewRequest("GET", e.srv.URL+"/api/connections", nil)
		req.Header.Set("Authorization", "Bearer tok")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var l []map[string]any
		json.NewDecoder(resp.Body).Decode(&l)
		return l[0]["lastConnected"].(string)
	}
	if list() != "" {
		t.Fatal("never connected: lastConnected must be empty")
	}
	e.open()
	if _, err := time.Parse(time.RFC3339, list()); err != nil {
		t.Fatalf("lastConnected after a connect: %v", err)
	}
}
