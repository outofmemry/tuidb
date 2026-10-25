package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHistoryMigrateLegacySkipsLiveCollision(t *testing.T) {
	h := &History{Dir: t.TempDir()}
	os.WriteFile(filepath.Join(h.Dir, "prod_db.jsonl"), []byte(`{"id":"1","sql":"select 1","src":"editor"}`+"\n"), 0o600)
	h.MigrateLegacy([]string{"prod_db", "prod db"})
	if got, _ := h.List("prod_db", "", 0); len(got) != 1 {
		t.Fatalf("prod_db lost its history: %v", got)
	}
	if got, _ := h.List("prod db", "", 0); len(got) != 0 {
		t.Fatalf("prod db stole history: %v", got)
	}
	// without a collision the legacy file is adopted once
	os.WriteFile(filepath.Join(h.Dir, "a_b.jsonl"), []byte(`{"id":"2","sql":"select 2","src":"editor"}`+"\n"), 0o600)
	h.MigrateLegacy([]string{"a b"})
	if got, _ := h.List("a b", "", 0); len(got) != 1 {
		t.Fatalf("legacy not adopted: %v", got)
	}
}

func TestHistoryTruncatesHugeSQL(t *testing.T) {
	h := &History{Dir: t.TempDir()}
	h.Append("c", HistoryEntry{SQL: strings.Repeat("x", 200<<10), Time: time.Now()})
	got, _ := h.List("c", "", 0)
	if len(got) != 1 || len(got[0].SQL) > maxSQLBytes+64 {
		t.Fatalf("sql not truncated: %d", len(got[0].SQL))
	}
}

func TestQueriesUpdateSharedAndRelativeDir(t *testing.T) {
	root, shared := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(shared, "x.sql"), []byte("select 1"), 0o600)
	q := &Queries{Root: root}
	q.AddDir(shared)
	if err := q.Update(filepath.ToSlash(filepath.Join(shared, "x.sql")), "drop table t"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("shared update: %v", err)
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(filepath.Dir(shared)); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	q2 := &Queries{Root: root}
	q2.AddDir(filepath.Base(shared))
	if !filepath.IsAbs(q2.shared[0]) {
		t.Fatalf("shared dir not absolute: %q", q2.shared[0])
	}
}

func TestSaveIfAbsentExclusive(t *testing.T) {
	q := &Queries{Root: t.TempDir()}
	if _, err := q.SaveIfAbsent("", "a", nil, "select 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := q.SaveIfAbsent("", "a", nil, "select 2"); !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
	if got, _ := q.Read("a.sql"); !strings.Contains(got.SQL, "select 1") {
		t.Fatalf("overwritten: %q", got.SQL)
	}
}

func TestStatePreservesUnknownKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(p, []byte(`{"future_key":{"a":1},"connection_sort":"name"}`), 0o600)
	s, err := LoadState(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.do(func(d *stateFile) { d.Files = []string{"x"} }); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "future_key") || !strings.Contains(string(b), "recent_files") {
		t.Fatalf("state: %s", b)
	}
}
