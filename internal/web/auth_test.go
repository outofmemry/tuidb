package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"webtuidb/internal/config"
)

func TestLoginCookie(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) { c.Web.Password = "hunter2" })
	hit := func(method, path, body string, cookie *http.Cookie) *http.Response {
		req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	if r := hit("GET", "/api/connections", "", nil); r.StatusCode != 401 {
		t.Fatalf("anonymous: %d", r.StatusCode)
	}
	if r := hit("POST", "/api/login", `{"password":"nope"}`, nil); r.StatusCode != 401 || len(r.Cookies()) != 0 {
		t.Fatalf("wrong password: %d", r.StatusCode)
	}
	r := hit("POST", "/api/login", `{"password":"hunter2"}`, nil)
	if r.StatusCode != 200 || len(r.Cookies()) != 1 {
		t.Fatalf("login: %d", r.StatusCode)
	}
	c := r.Cookies()[0]
	if !c.HttpOnly || len(c.Value) != 64 || c.MaxAge != 12*3600 {
		t.Fatalf("cookie: %+v", c)
	}
	if r := hit("GET", "/api/connections", "", c); r.StatusCode != 200 {
		t.Fatalf("cookie must replace the token: %d", r.StatusCode)
	}
	if r := hit("GET", "/api/connections", "", &http.Cookie{Name: cookieName, Value: "guess"}); r.StatusCode != 401 {
		t.Fatalf("forged cookie: %d", r.StatusCode)
	}
	if r := hit("GET", "/", "", nil); r.StatusCode == 401 {
		t.Fatal("the static index must not need a login")
	}
	var meta map[string]any
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/meta", nil)
	req.AddCookie(c)
	resp, _ := http.DefaultClient.Do(req)
	json.NewDecoder(resp.Body).Decode(&meta)
	resp.Body.Close()
	if meta["login"] != true {
		t.Fatalf("meta: %v", meta["login"])
	}
	// no password configured: the endpoint is off
	e2 := newEnv(t, "")
	if code, _ := e2.call("POST", "/api/login", map[string]string{"password": ""}, ""); code != 404 {
		t.Fatalf("login without a password: %d", code)
	}
}

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
