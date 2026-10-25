package web_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/store"
	"webtuidb/internal/web"

	_ "webtuidb/internal/db/postgres"
)

// Real driver, real server: open -> rows -> edit -> plan -> apply -> verify -> restore.
// Run with WEBTUIDB_INTEGRATION=1 and `make up` (compose postgres).
func TestPostgresRoundTrip(t *testing.T) {
	if os.Getenv("WEBTUIDB_INTEGRATION") == "" {
		t.Skip("set WEBTUIDB_INTEGRATION=1 (and `make up`) to run")
	}
	url := os.Getenv("WEBTUIDB_TEST_POSTGRES")
	if url == "" {
		url = "postgres://webtuidb:webtuidb@127.0.0.1:55432/webtuidb"
	}
	c, err := config.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	c.Name = "pg"
	dir := t.TempDir()
	st, _ := store.LoadState(dir + "/state.json")
	srv := httptest.NewServer(web.New(web.Options{
		Cfg: config.Defaults(), Conns: []config.Connection{c}, State: st, Token: "t",
		Hist: &store.History{Dir: dir + "/h"}, Queries: &store.Queries{Root: dir + "/q"},
	}).Handler())
	defer srv.Close()

	call := func(method, path string, body any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer t")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		if resp.StatusCode != 200 {
			t.Fatalf("%s %s: %d %v", method, path, resp.StatusCode, out)
		}
		return out
	}
	sid := call("POST", "/api/sessions", map[string]any{"Name": "pg"})["id"].(string)
	defer call("DELETE", "/api/sessions/"+sid, nil)

	ref := map[string]any{"Schema": "public", "Name": "customers", "Kind": "table"}
	name := func() string {
		r := call("POST", "/api/sessions/"+sid+"/rows", map[string]any{"ref": ref, "sort": []any{map[string]any{"Column": "id"}}})
		return *ptr(r["cells"].([]any)[0].([]any)[1])
	}
	orig := name()
	set := func(v string) {
		r := call("POST", "/api/sessions/"+sid+"/rows", map[string]any{"ref": ref, "sort": []any{map[string]any{"Column": "id"}}})
		p := call("POST", "/api/sessions/"+sid+"/plan", map[string]any{
			"resultId": r["resultId"],
			"updates":  []any{map[string]any{"row": 0, "set": map[string]any{"name": v}}},
		})
		call("POST", "/api/sessions/"+sid+"/apply", map[string]any{"planId": p["planId"]})
	}
	defer set(orig) // leave the seed data as it was
	set("web-roundtrip")
	if got := name(); got != "web-roundtrip" {
		t.Fatalf("name after apply = %q", got)
	}
}

func ptr(v any) *string { s, _ := v.(string); return &s }
