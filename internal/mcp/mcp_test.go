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
	ran        []string
	dialect    string // "" = postgres
}

func (f *fakeSess) Caps() db.Capabilities {
	if f.dialect != "" {
		return db.Capabilities{Dialect: f.dialect}
	}
	return db.Capabilities{Dialect: "postgres"}
}
func (f *fakeSess) ServerInfo() string { return "fake" }
func (f *fakeSess) Close() error       { return nil }
func (f *fakeSess) Exec(_ context.Context, stmt string, _ int) (*db.Result, error) {
	f.ran = append(f.ran, stmt)
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
	if len(fs.ran) != 1 {
		t.Errorf("ran %v", fs.ran)
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
	if len(fs.ran) != 0 {
		t.Errorf("ran %v", fs.ran)
	}
	s, _ = mk("cql", false)
	if out, bad := call(t, s, full, "execute_read_query", map[string]any{"connection": "c", "sql": "SELECT 1"}); !bad || !strings.Contains(out, "read_only") {
		t.Errorf("no begin-read-only dialect: %s", out)
	}
	s, _ = mk("cql", true)
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
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i == 4 {
				call(t, s2, full, "disconnect", map[string]any{"connection": "c"})
				return
			}
			call(t, s2, full, "execute_read_query", map[string]any{"connection": "c", "sql": "SELECT 1"})
		}(i)
	}
	wg.Wait()
}
