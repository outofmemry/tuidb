package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"webtuidb/internal/db"
)

var _ db.Pinner = (*session)(nil)

// pinned runs every Exec on one connection taken out of the pool; all other methods delegate to the parent.
type pinned struct {
	*session
	p  *pool
	mu sync.Mutex
	c  *conn // nil once closed
}

// Pin lends one connection for as long as the returned session is open (see db.Pinner).
func (s *session) Pin(ctx context.Context) (db.Session, error) {
	p := s.p()
	c, err := p.get(ctx)
	if err != nil {
		return nil, err
	}
	c.dirty, c.pinned = true, true // SET / temp tables must not leak into the pool: reset on return
	return &pinned{session: s, p: p, c: c}, nil
}

func (s *pinned) Exec(ctx context.Context, stmt string, limit int) (*db.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c == nil {
		return nil, errors.New("pinned session closed")
	}
	if s.session.c.ReadOnly && !db.IsReadOnly("postgres", stmt) {
		return nil, errReadOnly
	}
	// fetch never cancels on a pinned connection (it drains the rest), but draining a huge result is slow:
	// let the server stop at limit+1 rows instead when the statement is a plain SELECT.
	sql, trim := stmt, 0
	if w, ok := db.PageStatement("postgres", stmt, limit+1, 0); ok && limit > 0 {
		sql, trim = w, limit
	}
	start := time.Now()
	if trim > 0 {
		limit = 0
	}
	res, err := s.c.fetch(ctx, s.p, sql, nil, limit)
	if err != nil {
		return nil, err
	}
	if trim > 0 && len(res.Rows) > trim {
		res.Rows, res.Truncated = res.Rows[:trim], true
	}
	res.Duration = time.Since(start)
	return res, nil
}

func (s *pinned) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c != nil {
		s.c.pinned = false
		s.p.put(s.c) // closes the connection if a transaction is still open, which rolls it back
		s.c = nil
	}
	return nil
}

// Apply runs the statements on the pinned connection. When the engine has transactions, no statement needs to
// run outside one and the pin holds none of its own, the batch gets its own BEGIN/COMMIT, so a failure midway
// does not leave the earlier statements committed. Inside the user's transaction they just join it.
func (s *pinned) Apply(ctx context.Context, stmts []db.Statement) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c == nil {
		return errors.New("pinned session closed")
	}
	for _, st := range stmts {
		if s.session.c.ReadOnly && !st.ReadOnly && !db.IsReadOnly("postgres", st.Text) {
			return errReadOnly
		}
	}
	s.session.colMu.Lock()
	s.session.cols = map[string][]string{} // structure may change
	s.session.colMu.Unlock()
	own := wantTx(s.session.caps.Transactions, stmts) && s.c.PgConn().TxStatus() == 'I'
	return applyOn(ctx, s.c.Conn, own, stmts)
}

// applyOn runs stmts on ex, wrapped in BEGIN/COMMIT (ROLLBACK on error) when own is set.
func applyOn(ctx context.Context, ex execer, own bool, stmts []db.Statement) error {
	if own {
		if _, err := ex.Exec(ctx, "BEGIN"); err != nil {
			return fmt.Errorf("begin: %w", err)
		}
	}
	for _, st := range stmts {
		if _, err := ex.Exec(ctx, st.Text, st.Args...); err != nil {
			if own {
				rctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				ex.Exec(rctx, "ROLLBACK")
				cancel()
			}
			return fmt.Errorf("apply %q: %w", firstLine(st.Shown()), err)
		}
	}
	if own {
		if _, err := ex.Exec(ctx, "COMMIT"); err != nil {
			return fmt.Errorf("commit: %w", err)
		}
	}
	return nil
}
