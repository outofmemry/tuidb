package web

// Milestone 9 server side: data types, editable query results, structure plans, query rename,
// search pause/resume, Mongo syntax choice.

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"webtuidb/internal/db"
	"webtuidb/internal/store"
)

// tableInfo says which table an editable query result reads, so the client can send /plan edits for it.
type tableInfo struct {
	Schema string   `json:"schema"`
	Name   string   `json:"name"`
	PK     []string `json:"pk"`
}

// editableResult marks out editable when stmt is a plain single-table SELECT on a table with a primary key
// whose columns are all in the select list (db.EditableSelect + the pk check). The result is cached like a
// table page, so /plan works on it with the same resultId and row numbers.
func (ss *session) editableResult(ctx context.Context, run db.Session, stmt string, res *db.Result, out *resultJSON) {
	sess := ss.o.Sess
	// a statement that ran on a pinned connection may see temp tables or another database: not editable
	if !res.HasRows || ss.conn.ReadOnly || run != sess {
		return
	}
	schema, name, ok := db.EditableSelect(stmt, sess.Caps().Dialect)
	if !ok {
		return
	}
	if schema == "" {
		schema = sess.CurrentSchema()
	}
	ref := db.ObjRef{Schema: schema, Name: name, Kind: db.KindTable}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	isTable := false
	if objs, err := sess.Objects(ctx, schema); err == nil {
		for _, o := range objs {
			if o.Name == name && o.Kind == db.KindTable {
				isTable = true
			}
		}
	}
	if !isTable {
		return
	}
	cols, err := ss.columns(ctx, ref)
	if err != nil {
		return
	}
	have := map[string]bool{}
	for _, c := range res.Columns {
		have[c.Name] = true
	}
	pk := []string{}
	for _, c := range cols {
		if c.PK {
			if !have[c.Name] {
				return
			}
			pk = append(pk, c.Name)
		}
	}
	if len(pk) == 0 {
		return
	}
	*out = toJSON(res, cols)
	out.Editable = true
	out.ResultID = ss.put(&cached{ref: ref, cols: cols, res: res})
	out.Table = &tableInfo{Schema: schema, Name: name, PK: pk}
}

// checkSyntax enforces a Mongo query tab's syntax choice: "command" is a raw {...} document, "shell" is
// db.collection.method(...). Empty (or any other dialect) accepts both, as the driver tells them apart by the "{".
func checkSyntax(dialect, syntax, text string) error {
	if dialect != "mongo" || syntax == "" {
		return nil
	}
	cmd := strings.HasPrefix(strings.TrimSpace(text), "{")
	switch syntax {
	case "command":
		if !cmd {
			return bad("command syntax runs a {...} document; switch the tab to shell syntax to run db.collection.method(...)")
		}
	case "shell":
		if cmd {
			return bad("shell syntax runs db.collection.method(...); switch the tab to command syntax to run a {...} document")
		}
	default:
		return bad("unknown syntax %q (shell or command)", syntax)
	}
	return nil
}

func (s *Server) datatypes(ss *session, r *http.Request) (any, error) {
	t := db.DataTypesOf(ss.o.Sess)
	if t == nil {
		t = []string{}
	}
	return map[string]any{"types": t}, nil
}

