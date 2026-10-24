package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tuidb/internal/config"
	"tuidb/internal/db"
	"tuidb/internal/safemode"
	"tuidb/internal/store"
)

var dialects = []string{"postgres", "mysql", "clickhouse", "redis", "mongo", "sqlite", "duckdb", "cql", "partiql"}

// ---- meta and connections ---------------------------------------------------------

func (s *Server) meta(r *http.Request) (any, error) {
	ops := map[string][]string{}
	for _, d := range dialects {
		ops[d] = db.Operators(d)
	}
	c := s.o.Cfg
	// The web UI runs the same prefix state machine as the terminal, so it gets the resolved bindings.
	km, err := c.Keymap()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"drivers": config.Drivers, "sslModes": config.SSLModes, "safeModes": safemode.All, "operators": ops,
		"config": map[string]any{"general": c.General, "editor": c.Editor, "table": c.Table, "sidebar": c.Sidebar},
		"keymap": map[string]any{"prefix": km.Prefix, "bindings": km.All()},
	}, nil
}

func (s *Server) getConns(r *http.Request) (any, error) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.o.Conns == nil {
		return []config.Connection{}, nil
	}
	return s.o.Conns, nil
}

// putConns replaces the whole list, exactly like the TUI saves it.
func (s *Server) putConns(r *http.Request) (any, error) {
	var in *[]config.Connection
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in == nil { // empty body or null would otherwise wipe every saved connection
		return nil, bad("a JSON array of connections is required (send [] to clear)")
	}
	conns := *in
	names := map[string]bool{}
	for _, c := range conns {
		if err := c.Validate(); err != nil {
			return nil, bad("%v", err)
		}
		if names[c.Name] {
			return nil, bad("duplicate connection name %q", c.Name)
		}
		names[c.Name] = true
	}
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.o.ConnPath != "" {
		if err := config.SaveConnections(s.o.ConnPath, conns); err != nil {
			return nil, err
		}
	}
	s.o.Conns = conns
	return nil, nil
}

func (s *Server) testConn(r *http.Request) (any, error) {
	var c config.Connection
	if err := decode(r, &c); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, bad("%v", err)
	}
	o, err := s.o.Connect(r.Context(), c)
	if err != nil {
		return nil, bad("%v", err)
	}
	defer o.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := o.Sess.Ping(ctx); err != nil {
		return nil, bad("%v", err)
	}
	return map[string]any{"ok": true, "serverInfo": o.Sess.ServerInfo()}, nil
}

