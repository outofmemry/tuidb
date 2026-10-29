// Package mysql is the MySQL / MariaDB driver (go-sql-driver/mysql through database/sql).
package mysql

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gomysql "github.com/go-sql-driver/mysql"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/db/iam"
)

func init() { db.Register(drv{}) }

// drv is the MySQL driver; its zero value is "mysql". New builds a sibling that speaks the same
// protocol under another registered name (MariaDB), with its capabilities adjusted by adjust.
type drv struct {
	name   string
	adjust func(*db.Capabilities)
}

// New returns a driver registered under name that reuses this package's session code.
func New(name string, adjust func(*db.Capabilities)) db.Driver { return drv{name, adjust} }

func (d drv) Name() string {
	if d.name == "" {
		return "mysql"
	}
	return d.name
}

var caps = db.Capabilities{
	Indexes: true, Triggers: true, ForeignKeys: true, Views: true, Functions: true,
	ReorderColumns: true, Transactions: true, ProcessList: true, Users: true,
	Editor: "sql", Dialect: "mysql", DatabaseNoun: "database",
}

type session struct {
	conn config.Connection
	dial db.DialFunc
	caps db.Capabilities

	mu     sync.RWMutex
	pool   *sql.DB
	dbname string
	cols   map[string][]string // column names per table, for Any-column filters
	// MariaDB system-versioned tables (seen by Objects and Columns), which cannot be TRUNCATEd and
	// refuse ALTER unless system_versioning_alter_history=KEEP.
	versioned map[string]bool

	info  string
	maria bool

	pinned bool // set by Pin: the one connection holds a transaction, so it is never cancelled or silently replaced
}

func (d drv) Open(ctx context.Context, conn config.Connection, dial db.DialFunc) (db.Session, error) {
	s := &session{conn: conn, dial: dial, caps: caps, cols: map[string][]string{}, versioned: map[string]bool{}}
	if d.adjust != nil {
		d.adjust(&s.caps)
	}
	pool, err := s.openPool(ctx, conn.Database)
	if err != nil {
		return nil, err
	}
	s.pool = pool
	var version string
	var cur sql.NullString
	if err := pool.QueryRowContext(ctx, "SELECT VERSION(), DATABASE()").Scan(&version, &cur); err != nil {
		pool.Close()
		return nil, fmt.Errorf("read server version: %w", err)
	}
	s.info, s.maria = serverInfo(version)
	s.dbname = cur.String
	return s, nil
}

func serverInfo(version string) (string, bool) {
	if i := strings.Index(version, "-MariaDB"); i >= 0 {
		return "MariaDB " + version[:i], true
	}
	return "MySQL " + version, false
}

func (s *session) engine() string {
	if s.maria {
		return "MariaDB"
	}
	return "MySQL"
}

// bootConnector runs the Bootstrap statements on every new physical connection.
type bootConnector struct {
	driver.Connector
	stmts []string
}

func (b bootConnector) Connect(ctx context.Context) (driver.Conn, error) {
	c, err := b.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	ex, ok := c.(driver.ExecerContext)
	if !ok {
		c.Close()
		return nil, errors.New("mysql connection cannot run bootstrap statements")
	}
	for _, st := range b.stmts {
		if _, err := ex.ExecContext(ctx, st, nil); err != nil {
			c.Close()
			return nil, fmt.Errorf("bootstrap (%s): %w", st, err)
		}
	}
	return c, nil
}

// onceConnector allows one physical connection: database/sql would otherwise quietly open a new one
// after the pinned connection dropped, and the next statement would run outside the lost transaction.
type onceConnector struct {
	driver.Connector
	n atomic.Int32
}

func (o *onceConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if o.n.Add(1) > 1 {
		return nil, errors.New("the pinned connection was lost (its transaction is gone); close and pin again")
	}
	return o.Connector.Connect(ctx)
}

// iamConnector signs an RDS IAM token into the password of each new connection.
type iamConnector struct {
	cfg  *gomysql.Config
	conn config.Connection
}

func (i iamConnector) Connect(ctx context.Context) (driver.Conn, error) {
	tok, err := iam.Token(ctx, i.conn)
	if err != nil {
		return nil, err
	}
	cfg := i.cfg.Clone()
	cfg.Passwd = tok
	c, err := gomysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	return c.Connect(ctx)
}

func (i iamConnector) Driver() driver.Driver { return gomysql.MySQLDriver{} }

