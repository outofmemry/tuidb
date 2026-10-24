package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/store"
)

type fakeSess struct {
	db.Session // nil: only the methods below are used
	mu         sync.Mutex
	ran        []string
	dialect    string // "" = postgres
}

func (f *fakeSess) record(stmt string) {
	f.mu.Lock()
	f.ran = append(f.ran, stmt)
	f.mu.Unlock()
}

func (f *fakeSess) log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ran...)
}

// pinned is what fakeSess.Pin lends: it records into the parent so a test sees BEGIN, the statement and ROLLBACK.
type pinned struct {
	db.Session
	f *fakeSess
}

func (p pinned) Caps() db.Capabilities { return p.f.Caps() }
func (p pinned) ServerInfo() string    { return "fake" }
func (p pinned) Close() error          { return nil }
func (p pinned) Exec(ctx context.Context, stmt string, n int) (*db.Result, error) {
	return p.f.Exec(ctx, stmt, n)
}
func (f *fakeSess) Pin(context.Context) (db.Session, error) { return pinned{f: f}, nil }

// plainSess is a Session that cannot pin.
type plainSess struct {
	db.Session
	f *fakeSess
}

func (p plainSess) Caps() db.Capabilities { return p.f.Caps() }
func (p plainSess) ServerInfo() string    { return "fake" }
func (p plainSess) Close() error          { return nil }
func (p plainSess) Exec(ctx context.Context, stmt string, n int) (*db.Result, error) {
	return p.f.Exec(ctx, stmt, n)
}

func (f *fakeSess) Caps() db.Capabilities {
	if f.dialect != "" {
		return db.Capabilities{Dialect: f.dialect, Transactions: f.dialect != "clickhouse"}
	}
	return db.Capabilities{Dialect: "postgres", Transactions: true}
}
func (f *fakeSess) ServerInfo() string { return "fake" }
func (f *fakeSess) Close() error       { return nil }
func (f *fakeSess) Exec(_ context.Context, stmt string, _ int) (*db.Result, error) {
	f.record(stmt)
	return &db.Result{Columns: []db.ResultColumn{{Name: "a"}}, Rows: [][]any{{1}}, HasRows: true}, nil
}

func call(t *testing.T, s *Server, tok *config.MCPToken, tool string, args any) (string, bool) {
	t.Helper()
	ctx := context.Background()
	ct, st := sdk.NewInMemoryTransports()
	if _, err := s.build(tok).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	r, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return r.Content[0].(*sdk.TextContent).Text, r.IsError
}

func TestFirstWordGate(t *testing.T) {
	for _, c := range []struct{ dialect, sql, want string }{
		{"postgres", "/* x */ -- y\n drop table t", "DROP"},
		{"postgres", "DO $$ BEGIN EXECUTE 'DROP TABLE orders'; END $$", "DO"},
		{"postgres", "CALL purge()", "CALL"},
		{"mysql", "# x\nDROP TABLE orders", "DROP"},
		{"mysql", "/*!50000 DROP TABLE orders */", ""},
		{"postgres", "# x\nDROP TABLE orders", ""},
		{"postgres", "UPDATE t SET a=1", "UPDATE"},
	} {
		if got := firstWord(c.dialect, c.sql); got != c.want {
			t.Errorf("firstWord(%s, %q) = %q, want %q", c.dialect, c.sql, got, c.want)
		}
	}
	for _, w := range []string{"DO", "CALL", "EXECUTE", ""} {
		if !ddl[w] && w != "" {
			t.Errorf("%s not gated", w)
		}
	}
}

