package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/store"
)

// fakeSession is an in-memory db.Session with one table, "customers".
type fakeSession struct {
	mu               sync.Mutex
	rows             [][]any
	applied          [][]db.Statement
	execs            []string
	dialect          string
	failApply        error
	lastQuery        db.RowQuery
	lastOld, lastNew *db.TableDef
	document         bool
	objs             []db.Object // overrides Objects when set
	editor           string      // overrides Caps.Editor ("redis")
	curDB            string
	failRows         error // Rows returns this error
	colCalls         atomic.Int64
}

var customerCols = []db.Column{{Name: "id", Type: "int", PK: true}, {Name: "name", Type: "text", Nullable: true}, {Name: "email", Type: "text", Nullable: true}}

func newFake() *fakeSession {
	return &fakeSession{dialect: "postgres", rows: [][]any{{int64(1), "Ann", "ann@x.io"}, {int64(2), "Bob", nil}, {int64(3), "Cy", "cy@x.io"}}}
}

func (f *fakeSession) Caps() db.Capabilities {
	return db.Capabilities{Schemas: true, Indexes: true, Triggers: true, ForeignKeys: true, Views: true, Functions: true, Transactions: true, Editor: orStr(f.editor, "sql"), Dialect: f.dialect, DatabaseNoun: "database", Document: f.document}
}
func (f *fakeSession) ServerInfo() string         { return "Fake 1.0" }
func (f *fakeSession) Ping(context.Context) error { return nil }
func (f *fakeSession) Close() error               { return nil }
func (f *fakeSession) CurrentDatabase() string {
	if f.curDB != "" {
		return f.curDB
	}
	return "shop"
}
func (f *fakeSession) CurrentSchema() string { return "public" }
func (f *fakeSession) UseDatabase(_ context.Context, name string) error {
	f.curDB = name
	return nil
}
func (f *fakeSession) Databases(context.Context) ([]string, error) {
	return []string{"shop", "other"}, nil
}
func (f *fakeSession) Schemas(context.Context) ([]string, error) {
	return []string{"public", "app"}, nil
}
func orStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func (f *fakeSession) Objects(context.Context, string) ([]db.Object, error) {
	if f.objs != nil {
		return f.objs, nil
	}
	return []db.Object{
		{ObjRef: db.ObjRef{Schema: "public", Name: "customers", Kind: db.KindTable}, Detail: "3 rows"},
		{ObjRef: db.ObjRef{Schema: "public", Name: "orders", Kind: db.KindTable}},
		{ObjRef: db.ObjRef{Schema: "public", Name: "recent", Kind: db.KindView}},
		{ObjRef: db.ObjRef{Schema: "public", Name: "order_count", Kind: db.KindFunction}},
	}, nil
}
func (f *fakeSession) Columns(context.Context, db.ObjRef) ([]db.Column, error) {
	f.colCalls.Add(1)
	return append([]db.Column(nil), customerCols...), nil
}
func (f *fakeSession) Indexes(context.Context, db.ObjRef) ([]db.Index, error) {
	return []db.Index{{Name: "customers_pkey", Columns: []string{"id"}, Unique: true, Primary: true}}, nil
}
func (f *fakeSession) Triggers(context.Context, db.ObjRef) ([]db.Trigger, error) {
	return []db.Trigger{{Name: "audit", Timing: "AFTER", Event: "INSERT", Statement: "FOR EACH ROW EXECUTE FUNCTION log_order()"}}, nil
}
func (f *fakeSession) ForeignKeys(context.Context, db.ObjRef) ([]db.ForeignKey, error) {
	return nil, nil
}
func (f *fakeSession) DDL(_ context.Context, r db.ObjRef) (string, error) {
	if r.Kind == db.KindFunction {
		return "CREATE OR REPLACE FUNCTION " + r.Name + "() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$", nil
	}
	return "CREATE TABLE " + r.Name + " (id int primary key);", nil
}
func (f *fakeSession) Script(_ context.Context, k db.ScriptKind, r db.ObjRef) (string, error) {
	return fmt.Sprintf("-- %s %s", k, r.Name), nil
}

