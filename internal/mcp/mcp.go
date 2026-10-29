// Package mcp exposes the configured connections to MCP clients (stdio or streamable HTTP).
package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"webtuidb/internal/board"
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/store"
)

const (
	logSize = 200

	sessionTimeout      = 30 * time.Minute // idle HTTP sessions are closed after this
	maxSessionsPerToken = 20
)

// readOnlyBegin starts a server-enforced read-only transaction, for the dialects that have one.
var readOnlyBegin = map[string]string{"postgres": "BEGIN READ ONLY", "mysql": "START TRANSACTION READ ONLY", "mariadb": "START TRANSACTION READ ONLY"}

// serverReadOnly are the dialects whose driver refuses writes on a read_only connection at the server. Others
// (ClickHouse, ...) only have a keyword filter, which cannot stop file()/url() table functions.
var serverReadOnly = map[string]bool{"postgres": true, "mysql": true, "mariadb": true}

type Entry struct {
	Time       time.Time
	Token      string
	Tool       string
	Connection string
	SQL        string
	Err        string
}

type open struct {
	sess   db.Session
	closer func()
	use    sync.RWMutex // held shared while a tool runs on sess; disconnect takes it exclusively before closing
	closed bool         // set under use.Lock once disconnected; a call that took RLock afterwards must not use sess
}

type Server struct {
	cfg   config.Config
	conns []config.Connection
	// connect is replaced in tests.
	connect func(ctx context.Context, c config.Connection) (db.Session, func(), error)

	mu   sync.Mutex
	open map[string]*open
	logs []Entry

	// Approver is the UI's approval sheet for non-read-only statements when approval_for_unsafe is on.
	// It blocks until the user answers; nil means every unsafe statement is refused.
	Approver func(ctx context.Context, a Approval) (bool, error)

	// Optional data sources for the saved-query, history and board tools; unset = the tool says "not configured".
	Queries *store.Queries
	Hist    *store.History
	Boards  bool
}

// Approval is one unsafe statement waiting for the user.
type Approval struct {
	Token, Connection, SQL string
}

func New(cfg config.Config, conns []config.Connection) *Server {
	return &Server{cfg: cfg, conns: conns, connect: dial, open: map[string]*open{}}
}

// dial mirrors ui.DefaultConnect (which this package cannot import: the UIs import us).
func dial(ctx context.Context, c config.Connection) (db.Session, func(), error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pw, err := c.ResolvePassword(ctx)
	if err != nil {
		return nil, nil, err
	}
	c.Password = pw
	drv, err := db.Lookup(c.Driver)
	if err != nil {
		return nil, nil, err
	}
	d, closer, err := db.DialFor(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	s, err := drv.Open(ctx, c, d)
	if err != nil {
		closer()
		return nil, nil, fmt.Errorf("connect %s: %w", c.Name, err)
	}
	return s, closer, nil
}

// Log returns the last 200 tool calls, oldest first.
func (s *Server) Log() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.logs)
}

func (s *Server) record(e Entry) {
	e.Time = time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.logs = append(s.logs, e); len(s.logs) > logSize {
		s.logs = s.logs[1:]
	}
}

// ServeStdio serves the first configured token's scope (no auth when none: stdio is the local user).
func (s *Server) ServeStdio(ctx context.Context) error {
	var tok *config.MCPToken
	if len(s.cfg.MCP.Tokens) > 0 {
		tok = &s.cfg.MCP.Tokens[0]
	}
	return s.build(tok).Run(ctx, stdioTransport{in: os.Stdin, out: os.Stdout})
}

