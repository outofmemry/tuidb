package dynamodb

import (
	"context"
	"sync"
	"testing"

	"webtuidb/internal/db"
)

// The UI loads rows, columns and counts of one table at the same time, and keeps pinging.
func TestConcurrentUse(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := ref("webtuidb_events")
			switch i % 4 {
			case 0:
				if _, err := s.Rows(ctx, db.RowQuery{ObjRef: r, Filters: flt(db.AnyColumn, "Contains", "log")}); err != nil {
					t.Error(err)
				}
			case 1:
				if _, err := s.Columns(ctx, r); err != nil {
					t.Error(err)
				}
			case 2:
				if _, err := s.Count(ctx, db.RowQuery{ObjRef: r}, false); err != nil {
					t.Error(err)
				}
				if _, err := s.Plan([]db.Change{{Kind: db.Delete, ObjRef: r, Key: []db.KeyVal{{Col: "event_id", Val: "x"}}}}); err != nil {
					t.Error(err)
				}
			default:
				if err := s.Ping(ctx); err != nil {
					t.Error(err)
				}
			}
		}(i)
	}
	wg.Wait()
}