func (f *fakeSession) Rows(_ context.Context, q db.RowQuery) (*db.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastQuery = q
	if f.failRows != nil && len(q.Filters.Active()) > 0 {
		return nil, f.failRows
	}
	keep := func(name string) bool { // honour the visible-columns projection like a real driver
		if len(q.Columns) == 0 {
			return true
		}
		for _, c := range q.Columns {
			if c == name {
				return true
			}
		}
		return false
	}
	res := &db.Result{HasRows: true}
	var idx []int
	for i, c := range customerCols {
		if keep(c.Name) {
			idx = append(idx, i)
			res.Columns = append(res.Columns, db.ResultColumn{Name: c.Name, Type: c.Type})
		}
	}
	for i, r := range f.rows {
		if i < q.Offset || (q.Limit > 0 && i >= q.Offset+q.Limit) {
			continue
		}
		row := make([]any, len(idx))
		for j, k := range idx {
			row[j] = r[k]
		}
		res.Rows = append(res.Rows, row)
		if f.document {
			res.Docs = append(res.Docs, fmt.Sprintf(`{"id":%v,"name":%q,"tags":["a","b"],"addr":{"city":"Oslo","zip":null}}`, r[0], r[1]))
		}
	}
	return res, nil
}
func (f *fakeSession) Count(context.Context, db.RowQuery, bool) (db.Count, error) {
	return db.Count{N: int64(len(f.rows)), Exact: true}, nil
}

func (f *fakeSession) Exec(ctx context.Context, stmt string, limit int) (*db.Result, error) {
	f.mu.Lock()
	f.execs = append(f.execs, stmt)
	f.mu.Unlock()
	switch {
	case strings.Contains(stmt, "boom"):
		return nil, errors.New("syntax error near boom")
	case strings.Contains(stmt, "sleep"):
		<-ctx.Done()
		return nil, ctx.Err()
	case strings.HasPrefix(strings.ToLower(stmt), "select * from customers"):
		res := &db.Result{HasRows: true}
		for _, c := range customerCols {
			res.Columns = append(res.Columns, db.ResultColumn{Name: c.Name, Type: c.Type})
		}
		for _, r := range f.rows {
			res.Rows = append(res.Rows, append([]any(nil), r...))
		}
		return res, nil
	case strings.HasPrefix(strings.ToLower(stmt), "select"):
		return &db.Result{HasRows: true, Columns: []db.ResultColumn{{Name: "n", Type: "int"}}, Rows: [][]any{{int64(1)}, {int64(2)}}}, nil
	}
	return &db.Result{Affected: 1, Message: "1 row affected"}, nil
}

func (f *fakeSession) Plan(ch []db.Change) ([]db.Statement, error) { return db.PlanSQL(db.PG, ch) }
func (f *fakeSession) PlanAdmin(op db.AdminOp, ref db.ObjRef, _ map[string]string) ([]db.Statement, error) {
	return []db.Statement{{Text: fmt.Sprintf("-- %s %s", op, ref.Name)}}, nil
}
func (f *fakeSession) PlanTable(old, new *db.TableDef) ([]db.Statement, error) {
	f.lastOld, f.lastNew = old, new
	return []db.Statement{{Text: "ALTER TABLE " + new.Name}}, nil
}
func (f *fakeSession) Apply(_ context.Context, st []db.Statement) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failApply != nil {
		return f.failApply
	}
	f.applied = append(f.applied, st)
	for _, s := range st { // mimic the one UPDATE the tests make
		if strings.HasPrefix(s.Text, "UPDATE") && len(s.Args) >= 2 {
			for _, r := range f.rows {
				if fmt.Sprint(r[0]) == fmt.Sprint(s.Args[len(s.Args)-1]) {
					r[1] = s.Args[0]
				}
			}
		}
	}
	return nil
}

// ---- harness ----------------------------------------------------------------------------

type harness struct {
	connected []config.Connection // every connection Connect was called with
	t         *testing.T
	a         *App
	fake      *fakeSession
	wait      time.Duration // how long pump waits for a command before treating it as a timer
}