func TestTools(t *testing.T) {
	fs := &fakeSess{}
	s := New(config.Config{MCP: config.MCP{ApprovalForUnsafe: true}, General: config.General{RowLimit: 10}}, []config.Connection{{Name: "c", Driver: "fake"}})
	s.connect = func(context.Context, config.Connection) (db.Session, func(), error) { return fs, func() {}, nil }
	full := &config.MCPToken{Name: "f", Scope: "full"}
	ro := &config.MCPToken{Name: "r", Scope: "read-only"}

	if _, bad := call(t, s, full, "open_connection", map[string]any{"connection": "c"}); bad {
		t.Fatal("open failed")
	}
	if out, bad := call(t, s, full, "execute_read_query", map[string]any{"connection": "c", "sql": "SELECT 1"}); bad || !strings.Contains(out, `"rows":[[1]]`) {
		t.Errorf("select: %s", out)
	}
	if _, bad := call(t, s, full, "execute_read_query", map[string]any{"connection": "c", "sql": "UPDATE t SET a=1"}); !bad {
		t.Error("UPDATE accepted by execute_read_query")
	}
	if out, bad := call(t, s, full, "execute_query", map[string]any{"connection": "c", "sql": "UPDATE t SET a=1"}); !bad || !strings.Contains(out, "needs approval") {
		t.Errorf("unsafe: %s", out)
	}
	if _, bad := call(t, s, ro, "execute_query", map[string]any{"connection": "c", "sql": "SELECT 1"}); !bad {
		t.Error("read-only token ran execute_query")
	}
	if got := strings.Join(fs.log(), "|"); got != "BEGIN READ ONLY|SELECT 1|ROLLBACK" {
		t.Errorf("ran %v, want the SELECT inside a read-only transaction", got)
	}
	if l := s.Log(); len(l) != 5 || l[1].Tool != "execute_read_query" || l[1].SQL != "SELECT 1" || l[2].Err == "" {
		t.Errorf("log %+v", l)
	}
	scoped := &config.MCPToken{Name: "s", Scope: "full", Connections: []string{"other"}}
	if _, bad := call(t, s, scoped, "open_connection", map[string]any{"connection": "c"}); !bad {
		t.Error("connection restriction ignored")
	}
}

func TestHandlerAuth(t *testing.T) {
	s := New(config.Config{MCP: config.MCP{Tokens: []config.MCPToken{{Name: "a", Value: "tok", Scope: "full"}}}}, nil)
	for _, h := range []string{"", "Bearer nope"} {
		req := httptest.NewRequest("POST", "/", nil)
		if h != "" {
			req.Header.Set("Authorization", h)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%q: %d", h, w.Code)
		}
	}
}

func TestScopesAndRedact(t *testing.T) {
	fs := &fakeSess{}
	s := New(config.Config{General: config.General{RowLimit: 10}}, []config.Connection{{Name: "c", Driver: "fake"}})
	s.connect = func(context.Context, config.Connection) (db.Session, func(), error) { return fs, func() {}, nil }
	full := &config.MCPToken{Name: "f", Scope: "full"}
	rw := &config.MCPToken{Name: "w", Scope: "read-write"}
	ro := &config.MCPToken{Name: "r", Scope: "read-only"}
	call(t, s, full, "open_connection", map[string]any{"connection": "c"})
	if _, bad := call(t, s, ro, "disconnect", map[string]any{"connection": "c"}); !bad {
		t.Error("read-only token disconnected")
	}
	if _, bad := call(t, s, rw, "execute_query", map[string]any{"connection": "c", "sql": "/* x */ drop table t"}); !bad {
		t.Error("read-write token ran DROP")
	}
	if _, bad := call(t, s, rw, "execute_query", map[string]any{"connection": "c", "sql": "UPDATE t SET a=1"}); bad {
		t.Error("read-write token refused UPDATE")
	}
	if got := redact("ALTER ROLE a PASSWORD 'it''s'; SELECT 'x'"); got != "ALTER ROLE a PASSWORD '…'; SELECT '…'" {
		t.Error(got)
	}
	for _, l := range s.Log() {
		if strings.Contains(l.SQL, "UPDATE") && l.SQL != "UPDATE t SET a=1" {
			t.Error(l.SQL)
		}
	}
}

