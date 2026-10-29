// Package cassandra is the Apache Cassandra driver (github.com/apache/cassandra-gocql-driver/v2, CQL over the native protocol).
package cassandra

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func init() { db.Register(drv{}) }

type drv struct{}

func (drv) Name() string { return config.Cassandra }

var caps = db.Capabilities{
	Indexes: true, Triggers: true, MatViews: true, Functions: true,
	Editor: "sql", Dialect: "cql", DatabaseNoun: "keyspace",
	Types: strings.Fields("text varchar ascii int bigint smallint tinyint varint float double decimal boolean uuid timeuuid timestamp date time duration inet blob counter " +
		"list<text> set<text> map<text,text> frozen<list<text>> frozen<set<text>> tuple<int,text>"),
}

var q = db.CQL.Quote

type session struct {
	conn config.Connection
	dial db.DialFunc
	opts options

	mu     sync.RWMutex
	closed bool
	ses    *gocql.Session
	ks     string
	cons   gocql.Consistency
	types  map[string]map[string]gocql.TypeInfo // column types per "keyspace.table", for turning typed text into driver values
	aggs   map[string]bool                      // function-list entries that are aggregates ("name(args)")
	info   string
}

// options are the driver extras (Connection.Options): consistency, local_dc, keyspace, timeout, protocol_version.
type options struct {
	cons     gocql.Consistency
	localDC  string
	keyspace string
	timeout  time.Duration
	proto    int
}

func parseOptions(m map[string]string) (o options, err error) {
	o.cons = gocql.LocalQuorum
	for k, v := range m {
		switch strings.ToLower(k) {
		case "consistency":
			if o.cons, err = gocql.ParseConsistencyWrapper(v); err != nil {
				return o, fmt.Errorf("option consistency: %w", err)
			}
		case "local_dc":
			o.localDC = v
		case "keyspace":
			o.keyspace = v
		case "timeout":
			if o.timeout, err = time.ParseDuration(v); err != nil {
				return o, fmt.Errorf("option timeout: %w", err)
			}
		case "protocol_version":
			if o.proto, err = strconv.Atoi(v); err != nil {
				return o, fmt.Errorf("option protocol_version: %w", err)
			}
		default:
			return o, fmt.Errorf("unknown cassandra option %q (want consistency, local_dc, keyspace, timeout, protocol_version)", k)
		}
	}
	return o, nil
}

// dialer adapts the SSH tunnel's DialFunc.
type dialer db.DialFunc

func (d dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d(ctx, network, addr)
}

func (drv) Open(ctx context.Context, conn config.Connection, dial db.DialFunc) (db.Session, error) {
	o, err := parseOptions(conn.Options)
	if err != nil {
		return nil, err
	}
	s := &session{conn: conn, dial: dial, opts: o, cons: o.cons, types: map[string]map[string]gocql.TypeInfo{}, aggs: map[string]bool{}}
	ks := conn.Database
	if ks == "" {
		ks = o.keyspace
	}
	// shortcut: gocql has no per-connection init hook, so Bootstrap runs once on connect: `USE ks` picks the
	// keyspace, `CONSISTENCY level` the default consistency, anything else is executed a single time.
	var once []string
	for _, b := range conn.Bootstrap {
		switch m := cmdRe.FindStringSubmatch(b); {
		case m != nil && strings.EqualFold(m[1], "CONSISTENCY"):
			if s.cons, err = gocql.ParseConsistencyWrapper(m[2]); err != nil {
				return nil, fmt.Errorf("bootstrap (%s): %w", b, err)
			}
		case useRe.MatchString(b):
			ks = keyspaceName(useRe.FindStringSubmatch(b))
		default:
			once = append(once, b)
		}
	}
	if s.ses, err = s.openSession(ctx, ks); err != nil {
		return nil, err
	}
	s.ks = ks
	for _, b := range once {
		if err := s.ses.Query(b).ExecContext(ctx); err != nil {
			s.ses.Close()
			return nil, fmt.Errorf("bootstrap (%s): %w", b, err)
		}
	}
	var v string
	if err := s.ses.Query("SELECT release_version FROM system.local").Consistency(gocql.LocalOne).ScanContext(ctx, &v); err != nil {
		s.ses.Close()
		return nil, fmt.Errorf("read server version: %w", err)
	}
	s.info = "Cassandra " + v
	return s, nil
}

