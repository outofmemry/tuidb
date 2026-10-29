package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"webtuidb/internal/board"
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/export"
	"webtuidb/internal/keymap"
	"webtuidb/internal/mcp"
	"webtuidb/internal/plugin"
	"webtuidb/internal/store"
)

// Milestone 13: jobs centre, board exporter/dropdown, chats, settings editor, passcode, reveal, plugin host.

// ---- live config ----------------------------------------------------------------------------------

// cfg is the configuration as the settings editor last wrote it, else the one the server started with.
// shortcut: only /api/meta and the settings routes read it; the rest of the server keeps the startup
// config (hundreds of unsynchronised reads of s.o.Cfg), so a changed setting reaches those on restart.
func (s *Server) cfg() config.Config {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	if s.live != nil {
		return *s.live
	}
	return s.o.Cfg
}

func (s *Server) reloadCfg() error {
	c, err := config.Load(s.configPath())
	if err != nil {
		return err
	}
	s.liveMu.Lock()
	s.live = &c
	s.liveMu.Unlock()
	return nil
}

// configPath is the config.toml the process was started with (-config), else the default one.
func (s *Server) configPath() string {
	if s.o.CfgPath != "" {
		return s.o.CfgPath
	}
	return filepath.Join(config.ConfigDir(), "config.toml")
}

// camel turns a toml key such as filter_default_column into filterDefaultColumn.
func camel(k string) string {
	p := strings.Split(k, "_")
	for i := 1; i < len(p); i++ {
		if p[i] != "" {
			p[i] = strings.ToUpper(p[i][:1]) + p[i][1:]
		}
	}
	return strings.Join(p, "")
}

// section renders a config table with its Go field names (what the web client has always read) and, beside
// them, the camelCase spelling of every toml key (filterDefaultColumn, closeBraces, ...).
func section(v any) map[string]any {
	out := map[string]any{}
	rv := reflect.ValueOf(v)
	for i := 0; i < rv.NumField(); i++ {
		f := rv.Type().Field(i)
		if !f.IsExported() {
			continue
		}
		out[f.Name] = rv.Field(i).Interface()
		if name := strings.Split(f.Tag.Get("toml"), ",")[0]; name != "" && name != "-" {
			out[camel(name)] = rv.Field(i).Interface()
		}
	}
	return out
}

// pluginBindings are the shortcuts of the installed plugins as keymap bindings (action "plugin:<name>").
func pluginBindings() []keymap.Binding { return plugin.Bindings() }

// ---- jobs centre ------------------------------------------------------------------------------------

func (j *job) summary(id string) map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	lines := j.lines
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	progress := -1.0
	switch {
	case j.done && j.err == "":
		progress = 1
	case j.total > 0:
		progress = float64(j.checked) / float64(j.total)
	}
	file := ""
	if j.file != "" && j.err == "" { // backups keep only fileName; "file" promises a downloadable temp file
		file = j.fileName
	}
	m := map[string]any{
		"id": id, "kind": j.kind, "title": j.title, "done": j.done, "err": j.err, "progress": progress,
		"file": file, "created": j.created.Format(time.RFC3339), "lines": append([]string{}, lines...),
		"download": j.done && j.err == "" && j.file != "",
	}
	if j.path != "" && j.done && j.err == "" { // a finished backup: the file on the server, for POST /reveal
		m["path"] = j.path
	}
	return m
}

// jobs lists the session's jobs, newest first.
func (s *Server) jobs(ss *session, r *http.Request) (any, error) {
	ss.mu.Lock()
	type kv struct {
		id string
		j  *job
	}
	var l []kv
	for id, j := range ss.jobs {
		l = append(l, kv{id, j})
	}
	ss.mu.Unlock()
	sort.Slice(l, func(a, b int) bool { return l[a].j.created.After(l[b].j.created) })
	out := make([]map[string]any, 0, len(l))
	for _, e := range l {
		out = append(out, e.j.summary(e.id))
	}
	return map[string]any{"jobs": out}, nil
}

// jobCancel stops a running job; it stays listed (failed/cancelled) until dismissed with DELETE.
func (s *Server) jobCancel(ss *session, r *http.Request) (any, error) {
	j, err := s.job(ss, r)
	if err != nil {
		return nil, err
	}
	j.cancel()
	return nil, nil
}

// ---- boards: dropdown options and the exporter panel ----------------------------------------------------

func boardPanelNamed(b board.Board, title string) (board.Panel, bool) {
	for _, p := range b.Panels {
		if p.Title == title {
			return p, true
		}
	}
	return board.Panel{}, false
}