func (s *session) openPool(ctx context.Context, dbname string) (*sql.DB, error) {
	cfg := gomysql.NewConfig()
	cfg.User, cfg.Passwd = s.conn.User, s.conn.Password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(s.conn.Host, strconv.Itoa(s.conn.EffectivePort()))
	cfg.DBName = dbname
	cfg.ParseTime, cfg.Loc = true, time.UTC
	cfg.Timeout = 10 * time.Second
	cfg.DialFunc = s.dial
	// Client-side placeholder interpolation: one round trip per query instead of prepare + execute.
	// The driver escapes with the connection's charset and NO_BACKSLASH_ESCAPES mode.
	cfg.InterpolateParams = true
	if s.conn.Encoding != "" {
		cfg.Params = map[string]string{"charset": s.conn.Encoding}
	}
	var err error
	tc := s.conn
	if tc.IAMAuth {
		switch tc.SSLMode {
		case "":
			tc.SSLMode = "require" // RDS accepts IAM tokens over TLS only
		case "disable", "prefer":
			return nil, fmt.Errorf("iam_auth needs TLS: ssl_mode %q would send the IAM token unencrypted (use require, verify-ca or verify-full)", tc.SSLMode)
		}
	}
	if cfg.TLS, cfg.AllowFallbackToPlaintext, err = tlsConfig(tc); err != nil {
		return nil, err
	}
	c, err := gomysql.NewConnector(cfg)
	if err != nil {
		return nil, fmt.Errorf("mysql config: %w", err)
	}
	var base driver.Connector = c
	if s.conn.IAMAuth { // a fresh token for every new physical connection: they expire after 15 minutes
		cfg.AllowCleartextPasswords = true
		base = iamConnector{cfg, s.conn}
	}
	if s.pinned {
		base = &onceConnector{Connector: base}
	}
	boot := s.conn.Bootstrap
	if s.conn.ReadOnly { // the server refuses writes too, not only the client keyword check
		boot = append([]string{"SET SESSION TRANSACTION READ ONLY"}, boot...)
	}
	pool := sql.OpenDB(bootConnector{base, boot})
	pool.SetMaxOpenConns(8)
	pool.SetMaxIdleConns(8) // the default of 2 would close and reopen connections under the UI's parallel loads
	pool.SetConnMaxIdleTime(5 * time.Minute)
	if err := pool.PingContext(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to %s: %w", cfg.Addr, err)
	}
	return pool, nil
}

// tlsConfig maps ssl_mode to a client TLS config; the bool allows falling back to plaintext (prefer).
func tlsConfig(c config.Connection) (*tls.Config, bool, error) {
	mode := c.SSLMode
	if mode == "" || mode == "disable" {
		return nil, false, nil
	}
	t := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	if c.SSLCA != "" {
		pem, err := os.ReadFile(c.SSLCA)
		if err != nil {
			return nil, false, fmt.Errorf("ssl_ca: %w", err)
		}
		t.RootCAs = x509.NewCertPool()
		if !t.RootCAs.AppendCertsFromPEM(pem) {
			return nil, false, fmt.Errorf("ssl_ca %s: no certificates found", c.SSLCA)
		}
	}
	if c.SSLCert != "" || c.SSLKey != "" {
		pair, err := tls.LoadX509KeyPair(c.SSLCert, c.SSLKey)
		if err != nil {
			return nil, false, fmt.Errorf("ssl_cert/ssl_key: %w", err)
		}
		t.Certificates = []tls.Certificate{pair}
	}
	switch mode {
	case "prefer":
		t.InsecureSkipVerify = true
		return t, true, nil
	case "require":
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
		return nil, false, fmt.Errorf("unsupported ssl_mode %q for mysql (want disable|prefer|require|verify-ca|verify-full|verify-identity)", mode)
	}
	return t, false, nil
}

func (s *session) Caps() db.Capabilities { return s.caps }
func (s *session) ServerInfo() string    { return s.info }
func (s *session) Close() error          { return s.db().Close() }
func (s *session) Ping(ctx context.Context) error {
	return s.db().PingContext(ctx)
}

func (s *session) db() *sql.DB {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pool
}

func (s *session) CurrentDatabase() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dbname
}

func (s *session) Schemas(context.Context) ([]string, error) { return nil, nil }
func (s *session) CurrentSchema() string                     { return "" }

// UseDatabase reopens the pool: USE would only change one pooled connection.
func (s *session) UseDatabase(ctx context.Context, name string) error {
	pool, err := s.openPool(ctx, name)
	if err != nil {
		return err
	}
	s.mu.Lock()
	old := s.pool
	s.pool, s.dbname, s.cols, s.versioned = pool, name, map[string][]string{}, map[string]bool{}
	s.mu.Unlock()
	old.Close()
	return nil
}