func (s *session) openSession(ctx context.Context, keyspace string) (*gocql.Session, error) {
	c := s.conn
	cl := gocql.NewCluster(c.Host)
	cl.Port = c.EffectivePort()
	cl.Keyspace = keyspace
	cl.Consistency = s.consistency()
	cl.ConnectTimeout = 10 * time.Second
	if s.opts.timeout > 0 {
		cl.Timeout = s.opts.timeout
	}
	cl.ProtoVersion = s.opts.proto
	// A prepared SELECT * keeps its old column list after an ALTER unless the result metadata is sent every time.
	cl.DisableSkipMetadata = true
	if c.User != "" {
		cl.Authenticator = gocql.PasswordAuthenticator{Username: c.User, Password: c.Password}
	}
	if s.opts.localDC != "" {
		cl.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(gocql.DCAwareRoundRobinPolicy(s.opts.localDC))
	}
	if s.dial != nil {
		cl.Dialer = dialer(s.dial)
	}
	tc, err := tlsConfig(c)
	if err != nil {
		return nil, err
	}
	if tc != nil {
		cl.SslOpts = &gocql.SslOptions{Config: tc}
	}
	ses, err := cl.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("connect to %s:%d: %w", c.Host, cl.Port, err)
	}
	return ses, nil
}

// tlsConfig maps ssl_mode to a client TLS config (nil for plaintext).
func tlsConfig(c config.Connection) (*tls.Config, error) {
	mode := c.SSLMode
	if mode == "" || mode == "disable" {
		return nil, nil
	}
	t := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	if c.SSLCA != "" {
		pem, err := os.ReadFile(c.SSLCA)
		if err != nil {
			return nil, fmt.Errorf("ssl_ca: %w", err)
		}
		t.RootCAs = x509.NewCertPool()
		if !t.RootCAs.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ssl_ca %s: no certificates found", c.SSLCA)
		}
	}
	if c.SSLCert != "" || c.SSLKey != "" {
		pair, err := tls.LoadX509KeyPair(c.SSLCert, c.SSLKey)
		if err != nil {
			return nil, fmt.Errorf("ssl_cert/ssl_key: %w", err)
		}
		t.Certificates = []tls.Certificate{pair}
	}
	switch mode {
	case "prefer", "require": // encrypted, certificate not checked
		t.InsecureSkipVerify = true
	case "verify-ca": // verify the chain but not the host name
		t.InsecureSkipVerify = true
		t.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("server sent no certificate")
			}
			inter := x509.NewCertPool()
			for _, ic := range cs.PeerCertificates[1:] {
				inter.AddCert(ic)
			}
			_, err := cs.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: t.RootCAs, Intermediates: inter})
			return err
		}
	case "verify-full", "verify-identity":
	default:
		return nil, fmt.Errorf("unsupported ssl_mode %q for cassandra", mode)
	}
	return t, nil
}

func (s *session) Caps() db.Capabilities { return caps }
func (s *session) ServerInfo() string    { return s.info }
func (s *session) Close() error {
	s.mu.Lock()
	s.closed = true
	ses := s.ses
	s.mu.Unlock()
	if ses != nil {
		ses.Close() // idempotent in gocql
	}
	return nil
}

func (s *session) Ping(ctx context.Context) error {
	return s.query("SELECT key FROM system.local").Consistency(gocql.LocalOne).ScanContext(ctx, new(string))
}

func (s *session) sess() *gocql.Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ses
}

func (s *session) consistency() gocql.Consistency {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cons
}

// query builds a query on the current session with the session's consistency.
func (s *session) query(stmt string, args ...any) *gocql.Query {
	return s.sess().Query(stmt, args...).Consistency(s.consistency())
}

func (s *session) CurrentDatabase() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ks
}

func (s *session) Schemas(context.Context) ([]string, error) { return nil, nil }
func (s *session) CurrentSchema() string                     { return "" }

// UseDatabase reopens the session: gocql does not support USE (it would change one pooled connection only).
func (s *session) UseDatabase(ctx context.Context, name string) error {
	ses, err := s.openSession(ctx, name)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		ses.Close()
		return errors.New("session is closed")
	}
	old := s.ses
	s.ses, s.ks, s.types, s.aggs = ses, name, map[string]map[string]gocql.TypeInfo{}, map[string]bool{}
	s.mu.Unlock()
	old.Close()
	return nil
}

var (
	// leading comments are allowed: db.Split keeps them in the statement text, and the server would still run the USE.
	useRe = regexp.MustCompile(`(?is)^(?:\s|--[^\n]*|//[^\n]*|/\*.*?\*/)*use\s+(?:"((?:[^"]|"")+)"|(\w+))\s*;?\s*$`)
	// cmdRe: cqlsh-style session commands the server does not know.
	cmdRe = regexp.MustCompile(`(?is)^\s*(consistency)\s+(\w+)\s*;?\s*$`)
)

