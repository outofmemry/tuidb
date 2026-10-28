package store

import (
	"testing"
	"time"
)

func TestAppendAllDeterministicAndIdempotent(t *testing.T) {
	h := &History{Dir: t.TempDir()}
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	es := []HistoryEntry{{Time: ts, SQL: "select 1"}, {Time: ts, SQL: "select 2"}, {Time: ts, SQL: "select 1"}}
	if err := h.AppendAll("c", es); err != nil {
		t.Fatal(err)
	}
	if err := h.AppendAll("c", es); err != nil { // retry: nothing new
		t.Fatal(err)
	}
	got, _ := h.List("c", "", 0)
	if len(got) != 2 {
		t.Fatalf("want 2 unique entries, got %d", len(got))
	}
}
