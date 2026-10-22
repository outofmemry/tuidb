// Package postgres is the PostgreSQL driver (pgx). It keeps its own tiny connection pool because
// pgxpool needs github.com/jackc/puddle, which is not in go.mod.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"

	"tuidb/internal/config"
	"tuidb/internal/db"
)

func init() { db.Register(driver{}) }

const maxConns = 8

type driver struct{}

func (driver) Name() string { return "postgres" }

func (driver) Open(ctx context.Context, c config.Connection, dial db.DialFunc) (db.Session, error) {
	s := &session{c: c, dial: dial, cols: map[string][]string{}}
	p, err := s.connect(ctx, c.Database)
	if err != nil {
		return nil, err
	}
	s.pool = p
	return s, nil
}

// sslMode maps tuidb's ssl_mode names to libpq's. "" keeps whatever Options say (default prefer).
func sslMode(m string) (string, error) {
	switch m {
	case "":
		return "", nil
	case "prefer", "disable", "require", "verify-ca", "verify-full":
		return m, nil
	}
	return "", fmt.Errorf("ssl_mode %q is not supported by PostgreSQL (use disable, prefer, require, verify-ca or verify-full)", m)
}

// connString renders c as a postgres:// URL so pgx's own parser handles TLS and escaping.
func connString(c config.Connection, database string) (string, error) {
	q := url.Values{}
	for k, v := range c.Options {
		q.Set(k, v)
	}
	mode, err := sslMode(c.SSLMode)
	if err != nil {
		return "", err
	}
	if mode != "" {
		q.Set("sslmode", mode)
	}
	if c.Host != "" {
		q.Set("host", c.Host)
	}
	if c.Port != 0 {
		q.Set("port", strconv.Itoa(c.Port))
	}
	for k, v := range map[string]string{"sslrootcert": c.SSLCA, "sslcert": c.SSLCert, "sslkey": c.SSLKey} {
		if v != "" {
			q.Set(k, v)
		}
	}
	u := url.URL{Scheme: "postgres", Path: "/" + database, RawQuery: q.Encode()}
	if c.User != "" {
		u.User = url.UserPassword(c.User, c.Password)
	}
	return u.String(), nil
}

type session struct {
	c    config.Connection
	dial db.DialFunc

	mu      sync.RWMutex
	pool    *pool
	dbName  string
	schema  string
	version string

	colMu sync.Mutex
	cols  map[string][]string // "schema.table" -> column names, for Any-column filters
}

// connect builds a pool for database and proves it works.
func (s *session) connect(ctx context.Context, database string) (*pool, error) {
	cs, err := connString(s.c, database)
	if err != nil {
		return nil, err
	}
	cfg, err := pgx.ParseConfig(cs)
	if err != nil {
		return nil, fmt.Errorf("postgres config: %w", err)
	}
	// Unnamed statements, text params of unknown type: "7" against an int column and
	// "2024-01-02" against a date work, and DDL never trips over a cached plan.
	cfg.DefaultQueryExecMode = pgx.QueryExecModeExec
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 15 * time.Second
	}
	if s.dial != nil {
		cfg.DialFunc = pgconn.DialFunc(s.dial)
		cfg.LookupFunc = func(_ context.Context, host string) ([]string, error) { return []string{host}, nil } // resolve remotely
	}
	// Cancel asks the server to stop the query and keeps the connection; the deadline is the fallback.
	cfg.BuildContextWatcherHandler = func(pc *pgconn.PgConn) ctxwatch.Handler {
		return &pgconn.CancelRequestContextWatcherHandler{Conn: pc, DeadlineDelay: time.Second}
	}
	p := &pool{cfg: cfg, bootstrap: s.c.Bootstrap, sem: make(chan struct{}, maxConns)}
	var name, schema, version string
	err = p.use(ctx, func(c *conn) error {
		version, _, _ = strings.Cut(c.PgConn().ParameterStatus("server_version"), " ")
		return c.QueryRow(ctx, "SELECT current_database()::text, coalesce(current_schema()::text, 'public')").Scan(&name, &schema)
	})
	if err != nil {
		p.close()
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	s.mu.Lock()
	s.dbName, s.schema, s.version = name, schema, version
	s.mu.Unlock()
	return p, nil
}