// Handler serves streamable HTTP; the Bearer token picks the scope. The verifier returns the token
// name as UserID so the SDK binds each session to the token that created it.
func (s *Server) Handler() http.Handler {
	var mu sync.Mutex
	live := map[string][]*sdk.Server{} // token name -> servers, one per session
	h := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		t := s.auth(r)
		key := ""
		if t != nil {
			key = t.Name
		}
		mu.Lock()
		defer mu.Unlock()
		keep, n := live[key][:0], 0
		for _, x := range live[key] {
			c := 0
			for range x.Sessions() {
				c++
			}
			if c > 0 { // a server whose sessions all closed is dropped
				keep, n = append(keep, x), n+c
			}
		}
		live[key] = keep
		if n >= maxSessionsPerToken {
			return nil
		}
		srv := s.build(t)
		live[key] = append(keep, srv)
		return srv
	}, &sdk.StreamableHTTPOptions{SessionTimeout: sessionTimeout})
	verify := func(_ context.Context, _ string, r *http.Request) (*auth.TokenInfo, error) {
		t := s.auth(r)
		if t == nil {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{UserID: t.Name, Expiration: time.Now().Add(time.Hour)}, nil
	}
	return auth.RequireBearerToken(verify, nil)(h)
}

func (s *Server) auth(r *http.Request) *config.MCPToken {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || got == "" {
		return nil
	}
	for i := range s.cfg.MCP.Tokens {
		t := &s.cfg.MCP.Tokens[i]
		if t.Value != "" && subtle.ConstantTimeCompare([]byte(got), []byte(t.Value)) == 1 {
			return t
		}
	}
	return nil
}

type none struct{}

type connIn struct {
	Connection string `json:"connection" jsonschema:"saved connection name"`
}

type sqlIn struct {
	Connection string `json:"connection" jsonschema:"name of an open connection"`
	SQL        string `json:"sql" jsonschema:"the statement(s) to run"`
}