func newHarness(t *testing.T, mutate func(*config.Connection, *config.Config)) *harness {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Defaults()
	conn := config.Connection{Name: "shop", Driver: config.Postgres, Host: "h", Password: "pw"}
	if mutate != nil {
		mutate(&conn, &cfg)
	}
	km, err := cfg.Keymap()
	if err != nil {
		t.Fatal(err)
	}
	state, _ := store.LoadState(filepath.Join(dir, "state.json"))
	fake := newFake()
	d := &Deps{Cfg: cfg, Keys: km, Conns: []config.Connection{conn}, ConnPath: filepath.Join(dir, "connections.toml"), State: state,
		Hist: &store.History{Dir: filepath.Join(dir, "h")}, Queries: &store.Queries{Root: filepath.Join(dir, "q")},
		Connect: func(context.Context, config.Connection) (*Opened, error) {
			return &Opened{Sess: fake, Password: conn.Password}, nil
		}}
	h := &harness{t: t, a: NewApp(d), fake: fake, wait: 60 * time.Millisecond}
	d.Connect = func(_ context.Context, c config.Connection) (*Opened, error) {
		h.connected = append(h.connected, c)
		return &Opened{Sess: fake, Password: conn.Password}, nil
	}
	h.a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	return h
}

// pump runs cmd like Bubble Tea does (batched commands concurrently) and feeds every resulting
// message back into the app on this goroutine. It returns once nothing has arrived for h.wait:
// commands still running then are timers or deliberately blocked queries, and are abandoned.
func (h *harness) pump(cmd tea.Cmd) {
	h.t.Helper()
	msgs := make(chan tea.Msg, 256)
	pending := 0
	launch := func(c tea.Cmd) {
		if c != nil {
			pending++
			go func() { msgs <- c() }()
		}
	}
	launch(cmd)
	for pending > 0 {
		select {
		case msg := <-msgs:
			pending--
			switch m := msg.(type) {
			case nil:
			case tea.BatchMsg:
				for _, c := range m {
					launch(c)
				}
			default:
				_, next := h.a.Update(msg)
				launch(next)
			}
		case <-time.After(h.wait):
			return // what is left are timers or deliberately blocked queries
		}
	}
}

func keyMsg(s string) tea.KeyPressMsg {
	k := tea.Key{}
	for strings.Contains(s, "+") && len(s) > 1 {
		i := strings.Index(s, "+")
		switch s[:i] {
		case "ctrl":
			k.Mod |= tea.ModCtrl
		case "alt":
			k.Mod |= tea.ModAlt
		case "shift":
			k.Mod |= tea.ModShift
		}
		s = s[i+1:]
	}
	if s == "space" {
		return tea.KeyPressMsg{Code: ' ', Text: " "}
	}
	named := map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "backspace": tea.KeyBackspace, "down": tea.KeyDown,
		"up": tea.KeyUp, "left": tea.KeyLeft, "right": tea.KeyRight, "delete": tea.KeyDelete, "pgdown": tea.KeyPgDown}
	if r, ok := named[s]; ok {
		k.Code = r
		return tea.KeyPressMsg(k)
	}
	r := []rune(s)[0]
	k.Code = r
	if k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
		k.Text = s
		if r >= 'A' && r <= 'Z' {
			k.Mod |= tea.ModShift
		}
	}
	return tea.KeyPressMsg(k)
}

// press sends keys like "ctrl+d", "s", "enter" and pumps the resulting commands.
func (h *harness) press(keys ...string) {
	h.t.Helper()
	for _, k := range keys {
		_, cmd := h.a.Update(keyMsg(k))
		h.pump(cmd)
	}
}

func (h *harness) typeText(s string) {
	for _, r := range s {
		h.press(string(r))
	}
}

func (h *harness) prefix(keys ...string) { h.press(append([]string{"ctrl+d"}, keys...)...) }

func (h *harness) screen() string { return stripANSI(h.a.render()) }

func (h *harness) want(substr string) {
	h.t.Helper()
	if s := h.screen(); !strings.Contains(s, substr) {
		h.t.Fatalf("screen does not contain %q:\n%s", substr, s)
	}
}

func (h *harness) wantNot(substr string) {
	h.t.Helper()
	if s := h.screen(); strings.Contains(s, substr) {
		h.t.Fatalf("screen unexpectedly contains %q:\n%s", substr, s)
	}
}

// connect opens the first connection and waits for the sidebar to load.
func (h *harness) connect() {
	h.t.Helper()
	h.press("enter")
	if h.a.ws() == nil {
		h.t.Fatalf("no workspace after connecting:\n%s", h.screen())
	}
	h.want("customers")
}

// openCustomers opens the customers table and lands on the grid.
func (h *harness) openCustomers() {
	h.t.Helper()
	h.press("enter") // sidebar selection snaps to the first object
	h.want("ann@x.io")
}
