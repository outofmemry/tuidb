package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/export"
)

// Wave C: meta extras, pinned query tabs, load more by page, table edits (foreign key, column order),
// assistant models, board duplicate, export file names.

// metaExtra is what the start page and the dialogs need beyond the config sections: file based drivers,
// the tags with their colours, the MCP listener, the export file-name pattern and the assistant provider.
func (s *Server) metaExtra() map[string]any {
	c := s.o.Cfg
	fb := []string{}
	for _, d := range config.Drivers {
		if config.FileBased(d) {
			fb = append(fb, d)
		}
	}
	colors := config.Tags(c.Tags)
	tags := []map[string]string{}
	for _, n := range config.DefaultTags {
		tags = append(tags, map[string]string{"name": n, "color": colors[n]})
	}
	for _, t := range c.Tags {
		if _, isDefault := indexOf(config.DefaultTags, t.Name); !isDefault {
			tags = append(tags, map[string]string{"name": t.Name, "color": t.Color})
		}
	}
	dir := ""
	if s.o.Queries != nil {
		dir = s.o.Queries.Root
	}
	return map[string]any{
		"fileBased": fb, "tags": tags, "queriesDir": dir,
		"mcp":        map[string]any{"enabled": c.MCP.Enabled, "listen": s.mcpAddr(), "tls": c.MCP.TLSCert != ""},
		"export":     map[string]any{"filename": c.Export.Filename, "header": c.Export.Header},
		"ai":         map[string]any{"provider": c.AI.Provider, "model": c.AI.Model, "providers": config.AIProviderNames(c.AI)},
		"csvWarning": export.CheckCSV(c.CSV),
	}
}

func indexOf(l []string, v string) (int, bool) {
	for i, x := range l {
		if x == v {
			return i, true
		}
	}
	return -1, false
}

// mcpAddr is where the MCP listener binds: [mcp] listen, else 127.0.0.1:port.
func (s *Server) mcpAddr() string {
	if l := s.o.Cfg.MCP.Listen; l != "" {
		return l
	}
	return fmt.Sprintf("127.0.0.1:%d", s.o.Cfg.MCP.Port)
}

// ---- export file names ----------------------------------------------------------------------------------

// exportName expands a file name pattern (default [export] filename; {connection} {database} {table} {tag}
// {date} {time}) with export.Filename and adds the extension unless the pattern already ends with it.
func (ss *session) exportName(pattern, table, ext string) string {
	if pattern == "" {
		pattern = ss.srv.o.Cfg.Export.Filename
	}
	if pattern == "" {
		pattern = "{table}-{date}"
	}
	name := export.Filename(pattern, map[string]string{"connection": ss.conn.Name, "database": ss.o.Sess.CurrentDatabase(), "table": table, "tag": ss.conn.Tag})
	if strings.HasSuffix(strings.ToLower(name), "."+strings.ToLower(ext)) {
		return name
	}
	return name + "." + ext
}

// ---- pinned query tabs ----------------------------------------------------------------------------------

const maxPins = 8

// pinTTL is how long a pinned connection may sit unused before it is released (an open transaction holds locks).
var pinTTL = 10 * time.Minute

func (ss *session) sweepPins(ttl time.Duration) {
	ss.pinMu.Lock()
	var old []db.Session
	for tab, p := range ss.pins {
		if time.Since(ss.pinUsed[tab]) > ttl {
			old = append(old, p)
			delete(ss.pins, tab)
			delete(ss.pinUsed, tab)
			delete(ss.pinTx, tab)
		}
	}
	ss.pinMu.Unlock()
	for _, p := range old {
		p.Close()
		ss.srv.con.add(ss.conn.Name, "-- a pinned query tab idle for "+ttl.String()+" was closed; anything it had open (BEGIN, temp tables) was rolled back")
	}
}

func isPinner(s db.Session) bool { _, ok := s.(db.Pinner); return ok }

