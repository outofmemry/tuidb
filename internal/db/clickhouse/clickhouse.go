// Package clickhouse is the webtuidb driver for ClickHouse over the native protocol.
package clickhouse

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func init() { db.Register(driver{}) }

type driver struct{}

func (driver) Name() string { return config.ClickHouse }

var setRe = regexp.MustCompile(`(?is)^\s*SET\s+(\w+)\s*=\s*(.+?)\s*;?\s*$`)

func (driver) Open(ctx context.Context, c config.Connection, dial db.DialFunc) (db.Session, error) {
	tc, err := tlsConfig(c)
	if err != nil {
		return nil, err
	}
	user := c.User
	if user == "" {
		user = "default"
	}
	opts := &ch.Options{
		Addr:         []string{net.JoinHostPort(c.Host, strconv.Itoa(c.EffectivePort()))},
		Auth:         ch.Auth{Database: c.Database, Username: user, Password: c.Password},
		MaxOpenConns: 8,
		MaxIdleConns: 2,
		Settings:     ch.Settings{},
		DialContext: func(ctx context.Context, addr string) (net.Conn, error) {
			var conn net.Conn
			var err error
			if dial != nil {
				conn, err = dial(ctx, "tcp", addr)
			} else {
				conn, err = (&net.Dialer{Timeout: 30 * time.Second}).DialContext(ctx, "tcp", addr)
			}
			if err != nil || tc == nil {
				return conn, err
			}
			tconn := tls.Client(conn, tc)
			if err := tconn.HandshakeContext(ctx); err != nil {
				conn.Close()
				return nil, err
			}
			return tconn, nil
		},
	}
	// shortcut: the native client has no per-connection init hook, so `SET k = v` bootstrap
	// statements become client settings (applied to every pooled connection) and any other
	// bootstrap statement runs once on one connection. Upgrade if non-SET bootstrap matters.
	var once []string
	for _, b := range c.Bootstrap {
		if m := setRe.FindStringSubmatch(b); m != nil {
			opts.Settings[m[1]] = settingValue(m[2])
		} else {
			once = append(once, b)
		}
	}
	s := &session{opts: *opts}
	if s.conn, err = ch.Open(opts); err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}
	fail := func(err error) (db.Session, error) { s.conn.Close(); return nil, err }
	if err := s.conn.Ping(ctx); err != nil {
		return fail(fmt.Errorf("connect to clickhouse: %w", err))
	}
	for _, b := range once {
		if err := s.conn.Exec(ctx, b); err != nil {
			return fail(fmt.Errorf("bootstrap %q: %w", b, err))
		}
	}
	v, err := s.conn.ServerVersion()
	if err != nil {
		return fail(fmt.Errorf("server version: %w", err))
	}
	s.info = "ClickHouse " + v.Version.String()
	rows, err := s.rows(ctx, "SELECT currentDatabase()")
	if err != nil {
		return fail(err)
	}
	s.cur = str(rows[0][0])
	return s, nil
}

func settingValue(v string) any {
	v = strings.Trim(strings.TrimSpace(v), `'"`)
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n
	}
	return v
}

