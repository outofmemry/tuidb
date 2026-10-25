package ui

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/anthropic-sdk-go"

	"webtuidb/internal/board"
	"webtuidb/internal/clip"
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/export"
	"webtuidb/internal/mcp"
)

// ---- connections: export / import (M4 item 7) ---------------------------------------------

// exportConns is x on the welcome screen: all connections, or one group, to a passphrase-protected file.
func (w *welcome) exportConns() tea.Cmd {
	scopes := []string{"all"}
	scopes = append(scopes, config.Groups(w.a.d.Conns)...)
	w.a.push(newForm("Export connections", []formField{
		{Key: "scope", Label: "What", Kind: fChoice, Options: scopes, Value: "all"},
		{Key: "path", Label: "File", Kind: fText, Value: "connections.wtdb"},
		{Key: "pass", Label: "Passphrase", Kind: fSecret, Hint: "required with passwords; empty writes plain TOML"},
		{Key: "pw", Label: "Include passwords", Kind: fBool, Value: "false"},
	}, func(v map[string]string) tea.Cmd {
		var cs []config.Connection
		for _, c := range w.a.d.Conns {
			if v["scope"] == "all" || c.Group == v["scope"] || strings.HasPrefix(c.Group, v["scope"]+"/") {
				cs = append(cs, c)
			}
		}
		path := strings.TrimSpace(v["path"])
		if path == "" {
			return w.a.fail(errors.New("file name required"))
		}
		if v["pw"] == "true" && v["pass"] == "" {
			return w.a.fail(errors.New("include passwords needs a passphrase"))
		}
		return w.a.ifFree(func() tea.Cmd {
			err := writeAtomic(path, func(f io.Writer) error { return config.ExportConnections(f, cs, v["pass"], v["pw"] == "true") })
			if err != nil {
				return w.a.fail(err)
			}
			return w.a.say("exported %s to %s", plural(len(cs), "connection"), path)
		}, path)
	}))
	return nil
}

// importConns is I on the welcome screen. Names already taken get " (imported)" appended.
func (w *welcome) importConns() tea.Cmd {
	w.a.push(newForm("Import connections", []formField{
		{Key: "path", Label: "File", Kind: fText},
		{Key: "pass", Label: "Passphrase", Kind: fSecret},
	}, func(v map[string]string) tea.Cmd {
		f, err := os.Open(strings.TrimSpace(v["path"]))
		if err != nil {
			return w.a.fail(err)
		}
		defer f.Close()
		cs, dropped, err := config.ImportConnectionsOpts(f, v["pass"], false)
		if err != nil {
			return w.a.fail(err)
		}
		taken := map[string]bool{}
		for _, c := range w.a.d.Conns {
			taken[c.Name] = true
		}
		for _, c := range cs {
			for taken[c.Name] {
				c.Name += " (imported)"
			}
			taken[c.Name] = true
			c.Source = ""
			w.a.d.Conns = append(w.a.d.Conns, c)
		}
		if err := w.a.saveConns(); err != nil {
			return w.a.fail(err)
		}
		msg := "imported " + plural(len(cs), "connection")
		if len(dropped) > 0 { // password_cmd, bootstrap and key files from a shared file are not trusted
			msg += "; left out for safety: " + strings.Join(dropped, ", ")
		}
		return w.a.say("%s", msg)
	}))
	return nil
}

// ---- URL open target (M4 item 7) ------------------------------------------------------------

// openAliases maps the spellings a URL may use for operation= to a filter operator.
var openAliases = map[string]string{"eq": "=", "equal": "=", "equals": "=", "ne": "<>", "neq": "<>", "!=": "<>", "gt": ">", "lt": "<", "gte": ">=", "lte": "<=",
	"contain": "Contains", "contains": "Contains", "like": "LIKE", "in": "IN", "isnull": "IS NULL", "isnotnull": "IS NOT NULL", "startswith": "Has prefix", "endswith": "Has suffix"}