func (s *Server) boardOptions(ss *session, r *http.Request) (any, error) {
	b, err := findBoard(r.PathValue("name"))
	if err != nil {
		return nil, err
	}
	p, ok := boardPanelNamed(b, r.URL.Query().Get("panel"))
	if !ok || p.Kind != "dropdown" {
		return nil, bad("no dropdown panel %q in %q", r.URL.Query().Get("panel"), b.Name)
	}
	if len(p.Options) == 0 && strings.TrimSpace(p.Query) != "" && !db.IsReadOnly(ss.o.Sess.Caps().Dialect, p.Query) {
		return nil, bad("panel %q is not read-only; boards only run read-only queries", p.Title)
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	opts, cut, err := board.DropdownOptions(ctx, ss.o.Sess, b, p, nil)
	if err != nil {
		return nil, bad("%v", err)
	}
	return map[string]any{"options": nonNil(opts), "truncated": cut}, nil
}

// boardExport runs an exporter panel's query as a background export job (download it like any other export).
func (s *Server) boardExport(ss *session, r *http.Request) (any, error) {
	var req struct {
		Panel, Format string
		Vals          map[string]string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	b, err := findBoard(r.PathValue("name"))
	if err != nil {
		return nil, err
	}
	p, ok := boardPanelNamed(b, req.Panel)
	if !ok || strings.TrimSpace(p.Query) == "" {
		return nil, bad("no exporter panel %q in %q", req.Panel, b.Name)
	}
	format := req.Format
	if format == "" {
		format = p.Format
	}
	if format == "excel" {
		format = "xlsx"
	}
	switch format {
	case "csv", "json", "sql", "xlsx":
	default:
		return nil, bad("unknown export format %q", format)
	}
	sess := ss.o.Sess
	if !db.IsReadOnly(sess.Caps().Dialect, p.Query) {
		return nil, bad("panel %q is not read-only; boards only run read-only queries", p.Title)
	}
	if err := ss.jobSlot(); err != nil {
		return nil, err
	}
	if err := ss.reserve(0); err != nil {
		return nil, err
	}
	o := export.Options{Format: export.Format(format), CSV: s.o.Cfg.CSV, Gzip: false, Table: p.Title, Dialect: sess.Caps().Dialect, NoHeader: !s.o.Cfg.Export.Header}
	f, err := os.CreateTemp("", "webtuidb-export-*")
	if err != nil {
		return nil, err
	}
	j := &job{kind: "export", title: "Export " + p.Title, file: f.Name(), fileName: ss.exportName("", b.Name+"-"+p.Title, export.Ext(o))}
	id := ss.startJob(j, func(ctx context.Context) error {
		res, err := board.ExportPanel(ctx, sess, b, p, req.Vals, ss.rowLimit()) // shortcut: capped at the row limit, not paged
		if err == nil {
			err = export.Write(f, res, o)
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(f.Name())
			j.mu.Lock()
			j.file = ""
			j.mu.Unlock()
		}
		return err
	})
	return map[string]any{"job": id}, nil
}

// ---- assistant chats -----------------------------------------------------------------------------------

var chatIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func (s *Server) chats() *store.Chats {
	s.chatOnce.Do(func() { s.chatStore = store.DefaultChats() })
	return s.chatStore
}

func (s *Server) chatList(ss *session, r *http.Request) (any, error) {
	var l []store.ChatSummary
	var err error
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		l = s.chats().Search(ss.conn.Name, q)
	} else {
		l, err = s.chats().List(ss.conn.Name)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"chats": nonNil(l)}, nil
}

func (s *Server) chatNew(ss *session, r *http.Request) (any, error) {
	ch := s.chats().New(ss.conn.Name)
	if err := s.chats().Save(ss.conn.Name, ch); err != nil {
		return nil, err
	}
	return ch, nil
}

func (s *Server) chatGet(ss *session, r *http.Request) (any, error) {
	id := r.PathValue("cid")
	if !chatIDRe.MatchString(id) {
		return nil, bad("bad chat id")
	}
	ch, err := s.chats().Load(ss.conn.Name, id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, &apiErr{Code: http.StatusNotFound, Msg: "no such chat"}
		}
		return nil, err
	}
	return ch, nil
}