// noteVersioned records whether schema.name is a system-versioned table.
func (s *session) noteVersioned(schema, name string, v bool) {
	s.mu.Lock()
	if s.versioned == nil {
		s.versioned = map[string]bool{}
	}
	s.versioned[schema+"."+name] = v
	s.mu.Unlock()
}

func (s *session) isVersioned(schema, name string) bool {
	if schema == "" {
		schema = s.CurrentDatabase()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.versioned[schema+"."+name]
}

func (s *session) Plan(changes []db.Change) ([]db.Statement, error) {
	return db.PlanSQL(db.MySQL, changes)
}

// convertArg turns the UI's special cell values into driver values.
func convertArg(a any) any {
	switch a {
	case db.Null:
		return nil
	case db.Empty:
		return ""
	}
	if e, ok := a.(db.Expr); ok {
		return string(e)
	}
	return a
}

// isDDL: MySQL commits implicitly around these, so a transaction cannot undo them.
func isDDL(text string) bool {
	f := strings.Fields(text)
	if len(f) > 2 && strings.EqualFold(f[0], "SET") && strings.EqualFold(f[1], "STATEMENT") { // MariaDB: SET STATEMENT v=x FOR <stmt>
		for i, w := range f {
			if strings.EqualFold(w, "FOR") {
				f = f[i+1:]
				break
			}
		}
	}
	if len(f) == 0 {
		return false
	}
	switch strings.ToUpper(f[0]) {
	case "CREATE", "ALTER", "DROP", "RENAME", "TRUNCATE":
		return true
	}
	return false
}

var errReadOnly = errors.New("connection is read_only")

func (s *session) Apply(ctx context.Context, stmts []db.Statement) error {
	if s.conn.ReadOnly {
		for _, st := range stmts {
			if !st.ReadOnly && !db.IsReadOnly("mysql", st.Text) {
				return errReadOnly
			}
		}
	}
	for _, st := range stmts {
		if isDDL(st.Text) {
			return s.applyDDL(ctx, stmts)
		}
	}
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	for i, st := range stmts {
		args := make([]any, len(st.Args))
		for j, a := range st.Args {
			args[j] = convertArg(a)
		}
		if _, err := tx.ExecContext(ctx, st.Text, args...); err != nil {
			tx.Rollback()
			return fmt.Errorf("statement %d (%s): %w", i+1, st.Shown(), err)
		}
	}
	s.mu.Lock()
	s.cols = map[string][]string{} // DDL may have changed column lists
	s.mu.Unlock()
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// applyDDL runs a batch holding DDL one statement at a time without a transaction (it could not
// roll DDL back anyway), stops at the first error and says what was already applied.
func (s *session) applyDDL(ctx context.Context, stmts []db.Statement) error {
	defer func() {
		s.mu.Lock()
		s.cols = map[string][]string{} // DDL may have changed column lists
		s.mu.Unlock()
	}()
	for i, st := range stmts {
		args := make([]any, len(st.Args))
		for j, a := range st.Args {
			args[j] = convertArg(a)
		}
		if _, err := s.db().ExecContext(ctx, st.Text, args...); err != nil {
			return ddlFailure(s.engine(), i, len(stmts), st.Shown(), err)
		}
	}
	return nil
}

// ddlFailure names the failed statement and says that the ones before it are committed for good.
func ddlFailure(engine string, i, n int, shown string, err error) error {
	return fmt.Errorf("statement %d (%s): %w (%d of %d statements were already applied and stay applied: %s cannot roll back DDL)",
		i+1, shown, err, i, n, engine)
}

var useRe = regexp.MustCompile("(?is)^\\s*use\\s+(?:`([^`]+)`|([^\\s;`]+))\\s*;?\\s*$")

func (s *session) Exec(ctx context.Context, stmt string, limit int) (*db.Result, error) {
	if s.conn.ReadOnly && !db.IsReadOnly("mysql", stmt) && !useRe.MatchString(stmt) {
		return nil, errReadOnly
	}
	start := time.Now()
	if m := useRe.FindStringSubmatch(stmt); m != nil {
		if err := s.UseDatabase(ctx, m[1]+m[2]); err != nil {
			return nil, err
		}
		return &db.Result{Message: "Database changed", Duration: time.Since(start)}, nil
	}
	res, err := s.query(ctx, stmt, nil, limit)
	if err != nil {
		return nil, err
	}
	res.Duration = time.Since(start)
	return res, nil
}

// query runs one statement on a dedicated connection and reads at most limit rows.
func (s *session) query(ctx context.Context, q string, args []any, limit int) (*db.Result, error) {
	c, err := s.db().Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("get connection: %w", err)
	}
	defer c.Close()
	qctx, cancel := context.WithCancel(ctx)
	defer cancel()
	rows, err := c.QueryContext(qctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("read columns: %w", err)
	}
	res := &db.Result{}
	if len(types) == 0 { // no row set: ask the same connection how many rows changed
		rows.Close()
		var n int64
		if err := c.QueryRowContext(ctx, "SELECT ROW_COUNT()").Scan(&n); err != nil {
			return nil, fmt.Errorf("row count: %w", err)
		}
		if n < 0 {
			n = 0
		}
		res.Affected, res.Message = n, affectedMessage(n)
		return res, nil
	}
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = t.DatabaseTypeName()
		res.Columns = append(res.Columns, db.ResultColumn{Name: t.Name(), Type: names[i]})
	}
	res.HasRows = true
	vals := make([]any, len(types))
	ptrs := make([]any, len(types))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if limit > 0 && len(res.Rows) == limit {
			res.Truncated = true
			if !s.pinned { // shortcut: a pinned connection is drained (rows.Close), not cancelled, or its transaction would die
				cancel() // closing the connection beats draining the rest of a huge result set
			}
			return res, nil
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		row := make([]any, len(vals))
		for i, v := range vals {
			row[i] = normalize(v, names[i])
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read rows: %w", err)
	}
	return res, nil
}

func affectedMessage(n int64) string {
	if n == 1 {
		return "1 row affected"
	}
	return fmt.Sprintf("%d rows affected", n)
}

// normalize converts driver values to the shapes documented on db.Result.
// The text protocol hands almost everything over as []byte, so the column type decides.
func normalize(v any, typ string) any {
	switch x := v.(type) {
	case []byte:
		return fromBytes(x, typ)
	case time.Time:
		if x.IsZero() { // the driver maps 0000-00-00 to Go's zero time
			if typ == "DATE" {
				return "0000-00-00"
			}
			return "0000-00-00 00:00:00"
		}
	case int8:
		return int64(x)
	case int16:
		return int64(x)
	case int32:
		return int64(x)
	case int:
		return int64(x)
	case uint8:
		return uint64(x)
	case uint16:
		return uint64(x)
	case uint32:
		return uint64(x)
	case uint:
		return uint64(x)
	case float32:
		return float64(x)
	}
	return v
}

func fromBytes(b []byte, typ string) any {
	switch typ {
	case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "BIGINT", "YEAR":
		if n, err := strconv.ParseInt(string(b), 10, 64); err == nil {
			return n
		}
	case "UNSIGNED TINYINT", "UNSIGNED SMALLINT", "UNSIGNED MEDIUMINT", "UNSIGNED INT", "UNSIGNED BIGINT":
		if n, err := strconv.ParseUint(string(b), 10, 64); err == nil {
			return n
		}
	case "FLOAT", "DOUBLE":
		if f, err := strconv.ParseFloat(string(b), 64); err == nil {
			return f
		}
	case "BIT":
		if len(b) <= 8 {
			var buf [8]byte
			copy(buf[8-len(b):], b)
			return binary.BigEndian.Uint64(buf[:])
		}
	case "BLOB", "TINYBLOB", "MEDIUMBLOB", "LONGBLOB", "BINARY", "VARBINARY", "GEOMETRY", "VECTOR":
		return b
	}
	return string(b)
}

func (s *session) schemaOf(ref db.ObjRef) string {
	if ref.Schema != "" {
		return ref.Schema
	}
	return s.CurrentDatabase()
}

// searchable: the "Any column" filter casts every listed column to text for every row; binary and
// spatial columns are huge and meaningless as text, so they are left out of that search.
func searchable(typ string) bool {
	t := strings.ToLower(typ)
	for _, p := range []string{"blob", "tinyblob", "mediumblob", "longblob", "binary", "varbinary", "geometry", "point", "linestring", "polygon"} {
		if strings.HasPrefix(t, p) {
			return false
		}
	}
	return true
}

func (s *session) colNames(ctx context.Context, ref db.ObjRef) ([]string, error) {
	key := ref.Schema + "." + ref.Name
	s.mu.RLock()
	names, ok := s.cols[key]
	s.mu.RUnlock()
	if ok {
		return names, nil
	}
	cols, err := s.loadColumns(ctx, ref)
	if err != nil {
		return nil, err
	}
	for _, c := range cols {
		if searchable(c.Type) {
			names = append(names, c.Name)
		}
	}
	s.mu.Lock()
	s.cols[key] = names
	s.mu.Unlock()
	return names, nil
}

// pkey returns the primary key columns in key order (cached with the filter columns, so DDL clears it).
func (s *session) pkey(ctx context.Context, ref db.ObjRef) []string {
	key := "pk\x00" + ref.Schema + "." + ref.Name
	s.mu.RLock()
	pk, ok := s.cols[key]
	s.mu.RUnlock()
	if ok {
		return pk
	}
	pk = []string{}
	rows, err := s.db().QueryContext(ctx, `SELECT COLUMN_NAME FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND INDEX_NAME = 'PRIMARY' ORDER BY SEQ_IN_INDEX`, s.schemaOf(ref), ref.Name)
	if err != nil {
		return nil // no key known: the browse stays unordered
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if rows.Scan(&n) != nil {
			return nil
		}
		pk = append(pk, n)
	}
	if rows.Err() != nil {
		return nil
	}
	s.mu.Lock()
	s.cols[key] = pk
	s.mu.Unlock()
	return pk
}

func (s *session) Rows(ctx context.Context, q db.RowQuery) (*db.Result, error) {
	var cols []string
	if len(q.Filters.Active()) > 0 {
		var err error
		if cols, err = s.colNames(ctx, q.ObjRef); err != nil {
			return nil, err
		}
	}
	if len(q.Sort) == 0 && q.Key == nil {
		q.Key = s.pkey(ctx, q.ObjRef)
	}
	text, args, err := db.BuildSelect(db.MySQL, q, cols)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	res, err := s.query(ctx, text, args, 0)
	if err != nil {
		return nil, err
	}
	res.Duration = time.Since(start)
	return res, nil
}

func (s *session) Count(ctx context.Context, q db.RowQuery, estimate bool) (db.Count, error) {
	if estimate && len(q.Filters.Active()) == 0 {
		var n sql.NullInt64
		err := s.db().QueryRowContext(ctx, "SELECT TABLE_ROWS FROM information_schema.tables WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?",
			s.schemaOf(q.ObjRef), q.Name).Scan(&n)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return db.Count{}, fmt.Errorf("count estimate: %w", err)
		}
		// 0 or NULL (views, stale stats of a fresh table): fall through to the exact count.
		if err == nil && n.Valid && n.Int64 > 0 {
			return db.Count{N: n.Int64}, nil
		}
	}
	var cols []string
	if len(q.Filters.Active()) > 0 {
		var err error
		if cols, err = s.colNames(ctx, q.ObjRef); err != nil {
			return db.Count{}, err
		}
	}
	text, args, err := db.BuildCount(db.MySQL, q, cols)
	if err != nil {
		return db.Count{}, err
	}
	var n int64
	if err := s.db().QueryRowContext(ctx, text, args...).Scan(&n); err != nil {
		return db.Count{}, fmt.Errorf("count: %w", err)
	}
	return db.Count{N: n, Exact: true}, nil
}

