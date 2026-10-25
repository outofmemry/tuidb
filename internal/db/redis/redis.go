// Package redis implements the db.Driver for Redis (and compatible servers). A "table" is a key;
// Columns are synthesized from the key's type and edits are translated into commands.
package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

const maxKeys = 10000 // Objects stops listing here

// shortcut: filtering/sorting loads at most this many elements of one key, and hash/set pages
// cannot start deeper than this; a var so tests can lower it.
var maxLoad = 100000

func init() { db.Register(driver{}) }

type driver struct{}

func (driver) Name() string { return "redis" }

func (driver) Open(ctx context.Context, conn config.Connection, dial db.DialFunc) (db.Session, error) {
	opt, err := buildOptions(conn, dial)
	if err != nil {
		return nil, err
	}
	s := &session{opt: opt, cl: goredis.NewClient(&opt)}
	s.db = opt.DB
	if err := s.Ping(ctx); err != nil {
		s.cl.Close()
		return nil, fmt.Errorf("connect: %w", err)
	}
	s.info = "Redis"
	if txt, err := s.cl.Info(ctx, "server").Result(); err == nil {
		for _, l := range strings.Split(txt, "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(l), "redis_version:"); ok {
				s.info = "Redis " + v
			}
		}
	}
	return s, nil
}

func buildOptions(conn config.Connection, dial db.DialFunc) (goredis.Options, error) {
	host := conn.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := conn.Port
	if port == 0 {
		port = config.DefaultPort(config.Redis)
	}
	opt := goredis.Options{
		Addr:     net.JoinHostPort(host, strconv.Itoa(port)),
		Username: conn.User,
		Password: conn.Password,
		PoolSize: 8,
		// Maintenance notifications are a cluster-upgrade handshake we never need.
		MaintNotificationsConfig: &maintnotifications.Config{Mode: maintnotifications.ModeDisabled},
	}
	if conn.Database != "" {
		n, err := strconv.Atoi(conn.Database)
		if err != nil || n < 0 {
			return opt, fmt.Errorf("redis database must be a db index, got %q", conn.Database)
		}
		opt.DB = n
	}
	tlsCfg, err := tlsConfig(conn, host)
	if err != nil {
		return opt, err
	}
	opt.Dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var c net.Conn
		var err error
		if dial != nil {
			c, err = dial(ctx, network, addr)
		} else {
			c, err = (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
		}
		if err != nil || tlsCfg == nil {
			return c, err
		}
		tc := tls.Client(c, tlsCfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			c.Close()
			return nil, err
		}
		return tc, nil
	}
	if len(conn.Bootstrap) > 0 {
		var cmds [][]string
		for _, b := range conn.Bootstrap {
			args, err := parseCommand(b)
			if err != nil || len(args) == 0 {
				return opt, fmt.Errorf("bootstrap %q: %v", b, err)
			}
			cmds = append(cmds, args)
		}
		opt.OnConnect = func(ctx context.Context, cn *goredis.Conn) error {
			for _, a := range cmds {
				if err := cn.Do(ctx, toAny(a)...).Err(); err != nil {
					return fmt.Errorf("bootstrap %s: %w", a[0], err)
				}
			}
			return nil
		}
	}
	return opt, nil
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func tlsConfig(conn config.Connection, host string) (*tls.Config, error) {
	switch conn.SSLMode {
	case "", "prefer", "disable":
		return nil, nil
	case "require", "verify-ca", "verify-full", "verify-identity":
	default:
		return nil, fmt.Errorf("unsupported ssl_mode %q for redis", conn.SSLMode)
	}
	cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if conn.SSLCA != "" {
		pem, err := os.ReadFile(conn.SSLCA)
		if err != nil {
			return nil, fmt.Errorf("ssl_ca: %w", err)
		}
		cfg.RootCAs = x509.NewCertPool()
		if !cfg.RootCAs.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ssl_ca %s: no certificates found", conn.SSLCA)
		}
	}
	if conn.SSLCert != "" || conn.SSLKey != "" {
		pair, err := tls.LoadX509KeyPair(conn.SSLCert, conn.SSLKey)
		if err != nil {
			return nil, fmt.Errorf("ssl_cert/ssl_key: %w", err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	switch conn.SSLMode {
	case "require":
		cfg.InsecureSkipVerify = true
	case "verify-ca": // verify the chain but not the host name
		cfg.InsecureSkipVerify = true
		cfg.VerifyPeerCertificate = func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("server sent no certificate")
			}
			opts := x509.VerifyOptions{Roots: cfg.RootCAs, Intermediates: x509.NewCertPool()}
			var leaf *x509.Certificate
			for i, r := range raw {
				c, err := x509.ParseCertificate(r)
				if err != nil {
					return err
				}
				if i == 0 {
					leaf = c
				} else {
					opts.Intermediates.AddCert(c)
				}
			}
			_, err := leaf.Verify(opts)
			return err
		}
	}
	return cfg, nil
}

type session struct {
	mu   sync.RWMutex
	cl   *goredis.Client
	opt  goredis.Options
	db   int
	info string
	// marks remembers, per stream key, where the last pages started and ended so the next page
	// continues from an entry ID instead of re-reading the stream from its head.
	marks map[string][2]streamMark
}

func (s *session) c() *goredis.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cl
}

