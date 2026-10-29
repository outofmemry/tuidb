package web

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
	"webtuidb/internal/config"
)

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

func TestConnectionsReloadAndSources(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(src+"/team.toml", []byte("[[connection]]\nname = \"shared\"\ndriver = \"postgres\"\nhost = \"h\"\ndatabase = \"d\"\n"), 0o600)
	e := newEnvCfg(t, "", func(c *config.Config) { c.Sources = []config.Source{{Path: src}} })
	names := func() string {
		var out []string
		resp := e.raw("GET", "/api/connections", nil, nil)
		var cs []config.Connection
		json.NewDecoder(resp.Body).Decode(&cs)
		for _, c := range cs {
			out = append(out, c.Name)
		}
		return strings.Join(out, ",")
	}
	if got := names(); got != "c1" { // no connections.toml yet: the injected list stays
		t.Fatalf("before: %s", got)
	}
	main := e.s.o.ConnPath
	os.WriteFile(main, []byte("[[connection]]\nname = \"c1\"\ndriver = \"postgres\"\nhost = \"h\"\ndatabase = \"app\"\n[[connection]]\nname = \"later\"\ndriver = \"postgres\"\nhost = \"h\"\ndatabase = \"d\"\n"), 0o600)
	if got := names(); got != "c1,later,shared" {
		t.Fatalf("after edit: %s", got)
	}
	os.WriteFile(src+"/team.toml", []byte("[[connection]]\nname = \"shared2\"\ndriver = \"postgres\"\nhost = \"h\"\ndatabase = \"d\"\n"), 0o600)
	if got := names(); got != "c1,later,shared2" {
		t.Fatalf("after source edit: %s", got)
	}
	var cs []config.Connection
	json.NewDecoder(e.raw("GET", "/api/connections", nil, nil).Body).Decode(&cs)
	// a source connection cannot be edited or dropped through the API ...
	edited := append([]config.Connection{}, cs...)
	edited[2].Host = "evil"
	if code, out := e.call("PUT", "/api/connections", edited, "tok"); code != 400 || !strings.Contains(out["error"].(string), "read-only") {
		t.Fatalf("edit: %d %v", code, out)
	}
	if code, _ := e.call("PUT", "/api/connections", cs[:2], "tok"); code != 400 {
		t.Fatalf("remove: %d", code)
	}
	// ... but the rest is saved, and only to the main file
	cs[1].Host = "h2"
	e.ok("PUT", "/api/connections", cs)
	main2, _ := config.LoadConnections(main)
	if len(main2) != 2 || main2[1].Host != "h2" {
		t.Fatalf("main file: %v", main2)
	}
	if got := names(); got != "c1,later,shared2" {
		t.Fatalf("after put: %s", got)
	}
}

func TestConnectionsExportImport(t *testing.T) {
	e := newEnv(t, "")
	if code, _ := e.call("GET", "/api/connections/export?passwords=1", nil, "tok"); code != 400 {
		t.Fatalf("passwords need a passphrase: %d", code)
	}
	r := e.raw("GET", "/api/connections/export?passphrase=s3cret&passwords=1", nil, nil)
	b, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || strings.Contains(string(b), "pw") || !strings.HasPrefix(string(b), "WTDB1") {
		t.Fatalf("export: %d %q", r.StatusCode, b)
	}
	if code, _ := e.call("POST", "/api/connections/import", map[string]any{"contentBase64": base64.StdEncoding.EncodeToString(b), "passphrase": "wrong"}, "tok"); code != 400 {
		t.Fatalf("wrong passphrase: %d", code)
	}
	out := e.ok("POST", "/api/connections/import", map[string]any{"contentBase64": base64.StdEncoding.EncodeToString(b), "passphrase": "s3cret"})
	if s := out["skipped"].([]any); len(s) != 1 || s[0] != "c1" || len(out["added"].([]any)) != 0 {
		t.Fatalf("an existing name is skipped: %v", out)
	}
	// a file with a new name is added and saved
	e.s.o.Conns[0].Name = "c9"
	b2 := e.raw("GET", "/api/connections/export?passphrase=s3cret", nil, nil)
	nb, _ := io.ReadAll(b2.Body)
	e.s.o.Conns[0].Name = "c1"
	out = e.ok("POST", "/api/connections/import", map[string]any{"contentBase64": base64.StdEncoding.EncodeToString(nb), "passphrase": "s3cret"})
	if a := out["added"].([]any); len(a) != 1 || a[0] != "c9" {
		t.Fatalf("add: %v", out)
	}
	if saved, _ := config.LoadConnections(e.s.o.ConnPath); len(saved) != 2 {
		t.Fatalf("saved: %v", saved)
	}
}

