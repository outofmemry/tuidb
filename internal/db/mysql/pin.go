package mysql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"webtuidb/internal/db"
)

var _ db.Pinner = (*session)(nil)

// Pin returns a session on a pool of exactly one connection, so BEGIN, SET and temp tables carry over
// between Exec calls (see db.Pinner). Close closes that connection, which rolls back an open transaction.
func (s *session) Pin(ctx context.Context) (db.Session, error) {
	s.mu.RLock()
	name := s.dbname
	p := &session{conn: s.conn, dial: s.dial, caps: s.caps, dbname: name, info: s.info, maria: s.maria,
		cols: map[string][]string{}, versioned: map[string]bool{}, pinned: true}
	s.mu.RUnlock()
	pool, err := p.openPool(ctx, name) // on p, so the pinned flag installs the single-connection guard
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	pool.SetConnMaxIdleTime(0) // never recycle: the connection holds the session state
	p.pool = pool
	return pinned{p}, nil
}

type pinned struct{ *session }

// UseDatabase runs USE on the one connection; reopening the pool would drop the pinned state.
func (s pinned) UseDatabase(ctx context.Context, name string) error {
	if _, err := s.query(ctx, "USE `"+strings.ReplaceAll(name, "`", "``")+"`", nil, 0); err != nil {
		return err
	}
	s.mu.Lock()
	s.dbname, s.cols, s.versioned = name, map[string][]string{}, map[string]bool{}
	s.mu.Unlock()
	return nil
}

// Exec answers USE on the pinned connection; session.Exec would call the unpinned UseDatabase and swap the pool.
func (s pinned) Exec(ctx context.Context, stmt string, limit int) (*db.Result, error) {
	if m := useRe.FindStringSubmatch(stmt); m != nil {
		start := time.Now()
		if err := s.UseDatabase(ctx, m[1]+m[2]); err != nil {
			return nil, err
		}
		return &db.Result{Message: "Database changed", Duration: time.Since(start)}, nil
	}
	return s.session.Exec(ctx, stmt, limit)
}

// Apply runs the statements on the pinned connection as they are.
// shortcut: no BEGIN of its own (it would implicitly commit the user's open transaction), so a failure
// midway leaves the earlier statements to the user's transaction, or applied under autocommit.
func (s pinned) Apply(ctx context.Context, stmts []db.Statement) error {
	for _, st := range stmts {
		if isDDL(st.Text) {
			return errors.New("structure changes are DDL, which MySQL commits implicitly together with your open transaction: release the pin first")
		}
	}
	for i, st := range stmts {
		if s.conn.ReadOnly && !st.ReadOnly && !db.IsReadOnly("mysql", st.Text) {
			return errReadOnly
		}
		args := make([]any, len(st.Args))
		for j, a := range st.Args {
			args[j] = convertArg(a)
		}
		if _, err := s.db().ExecContext(ctx, st.Text, args...); err != nil {
			return fmt.Errorf("statement %d (%s): %w", i+1, st.Shown(), err)
		}
	}
	s.mu.Lock()
	s.cols = map[string][]string{}
	s.mu.Unlock()
	return nil
}