// pinned is the session a query tab runs on: its dedicated connection (made on first use) when the tab
// is pinned, else the pool. BEGIN, SET and temp tables then carry over between the tab's statements.
// shortcut: a database switch leaves a pinned connection on the old database; unpin the tab and run again.
func (ss *session) pinned(tab string) (db.Session, error) {
	if tab == "" {
		return ss.o.Sess, nil
	}
	ss.pinMu.Lock()
	defer ss.pinMu.Unlock()
	if ss.pinUsed == nil {
		ss.pinUsed = map[string]time.Time{}
	}
	ss.pinUsed[tab] = time.Now()
	if p := ss.pins[tab]; p != nil {
		return p, nil
	}
	pn, ok := ss.o.Sess.(db.Pinner)
	if !ok {
		return nil, bad("%s cannot pin a connection to a tab", ss.conn.Driver)
	}
	if len(ss.pins) >= maxPins {
		return nil, bad("%d tabs are already pinned; unpin one first", maxPins)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, err := pn.Pin(ctx)
	if err != nil {
		return nil, bad("%v", err)
	}
	if ss.pins == nil {
		ss.pins = map[string]db.Session{}
	}
	ss.pins[tab] = p
	return p, nil
}

// pinState: auto = BEGIN pinned the tab (the pin is released again at COMMIT / ROLLBACK); tx = a transaction is
// open, guessed from the statements for drivers that cannot say (db.TxReporter).
type pinState struct{ auto, tx bool }

// autoPin gives an unpinned tab its own connection when the batch starts with BEGIN / START TRANSACTION, so the
// transaction carries over to the tab's next run instead of autocommitting on another pooled connection.
func (ss *session) autoPin(tab, pin string, stmts []db.Stmt) (string, bool) {
	if pin != "" || tab == "" || len(stmts) == 0 || !isPinner(ss.o.Sess) || db.TxVerb(ss.o.Sess.Caps().Dialect, stmts[0].Text) != 1 {
		return pin, false
	}
	return tab, true
}

// afterRun records what ran on a pinned tab and answers whether it is still pinned and holds a transaction; a pin
// that BEGIN made is released once the transaction is over.
func (ss *session) afterRun(pin string, auto bool, ran []string) (pinned, inTx bool) {
	if pin == "" {
		return false, false
	}
	ss.pinMu.Lock()
	p, st := ss.pins[pin], ss.pinTx[pin]
	st.auto = st.auto || auto
	ss.pinMu.Unlock()
	if p == nil {
		return false, false
	}
	st.tx = db.InTx(p, db.TxAfter(ss.o.Sess.Caps().Dialect, st.tx, ran))
	if st.auto && !st.tx {
		ss.releasePin(pin)
		return false, false
	}
	ss.pinMu.Lock()
	if ss.pinTx == nil {
		ss.pinTx = map[string]pinState{}
	}
	ss.pinTx[pin] = st
	ss.pinMu.Unlock()
	return true, st.tx
}

func (ss *session) releasePin(tab string) {
	ss.pinMu.Lock()
	p := ss.pins[tab]
	delete(ss.pins, tab)
	delete(ss.pinTx, tab)
	ss.pinMu.Unlock()
	if p != nil {
		p.Close() // ends an open transaction
	}
}

func (ss *session) releasePins() {
	ss.pinMu.Lock()
	ps := ss.pins
	ss.pins, ss.pinTx = nil, nil
	ss.pinMu.Unlock()
	for _, p := range ps {
		p.Close()
	}
}

// pinTab: POST {tab} pins the tab now (so the toggle shows the truth), DELETE releases it.
func (s *Server) pinTab(ss *session, r *http.Request) (any, error) {
	if r.Method == http.MethodDelete {
		ss.releasePin(r.PathValue("tab"))
		return nil, nil
	}
	_, err := ss.pinned(r.PathValue("tab"))
	return nil, err
}

// ---- load more by page ----------------------------------------------------------------------------------

// execPage fetches rows offset..offset+limit of one read-only SELECT (db.PageStatement) so "load more"
// does not re-run the statement with a doubled limit. supported=false: the caller falls back to /exec.
func (s *Server) execPage(ss *session, r *http.Request) (any, error) {
	var req struct {
		Text          string
		Limit, Offset int
		Pin           string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	req.Limit = ss.clampLimit(req.Limit)
	stmt, ok := db.PageStatement(ss.o.Sess.Caps().Dialect, req.Text, req.Limit, req.Offset)
	if !ok {
		return map[string]any{"supported": false}, nil
	}
	sess, err := ss.pinned(req.Pin)
	if err != nil {
		return nil, err
	}
	ctx, cancel := ss.ctxNoTimeout(r)
	defer cancel()
	start := time.Now()
	res, err := sess.Exec(ctx, stmt, 0)
	if err != nil {
		return nil, bad("%v", err)
	}
	ss.record(stmt, "editor", time.Since(start), "")
	out := toJSON(res, nil)
	// pages are never editable: editableResult would cache a result nobody uses and evict live ones
	return map[string]any{"supported": true, "result": out, "more": len(res.Rows) >= req.Limit}, nil
}

// ---- table edits: foreign key by drag, column order ---------------------------------------------------

// planTableEdit plans one structure edit of an existing table through the driver's PlanTable and stores
// the plan for /apply (which holds the safe-mode gate):
//
//	{ref, op: "add_fk", fk: {Name, Columns, RefSchema, RefTable, RefColumns, OnUpdate, OnDelete}}
//	{ref, op: "move_column", column, to}   // to = the new 0-based position; engines with ReorderColumns only
func (s *Server) planTableEdit(ss *session, r *http.Request) (any, error) {
	var req struct {
		Ref    db.ObjRef
		Op     string
		FK     db.ForeignKey
		Column string
		To     int
		Col    db.Column // alter_column: the new Name, Type, Nullable, Default and HasDef of Column
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if req.Ref.Name == "" {
		return nil, bad("ref.name is required")
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	old, err := ss.tableDef(ctx, req.Ref, req.Ref.Schema)
	if err != nil {
		return nil, bad("%v", err)
	}
	nw := old
	nw.Columns = append([]db.Column(nil), old.Columns...)
	for i := range nw.Columns {
		nw.Columns[i].OrigName = nw.Columns[i].Name // the planners keep a column only when its OrigName names an existing one; "" means a new column
	}
	nw.ForeignKeys = append([]db.ForeignKey(nil), old.ForeignKeys...)
	caps := ss.o.Sess.Caps()
	has := func(n string) int {
		for i, c := range old.Columns {
			if c.Name == n {
				return i
			}
		}
		return -1
	}
	switch req.Op {
	case "add_fk":
		fk := req.FK
		if !caps.ForeignKeys {
			return nil, bad("%s has no foreign keys", ss.conn.Driver)
		}
		if len(fk.Columns) == 0 || len(fk.Columns) != len(fk.RefColumns) || fk.RefTable == "" {
			return nil, bad("a foreign key needs matching columns and referenced columns, and a referenced table")
		}
		for _, c := range fk.Columns {
			if has(c) < 0 {
				return nil, bad("no column %q in %s", c, req.Ref.Name)
			}
		}
		if fk.Name == "" {
			fk.Name = "fk_" + req.Ref.Name + "_" + strings.Join(fk.Columns, "_")
		}
		for _, o := range old.ForeignKeys {
			if o.Name == fk.Name {
				return nil, bad("a foreign key named %q already exists", fk.Name)
			}
		}
		for _, f := range []*string{&fk.OnUpdate, &fk.OnDelete} {
			if *f == "" {
				*f = "NO ACTION"
			}
		}
		nw.ForeignKeys = append(nw.ForeignKeys, fk)
	case "alter_column":
		i := has(req.Column)
		if i < 0 || strings.TrimSpace(req.Col.Name) == "" || strings.TrimSpace(req.Col.Type) == "" {
			return nil, bad("no column %q, or the new name or type is empty", req.Column)
		}
		if j := has(req.Col.Name); j >= 0 && j != i {
			return nil, bad("there is already a column named %q", req.Col.Name)
		}
		c := &nw.Columns[i]
		c.Name, c.Type, c.Nullable = req.Col.Name, req.Col.Type, req.Col.Nullable
		c.Default, c.HasDef = req.Col.Default, req.Col.HasDef && req.Col.Default != "" // OrigName stays: Name != OrigName is a rename
	case "move_column":
		if !caps.ReorderColumns {
			return nil, bad("%s cannot reorder columns", ss.conn.Driver)
		}
		from := has(req.Column)
		if from < 0 || req.To < 0 || req.To >= len(old.Columns) {
			return nil, bad("no column %q or position %d out of range", req.Column, req.To)
		}
		if from == req.To {
			return nil, bad("the column is already there")
		}
		c := nw.Columns[from]
		nw.Columns = append(nw.Columns[:from:from], nw.Columns[from+1:]...)
		nw.Columns = append(nw.Columns[:req.To:req.To], append([]db.Column{c}, nw.Columns[req.To:]...)...)
	default:
		return nil, bad("unknown op %q", req.Op)
	}
	stmts, err := ss.o.Sess.PlanTable(&old, &nw)
	if err != nil {
		return nil, bad("%v", err)
	}
	if len(stmts) == 0 {
		return nil, bad("nothing to change")
	}
	return ss.storePlan(stmts, "", "")
}

// ---- assistant models -----------------------------------------------------------------------------------

// assistantModels asks the configured provider which models it offers (OpenAI-compatible GET /models,
// Anthropic GET /v1/models), so the picker lists real names. The model chosen travels with the next /assistant call.
func (s *Server) assistantModels(r *http.Request) (any, error) {
	cfg, err := config.AIFor(s.o.Cfg.AI, r.URL.Query().Get("provider"))
	if err != nil {
		return nil, bad("%v", err)
	}
	key, err := config.ResolveAIKey(cfg)
	if err != nil {
		return nil, bad("%v", err)
	}
	if key == "" && (cfg.Provider != "openai" || cfg.BaseURL == "") {
		return nil, bad("no AI key: set [ai] api_key or api_key_cmd in the config")
	}
	var u string
	hdr := http.Header{}
	if cfg.Provider == "openai" {
		base := cfg.BaseURL
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		u = strings.TrimRight(base, "/") + "/models"
		if key != "" {
			hdr.Set("Authorization", "Bearer "+key)
		}
	} else {
		base := cfg.BaseURL
		if base == "" {
			base = "https://api.anthropic.com"
		}
		u = strings.TrimRight(base, "/") + "/v1/models?limit=100"
		hdr.Set("x-api-key", key)
		hdr.Set("anthropic-version", "2023-06-01")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, bad("%v", err)
	}
	req.Header = hdr
	// the key must not follow a redirect to another host
	client := &http.Client{CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if next.URL.Host != via[0].URL.Host {
			next.Header = http.Header{}
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, bad("%v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, bad("the provider answered %s: %.200s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var out struct{ Data []struct{ ID string } }
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, bad("unexpected answer from the provider: %.200s", raw)
	}
	ids := []string{}
	for _, m := range out.Data {
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	return map[string]any{"provider": cfg.Provider, "current": cfg.Model, "models": ids}, nil
}

// ---- boards ------------------------------------------------------------------------------------------

// boardDuplicate copies a board under a new name (the panels and variables; a board is never overwritten).
func (s *Server) boardDuplicate(r *http.Request) (any, error) {
	var req struct{ Name, NewName string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.NewName) == "" {
		return nil, bad("a name for the copy is required")
	}
	b, err := findBoard(req.Name)
	if err != nil {
		return nil, err
	}
	b.Name, b.Path = strings.TrimSpace(req.NewName), ""
	if err := b.Save(); err != nil {
		return nil, bad("%v", err)
	}
	return nil, nil
}