func (s *Server) build(tok *config.MCPToken) *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{Name: "webtuidb", Version: "0.1"}, nil)
	name, scope := "", "full"
	if tok != nil {
		name, scope = tok.Name, tok.Scope
	}
	// run logs the call and wraps its text/error result.
	run := func(tool, conn, sql string, f func() (any, error)) (*sdk.CallToolResult, none, error) {
		v, err := f()
		e := Entry{Token: name, Tool: tool, Connection: conn, SQL: redact(sql)}
		if err != nil {
			e.Err = redact(err.Error())
		}
		s.record(e)
		if err != nil {
			return nil, none{}, err
		}
		b, _ := json.Marshal(v)
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}, none{}, nil
	}
	tool := func(n, d string) *sdk.Tool { return &sdk.Tool{Name: n, Description: d} }

	sdk.AddTool(srv, tool("list_connections", "List the saved connections (name and driver)."),
		func(ctx context.Context, _ *sdk.CallToolRequest, _ none) (*sdk.CallToolResult, none, error) {
			return run("list_connections", "", "", func() (any, error) {
				out := []map[string]string{}
				for _, c := range s.conns {
					if allowed(tok, c.Name) {
						out = append(out, map[string]string{"name": c.Name, "driver": c.Driver})
					}
				}
				return out, nil
			})
		})
	sdk.AddTool(srv, tool("list_open_connections", "List the connections currently open."),
		func(ctx context.Context, _ *sdk.CallToolRequest, _ none) (*sdk.CallToolResult, none, error) {
			return run("list_open_connections", "", "", func() (any, error) {
				s.mu.Lock()
				defer s.mu.Unlock()
				out := []string{}
				for n := range s.open {
					if allowed(tok, n) {
						out = append(out, n)
					}
				}
				slices.Sort(out)
				return out, nil
			})
		})
	sdk.AddTool(srv, tool("open_connection", "Open a saved connection by name."),
		func(ctx context.Context, _ *sdk.CallToolRequest, in connIn) (*sdk.CallToolResult, none, error) {
			return run("open_connection", in.Connection, "", func() (any, error) {
				if !allowed(tok, in.Connection) {
					return nil, fmt.Errorf("token %q may not open %q", name, in.Connection)
				}
				i := slices.IndexFunc(s.conns, func(c config.Connection) bool { return c.Name == in.Connection })
				if i < 0 {
					return nil, fmt.Errorf("no saved connection named %q", in.Connection)
				}
				s.mu.Lock()
				_, have := s.open[in.Connection]
				s.mu.Unlock()
				if have {
					return "already open", nil
				}
				sess, closer, err := s.connect(ctx, s.conns[i])
				if err != nil {
					return nil, err
				}
				s.mu.Lock()
				defer s.mu.Unlock()
				if _, dup := s.open[in.Connection]; dup { // lost a race: keep the first
					sess.Close()
					closer()
					return "already open", nil
				}
				s.open[in.Connection] = &open{sess: sess, closer: closer}
				return "opened " + sess.ServerInfo(), nil
			})
		})
	sdk.AddTool(srv, tool("disconnect", "Close an open connection."),
		func(ctx context.Context, _ *sdk.CallToolRequest, in connIn) (*sdk.CallToolResult, none, error) {
			return run("disconnect", in.Connection, "", func() (any, error) {
				if scope == "read-only" {
					return nil, fmt.Errorf("token %q is read-only", name)
				}
				if !allowed(tok, in.Connection) {
					return nil, fmt.Errorf("token %q may not use %q", name, in.Connection)
				}
				s.mu.Lock()
				o := s.open[in.Connection]
				delete(s.open, in.Connection)
				s.mu.Unlock()
				if o == nil {
					return nil, fmt.Errorf("%q is not open", in.Connection)
				}
				o.use.Lock() // waits for tools still running on it
				defer o.use.Unlock()
				o.closed = true
				o.sess.Close()
				o.closer()
				return "closed", nil
			})
		})
	exec := func(tool string, write bool) func(context.Context, *sdk.CallToolRequest, sqlIn) (*sdk.CallToolResult, none, error) {
		return func(ctx context.Context, _ *sdk.CallToolRequest, in sqlIn) (*sdk.CallToolResult, none, error) {
			return run(tool, in.Connection, in.SQL, func() (any, error) {
				if write && scope == "read-only" {
					return nil, fmt.Errorf("token %q is read-only", name)
				}
				if !allowed(tok, in.Connection) {
					return nil, fmt.Errorf("token %q may not use %q", name, in.Connection)
				}
				s.mu.Lock()
				o := s.open[in.Connection]
				s.mu.Unlock()
				if o == nil {
					return nil, fmt.Errorf("%q is not open; call open_connection first", in.Connection)
				}
				o.use.RLock()
				defer o.use.RUnlock()
				if o.closed {
					return nil, fmt.Errorf("%q is disconnected", in.Connection)
				}
				dialect := o.sess.Caps().Dialect
				stmts := db.Split(dialect, in.SQL)
				for _, st := range stmts {
					// shortcut: IsReadOnly is a keyword pre-filter; execute_read_query also runs inside a server-enforced
					// read-only transaction where the dialect and session support one (see below), otherwise only this filter guards it.
					ro := db.IsReadOnly(dialect, st.Text)
					if w := firstWord(dialect, st.Text); write && scope == "read-write" && (ddl[w] || w == "") {
						if w == "" {
							w = "an unclassifiable statement"
						}
						return nil, fmt.Errorf("token %q is read-write: %s is not allowed", name, w)
					}
					if !write && !ro {
						return nil, fmt.Errorf("not a read-only statement; use execute_query")
					}
					if !write {
						if fn := sideEffectFunc(dialect, st.Text); fn != "" {
							return nil, fmt.Errorf("%s is not allowed in a read-only query; use execute_query", fn)
						}
					}
					if write && !ro && s.cfg.MCP.ApprovalForUnsafe {
						if s.Approver == nil {
							return nil, fmt.Errorf("needs approval: %.60q is not read-only and approval_for_unsafe is on", st.Text)
						}
						ok, err := s.Approver(ctx, Approval{Token: name, Connection: in.Connection, SQL: st.Text})
						if err != nil {
							return nil, err
						}
						if !ok {
							return nil, fmt.Errorf("denied by the user: %.60q", st.Text)
						}
					}
				}
				if t := s.cfg.General.QueryTimeoutSecs; t > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, time.Duration(t)*time.Second)
					defer cancel()
				}
				runner := o.sess
				if !write {
					r, done, err := s.readOnlyRunner(ctx, o, in.Connection, dialect)
					if err != nil {
						return nil, err
					}
					defer done()
					runner = r
				}
				var last *db.Result
				for _, st := range stmts {
					r, err := runner.Exec(ctx, st.Text, s.cfg.General.RowLimit)
					if err != nil {
						return nil, err
					}
					last = r
				}
				if last == nil {
					return "no statements", nil
				}
				cols := make([]string, len(last.Columns))
				for i, c := range last.Columns {
					cols[i] = c.Name
				}
				return map[string]any{"columns": cols, "rows": last.Rows, "affected": last.Affected, "message": last.Message, "truncated": last.Truncated}, nil
			})
		}
	}
	sdk.AddTool(srv, tool("execute_read_query", "Run read-only statement(s) on an open connection."), exec("execute_read_query", false))
	sdk.AddTool(srv, tool("execute_query", "Run statement(s) on an open connection (needs a read-write or full token)."), exec("execute_query", true))
	s.addStoreTools(srv, tok, run)
	return srv
}