// parseConnURL turns a connection URL into a Connection. The error text never echoes the URL.
func (s *Server) parseConnURL(r *http.Request) (any, error) {
	var in struct{ URL string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	c, err := config.ParseURL(strings.TrimSpace(in.URL))
	if err != nil {
		return nil, bad("%s", redactURL(err.Error(), in.URL))
	}
	return c, nil
}

// formatConnURL is the inverse: Connection -> {"url"} (includes the password, like "Copy as URL").
func (s *Server) formatConnURL(r *http.Request) (any, error) {
	var c config.Connection
	if err := decode(r, &c); err != nil {
		return nil, err
	}
	return map[string]string{"url": c.FormatURL()}, nil
}

// redactURL drops the raw URL (which may hold a password) from a parser error message.
func redactURL(msg, raw string) string {
	if raw = strings.TrimSpace(raw); raw != "" {
		msg = strings.ReplaceAll(msg, raw, "<url>")
	}
	return msg
}

// ---- sessions ---------------------------------------------------------------------

func (ss *session) info() map[string]any {
	sess := ss.o.Sess
	return map[string]any{
		"id": ss.id, "name": ss.conn.Name, "driver": ss.conn.Driver, "caps": sess.Caps(), "serverInfo": sess.ServerInfo(),
		"database": sess.CurrentDatabase(), "schema": sess.CurrentSchema(),
		"level": ss.level, "levelLabel": ss.level.Label(), "rowLimit": ss.rowLimit(),
		"color": ss.conn.Color, "tag": ss.conn.Tag,
	}
}

func (s *Server) openSession(r *http.Request) (any, error) {
	var req struct{ Name string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	c, ok := s.findConn(req.Name)
	if !ok {
		return nil, bad("no saved connection named %q", req.Name)
	}
	ss, err := s.newSession(r.Context(), c)
	if err != nil {
		return nil, bad("%v", err)
	}
	return ss.info(), nil
}

// closeSession is not wrapped in withSession: close waits for in-flight handlers, which would include itself.
func (s *Server) closeSession(r *http.Request) (any, error) {
	s.mu.Lock()
	ss := s.sessions[r.PathValue("id")]
	delete(s.sessions, r.PathValue("id"))
	s.mu.Unlock()
	if ss == nil {
		return nil, &apiErr{Code: http.StatusNotFound, Msg: "no such session (it was closed or the server restarted)"}
	}
	ss.close()
	return nil, nil
}

func (s *Server) ping(ss *session, r *http.Request) (any, error) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	return nil, ss.o.Sess.Ping(ctx)
}

func (s *Server) databases(ss *session, r *http.Request) (any, error) {
	ctx, cancel := ss.ctx(r)
	defer cancel()
	l, err := ss.o.Sess.Databases(ctx)
	return map[string]any{"databases": l, "current": ss.o.Sess.CurrentDatabase()}, err
}

func (s *Server) useDatabase(ss *session, r *http.Request) (any, error) {
	var req struct{ Name string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	if err := ss.o.Sess.UseDatabase(ctx, req.Name); err != nil {
		return nil, bad("%v", err)
	}
	ss.resetCaches()
	return ss.info(), nil
}

func (s *Server) schemas(ss *session, r *http.Request) (any, error) {
	ctx, cancel := ss.ctx(r)
	defer cancel()
	l, err := ss.o.Sess.Schemas(ctx)
	return map[string]any{"schemas": l, "current": ss.o.Sess.CurrentSchema()}, err
}

func (s *Server) objects(ss *session, r *http.Request) (any, error) {
	ctx, cancel := ss.ctx(r)
	defer cancel()
	l, err := ss.o.Sess.Objects(ctx, r.URL.Query().Get("schema"))
	return l, err
}

func refFrom(r *http.Request) db.ObjRef {
	q := r.URL.Query()
	return db.ObjRef{Schema: q.Get("schema"), Name: q.Get("name"), Kind: db.Kind(q.Get("kind"))}
}

// object serves per-object metadata: /object/{columns|indexes|triggers|fks|ddl|script}?schema=&name=&kind=
func (s *Server) object(ss *session, r *http.Request) (any, error) {
	ctx, cancel := ss.ctx(r)
	defer cancel()
	ref, sess := refFrom(r), ss.o.Sess
	if ref.Name == "" {
		return nil, bad("name is required")
	}
	switch what := r.PathValue("what"); what {
	case "columns":
		return ss.columns(ctx, ref)
	case "indexes":
		return sess.Indexes(ctx, ref)
	case "triggers":
		return sess.Triggers(ctx, ref)
	case "fks":
		return sess.ForeignKeys(ctx, ref)
	case "ddl":
		t, err := sess.DDL(ctx, ref)
		return map[string]string{"text": t}, err
	case "script":
		t, err := sess.Script(ctx, db.ScriptKind(r.URL.Query().Get("script")), ref)
		return map[string]string{"text": t}, err
	default:
		return nil, bad("unknown object view %q", what)
	}
}

// ---- rows, count, exec -------------------------------------------------------------

type colJSON struct {
	Name string `json:"name"`
	Type string `json:"type"`
	PK   bool   `json:"pk,omitempty"`
}

type resultJSON struct {
	ResultID   string      `json:"resultId,omitempty"`
	Columns    []colJSON   `json:"columns"`
	Cells      [][]*string `json:"cells"`
	Docs       []string    `json:"docs,omitempty"`
	Affected   int64       `json:"affected"`
	HasRows    bool        `json:"hasRows"`
	Message    string      `json:"message,omitempty"`
	DurationMs int64       `json:"durationMs"`
	Truncated  bool        `json:"truncated"`
	Editable   bool        `json:"editable"`
}

// toJSON renders cells with db.FormatValue; SQL NULL stays JSON null.
func toJSON(res *db.Result, meta []db.Column) resultJSON {
	pk := map[string]bool{}
	for _, c := range meta {
		if c.PK {
			pk[c.Name] = true
		}
	}
	out := resultJSON{
		Columns: make([]colJSON, len(res.Columns)), Cells: make([][]*string, len(res.Rows)), Docs: res.Docs,
		Affected: res.Affected, HasRows: res.HasRows, Message: res.Message,
		DurationMs: res.Duration.Milliseconds(), Truncated: res.Truncated,
	}
	for i, c := range res.Columns {
		out.Columns[i] = colJSON{Name: c.Name, Type: c.Type, PK: pk[c.Name]}
	}
	for i, row := range res.Rows {
		cells := make([]*string, len(row))
		for j, v := range row {
			if !db.IsNull(v) {
				t := db.FormatValue(v)
				cells[j] = &t
			}
		}
		out.Cells[i] = cells
	}
	return out
}

type rowsReq struct {
	Ref     db.ObjRef
	Filters db.FilterSet
	Sort    []db.Sort
	Columns []string
	Limit   int
	Offset  int
}

func (q rowsReq) query() db.RowQuery {
	return db.RowQuery{ObjRef: q.Ref, Filters: q.Filters, Sort: q.Sort, Columns: q.Columns, Limit: q.Limit, Offset: q.Offset}
}

func (s *Server) rows(ss *session, r *http.Request) (any, error) {
	var req rowsReq
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if req.Ref.Name == "" {
		return nil, bad("ref.name is required")
	}
	if rl := ss.rowLimit(); req.Limit <= 0 || (rl > 0 && req.Limit > rl) { // never above the configured limit
		req.Limit = rl
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	// columns first: Any-column filters and the PK flags need them
	cols, cerr := ss.columns(ctx, req.Ref)
	res, err := ss.o.Sess.Rows(ctx, req.query())
	if err != nil {
		return nil, bad("%v", err)
	}
	out := toJSON(res, cols)
	// like the TUI: only plain tables with a primary key are editable (a key-less WHERE can hit many rows)
	if cerr == nil && res.HasRows && req.Ref.Kind == db.KindTable && hasPK(cols) {
		out.Editable = true
		out.ResultID = ss.put(&cached{ref: req.Ref, cols: cols, res: res})
	}
	return out, nil
}

func hasPK(cols []db.Column) bool {
	for _, c := range cols {
		if c.PK {
			return true
		}
	}
	return false
}

func (s *Server) count(ss *session, r *http.Request) (any, error) {
	var req struct {
		Ref      db.ObjRef
		Filters  db.FilterSet
		Estimate bool
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	n, err := ss.o.Sess.Count(ctx, db.RowQuery{ObjRef: req.Ref, Filters: req.Filters}, req.Estimate)
	if err != nil {
		return nil, bad("%v", err)
	}
	return map[string]any{"n": n.N, "exact": n.Exact}, nil
}

type execItem struct {
	Stmt  string `json:"stmt"`
	Error string `json:"error,omitempty"`
	resultJSON
}

func (s *Server) exec(ss *session, r *http.Request) (any, error) {
	var req struct {
		Text      string
		Limit     int // 0 = the connection's row limit, <0 = no limit
		Current   bool
		Offset    int // cursor, with Current
		Confirmed bool
		Password  string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	dialect := ss.o.Sess.Caps().Dialect
	stmts := db.Split(dialect, req.Text)
	if req.Current {
		st, ok := db.StatementAt(stmts, req.Offset)
		if !ok {
			return nil, bad("no statement at the cursor")
		}
		stmts = []db.Stmt{st}
	}
	if len(stmts) == 0 {
		return nil, bad("nothing to run")
	}
	gated := make([]db.Statement, len(stmts))
	shown := make([]string, len(stmts))
	for i, st := range stmts {
		gated[i], shown[i] = db.Statement{Text: st.Text}, st.Text
	}
	g := ss.gate(gated)
	if err := ss.check(g, req.Confirmed, req.Password); err != nil {
		if ae, ok := err.(*apiErr); ok {
			ae.Extra["statements"] = shown
		}
		return nil, err
	}
	limit := req.Limit
	switch {
	case limit == 0:
		limit = ss.rowLimit()
	case limit < 0:
		limit = 0
	}
	var out []execItem
	for _, st := range stmts {
		ctx, cancel := ss.ctx(r)
		start := time.Now()
		res, err := ss.o.Sess.Exec(ctx, st.Text, limit)
		cancel()
		item := execItem{Stmt: st.Text}
		if err != nil {
			item.Error = err.Error()
			out = append(out, item)
			break // stop at the first error, like the editor
		}
		item.resultJSON = toJSON(res, nil) // not cached: exec results are never editable
		if !db.IsReadOnly(dialect, st.Text) {
			ss.clearColCache() // DDL may have changed table structure
		}
		out = append(out, item)
		ss.record(st.Text, "editor", time.Since(start))
	}
	return map[string]any{"results": out}, nil
}

func (ss *session) record(sql, source string, dur time.Duration) {
	if h := ss.srv.o.Hist; h != nil {
		h.Append(ss.conn.Name, store.HistoryEntry{Database: ss.o.Sess.CurrentDatabase(), SQL: sql, Duration: dur, Source: source})
	}
}

func (s *Server) dropResult(ss *session, r *http.Request) (any, error) {
	ss.dropResult(r.PathValue("rid"))
	return nil, nil
}

func (ss *session) dropResult(rid string) {
	ss.mu.Lock()
	delete(ss.results, rid)
	for i, id := range ss.order {
		if id == rid {
			ss.order = append(ss.order[:i], ss.order[i+1:]...)
			break
		}
	}
	ss.mu.Unlock()
}

// ---- plan and apply -----------------------------------------------------------------

type stmtJSON struct {
	Display  string `json:"display"`
	ReadOnly bool   `json:"readOnly"`
}

func (ss *session) storePlan(stmts []db.Statement, resultID string) (any, error) {
	id := randID(6)
	ss.mu.Lock()
	ss.plans[id] = stmts
	if resultID != "" {
		ss.planSrc[id] = resultID
	}
	ss.mu.Unlock()
	list := make([]stmtJSON, len(stmts))
	for i, st := range stmts {
		list[i] = stmtJSON{Display: st.Shown(), ReadOnly: st.ReadOnly}
	}
	return map[string]any{
		"planId": id, "statements": list, "gate": ss.gate(stmts),
		"transactions": ss.o.Sess.Caps().Transactions, "levelLabel": ss.level.Label(),
	}, nil
}

func (s *Server) plan(ss *session, r *http.Request) (any, error) {
	var req editReq
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	c, err := ss.result(req.ResultID)
	if err != nil {
		return nil, err
	}
	changes, err := buildChanges(c, req)
	if err != nil {
		return nil, err
	}
	stmts, err := ss.o.Sess.Plan(changes)
	if err != nil {
		return nil, bad("%v", err)
	}
	return ss.storePlan(stmts, req.ResultID)
}

func (s *Server) planAdmin(ss *session, r *http.Request) (any, error) {
	var req struct {
		Op   db.AdminOp
		Ref  db.ObjRef
		Opts map[string]string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	stmts, err := ss.o.Sess.PlanAdmin(req.Op, req.Ref, req.Opts)
	if err != nil {
		return nil, bad("%v", err)
	}
	return ss.storePlan(stmts, "")
}

// apply runs a stored plan after re-checking the gate. Only statements the server planned
// can run through here; ad-hoc SQL goes through exec, which is gated the same way.
func (s *Server) apply(ss *session, r *http.Request) (any, error) {
	var req struct {
		PlanID    string
		Confirmed bool
		Password  string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	// lookup, the applying guard and consuming the plan are one critical section (a concurrent
	// apply of the same plan must not slip in between); a failed gate puts the plan back.
	ss.mu.Lock()
	stmts, ok := ss.plans[req.PlanID]
	if !ok {
		ss.mu.Unlock()
		return nil, bad("unknown or already applied plan; preview again")
	}
	if ss.applying {
		ss.mu.Unlock()
		return nil, &apiErr{Code: http.StatusConflict, Msg: "a change is still being sent; wait for it to finish"}
	}
	ss.applying = true
	src := ss.planSrc[req.PlanID]
	delete(ss.plans, req.PlanID)
	delete(ss.planSrc, req.PlanID)
	ss.mu.Unlock()
	defer func() { ss.mu.Lock(); ss.applying = false; ss.mu.Unlock() }()
	if err := ss.check(ss.gate(stmts), req.Confirmed, req.Password); err != nil {
		ss.mu.Lock()
		ss.plans[req.PlanID] = stmts
		if src != "" {
			ss.planSrc[req.PlanID] = src
		}
		ss.mu.Unlock()
		return nil, err
	}

	start := time.Now()
	// a commit must never be cut off half way by a client disconnect: detach from the request's
	// cancellation, but bound it so a lock wait cannot wedge the session forever
	actx, acancel := context.WithTimeout(context.WithoutCancel(r.Context()), applyTimeout)
	defer acancel()
	if err := ss.o.Sess.Apply(actx, stmts); err != nil {
		return nil, &apiErr{Code: http.StatusBadRequest, Msg: err.Error(), Extra: map[string]any{
			"partial": !ss.o.Sess.Caps().Transactions || hasDDL(stmts),
		}}
	}
	dur := time.Since(start)
	for _, st := range stmts {
		ss.record(st.Shown(), "gui", dur/time.Duration(len(stmts)))
	}
	ss.clearColCache() // structure may have changed
	if src != "" {     // the snapshot is stale now; a retry must reload instead of re-planning old rows
		ss.dropResult(src)
	}
	return map[string]any{"ok": true, "durationMs": dur.Milliseconds(), "statements": len(stmts)}, nil
}

const applyTimeout = 5 * time.Minute

var ddlWords = []string{"CREATE", "ALTER", "DROP", "TRUNCATE", "RENAME"}

func hasDDL(stmts []db.Statement) bool {
	for _, st := range stmts {
		t := strings.ToUpper(strings.TrimSpace(st.Text))
		for _, w := range ddlWords {
			if strings.HasPrefix(t, w) {
				return true
			}
		}
	}
	return false
}

func atoi(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}