func (s *session) Processes(ctx context.Context) ([]db.ProcessInfo, error) {
	rows, err := s.db().QueryContext(ctx, "SHOW FULL PROCESSLIST")
	if err != nil {
		return nil, fmt.Errorf("process list: %w", err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("process list: %w", err)
	}
	var out []db.ProcessInfo
	for rows.Next() {
		var id, secs int64
		var user, host, dbn, cmd, state, info sql.NullString
		dest := []any{&id, &user, &host, &dbn, &cmd, &secs, &state, &info}
		for len(dest) < len(cols) { // MariaDB appends a Progress column
			dest = append(dest, new(sql.RawBytes))
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("process list: %w", err)
		}
		st := state.String
		if st == "" {
			st = cmd.String
		}
		out = append(out, db.ProcessInfo{ID: strconv.FormatInt(id, 10), User: user.String, DB: dbn.String, State: st,
			Elapsed: time.Duration(secs) * time.Second, Query: info.String})
	}
	return out, rows.Err()
}

func (s *session) Kill(ctx context.Context, id string) error {
	if s.conn.ReadOnly {
		return errReadOnly
	}
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return fmt.Errorf("bad process id %q", id)
	}
	if _, err := s.db().ExecContext(ctx, "KILL QUERY "+strconv.FormatUint(n, 10)); err != nil {
		return fmt.Errorf("kill %d: %w", n, err)
	}
	return nil
}

var _ db.ProcessLister = (*session)(nil)
