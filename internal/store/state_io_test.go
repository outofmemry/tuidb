package store

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOpenTabsRoundTrip(t *testing.T) {
	s, _ := LoadState(filepath.Join(t.TempDir(), "state.json"))
	want := []OpenTab{{Kind: "sql", Text: "select 1"}, {Kind: "table", Name: "public.users"}}
	if err := s.SetOpenTabs("pg", want); err != nil {
		t.Fatal(err)
	}
	again, _ := LoadState(s.Path)
	if got := again.OpenTabs("pg"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	s.SetOpenTabs("pg", nil)
	if got := s.OpenTabs("pg"); len(got) != 0 {
		t.Fatalf("not forgotten: %v", got)
	}
}

func TestSharedDirs(t *testing.T) {
	root, shared := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(shared, "team.sql"), []byte("-- name: Team report\nselect 2"), 0o600)
	qs := &Queries{Root: root}
	qs.AddDir(shared)
	qs.AddDir(filepath.Join(root, "missing")) // a folder that does not exist yet is not an error
	if _, err := qs.Save("", "mine", nil, "select 1"); err != nil {
		t.Fatal(err)
	}
	list, err := qs.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("list %v %v", list, err)
	}
	var sq Query
	for _, q := range list {
		if q.Shared {
			sq = q
		}
	}
	if sq.Name != "Team report" || sq.Folder != filepath.Base(shared) {
		t.Fatalf("shared query %+v", sq)
	}
	if got, err := qs.Read(sq.Path); err != nil || !got.Shared || got.SQL != "select 2" {
		t.Fatalf("read %+v %v", got, err)
	}
	if qs.Update(sq.Path, "drop table x") == nil || qs.Delete(sq.Path) == nil {
		t.Fatal("shared query must be read-only")
	}
	if _, err := qs.Read("/etc/passwd"); err == nil {
		t.Fatal("read outside the store and shared dirs")
	}
}

func TestSortAndImport(t *testing.T) {
	now := time.Now()
	qs := []Query{{Name: "b", Modified: now.Add(-time.Hour)}, {Name: "a", Modified: now.Add(-2 * time.Hour)}}
	SortQueries(qs, "modified")
	if qs[0].Name != "b" {
		t.Fatalf("modified: %v", qs)
	}
	SortQueries(qs, "name")
	if qs[0].Name != "a" {
		t.Fatalf("name: %v", qs)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "plain.sql"), []byte("select 1"), 0o600)
	os.WriteFile(filepath.Join(dir, "named.sql"), []byte("-- name: Nice\nselect 2"), 0o600)
	if q, err := ImportSQLFile(filepath.Join(dir, "plain.sql")); err != nil || q.Name != "plain" || q.SQL != "select 1" {
		t.Fatalf("%+v %v", q, err)
	}
	if q, _ := ImportSQLFile(filepath.Join(dir, "named.sql")); q.Name != "Nice" {
		t.Fatalf("%+v", q)
	}
}

func TestHistoryExportImport(t *testing.T) {
	in := []HistoryEntry{{Time: time.Date(2026, 10, 28, 1, 2, 3, 4, time.UTC), Database: "d", SQL: "select 'a,b'\nfrom t", Duration: 12 * time.Millisecond, Source: "editor"}}
	for _, f := range []string{"csv", "json"} {
		var buf bytes.Buffer
		if err := ExportHistory(&buf, in, f); err != nil {
			t.Fatal(err)
		}
		out, err := ImportHistory(&buf)
		if err != nil || len(out) != 1 || !out[0].Time.Equal(in[0].Time) || out[0].SQL != in[0].SQL || out[0].Duration != in[0].Duration || out[0].Source != "editor" {
			t.Fatalf("%s: %+v %v", f, out, err)
		}
	}
	if ExportHistory(&bytes.Buffer{}, in, "xml") == nil {
		t.Fatal("unknown format accepted")
	}
	if _, err := ImportHistory(strings.NewReader("hello")); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestConsole(t *testing.T) {
	var c Console
	for i := 0; i < 1005; i++ {
		c.Add("select 1", false, 0)
	}
	if n := len(c.Lines()); n != 1000 {
		t.Fatalf("ring holds %d", n)
	}
	c.Add(strings.Repeat("x", 100<<10), true, time.Second)
	ls := c.Lines()
	if last := ls[len(ls)-1]; last.SQL != "<100 KB statement>" || !last.Failed || last.At.IsZero() {
		t.Fatalf("%+v", last)
	}
	c.Clear()
	if len(c.Lines()) != 0 {
		t.Fatal("not cleared")
	}
}
