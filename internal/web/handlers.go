package web

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/menus"
	"webtuidb/internal/safemode"
	"webtuidb/internal/store"
)

var dialects = []string{"postgres", "mysql", "clickhouse", "redis", "mongo", "sqlite", "duckdb", "cql", "partiql"}

// ---- meta and connections ---------------------------------------------------------

func (s *Server) meta(r *http.Request) (any, error) {
	ops := map[string][]string{}
	for _, d := range dialects {
		ops[d] = db.Operators(d)
	}
	c := s.cfg()
	// The web UI runs the same prefix state machine as the terminal, so it gets the resolved bindings
	// (plugin shortcuts included), in keymap table order.
	km, conflicts, err := c.KeymapChecked(pluginBindings())
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"drivers": config.Drivers, "sslModes": config.SSLModes, "safeModes": safemode.All, "operators": ops,
		"config":             map[string]any{"general": section(c.General), "editor": section(c.Editor), "table": section(c.Table), "sidebar": section(c.Sidebar), "csv": c.CSV},
		"theme":              webTheme(c.Theme),
		"web":                map[string]any{"fontSize": c.Web.FontSize, "lineHeight": c.Web.LineHeight, "notifications": c.Web.Notifications, "autoHideScrollbars": c.Web.AutoHideScrollbars, "toolbar": nonNil(c.Web.Toolbar)},
		"security":           map[string]any{"passcode": c.Security.Passcode != "", "lockAfterMins": c.Security.LockAfterMins}, // never the hash
		"menus":              menus.All(),
		"themes":             nonNil(config.InstalledThemes()),
		"passwordCmdPresets": config.PasswordCmdPresets(),
		"version":            s.o.Version,
		"keymap":             map[string]any{"prefix": km.Prefix, "bindings": km.All()},
		"pluginConflicts":    nonNil(conflicts), // plugin shortcuts that were skipped (a clash); never an error
		"login":              c.Web.Password != "",
		"query_timeout_secs": c.General.QueryTimeoutSecs, // the client prompts keep waiting / cancel after this; /exec has no hard timeout
		"rowLimit":           c.General.RowLimit,         // /rows and /exec clamp every limit to this (a connection's override is in its session info)
		"export":             map[string]any{"filename": c.Export.Filename, "header": c.Export.Header},
	}
	for k, v := range s.metaExtra() {
		out[k] = v
	}
	return out, nil
}

func (s *Server) getConns(r *http.Request) (any, error) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	s.reloadConns()
	s.setStamp(r)
	type connOut struct {
		config.Connection
		LastConnected string `json:"lastConnected"` // RFC3339 or ""
	}
	var last map[string]time.Time
	if s.o.State != nil {
		last = s.o.State.LastConnected()
	}
	out := make([]connOut, 0, len(s.o.Conns))
	for _, c := range s.o.Conns {
		o := connOut{Connection: c}
		if t, ok := last[c.Name]; ok && !t.IsZero() {
			o.LastConnected = t.Format(time.RFC3339)
		}
		out = append(out, o)
	}
	return out, nil
}