func (s *session) Caps() db.Capabilities {
	return db.Capabilities{Transactions: true, ProcessList: true, Document: true,
		Editor: "redis", Dialect: "redis", DatabaseNoun: "db index"}
}

func (s *session) ServerInfo() string { return s.info }

func (s *session) Ping(ctx context.Context) error { return s.c().Ping(ctx).Err() }

func (s *session) Close() error { return s.c().Close() }

func (s *session) Databases(ctx context.Context) ([]string, error) {
	n := 16
	if m, err := s.c().ConfigGet(ctx, "databases").Result(); err == nil {
		if v, err := strconv.Atoi(m["databases"]); err == nil && v > 0 {
			n = v
		}
	}
	out := make([]string, n)
	for i := range out {
		out[i] = strconv.Itoa(i)
	}
	return out, nil
}

func (s *session) CurrentDatabase() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strconv.Itoa(s.db)
}

func (s *session) UseDatabase(ctx context.Context, name string) error {
	n, err := strconv.Atoi(name)
	if err != nil || n < 0 {
		return fmt.Errorf("db index must be a number, got %q", name)
	}
	opt := s.opt
	opt.DB = n
	cl := goredis.NewClient(&opt)
	if err := cl.Ping(ctx).Err(); err != nil {
		cl.Close()
		return fmt.Errorf("use db %d: %w", n, err)
	}
	s.mu.Lock()
	old := s.cl
	s.cl, s.db, s.marks = cl, n, nil
	s.mu.Unlock()
	return old.Close()
}

func (s *session) Schemas(context.Context) ([]string, error) { return nil, nil }
func (s *session) CurrentSchema() string                     { return "" }

func (s *session) Indexes(context.Context, db.ObjRef) ([]db.Index, error)          { return nil, nil }
func (s *session) Triggers(context.Context, db.ObjRef) ([]db.Trigger, error)       { return nil, nil }
func (s *session) ForeignKeys(context.Context, db.ObjRef) ([]db.ForeignKey, error) { return nil, nil }

func (s *session) PlanTable(_, _ *db.TableDef) ([]db.Statement, error) {
	return nil, errors.New("redis keys have no table structure to edit")
}

