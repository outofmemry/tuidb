package web

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"webtuidb/internal/board"
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/store"
)

// Milestones 6-8: state the two UIs share (groups, recent files, connection sort, backup options), the
// settings page, the filter bar's WHERE text, connection colour, metrics board editing and single-panel runs,
// the tool path check and the "more than 100 statements" summary.

func (s *Server) needState() error {
	if s.o.State == nil {
		return bad("no state store")
	}
	return nil
}

// ---- sidebar groups ------------------------------------------------------------------------------------

func (s *Server) getGroups(r *http.Request) (any, error) {
	c := r.URL.Query().Get("conn")
	if c == "" || s.needState() != nil {
		return nil, bad("conn is required")
	}
	return map[string]any{"groups": s.o.State.Groups(c)}, nil
}

func (s *Server) putGroups(r *http.Request) (any, error) {
	c := r.URL.Query().Get("conn")
	var in struct{ Groups []store.ItemGroup }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if c == "" || s.needState() != nil {
		return nil, bad("conn is required")
	}
	seen := map[string]bool{}
	for _, g := range in.Groups {
		if strings.TrimSpace(g.Name) == "" || seen[g.Name] {
			return nil, bad("every group needs its own non-empty name")
		}
		seen[g.Name] = true
	}
	return nil, s.o.State.SetGroups(c, in.Groups)
}

// ---- recent files and the connection sort -----------------------------------------------------------------

func (s *Server) getRecentFiles(r *http.Request) (any, error) {
	if s.needState() != nil {
		return nil, bad("no state store")
	}
	f := s.o.State.RecentFiles()
	if f == nil {
		f = []string{}
	}
	return map[string]any{"files": f, "max": s.o.Cfg.General.RecentFiles}, nil
}

