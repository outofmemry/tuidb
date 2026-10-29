package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestWithPosition(t *testing.T) {
	stmt := "select 1;\nselect *\n  from nope"
	pe := &pgconn.PgError{Severity: "ERROR", Message: `relation "nope" does not exist`, Code: "42P01", Position: 27}
	err := withPosition(pe, stmt, stmt)
	if got := err.Error(); got != `ERROR: relation "nope" does not exist (SQLSTATE 42P01) at line 3, column 8` {
		t.Fatalf("%s", got)
	}
	if !errors.As(err, new(*pgconn.PgError)) {
		t.Error("the PgError must stay reachable")
	}
	wrapped := "SELECT * FROM (" + stmt + ") AS webtuidb_page LIMIT 3"
	pe.Position = 27 + 15
	if got := withPosition(pe, wrapped, stmt).Error(); got != `ERROR: relation "nope" does not exist (SQLSTATE 42P01) at line 3, column 8` {
		t.Fatalf("paged: %s", got)
	}
	plain := errors.New("x")
	if withPosition(plain, stmt, stmt) != plain {
		t.Error("errors without a position pass through")
	}
}