// tlsConfig maps ssl_mode and the cert paths to a tls.Config; nil means plain TCP.
func tlsConfig(c config.Connection) (*tls.Config, error) {
	switch c.SSLMode {
	case "", "prefer", "disable":
		return nil, nil
	case "require", "verify-ca", "verify-full", "verify-identity":
	default:
		return nil, fmt.Errorf("clickhouse: unsupported ssl_mode %q", c.SSLMode)
	}
	cfg := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	if c.SSLCA != "" {
		pem, err := os.ReadFile(c.SSLCA)
		if err != nil {
			return nil, fmt.Errorf("ssl_ca: %w", err)
		}
		cfg.RootCAs = x509.NewCertPool()
		if !cfg.RootCAs.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ssl_ca %s: no certificates found", c.SSLCA)
		}
	}
	if (c.SSLCert == "") != (c.SSLKey == "") {
		return nil, errors.New("ssl_cert and ssl_key must be set together")
	}
	if c.SSLCert != "" {
		pair, err := tls.LoadX509KeyPair(c.SSLCert, c.SSLKey)
		if err != nil {
			return nil, fmt.Errorf("ssl_cert/ssl_key: %w", err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	switch c.SSLMode {
	case "require":
		cfg.InsecureSkipVerify = true
	case "verify-ca": // verify the chain but not the host name
		cfg.InsecureSkipVerify = true
		cfg.VerifyPeerCertificate = func(raw [][]byte, _ [][]*x509.Certificate) error {
			var certs []*x509.Certificate
			for _, r := range raw {
				cert, err := x509.ParseCertificate(r)
				if err != nil {
					return err
				}
				certs = append(certs, cert)
			}
			if len(certs) == 0 {
				return errors.New("server sent no certificate")
			}
			inter := x509.NewCertPool()
			for _, cert := range certs[1:] {
				inter.AddCert(cert)
			}
			_, err := certs[0].Verify(x509.VerifyOptions{Roots: cfg.RootCAs, Intermediates: inter})
			return err
		}
	}
	return cfg, nil
}

type session struct {
	opts ch.Options
	info string

	mu       sync.RWMutex
	conn     chdriver.Conn
	cur      string
	colCache map[string][]string
}

var dialect = db.ClickHouse

func (s *session) Caps() db.Capabilities {
	return db.Capabilities{Views: true, MatViews: true, ReorderColumns: true, RenameDB: true, ProcessList: true,
		Editor: "sql", Dialect: "clickhouse", DatabaseNoun: "database"}
}

func (s *session) ServerInfo() string { return s.info }

func (s *session) c() chdriver.Conn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.conn
}

func (s *session) Ping(ctx context.Context) error { return s.c().Ping(ctx) }

func (s *session) Close() error { return s.c().Close() }

func (s *session) CurrentDatabase() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

func (s *session) UseDatabase(ctx context.Context, name string) error {
	s.mu.RLock()
	opts := s.opts
	s.mu.RUnlock()
	opts.Auth.Database = name
	c, err := ch.Open(&opts)
	if err != nil {
		return fmt.Errorf("use database %s: %w", name, err)
	}
	if err := c.Ping(ctx); err != nil {
		c.Close()
		return fmt.Errorf("use database %s: %w", name, err)
	}
	s.mu.Lock()
	old := s.conn
	s.conn, s.cur, s.colCache, s.opts = c, name, nil, opts
	s.mu.Unlock()
	old.Close()
	return nil
}

func (s *session) Databases(ctx context.Context) ([]string, error) {
	rows, err := s.rows(ctx, "SELECT name FROM system.databases ORDER BY name")
	if err != nil {
		return nil, err
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = str(r[0])
	}
	return out, nil
}

func (s *session) Schemas(context.Context) ([]string, error) { return nil, nil }
func (s *session) CurrentSchema() string                     { return "" }

func (s *session) dbOf(ref db.ObjRef) string {
	if ref.Schema != "" {
		return ref.Schema
	}
	return s.CurrentDatabase()
}

func (s *session) Objects(ctx context.Context, schema string) ([]db.Object, error) {
	rows, err := s.rows(ctx, "SELECT name, engine, total_rows FROM system.tables WHERE database = ? AND NOT startsWith(name, '.inner') ORDER BY name",
		s.dbOf(db.ObjRef{Schema: schema}))
	if err != nil {
		return nil, err
	}
	out := make([]db.Object, len(rows))
	refSchema := "" // refs of another database carry it, or an all-schemas search counts the current one
	if schema != "" && schema != s.CurrentDatabase() {
		refSchema = schema
	}
	for i, r := range rows {
		o := db.Object{ObjRef: db.ObjRef{Schema: refSchema, Name: str(r[0]), Kind: db.KindTable}, Detail: str(r[2])}
		switch str(r[1]) {
		case "View":
			o.Kind = db.KindView
		case "MaterializedView":
			o.Kind = db.KindMatView
		}
		out[i] = o
	}
	return out, nil
}

func (s *session) Columns(ctx context.Context, ref db.ObjRef) ([]db.Column, error) {
	rows, err := s.rows(ctx, "SELECT name, type, default_kind, default_expression, is_in_primary_key, comment FROM system.columns WHERE database = ? AND table = ? ORDER BY position",
		s.dbOf(ref), ref.Name)
	if err != nil {
		return nil, err
	}
	out := make([]db.Column, len(rows))
	for i, r := range rows {
		c := db.Column{Name: str(r[0]), OrigName: str(r[0]), Type: str(r[1]), PK: r[4] == uint64(1)}
		c.Nullable = strings.HasPrefix(c.Type, "Nullable(")
		kind, expr := str(r[2]), str(r[3])
		switch {
		case kind == "DEFAULT":
			c.Default, c.HasDef = expr, true
		case kind != "":
			c.Extra = kind + " " + expr
		}
		if cm := str(r[5]); cm != "" {
			c.Extra = strings.TrimSpace(c.Extra + " COMMENT " + dialect.Literal(cm))
		}
		out[i] = c
	}
	return out, nil
}

func (s *session) Indexes(context.Context, db.ObjRef) ([]db.Index, error)          { return nil, nil }
func (s *session) Triggers(context.Context, db.ObjRef) ([]db.Trigger, error)       { return nil, nil }
func (s *session) ForeignKeys(context.Context, db.ObjRef) ([]db.ForeignKey, error) { return nil, nil }

func (s *session) DDL(ctx context.Context, ref db.ObjRef) (string, error) {
	rows, err := s.rows(ctx, "SHOW CREATE TABLE "+dialect.QualifiedName(db.ObjRef{Schema: ref.Schema, Name: ref.Name}))
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("no DDL for %s", ref.Name)
	}
	return str(rows[0][0]) + ";", nil
}

