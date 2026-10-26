package ui

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestConfirmIgnoresKeysWhileArming(t *testing.T) {
	ran := 0
	d := &confirmDlg{armed: time.Now().Add(time.Hour), onYes: func() tea.Cmd { ran++; return nil }}
	if o, _ := feed(d, tea.KeyPressMsg{Code: 'y', Text: "y"}); o == nil || ran != 0 {
		t.Fatal("y inside the arming window must not approve")
	}
	d.armed = time.Now().Add(-time.Second)
	if o, _ := feed(d, tea.KeyPressMsg{Code: 'y', Text: "y"}); o != nil || ran != 1 {
		t.Fatal("y after the window approves")
	}
}

func TestWriteAtomicKeepsOldFileOnFailure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "out.txt")
	os.WriteFile(p, []byte("old"), 0o600)
	if err := writeAtomic(p, func(w io.Writer) error { w.Write([]byte("par")); return errors.New("boom") }); err == nil {
		t.Fatal("want error")
	}
	if b, _ := os.ReadFile(p); string(b) != "old" {
		t.Fatalf("old file lost: %q", b)
	}
	if es, _ := os.ReadDir(filepath.Dir(p)); len(es) != 1 {
		t.Fatal("temp file left behind")
	}
	if err := writeFile(p, "new"); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
}

func TestIfFreeAsksBeforeOverwrite(t *testing.T) {
	h := newHarness(t, nil)
	p := filepath.Join(t.TempDir(), "x")
	ran := 0
	run := func() tea.Cmd { ran++; return nil }
	h.a.ifFree(run, p)
	if ran != 1 || len(h.a.overlays) != 0 {
		t.Fatal("a free path runs at once")
	}
	os.WriteFile(p, nil, 0o600)
	h.a.ifFree(run, p)
	if ran != 1 || len(h.a.overlays) != 1 {
		t.Fatal("an existing path must ask first")
	}
}

func TestIfNoQueryAsksBeforeOverwrite(t *testing.T) {
	h := newHarness(t, nil)
	ran := 0
	run := func() tea.Cmd { ran++; return nil }
	h.a.ifNoQuery("", "daily", run)
	h.a.d.Queries.Save("", "daily", nil, "select 1")
	h.a.ifNoQuery("", "daily", run)
	if ran != 1 || len(h.a.overlays) != 1 {
		t.Fatalf("ran %d overlays %d", ran, len(h.a.overlays))
	}
}

func TestCheckExpr(t *testing.T) {
	for _, ok := range []string{"now()", "coalesce(a, 'it''s')"} {
		if err := checkExpr(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"1; DROP TABLE t", "1 -- x", "1 /* x", "$$x$$", "'abc", "(1", "1)"} {
		if checkExpr(bad) == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}
