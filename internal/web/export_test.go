package web

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"webtuidb/internal/config"
)

func TestExportModes(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	post := func(body map[string]any) (int, string) {
		code, out := e.call("POST", "/api/sessions/"+id+"/export", body, "tok")
		if code == 200 {
			return code, ""
		}
		return code, out["error"].(string)
	}
	if code, msg := post(map[string]any{"mode": "table", "ref": map[string]string{"Name": "t"}, "format": "csv", "columns": []string{"id"}}); code != 200 {
		t.Fatalf("table export: %d %s", code, msg)
	}
	if code, _ := post(map[string]any{"mode": "query", "text": "select 1", "format": "json"}); code != 200 {
		t.Fatalf("query export: %d", code)
	}
	if code, _ := post(map[string]any{"mode": "query", "text": "delete from t", "format": "csv"}); code != 400 {
		t.Fatalf("a write must not be re-run by an export: %d", code)
	}
	if code, _ := post(map[string]any{"mode": "page", "ref": map[string]string{"Name": "t"}, "format": "nope"}); code != 400 {
		t.Fatalf("unknown format: %d", code)
	}
}

func TestExportOptionsAndFileName(t *testing.T) {
	e, _ := richEnv(t, func(c *config.Config) { c.Export.Filename = "{connection}-{table}" })
	id := e.open()
	resp := e.post("/api/sessions/"+id+"/export", map[string]any{"mode": "query", "format": "csv", "text": "select 1", "nullAs": "∅"})
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "c1-result.csv") {
		t.Fatalf("pattern name: %q", cd)
	}
	resp = e.post("/api/sessions/"+id+"/export", map[string]any{"mode": "query", "format": "csv", "text": "select 1", "fileName": "a/b.csv"})
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "a_b.csv") {
		t.Fatalf("typed name: %q", cd)
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
