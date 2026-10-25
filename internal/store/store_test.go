package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/db"
)

func TestHistory(t *testing.T) {
	h := &History{Dir: t.TempDir()}
	for i, q := range []string{"select 1", "UPDATE t SET a=1", "select 2"} {
		if err := h.Append("Acme Prod/1", HistoryEntry{SQL: q, Source: "editor", Time: time.Unix(int64(1000+i), 0)}); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := h.List("Acme Prod/1", "", 0)
	if len(got) != 3 || got[0].SQL != "select 2" || got[2].SQL != "select 1" {
		t.Fatalf("order: %+v", got)
	}
	if got, _ = h.List("Acme Prod/1", "UPDATE", 0); len(got) != 1 {
		t.Errorf("search: %+v", got)
	}
	if got, _ = h.List("Acme Prod/1", "", 2); len(got) != 2 {
		t.Errorf("limit: %+v", got)
	}
	all, _ := h.List("Acme Prod/1", "", 0)
	if err := h.Delete("Acme Prod/1", all[1].ID); err != nil {
		t.Fatal(err)
	}
	if got, _ = h.List("Acme Prod/1", "", 0); len(got) != 2 || got[1].SQL != "select 1" {
		t.Errorf("after delete: %+v", got)
	}
	if st, _ := os.Stat(h.path("Acme Prod/1")); st.Mode().Perm() != 0o600 {
		t.Errorf("perms %v", st.Mode().Perm())
	}
	h.Clear("Acme Prod/1")
	if got, _ = h.List("Acme Prod/1", "", 0); len(got) != 0 {
		t.Error("clear")
	}
	if err := h.Clear("never-existed"); err != nil {
		t.Errorf("clearing nothing must not fail: %v", err)
	}
}

func TestHistoryTornLine(t *testing.T) {
	h := &History{Dir: t.TempDir()}
	h.Append("c", HistoryEntry{SQL: "select 1"})
	f, _ := os.OpenFile(h.path("c"), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"id":"x","sql":"tor`)
	f.Close()
	if got, err := h.List("c", "", 0); err != nil || len(got) != 1 {
		t.Errorf("%+v %v", got, err)
	}
}

func TestQueries(t *testing.T) {
	s := &Queries{Root: filepath.Join(t.TempDir(), "q")}
	q, err := s.Save("reports", "Recent orders", []string{"ro", "recent"}, "SELECT * FROM orders;\n")
	if err != nil || q.Path != "reports/Recent_orders.sql" || q.Name != "Recent orders" || len(q.Keywords) != 2 || q.SQL != "SELECT * FROM orders;\n" {
		t.Fatalf("%+v %v", q, err)
	}
	if _, err := s.Save("", "plain", nil, "select 1"); err != nil {
		t.Fatal(err)
	}
	list, _ := s.List()
	if len(list) != 2 || list[0].Name != "plain" || list[1].Folder != "reports" {
		t.Fatalf("%+v", list)
	}
	if Keywords(list)["ro"].Name != "Recent orders" {
		t.Error("keyword lookup")
	}
	if err := s.Update(q.Path, "SELECT 2"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Read(q.Path); got.SQL != "SELECT 2" || got.Name != "Recent orders" || len(got.Keywords) != 2 {
		t.Errorf("update lost header: %+v", got)
	}
	if err := s.Move(q.Path, "archive"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Read("archive/Recent_orders.sql"); err != nil || got.Folder != "archive" {
		t.Errorf("%+v %v", got, err)
	}
	if err := s.Delete("archive/Recent_orders.sql"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../x.sql", "/etc/passwd", ".."} {
		if _, err := s.Read(bad); err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Errorf("path %q must be rejected, got %v", bad, err)
		}
	}
	if _, err := s.Save("../escape", "x", nil, ""); err == nil {
		t.Error("folder escape must be rejected")
	}
	if _, err := s.Save("", "  ", nil, ""); err == nil {
		t.Error("empty name")
	}
	if got, _ := (&Queries{Root: filepath.Join(t.TempDir(), "none")}).List(); len(got) != 0 {
		t.Error("missing root lists empty")
	}
}

func TestState(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "state.json")
	s, err := LoadState(p)
	if err != nil {
		t.Fatal(err)
	}
	k := TableKey("c", "d", "t")
	ts := TableState{Hidden: []string{"x"}, Sort: []db.Sort{{Column: "id", Desc: true}},
		Filters: db.FilterSet{Filters: []db.Filter{{Column: "a", Op: "=", Value: "1", Enabled: true}}}}
	if err := s.SetTable(k, ts); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.TogglePin("c", "public.orders"); !on {
		t.Error("pin")
	}
	if on, _ := s.TogglePin("c", "public.orders"); on {
		t.Error("unpin")
	}
	for _, it := range []string{"a", "b", "a", "c"} {
		s.Touch("c", it)
	}
	s.SetLastSession([]Workspace{{"c", "d"}})
	s.SetRowLimit("c", 500)
	again, err := LoadState(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Table(k); len(got.Hidden) != 1 || got.Sort[0].Column != "id" || !got.Filters.Filters[0].Enabled {
		t.Errorf("%+v", got)
	}
	if r := again.Recent("c"); strings.Join(r, ",") != "c,a,b" {
		t.Errorf("recent %v", r)
	}
	if again.RowLimit("c", 100) != 500 || again.RowLimit("other", 100) != 100 || len(again.Pinned("c")) != 0 || len(again.LastSession()) != 1 {
		t.Error("misc")
	}
	// the copy handed out must not alias the saved state
	cp := again.Table(k)
	cp.Filters.Filters[0].Value = "mutated"
	cp.Hidden[0] = "mutated"
	if again.Table(k).Filters.Filters[0].Value == "mutated" || again.Table(k).Hidden[0] == "mutated" {
		t.Error("Table() must return a copy")
	}
}

func TestCorruptStateIsMovedAsideNotFatal(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(p, []byte("{broken"), 0o600)
	s, err := LoadState(p)
	if err != nil || s.Recovered == "" {
		t.Fatalf("corrupt state must start fresh with a notice: %v %q", err, s.Recovered)
	}
	if _, err := os.Stat(p + ".corrupt"); err != nil {
		t.Error("the bad file is kept for inspection")
	}
	if err := s.SetRowLimit("c", 5); err != nil {
		t.Fatal(err)
	}
	if again, _ := LoadState(p); again.RowLimit("c", 0) != 5 {
		t.Error("the fresh state is usable")
	}
}

func TestHistoryIsBounded(t *testing.T) {
	h := &History{Dir: t.TempDir()}
	big := strings.Repeat("x", 300) // ~ 400 bytes a line: 5000 lines is ~2 MB, well over the 1 MB floor
	for i := 0; i < maxEntries*2+10; i++ {
		if err := h.Append("c", HistoryEntry{SQL: fmt.Sprintf("%d %s", i, big)}); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := h.List("c", "", 0)
	if len(all) > maxEntries*2 || len(all) < maxEntries/2 { // trimmed back to the cap whenever it reaches twice the cap
		t.Fatalf("history must stay between %d and %d entries, got %d", maxEntries/2, maxEntries*2, len(all))
	}
	if !strings.HasPrefix(all[0].SQL, fmt.Sprint(maxEntries*2+9)+" ") {
		t.Errorf("the newest entry survives: %q", all[0].SQL[:10])
	}
}

func TestStateMergesWritesFromAnotherProcess(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	a, _ := LoadState(p)
	b, _ := LoadState(p)
	if _, err := a.TogglePin("c", "public.users"); err != nil {
		t.Fatal(err)
	}
	if err := b.Touch("c", "public.orders"); err != nil { // b's snapshot predates a's pin
		t.Fatal(err)
	}
	r, _ := LoadState(p)
	if len(r.Pinned("c")) != 1 || len(r.Recent("c")) != 1 {
		t.Errorf("pin %v recent %v: a write from one process must not drop the other's", r.Pinned("c"), r.Recent("c"))
	}
}

func TestHistoryAppendAfterTornLineKeepsNewEntry(t *testing.T) {
	h := &History{Dir: t.TempDir()}
	h.Append("c", HistoryEntry{SQL: "select 1"})
	f, _ := os.OpenFile(h.path("c"), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"id":"x","sql":"tor`)
	f.Close()
	h.Append("c", HistoryEntry{SQL: "select AFTER"})
	if got, _ := h.List("c", "AFTER", 0); len(got) != 1 {
		t.Errorf("the entry appended after a torn line was lost: %+v", got)
	}
}

func TestQueriesSaveRefusesNameCollision(t *testing.T) {
	s := &Queries{Root: t.TempDir()}
	if _, err := s.Save("", "a b", nil, "select 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("", "a?b", nil, "select 2"); err == nil {
		t.Error("a different query with the same file name must not overwrite")
	}
	if _, err := s.Save("", "a b", nil, "select 3"); err != nil {
		t.Errorf("re-saving the same query overwrites: %v", err)
	}
}
