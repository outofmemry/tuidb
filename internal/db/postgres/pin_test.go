package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"webtuidb/internal/db"
)

func TestPinKeepsTransactionAndTempTable(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	pin := must(s.(db.Pinner).Pin(ctx))
	for _, q := range []string{"BEGIN", "CREATE TEMP TABLE webtuidb_pin (a int)", "INSERT INTO webtuidb_pin VALUES (1),(2),(3)"} {
		must(pin.Exec(ctx, q, 0))
	}
	res := must(pin.Exec(ctx, "SELECT a FROM webtuidb_pin ORDER BY a", 2)) // temp table is visible on the pinned connection only
	if len(res.Rows) != 2 || !res.Truncated {
		t.Errorf("limit: %+v", res)
	}
	must(pin.Exec(ctx, "ROLLBACK", 0))
	if _, err := pin.Exec(ctx, "SELECT a FROM webtuidb_pin", 0); err == nil {
		t.Error("ROLLBACK should have removed the temp table")
	}
	pin.Close()
	if _, err := pin.Exec(ctx, "SELECT 1", 0); err == nil {
		t.Error("Exec after Close should fail")
	}
}
