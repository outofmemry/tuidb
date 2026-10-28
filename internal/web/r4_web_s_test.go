package web

import (
	"encoding/json"
	"strings"
	"testing"
)

// P2-26: the backup dialog's option list comes from the backup package.
func TestBackupOptionsRoute(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	for _, restore := range []string{"0", "1"} {
		out := e.ok("GET", "/api/sessions/"+id+"/backup/options?restore="+restore, nil)
		opts, ok := out["options"].([]any)
		extra, ok2 := out["extra"].([]any)
		if !ok || !ok2 || len(opts) == 0 || len(extra) == 0 {
			t.Fatalf("restore=%s: %v", restore, out)
		}
		if o := opts[0].(map[string]any); o["Flag"] == "" || o["Desc"] == "" {
			t.Fatalf("option = %v", o)
		}
	}
}

// P2-27: only a job with a temp file advertises a "file"; a backup has none.
func TestJobFileOnlyWhenDownloadable(t *testing.T) {
	bk := &job{kind: "backup", fileName: "a.sql", path: "/x/a.sql", done: true}
	if bk.summary("j")["file"] != "" || bk.snapshot()["file"] != "" {
		t.Fatalf("a backup has no downloadable file: %v %v", bk.summary("j"), bk.snapshot())
	}
	ex := &job{kind: "export", fileName: "a.csv", file: "/tmp/a", done: true}
	if ex.summary("j")["file"] != "a.csv" || ex.snapshot()["file"] != "a.csv" || ex.summary("j")["download"] != true {
		t.Fatalf("export job: %v", ex.summary("j"))
	}
}

// P2-39: path settings are flagged read-only in the schema.
func TestSettingsSchemaReadOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e := newEnv(t, "")
	seen := map[string]bool{}
	for _, f := range e.ok("GET", "/api/settings/schema", nil)["fields"].([]any) {
		m := f.(map[string]any)
		k := m["key"].(string)
		ro := m["readOnly"] == true
		want := false
		for _, p := range pathSettings {
			want = want || p == k
		}
		if ro != want {
			t.Errorf("%s readOnly = %v, want %v", k, ro, want)
		}
		seen[k] = ro
	}
	if !seen["theme.file"] || !seen["web.tls_cert"] {
		t.Errorf("path settings missing or editable: %v", seen)
	}
}

// P2-48/P2-59: the import request carries null_as, empty_is_null and keep_formula_quote.
func TestImportNullOptions(t *testing.T) {
	e := newEnv(t, "")
	var req importReq
	if err := json.Unmarshal([]byte(`{"source":"csv","null_as":"\\N","empty_is_null":true,"keep_formula_quote":true}`), &req); err != nil {
		t.Fatal(err)
	}
	o, err := e.s.importOpts(req)
	if err != nil || o.NullAs != `\N` || !o.EmptyIsNull || !o.KeepFormulaQuote {
		t.Fatalf("opts = %+v %v", o, err)
	}
}

// P1-01: the chat endpoints speak Go names, and a Go-named body round-trips.
func TestChatsGoNames(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "")
	id := e.open()
	base := "/api/sessions/" + id + "/chats"
	c := e.ok("POST", base, nil)
	cid, _ := c["ID"].(string)
	if cid == "" {
		t.Fatalf("POST /chats = %v", c)
	}
	e.ok("PUT", base+"/"+cid, map[string]any{"Messages": []map[string]any{{"Role": "user", "Content": "hello there"}}})
	got := e.ok("GET", base+"/"+cid, nil)
	if got["ID"] != cid || len(got["Messages"].([]any)) != 1 || !strings.Contains(got["Title"].(string), "hello") {
		t.Fatalf("round trip = %v", got)
	}
	l := e.ok("GET", base, nil)["chats"].([]any)
	if len(l) != 1 || l[0].(map[string]any)["ID"] != cid || l[0].(map[string]any)["Count"] == nil {
		t.Fatalf("list = %v", l)
	}
}
