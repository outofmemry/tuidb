package mcp

import (
	"context"
	"fmt"

	"webtuidb/internal/db"
)

// SideEffect returns the first function or verb in stmt that changes state or reaches outside the database
// ("" = none), the check execute_query's read-only path uses. Plugin hosts use it too.
func SideEffect(dialect, stmt string) string { return sideEffectFunc(dialect, stmt) }

// ReadOnlyTx returns the session read-only statements must run on, and a func that releases it. A connection
// marked read_only is already enforced by the server for the dialects in serverReadOnly; otherwise the session must be able to pin one connection
// (db.Pinner) and the dialect must have a read-only transaction. Without both it refuses: the keyword filter
// alone cannot stop a SELECT that calls a side-effecting function.
func ReadOnlyTx(ctx context.Context, sess db.Session, dialect string, connReadOnly bool) (db.Session, func(), error) {
	if connReadOnly && serverReadOnly[dialect] {
		return sess, func() {}, nil
	}
	begin := readOnlyBegin[dialect]
	pn, ok := sess.(db.Pinner)
	// Vertica reports the postgres dialect but has no transactions, so BEGIN READ ONLY would fail there.
	if !ok || begin == "" || !sess.Caps().Transactions {
		return nil, nil, fmt.Errorf("read-only queries cannot be enforced read-only on this %s connection (no server-side read-only mode or transaction)", dialect)
	}
	p, err := pn.Pin(ctx)
	if err != nil {
		return nil, nil, err
	}
	if _, err := p.Exec(ctx, begin, 0); err != nil {
		p.Close()
		return nil, nil, err
	}
	return p, func() {
		p.Exec(context.Background(), "ROLLBACK", 0)
		p.Close()
	}, nil
}