// structurePlan plans a whole-table definition change ({old, new}; old null = create) through the driver's
// PlanTable, the call the terminal UI's structure editor makes, and stores it for /apply.
func (s *Server) structurePlan(ss *session, r *http.Request) (any, error) {
	var req struct {
		Old *db.TableDef
		New *db.TableDef
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if req.New == nil || strings.TrimSpace(req.New.Name) == "" {
		return nil, bad("new.Name is required")
	}
	if len(req.New.Columns) == 0 {
		return nil, bad("a table needs at least one column")
	}
	for _, c := range req.New.Columns {
		if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Type) == "" {
			return nil, bad("every column needs a name and a data type")
		}
		if !validColumnType(strings.TrimSpace(c.Type)) {
			return nil, bad("invalid data type %q for column %q", c.Type, c.Name)
		}
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	old := req.Old
	if live := ss.liveTableDef(ctx, req.Old, req.New); live != nil {
		old = live // never trust the client's copy of what the table looks like now
	}
	stmts, err := ss.o.Sess.PlanTable(old, req.New)
	if err != nil {
		return nil, bad("%v", err)
	}
	if len(stmts) == 0 {
		return nil, bad("no structure changes")
	}
	return ss.storePlan(stmts, "", "")
}

// liveTableDef is the table's current definition read from the server (what the terminal UI's oldDef holds),
// for the table the edit starts from (old.Name, else new.Name); nil when that table does not exist.
func (ss *session) liveTableDef(ctx context.Context, old, nw *db.TableDef) *db.TableDef {
	sess := ss.o.Sess
	schema, name := nw.Schema, nw.Name
	if old != nil && old.Name != "" {
		schema, name = old.Schema, old.Name
	}
	if schema == "" {
		schema = sess.CurrentSchema()
	}
	exists := false
	if objs, err := sess.Objects(ctx, schema); err == nil {
		for _, o := range objs {
			if o.Name == name && o.Kind == db.KindTable {
				exists = true
			}
		}
	}
	if !exists {
		return nil
	}
	ref := db.ObjRef{Schema: schema, Name: name, Kind: db.KindTable}
	cols, err := sess.Columns(ctx, ref)
	if err != nil {
		return nil
	}
	d := &db.TableDef{Schema: schema, Name: name}
	for _, c := range cols {
		c.OrigName = c.Name
		c.FK = nil
		d.Columns = append(d.Columns, c)
		if c.PK {
			d.PrimaryKey = append(d.PrimaryKey, c.Name)
		}
	}
	caps := sess.Caps()
	if caps.Indexes {
		if ixs, err := sess.Indexes(ctx, ref); err == nil {
			for _, ix := range ixs {
				if !ix.Primary {
					d.Indexes = append(d.Indexes, ix)
				}
			}
		}
	}
	if caps.ForeignKeys {
		if fks, err := sess.ForeignKeys(ctx, ref); err == nil {
			d.ForeignKeys = fks
		}
	}
	return d
}

// renameQuery moves a saved query to a new path. store.Queries has no Rename, so (like the TUI) it saves the
// new file and deletes the old one; an existing target is a 409.
// shortcut: not atomic, a crash between the two steps leaves both files; add Queries.Rename.
func (s *Server) renameQuery(r *http.Request) (any, error) {
	var req struct{ From, To string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if !strings.HasSuffix(req.From, ".sql") || !strings.HasSuffix(req.To, ".sql") {
		return nil, bad("from and to must be saved query files (.sql)")
	}
	s.queryMu.Lock()
	defer s.queryMu.Unlock()
	q, err := s.o.Queries.Read(req.From)
	if err != nil {
		return nil, bad("%q is not a saved query file", req.From)
	}
	if q.Shared {
		return nil, bad("shared queries are read-only")
	}
	to := strings.TrimPrefix(strings.ReplaceAll(req.To, "\\", "/"), "/")
	folder, base := "", to
	if i := strings.LastIndex(to, "/"); i >= 0 {
		folder, base = to[:i], to[i+1:]
	}
	nq, err := s.saveNew(folder, strings.TrimSuffix(base, ".sql"), q.Keywords, q.SQL, false)
	if err != nil {
		return nil, err
	}
	if saved, ok := nq.(store.Query); ok && saved.Path == req.From {
		return nq, nil // same file name after sanitizing: nothing to delete
	}
	if err := s.o.Queries.Delete(req.From); err != nil {
		return nil, err
	}
	return nq, nil
}

// searchPause / searchResume flip the flag the search job checks between tables.
func (s *Server) searchPause(ss *session, r *http.Request) (any, error) { return ss.setPaused(r, true) }
func (s *Server) searchResume(ss *session, r *http.Request) (any, error) {
	return ss.setPaused(r, false)
}

func (ss *session) setPaused(r *http.Request, v bool) (any, error) {
	ss.mu.Lock()
	j := ss.jobs[r.PathValue("job")]
	ss.mu.Unlock()
	if j == nil || j.paused == nil {
		return nil, &apiErr{Code: http.StatusNotFound, Msg: "no such search"}
	}
	j.paused.Store(v)
	return map[string]any{"paused": v}, nil
}

// record notes one table's result under the job lock.
func (j *job) record(h db.SearchHit) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.checked++
	if h.Count > 0 || h.Err != nil {
		x := hitJSON{Ref: h.Ref, Count: h.Count, Filters: h.Filters, Schema: h.Ref.Schema}
		if h.Err != nil {
			x.Error = h.Err.Error()
		}
		j.hits = append(j.hits, x)
	}
}

// maxPause is how long a search may stay paused before it is cancelled and frees its job slot.
const maxPause = 10 * time.Minute

// waitWhilePaused blocks a search between two tables until it is resumed or cancelled, holding no lock.
// It returns false when the pause outlasted max (the caller then cancels the job).
func waitWhilePaused(ctx context.Context, p *atomic.Bool, max time.Duration) bool {
	deadline := time.Now().Add(max)
	for p.Load() && ctx.Err() == nil {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
	return true
}

var columnTypeRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_ ]*(\(\s*\d+(\s*,\s*\d+)?\s*\))?(\[\])?$`)

// validColumnType accepts a plain SQL type name with optional length / precision and array suffix, so a
// client cannot smuggle SQL into CREATE TABLE / ALTER TABLE through a type.
func validColumnType(t string) bool { return len(t) <= 64 && columnTypeRE.MatchString(t) }