// setStamp puts the connection files' version in the ETag header. shortcut: it is a header, not a body
// field, so the array body (and the current client) keep working; PUT checks If-Match only when sent.
func (s *Server) setStamp(r *http.Request) {
	if w := rw(r); w != nil {
		w.Header().Set("ETag", fmt.Sprintf(`"%x"`, sha256.Sum256([]byte(s.stamp))))
	}
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
	s.connMu.Lock()
	defer s.connMu.Unlock()
	// GET publishes and PUT compares the same value: the stamp of the last successful (re)load
	if err := s.reloadConns(); err != nil {
		return nil, &apiErr{Code: http.StatusConflict, Msg: "connections.toml does not parse, so nothing can be saved until it is fixed: " + err.Error()}
	}
	if m := r.Header.Get("If-Match"); m != "" { // a tab that loaded the list before another one changed it must reload first
		cur := fmt.Sprintf(`"%x"`, sha256.Sum256([]byte(s.stamp)))
		if m != cur {
			return nil, &apiErr{Code: http.StatusConflict, Msg: "the connection list changed elsewhere; reload it and retry", Extra: map[string]any{"stamp": cur}}
		}
	}
	conns, fromSource, err := s.splitSources(*in)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	old := map[string]config.Connection{}
	for _, c := range s.o.Conns {
		old[c.Name] = c
	}
	for _, c := range conns {
		if err := c.Validate(); err != nil {
			return nil, bad("%v", err)
		}
		// password_cmd and bootstrap run on the server: only the file may introduce them.
		if o := old[c.Name]; c.PasswordCmd != o.PasswordCmd || !slices.Equal(c.Bootstrap, o.Bootstrap) {
			return nil, bad("%q: password_cmd and bootstrap can only be edited in connections.toml", c.Name)
		}
		if err := checkTools(c, old[c.Name]); err != nil {
			return nil, err
		}
		// hex only, as the colour menu (setColor) and the terminal (lipgloss) take no CSS names;
		// only a changed colour is checked, so a hand-edited file does not block saving the rest of the list
		if c.Color != old[c.Name].Color && !colorRe.MatchString(c.Color) {
			return nil, bad("%q: Use a colour code like #7aa2f7.", c.Name)
		}
		if names[c.Name] {
			return nil, bad("duplicate connection name %q", c.Name)
		}
		names[c.Name] = true
	}
	if s.o.ConnPath != "" {
		if err := config.SaveConnections(s.o.ConnPath, conns); err != nil {
			return nil, err
		}
		s.stamp = s.connStamp()
	}
	s.o.Conns = append(conns, fromSource...)
	s.setStamp(r)
	return nil, nil
}

