package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestM4HistorySaveAndImport(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	queryResult(h, "select 1")
	if es, _ := h.a.d.Hist.List("shop", "", 0); len(es) == 0 {
		t.Fatal("no history to export")
	}
	out := filepath.Join(t.TempDir(), "h.json")
	h.press("esc", "ctrl+d", "alt+h") // back to the sidebar
	h.a.ws().focusSB = true
	h.a.ws().sb.switchTab(2)
	h.press("S")
	setField(t, h, "path", out)
	h.fakeFormChoice("format", "json")
	h.press("enter")
	if b, err := os.ReadFile(out); err != nil || !strings.Contains(string(b), "select 1") {
		t.Fatalf("export file %q: %v", b, err)
	}
	before, _ := h.a.d.Hist.List("shop", "", 0)
	h.press("i")
	setField(t, h, "path", out)
	h.press("enter")
	after, _ := h.a.d.Hist.List("shop", "", 0)
	if len(after) != len(before) { // the same entries again are skipped, not duplicated
		t.Fatalf("import: %d entries, want %d", len(after), len(before))
	}
}

func TestM6OpenFileRecentAndViewer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.sql")
	if err := os.WriteFile(path, []byte("select 42 from dual;"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, nil)
	h.run("open_file") // welcome: the read-only viewer
	setField(t, h, "path", path)
	h.press("enter")
	h.want("SQL viewer")
	h.want("select 42")
	h.press("esc")
	if rf := h.a.d.State.RecentFiles(); len(rf) != 1 || rf[0] != path {
		t.Fatalf("recent %v", rf)
	}
	h.connect()
	h.run("open_recent")
	h.want("q.sql")
	h.press("enter")
	if q := h.queryTab(); q.ed.Text() != "select 42 from dual;" || q.Title() != "q.sql" {
		t.Fatalf("tab %q %q", q.Title(), q.ed.Text())
	}
}