func (s *session) Caps() db.Capabilities {
	return db.Capabilities{Schemas: true, Indexes: true, Triggers: true, ForeignKeys: true, Views: true, MatViews: true,
		Functions: true, Sequences: true, RenameDB: true, Transactions: true, ProcessList: true, Users: true,
		Editor: "sql", Dialect: "postgres", DatabaseNoun: "database"}
}

func (s *session) ServerInfo() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return "PostgreSQL " + s.version
}

func (s *session) p() *pool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pool
}

func (s *session) use(ctx context.Context, f func(*conn) error) error {
	err := s.p().use(ctx, f)
	if err != nil && ctx.Err() != nil && !errors.Is(err, ctx.Err()) {
		err = fmt.Errorf("%w: %v", ctx.Err(), err) // pgx may report the server's "canceling statement" instead
	}
	return err
}

func (s *session) Ping(ctx context.Context) error {
	return s.use(ctx, func(c *conn) error { return c.Ping(ctx) })
}

func (s *session) Close() error {
	s.p().close()
	return nil
}

func (s *session) CurrentDatabase() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dbName
}

func (s *session) CurrentSchema() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.schema
}

func (s *session) Databases(ctx context.Context) ([]string, error) {
	return s.strings(ctx, "SELECT datname::text FROM pg_database WHERE NOT datistemplate AND datallowconn ORDER BY 1")
}

// UseDatabase opens a new pool on name and retires the old one.
func (s *session) UseDatabase(ctx context.Context, name string) error {
	if name == s.CurrentDatabase() {
		return nil
	}
	old := s.p()
	np, err := s.connect(ctx, name)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.pool = np
	s.mu.Unlock()
	s.colMu.Lock()
	s.cols = map[string][]string{}
	s.colMu.Unlock()
	old.close()
	return nil
}

// pool is a minimal connection pool: at most maxConns open, idle ones reused.
type pool struct {
	cfg       *pgx.ConnConfig
	bootstrap []string
	sem       chan struct{}
	types     sync.Map // oid -> type name for types pgx does not know

	mu     sync.Mutex
	idle   []*conn
	closed bool
}

// conn is one pooled connection plus the server notices it collected during the current call.
type conn struct {
	*pgx.Conn
	nmu     sync.Mutex
	notices []string
	bad     bool // must not be reused
}

func (c *conn) addNotice(_ *pgconn.PgConn, n *pgconn.Notice) {
	c.nmu.Lock()
	c.notices = append(c.notices, n.Severity+": "+n.Message)
	c.nmu.Unlock()
}

func (c *conn) takeNotices() string {
	c.nmu.Lock()
	defer c.nmu.Unlock()
	s := strings.Join(c.notices, "\n")
	c.notices = nil
	return s
}

func closeConn(c *conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c.Conn.Close(ctx)
}

func (p *pool) get(ctx context.Context) (*conn, error) {
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		<-p.sem
		return nil, errors.New("connection closed")
	}
	for len(p.idle) > 0 {
		c := p.idle[len(p.idle)-1]
		p.idle = p.idle[:len(p.idle)-1]
		if !c.IsClosed() {
			p.mu.Unlock()
			return c, nil
		}
	}
	p.mu.Unlock()
	c, err := p.dial(ctx)
	if err != nil {
		<-p.sem
		return nil, err
	}
	return c, nil
}

func (p *pool) dial(ctx context.Context) (*conn, error) {
	c := &conn{}
	cfg := p.cfg.Copy()
	cfg.OnNotice = c.addNotice
	pc, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	c.Conn = pc
	for _, b := range p.bootstrap {
		if _, err := pc.Exec(ctx, b); err != nil {
			closeConn(c)
			return nil, fmt.Errorf("bootstrap %q: %w", b, err)
		}
	}
	c.takeNotices()
	return c, nil
}

func (p *pool) put(c *conn) {
	defer func() { <-p.sem }()
	p.mu.Lock()
	if !p.closed && !c.bad && !c.IsClosed() && c.PgConn().TxStatus() == 'I' {
		c.takeNotices()
		p.idle = append(p.idle, c)
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	closeConn(c)
}

func (p *pool) use(ctx context.Context, f func(*conn) error) error {
	c, err := p.get(ctx)
	if err != nil {
		return err
	}
	defer p.put(c)
	return f(c)
}

// close drops idle connections now; busy ones are closed when released.
func (p *pool) close() {
	p.mu.Lock()
	p.closed = true
	idle := p.idle
	p.idle = nil
	p.mu.Unlock()
	for _, c := range idle {
		closeConn(c)
	}
}
