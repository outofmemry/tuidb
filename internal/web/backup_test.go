package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"webtuidb/internal/config"
	"webtuidb/internal/ui"
)

func TestBackupFileConfined(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	for _, f := range []string{"/etc/passwd", "../x.sql", "a/b.sql", "/tmp/x.sql"} {
		if _, _, err := backupFile(f); err == nil {
			t.Fatalf("%q must be refused", f)
		}
	}
	p, ex, err := backupFile("x.sql")
	if err != nil || ex || filepath.Dir(p) != filepath.Join(config.DataDir(), "backups") {
		t.Fatalf("name inside the dir: %q %v %v", p, ex, err)
	}
	os.WriteFile(p, nil, 0o600)
	if _, ex, _ := backupFile("x.sql"); !ex {
		t.Fatal("an existing file must be reported")
	}
	os.Symlink("/etc/hosts", filepath.Join(filepath.Dir(p), "l.sql"))
	if _, _, err := backupFile("l.sql"); err == nil {
		t.Fatal("a symlink must be refused")
	}
}

func TestBackupDisplayRedactedAndRestoreGated(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "alert1")
	id := e.open()
	if m := e.ok("POST", "/api/sessions/"+id+"/backup", map[string]any{"File": "x.sql"}); m["tool"].(map[string]any)["Found"] == true && m["display"] == "" {
		t.Fatalf("a found tool must give a command: %v", m)
	}
	bin := t.TempDir() // stub tools so the test does not depend on a database client install
	for _, n := range []string{"pg_dump", "pg_restore", "psql"} {
		os.WriteFile(filepath.Join(bin, n), []byte("#!/bin/sh\n"), 0o755)
	}
	t.Setenv("PATH", bin)
	b := e.ok("POST", "/api/sessions/"+id+"/backup", map[string]any{"File": "x.sql"})
	if d := b["display"].(string); !strings.Contains(d, "pg_dump") || strings.Contains(d, "pw") || b["tool"].(map[string]any)["Name"] != "pg_dump" {
		t.Fatalf("backup: %v", b)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/backup", map[string]any{}, "tok"); code != 400 {
		t.Fatalf("file required: %d", code)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/restore", map[string]any{"File": "x.sql"}, "tok"); code != 400 {
		t.Fatalf("restoring a missing file must be refused: %d", code)
	}
	touchBackup(t, "x.sql")
	r := e.ok("POST", "/api/sessions/"+id+"/restore", map[string]any{"File": "x.sql"})
	if r["gate"].(map[string]any)["confirm"] != true {
		t.Fatalf("restore must show the gate: %v", r)
	}
	code, _ := e.call("POST", "/api/sessions/"+id+"/restore", map[string]any{"File": "x.sql", "Run": true}, "tok")
	if code != 409 {
		t.Fatalf("an unconfirmed restore must not start: %d", code)
	}
}

func TestBackupRefusedOnReadOnlyConnection(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "")
	e.ok("PUT", "/api/connections", []map[string]any{{"Name": "c1", "Driver": "postgres", "Host": "h", "Database": "app", "Password": "pw", "ReadOnly": true}})
	id := e.open()
	touchBackup(t, "x.sql")
	for _, op := range []string{"restore", "backup"} {
		if code, _ := e.call("POST", "/api/sessions/"+id+"/"+op, map[string]any{"File": "x.sql"}, "tok"); code != 400 {
			t.Errorf("%s on a read-only connection: %d", op, code)
		}
	}
}

func TestToolPathsAreChecked(t *testing.T) {
	e := newEnv(t, "")
	conn := func(tools map[string]string) []map[string]any {
		return []map[string]any{{"Name": "c1", "Driver": "postgres", "Host": "h", "Database": "app", "Password": "pw", "Tools": tools}}
	}
	e.ok("PUT", "/api/connections", conn(map[string]string{"pg_dump": "/opt/pg16/bin/pg_dump-16"}))
	for _, bad := range []map[string]string{{"pg_dump": "/bin/sh"}, {"pg_dump": "pg_dump"}, {"sh": "/bin/sh"}} {
		if code, _ := e.call("PUT", "/api/connections", conn(bad), "tok"); code != 400 {
			t.Errorf("%v: %d, want 400", bad, code)
		}
	}
}

func TestRestoreIntoNewDatabase(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "alert1")
	e.s.o.Connect = func(context.Context, config.Connection) (*ui.Opened, error) {
		return &ui.Opened{Sess: &adminFake{e.fake}}, nil
	}
	bin := t.TempDir()
	tool := filepath.Join(bin, "pg_restore-16")
	os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	e.ok("PUT", "/api/connections", []map[string]any{{"Name": "c1", "Driver": "postgres", "Host": "h", "Database": "app", "Password": "pw", "SafeMode": "alert1", "Tools": map[string]string{"pg_restore": tool}}})
	id := e.open()
	body := map[string]any{"File": "x.dump", "Format": "custom", "NewDatabase": "fresh"}
	touchBackup(t, "x.dump")
	r := e.ok("POST", "/api/sessions/"+id+"/restore", body)
	if d := r["display"].(string); !strings.Contains(d, tool) { // the target database travels in the environment, not in the shown command
		t.Fatalf("display = %q", d)
	}
	if len(r["create"].([]any)) != 1 || r["gate"].(map[string]any)["confirm"] != true {
		t.Fatalf("review = %v", r)
	}
	body["Run"] = true
	if code, _ := e.call("POST", "/api/sessions/"+id+"/restore", body, "tok"); code != 409 || len(e.fake.applied) != 0 {
		t.Fatalf("an unconfirmed restore must not create anything: %d %v", code, e.fake.applied)
	}
	body["Confirmed"] = true
	e.ok("POST", "/api/sessions/"+id+"/restore", body)
	if len(e.fake.applied) != 1 || e.fake.applied[0][0].Text != "CREATE DATABASE fresh" {
		t.Fatalf("applied = %v", e.fake.applied)
	}
	if o := e.s.o.State.BackupOptions("c1"); o["restore.format"] != "custom" || o["backup.file"] != "" || o["restore.file"] != "" {
		t.Fatalf("remembered options = %v", o)
	}
}
