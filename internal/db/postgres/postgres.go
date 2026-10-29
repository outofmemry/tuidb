// Package postgres is the PostgreSQL driver (pgx). It keeps its own tiny connection pool because
// pgxpool needs github.com/jackc/puddle, which is not in go.mod.
package postgres

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/db/iam"
)

func init() { db.Register(driver{}) }

const maxConns = 8

// driver is the PostgreSQL driver; its zero value is "postgres". New builds a sibling that speaks
// the same protocol under another registered name (Redshift, Greenplum, ...), capabilities adjusted.
type driver struct {
	name   string
	adjust func(*db.Capabilities)
}

// resetSQL is the statement that clears session state before a connection returns to the pool; "" = none.
// Vertica has no DISCARD (shortcut: state set with SET then leaks to the next statement), Redshift has RESET.
func resetSQL(name string) string {
	switch name {
	case config.Vertica:
		return ""
	case config.Redshift:
		return "RESET ALL"
	}
	return "DISCARD ALL"
}

// New returns a driver registered under name that reuses this package's session code.
func New(name string, adjust func(*db.Capabilities)) db.Driver { return driver{name, adjust} }

func (d driver) Name() string {
	if d.name == "" {
		return "postgres"
	}
	return d.name
}

func (d driver) Open(ctx context.Context, c config.Connection, dial db.DialFunc) (db.Session, error) {
	s := &session{c: c, dial: dial, cols: map[string][]string{}, caps: caps, reset: resetSQL(d.name)}
	if d.adjust != nil {
		d.adjust(&s.caps)
	}
	p, err := s.connect(ctx, c.Database)
	if err != nil {
		return nil, err
	}
	s.pool, s.dbName, s.schema, s.version = p, p.name, p.schema, p.version
	return s, nil
}

// sslMode maps webtuidb's ssl_mode names to libpq's. "" keeps whatever Options say (default prefer).
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
		if c.IAMAuth && k == "region" { // the IAM token's AWS region, not a server setting
			continue
		}
		q.Set(k, v)
	}
	mode, err := sslMode(c.SSLMode)
	if err != nil {
		return "", err
	}
	if c.IAMAuth { // the 15-minute token is the password: it must not cross the wire in clear
		switch m := cmp.Or(mode, q.Get("sslmode")); m {
		case "":
			mode = "require"
		case "disable", "prefer", "allow":
			return "", fmt.Errorf("iam_auth needs TLS: ssl_mode %q would send the IAM token unencrypted (use require, verify-ca or verify-full)", m)
		}
	}
	if mode != "" {
		q.Set("sslmode", mode)
	}
	if c.Encoding != "" {
		// pgx does no transcoding: any other client_encoding makes the server convert while the client still
		// treats the bytes as UTF-8, corrupting every non-ASCII value on the way in and out.
		if e := strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(c.Encoding)); e != "utf8" {
			return "", fmt.Errorf("encoding %q is not supported by PostgreSQL connections (only UTF-8; leave it empty)", c.Encoding)
		}
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

var caps = db.Capabilities{Schemas: true, Indexes: true, Triggers: true, ForeignKeys: true, Views: true, MatViews: true,
	Functions: true, Sequences: true, RenameDB: true, Transactions: true, ProcessList: true, Users: true,
	Editor: "sql", Dialect: "postgres", DatabaseNoun: "database"}

type session struct {
	c     config.Connection
	dial  db.DialFunc
	caps  db.Capabilities
	reset string // see resetSQL

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
	if s.c.ReadOnly { // enforced by the server too; it survives DISCARD ALL. The client keyword check stays as the first line.
		cfg.RuntimeParams["default_transaction_read_only"] = "on"
	}
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
	p := &pool{cfg: cfg, bootstrap: s.c.Bootstrap, resetSQL: s.reset, sem: make(chan struct{}, maxConns)}
	if s.c.IAMAuth { // a fresh token for every new pooled connection: they expire after 15 minutes
		c := s.c
		p.token = func(ctx context.Context) (string, error) { return iam.Token(ctx, c) }
	}
	var name, schema, version string
	err = p.use(ctx, func(c *conn) error {
		version, _, _ = strings.Cut(c.PgConn().ParameterStatus("server_version"), " ")
		return c.QueryRow(ctx, "SELECT current_database()::text, coalesce(current_schema()::text, 'public')").Scan(&name, &schema)
	})
	if err != nil {
		p.close()
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	p.name, p.schema, p.version = name, schema, version
	return p, nil
}

func (s *session) Caps() db.Capabilities { return s.caps }

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
	s.pool, s.dbName, s.schema, s.version = np, np.name, np.schema, np.version
	s.mu.Unlock()
	s.colMu.Lock()
	s.cols = map[string][]string{}
	s.colMu.Unlock()
	old.close()
	return nil
}

// SetSchema makes name the schema unqualified names resolve in: the search_path becomes name, then public,
// on every pooled connection and on pinned ones at their next statement. Back on the schema the session
// connected with, the server's default path applies again.
func (s *session) SetSchema(_ context.Context, name string) error {
	p := s.p()
	path := ""
	if name != p.schema {
		path = ident(name)
		if name != "public" {
			path += ", public" // extensions usually live there
		}
	}
	p.mu.Lock()
	p.path = path
	p.mu.Unlock()
	s.mu.Lock()
	s.schema = name
	s.mu.Unlock()
	return nil
}

