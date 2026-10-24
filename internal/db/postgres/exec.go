package postgres

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"tuidb/internal/db"
)

func (s *session) Exec(ctx context.Context, stmt string, limit int) (*db.Result, error) {
	res, err := s.run(ctx, stmt, nil, limit, true)
	if err == nil { // the statement may have changed any table's columns
		s.colMu.Lock()
		s.cols = map[string][]string{}
		s.colMu.Unlock()
	}
	return res, err
}

func (s *session) run(ctx context.Context, stmt string, args []any, limit int, dirty bool) (*db.Result, error) {
	start := time.Now()
	var res *db.Result
	err := s.use(ctx, func(c *conn) (err error) {
		c.dirty = c.dirty || dirty
		res, err = c.fetch(ctx, s.p(), stmt, args, limit)
		return err
	})
	if err != nil {
		return nil, err
	}
	res.Duration = time.Since(start)
	return res, nil
}

// fetch runs one statement and normalizes its rows (text format, decoded by result OID).
func (c *conn) fetch(ctx context.Context, p *pool, stmt string, args []any, limit int) (*db.Result, error) {
	c.takeNotices()
	qctx, stop := context.WithCancel(ctx)
	defer stop()
	rows, err := c.Query(qctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	fds := rows.FieldDescriptions()
	res := &db.Result{HasRows: len(fds) > 0}
	oids := make([]uint32, len(fds))
	for i, fd := range fds {
		oids[i] = fd.DataTypeOID
		res.Columns = append(res.Columns, db.ResultColumn{Name: fd.Name})
	}
	tm := c.TypeMap()
	for rows.Next() {
		if limit > 0 && len(res.Rows) == limit {
			res.Truncated = true
			stop() // abandon the rest instead of draining it
			break
		}
		raw := rows.RawValues()
		row := make([]any, len(raw))
		for i, b := range raw {
			row[i] = normalize(tm, oids[i], b)
		}
		res.Rows = append(res.Rows, row)
	}
	rows.Close()
	if res.Truncated {
		c.bad = true // cancelled mid-result: do not reuse
	} else if err := rows.Err(); err != nil {
		return nil, err
	}
	tag := rows.CommandTag()
	res.Affected = tag.RowsAffected()
	if !res.HasRows {
		res.Message = tagMessage(tag)
	}
	if n := c.takeNotices(); n != "" {
		res.Message = strings.TrimSpace(res.Message + "\n" + n)
	}
	if !c.bad {
		for i, oid := range oids {
			res.Columns[i].Type = c.typeName(ctx, p, oid)
		}
	} else {
		for i, oid := range oids {
			res.Columns[i].Type = knownTypeName(tm, oid)
		}
	}
	return res, nil
}

func tagMessage(tag pgconn.CommandTag) string {
	switch {
	case tag.Insert(), tag.Update(), tag.Delete(), tag.Select():
		n := tag.RowsAffected()
		if n == 1 {
			return "1 row affected"
		}
		return fmt.Sprintf("%d rows affected", n)
	}
	return tag.String()
}

func knownTypeName(tm *pgtype.Map, oid uint32) string {
	if t, ok := tm.TypeForOID(oid); ok {
		return t.Name
	}
	return "oid:" + strconv.FormatUint(uint64(oid), 10)
}

// typeName resolves custom types (enums, domains, extensions) through pg_type once per pool.
func (c *conn) typeName(ctx context.Context, p *pool, oid uint32) string {
	if t, ok := c.TypeMap().TypeForOID(oid); ok {
		return t.Name
	}
	if v, ok := p.types.Load(oid); ok {
		return v.(string)
	}
	var name string
	if err := c.QueryRow(ctx, "SELECT format_type($1::oid, NULL)", strconv.FormatUint(uint64(oid), 10)).Scan(&name); err != nil {
		return knownTypeName(c.TypeMap(), oid)
	}
	p.types.Store(oid, name)
	return name
}

// normalize turns a text-format value into the shared Result value types.
func normalize(tm *pgtype.Map, oid uint32, b []byte) any {
	if b == nil {
		return nil
	}
	s := string(b)
	switch oid {
	case pgtype.BoolOID:
		return s == "t"
	case pgtype.Int2OID, pgtype.Int4OID, pgtype.Int8OID:
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	case pgtype.OIDOID, pgtype.XIDOID, pgtype.CIDOID:
		if n, err := strconv.ParseUint(s, 10, 64); err == nil {
			return n
		}
	case pgtype.Float4OID, pgtype.Float8OID:
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	case pgtype.ByteaOID:
		if h, ok := strings.CutPrefix(s, `\x`); ok {
			if out, err := hex.DecodeString(h); err == nil {
				return out
			}
		}
	// timestamptz stays the server's text: it carries the offset, so the shown value, a filter on it
	// and a key written back all denote the same instant whatever the client and session zones are.
	case pgtype.DateOID, pgtype.TimestampOID:
		var t time.Time
		if err := tm.Scan(oid, pgtype.TextFormatCode, b, &t); err == nil { // infinity falls through as text
			return t
		}
	}
	return s
}

// strings runs a query returning one text column.
func (s *session) strings(ctx context.Context, sql string, args ...any) ([]string, error) {
	var out []string
	err := s.query(ctx, sql, args, func(r pgx.Rows) error {
		var v string
		if err := r.Scan(&v); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	return out, err
}

// query runs a catalog query and calls scan for each row.
func (s *session) query(ctx context.Context, sql string, args []any, scan func(pgx.Rows) error) error {
	return s.use(ctx, func(c *conn) error {
		rows, err := c.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	})
}

// ref fills in the default schema.
func (s *session) ref(r db.ObjRef) db.ObjRef {
	if r.Schema == "" {
		r.Schema = s.CurrentSchema()
	}
	return r
}

// filterCols returns the column names Any-column filters expand to (cached per table).
func (s *session) filterCols(ctx context.Context, q db.RowQuery) ([]string, error) {
	hasAny := false
	for _, f := range q.Filters.Active() {
		hasAny = hasAny || f.Column == db.AnyColumn
	}
	if !hasAny {
		return nil, nil
	}
	key := q.Schema + "." + q.Name
	s.colMu.Lock()
	cols, ok := s.cols[key]
	s.colMu.Unlock()
	if ok {
		return cols, nil
	}
	cs, err := s.columns(ctx, q.ObjRef)
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		if searchable(c.Type) {
			cols = append(cols, c.Name)
		}
	}
	s.colMu.Lock()
	s.cols[key] = cols
	s.colMu.Unlock()
	return cols, nil
}

// searchable: the "Any column" filter casts every listed column to text for every row; binary and
// text-search columns are huge and meaningless as text, so they are left out of that search.
func searchable(typ string) bool {
	switch strings.ToLower(typ) {
	case "bytea", "tsvector", "tsquery", "oid":
		return false
	}
	return true
}

func (s *session) Rows(ctx context.Context, q db.RowQuery) (*db.Result, error) {
	q.ObjRef = s.ref(q.ObjRef)
	cols, err := s.filterCols(ctx, q)
	if err != nil {
		return nil, err
	}
	sql, args, err := db.BuildSelect(db.PG, q, cols)
	if err != nil {
		return nil, err
	}
	return s.run(ctx, sql, args, 0, false)
}

func (s *session) Count(ctx context.Context, q db.RowQuery, estimate bool) (db.Count, error) {
	q.ObjRef = s.ref(q.ObjRef)
	if estimate && len(q.Filters.Active()) == 0 {
		n := int64(-1)
		err := s.query(ctx, `SELECT CASE WHEN c.relkind IN ('r','m','p') THEN c.reltuples::bigint ELSE -1 END
			FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND c.relname = $2`,
			[]any{q.Schema, q.Name}, func(r pgx.Rows) error { return r.Scan(&n) })
		if err != nil {
			return db.Count{}, fmt.Errorf("estimate rows: %w", err)
		}
		if n > 0 { // -1 is never analyzed, 0 can be stale stats of a filled table: count exactly
			return db.Count{N: n}, nil
		}
	}
	cols, err := s.filterCols(ctx, q)
	if err != nil {
		return db.Count{}, err
	}
	sql, args, err := db.BuildCount(db.PG, q, cols)
	if err != nil {
		return db.Count{}, err
	}
	var n int64
	err = s.use(ctx, func(c *conn) error { return c.QueryRow(ctx, sql, args...).Scan(&n) })
	if err != nil {
		return db.Count{}, fmt.Errorf("count rows: %w", err)
	}
	return db.Count{N: n, Exact: true}, nil
}

func (s *session) Plan(changes []db.Change) ([]db.Statement, error) {
	cs := make([]db.Change, len(changes))
	for i, c := range changes {
		c.ObjRef = s.ref(c.ObjRef)
		cs[i] = c
	}
	return db.PlanSQL(db.PG, cs)
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// noTx statements cannot run inside a transaction block.
func noTx(text string) bool {
	t := strings.ToUpper(strings.Join(strings.Fields(text), " "))
	return strings.HasPrefix(t, "CREATE DATABASE") || strings.HasPrefix(t, "DROP DATABASE") ||
		strings.Contains(t, " CONCURRENTLY") || strings.HasPrefix(t, "VACUUM")
}

func (s *session) Apply(ctx context.Context, stmts []db.Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	s.colMu.Lock()
	s.cols = map[string][]string{} // structure may change
	s.colMu.Unlock()
	useTx := true
	for _, st := range stmts {
		useTx = useTx && !noTx(st.Text)
	}
	return s.use(ctx, func(c *conn) error {
		var ex execer = c.Conn
		var tx pgx.Tx
		if useTx {
			var err error
			if tx, err = c.Begin(ctx); err != nil {
				return fmt.Errorf("begin: %w", err)
			}
			ex = tx
		}
		for _, st := range stmts {
			if _, err := ex.Exec(ctx, st.Text, st.Args...); err != nil {
				if tx != nil {
					rctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					tx.Rollback(rctx)
					cancel()
				}
				return fmt.Errorf("apply %q: %w", firstLine(st.Shown()), err)
			}
		}
		if tx != nil {
			if err := tx.Commit(ctx); err != nil {
				return fmt.Errorf("commit: %w", err)
			}
		}
		return nil
	})
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

func (s *session) Processes(ctx context.Context) ([]db.ProcessInfo, error) {
	var out []db.ProcessInfo
	err := s.query(ctx, `SELECT pid::text, coalesce(usename, '')::text, coalesce(datname, '')::text, coalesce(state, '')::text,
		coalesce(extract(epoch FROM now() - query_start), 0)::float8, coalesce(query, '')
		FROM pg_stat_activity WHERE state IS NOT NULL AND state <> 'idle' AND pid <> pg_backend_pid() ORDER BY query_start`, nil,
		func(r pgx.Rows) error {
			var p db.ProcessInfo
			var secs float64
			if err := r.Scan(&p.ID, &p.User, &p.DB, &p.State, &secs, &p.Query); err != nil {
				return err
			}
			p.Elapsed = time.Duration(secs * float64(time.Second))
			out = append(out, p)
			return nil
		})
	return out, err
}

func (s *session) Kill(ctx context.Context, id string) error {
	pid, err := strconv.Atoi(id)
	if err != nil {
		return fmt.Errorf("bad process id %q", id)
	}
	var ok bool
	if err := s.use(ctx, func(c *conn) error {
		return c.QueryRow(ctx, "SELECT pg_cancel_backend($1)", pid).Scan(&ok)
	}); err != nil {
		return fmt.Errorf("cancel backend: %w", err)
	}
	if !ok {
		return errors.New("cancel backend: no such process")
	}
	return nil
}

var _ db.ProcessLister = (*session)(nil)
