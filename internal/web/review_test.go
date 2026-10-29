package web

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"webtuidb/internal/config"
)

func TestOriginCheckExactListenAddress(t *testing.T) {
	s := &Server{o: Options{Listen: "127.0.0.1:7788"}}
	try := func(origin, host string) bool {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Origin", origin)
		r.Host = host
		return s.originOK(r)
	}
	if !try("http://127.0.0.1:7788", "127.0.0.1:7788") || !try("http://localhost:5173", "localhost:5173") {
		t.Error("the listen address and a proxy that keeps Host must pass")
	}
	if try("http://localhost:3000", "127.0.0.1:7788") || try("http://evil.example", "127.0.0.1:7788") {
		t.Error("another local port is another app")
	}
	if !try("http://localhost:7788", "127.0.0.1:7788") {
		t.Error("localhost on the listen port is the same app")
	}
	t.Setenv("WEBTUIDB_DEV_ORIGIN", "1")
	if try("http://localhost:3000", "127.0.0.1:7788") {
		t.Error("=1 is not an allow-list entry")
	}
	t.Setenv("WEBTUIDB_DEV_ORIGIN", "http://localhost:3000")
	if !try("http://localhost:3000", "127.0.0.1:7788") || try("http://localhost:3001", "127.0.0.1:7788") {
		t.Error("only the listed origin passes")
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

func TestImportDropsCommandsAndReportsThem(t *testing.T) {
	e := newEnv(t, "")
	src := "[[connection]]\nname = 'n'\ndriver = 'postgres'\nhost = 'h'\npassword_cmd = 'sh -c id'\nbootstrap = ['select 1']\n"
	out := e.ok("POST", "/api/connections/import", map[string]any{"contentBase64": base64.StdEncoding.EncodeToString([]byte(src))})
	if d := out["dropped"].([]any); len(d) != 2 {
		t.Errorf("%v", out)
	}
	b, _ := os.ReadFile(e.s.o.ConnPath)
	if strings.Contains(string(b), "password_cmd") || strings.Contains(string(b), "bootstrap") {
		t.Errorf("saved:\n%s", b)
	}
	if n := len(e.s.o.Conns); n != 2 {
		t.Errorf("conns: %d", n)
	}
}

func TestExportViaPostKeepsPassphraseOutOfURL(t *testing.T) {
	e := newEnv(t, "")
	resp := e.post("/api/connections/export", map[string]any{"passphrase": "pw", "passwords": true})
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	var b bytes.Buffer
	b.ReadFrom(resp.Body)
	if !strings.HasPrefix(b.String(), "WTDB1") {
		t.Errorf("not encrypted: %.20q", b.String())
	}
}

func TestLoginLockout(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) { c.Web.Password = "right" })
	for i := 0; i < loginMaxFails; i++ {
		e.s.logins.failed()
	}
	r, _ := http.NewRequest("POST", e.srv.URL+"/api/login", strings.NewReader(`{"password":"right"}`))
	r.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(r)
	if resp.StatusCode != 401 {
		t.Errorf("locked out login: %d", resp.StatusCode)
	}
}

func TestDataDiffRefusesTruncatedRead(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) { c.General.RowLimit = 3 })
	id := e.open()
	code, out := e.call("POST", "/api/sessions/"+id+"/tools/data-diff", map[string]any{"from": map[string]string{"Name": "t1"}, "to": map[string]string{"Name": "d2"}}, "tok")
	if code != 400 || !strings.Contains(out["error"].(string), "row limit") {
		t.Errorf("%d %v", code, out)
	}
}

// W-P2-01/02: the row limit is in /api/meta, and /rows says when the clamp cut the client's request short.
func TestRowLimitExposed(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) { c.General.RowLimit = 3 })
	if m := e.ok("GET", "/api/meta", nil); m["rowLimit"] != float64(3) {
		t.Errorf("meta rowLimit %v", m["rowLimit"])
	}
	id := e.open()
	ref := map[string]any{"name": "t", "kind": "table"}
	for _, c := range []struct {
		limit int
		want  bool
	}{{0, true}, {1000, true}, {3, false}} {
		if out := e.ok("POST", "/api/sessions/"+id+"/rows", map[string]any{"ref": ref, "limit": c.limit}); out["truncated"] != c.want {
			t.Errorf("limit %d: truncated %v", c.limit, out["truncated"])
		}
	}
}

func TestSessionCloseRemovesExportFile(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	f, _ := os.CreateTemp("", "webtuidb-export-*")
	f.Close()
	ss := e.s.sessions[id]
	ss.jobs["j"] = &job{file: f.Name(), done: true}
	e.ok("DELETE", "/api/sessions/"+id, nil)
	if _, err := os.Stat(f.Name()); err == nil {
		os.Remove(f.Name())
		t.Error("export file left behind")
	}
}