type nameIn struct {
	Name string `json:"name" jsonschema:"saved query path or board name"`
}

type histIn struct {
	Connection string `json:"connection" jsonschema:"saved connection name"`
	Limit      int    `json:"limit,omitempty" jsonschema:"newest N entries (default 50)"`
}

type boardIn struct {
	Name       string            `json:"name" jsonschema:"board name"`
	Connection string            `json:"connection,omitempty" jsonschema:"open connection to run on (default: the board's own)"`
	Vars       map[string]string `json:"vars,omitempty"`
}

var errNotConfigured = fmt.Errorf("not configured")

// addStoreTools registers the read-only saved query, history and board tools; history is withheld from read-only tokens.
func (s *Server) addStoreTools(srv *sdk.Server, tok *config.MCPToken, run func(tool, conn, sql string, f func() (any, error)) (*sdk.CallToolResult, none, error)) {
	name, scope := "", "full"
	if tok != nil {
		name, scope = tok.Name, tok.Scope
	}
	type res = *sdk.CallToolResult
	sdk.AddTool(srv, &sdk.Tool{Name: "list_saved_queries", Description: "List the saved queries (path, name, keywords)."},
		func(ctx context.Context, _ *sdk.CallToolRequest, _ none) (res, none, error) {
			return run("list_saved_queries", "", "", func() (any, error) {
				if s.Queries == nil {
					return nil, errNotConfigured
				}
				qs, err := s.Queries.List()
				out := []map[string]any{}
				for _, q := range qs {
					out = append(out, map[string]any{"path": q.Path, "name": q.Name, "keywords": q.Keywords})
				}
				return out, err
			})
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "get_saved_query", Description: "Return the SQL of a saved query (by path from list_saved_queries)."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in nameIn) (res, none, error) {
			return run("get_saved_query", "", "", func() (any, error) {
				if s.Queries == nil {
					return nil, errNotConfigured
				}
				q, err := s.Queries.Read(in.Name)
				if err != nil {
					return nil, err
				}
				return q.SQL, nil
			})
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "list_history", Description: "List the newest query history entries of a connection."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in histIn) (res, none, error) {
			return run("list_history", in.Connection, "", func() (any, error) {
				if s.Hist == nil {
					return nil, errNotConfigured
				}
				if !allowed(tok, in.Connection) {
					return nil, fmt.Errorf("token %q may not use %q", name, in.Connection)
				}
				if in.Limit <= 0 {
					in.Limit = 50
				}
				if scope == "read-only" { // history holds literals and row pre-images
					return nil, fmt.Errorf("token %q is read-only; history is not available to it", name)
				}
				es, err := s.Hist.List(in.Connection, "", in.Limit)
				for i := range es {
					es[i].SQL, es[i].Original = redact(es[i].SQL), ""
				}
				return es, err
			})
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "list_boards", Description: "List the metrics boards (name, connection, panel titles)."},
		func(ctx context.Context, _ *sdk.CallToolRequest, _ none) (res, none, error) {
			return run("list_boards", "", "", func() (any, error) {
				if !s.Boards {
					return nil, errNotConfigured
				}
				bs, err := board.List()
				out := []map[string]any{}
				for _, b := range bs {
					if b.Connection != "" && !allowed(tok, b.Connection) {
						continue
					}
					ps := []string{}
					for _, p := range b.Panels {
						ps = append(ps, p.Title)
					}
					out = append(out, map[string]any{"name": b.Name, "connection": b.Connection, "panels": ps})
				}
				return out, err
			})
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "run_board", Description: "Run a metrics board's read-only panels on an open connection."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in boardIn) (res, none, error) {
			return run("run_board", in.Connection, "", func() (any, error) {
				if !s.Boards {
					return nil, errNotConfigured
				}
				bs, err := board.List()
				if err != nil {
					return nil, err
				}
				i := slices.IndexFunc(bs, func(b board.Board) bool { return b.Name == in.Name && b.Err == nil })
				if i < 0 {
					return nil, fmt.Errorf("no board named %q", in.Name)
				}
				conn := in.Connection
				if conn == "" {
					conn = bs[i].Connection
				}
				if !allowed(tok, conn) {
					return nil, fmt.Errorf("token %q may not use %q", name, conn)
				}
				s.mu.Lock()
				o := s.open[conn]
				s.mu.Unlock()
				if o == nil {
					return nil, fmt.Errorf("%q is not open; call open_connection first", conn)
				}
				o.use.RLock()
				defer o.use.RUnlock()
				if o.closed {
					return nil, fmt.Errorf("%q is disconnected", conn)
				}
				dialect := o.sess.Caps().Dialect
				for _, p := range bs[i].Panels {
					if p.Kind == "button" || p.Kind == "import" {
						continue
					}
					if fn := sideEffectFunc(dialect, p.Query); fn != "" {
						return nil, fmt.Errorf("panel %q: %s is not allowed in a read-only query", p.Title, fn)
					}
				}
				runner, done, err := s.readOnlyRunner(ctx, o, conn, dialect)
				if err != nil {
					return nil, err
				}
				defer done()
				out := []map[string]any{}
				for _, r := range board.Run(ctx, runner, bs[i], in.Vars, s.cfg.General.RowLimit) {
					m := map[string]any{"title": r.Panel.Title, "kind": r.Panel.Kind}
					switch {
					case r.Err != nil:
						m["error"] = r.Err.Error()
					case r.Panel.Kind == "number":
						m["number"] = r.Number
					case r.Result != nil:
						m["columns"], m["rows"] = colNames(r.Result), r.Result.Rows
					}
					out = append(out, m)
				}
				return out, nil
			})
		})
}