func (s *Server) testConn(r *http.Request) (any, error) {
	var req struct {
		config.Connection
		AskedPassword string `json:"askedPassword"` // the typed password for a password = "ask" connection
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	c := req.Connection
	if err := c.Validate(); err != nil {
		return nil, bad("%v", err)
	}
	// The body is a whole connection: it must not be able to run a program on the server.
	if c.PasswordCmd != "" || len(c.Bootstrap) > 0 {
		return nil, bad("password_cmd and bootstrap are not accepted in a test; save the connection and open it")
	}
	if err := askPassword(&c, req.AskedPassword); err != nil {
		return nil, err
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

// askPassword fills in the typed password of a password = "ask" connection, or asks the client for it.
func askPassword(c *config.Connection, pw string) error {
	if c.Password != config.PasswordAsk {
		return nil
	}
	if pw == "" {
		return &apiErr{Code: http.StatusUnprocessableEntity, Msg: "this connection asks for its password", Extra: map[string]any{"passwordRequired": true}}
	}
	c.Password = pw
	return nil
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
		"encoding": ss.conn.Encoding, "database": sess.CurrentDatabase(), "schema": sess.CurrentSchema(),
		"level": ss.lvl(), "levelLabel": ss.lvl().Label(), "rowLimit": ss.rowLimit(),
		"color": ss.color(), "tag": ss.conn.Tag, "readOnly": ss.conn.ReadOnly, "open": ss.conn.Open, "pinnable": isPinner(sess),
		"host": ss.conn.Host, "port": ss.conn.EffectivePort(), "user": ss.conn.User, "sslMode": ss.conn.SSLMode, "ssh": ss.conn.SSH != nil && ss.conn.SSH.Host != "",
	}
}

func (s *Server) openSession(r *http.Request) (any, error) {
	var req struct{ Name, Password string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	s.connMu.Lock()
	s.reloadConns()
	s.connMu.Unlock()
	c, ok := s.findConn(req.Name)
	if !ok {
		return nil, bad("no saved connection named %q", req.Name)
	}
	if err := askPassword(&c, req.Password); err != nil {
		return nil, err
	}
	ss, err := s.newSession(r.Context(), c)
	if err != nil {
		return nil, bad("%v", err)
	}
	if s.o.State != nil { // sort "recent" in both UIs
		_ = s.o.State.TouchConnected(c.Name)
	}
	if c.Open != nil { // a URL's open target applies to this connect only
		s.connMu.Lock()
		for i := range s.o.Conns {
			if s.o.Conns[i].Name == c.Name {
				s.o.Conns[i].Open = nil
			}
		}
		s.connMu.Unlock()
	}
	out := ss.info()
	if bad := ss.runBootstrap(r.Context()); len(bad) > 0 { // the terminal shows "Bootstrap command failed (the connection is still open)"
		out["bootstrapErrors"] = bad
	}
	return out, nil
}

// runBootstrap runs the connection's bootstrap statements once the session is open, as the terminal's workspace does; it
// returns "statement\n  error" for each failure (the connection stays open), and logs every statement to the console.
// shortcut: statements run one after another on the pool, a driver that bootstraps per physical connection does that itself.
func (ss *session) runBootstrap(ctx context.Context) []string {
	var bad []string
	for _, st := range ss.conn.Bootstrap {
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		start := time.Now()
		_, err := ss.o.Sess.Exec(cctx, st, 1)
		cancel()
		ss.record(st, "bootstrap", time.Since(start), "")
		if err != nil {
			bad = append(bad, st+"\n  "+err.Error())
		}
	}
	return bad
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

// useSchema makes req.Name the session's current schema (db.SchemaSetter), as the terminal's sidebar switch does:
// the editor, search, Open anything and completion then work in it, and info().schema survives a reload.
func (s *Server) useSchema(ss *session, r *http.Request) (any, error) {
	var req struct{ Name string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if !ss.o.Sess.Caps().Schemas || strings.TrimSpace(req.Name) == "" {
		return nil, bad("this connection has no schemas to switch to")
	}
	set, ok := ss.o.Sess.(db.SchemaSetter)
	if !ok {
		return nil, bad("%s cannot switch the current schema", ss.conn.Driver)
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	if err := set.SetSchema(ctx, req.Name); err != nil {
		return nil, bad("%v", err)
	}
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
	Table      *tableInfo  `json:"table,omitempty"` // exec results: the table an editable SELECT reads (see editableResult)
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
	asked := req.Limit
	req.Limit = ss.clampLimit(req.Limit) // never above the configured limit
	ctx, cancel := ss.ctx(r)
	defer cancel()
	// columns first: Any-column filters and the PK flags need them
	cols, cerr := ss.columns(ctx, req.Ref)
	res, err := ss.o.Sess.Rows(ctx, req.query())
	if err != nil {
		return nil, bad("%v", err)
	}
	out := toJSON(res, cols)
	// the clamp cut the client's request short: a full page means the row limit hid the rest
	if req.Limit > 0 && (asked <= 0 || asked > req.Limit) && len(res.Rows) >= req.Limit {
		out.Truncated = true
	}
	// like the TUI: only plain tables with a primary key are editable (a key-less WHERE can hit many rows)
	if cerr == nil && !ss.conn.ReadOnly && res.HasRows && req.Ref.Kind == db.KindTable && hasPK(cols) {
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
	Stmt     string `json:"stmt"`
	Label    string `json:"label,omitempty"` // from a leading "-- name:" (or label: / tab:) comment: the result tab's title
	Error    string `json:"error,omitempty"`
	NoticeID string `json:"noticeId,omitempty"` // GET .../notices/{id}: the server notices (PostgreSQL RAISE NOTICE)
	resultJSON
}

// execPrep decodes an exec request and does everything before the first statement runs:
// split, bind parameters, safe-mode gate, row limit. Shared by /exec and /exec/stream.
// execRun is how a prepared batch runs: on a pinned tab's connection (pin, "" = the pool), auto when BEGIN pinned it.
type execRun struct {
	pin  string
	auto bool
}

// done reports the tab's pin and transaction state after ran (the statements that were sent) for the response.
func (x execRun) done(ss *session, ran []string) map[string]any {
	pinned, inTx := ss.afterRun(x.pin, x.auto, ran)
	return map[string]any{"pinned": pinned, "inTx": inTx, "autoPinned": x.auto && pinned}
}

func (s *Server) execPrep(ss *session, r *http.Request) (stmts []db.Stmt, limit int, sess db.Session, run execRun, err error) {
	var req struct {
		Text        string
		Limit       int // 0 = the connection's row limit, <0 = no limit
		Current     bool
		Offset      int // cursor, with Current
		Confirmed   bool
		Password    string
		Vars        map[string]string // query parameter values; nil = ask when the statements have any
		ParamFormat string            // overrides [editor] param_format for this run (the Variables dialog's picker)
		Pin         string            // a query tab's id: its statements run on one dedicated connection (see session.pinned)
		Tab         string            // the query tab's pin id even when it is not pinned: BEGIN pins it (session.autoPin)
		Syntax      string            // Mongo: "shell" | "command" | "" (either, told apart by a leading "{")
	}
	if err := decode(r, &req); err != nil {
		return nil, 0, nil, run, err
	}
	dialect := ss.o.Sess.Caps().Dialect
	if err := checkSyntax(dialect, req.Syntax, req.Text); err != nil {
		return nil, 0, nil, run, err
	}
	stmts = db.Split(dialect, req.Text)
	if req.Current {
		st, ok := db.StatementAt(stmts, req.Offset)
		if !ok {
			return nil, 0, nil, run, bad("no statement at the cursor")
		}
		stmts = []db.Stmt{st}
	}
	if len(stmts) == 0 {
		return nil, 0, nil, run, bad("nothing to run")
	}
	ed := ss.srv.o.Cfg.Editor
	if req.Vars != nil || ed.QueryParams || req.ParamFormat != "" {
		pf := db.ParamFormat(ed.ParamFormat)
		if req.ParamFormat != "" {
			pf = db.ParamFormat(req.ParamFormat)
		}
		names := paramNames(stmts, pf, dialect)
		var missing bool
		for _, n := range names {
			if _, ok := req.Vars[n]; !ok { // a nil map (no "vars") and {} both land here
				missing = true
			}
		}
		if missing {
			return nil, 0, nil, run, &apiErr{Code: http.StatusUnprocessableEntity, Msg: "the statements have query parameters", Extra: map[string]any{"params": names}}
		}
		for i := range stmts {
			stmts[i].Text = db.Bind(stmts[i].Text, pf, req.Vars, dialect)
		}
	}
	gated := make([]db.Statement, len(stmts))
	shown := make([]string, len(stmts))
	for i, st := range stmts {
		gated[i], shown[i] = db.Statement{Text: st.Text}, st.Text
	}
	if err := ss.readOnlyViolation(gated); err != nil {
		return nil, 0, nil, run, err
	}
	g := ss.gate(gated)
	if err := ss.check(g, req.Confirmed, req.Password); err != nil {
		if ae, ok := err.(*apiErr); ok {
			ae.Extra["statements"] = shown
		}
		return nil, 0, nil, run, err
	}
	limit = ss.clampLimit(req.Limit)
	run.pin, run.auto = ss.autoPin(req.Tab, req.Pin, stmts)
	if sess, err = ss.pinned(run.pin); err != nil {
		return nil, 0, nil, run, err
	}
	return stmts, limit, sess, run, nil
}

// execOne runs one statement; a failure is the item's Error, not a request error.
func (ss *session) execOne(r *http.Request, sess db.Session, st db.Stmt, limit int) execItem {
	ctx, cancel := ss.ctxNoTimeout(r) // the client's keep-waiting/cancel prompt replaces the hard timeout (M12.6)
	start := time.Now()
	res, err := sess.Exec(ctx, st.Text, limit)
	cancel()
	item := execItem{Stmt: st.Text, Label: st.Label}
	if err != nil {
		item.Error = err.Error()
		ss.srv.con.add(ss.conn.Name, "ERROR: "+err.Error()+"\n"+st.Text)
		return item
	}
	item.resultJSON = toJSON(res, nil)
	ss.editableResult(r.Context(), sess, st.Text, res, &item.resultJSON)
	item.NoticeID = ss.putNotice(res.Message)
	if !db.IsReadOnly(ss.o.Sess.Caps().Dialect, st.Text) {
		ss.clearColCache() // DDL may have changed table structure
	}
	ss.record(st.Text, "editor", time.Since(start), "")
	return item
}

func (s *Server) exec(ss *session, r *http.Request) (any, error) {
	stmts, limit, sess, run, err := s.execPrep(ss, r)
	if err != nil {
		return nil, err
	}
	var out []execItem
	var ran []string
	if len(stmts) > summarizeOver {
		out = []execItem{ss.runSummary(r, sess, stmts, limit)}
		for _, st := range stmts {
			ran = append(ran, st.Text)
		}
	} else {
		for _, st := range stmts {
			item := ss.execOne(r, sess, st, limit)
			out, ran = append(out, item), append(ran, st.Text)
			if item.Error != "" && ss.srv.o.Cfg.Editor.ReturnOnError {
				break // otherwise later statements still run, each with its own error tab
			}
		}
	}
	res := run.done(ss, ran)
	res["results"] = out
	return res, nil
}

func (ss *session) record(sql, source string, dur time.Duration, orig string) {
	ss.srv.con.add(ss.conn.Name, sql)
	if h := ss.srv.o.Hist; h != nil {
		h.Append(ss.conn.Name, store.HistoryEntry{Database: ss.o.Sess.CurrentDatabase(), SQL: sql, Duration: dur, Source: source, Original: orig})
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

func (ss *session) storePlan(stmts []db.Statement, resultID, orig string) (any, error) {
	if err := ss.readOnlyViolation(stmts); err != nil {
		return nil, err
	}
	id := randID(6)
	ss.mu.Lock()
	ss.plans[id] = stmts
	if resultID != "" {
		ss.planSrc[id] = resultID
	}
	if orig != "" {
		ss.planOrig[id] = orig
	}
	ss.mu.Unlock()
	list := make([]stmtJSON, len(stmts))
	for i, st := range stmts {
		list[i] = stmtJSON{Display: st.Shown(), ReadOnly: st.ReadOnly}
	}
	return map[string]any{
		"planId": id, "statements": list, "gate": ss.gate(stmts),
		"transactions": ss.o.Sess.Caps().Transactions, "levelLabel": ss.lvl().Label(),
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
	orig := ""
	if ss.srv.o.Cfg.General.LogOriginalValues {
		orig = db.Preimage(changes)
	}
	return ss.storePlan(stmts, req.ResultID, orig)
}

func (s *Server) planAdmin(ss *session, r *http.Request) (any, error) {
	var req struct {
		Op   db.AdminOp
		Ref  db.ObjRef
		Refs []db.ObjRef // marked sidebar items: one plan holds the statements for every one of them
		Opts map[string]string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	refs := req.Refs
	if len(refs) == 0 {
		refs = []db.ObjRef{req.Ref}
	}
	var stmts []db.Statement
	for _, ref := range refs {
		one, err := ss.o.Sess.PlanAdmin(req.Op, ref, req.Opts)
		if err != nil {
			return nil, bad("%v", err)
		}
		stmts = append(stmts, one...)
	}
	return ss.storePlan(stmts, "", "")
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
	src, orig := ss.planSrc[req.PlanID], ss.planOrig[req.PlanID]
	delete(ss.plans, req.PlanID)
	delete(ss.planSrc, req.PlanID)
	delete(ss.planOrig, req.PlanID)
	ss.mu.Unlock()
	defer func() { ss.mu.Lock(); ss.applying = false; ss.mu.Unlock() }()
	err := ss.readOnlyViolation(stmts)
	if err == nil {
		err = ss.check(ss.gate(stmts), req.Confirmed, req.Password)
	}
	if err != nil {
		ss.mu.Lock()
		ss.plans[req.PlanID] = stmts
		if src != "" {
			ss.planSrc[req.PlanID] = src
		}
		if orig != "" {
			ss.planOrig[req.PlanID] = orig
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
	for i, st := range stmts {
		o := ""
		if i == 0 { // one pre-image per commit, on its first statement
			o = orig
		}
		ss.record(st.Shown(), "gui", dur/time.Duration(len(stmts)), o)
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