func (s *session) Script(ctx context.Context, kind db.ScriptKind, ref db.ObjRef) (string, error) {
	name := dialect.QualifiedName(ref)
	switch kind {
	case db.ScriptCreate:
		return s.DDL(ctx, ref)
	case db.ScriptDrop:
		return "DROP " + dropWord(ref) + " " + name + ";", nil
	case db.ScriptTruncate:
		return "TRUNCATE TABLE " + name + ";", nil
	case db.ScriptSelectTop:
		return "SELECT * FROM " + name + " LIMIT 100;", nil
	}
	return "", fmt.Errorf("unknown script kind %q", kind)
}

func dropWord(ref db.ObjRef) string {
	if ref.Kind == db.KindView || ref.Kind == db.KindMatView {
		return "VIEW"
	}
	return "TABLE"
}

// tableCols returns the column names for Any-column filters, cached per table.
func (s *session) tableCols(ctx context.Context, ref db.ObjRef) ([]string, error) {
	key := s.dbOf(ref) + "." + ref.Name
	s.mu.RLock()
	cols, ok := s.colCache[key]
	s.mu.RUnlock()
	if ok {
		return cols, nil
	}
	cs, err := s.Columns(ctx, ref)
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		if searchable(c.Type) {
			cols = append(cols, c.Name)
		}
	}
	s.mu.Lock()
	if s.colCache == nil {
		s.colCache = map[string][]string{}
	}
	s.colCache[key] = cols
	s.mu.Unlock()
	return cols, nil
}

// searchable: the "Any column" filter converts every listed column to text for every row, which on
// Map / Array / Nested / JSON columns (OpenTelemetry attributes, say) is slow enough to get the
// connection killed. Those columns are left out of that search; filter on them by name instead.
func searchable(typ string) bool {
	t := strings.TrimPrefix(strings.TrimSuffix(typ, ")"), "LowCardinality(")
	for _, p := range []string{"Map(", "Array(", "Tuple(", "Nested(", "JSON", "Object(", "AggregateFunction(", "SimpleAggregateFunction("} {
		if strings.HasPrefix(t, p) || strings.HasPrefix(strings.TrimPrefix(t, "Nullable("), p) {
			return false
		}
	}
	return true
}

func (s *session) Rows(ctx context.Context, q db.RowQuery) (*db.Result, error) {
	cols, err := s.tableCols(ctx, q.ObjRef)
	if err != nil {
		return nil, err
	}
	sql, args, err := db.BuildSelect(dialect, q, cols)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	res, err := s.query(ctx, sql, 0, args...)
	if err != nil {
		return nil, err
	}
	res.Duration = time.Since(start)
	return res, nil
}

func (s *session) Count(ctx context.Context, q db.RowQuery, estimate bool) (db.Count, error) {
	if estimate && len(q.Filters.Active()) == 0 {
		rows, err := s.rows(ctx, "SELECT total_rows FROM system.tables WHERE database = ? AND name = ?", s.dbOf(q.ObjRef), q.Name)
		if err != nil {
			return db.Count{}, err
		}
		if len(rows) == 1 {
			if n, ok := rows[0][0].(uint64); ok {
				return db.Count{N: int64(n)}, nil
			}
		}
	}
	cols, err := s.tableCols(ctx, q.ObjRef)
	if err != nil {
		return db.Count{}, err
	}
	sql, args, err := db.BuildCount(dialect, q, cols)
	if err != nil {
		return db.Count{}, err
	}
	rows, err := s.rows(ctx, sql, args...)
	if err != nil {
		return db.Count{}, err
	}
	n, _ := rows[0][0].(uint64)
	return db.Count{N: int64(n), Exact: true}, nil
}

// rows runs a metadata query and returns all its rows.
func (s *session) rows(ctx context.Context, sql string, args ...any) ([][]any, error) {
	res, err := s.query(ctx, sql, 0, args...)
	if err != nil {
		return nil, err
	}
	return res.Rows, nil
}

