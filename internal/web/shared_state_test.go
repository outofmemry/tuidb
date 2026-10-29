package web

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSharedState(t *testing.T) {
	e := newEnv(t, "")
	e.ok("PUT", "/api/state/groups?conn=c1", map[string]any{"groups": []map[string]any{{"name": "Core", "items": []string{"public.t1"}}}})
	g := e.ok("GET", "/api/state/groups?conn=c1", nil)["groups"].([]any)
	if len(g) != 1 || g[0].(map[string]any)["name"] != "Core" {
		t.Fatalf("groups = %v", g)
	}
	if code, _ := e.call("PUT", "/api/state/groups?conn=c1", map[string]any{"groups": []map[string]any{{"name": "a"}, {"name": "a"}}}, "tok"); code != 400 {
		t.Fatalf("duplicate group names: %d", code)
	}
	if code, _ := e.call("PUT", "/api/state/recent-files", map[string]any{"name": "q.sql"}, "tok"); code != 400 { // a browser only knows names
		t.Fatalf("a bare name must be refused: %d", code)
	}
	e.s.o.State.AddRecentFile("/home/me/dir/q.sql", 5)
	e.ok("PUT", "/api/state/recent-files", map[string]any{"name": "/home/me/dir/q.sql"})
	if rf := e.ok("GET", "/api/state/recent-files", nil); len(rf["files"].([]any)) != 1 {
		t.Fatalf("recent = %v", rf)
	}
	e.ok("PUT", "/api/state/connection-sort", map[string]any{"sort": "tag"})
	if cs := e.ok("GET", "/api/state/connection-sort", nil); cs["sort"] != "tag" || cs["default"] != "name" {
		t.Fatalf("sort = %v", cs)
	}
	if code, _ := e.call("PUT", "/api/state/connection-sort", map[string]any{"sort": "nope"}, "tok"); code != 400 {
		t.Fatalf("bad sort: %d", code)
	}
	e.ok("PUT", "/api/state/backup-options?conn=c1", map[string]any{"options": map[string]string{"backup.gzip": "true", "evil": "x"}})
	e.ok("PUT", "/api/state/backup-options?conn=c1", map[string]any{"options": map[string]string{"restore.gzip": "false"}})
	if o := e.ok("GET", "/api/state/backup-options?conn=c1", nil)["options"].(map[string]any); o["backup.gzip"] != "true" || o["evil"] != nil {
		t.Fatalf("options = %v", o)
	}
}

func TestOpenRecentFileOnlyListedSQL(t *testing.T) {
	e := newEnv(t, "")
	dir := t.TempDir()
	sql, txt := filepath.Join(dir, "a.sql"), filepath.Join(dir, "a.txt")
	os.WriteFile(sql, []byte("select 1;"), 0o600)
	os.WriteFile(txt, []byte("x"), 0o600)
	e.s.o.State.AddRecentFile(sql, 10)
	e.s.o.State.AddRecentFile(txt, 10)
	e.s.o.State.AddRecentFile("rel.sql", 10)
	if r := e.ok("POST", "/api/files/open", map[string]any{"path": sql}); r["text"] != "select 1;" || r["name"] != "a.sql" {
		t.Fatalf("open = %v", r)
	}
	for _, p := range []string{txt, "rel.sql", "/etc/passwd", filepath.Join(dir, "other.sql")} {
		if code, _ := e.call("POST", "/api/files/open", map[string]any{"path": p}, "tok"); code != 400 {
			t.Errorf("%q: %d, want 400", p, code)
		}
	}
}

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
