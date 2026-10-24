package db

import (
	"context"
	"fmt"
)

// Pinner is implemented by sessions that can lend one dedicated connection. Every Exec on the returned
// Session runs on that connection, so BEGIN, SET and temp tables carry over and ROLLBACK works; Close
// returns the connection (and ends an open transaction) instead of closing the parent session.
type Pinner interface {
	Pin(ctx context.Context) (Session, error)
}

// PageStatement wraps a plain read-only SELECT/WITH so one page can be fetched without re-running a
// doubled LIMIT: SELECT * FROM (<stmt>) AS webtuidb_page LIMIT n OFFSET m. It reports false for anything
// else (several statements, a LIMIT/OFFSET/FETCH/INTO/FOR or top-level ORDER BY present, other verbs, unknown dialect);
// the caller then falls back to the doubled limit.
// shortcut: the subquery re-runs the statement per page (no server cursor), upgrade with Pin + a cursor.
func PageStatement(dialect, stmt string, limit, offset int) (string, bool) {
	d, ok := DialectFor(dialect)
	if !ok || limit <= 0 || offset < 0 || !IsReadOnly(dialect, stmt) {
		return "", false
	}
	ts := scan(dialect, stmt)
	up := upper(ts)
	if len(up) == 0 || up[0] != "SELECT" && up[0] != "WITH" || hasAny(up, "LIMIT", "OFFSET", "FETCH", "INTO", "FOR", "LOCK") {
		return "", false
	}
	// A top-level ORDER BY inside the derived table is not binding for the outer query (it may be dropped or
	// merged away), so pages could overlap or skip rows. shortcut: such statements are not paged, hoist it later.
	for depth, i := 0, 0; i < len(up); i++ {
		switch {
		case up[i] == "(":
			depth++
		case up[i] == ")":
			depth--
		case depth == 0 && up[i] == "ORDER":
			return "", false
		}
	}
	parts := Split(dialect, stmt)
	if len(parts) != 1 {
		return "", false
	}
	body := parts[0].Text
	return fmt.Sprintf("SELECT * FROM (%s\n) AS %s LIMIT %d OFFSET %d", body, d.Quote("webtuidb_page"), limit, offset), true
}