func TestSessionBoundToToken(t *testing.T) {
	s := New(config.Config{MCP: config.MCP{Tokens: []config.MCPToken{
		{Name: "a", Value: "ta", Scope: "full"}, {Name: "b", Value: "tb", Scope: "read-only"}}}}, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	post := func(tok, sid, body string) *http.Response {
		req, _ := http.NewRequest("POST", srv.URL, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if sid != "" {
			req.Header.Set("Mcp-Session-Id", sid)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r
	}
	r := post("ta", "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	sid := r.Header.Get("Mcp-Session-Id")
	if sid == "" {
		t.Fatalf("no session: %d", r.StatusCode)
	}
	if r := post("tb", sid, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`); r.StatusCode != http.StatusForbidden {
		t.Errorf("hijack got %d", r.StatusCode)
	}
}

func TestStoreTools(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s := New(config.Config{}, nil)
	ro := &config.MCPToken{Name: "r", Scope: "read-only"}
	if out, bad := call(t, s, ro, "list_saved_queries", map[string]any{}); !bad || !strings.Contains(out, "not configured") {
		t.Errorf("unset: %s", out)
	}
	s.Queries = &store.Queries{Root: t.TempDir()}
	s.Hist = &store.History{Dir: t.TempDir()}
	s.Boards = true
	q, err := s.Queries.Save("", "Recent", nil, "select 1")
	if err != nil {
		t.Fatal(err)
	}
	if out, bad := call(t, s, ro, "get_saved_query", map[string]any{"name": q.Path}); bad || !strings.Contains(out, "select 1") {
		t.Errorf("get: %s", out)
	}
	s.Hist.Append("c", store.HistoryEntry{ID: "1", SQL: "select 2"})
	if out, bad := call(t, s, ro, "list_history", map[string]any{"connection": "c", "limit": 5}); !bad {
		t.Errorf("history must be withheld from a read-only token: %s", out)
	}
	s.Hist.Append("c", store.HistoryEntry{SQL: "alter role x password 'pw'", Original: "a,b"})
	rw := &config.MCPToken{Name: "rw", Scope: "read-write"}
	if out, bad := call(t, s, rw, "list_history", map[string]any{"connection": "c", "limit": 5}); bad || strings.Contains(out, "'pw'") || strings.Contains(out, "a,b") || !strings.Contains(out, "select 2") {
		t.Errorf("hist: %s", out)
	}
	if out, bad := call(t, s, ro, "list_boards", map[string]any{}); bad || out != "[]" {
		t.Errorf("boards: %s", out)
	}
	if out, bad := call(t, s, ro, "run_board", map[string]any{"name": "x"}); !bad || !strings.Contains(out, "no board") {
		t.Errorf("run: %s", out)
	}
}

func TestReadQueryGuards(t *testing.T) {
	full := &config.MCPToken{Name: "f", Scope: "full"}
	mk := func(dialect string, roConn bool) (*Server, *fakeSess) {
		fs := &fakeSess{dialect: dialect}
		s := New(config.Config{General: config.General{RowLimit: 10}}, []config.Connection{{Name: "c", Driver: "fake", ReadOnly: roConn}})
		s.connect = func(context.Context, config.Connection) (db.Session, func(), error) { return fs, func() {}, nil }
		call(t, s, full, "open_connection", map[string]any{"connection": "c"})
		return s, fs
	}
	s, fs := mk("postgres", false)
	for _, q := range []string{"SELECT pg_terminate_backend(1)", "select PG_READ_FILE('/etc/passwd')", "SELECT nextval('s')", "SELECT lo_export(1,'/x')", "SELECT setval('s',1)", "SELECT * FROM dblink('a','b') AS t(x int)"} {
		if out, bad := call(t, s, full, "execute_read_query", map[string]any{"connection": "c", "sql": q}); !bad || !strings.Contains(out, "not allowed in a read-only query") {
			t.Errorf("%s: %s", q, out)
		}
	}
	if got := fs.log(); len(got) != 0 {
		t.Errorf("ran %v", got)
	}
	s, _ = mk("cql", false)
	if out, bad := call(t, s, full, "execute_read_query", map[string]any{"connection": "c", "sql": "SELECT 1"}); !bad || !strings.Contains(out, "cannot be enforced") {
		t.Errorf("no begin-read-only dialect: %s", out)
	}
	s, _ = mk("cql", true) // cql does not enforce read_only on the server: still refused
	if out, bad := call(t, s, full, "execute_read_query", map[string]any{"connection": "c", "sql": "SELECT 1"}); !bad {
		t.Errorf("cql read_only connection was trusted: %s", out)
	}
	s, _ = mk("mysql", true)
	if out, bad := call(t, s, full, "execute_read_query", map[string]any{"connection": "c", "sql": "SELECT 1"}); bad {
		t.Errorf("read_only connection refused: %s", out)
	}
}

func TestDisconnectedUnderLock(t *testing.T) {
	fs := &fakeSess{}
	s := New(config.Config{General: config.General{RowLimit: 10}}, []config.Connection{{Name: "c", Driver: "fake"}})
	s.connect = func(context.Context, config.Connection) (db.Session, func(), error) { return fs, func() {}, nil }
	full := &config.MCPToken{Name: "f", Scope: "full"}
	call(t, s, full, "open_connection", map[string]any{"connection": "c"})
	s.mu.Lock()
	o := s.open["c"]
	s.mu.Unlock()
	o.use.Lock()
	o.closed = true // what disconnect does, while a call has already fetched o
	o.use.Unlock()
	if out, bad := call(t, s, full, "execute_read_query", map[string]any{"connection": "c", "sql": "SELECT 1"}); !bad {
		t.Fatalf("ran on a closed session: %s", out)
	}
	var wg sync.WaitGroup
	s2 := New(config.Config{General: config.General{RowLimit: 10}}, []config.Connection{{Name: "c", Driver: "fake"}})
	s2.connect = s.connect
	call(t, s2, full, "open_connection", map[string]any{"connection": "c"})
	type outcome struct {
		out string
		bad bool
	}
	res := make([]outcome, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i == 4 {
				res[i].out, res[i].bad = call(t, s2, full, "disconnect", map[string]any{"connection": "c"})
				return
			}
			res[i].out, res[i].bad = call(t, s2, full, "execute_read_query", map[string]any{"connection": "c", "sql": "SELECT 1"})
		}(i)
	}
	wg.Wait()
	for i, r := range res {
		// a query either ran before the disconnect or was refused as disconnected / not open; nothing else
		if i != 4 && r.bad && !strings.Contains(r.out, "disconnected") && !strings.Contains(r.out, "not open") {
			t.Errorf("call %d: unexpected error %q", i, r.out)
		}
	}
}

func TestSideEffectFunc(t *testing.T) {
	for _, c := range []struct{ sql, want string }{
		{"SELECT dblink_exec('host=x','DELETE FROM users')", "dblink_exec"},
		{"SELECT * FROM dblink ('a','b') AS t(x int)", "dblink"},
		{`SELECT "dblink_exec"('a','b')`, "dblink_exec"},
		{"SELECT public.lo_import('/x')", "lo_import"},
		{"SELECT pg_advisory_lock(1)", "pg_advisory_lock"},
		{"SELECT pg_switch_wal()", "pg_switch_wal"},
		{"SELECT pg_reload_conf()", "pg_reload_conf"},
		{"SELECT query_to_xml(concat('select pg_read_','file(''/etc/passwd'')'), true, false, '')", "query_to_xml"},
		{"SELECT cursor_to_xmlschema('c', true, false, '')", "cursor_to_xmlschema"},
		{"COPY t TO PROGRAM 'x'", "copy"},
		{"copy (select 1) to stdout", "copy"},
		{`SELECT U&"d\0062link_exec"('a','b')`, "a unicode-escaped identifier"},
		{"SELECT /* ok */ pg_terminate_backend(1)", "pg_terminate_backend"},
		{"SELECT * FROM audit WHERE action = 'copy'", ""},
		{"SELECT copy FROM t", ""},
		{"SELECT 1 -- copy this later\n", ""},
		{"SELECT 'dblink_exec(' || x FROM t", ""},
		{"SELECT $q$ nextval( $q$", ""},
		{`SELECT "copy" FROM "dblink_x"`, ""},
		{"SELECT * FROM t /* nextval(1) */", ""},
		{"SELECT 'it''s copy(' , 1", ""},
	} {
		if got := sideEffectFunc("postgres", c.sql); got != c.want {
			t.Errorf("sideEffectFunc(%q) = %q, want %q", c.sql, got, c.want)
		}
	}
	// a literal cannot open a window for what follows it
	if got := sideEffectFunc("mysql", `SELECT 'a\'', lo_import(1) -- '`); got != "lo_import" {
		t.Errorf("mysql backslash escape: %q", got)
	}
	if got := sideEffectFunc("postgres", `SELECT E'a\'' , nextval('s')`); got != "nextval" {
		t.Errorf("postgres E string: %q", got)
	}
}

func TestReadOnlyNeedsPinner(t *testing.T) {
	full := &config.MCPToken{Name: "f", Scope: "full"}
	fs := &fakeSess{}
	mk := func(roConn bool) *Server {
		s := New(config.Config{General: config.General{RowLimit: 10}}, []config.Connection{{Name: "c", Driver: "fake", ReadOnly: roConn}})
		s.connect = func(context.Context, config.Connection) (db.Session, func(), error) {
			return plainSess{f: fs}, func() {}, nil
		}
		call(t, s, full, "open_connection", map[string]any{"connection": "c"})
		return s
	}
	if out, bad := call(t, mk(false), full, "execute_read_query", map[string]any{"connection": "c", "sql": "SELECT 1"}); !bad || !strings.Contains(out, "cannot be enforced") {
		t.Errorf("a postgres session that cannot pin ran a read query: %s", out)
	}
	if got := fs.log(); len(got) != 0 {
		t.Errorf("ran %v", got)
	}
	if out, bad := call(t, mk(true), full, "execute_read_query", map[string]any{"connection": "c", "sql": "SELECT 1"}); bad {
		t.Errorf("read_only connection refused: %s", out)
	}
}

func TestRunBoardGuards(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "webtuidb", "boards"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, q string) {
		body := "name = \"" + name + "\"\n[[panel]]\ntitle = \"p\"\nkind = \"table\"\nquery = \"" + q + "\"\n"
		if err := os.WriteFile(filepath.Join(dir, "webtuidb", "boards", name+".toml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("bad", "SELECT dblink_exec('a','b')")
	write("ok", "SELECT 1")
	fs := &fakeSess{}
	s := New(config.Config{General: config.General{RowLimit: 10}}, []config.Connection{{Name: "c", Driver: "fake"}})
	s.Boards = true
	s.connect = func(context.Context, config.Connection) (db.Session, func(), error) { return fs, func() {}, nil }
	full := &config.MCPToken{Name: "f", Scope: "full"}
	call(t, s, full, "open_connection", map[string]any{"connection": "c"})
	if out, bad := call(t, s, full, "run_board", map[string]any{"name": "bad", "connection": "c"}); !bad || !strings.Contains(out, "dblink_exec") {
		t.Errorf("board with dblink_exec: %s", out)
	}
	if got := fs.log(); len(got) != 0 {
		t.Errorf("ran %v", got)
	}
	if out, bad := call(t, s, full, "run_board", map[string]any{"name": "ok", "connection": "c"}); bad {
		t.Errorf("ok board: %s", out)
	}
	if got := strings.Join(fs.log(), "|"); got != "BEGIN READ ONLY|SELECT 1|ROLLBACK" {
		t.Errorf("board panels must run in a read-only transaction, ran %v", got)
	}
}

func TestSideEffectReviewCases(t *testing.T) {
	for _, c := range []struct{ dialect, sql, want string }{
		{"postgres", `SELECT $a1$ x $$ $a1$ , pg_read_file('/etc/passwd')`, "pg_read_file"},
		{"postgres", `SELECT $a1$ x $$ $a1$ , dblink_exec('host=h','DELETE FROM users')`, "dblink_exec"},
		{"postgres", `SELECT $a1$ copy( $a1$`, ""},
		{"mysql", `SELECT LOAD_FILE('/etc/passwd')`, "load_file"},
		{"mysql", `SELECT 1 INTO  OUTFILE '/tmp/x'`, "into outfile"},
		{"mysql", `SELECT get_lock('a', 1)`, "get_lock"},
		{"clickhouse", `SELECT * FROM file('/etc/passwd')`, "file"},
		{"clickhouse", `SELECT * FROM url('http://169.254.169.254/')`, "url"},
		{"clickhouse", `SELECT 1`, ""},
	} {
		if got := sideEffectFunc(c.dialect, c.sql); got != c.want {
			t.Errorf("sideEffectFunc(%s, %q) = %q, want %q", c.dialect, c.sql, got, c.want)
		}
	}
}

func TestExplainAnalyzeDDLGate(t *testing.T) {
	for _, sql := range []string{"EXPLAIN ANALYZE CREATE TABLE x AS SELECT 1", "explain (analyze, buffers) drop table t", "EXPLAIN ANALYZE VERBOSE ALTER TABLE t ADD c int"} {
		if w := firstWord("postgres", sql); !ddl[w] {
			t.Errorf("%q: first word %q passes the DDL gate", sql, w)
		}
	}
	if w := firstWord("postgres", "EXPLAIN SELECT 1"); w != "SELECT" {
		t.Errorf("EXPLAIN SELECT: %q", w)
	}
}

func TestReadOnlyRunnerDialects(t *testing.T) {
	ctx := context.Background()
	// ClickHouse read_only is a keyword filter only: it must not be trusted, and it has no read-only transaction.
	if _, _, err := ReadOnlyTx(ctx, &fakeSess{dialect: "clickhouse"}, "clickhouse", true); err == nil {
		t.Error("clickhouse read_only connection was trusted")
	}
	// A dialect with no transactions (Vertica reports postgres) must not get BEGIN READ ONLY.
	if _, _, err := ReadOnlyTx(ctx, noTx{&fakeSess{}}, "postgres", false); err == nil {
		t.Error("BEGIN READ ONLY issued on a session without transactions")
	}
	if _, rel, err := ReadOnlyTx(ctx, &fakeSess{}, "postgres", true); err != nil {
		t.Error(err)
	} else {
		rel()
	}
}

type noTx struct{ *fakeSess }

func (n noTx) Caps() db.Capabilities { return db.Capabilities{Dialect: "postgres"} }
