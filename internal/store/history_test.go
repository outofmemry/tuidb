package store

import (
	"strings"
	"testing"
	"time"
)

func TestAppendAllDeterministicAndIdempotent(t *testing.T) {
	h := &History{Dir: t.TempDir()}
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	es := []HistoryEntry{{Time: ts, SQL: "select 1"}, {Time: ts, SQL: "select 2"}, {Time: ts, SQL: "select 1"}}
	if n, err := h.AppendAll("c", es); err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if n, err := h.AppendAll("c", es); err != nil || n != 0 { // retry: nothing new
		t.Fatalf("retry n=%d err=%v", n, err)
	}
	got, _ := h.List("c", "", 0)
	if len(got) != 2 {
		t.Fatalf("want 2 unique entries, got %d", len(got))
	}
}

// Exporting the history and importing it back into the same connection adds nothing, whichever path wrote it.
func TestAppendAllMatchesAppendIDs(t *testing.T) {
	h := &History{Dir: t.TempDir()}
	base := time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.UTC)
	for i := 0; i < 3; i++ {
		if err := h.Append("c", HistoryEntry{Time: base.Add(time.Duration(i) * time.Second), SQL: "select " + string(rune('1'+i)), Source: "editor"}); err != nil {
			t.Fatal(err)
		}
	}
	exported, _ := h.List("c", "", 0) // newest first, as ExportHistory writes it
	if n, err := h.AppendAll("c", exported); err != nil || n != 0 {
		t.Fatalf("re-import added %d (%v)", n, err)
	}
	// an older file whose IDs are not ours still dedupes on content
	old := make([]HistoryEntry, len(exported))
	for i, e := range exported {
		e.ID = "legacy-" + e.ID
		old[i] = e
	}
	if n, _ := h.AppendAll("c", old); n != 0 {
		t.Errorf("content dedupe failed, added %d", n)
	}
	// a non-empty incoming ID is kept
	if n, _ := h.AppendAll("c", []HistoryEntry{{ID: "mine", Time: base.Add(time.Hour), SQL: "select 9"}}); n != 1 {
		t.Fatal("new entry not written")
	}
	got, _ := h.List("c", "select 9", 0)
	if len(got) != 1 || got[0].ID != "mine" {
		t.Errorf("incoming ID lost: %+v", got)
	}
}

func TestAppendAllStoresOldestFirstSoTrimKeepsNewest(t *testing.T) {
	h := &History{Dir: t.TempDir()}
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var es []HistoryEntry // newest first, like an export
	for i := maxEntries + 500; i > 0; i-- {
		es = append(es, HistoryEntry{Time: base.Add(time.Duration(i) * time.Minute), SQL: "select " + strings.Repeat("x", 800) + string(rune(i))})
	}
	if n, err := h.AppendAll("c", es); err != nil || n != len(es) {
		t.Fatalf("n=%d err=%v", n, err)
	}
	got, _ := h.List("c", "", 1)
	if len(got) != 1 || !got[0].Time.Equal(base.Add(time.Duration(maxEntries+500)*time.Minute)) {
		t.Errorf("newest entry is not first in List: %+v", got)
	}
	// an import older than what is stored is interleaved by time, not appended after the newest
	h2 := &History{Dir: t.TempDir()}
	h2.Append("c", HistoryEntry{Time: base.Add(48 * time.Hour), SQL: "newer"})
	h2.AppendAll("c", []HistoryEntry{{Time: base, SQL: "older"}})
	all, _ := h2.List("c", "", 0)
	if len(all) != 2 || all[0].SQL != "newer" || all[1].SQL != "older" {
		t.Errorf("order: %+v", all)
	}
}

func TestEntryIDCoversAllFields(t *testing.T) {
	h := &History{Dir: t.TempDir()}
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	long := strings.Repeat("a", maxSQLBytes+10)
	es := []HistoryEntry{
		{Time: ts, SQL: "update t set a=1", Original: "1"},
		{Time: ts, SQL: "update t set a=1", Original: "2"},
		{Time: ts, SQL: "update t set a=1", Original: "2", Database: "other"},
		{Time: ts, SQL: "update t set a=1", Original: "2", Database: "other", Duration: time.Second},
		{Time: ts, SQL: long + "x"},
		{Time: ts, SQL: long + "y"},
	}
	if n, err := h.AppendAll("c", es); err != nil || n != len(es) {
		t.Fatalf("n=%d err=%v, entries collapsed", n, err)
	}
}