func colNames(r *db.Result) []string {
	cols := make([]string, len(r.Columns))
	for i, c := range r.Columns {
		cols[i] = c.Name
	}
	return cols
}

func allowed(tok *config.MCPToken, conn string) bool {
	return tok == nil || len(tok.Connections) == 0 || slices.Contains(tok.Connections, conn)
}

// readOnlyRunner is the session read-only statements must run on (see ReadOnlyTx).
func (s *Server) readOnlyRunner(ctx context.Context, o *open, conn, dialect string) (db.Session, func(), error) {
	return ReadOnlyTx(ctx, o.sess, dialect, s.connReadOnly(conn))
}

// connReadOnly reports whether the saved connection is configured read_only (enforced on the server only for the dialects in serverReadOnly).
func (s *Server) connReadOnly(name string) bool {
	i := slices.IndexFunc(s.conns, func(c config.Connection) bool { return c.Name == name })
	return i >= 0 && s.conns[i].ReadOnly
}

// ddl are the leading keywords a read-write token may not run. DO/CALL/EXECUTE/EXEC/PREPARE hide arbitrary
// SQL in a block or string, so they count too.
var ddl = map[string]bool{"DROP": true, "ALTER": true, "CREATE": true, "TRUNCATE": true, "GRANT": true, "REVOKE": true, "RENAME": true,
	"DO": true, "CALL": true, "EXECUTE": true, "EXEC": true, "PREPARE": true}

// skipLead is whitespace and comments; "/*!" and "/*+" are not skipped because MySQL runs their content.
var skipLead = regexp.MustCompile(`^(?:\s+|--[^\n]*|/\*(?:[^!+](?s:.*?))?\*/)`)

// hashLead also skips "# ..." comments, which MySQL and ClickHouse allow.
var hashLead = regexp.MustCompile(`^#[^\n]*`)

var execLead = regexp.MustCompile(`^/\*!\d*`)

