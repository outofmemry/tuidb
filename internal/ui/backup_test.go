package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/config"
)

func TestM13ToggleExtraAddsAndRemovesFlags(t *testing.T) {
	got := toggleExtra("--clean -j 4", []string{"--clean", "-j 4", "--if-exists"}, []bool{false, true, true})
	if got != "-j 4 --if-exists" {
		t.Fatalf("got %q", got)
	}
}

// P1-10: a backup over an existing file asks first, and the expanded default name is not remembered.
func TestTUI1BackupAsksBeforeOverwriteAndKeepsPattern(t *testing.T) {
	fakeTool(t, "pg_dump")
	h := newHarness(t, nil)
	h.connect()
	h.prefix("b")
	h.press("enter") // the default file name, submitted as it is
	if got := h.a.d.State.BackupOptions("shop")["backup.file"]; got != "" {
		t.Fatalf("the expanded default %q was remembered", got)
	}
	h.press("esc")
	h.prefix("b")
	existing := filepath.Join(t.TempDir(), "old.sql")
	writeTo(t, existing, "old", 0o644)
	setField(t, h, "file", existing)
	h.press("enter")
	h.press("enter")
	h.want("Overwrite") // the dialog title is truncated to the box
}

// P2-71: tool output and job errors lose escape sequences.
func TestTUI1BackupLinesAreSanitised(t *testing.T) {
	d := &backupRunDlg{}
	d.add("bad \x1b]52;c;ZXZpbA==\x07name \x1b[31mred\x1b[0m")
	if strings.ContainsRune(d.lines[0], 0x1b) || strings.Contains(d.lines[0], "ZXZpbA") {
		t.Fatalf("line %q", d.lines[0])
	}
	e := &jobEntry{done: true, err: errors.New("x \x1b]0;title\x07y")}
	if s := e.status(""); strings.ContainsRune(s, 0x1b) || strings.Contains(s, "title") {
		t.Fatalf("status %q", s)
	}
}

func TestM3BackupShowsRedactedCommandAndStreams(t *testing.T) {
	fakeTool(t, "pg_dump")
	h := newHarness(t, nil)
	h.connect()
	h.prefix("b")
	h.want("Backup shop")
	setField(t, h, "file", filepath.Join(t.TempDir(), "out.sql"))
	h.press("enter")
	h.want("pg_dump")
	h.wantNot("PGPASSWORD")
	h.wait = time.Second // starting the tool takes longer than the harness's idle window
	h.press("enter")
	h.want("dumping")
	h.want("done")
}

func TestM3BackupToolMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	h := newHarness(t, nil)
	h.connect()
	h.prefix("b")
	h.want("pg_dump not found") // said before the form is filled in (T-P3-17)
	h.want("Backup shop")       // the form still opens: Ctrl+t inside it is where the path is set
}

func TestM3RestoreGoesThroughSafeMode(t *testing.T) {
	fakeTool(t, "psql")
	fakeTool(t, "pg_restore")
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SafeMode = "alert1" })
	h.connect()
	dump := filepath.Join(t.TempDir(), "in.sql")
	writeTo(t, dump, "select 1;", 0o644)
	h.prefix("B")
	setField(t, h, "file", dump)
	h.press("enter")
	h.want("Restore shop?")
	h.press("y")
	h.want("send to the server?") // the safe-mode gate, with the command line as the statement
	h.press("n")
	h.wantNot("dumping")
}

func TestM7BackupToolPathAndRememberedOptions(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	tool := filepath.Join(t.TempDir(), "my-pg-dump")
	writeTo(t, tool, "#!/bin/sh\nexit 0\n", 0o755)
	h := newHarness(t, nil)
	h.connect()
	h.prefix("b")
	h.press("ctrl+t")
	h.want("Tool paths for shop")
	setField(t, h, "pg_dump", tool)
	h.press("enter")
	if h.a.d.Conns[0].Tools["pg_dump"] != tool {
		t.Fatalf("tools %v", h.a.d.Conns[0].Tools)
	}
	if saved, _ := config.LoadConnections(h.a.d.ConnPath); len(saved) != 1 || saved[0].Tools["pg_dump"] != tool {
		t.Fatalf("saved %+v", saved)
	}
	out := filepath.Join(t.TempDir(), "x.sql")
	setField(t, h, "file", out)
	setChoice(t, h, "gzip", "true")
	h.press("enter")
	h.want("my-pg-dump")
	h.press("esc")
	mem := h.a.d.State.BackupOptions("shop")
	if mem["backup.file"] != out || mem["backup.gzip"] != "true" {
		t.Fatalf("remembered %v", mem)
	}
	h.prefix("b")
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	if v := f.values(); v["file"] != out || v["gzip"] != "true" {
		t.Fatalf("form %v", v)
	}
}

func TestM7RestoreIntoANewDatabase(t *testing.T) {
	fakeTool(t, "psql")
	fakeTool(t, "pg_restore")
	h := newHarness(t, nil)
	h.connect()
	dump := filepath.Join(t.TempDir(), "in.sql")
	writeTo(t, dump, "select 1;", 0o644)
	h.prefix("B")
	setField(t, h, "file", dump)
	setField(t, h, "newdb", "fresh")
	h.press("enter")
	h.want("First creates database fresh")
	h.press("y")
	if len(h.fake.applied) != 1 || h.fake.applied[0][0].Text != "-- create_database fresh" {
		t.Fatalf("applied %+v", h.fake.applied)
	}
	h.want("Restore in.sql") // then the restore runs against it
}