func (s *Server) chatPut(ss *session, r *http.Request) (any, error) {
	id := r.PathValue("cid")
	if !chatIDRe.MatchString(id) {
		return nil, bad("bad chat id")
	}
	var ch store.Chat
	if err := decode(r, &ch); err != nil {
		return nil, err
	}
	ch.ID = id // the URL names the chat, whatever the body says
	if len(ch.Messages) > 2000 {
		return nil, bad("a chat holds at most 2000 messages")
	}
	if err := s.chats().Save(ss.conn.Name, &ch); err != nil {
		return nil, err
	}
	return ch, nil
}

func (s *Server) chatDelete(ss *session, r *http.Request) (any, error) {
	id := r.PathValue("cid")
	if !chatIDRe.MatchString(id) {
		return nil, bad("bad chat id")
	}
	return nil, s.chats().Delete(ss.conn.Name, id)
}

// ---- settings editor -------------------------------------------------------------------------------------

func (s *Server) settingsSchema(r *http.Request) (any, error) {
	return map[string]any{"fields": schemaFields(s.cfg()), "path": s.configPath()}, nil
}

// schemaField is a settings field plus whether the browser may write it.
type schemaField struct {
	config.Field
	ReadOnly bool `json:"readOnly,omitempty"`
}

// schemaFields marks the path settings read-only, so one of them cannot fail a whole save.
func schemaFields(c config.Config) []schemaField {
	fs := config.Schema(c)
	out := make([]schemaField, len(fs))
	for i, f := range fs {
		out[i] = schemaField{Field: f, ReadOnly: slices.Contains(pathSettings, f.Key)}
	}
	return out
}

// pathSettings are the settings the browser may not change: they point the server at files or folders.
var pathSettings = []string{"queries.shared_dirs", "theme.file", "web.tls_cert", "web.tls_key", "mcp.tls_cert", "mcp.tls_key"}

const commentsWarning = "comments in config.toml were not kept"

