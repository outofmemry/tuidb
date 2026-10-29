package web

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"webtuidb/internal/config"
)

func TestQueryUpdateAndDeleteGuards(t *testing.T) {
	e := newEnv(t, "")
	e.ok("POST", "/api/queries", map[string]any{"folder": "reports", "name": "daily", "sql": "select 1"})
	p := "/api/queries/reports/daily.sql"
	if code, _ := e.call("PUT", p, nil, "tok"); code != 400 {
		t.Fatalf("empty body: %d", code)
	}
	if code, _ := e.call("PUT", p, map[string]any{"sqll": "x"}, "tok"); code != 400 {
		t.Fatalf("missing sql: %d", code)
	}
	e.ok("PUT", p, map[string]any{"sql": "select 2"})
	if code, _ := e.call("DELETE", "/api/queries/reports/", nil, "tok"); code != 400 {
		t.Fatalf("folder delete: %d", code)
	}
	req := e.raw("PUT", p, strings.NewReader(`{"sql":"select 3"}`), map[string]string{"If-Match": `"stale"`})
	if req.StatusCode != 409 {
		t.Fatalf("stale If-Match: %d", req.StatusCode)
	}
	e.ok("DELETE", p, nil)
}

func TestImportQueryFileDoesNotOverwrite(t *testing.T) {
	e := newEnv(t, "")
	e.ok("POST", "/api/queries/import-file", map[string]any{"name": "daily.sql", "sql": "select 1"})
	if code, _ := e.call("POST", "/api/queries/import-file", map[string]any{"name": "daily.sql", "sql": "select 2"}, "tok"); code != 409 {
		t.Fatalf("an existing query must not be replaced silently: %d", code)
	}
	e.ok("POST", "/api/queries/import-file", map[string]any{"name": "daily.sql", "sql": "select 2", "overwrite": true})
}

func TestSharedDirsAreListed(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "team.sql"), []byte("select 1"), 0o600)
	e := newEnvCfg(t, "", func(c *config.Config) { c.Queries.SharedDirs = []string{dir} })
	b, _ := io.ReadAll(e.raw("GET", "/api/queries", nil, nil).Body)
	if !strings.Contains(string(b), `"Shared":true`) || !strings.Contains(string(b), "team") {
		t.Fatalf("queries: %s", b)
	}
}

func TestQueryGetAndListEmitETag(t *testing.T) {
	e := newEnv(t, "")
	e.ok("POST", "/api/queries", map[string]any{"folder": "r", "name": "daily", "sql": "select 1"})
	p := "/api/queries/r/daily.sql"
	resp := e.raw("GET", p, nil, nil)
	tag := resp.Header.Get("ETag")
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if tag == "" || body["sql"] != "select 1" || body["name"] != "daily" {
		t.Fatalf("GET: etag=%q body=%v", tag, body)
	}
	lr := e.raw("GET", "/api/queries", nil, nil)
	var l []map[string]any
	json.NewDecoder(lr.Body).Decode(&l)
	if len(l) != 1 || l[0]["etag"] != tag {
		t.Fatalf("list etag %v want %s", l, tag)
	}
	ok := e.raw("PUT", p, strings.NewReader(`{"sql":"select 2"}`), map[string]string{"If-Match": tag})
	if ok.StatusCode != 200 {
		t.Fatalf("PUT with fresh etag: %d", ok.StatusCode)
	}
	stale := e.raw("PUT", p, strings.NewReader(`{"sql":"select 3"}`), map[string]string{"If-Match": tag})
	if stale.StatusCode != 409 {
		t.Fatalf("PUT with stale etag: %d", stale.StatusCode)
	}
}