// openTarget opens the table a URL named (schema, table), filtered by column / operation / value;
// condition "or" joins the filters with OR.
func (w *Workspace) openTarget(t config.OpenTarget) tea.Cmd {
	ref := db.ObjRef{Schema: t.Schema, Name: t.Table, Kind: db.KindTable}
	if ref.Schema == "" && w.caps.Schemas {
		ref.Schema = w.sess.CurrentSchema()
	}
	if t.Column == "" {
		return w.openObject(ref, false)
	}
	if t.Column == db.RawColumn || strings.IndexFunc(t.Column, unicode.IsControl) >= 0 {
		return w.a.fail(fmt.Errorf("the URL names an invalid filter column"))
	}
	op := orDefault(t.Operation, "=")
	if a, ok := openAliases[strings.ToLower(op)]; ok {
		op = a
	}
	for _, o := range db.Operators(w.caps.Dialect) {
		if strings.EqualFold(o, op) {
			op = o
		}
	}
	return w.openFiltered(ref, db.FilterSet{Or: strings.EqualFold(t.Condition, "or"), Filters: []db.Filter{{Column: t.Column, Op: op, Value: t.Value, Enabled: true}}})
}

// ---- MCP activity (M4 item 10, 11) -----------------------------------------------------------

// mcpAddr is where the MCP listener binds: [mcp] listen, else 127.0.0.1:port.
func (a *App) mcpAddr() string {
	if l := a.d.Cfg.MCP.Listen; l != "" {
		return l
	}
	return fmt.Sprintf("127.0.0.1:%d", a.d.Cfg.MCP.Port)
}

func (w *Workspace) mcpLog() tea.Cmd {
	a := w.a
	if a.mcp == nil {
		return a.say("the MCP server is off: set enabled = true under [mcp]")
	}
	scheme := "http"
	if a.d.Cfg.MCP.TLSCert != "" {
		scheme = "https (TLS)"
	}
	lines := []string{"listening on " + a.mcpAddr() + " over " + scheme, mcpToolsHelp, ""}
	if a.mcpErr != nil {
		lines = []string{"✗ could not listen: " + a.mcpErr.Error(), ""}
	}
	es := a.mcp.Log()
	for i := len(es) - 1; i >= 0; i-- {
		e := es[i]
		l := fmt.Sprintf("%s  %s  %s  %s  %s", e.Time.Format("15:04:05"), e.Token, e.Tool, e.Connection, oneLine(e.SQL))
		if e.Err != "" {
			l += "  ✗ " + e.Err
		}
		lines = append(lines, l)
	}
	if len(es) == 0 {
		lines = append(lines, "no requests yet")
	}
	a.push(&textViewDlg{title: "MCP activity", lines: lines, actions: []viewAction{
		{Key: "e", Label: "e: export CSV", Do: func() tea.Cmd { return w.mcpExportCSV(es) }},
		{Key: "c", Label: "c: clients", Do: func() tea.Cmd { return w.mcpClients(es) }},
	}})
	return nil
}

func (w *Workspace) mcpExportCSV(es []mcp.Entry) tea.Cmd {
	w.a.push(newForm("Export the MCP log as CSV", []formField{{Key: "path", Label: "File", Kind: fText, Value: "mcp-log.csv"}}, func(v map[string]string) tea.Cmd {
		path := strings.TrimSpace(v["path"])
		return w.a.ifFree(func() tea.Cmd {
			err := writeAtomic(path, func(f io.Writer) error {
				cw := csv.NewWriter(f)
				cw.Write([]string{"time", "token", "tool", "connection", "sql", "error"})
				for _, e := range es {
					cw.Write([]string{e.Time.Format(time.RFC3339), e.Token, e.Tool, e.Connection, e.SQL, e.Err})
				}
				cw.Flush()
				return cw.Error()
			})
			if err != nil {
				return w.a.fail(err)
			}
			return w.a.say("saved %s to %s", plural(len(es), "request"), path)
		}, path)
	}))
	return nil
}

// mcpClients lists the tokens seen in the log. shortcut: the MCP server has no per-token
// disconnect, so this view only shows them; remove the token from [[mcp.token]] and restart to cut one off.
func (w *Workspace) mcpClients(es []mcp.Entry) tea.Cmd {
	type seen struct {
		n    int
		last time.Time
		conn string
	}
	by := map[string]*seen{}
	var order []string
	for _, e := range es {
		s := by[e.Token]
		if s == nil {
			s = &seen{}
			by[e.Token] = s
			order = append(order, e.Token)
		}
		s.n++
		s.last, s.conn = e.Time, e.Connection
	}
	lines := []string{"Clients seen in the activity log (disconnecting one is not available here)", ""}
	for _, t := range order {
		s := by[t]
		lines = append(lines, fmt.Sprintf("%-20s %4d requests   last %s on %s", t, s.n, s.last.Format("15:04:05"), orDefault(s.conn, "-")))
	}
	if len(order) == 0 {
		lines = append(lines, "no clients yet")
	}
	w.a.push(&textViewDlg{title: "MCP clients", lines: lines})
	return nil
}