// pool is a minimal connection pool: at most maxConns open, idle ones reused.
type pool struct {
	cfg                   *pgx.ConnConfig
	bootstrap             []string
	resetSQL              string                                // run before reuse of a dirty connection, "" = nothing
	token                 func(context.Context) (string, error) // IAM password, nil = the configured one
	sem                   chan struct{}
	name, schema, version string   // identity probed by connect, published with the pool swap
	types                 sync.Map // oid -> type name for types pgx does not know

	mu     sync.Mutex
	idle   []*conn
	closed bool
	path   string // search_path every connection runs with (SetSchema), "" = the server's default
}

// conn is one pooled connection plus the server notices it collected during the current call.
type conn struct {
	*pgx.Conn
	nmu     sync.Mutex
	notices []string
	bad     bool   // must not be reused
	reused  bool   // taken from the idle list, so the server may have dropped it meanwhile
	dirty   bool   // ran user SQL (SET, temp tables, ...): reset the session before reuse
	pinned  bool   // lent out by Pin: holds the user's transaction, so a statement is never cancelled
	path    string // the pool's search_path as last applied to this connection
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
			c.reused = true
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
	if p.token != nil {
		var err error
		if cfg.Password, err = p.token(ctx); err != nil {
			return nil, err
		}
	}
	pc, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	c.Conn = pc
	for _, b := range p.bootstrap {
		if _, err := pc.Exec(ctx, b); err != nil {
			closeConn(c)
			return nil, fmt.Errorf("bootstrap (%s): %w", b, err)
		}
	}
	c.takeNotices()
	return c, nil
}

func (p *pool) put(c *conn) {
	defer func() { <-p.sem }()
	ok := !c.bad && !c.IsClosed() && c.PgConn().TxStatus() == 'I' && p.reset(c) // not under p.mu: it round-trips
	p.mu.Lock()
	if !p.closed && ok {
		c.takeNotices()
		p.idle = append(p.idle, c)
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	closeConn(c)
}

// reset undoes session state left by user SQL (SET search_path, SET ROLE, temp tables, ...) and replays the
// bootstrap; false means the connection must be closed.
func (p *pool) reset(c *conn) bool {
	if !c.dirty {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	qs := p.bootstrap
	if p.resetSQL != "" {
		qs = append([]string{p.resetSQL}, qs...)
	}
	for _, q := range qs {
		if _, err := c.Exec(ctx, q); err != nil { // shortcut: no logger in this package; the connection is closed instead
			return false
		}
	}
	if p.resetSQL != "" {
		c.path = "" // DISCARD / RESET ALL dropped the search_path: syncPath sets it again
	}
	c.dirty = false
	return true
}

// syncPath gives c the pool's current search_path when SetSchema changed it since c last ran.
func (p *pool) syncPath(ctx context.Context, c *conn) error {
	p.mu.Lock()
	want := p.path
	p.mu.Unlock()
	if c.path == want {
		return nil
	}
	qs := []string{"SET search_path TO " + want}
	if want == "" { // the bootstrap may set its own search_path: replay it after the default
		qs = append([]string{"SET search_path TO DEFAULT"}, p.bootstrap...)
	}
	for _, q := range qs {
		if _, err := c.Exec(ctx, q); err != nil {
			return fmt.Errorf("set schema: %w", err)
		}
	}
	c.path = want
	return nil
}

// use runs f on a pooled connection. If a connection that sat idle turns out to have been dropped
// (server restart, idle timeout, firewall), the idle ones are discarded and f runs once more on a
// fresh connection.
// The retry is limited to reused idle connections whose socket died before anything was sent
// (pgconn.SafeToRetry) or that the server terminated at once (see retryable): an EOF after the write may
// mean the server committed, and Apply would run twice.
func (p *pool) use(ctx context.Context, f func(*conn) error) error {
	inner := f
	f = func(c *conn) error {
		if err := p.syncPath(ctx, c); err != nil {
			return err
		}
		return inner(c)
	}
	c, err := p.get(ctx)
	if err != nil {
		return err
	}
	start := time.Now()
	if err = f(c); err != nil && c.reused && ctx.Err() == nil && c.IsClosed() && retryable(err, time.Since(start)) {
		p.mu.Lock()
		idle := p.idle
		p.idle = nil
		p.mu.Unlock()
		for _, ic := range idle {
			closeConn(ic)
		}
		c.bad = true
		p.put(c)
		if c, err = p.get(ctx); err != nil {
			return err
		}
		err = f(c)
	}
	p.put(c)
	return err
}

// retryWindow: a "terminating connection" error this soon after the statement was sent means the backend
// was already going away while the connection sat idle. Later, it is a running statement that an
// administrator killed on purpose, which must not be started again.
const retryWindow = 2 * time.Second

// retryable: no byte of this statement reached the server (pgconn.SafeToRetry), or the server answered with
// a connection-fatal 57P01 admin_shutdown / 57P02 crash_shutdown / 57P05 idle_session_timeout. A terminated
// backend never commits, so replaying the one statement on a new connection cannot apply it twice.
func retryable(err error, took time.Duration) bool {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "57P01", "57P02", "57P05":
			return took < retryWindow
		}
		return false
	}
	return dropped(err) && pgconn.SafeToRetry(err)
}

// dropped reports a lost socket, as opposed to an error the server answered with.
func dropped(err error) bool {
	var pe *pgconn.PgError
	return !errors.As(err, &pe) && (pgconn.SafeToRetry(err) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, net.ErrClosed))
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