// putRecentFile moves an entry of the shared list to the front. It takes only an absolute path that is already in the
// list (the browser's own file picker gives a name, never a path, and a bare name would be opened by the terminal UI
// relative to its working directory).
func (s *Server) putRecentFile(r *http.Request) (any, error) {
	var in struct{ Name string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if s.needState() != nil || !filepath.IsAbs(in.Name) || !slices.Contains(s.o.State.RecentFiles(), in.Name) {
		return nil, bad("only a path from the recent list can be added")
	}
	return nil, s.o.State.AddRecentFile(in.Name, s.o.Cfg.General.RecentFiles)
}

// openRecentFile reads a recent file by the absolute path the terminal UI stored. Only .sql files that are in the recent
// list can be read, so the endpoint is not a way to read the server's disk.
func (s *Server) openRecentFile(r *http.Request) (any, error) {
	var in struct{ Path string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if s.needState() != nil {
		return nil, bad("no state store")
	}
	listed := false
	for _, f := range s.o.State.RecentFiles() {
		listed = listed || f == in.Path
	}
	switch {
	case !listed:
		return nil, bad("that file is not in the recent list")
	case !filepath.IsAbs(in.Path):
		return nil, bad("a browser cannot reopen %q by name: pick the file again", in.Path)
	case !strings.EqualFold(filepath.Ext(in.Path), ".sql"):
		return nil, bad("only .sql files can be opened")
	}
	st, err := os.Stat(in.Path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 8<<20 {
		return nil, bad("%s cannot be read (missing, not a file, or over 8 MB)", in.Path)
	}
	b, err := os.ReadFile(in.Path)
	if err != nil {
		return nil, bad("%v", err)
	}
	return map[string]any{"name": filepath.Base(in.Path), "text": string(b)}, nil
}

func (s *Server) getConnSort(r *http.Request) (any, error) {
	if s.needState() != nil {
		return nil, bad("no state store")
	}
	return map[string]any{"sort": s.o.State.ConnectionSort(), "default": s.o.Cfg.General.ConnectionSort}, nil
}

func (s *Server) putConnSort(r *http.Request) (any, error) {
	var in struct{ Sort string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.Sort != "" && in.Sort != "name" && in.Sort != "tag" && in.Sort != "driver" && in.Sort != "recent" {
		return nil, bad("sort must be name, tag, driver or recent")
	}
	if s.needState() != nil {
		return nil, bad("no state store")
	}
	return nil, s.o.State.SetConnectionSort(in.Sort)
}

func (s *Server) getBackupOptions(r *http.Request) (any, error) {
	c := r.URL.Query().Get("conn")
	if c == "" || s.needState() != nil {
		return nil, bad("conn is required")
	}
	return map[string]any{"options": s.o.State.BackupOptions(c)}, nil
}

func (s *Server) putBackupOptions(r *http.Request) (any, error) {
	c := r.URL.Query().Get("conn")
	var in struct{ Options map[string]string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if c == "" || s.needState() != nil {
		return nil, bad("conn is required")
	}
	clean := map[string]string{}
	for _, d := range []string{"backup.", "restore."} {
		for _, k := range []string{"file", "gzip", "format", "data", "schema"} {
			if v, ok := in.Options[d+k]; ok && len(v) <= 512 {
				clean[d+k] = v
			}
		}
	}
	if len(clean) == 0 {
		return nil, bad("no known backup option given")
	}
	return nil, s.o.State.SetBackupOptions(c, clean) // merges
}

// ---- settings (read only) -----------------------------------------------------------------------------------

// settings returns the resolved configuration as TOML, with every secret blanked, and the file it came from.
// shortcut: read only. Editing config.toml from a browser would need a validated writer; the terminal UI opens it in $EDITOR.
func (s *Server) settings(r *http.Request) (any, error) {
	c := s.o.Cfg
	blank := func(v *string) {
		if *v != "" {
			*v = "(hidden)"
		}
	}
	noUser := func(v *string) { // a base URL may carry user:pass@
		if u, err := url.Parse(*v); err == nil && u.User != nil {
			u.User = nil
			*v = u.String()
		}
	}
	blank(&c.AI.APIKey)
	blank(&c.AI.APIKeyCmd) // a command may embed the key itself
	noUser(&c.AI.BaseURL)
	blank(&c.Web.Password)
	c.Web.TLSCert, c.Web.TLSKey, c.MCP.TLSCert, c.MCP.TLSKey = "", "", "", ""
	ps := map[string]config.AIProvider{}
	for k, p := range c.AI.Providers {
		blank(&p.APIKey)
		blank(&p.APIKeyCmd)
		noUser(&p.BaseURL)
		ps[k] = p
	}
	c.AI.Providers = ps
	c.MCP.Tokens = append([]config.MCPToken(nil), c.MCP.Tokens...)
	for i := range c.MCP.Tokens {
		t := &c.MCP.Tokens[i]
		blank(&t.Value)
		t.Name, t.Scope, t.Connections = "token", "", nil
	}
	c.Keys = nil // raw TOML primitives; the key bindings are on the help screen
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(c); err != nil {
		return nil, err
	}
	return map[string]any{"path": filepath.Join(config.ConfigDir(), "config.toml"), "text": b.String(), "readOnly": true}, nil
}

// ---- filter bar: the WHERE text ----------------------------------------------------------------------------------

func (s *Server) filtersSQL(ss *session, r *http.Request) (any, error) {
	var req struct {
		Ref     db.ObjRef
		Filters db.FilterSet
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	d, ok := db.DialectFor(ss.o.Sess.Caps().Dialect)
	if !ok {
		return nil, bad("no SQL dialect for %s", ss.conn.Driver)
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	cols, err := ss.columns(ctx, req.Ref)
	if err != nil {
		return nil, bad("%v", err)
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	w, err := db.WhereText(d, req.Filters, names)
	if err != nil {
		return nil, bad("%v", err)
	}
	return map[string]any{"where": w}, nil
}

// ---- connection colour ----------------------------------------------------------------------------------------

var colorRe = regexp.MustCompile(`^(#([0-9a-fA-F]{3}|[0-9a-fA-F]{6}))?$`)

func (ss *session) color() string {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.conn.Color
}

// setColor changes the colour of the open session and of the saved connection (not one from a [[source]] file).
func (s *Server) setColor(ss *session, r *http.Request) (any, error) {
	var req struct{ Color string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if !colorRe.MatchString(req.Color) {
		return nil, bad("color must be empty or a hex colour like #3b82f6")
	}
	if len(req.Color) == 4 { // #abc -> #aabbcc: both UIs store 6 digits
		c := req.Color
		req.Color = "#" + c[1:2] + c[1:2] + c[2:3] + c[2:3] + c[3:4] + c[3:4]
	}
	ss.mu.Lock()
	ss.conn.Color = req.Color
	ss.mu.Unlock()
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.reloadConns() != nil { // connections.toml does not parse: keep the file as it is
		return ss.info(), nil
	}
	for i, c := range s.o.Conns {
		if c.Name == ss.conn.Name && c.Source == "" {
			s.o.Conns[i].Color = req.Color
			if s.o.ConnPath != "" {
				own, _, _ := s.splitSources(s.o.Conns)
				if err := config.SaveConnections(s.o.ConnPath, own); err != nil {
					return nil, err
				}
				s.stamp = s.connStamp()
			}
		}
	}
	return ss.info(), nil
}

// ---- dump tool paths ------------------------------------------------------------------------------------------

// checkTools lets the browser change a connection's tool paths only to what config.ToolPathOK accepts: a path override
// runs a program on the server, so it must not name /bin/sh.
func checkTools(c, old config.Connection) error {
	for k, p := range c.Tools {
		if p != "" && old.Tools[k] != p && !config.ToolPathOK(k, p) {
			return bad("%q: tool %q must be an absolute path to a file named like %s", c.Name, k, k)
		}
	}
	return nil
}

// ---- metrics boards: one panel, save / rename, delete ----------------------------------------------------------------

type namedSeries struct {
	Name   string    `json:"name"`
	Values []float64 `json:"values"`
}

func jsonSeries(l []db.NamedSeries) []namedSeries {
	out := []namedSeries{}
	for _, s := range l {
		out = append(out, namedSeries{s.Name, nonNil(s.Values)})
	}
	return out
}

type panelOut struct {
	Title  string        `json:"title"`
	Kind   string        `json:"kind"`
	Number string        `json:"number,omitempty"`
	Error  string        `json:"error,omitempty"`
	Result *resultJSON   `json:"result,omitempty"`
	Labels []string      `json:"labels"`
	Values []float64     `json:"values"`
	Series []namedSeries `json:"series"`
}

func toPanelOut(pr board.PanelResult) panelOut {
	p := panelOut{Title: pr.Panel.Title, Kind: pr.Panel.Kind, Number: pr.Number, Labels: nonNil(pr.Series.Labels), Values: nonNil(pr.Series.Values), Series: jsonSeries(pr.Series.Series)}
	if pr.Err != nil {
		p.Error = pr.Err.Error()
	}
	if pr.Result != nil && pr.Panel.Kind == "table" {
		rj := toJSON(pr.Result, nil)
		p.Result = &rj
	}
	return p
}

// panelRuns: button and import panels hold no query; the UIs act on them.
func panelRuns(p board.Panel) bool { return p.Kind != "button" && p.Kind != "import" }

// boardPanel runs the one panel titled `title` ({name, title, vals}) and names the panels that refresh when it runs.
func (s *Server) boardPanel(ss *session, r *http.Request) (any, error) {
	var req struct {
		Name, Title string
		Vals        map[string]string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	b, err := findBoard(req.Name)
	if err != nil {
		return nil, err
	}
	for _, p := range b.Panels {
		if p.Title != req.Title {
			continue
		}
		if !panelRuns(p) {
			return nil, bad("panel %q is a %s panel: the UI acts on it", p.Title, p.Kind)
		}
		if !db.IsReadOnly(ss.o.Sess.Caps().Dialect, p.Query) {
			return nil, bad("panel %q is not read-only; boards only run read-only queries", p.Title)
		}
		ctx, cancel := ss.ctx(r)
		defer cancel()
		deps := []string{}
		for _, d := range board.Dependents(b, p.Title) {
			deps = append(deps, d.Title)
		}
		return map[string]any{"panel": toPanelOut(board.RunPanel(ctx, ss.o.Sess, b, p, req.Vals, ss.rowLimit())), "dependents": deps}, nil
	}
	return nil, bad("no panel titled %q in %q", req.Title, req.Name)
}

func boardNamed(name string) (board.Board, bool) {
	l, _ := board.List()
	for _, b := range l {
		if b.Name == name {
			return b, true
		}
	}
	return board.Board{}, false
}

// boardPut saves the whole board under {name}: a new name is a new file, the same name rewrites it, and a different Name
// renames it (saved under the new name, then the old file is removed).
func (s *Server) boardPut(r *http.Request) (any, error) {
	var req struct {
		Name        string
		RefreshSecs int
		Connection  string
		Vars        []board.Var
		Panels      []board.Panel
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Panels) == 0 {
		return nil, bad("a board needs a name and at least one panel")
	}
	titles := map[string]board.Panel{}
	for _, p := range req.Panels {
		if strings.TrimSpace(p.Title) == "" || titles[p.Title].Title != "" {
			return nil, bad("every panel needs its own title")
		}
		titles[p.Title] = p
	}
	for _, p := range req.Panels {
		switch p.Kind {
		case "number", "table", "bar", "line":
			if strings.TrimSpace(p.Query) == "" {
				return nil, bad("panel %q needs a query", p.Title)
			}
		case "button":
			if t, ok := titles[p.Target]; !ok || !panelRuns(t) {
				return nil, bad("button %q must target a panel that runs a query", p.Title)
			}
		case "import":
			if strings.TrimSpace(p.Table) == "" {
				return nil, bad("import panel %q needs a table", p.Title)
			}
		default:
			return nil, bad("panel %q: kind must be number, table, bar, line, button or import", p.Title)
		}
		if p.RefreshOn != "" && titles[p.RefreshOn].Title == "" {
			return nil, bad("panel %q refreshes on %q, which is not a panel", p.Title, p.RefreshOn)
		}
	}
	nb := board.Board{Name: req.Name, RefreshSecs: req.RefreshSecs, Connection: req.Connection, Vars: req.Vars, Panels: req.Panels}
	old, had := boardNamed(r.PathValue("name"))
	if had && req.Connection == "" { // the pin (set in the terminal or earlier) survives a save that does not name one
		nb.Connection = old.Connection
	}
	if had && req.Name == old.Name {
		nb.Path = old.Path
	} else if _, clash := boardNamed(req.Name); clash {
		return nil, bad("a board named %q already exists", req.Name)
	}
	if err := nb.Save(); err != nil {
		return nil, bad("%v", err)
	}
	if had && req.Name != old.Name {
		os.Remove(old.Path)
	}
	return nil, nil
}

func (s *Server) boardDelete(r *http.Request) (any, error) {
	b, ok := boardNamed(r.PathValue("name"))
	if !ok {
		return nil, bad("no board named %q", r.PathValue("name"))
	}
	return nil, os.Remove(b.Path)
}

// ---- more than 100 statements: one summary tab ---------------------------------------------------------------------

const summarizeOver = 100

// runSummary runs every statement and returns one item: how many ran, how many failed and the rows they touched.
func (ss *session) runSummary(r *http.Request, sess db.Session, stmts []db.Stmt, limit int) execItem {
	var failed, ran int
	var rows, got, ms int64
	var firstErrs []string
	for _, st := range stmts {
		it := ss.execOne(r, sess, st, limit)
		ran++
		ms += it.DurationMs
		rows += it.Affected
		if it.HasRows {
			got += int64(len(it.Cells))
		}
		if it.Error != "" {
			failed++
			if len(firstErrs) < 5 {
				firstErrs = append(firstErrs, fmt.Sprintf("statement %d: %s", ran, it.Error))
			}
			if ss.srv.o.Cfg.Editor.ReturnOnError {
				break
			}
		}
		if r.Context().Err() != nil {
			break
		}
	}
	msg := fmt.Sprintf("%d statements run, %d failed, %d rows affected, %d rows returned", ran, failed, rows, got)
	if len(stmts) > ran {
		msg += fmt.Sprintf(" (%d not run)", len(stmts)-ran)
	}
	if len(firstErrs) > 0 {
		msg += "\n" + strings.Join(firstErrs, "\n")
	}
	it := execItem{Stmt: fmt.Sprintf("%d statements", len(stmts)), Label: "Summary"}
	it.Message, it.Affected, it.DurationMs = msg, rows, ms
	return it
}