// putSettings writes {values: {"section.key": "text"}} to config.toml after validating it, comments are not kept.
func (s *Server) putSettings(r *http.Request) (any, error) {
	var in struct{ Values map[string]string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if len(in.Values) == 0 {
		return nil, bad("no values to save")
	}
	for _, k := range pathSettings { // these name files or folders on the server: only config.toml itself may set them
		if _, ok := in.Values[k]; ok {
			return nil, bad("%s names a path on the server and can only be edited in config.toml", k)
		}
	}
	had := config.HasComments(s.configPath())
	if err := config.ApplyValues(s.configPath(), in.Values); err != nil {
		return nil, bad("%v", err)
	}
	if err := s.reloadCfg(); err != nil {
		return nil, bad("saved, but the config does not load: %v", err)
	}
	out := map[string]any{"fields": schemaFields(s.cfg()), "restart": true}
	if had {
		out["warning"] = commentsWarning
	}
	return out, nil
}

// putKeys writes {keys: {"commit": ["x"]}} to [keys]; a keymap conflict is a 409 with the message and nothing is written.
func (s *Server) putKeys(r *http.Request) (any, error) {
	var in struct{ Keys map[string][]string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	// dry run: the same "takes the key from" check as the terminal UI's key form
	cur := s.cfg()
	old, _, _ := cur.KeymapChecked(pluginBindings())
	next := cur
	next.KeyOverrides = in.Keys
	nm, _, err := next.KeymapChecked(pluginBindings())
	if err != nil {
		return nil, &apiErr{Code: http.StatusConflict, Msg: err.Error()}
	}
	if old != nil {
		var changed []string // the actions this save edits; losing one of those is the point of the edit
		for a, specs := range in.Keys {
			if !slices.Equal(specs, cur.KeyOverrides[a]) {
				changed = append(changed, a)
			}
		}
		for a := range cur.KeyOverrides {
			if _, ok := in.Keys[a]; !ok {
				changed = append(changed, a)
			}
		}
		if len(changed) == 0 {
			changed = []string{""}
		}
		var terr error
		for _, e := range changed {
			if terr = keymap.TakenFrom(old, nm, e); terr == nil {
				break
			}
		}
		if terr != nil {
			return nil, &apiErr{Code: http.StatusConflict, Msg: terr.Error()}
		}
	}
	had := config.HasComments(s.configPath())
	if err := config.WriteKeys(s.configPath(), in.Keys, pluginBindings()...); err != nil {
		return nil, &apiErr{Code: http.StatusConflict, Msg: err.Error()}
	}
	if err := s.reloadCfg(); err != nil {
		return nil, bad("saved, but the config does not load: %v", err)
	}
	if had {
		return map[string]any{"warning": commentsWarning}, nil
	}
	return nil, nil
}

// ---- passcode ---------------------------------------------------------------------------------------------

type passFails struct {
	mu sync.Mutex
	n  map[string]*passEntry
}

// passEntry serialises the attempts of one address: its mutex is held across the delay and the bcrypt check,
// so parallel requests queue instead of waiting together.
type passEntry struct {
	mu    sync.Mutex
	fails int          // guarded by mu
	last  atomic.Int64 // unix nanoseconds of the last attempt
}

const (
	passFailExpiry = 15 * time.Minute // a quiet address starts again
	passMaxIPs     = 1000
)

// entry returns the record of ip, dropping the expired ones (and the oldest past passMaxIPs).
func (p *passFails) entry(ip string) *passEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.n == nil {
		p.n = map[string]*passEntry{}
	}
	now := time.Now()
	for k, e := range p.n {
		if now.Sub(time.Unix(0, e.last.Load())) > passFailExpiry && e.mu.TryLock() {
			delete(p.n, k)
			e.mu.Unlock()
		}
	}
	for len(p.n) >= passMaxIPs && p.n[ip] == nil {
		oldest, ot := "", now.UnixNano()
		for k, e := range p.n {
			if l := e.last.Load(); l < ot {
				oldest, ot = k, l
			}
		}
		if oldest == "" {
			break
		}
		delete(p.n, oldest)
	}
	e := p.n[ip]
	if e == nil {
		e = &passEntry{}
		p.n[ip] = e
	}
	e.last.Store(now.UnixNano())
	return e
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// passcodeCheck answers {ok}. It never returns the hash. After 3 wrong answers from one address every
// further attempt from it waits 1 s, which makes guessing slow; a right answer clears the count.
func (s *Server) passcodeCheck(r *http.Request) (any, error) {
	var in struct{ Passcode string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	e := s.pass.entry(clientIP(r))
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fails >= 3 {
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	// config.CheckPasscode compares with bcrypt, which is constant-time in the guess; no passcode set: never matches
	ok := config.CheckPasscode(s.cfg().Security.Passcode, in.Passcode)
	if ok {
		e.fails = 0
	} else {
		e.fails++
	}
	return map[string]any{"ok": ok}, nil
}

// ---- reveal -----------------------------------------------------------------------------------------------

// reveal opens the folder of a file on the machine running webtuidb; a remote browser gets a 403 and offers the download.
func (s *Server) reveal(r *http.Request) (any, error) {
	if !loopback(r.RemoteAddr) {
		return nil, &apiErr{Code: http.StatusForbidden, Msg: "reveal works only on the machine running webtuidb"}
	}
	var in struct{ Path string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(in.Path) {
		return nil, bad("an absolute path is required")
	}
	if _, err := os.Stat(in.Path); err != nil {
		return nil, bad("%v", err)
	}
	if err := store.Reveal(in.Path); err != nil {
		return nil, bad("%v", err)
	}
	return nil, nil
}

// ---- plugins: the query host ----------------------------------------------------------------------------------

// pluginHost lets a .js plugin run read-only SQL on the session it was started from.
type pluginHost struct {
	ss *session
}

func (h pluginHost) Query(ctx context.Context, sql string) ([]string, [][]string, error) {
	sess := h.ss.o.Sess
	dialect := sess.Caps().Dialect
	st := db.Split(dialect, sql)
	if len(st) != 1 || !db.IsReadOnly(dialect, st[0].Text) {
		return nil, nil, errors.New("query(): only a single read-only statement is allowed")
	}
	if fn := mcp.SideEffect(dialect, st[0].Text); fn != "" {
		return nil, nil, fmt.Errorf("query(): %s is not allowed in a plugin query", fn)
	}
	run, release, err := mcp.ReadOnlyTx(ctx, sess, dialect, h.ss.conn.ReadOnly) // a read-only transaction, so a side effect cannot commit
	if err != nil {
		return nil, nil, err
	}
	defer release()
	res, err := run.Exec(ctx, st[0].Text, h.ss.rowLimit())
	if err != nil {
		return nil, nil, err
	}
	cols := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		cols[i] = c.Name
	}
	rows := make([][]string, len(res.Rows))
	for i, r := range res.Rows {
		rows[i] = make([]string, len(r))
		for k, v := range r {
			if v != nil {
				rows[i][k] = fmt.Sprint(v)
			}
		}
	}
	return cols, rows, nil
}

func effectsJSON(l []plugin.Effect) []map[string]any {
	out := make([]map[string]any, 0, len(l))
	for _, e := range l {
		out = append(out, map[string]any{"kind": e.Kind, "text": e.Text, "ref": e.Ref})
	}
	return out
}