// keyspaceName reads a useRe match: quoted names keep their case, bare ones are folded to lower case as CQL does.
func keyspaceName(m []string) string {
	if m[1] != "" {
		return strings.ReplaceAll(m[1], `""`, `"`)
	}
	return strings.ToLower(m[2])
}

// ksOf is the keyspace a reference lives in.
func (s *session) ksOf(ref db.ObjRef) (string, error) {
	if ref.Schema != "" {
		return ref.Schema, nil
	}
	if ks := s.CurrentDatabase(); ks != "" {
		return ks, nil
	}
	return "", errors.New("no keyspace selected: open one first")
}

// qual is the quoted keyspace.name of a reference.
func (s *session) qual(ref db.ObjRef) (string, error) {
	ks, err := s.ksOf(ref)
	if err != nil {
		return "", err
	}
	return q(ks) + "." + q(ref.Name), nil
}

func (s *session) invalidate() {
	s.mu.Lock()
	s.types = map[string]map[string]gocql.TypeInfo{} // aggs stays: only Objects knows it, and the sidebar reloads that after DDL
	s.mu.Unlock()
}

// colTypes returns the driver types of a table's columns. They come from the result metadata of a one-row SELECT,
// and are cached until the schema may have changed.
func (s *session) colTypes(ctx context.Context, ref db.ObjRef) (map[string]gocql.TypeInfo, error) {
	ks, err := s.ksOf(ref)
	if err != nil {
		return nil, err
	}
	key := ks + "." + ref.Name
	s.mu.RLock()
	m, ok := s.types[key]
	s.mu.RUnlock()
	if ok {
		return m, nil
	}
	it := s.query("SELECT * FROM " + q(ks) + "." + q(ref.Name) + " LIMIT 1").IterContext(ctx)
	m = map[string]gocql.TypeInfo{}
	for _, c := range it.Columns() {
		m[c.Name] = c.TypeInfo
	}
	if err := it.Close(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.types[key] = m
	s.mu.Unlock()
	return m, nil
}

func (s *session) Exec(ctx context.Context, stmt string, limit int) (*db.Result, error) {
	start := time.Now()
	if m := useRe.FindStringSubmatch(stmt); m != nil {
		if err := s.UseDatabase(ctx, keyspaceName(m)); err != nil {
			return nil, err
		}
		return &db.Result{Message: "Keyspace changed", Duration: time.Since(start)}, nil
	}
	if m := cmdRe.FindStringSubmatch(stmt); m != nil {
		c, err := gocql.ParseConsistencyWrapper(m[2])
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.cons = c
		s.mu.Unlock()
		return &db.Result{Message: "Consistency level is now " + c.String(), Duration: time.Since(start)}, nil
	}
	if !db.IsReadOnly("cql", stmt) {
		defer s.invalidate() // DDL may have changed column lists
	}
	// Split trims the statement, but the server's lexer wants a newline to end a trailing -- or // comment.
	res, err := s.run(ctx, stmt+"\n", nil, limit, 0)
	if err != nil {
		return nil, err
	}
	res.Duration = time.Since(start)
	return res, nil
}

// run executes one statement and reads at most limit rows (0 = all) after skipping skip of them.
// CQL has no OFFSET: skipped rows are still fetched and decoded by the driver, then dropped, so deep pages cost O(offset).
func (s *session) run(ctx context.Context, text string, args []any, limit, skip int) (*db.Result, error) {
	qr := s.query(text, args...)
	if limit > 0 && limit+skip < 5000 {
		qr = qr.PageSize(limit + skip + 1) // do not pull a 5000-row page for a 10-row preview
	}
	it := qr.IterContext(ctx)
	cols := it.Columns()
	res := &db.Result{}
	if len(cols) == 0 { // DDL, INSERT, UPDATE, DELETE
		warn := warnings(it)
		if err := it.Close(); err != nil {
			return nil, err
		}
		res.Message = "OK" + warn
		return res, nil
	}
	rd := newReader(cols)
	for _, c := range cols {
		res.Columns = append(res.Columns, db.ResultColumn{Name: c.Name, Type: typeName(c.TypeInfo)})
	}
	res.HasRows = true
	for n := 0; it.Scan(rd.dest...); n++ {
		if n < skip {
			continue
		}
		if limit > 0 && len(res.Rows) == limit {
			res.Truncated = true
			break
		}
		res.Rows = append(res.Rows, rd.row())
	}
	res.Message = strings.TrimSpace(warnings(it))
	if err := it.Close(); err != nil {
		return nil, err
	}
	return res, nil
}

func warnings(it *gocql.Iter) string {
	if w := it.Warnings(); len(w) > 0 {
		return " (" + strings.Join(w, "; ") + ")"
	}
	return ""
}