func (s *session) Objects(ctx context.Context, _ string) ([]db.Object, error) {
	cl := s.c()
	seen := map[string]bool{}
	var cursor uint64
	for len(seen) < maxKeys {
		keys, next, err := cl.Scan(ctx, cursor, "*", 1000).Result()
		if err != nil {
			return nil, fmt.Errorf("scan keys: %w", err)
		}
		for _, k := range keys {
			seen[k] = true
		}
		if cursor = next; cursor == 0 {
			break
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > maxKeys {
		keys = keys[:maxKeys]
	}
	out := make([]db.Object, 0, len(keys))
	for lo := 0; lo < len(keys); lo += 1000 {
		chunk := keys[lo:min(lo+1000, len(keys))]
		pipe := cl.Pipeline()
		types := make([]*goredis.StatusCmd, len(chunk))
		ttls := make([]*goredis.DurationCmd, len(chunk))
		for i, k := range chunk {
			types[i], ttls[i] = pipe.Type(ctx, k), pipe.PTTL(ctx, k)
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
			return nil, fmt.Errorf("key types: %w", err)
		}
		for i, k := range chunk {
			d := types[i].Val()
			if t := ttls[i].Val(); t > 0 {
				d += fmt.Sprintf(" ttl %ds", ceilSecs(t))
			}
			out = append(out, db.Object{ObjRef: db.ObjRef{Name: k, Kind: db.KindTable}, Detail: d})
		}
	}
	return out, nil
}

func ceilSecs(d time.Duration) int64 { return int64((d + time.Second - 1) / time.Second) }

func (s *session) typeOf(ctx context.Context, key string) (string, error) {
	t, err := s.c().Type(ctx, key).Result()
	if err != nil {
		return "", fmt.Errorf("type of %q: %w", key, err)
	}
	if t == "none" {
		return "", fmt.Errorf("key %q does not exist", key)
	}
	return t, nil
}

// colNames are the synthesized columns per key type; the first is the row identity.
func colNames(typ string) ([]string, error) {
	switch typ {
	case "string", "ReJSON-RL":
		return []string{"value"}, nil
	case "hash":
		return []string{"field", "value"}, nil
	case "list":
		return []string{"index", "value"}, nil
	case "set":
		return []string{"member"}, nil
	case "zset":
		return []string{"member", "score"}, nil
	case "stream":
		return []string{"id", "fields"}, nil
	}
	return nil, fmt.Errorf("unsupported redis type %q", typ)
}

var colTypes = map[string]string{"index": "int", "score": "double"}

func (s *session) Columns(ctx context.Context, ref db.ObjRef) ([]db.Column, error) {
	typ, err := s.typeOf(ctx, ref.Name)
	if err != nil {
		return nil, err
	}
	names, err := colNames(typ)
	if err != nil {
		return nil, err
	}
	cols := make([]db.Column, len(names))
	for i, n := range names {
		t := colTypes[n]
		if t == "" {
			t = "string"
		}
		cols[i] = db.Column{Name: n, OrigName: n, Type: t, PK: i == 0}
	}
	return cols, nil
}

func (s *session) DDL(ctx context.Context, ref db.ObjRef) (string, error) {
	cl := s.c()
	typ, err := s.typeOf(ctx, ref.Name)
	if err != nil {
		return "", err
	}
	out := "TYPE " + typ
	ttl := int64(-1)
	if d, err := cl.PTTL(ctx, ref.Name).Result(); err == nil && d > 0 {
		ttl = ceilSecs(d)
	}
	out += fmt.Sprintf("\nTTL %d", ttl)
	if enc, err := cl.ObjectEncoding(ctx, ref.Name).Result(); err == nil {
		out += "\nENCODING " + enc
	}
	if m, err := cl.MemoryUsage(ctx, ref.Name).Result(); err == nil {
		out += fmt.Sprintf("\nMEMORY %d bytes", m)
	}
	return out, nil
}

func (s *session) Script(ctx context.Context, kind db.ScriptKind, ref db.ObjRef) (string, error) {
	key := ref.Name
	switch kind {
	case db.ScriptDrop, db.ScriptTruncate:
		return formatCommand("DEL", key), nil
	case db.ScriptCreate:
		return s.createScript(ctx, key)
	case db.ScriptSelectTop:
		typ, err := s.typeOf(ctx, key)
		if err != nil {
			return "", err
		}
		switch typ {
		case "hash":
			return formatCommand("HSCAN", key, "0", "COUNT", "100"), nil
		case "list":
			return formatCommand("LRANGE", key, "0", "99"), nil
		case "set":
			return formatCommand("SSCAN", key, "0", "COUNT", "100"), nil
		case "zset":
			return formatCommand("ZRANGE", key, "0", "99", "WITHSCORES"), nil
		case "stream":
			return formatCommand("XRANGE", key, "-", "+", "COUNT", "100"), nil
		case "ReJSON-RL":
			return formatCommand("JSON.GET", key), nil
		}
		return formatCommand("GET", key), nil
	}
	return "", fmt.Errorf("unknown script kind %q", kind)
}

// createScript renders the commands that would recreate the key, one per line.
func (s *session) createScript(ctx context.Context, key string) (string, error) {
	typ, err := s.typeOf(ctx, key)
	if err != nil {
		return "", err
	}
	rows, capped, err := s.loadAll(ctx, key, typ)
	if err != nil {
		return "", err
	}
	if capped {
		return "", fmt.Errorf("%q has more than %d elements: too large to script", key, maxLoad)
	}
	var lines []string
	add := func(args ...string) { lines = append(lines, formatCommand(args...)) }
	cmd := func(name string, f func(r []any) []string) {
		args := []string{name, key}
		for _, r := range rows {
			args = append(args, f(r)...)
		}
		if len(rows) > 0 {
			add(args...)
		}
	}
	switch typ {
	case "string":
		add("SET", key, str(rows[0][0]))
	case "ReJSON-RL":
		add("JSON.SET", key, "$", str(rows[0][0]))
	case "hash":
		cmd("HSET", func(r []any) []string { return []string{str(r[0]), str(r[1])} })
	case "list":
		cmd("RPUSH", func(r []any) []string { return []string{str(r[1])} })
	case "set":
		cmd("SADD", func(r []any) []string { return []string{str(r[0])} })
	case "zset":
		cmd("ZADD", func(r []any) []string { return []string{str(r[1]), str(r[0])} })
	case "stream":
		for _, r := range rows {
			m := map[string]string{}
			if err := jsonUnmarshal(str(r[1]), &m); err != nil {
				return "", err
			}
			args := []string{"XADD", key, str(r[0])}
			names := make([]string, 0, len(m))
			for k := range m {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				args = append(args, k, m[k])
			}
			add(args...)
		}
	}
	if d, err := s.c().PTTL(ctx, key).Result(); err == nil && d > 0 {
		add("EXPIRE", key, strconv.FormatInt(ceilSecs(d), 10))
	}
	return strings.Join(lines, "\n"), nil
}

// Processes implements db.ProcessLister via CLIENT LIST.
func (s *session) Processes(ctx context.Context) ([]db.ProcessInfo, error) {
	txt, err := s.c().ClientList(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("client list: %w", err)
	}
	return parseClientList(txt), nil
}

func parseClientList(txt string) []db.ProcessInfo {
	var out []db.ProcessInfo
	for _, line := range strings.Split(txt, "\n") {
		f := map[string]string{}
		for _, kv := range strings.Fields(line) {
			if k, v, ok := strings.Cut(kv, "="); ok {
				f[k] = v
			}
		}
		if f["id"] == "" {
			continue
		}
		age, _ := strconv.Atoi(f["age"])
		state := f["addr"]
		if f["name"] != "" {
			state += " " + f["name"]
		}
		out = append(out, db.ProcessInfo{ID: f["id"], User: f["user"], DB: f["db"], State: state,
			Elapsed: time.Duration(age) * time.Second, Query: f["cmd"]})
	}
	return out
}

func (s *session) Kill(ctx context.Context, id string) error {
	n, err := s.c().Do(ctx, "CLIENT", "KILL", "ID", id).Int64()
	if err != nil {
		return fmt.Errorf("kill client %s: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("kill client %s: no such client", id)
	}
	return nil
}