// stackLabel names a workspace in the session switcher: [general] show_connection_name / show_database_name
// pick the parts; with neither set both are shown.
func (a *App) stackLabel(w *Workspace) string {
	g := a.d.Cfg.General
	switch {
	case g.ShowConnectionName && !g.ShowDatabaseName:
		return w.conn.Name
	case g.ShowDatabaseName && !g.ShowConnectionName:
		return w.sess.CurrentDatabase()
	}
	return w.conn.Name + " / " + w.sess.CurrentDatabase()
}

// ---- row detail: Search for field… (M4 item 11) -------------------------------------------------

// searchField moves the grid cursor (and so the detail pane) to the best-matching column; [sidebar] matching
// picks fuzzy or contains, like the sidebar search.
func (t *tableTab) searchField() tea.Cmd {
	t.ws.a.push(newForm("Search for field", []formField{{Key: "q", Label: "Field", Kind: fText}}, func(v map[string]string) tea.Cmd {
		q := strings.TrimSpace(v["q"])
		best, score := -1, -1
		for i, c := range t.grid.Cols {
			if s, ok := matchName(q, c.Name, t.ws.a.d.Cfg.Sidebar.Matching != "contains"); ok && s > score {
				best, score = i, s
			}
		}
		if best < 0 {
			return t.ws.a.say("no field matches %q", q)
		}
		t.grid.GoCol(best, false)
		return nil
	}))
	return nil
}

// exportName expands [export] filename ({connection} {database} {table} {tag} {date} {time}) plus the extension.
func (w *Workspace) exportName(table, ext string) string {
	name := export.Filename(orDefault(w.a.d.Cfg.Export.Filename, "{table}-{date}"), w.exportVars(table))
	if strings.HasSuffix(strings.ToLower(name), "."+strings.ToLower(ext)) { // "{table}.csv" already names the extension
		return name
	}
	return name + "." + ext
}

// exportVars are the {tokens} a file-name pattern can use.
func (w *Workspace) exportVars(table string) map[string]string {
	return map[string]string{"connection": w.conn.Name, "database": w.sess.CurrentDatabase(), "table": table, "tag": w.conn.Tag}
}

// ---- process list: sort, copy, export (M4 item 11) ----------------------------------------------

var procColumns = []string{"elapsed", "id", "user", "db", "state"}

// sortProcesses orders by procColumns[by]; elapsed is slowest first.
func sortProcesses(ps []db.ProcessInfo, by int) {
	sort.SliceStable(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]
		switch procColumns[by] {
		case "id":
			return a.ID < b.ID
		case "user":
			return a.User < b.User
		case "db":
			return a.DB < b.DB
		case "state":
			return a.State < b.State
		}
		return a.Elapsed > b.Elapsed
	})
}

// processText renders the rows, header first, joined by sep.
func processText(ps []db.ProcessInfo, sep string) string {
	lines := []string{strings.Join([]string{"id", "user", "db", "state", "elapsed", "query"}, sep)}
	for _, x := range ps {
		lines = append(lines, strings.Join([]string{x.ID, x.User, x.DB, x.State, x.Elapsed.String(), strings.Join(strings.Fields(x.Query), " ")}, sep))
	}
	return strings.Join(lines, "\n")
}

func (w *Workspace) exportProcesses(ps []db.ProcessInfo) tea.Cmd {
	w.a.push(newForm("Export the process list", []formField{{Key: "path", Label: "File (CSV)", Kind: fText, Value: "processes.csv"}}, func(v map[string]string) tea.Cmd {
		path := strings.TrimSpace(v["path"])
		return w.a.ifFree(func() tea.Cmd {
			err := writeAtomic(path, func(f io.Writer) error {
				cw := csv.NewWriter(f)
				cw.Write([]string{"id", "user", "db", "state", "elapsed", "query"})
				for _, x := range ps {
					cw.Write([]string{x.ID, x.User, x.DB, x.State, x.Elapsed.String(), x.Query})
				}
				cw.Flush()
				return cw.Error()
			})
			if err != nil {
				return w.a.fail(err)
			}
			return w.a.say("saved %s to %s", plural(len(ps), "process"), path)
		}, path)
	}))
	return nil
}

// ---- clipboard (M5) --------------------------------------------------------------------------