func TestPutConnsRefusesNewCommands(t *testing.T) {
	e := newEnv(t, "")
	c := map[string]any{"Name": "c1", "Driver": "postgres", "Host": "h", "Database": "app", "PasswordCmd": "sh -c id"}
	if code, _ := e.call("PUT", "/api/connections", []any{c}, "tok"); code != 400 {
		t.Fatalf("a new password_cmd must be refused: %d", code)
	}
	delete(c, "PasswordCmd")
	if code, _ := e.call("PUT", "/api/connections", []any{c}, "tok"); code != 200 && code != 204 {
		t.Fatalf("a plain edit must still work: %d", code)
	}
}

func TestSSHURLRoundTrip(t *testing.T) {
	e := newEnv(t, "")
	c := e.ok("POST", "/api/connections/parse", map[string]any{"url": "postgresql+ssh://u:p@db:5432/app?ssh_host=bastion&ssh_user=me&ssh_key=/k&ssh_port=2222&tLSMode=require"})
	ssh, _ := c["SSH"].(map[string]any)
	if ssh["Host"] != "bastion" || ssh["User"] != "me" || ssh["Key"] != "/k" || ssh["Port"] != float64(2222) || c["SSLMode"] != "require" {
		t.Fatalf("parsed = %v", c)
	}
	u := e.ok("POST", "/api/connections/format", c)["url"].(string)
	if !strings.Contains(u, "+ssh") || !strings.Contains(u, "ssh_host=bastion") || !strings.Contains(u, "2222") {
		t.Fatalf("formatted = %q", u)
	}
}

func TestPutConnectionsStaleStamp(t *testing.T) {
	e := newEnv(t, "")
	resp := e.raw("GET", "/api/connections", nil, nil)
	tag := resp.Header.Get("ETag")
	if tag == "" {
		t.Fatal("no ETag")
	}
	body := []byte(`[{"Name":"c1","Driver":"postgres","Host":"h","Database":"app"}]`)
	put := func(tag string) int {
		hdr := map[string]string{}
		if tag != "" {
			hdr["If-Match"] = tag
		}
		return e.raw("PUT", "/api/connections", bytes.NewReader(body), hdr).StatusCode
	}
	if put("") != 200 { // current client: no header, no check
		t.Fatal("PUT without a stamp must still work")
	}
	if c := put(tag); c != 409 { // the first PUT changed the files
		t.Errorf("stale stamp: %d", c)
	}
	tag = e.raw("GET", "/api/connections", nil, nil).Header.Get("ETag")
	if c := put(tag); c != 200 {
		t.Errorf("fresh stamp: %d", c)
	}
}

func TestTestConnRefusesCommands(t *testing.T) {
	e := newEnv(t, "")
	for _, c := range []map[string]any{
		{"Name": "x", "Driver": "postgres", "Host": "h", "PasswordCmd": "sh -c id"},
		{"Name": "x", "Driver": "postgres", "Host": "h", "Bootstrap": []string{"select 1"}},
	} {
		if code, _ := e.call("POST", "/api/connections/test", c, "tok"); code != 400 {
			t.Errorf("%v: %d", c, code)
		}
	}
}

func TestAskPasswordOverTheWire(t *testing.T) {
	e := newEnv(t, "")
	e.s.o.Conns = append(e.s.o.Conns, config.Connection{Name: "ask", Driver: config.Postgres, Host: "h", Password: config.PasswordAsk})
	e.s.stamp = "" // the next reload sees no change anyway: ConnPath has no file
	code, out := e.call("POST", "/api/sessions", map[string]any{"name": "ask"}, "tok")
	if code != 422 || out["passwordRequired"] != true {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ = e.call("POST", "/api/sessions", map[string]any{"name": "ask", "password": "pw"}, "tok"); code != 200 {
		t.Errorf("with a password: %d", code)
	}
}