func (s *session) query(ctx context.Context, sql string, limit int, args ...any) (*db.Result, error) {
	ctx, cancel := context.WithCancel(ctx) // closing rows early would drain the whole result: cancel instead
	defer cancel()
	rows, err := s.c().Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()
	cts := rows.ColumnTypes()
	res := &db.Result{HasRows: len(cts) > 0}
	for _, ct := range cts {
		res.Columns = append(res.Columns, db.ResultColumn{Name: ct.Name(), Type: ct.DatabaseTypeName()})
	}
	for rows.Next() {
		if limit > 0 && len(res.Rows) == limit {
			res.Truncated = true
			cancel()
			return res, nil
		}
		dest := make([]any, len(cts))
		for i, ct := range cts {
			dest[i] = reflect.New(ct.ScanType()).Interface()
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		row := make([]any, len(cts))
		for i, d := range dest {
			row[i] = normalize(d, res.Columns[i].Type)
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	return res, nil
}

// run executes a statement without a result set. Mutations are made synchronous so the UI
// sees their effect at once; the setting is skipped for reads because readonly users may not set it.
func (s *session) run(ctx context.Context, sql string, args ...any) (uint64, error) {
	var wrote atomic.Uint64
	ctx = ch.Context(ctx, ch.WithSettings(ch.Settings{"mutations_sync": 1}), ch.WithProgress(func(p *ch.Progress) { wrote.Add(p.WroteRows) }))
	err := s.c().Exec(ctx, sql, args...)
	return wrote.Load(), err
}

// useRe matches USE <db>; it must switch the session, not one pooled connection.
var useRe = regexp.MustCompile("(?is)^" + chWS + "use" + chWS1 + "(?:`([^`]+)`|\"([^\"]+)\"|([^\\s;`\"/-][^\\s;`\"/]*))" + chWS + ";?" + chWS + "$")

// chWS is whitespace or comments, so "-- note\nUSE db" still switches the session.
const chWS = `(?:\s|--[^\n]*(?:\n|$)|/\*.*?\*/)*`
const chWS1 = `(?:\s|--[^\n]*(?:\n|$)|/\*.*?\*/)+`

func (s *session) Exec(ctx context.Context, stmt string, limit int) (*db.Result, error) {
	start := time.Now()
	if m := useRe.FindStringSubmatch(stmt); m != nil {
		if err := s.UseDatabase(ctx, m[1]+m[2]+m[3]); err != nil {
			return nil, err
		}
		return &db.Result{Message: "Database changed", Duration: time.Since(start)}, nil
	}
	var res *db.Result
	if db.IsReadOnly("clickhouse", stmt) {
		var err error
		if res, err = s.query(ctx, stmt, limit); err != nil {
			return nil, err
		}
	} else {
		n, err := s.run(ctx, stmt)
		if err != nil {
			return nil, fmt.Errorf("exec: %w", err)
		}
		res = &db.Result{Affected: int64(n), Message: "Statement executed"}
		if n > 0 {
			res.Message = fmt.Sprintf("%d rows affected", n)
		}
		s.invalidate()
	}
	if !res.HasRows && res.Message == "" {
		res.Message = "OK"
	}
	res.Duration = time.Since(start)
	return res, nil
}

func (s *session) invalidate() {
	s.mu.Lock()
	s.colCache = nil
	s.mu.Unlock()
}

func (s *session) Apply(ctx context.Context, stmts []db.Statement) error {
	defer s.invalidate()
	for _, st := range stmts {
		if _, err := s.run(ctx, st.Text, st.Args...); err != nil {
			return fmt.Errorf("%s: %w", st.Shown(), err)
		}
	}
	return nil
}

func (s *session) Plan(changes []db.Change) ([]db.Statement, error) {
	return db.PlanSQL(dialect, changes)
}

func (s *session) Processes(ctx context.Context) ([]db.ProcessInfo, error) {
	rows, err := s.rows(ctx, "SELECT query_id, user, current_database, is_cancelled, elapsed, query FROM system.processes WHERE query_id != queryID() ORDER BY elapsed DESC")
	if err != nil {
		return nil, err
	}
	out := make([]db.ProcessInfo, len(rows))
	for i, r := range rows {
		state := "Running"
		if r[3] == uint64(1) {
			state = "Cancelled"
		}
		el, _ := r[4].(float64)
		out[i] = db.ProcessInfo{ID: str(r[0]), User: str(r[1]), DB: str(r[2]), State: state,
			Elapsed: time.Duration(el * float64(time.Second)), Query: str(r[5])}
	}
	return out, nil
}

func (s *session) Kill(ctx context.Context, id string) error {
	if _, err := s.rows(ctx, "KILL QUERY WHERE query_id = ? SYNC", id); err != nil {
		return fmt.Errorf("kill query: %w", err)
	}
	return nil
}