// firstWord is the upper-cased first keyword of sql, after whitespace and comments ("" when it is not a plain
// keyword, which the read-write gate treats as unsafe).
func firstWord(dialect, sql string) string {
	for {
		m := skipLead.FindString(sql)
		if m == "" && (dialect == "mysql" || dialect == "mariadb" || dialect == "clickhouse") {
			m = hashLead.FindString(sql)
		}
		if m == "" && (dialect == "mysql" || dialect == "mariadb") {
			m = execLead.FindString(sql) // the executable comment's marker, the body is the statement (NEW-2)
		}
		if m == "" {
			break
		}
		sql = sql[len(m):]
	}
	i := 0
	for i < len(sql) && (sql[i] >= 'a' && sql[i] <= 'z' || sql[i] >= 'A' && sql[i] <= 'Z') {
		i++
	}
	w := strings.ToUpper(sql[:i])
	if w == "EXPLAIN" { // EXPLAIN ANALYZE runs the statement it explains, so judge that one
		rest := sql[i:]
		for {
			rest = strings.TrimLeft(rest, " \t\r\n")
			j := 0
			for j < len(rest) && (rest[j] >= 'a' && rest[j] <= 'z' || rest[j] >= 'A' && rest[j] <= 'Z') {
				j++
			}
			if opt := strings.ToUpper(rest[:j]); opt == "ANALYZE" || opt == "ANALYSE" || opt == "VERBOSE" {
				rest = rest[j:]
				continue
			}
			if strings.HasPrefix(rest, "(") { // EXPLAIN (ANALYZE, ...) stmt
				if k := strings.IndexByte(rest, ')'); k > 0 {
					rest = rest[k+1:]
					continue
				}
				return ""
			}
			if j == 0 {
				return ""
			}
			return strings.ToUpper(rest[:j])
		}
	}
	return w
}

var strLit = regexp.MustCompile(`'(?:[^']|'')*'`)

// redact hides string literals (passwords in ALTER ROLE ... PASSWORD 'x') before they reach the log.
// shortcut: backslash escapes and $tag$ strings are not recognised; upgrade to the db scanner.
func redact(sql string) string { return strLit.ReplaceAllString(sql, "'…'") }

// sideEffectRe matches functions that change state or reach outside the database even inside a statement the
// keyword classifier calls read-only. A trailing identifier tail (dblink_exec, lo_put, ...) is part of the match,
// and a call needs the "(" so a column that happens to be named copy is fine. The query_to_xml and cursor_to_xml
// families run the SQL text they are given, which string concatenation can hide from this filter, so they are refused outright.
var sideEffectRe = regexp.MustCompile(`(?i)\b(dblink\w*|lo_\w+|pg_terminate_backend|pg_cancel_backend|pg_read_file|pg_read_binary_file|pg_ls_dir|pg_switch_wal|pg_create_restore_point|pg_reload_conf|pg_rotate_logfile|pg_advisory_\w+|query_to_xml\w*|cursor_to_xml\w*|setval|nextval|copy)\s*\(`)

// sideEffectByDialect adds each dialect's own file, network and lock primitives. LOAD_FILE and INTO OUTFILE pass
// START TRANSACTION READ ONLY; ClickHouse table functions read files and URLs.
var sideEffectByDialect = map[string]*regexp.Regexp{
	"mysql":      mysqlSideEffect,
	"mariadb":    mysqlSideEffect,
	"clickhouse": regexp.MustCompile(`(?i)\b(file|url|urlcluster|s3|s3cluster|remote|remotesecure|hdfs|hdfscluster|executable|azureblobstorage|gcs|mysql|postgresql|mongodb|odbc|jdbc|sqlite|input|cluster|clusterallreplicas|deltalake|iceberg|hudi)\s*\(`),
}

var mysqlSideEffect = regexp.MustCompile(`(?i)\b(load_file|get_lock|release_lock|release_all_locks|sleep|benchmark|sys_exec|sys_eval|into\s+(?:outfile|dumpfile)|load\s+data)\b`)

// copyStmtRe is COPY used as a statement keyword (COPY t TO PROGRAM ...), which has no "(" after it.
var copyStmtRe = regexp.MustCompile(`(?i)^\s*copy\b`)

