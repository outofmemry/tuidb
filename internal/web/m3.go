package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"webtuidb/internal/ai"
	"webtuidb/internal/backup"
	"webtuidb/internal/board"
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/diagram"
	"webtuidb/internal/export"
	"webtuidb/internal/mcp"
	"webtuidb/internal/plugin"
)

// Milestone 3: users, backup/restore, diagram, metrics boards, plugins, assistant, MCP log.

// ---- users -----------------------------------------------------------------------------------

func (ss *session) userAdmin() (db.UserAdmin, error) {
	if ua, ok := ss.o.Sess.(db.UserAdmin); ok && ss.o.Sess.Caps().Users {
		return ua, nil
	}
	return nil, bad("not supported by %s", ss.conn.Driver)
}

func (s *Server) users(ss *session, r *http.Request) (any, error) {
	ua, err := ss.userAdmin()
	if err != nil {
		return map[string]any{"supported": false, "driver": ss.conn.Driver, "users": []db.User{}}, nil
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	l, err := ua.Users(ctx)
	if err != nil {
		return nil, bad("%v", err)
	}
	if l == nil {
		l = []db.User{}
	}
	return map[string]any{"supported": true, "users": l}, nil
}

// usersPlan only plans: the client sends the statements through /apply, which holds the safe-mode gate.
func (s *Server) usersPlan(ss *session, r *http.Request) (any, error) {
	var ch db.UserChange
	if err := decode(r, &ch); err != nil {
		return nil, err
	}
	ua, err := ss.userAdmin()
	if err != nil {
		return nil, err
	}
	stmts, err := ua.PlanUser(ch)
	if err != nil {
		return nil, bad("%v", err)
	}
	return ss.storePlan(stmts, "", "")
}

// ---- backup / restore --------------------------------------------------------------------------

type backupReq struct {
	File                 string
	Database             string
	Gzip                 bool
	Format               string
	DataOnly, SchemaOnly bool
	NewDatabase          string            // restore: create this database first (through the safe-mode gate) and restore into it
	CreateOpts           map[string]string // encoding, collation for NewDatabase
	Run                  bool              // false = only show the command line
	Confirmed            bool
	Password             string
	Extra                []string // free-form tool arguments, appended last (backup.Options.Extra)
	Filename             string   // a name pattern ({connection} {database} {table}=database {tag} {date} {time}) used when File is empty
}

func (s *Server) backup(ss *session, r *http.Request) (any, error) {
	return s.backupRestore(ss, r, false)
}
func (s *Server) restore(ss *session, r *http.Request) (any, error) {
	return s.backupRestore(ss, r, true)
}

// backupFile confines a backup/restore file to <DataDir>/backups: no subdirectories, no "..", no symlinks.
func backupFile(f string) (path string, exists bool, err error) {
	dir := filepath.Join(config.DataDir(), "backups")
	if strings.Contains(f, "..") {
		return "", false, bad("%q: .. is not allowed", f)
	}
	p := filepath.Clean(f)
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	if filepath.Dir(p) != dir || filepath.Base(p) == "." {
		return "", false, bad("backup files live in %s; give a file name there", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", false, err
	}
	if fi, err := os.Lstat(dir); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		return "", false, bad("%s must not be a symlink", dir)
	}
	switch fi, err := os.Lstat(p); {
	case err == nil && !fi.Mode().IsRegular():
		return "", false, bad("%q is not a regular file", p)
	case err == nil:
		exists = true
	}
	return p, exists, nil
}

func (s *Server) backupRestore(ss *session, r *http.Request, restore bool) (any, error) {
	var req backupReq
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if req.File == "" {
		return nil, bad("a file path is required")
	}
	var file string
	var exists bool
	var err error
	if ss.conn.Driver == config.ClickHouse {
		// the name is on the server's "backups" disk, not a local path: no rewriting, no local existence check
		if strings.HasPrefix(req.File, "-") {
			return nil, bad("%q: a file name cannot start with -", req.File)
		}
		file = req.File
	} else if file, exists, err = backupFile(req.File); err != nil {
		return nil, err
	}
	req.File = file
	conn := ss.conn
	if conn.ReadOnly { // the dump tools log in on their own, so the session's read-only guard does not cover them
		return nil, bad("this connection is read only; backup and restore are refused")
	}
	if restore && !exists && ss.conn.Driver != config.ClickHouse {
		return nil, bad("%q does not exist in the backups folder", filepath.Base(file))
	}
	conn.Password = ss.o.Password
	if req.Database == "" {
		req.Database = ss.o.Sess.CurrentDatabase()
	}
	tool := backup.ToolForConn(conn, restore)
	if !restore {
		req.NewDatabase = ""
	}
	cmd, err := backup.Plan(backup.Options{Conn: conn, Database: req.Database, File: req.File, Gzip: req.Gzip, Format: req.Format, DataOnly: req.DataOnly, SchemaOnly: req.SchemaOnly, NewDatabase: req.NewDatabase}, restore)
	if err != nil {
		if !tool.Found && !req.Run { // let the dialog name the missing executable
			return map[string]any{"display": "", "tool": tool, "levelLabel": ss.lvl().Label()}, nil
		}
		return nil, bad("%v", err)
	}
	display := backup.Display(cmd)
	info := map[string]any{"display": display, "tool": tool}
	gated := []db.Statement{{Text: display}}
	var create []db.Statement
	if req.NewDatabase != "" { // the new database is created first, through the same gate
		if create, err = ss.o.Sess.PlanAdmin(db.OpCreateDatabase, db.ObjRef{Name: req.NewDatabase}, req.CreateOpts); err != nil {
			return nil, bad("%v", err)
		}
		gated = append(create, gated...)
		info["create"] = create
	}
	if !req.Run {
		info["gate"] = ss.gate(gated)
		info["exists"] = exists
		info["levelLabel"] = ss.lvl().Label()
		return info, nil
	}
	if !tool.Found {
		return nil, bad("%s is not on PATH", tool.Name)
	}
	// a restore overwrites data and a backup overwrites a file: both are gated like a destructive statement
	if err := ss.check(ss.gate(gated), req.Confirmed, req.Password); err != nil {
		return nil, err
	}
	if len(create) > 0 {
		// detached like /apply: a closed tab must not cancel a CREATE DATABASE that may already have committed
		cctx, ccancel := context.WithTimeout(context.WithoutCancel(r.Context()), applyTimeout)
		defer ccancel()
		if err := ss.o.Sess.Apply(cctx, create); err != nil {
			return nil, bad("create %s: %v", req.NewDatabase, err)
		}
		ss.record(create[0].Shown(), "gui", 0, "")
	}
	if st := s.o.State; st != nil { // remembered per connection for the next time (M7)
		pre := "backup." // same keys as the terminal UI; a restore file name is never remembered
		mem := map[string]string{pre + "gzip": strconv.FormatBool(req.Gzip), pre + "format": req.Format, pre + "data": strconv.FormatBool(req.DataOnly), pre + "schema": strconv.FormatBool(req.SchemaOnly)}
		if restore {
			pre = "restore."
			mem = map[string]string{pre + "gzip": strconv.FormatBool(req.Gzip), pre + "format": req.Format}
		} else {
			mem[pre+"file"] = filepath.Base(req.File)
		}
		st.SetBackupOptions(ss.conn.Name, mem)
	}
	if exists && !restore && !req.Confirmed {
		return nil, &apiErr{Code: http.StatusConflict, Msg: "the backup file already exists; confirm to overwrite it", Extra: map[string]any{"exists": true}}
	}
	if err := ss.jobSlot(); err != nil {
		return nil, err
	}
	j := &job{}
	info["job"] = ss.startJob(j, func(ctx context.Context) error {
		err := backup.Run(ctx, cmd, func(line string) {
			j.mu.Lock()
			defer j.mu.Unlock()
			if len(j.lines) < 5000 { // shortcut: output past 5000 lines is dropped
				j.lines = append(j.lines, line)
			}
		})
		if err == nil {
			ss.clearColCache()
			ss.record(display, "backup", 0, "")
		}
		if err != nil && len(create) > 0 { // shortcut: not dropped automatically (that would need its own gate); say so
			err = fmt.Errorf("%w (database %s was created before the restore and is still there; drop it if you do not want it)", err, req.NewDatabase)
		}
		return err
	})
	return info, nil
}

// ---- diagram -------------------------------------------------------------------------------------

func (s *Server) diagram(ss *session, r *http.Request) (any, error) {
	ctx, cancel := ss.ctx(r)
	defer cancel()
	schema := r.URL.Query().Get("schema")
	if schema == "" {
		schema = ss.o.Sess.CurrentSchema()
	}
	tabs, err := diagram.Load(ctx, ss.o.Sess, schema)
	if err != nil {
		return nil, bad("%v", err)
	}
	cl := diagram.Cluster(tabs)
	type fkJSON struct {
		Columns    []string `json:"columns"`
		RefSchema  string   `json:"refSchema"`
		RefTable   string   `json:"refTable"`
		RefColumns []string `json:"refColumns"`
	}
	type tabJSON struct {
		Ref     db.ObjRef `json:"ref"`
		Columns []colJSON `json:"columns"`
		FKs     []fkJSON  `json:"fks"`
		Cluster int       `json:"cluster"`
	}
	out := make([]tabJSON, len(tabs))
	for i, t := range tabs {
		o := tabJSON{Ref: t.Ref, Columns: []colJSON{}, FKs: []fkJSON{}, Cluster: cl[t.Ref.Schema+"."+t.Ref.Name]}
		for _, c := range t.Columns {
			o.Columns = append(o.Columns, colJSON{Name: c.Name, Type: c.Type, PK: c.PK})
		}
		for _, f := range t.FKs {
			o.FKs = append(o.FKs, fkJSON{f.Columns, f.RefSchema, f.RefTable, f.RefColumns})
		}
		out[i] = o
	}
	return map[string]any{"schema": schema, "tables": out}, nil
}

// ---- metrics boards -------------------------------------------------------------------------------

func boardJSON(b board.Board) map[string]any {
	e := ""
	if b.Err != nil {
		e = b.Err.Error()
	}
	return map[string]any{"name": b.Name, "refreshSecs": b.RefreshSecs, "connection": b.Connection, "vars": nonNil(b.Vars), "panels": nonNil(b.Panels), "error": e}
}

func nonNil[T any](l []T) []T {
	if l == nil {
		return []T{}
	}
	return l
}

func (s *Server) boards(r *http.Request) (any, error) {
	l, err := board.List()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, b := range l {
		out = append(out, boardJSON(b))
	}
	return out, nil
}

func findBoard(name string) (board.Board, error) {
	l, err := board.List()
	if err != nil {
		return board.Board{}, err
	}
	for _, b := range l {
		if b.Name == name && b.Err == nil {
			return b, nil
		}
	}
	return board.Board{}, bad("no board named %q", name)
}

// boardSave adds a chart to the board of that name (creating it), so an existing board is never overwritten.
func (s *Server) boardSave(r *http.Request) (any, error) {
	var req struct {
		Name, Query string
		Kind, X, Y  string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Query) == "" {
		return nil, bad("a board name and a query are required")
	}
	spec, _ := db.ParseChart(req.Query) // a "-- barchart x: a, y: b" comment names the columns
	for _, f := range []struct {
		to *string
		v  string
	}{{&spec.Kind, req.Kind}, {&spec.X, req.X}, {&spec.Y, req.Y}} {
		if f.v != "" {
			*f.to = f.v
		}
	}
	if req.Y != "" { // an explicit y replaces the comment's series; FromChart prefers Ys
		spec.Ys = []string{req.Y}
	}
	nb := board.FromChart(req.Name, req.Query, spec)
	if old, err := findBoard(req.Name); err == nil {
		old.Panels = append(old.Panels, nb.Panels...)
		nb = old
	}
	if err := nb.Save(); err != nil {
		return nil, bad("%v", err)
	}
	return nil, nil
}

func (s *Server) boardRun(ss *session, r *http.Request) (any, error) {
	var req struct {
		Name string
		Vals map[string]string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	b, err := findBoard(req.Name)
	if err != nil {
		return nil, err
	}
	dialect := ss.o.Sess.Caps().Dialect
	for _, p := range b.Panels { // a board file must not become a write path around safe mode
		if !panelRuns(p) {
			continue
		}
		if !db.IsReadOnly(dialect, p.Query) {
			return nil, bad("panel %q is not read-only; boards only run read-only queries", p.Title)
		}
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	out := []panelOut{}
	for _, pr := range board.Run(ctx, ss.o.Sess, b, req.Vals, ss.rowLimit()) {
		out = append(out, toPanelOut(pr))
	}
	return map[string]any{"panels": out}, nil
}

// ---- plugins -----------------------------------------------------------------------------------------

func (s *Server) plugins(r *http.Request) (any, error) {
	l, err := plugin.List()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, p := range l {
		e := ""
		if p.Err != nil {
			e = p.Err.Error()
		}
		out = append(out, map[string]any{"name": p.Name, "contexts": nonNil(p.Contexts), "output": p.Output, "error": e})
	}
	return out, nil
}

func (s *Server) pluginRun(ss *session, r *http.Request) (any, error) {
	var req struct {
		Name, Context, Selection, Cell string
		Object                         *db.ObjRef
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	l, err := plugin.List() // the command comes from the plugin file, never from the browser
	if err != nil {
		return nil, err
	}
	for _, p := range l {
		if p.Name != req.Name || p.Err != nil {
			continue
		}
		ctx, cancel := ss.ctx(r)
		defer cancel()
		txt, err := p.Run(ctx, plugin.Context{Connection: ss.conn.Name, Driver: ss.conn.Driver, Database: ss.o.Sess.CurrentDatabase(), Context: req.Context, Object: req.Object, Selection: req.Selection, Cell: req.Cell})
		if err != nil {
			return nil, bad("%v", err)
		}
		return map[string]any{"output": p.Output, "text": txt}, nil
	}
	return nil, bad("no plugin named %q", req.Name)
}

// ---- assistant ----------------------------------------------------------------------------------------

func (s *Server) assistant(ss *session, r *http.Request) (any, error) {
	var req struct {
		Messages []ai.Message
		Model    string // overrides [ai] model for this call (the picker)
		Provider string // a [ai.providers.<name>]; "" = the default (M7)
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if len(req.Messages) == 0 || req.Messages[len(req.Messages)-1].Role != "user" {
		return nil, bad("the last message must be from the user")
	}
	cfg, err := config.AIFor(s.o.Cfg.AI, req.Provider)
	if err != nil {
		return nil, bad("%v", err)
	}
	key, err := config.ResolveAIKey(cfg)
	if err != nil {
		return nil, bad("%v", err)
	}
	if key == "" && (cfg.Provider != "openai" || cfg.BaseURL == "") { // a local OpenAI-compatible server (Ollama) needs no key
		return nil, bad("no AI key: set [ai] api_key or api_key_cmd in the config")
	}
	sess := ss.o.Sess
	ddl := map[string]string{}
	var names []string
	for _, m := range req.Messages {
		if m.Role == "user" {
			names = append(names, ai.Mentions(m.Content)...)
		}
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	for _, n := range names {
		ref := db.ObjRef{Schema: sess.CurrentSchema(), Name: n, Kind: db.KindTable}
		if i := strings.Index(n, "."); i > 0 {
			ref.Schema, ref.Name = n[:i], n[i+1:]
		}
		if t, err := sess.DDL(ctx, ref); err == nil { // an unknown mention is just prose
			ddl[n] = t
		}
	}
	if req.Model != "" {
		cfg.Model = req.Model
	}
	reply, err := ai.New(cfg, key).Chat(ctx, ai.SystemPrompt(sess.Caps().Dialect, sess.CurrentDatabase(), ddl), req.Messages)
	if err != nil {
		return nil, bad("%v", err)
	}
	dialect := sess.Caps().Dialect
	type block struct {
		SQL      string `json:"sql"`
		ReadOnly bool   `json:"readOnly"`
	}
	blocks := []block{}
	for _, b := range ai.SQLBlocks(reply) {
		blocks = append(blocks, block{b, db.IsReadOnly(dialect, b)})
	}
	return map[string]any{"reply": reply, "blocks": blocks}, nil
}

// ---- MCP ------------------------------------------------------------------------------------------------

func (s *Server) mcpLog(r *http.Request) (any, error) {
	if r.URL.Query().Get("format") == "csv" {
		return s.mcpLogCSV()
	}
	if s.mcp == nil {
		return map[string]any{"enabled": false, "entries": []any{}}, nil
	}
	type ent struct {
		Time       string `json:"time"`
		Token      string `json:"token"`
		Tool       string `json:"tool"`
		Connection string `json:"connection"`
		SQL        string `json:"sql"`
		Err        string `json:"error"`
	}
	out := []ent{}
	for _, e := range s.mcp.Log() {
		out = append(out, ent{e.Time.Format("15:04:05"), e.Token, e.Tool, e.Connection, e.SQL, e.Err})
	}
	return map[string]any{"enabled": true, "port": s.o.Cfg.MCP.Port, "listen": s.mcpAddr(), "tls": s.o.Cfg.MCP.TLSCert != "", "entries": out}, nil
}

// ---- MCP approvals ---------------------------------------------------------------------------

const approvalTimeout = 5 * time.Minute

type pendingApproval struct {
	ID         string `json:"id"`
	Token      string `json:"token"`
	Connection string `json:"connection"`
	SQL        string `json:"sql"`
	ch         chan bool
}

type approvals struct {
	mu    sync.Mutex
	items []*pendingApproval
}

// ask is mcp.Server.Approver: it parks the statement until the browser answers, ctx ends or 5 minutes pass.
func (a *approvals) ask(ctx context.Context, ap mcp.Approval) (bool, error) {
	p := &pendingApproval{ID: randID(8), Token: ap.Token, Connection: ap.Connection, SQL: ap.SQL, ch: make(chan bool, 1)}
	a.mu.Lock()
	a.items = append(a.items, p)
	a.mu.Unlock()
	defer a.remove(p.ID)
	t := time.NewTimer(approvalTimeout)
	defer t.Stop()
	select {
	case ok := <-p.ch:
		return ok, nil
	case <-ctx.Done():
		return false, ctx.Err()
	case <-t.C:
		return false, errors.New("approval timed out after 5 minutes")
	}
}

func (a *approvals) remove(id string) *pendingApproval {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, p := range a.items {
		if p.ID == id {
			a.items = append(a.items[:i:i], a.items[i+1:]...)
			return p
		}
	}
	return nil
}

func (a *approvals) list() []*pendingApproval {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*pendingApproval{}, a.items...)
}

func (s *Server) answerApproval(r *http.Request) (any, error) {
	var req struct{ Approve bool }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	p := s.ap.remove(r.PathValue("id"))
	if p == nil {
		return nil, bad("no such pending approval (answered or timed out)")
	}
	p.ch <- req.Approve // buffered, so it never blocks
	return nil, nil
}

// ---- editor folds / statement offsets -----------------------------------------------------------

func foldKey(r *http.Request) (string, error) {
	k := r.URL.Query().Get("key")
	if k == "" {
		return "", bad("key is required")
	}
	return k, nil
}

func (s *Server) getFolds(r *http.Request) (any, error) {
	k, err := foldKey(r)
	if err != nil {
		return nil, err
	}
	f := s.o.State.Folds(k)
	if f == nil {
		f = []int{}
	}
	return f, nil
}

func (s *Server) putFolds(r *http.Request) (any, error) {
	k, err := foldKey(r)
	if err != nil {
		return nil, err
	}
	var f []int
	if err := decode(r, &f); err != nil {
		return nil, err
	}
	return nil, s.o.State.SetFolds(k, f)
}

// split returns each statement's [from,to] as UTF-16 offsets (what CodeMirror uses), cut by the same db.Split as the terminal.
func (s *Server) split(r *http.Request) (any, error) {
	var req struct{ Text, Dialect string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	u := func(b int) int { return len(utf16.Encode([]rune(req.Text[:b]))) }
	out := [][2]int{}
	for _, st := range db.Split(req.Dialect, req.Text) {
		out = append(out, [2]int{u(st.Start), u(st.End)})
	}
	return out, nil
}