// setClip copies text with OSC 52 through internal/clip when that applies (over SSH), else with Bubble Tea's clipboard.
func setClip(text string) tea.Cmd {
	return func() tea.Msg {
		if clip.Available() && clip.Write(text) == nil {
			return nil
		}
		return tea.SetClipboard(text)()
	}
}

func (a *App) copyText(text, what string) tea.Cmd {
	return tea.Batch(setClip(text), a.say("copied %s", what))
}

// ---- assistant: model list, message copy / resend / delete (M4 item 11) -----------------------

// pickModel lists the provider's models (OpenAI-compatible: GET /models; Anthropic: the current ids) and
// uses the pick for this session. shortcut: ACP custom agents and Copilot / Codex sign-in are not here.
func (t *assistantTab) pickModel() tea.Cmd {
	w := t.ws
	cfg := t.aiCfg()
	show := func(ids []string) {
		var items []pickItem
		for _, id := range ids {
			d := ""
			if id == orDefault(w.m3.model, cfg.Model) {
				d = "current"
			}
			items = append(items, pickItem{Label: id, Detail: d, Value: id})
		}
		w.a.push(&pickerDlg{title: "Assistant model (this session)", filter: true, items: items, empty: "the provider returned no models",
			onPick: func(it pickItem, _ bool) tea.Cmd {
				w.m3.model = it.Value.(string)
				return w.a.say("assistant model: %s", w.m3.model)
			}})
	}
	if cfg.Provider != "openai" {
		show([]string{string(anthropic.ModelClaudeOpus5_5), string(anthropic.ModelClaudeSonnet5_5), string(anthropic.ModelClaudeHaiku5_5)})
		return nil
	}
	return liveCmd(func() func() tea.Cmd {
		key, err := config.ResolveAIKey(cfg) // may run api_key_cmd: keep it off the UI loop
		var ids []string
		if err == nil {
			ids, err = openAIModels(cfg, key)
		}
		return func() tea.Cmd {
			if err != nil {
				return w.a.fail(fmt.Errorf("models: %w", err))
			}
			show(ids)
			return nil
		}
	})
}

// openAIModels asks GET <base_url>/models for the model ids.
func openAIModels(cfg config.AI, key string) ([]string, error) {
	base := orDefault(cfg.BaseURL, "https://api.openai.com/v1")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	var out struct{ Data []struct{ ID string } }
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, err
	}
	var ids []string
	for _, m := range out.Data {
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	return ids, nil
}

// messages lists the conversation, newest first: Enter resends a question (copies an answer), c copies, d deletes.
func (t *assistantTab) messages() tea.Cmd {
	w := t.ws
	p := &pickerDlg{title: "Assistant messages", hint: "Enter: resend (copy an answer)   c: copy   d: delete   Esc: close", empty: "no messages yet"}
	rebuild := func() {
		p.items = nil
		for i := len(w.m3.chat) - 1; i >= 0; i-- {
			m := w.m3.chat[i]
			p.items = append(p.items, pickItem{Label: m.Role + ": " + oneLine(m.Content), Value: i})
		}
		p.sel = clamp(p.sel, 0, max(0, len(p.items)-1))
	}
	rebuild()
	p.keys = map[string]func(pickItem, bool) tea.Cmd{
		"c": func(it pickItem, ok bool) tea.Cmd {
			if !ok {
				return nil
			}
			return w.a.copyText(w.m3.chat[it.Value.(int)].Content, "message")
		},
		"d": func(it pickItem, ok bool) tea.Cmd {
			if !ok {
				return nil
			}
			i := it.Value.(int)
			if t.running && i >= len(w.m3.chat)-1 {
				return w.a.say("the assistant is still answering that question (Esc stops it)")
			}
			if i >= len(w.m3.chat) {
				return nil
			}
			w.m3.chat = append(w.m3.chat[:i:i], w.m3.chat[i+1:]...)
			rebuild()
			return nil
		},
	}
	p.onPick = func(it pickItem, _ bool) tea.Cmd {
		i := it.Value.(int)
		m := w.m3.chat[i]
		if m.Role != "user" {
			return w.a.copyText(m.Content, "answer")
		}
		if t.running {
			return w.a.say("the assistant is still answering (Esc stops it)")
		}
		w.m3.chat = w.m3.chat[:i] // the question and everything after it go; send asks again
		t.in.Set(m.Content)
		return t.send()
	}
	w.a.push(p)
	return nil
}

// ---- structure grid: move columns (M4 item 11) ----------------------------------------------------

