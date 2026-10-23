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

type recExec struct {
	ran    []string
	failOn string
}

func (r *recExec) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	r.ran = append(r.ran, sql)
	if sql == r.failOn {
		return pgconn.CommandTag{}, errors.New("boom")
	}
	return pgconn.CommandTag{}, nil
}

func TestPinnedApplyOwnsTransaction(t *testing.T) {
	stmts := []db.Statement{{Text: "UPDATE a"}, {Text: "UPDATE b"}, {Text: "UPDATE c"}}
	r := &recExec{}
	if err := applyOn(context.Background(), r, true, stmts); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.ran, "|"); got != "BEGIN|UPDATE a|UPDATE b|UPDATE c|COMMIT" {
		t.Errorf("order: %s", got)
	}
	r = &recExec{failOn: "UPDATE b"}
	if err := applyOn(context.Background(), r, true, stmts); err == nil {
		t.Fatal("no error")
	}
	if got := strings.Join(r.ran, "|"); got != "BEGIN|UPDATE a|UPDATE b|ROLLBACK" {
		t.Errorf("failure order: %s", got)
	}
	r = &recExec{} // the pin already holds a transaction: join it
	applyOn(context.Background(), r, false, stmts)
	if got := strings.Join(r.ran, "|"); got != "UPDATE a|UPDATE b|UPDATE c" {
		t.Errorf("joined: %s", got)
	}
}