// uescRe is a Postgres U&"..." identifier, whose \XXXX escapes can spell any function name past the filter.
var uescRe = regexp.MustCompile(`(?i)\bu&\s*"`)

// dollarTagRe is db's tag rule: digits are allowed after the first character ($a1$), or the closing tag is
// missed and the rest of the statement is blanked in the copy the deny-list sees.
var dollarTagRe = regexp.MustCompile(`^\$(?:[A-Za-z_\x80-\xff][A-Za-z0-9_\x80-\xff]*)?\$`)

// sideEffectFunc returns the first denied function or verb in stmt ("" = none). It matches on a copy of the
// statement with comments and string literals removed and quoted identifiers unquoted, so 'copy' in a literal or
// "-- copy this" does not trip it while "dblink_exec"(...) still does.
func sideEffectFunc(dialect, stmt string) string {
	if uescRe.MatchString(stmt) {
		return "a unicode-escaped identifier"
	}
	c := stripLiterals(dialect, stmt)
	if copyStmtRe.MatchString(c) {
		return "copy"
	}
	if m := sideEffectRe.FindStringSubmatch(c); m != nil {
		return strings.ToLower(m[1])
	}
	if re := sideEffectByDialect[dialect]; re != nil {
		if m := re.FindStringSubmatch(c); m != nil {
			return strings.ToLower(strings.Join(strings.Fields(m[1]), " "))
		}
	}
	return ""
}

// stripLiterals blanks comments and string literals and unquotes "ident" and `ident`.
func stripLiterals(dialect, sql string) string {
	var b strings.Builder
	my := dialect == "mysql" || dialect == "mariadb"
	for i := 0; i < len(sql); {
		c := sql[i]
		switch {
		case c == '-' && strings.HasPrefix(sql[i:], "--"), c == '#' && my:
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
		case my && strings.HasPrefix(sql[i:], "/*!"):
			// /*!50000 SELECT ... */ runs on MySQL: keep the body, drop only the marker and its version digits (NEW-2)
			i += 3
			for i < len(sql) && sql[i] >= '0' && sql[i] <= '9' {
				i++
			}
			b.WriteByte(' ')
		case c == '/' && strings.HasPrefix(sql[i:], "/*"):
			depth := 0
			for i < len(sql) {
				if strings.HasPrefix(sql[i:], "/*") {
					depth++
					i += 2
				} else if strings.HasPrefix(sql[i:], "*/") {
					depth--
					i += 2
					if depth == 0 {
						break
					}
				} else {
					i++
				}
			}
			b.WriteByte(' ')
		case c == '\'':
			// backslash escapes: MySQL always, Postgres only in E'...'
			esc := my || (i > 0 && (sql[i-1] == 'e' || sql[i-1] == 'E') && (i < 2 || !isIdentByte(sql[i-2])))
			i++
			for i < len(sql) {
				if esc && sql[i] == '\\' {
					i += 2
					continue
				}
				if sql[i] == '\'' {
					if i+1 < len(sql) && sql[i+1] == '\'' {
						i += 2
						continue
					}
					break
				}
				i++
			}
			i++
			b.WriteString("''")
		case c == '$' && dialect != "mysql" && dialect != "mariadb":
			if tag := dollarTagRe.FindString(sql[i:]); tag != "" && (i == 0 || !isIdentByte(sql[i-1])) {
				end := strings.Index(sql[i+len(tag):], tag)
				if end < 0 {
					i = len(sql)
				} else {
					i += len(tag) + end + len(tag)
				}
				b.WriteString("''")
				break
			}
			b.WriteByte(c)
			i++
		case c == '"' || c == '`':
			j := i + 1
			for j < len(sql) {
				if sql[j] == c {
					if j+1 < len(sql) && sql[j+1] == c {
						j += 2
						continue
					}
					break
				}
				j++
			}
			if j > len(sql) {
				j = len(sql)
			}
			b.WriteByte(' ')
			b.WriteString(strings.NewReplacer(string(c)+string(c), string(c)).Replace(sql[i+1 : min(j, len(sql))]))
			b.WriteByte(' ')
			i = j + 1
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}
