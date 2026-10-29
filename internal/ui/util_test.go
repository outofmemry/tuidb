package ui

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// T-P3-03: dialog prose breaks at spaces, not inside words or numbers.
func TestM18WordWrapKeepsWords(t *testing.T) {
	s := "Rows are identified by all of their values, so every row identical to an edited or deleted one is changed. Error 1045 SQLSTATE 23514"
	words := map[string]bool{}
	for _, w := range strings.Fields(s) {
		words[w] = true
	}
	for _, l := range wordWrap(s, 23) {
		if width(l) > 23 {
			t.Errorf("line too wide: %q", l)
		}
		for _, w := range strings.Fields(l) {
			if !words[w] {
				t.Errorf("word cut: %q in %q", w, l)
			}
		}
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