// structReordered: the existing columns are no longer in the order they were loaded in.
func (t *tableTab) structReordered() bool {
	if t.structGrid == nil || t.creating {
		return false
	}
	var cur []string
	for _, r := range t.structGrid.Rows {
		if m, ok := r.Meta.(colMeta); ok && r.State != rowNew && r.State != rowDeleted {
			cur = append(cur, m.orig)
		}
	}
	i := 0
	for _, c := range t.loaded.cols {
		if i < len(cur) && cur[i] == c.Name {
			i++
		}
	}
	return i != len(cur) // loaded order, minus deleted columns, must contain cur in sequence
}

// moveColumn is alt+up / alt+down on the structure grid where the engine can place columns (MySQL, ClickHouse).
func (t *tableTab) moveColumn(g *Grid, d int) tea.Cmd {
	if t.view != viewStructure || !g.Editable {
		return nil
	}
	if !t.ws.caps.ReorderColumns {
		return t.ws.a.say("%s cannot reorder columns", t.ws.conn.Driver)
	}
	j := g.row + d
	if j < 0 || j >= len(g.Rows) {
		return nil
	}
	g.snapshot()
	g.Rows[g.row], g.Rows[j] = g.Rows[j], g.Rows[g.row]
	for _, i := range []int{g.row, j} { // the # column follows the position
		g.own(i).Cur[scNum] = int64(i + 1)
	}
	g.row = j
	g.scroll()
	return nil
}

// ---- mouse: click selects a sidebar item or a grid cell (M5) ---------------------------------------

// click handles a left click at screen cell (x, y): row 0 is the top bar, the body follows.
// shortcut: only the sidebar list and the table data grid react; tabs, dialogs, query results and split panes ignore clicks.
func (w *Workspace) click(x, y int) tea.Cmd {
	sbW, _, _, mainW, mainH := layoutOf(w.showSB, w.showDetail, w.showConsole, w.W, w.H)
	y-- // the top bar
	if y < 0 || y >= mainH || w.m3.split {
		return nil
	}
	switch {
	case x < sbW:
		w.focusSB = true
		rows := w.sb.rows()
		i := w.sb.top + (y - 1 - 2) // frame border, then the two header lines of the sidebar
		if i < 0 || i >= len(rows) {
			return nil
		}
		if i == w.sb.sel { // a second click on the selected item opens it
			return w.a.key(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
		w.sb.sel = i
	case sbW > 0 && x == sbW, x >= sbW+boolInt(sbW > 0)+mainW:
	default:
		t, ok := w.tab().(*tableTab)
		if !ok {
			return nil
		}
		mainX := sbW + boolInt(sbW > 0)
		if t.Click(x-(mainX+2), y-1) { // frame border and padding (x only)
			w.focusSB, w.focusDetail = false, false
		}
	}
	return nil
}

// Click moves the grid cursor to the cell under (x, y), relative to the tab's view; the filter bar sits above the grid.
func (t *tableTab) Click(x, y int) bool {
	if t.view != viewData {
		return false
	}
	bar := len(t.filterView(t.ws.a.th, t.grid.W))
	if !t.showFilters && len(t.filters.Active()) > 0 {
		bar = 1
	}
	return t.grid.Click(x, y-bar)
}

// Click puts the cursor on the cell at (x, y) of the grid view (y 0 is the header) and drops the selection.
func (g *Grid) Click(x, y int) bool {
	ri := g.top + y - 1
	if y < 1 || ri >= len(g.Rows) || x < 0 {
		return false
	}
	used := g.gutterW()
	for ci := g.left; ci < len(g.Cols); ci++ {
		used += g.Cols[ci].Width + 1
		if x < used {
			if x < used-g.Cols[ci].Width-1 { // the gutter
				return false
			}
			g.row, g.col, g.selecting = ri, ci, false
			g.scroll()
			return true
		}
	}
	return false
}

// ---- metrics board: duplicate (M4 item 11) ------------------------------------------------------

func (w *Workspace) duplicateBoard(b board.Board) tea.Cmd {
	if b.Err != nil {
		return w.a.fail(b.Err)
	}
	nb := b
	nb.Path = ""
	for n := 1; ; n++ {
		nb.Name = b.Name + " copy"
		if n > 1 {
			nb.Name += " " + strconv.Itoa(n)
		}
		err := nb.Save()
		if err == nil {
			return w.a.say("duplicated as %s (reopen the boards list to see it)", nb.Name)
		}
		if n > 20 || !strings.Contains(err.Error(), "already exists") {
			return w.a.fail(err)
		}
	}
}
