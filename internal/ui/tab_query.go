package ui

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"tuidb/internal/db"
	"tuidb/internal/keymap"
	"tuidb/internal/store"
)

type resultView struct {
	title   string
	grid    *Grid
	msg     string
	isErr   bool
	hasRows bool
	took    time.Duration
	trunc   bool
	rows    int
	stmt    string
	pinned  bool       // survives the next run
	ref     *db.ObjRef // set when the result maps to one table and can be edited
	find    string
}

type queryTab struct {
	ws           *Workspace
	id           int
	name         string
	ed           *Editor
	res          []*resultView
	rc           int
	focusResults bool
	hideResults  bool

	running bool
	cancel  context.CancelFunc
	queue   []db.Stmt
	batch   int
	started time.Time
	limit   int
	status  string

	cols     map[string][]string // lowercased table name -> column names (autocomplete)
	loading  map[string]bool
	savedAs  string // path of the saved query this tab edits (auto-save)
	lastAuto string
	firstErr int // index of the first failed result in this batch, -1 if none
}

func newQueryTab(ws *Workspace, id int) *queryTab {
	c := ws.a.d.Cfg.Editor
	t := &queryTab{ws: ws, id: id, name: fmt.Sprintf("SQL %d", id), cols: map[string][]string{}, loading: map[string]bool{},
		limit: ws.a.d.State.RowLimit(ws.conn.Name, 1000)}
	t.ed = NewEditor(ws.caps.Dialect, EditorOpts{
		TabWidth: c.IndentWidth, UseTabs: c.Indent == "tabs", CloseBraces: c.CloseBraces, Uppercase: c.UppercaseKeywords,
		CompleteKey: c.CompleteKey, Highlight: c.HighlightCurrent, Autocomplete: c.Autocomplete,
	})
	t.ed.Complete = t.complete
	return t
}

func (t *queryTab) ID() int       { return t.id }
func (t *queryTab) Title() string { return t.name }
func (t *queryTab) Init() tea.Cmd { return nil }

func (t *queryTab) Context() string {
	if t.focusResults && !t.hideResults && len(t.res) > 0 {
		return keymap.Results
	}
	return keymap.Editor
}

// ---- autocomplete -----------------------------------------------------------------------

var refRe = regexp.MustCompile(`(?i)\b(?:from|join|update|into|table)\s+((?:"[^"]+"|\w+)(?:\.(?:"[^"]+"|\w+))?)`)

func (t *queryTab) complete(prefix string) []Candidate {
	var all []Candidate
	qual := ""
	word := prefix
	if i := strings.LastIndex(prefix, "."); i >= 0 {
		qual, word = strings.ToLower(strings.Trim(prefix[:i], `"`)), prefix[i+1:]
	}
	if qual != "" {
		for _, c := range t.cols[qual] {
			all = append(all, Candidate{Text: c, Kind: "column"})
		}
		for _, o := range t.ws.sb.objs { // schema.table
			if strings.EqualFold(o.Schema, qual) {
				all = append(all, Candidate{Text: o.Name, Kind: string(o.Kind)})
			}
		}
		return RankCandidates(word, all, 50)
	}
	seen := map[string]bool{}
	add := func(text, kind string) {
		k := strings.ToLower(text)
		if !seen[k] {
			seen[k] = true
			all = append(all, Candidate{Text: text, Kind: kind})
		}
	}
	for _, o := range t.ws.sb.objs {
		add(o.Name, string(o.Kind))
	}
	for _, m := range refRe.FindAllStringSubmatch(t.ed.Text(), -1) {
		name := strings.ToLower(strings.Trim(m[1], `"`))
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		for _, c := range t.cols[name] {
			add(c, "column")
		}
	}
	if qs, err := t.ws.a.d.Queries.List(); err == nil {
		for k, q := range store.Keywords(qs) {
			if !seen[strings.ToLower(k)] {
				seen[strings.ToLower(k)] = true
				all = append(all, Candidate{Text: k, Kind: "saved", Insert: q.SQL})
			}
		}
	}
	switch t.ws.caps.Dialect {
	case "redis":
		for k := range redisCommands {
			add(k, "keyword")
		}
	case "mongo":
		for k := range mongoWords {
			add(k, "keyword")
		}
	default:
		for k := range sqlKeywords {
			add(k, "keyword")
		}
	}
	return RankCandidates(word, all, 50)
}

type colsLoadedMsg struct {
	name string
	cols []string
}

// prefetch loads columns for tables the buffer mentions, so t.col / bare column completion works.
func (t *queryTab) prefetch() tea.Cmd {
	var cmds []tea.Cmd
	for _, m := range refRe.FindAllStringSubmatch(t.ed.Text(), -1) {
		name := strings.Trim(m[1], `"`)
		short := name
		if i := strings.LastIndex(name, "."); i >= 0 {
			short = name[i+1:]
		}
		key := strings.ToLower(short)
		if _, ok := t.cols[key]; ok || t.loading[key] || len(cmds) >= 3 {
			continue
		}
		cachedHit := false
		for ref, cols := range t.ws.meta { // reuse what table tabs or the sidebar already fetched
			if strings.EqualFold(ref.Name, short) {
				names := make([]string, len(cols))
				for i, c := range cols {
					names[i] = c.Name
				}
				t.cols[key], cachedHit = names, true
				break
			}
		}
		if cachedHit {
			continue
		}
		var ref *db.ObjRef
		for _, o := range t.ws.sb.objs {
			if strings.EqualFold(o.Name, short) && (o.Kind == db.KindTable || o.Kind == db.KindView || o.Kind == db.KindMatView) {
				r := o.ObjRef
				ref = &r
				break
			}
		}
		if ref == nil {
			continue
		}
		t.loading[key] = true
		r := *ref
		cmds = append(cmds, t.ws.async(t.id, func(ctx context.Context, sess db.Session) tea.Msg {
			cols, err := sess.Columns(ctx, r)
			names := make([]string, 0, len(cols))
			for _, c := range cols {
				names = append(names, c.Name)
			}
			if err != nil {
				names = nil
			}
			return colsLoadedMsg{name: key, cols: names}
		}))
	}
	return tea.Batch(cmds...)
}

// ---- running ----------------------------------------------------------------------------

// queryTimeoutMsg fires when a batch has run longer than general.query_timeout_secs.
type queryTimeoutMsg struct{ batch int }

func (t *queryTab) timeoutTick() tea.Cmd {
	secs := t.ws.a.d.Cfg.General.QueryTimeoutSecs
	if secs <= 0 || t.ws.a.d.NoTimers {
		return nil
	}
	ws, tab, batch := t.ws.id, t.id, t.batch
	return tea.Tick(time.Duration(secs)*time.Second, func(time.Time) tea.Msg {
		return wsMsg{ws: ws, tab: tab, msg: queryTimeoutMsg{batch}}
	})
}

type queryDoneMsg struct {
	batch int
	stmt  db.Stmt
	res   *db.Result
	err   error
	took  time.Duration
}

func (t *queryTab) runCurrent() tea.Cmd {
	if sel := t.ed.Selection(); strings.TrimSpace(sel) != "" {
		return t.run(db.Split(t.ws.caps.Dialect, sel))
	}
	s, ok := t.ed.CurrentStatement()
	if !ok {
		return t.ws.a.say("nothing to run")
	}
	return t.run([]db.Stmt{s})
}

func (t *queryTab) runAll() tea.Cmd {
	return t.run(db.Split(t.ws.caps.Dialect, t.ed.Text()))
}

func (t *queryTab) run(stmts []db.Stmt) tea.Cmd {
	if len(stmts) == 0 {
		return t.ws.a.say("nothing to run")
	}
	if t.running {
		return t.ws.a.say("a query is already running; cancel it first")
	}
	var gated []db.Statement
	for _, s := range stmts {
		gated = append(gated, db.Statement{Text: s.Text})
	}
	return t.ws.gate(gated, func() tea.Cmd { return t.start(stmts) })
}

func (t *queryTab) start(stmts []db.Stmt) tea.Cmd {
	var keep []*resultView // pinned results stay; everything else is replaced
	for _, r := range t.res {
		if r.pinned {
			keep = append(keep, r)
		}
	}
	t.running, t.queue, t.res, t.rc, t.status, t.firstErr = true, stmts, keep, 0, "", -1
	t.batch++
	t.started = time.Now()
	t.focusResults = false
	return tea.Batch(t.next(), t.timeoutTick())
}

func (t *queryTab) next() tea.Cmd {
	if len(t.queue) == 0 {
		t.running = false
		if t.cancel != nil {
			t.cancel()
		}
		return nil
	}
	s := t.queue[0]
	t.queue = t.queue[1:]
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	ws, sess, batch, tab, limit := t.ws, t.ws.sess, t.batch, t.id, t.limit
	return func() tea.Msg {
		start := time.Now()
		res, err := sess.Exec(ctx, s.Text, limit)
		return wsMsg{ws: ws.id, tab: tab, msg: queryDoneMsg{batch: batch, stmt: s, res: res, err: err, took: time.Since(start)}}
	}
}

// Close cancels a running statement when the tab goes away.
func (t *queryTab) Close() {
	if t.cancel != nil {
		t.cancel()
	}
	t.queue = nil
	t.running = false
}

func (t *queryTab) Cancel() tea.Cmd {
	if !t.running {
		return nil
	}
	if t.cancel != nil {
		t.cancel()
	}
	t.queue = nil
	t.batch++ // late results of the cancelled statement are dropped
	t.running = false
	t.status = "cancelled"
	return t.ws.a.say("query cancelled")
}

func (t *queryTab) Handle(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case queryTimeoutMsg:
		if m.batch != t.batch || !t.running {
			return nil
		}
		secs := t.ws.a.d.Cfg.General.QueryTimeoutSecs
		t.ws.a.push(&pickerDlg{title: fmt.Sprintf("The server is taking too long (over %ds)", secs), hint: "Enter: choose   Esc: keep waiting",
			items: []pickItem{{Label: "Keep waiting", Value: "wait"}, {Label: "Cancel the query", Value: "cancel"}},
			onPick: func(it pickItem, _ bool) tea.Cmd {
				if it.Value.(string) == "cancel" {
					return t.Cancel()
				}
				return t.timeoutTick()
			}})
		return nil
	case resultColsMsg:
		t.applyResultCols(m)
	case colsLoadedMsg:
		t.cols[m.name] = m.cols
		delete(t.loading, m.name)
	case queryDoneMsg:
		if m.batch != t.batch {
			return nil
		}
		t.ws.record(m.stmt.Text, "editor", m.took, m.err)
		rv := t.toResult(m)
		if t.ws.a.d.Cfg.Editor.ResultMode == "append" && len(t.res) > 0 {
			rv.title = t.res[0].title
		}
		t.res = append(t.res, rv)
		if m.err != nil && t.firstErr < 0 {
			t.firstErr = len(t.res) - 1
		}
		t.rc = len(t.res) - 1
		if t.firstErr >= 0 {
			t.rc = t.firstErr // show the failure, not whatever ran after it
		}
		if m.err != nil && t.ws.a.d.Cfg.Editor.ReturnOnError {
			t.queue = nil
		}
		t.status = fmt.Sprintf("%d statement(s) · %s", len(t.res), time.Since(t.started).Round(time.Millisecond))
		cmd := tea.Batch(t.next(), t.makeEditable(len(t.res)-1))
		if !t.running && m.err != nil && len(t.res) == 1 {
			return tea.Batch(cmd, t.ws.a.fail(m.err))
		}
		return cmd
	default:
		if cmd, ok := t.ws.handleGlobal(msg); ok {
			return cmd
		}
	}
	return nil
}

func (t *queryTab) toResult(m queryDoneMsg) *resultView {
	title := m.stmt.Label
	if title == "" {
		title = fmt.Sprintf("Query %d", len(t.res)+1)
	}
	rv := &resultView{title: title, took: m.took, stmt: m.stmt.Text}
	if m.err != nil {
		rv.isErr, rv.msg = true, m.err.Error()
		return rv
	}
	r := m.res
	rv.trunc, rv.msg = r.Truncated, r.Message
	if !r.HasRows {
		if rv.msg == "" {
			rv.msg = fmt.Sprintf("%d row(s) affected", r.Affected)
		}
		return rv
	}
	cols := make([]gridCol, len(r.Columns))
	for i, c := range r.Columns {
		cols[i] = gridCol{Name: c.Name, Type: c.Type}
	}
	rv.hasRows, rv.rows = true, len(r.Rows)
	rv.grid = &Grid{}
	rv.grid.SetData(cols, r.Rows)
	return rv
}

// ---- input & actions --------------------------------------------------------------------

func (t *queryTab) Input(msg tea.Msg) tea.Cmd {
	if t.focusResults && !t.hideResults && len(t.res) > 0 {
		return t.resultsInput(msg)
	}
	switch m := msg.(type) {
	case tea.ClipboardMsg:
		t.ed.Handle(tea.PasteMsg{Content: m.Content})
		return t.afterEdit()
	case tea.KeyPressMsg:
		switch m.Keystroke() {
		case "ctrl+c":
			if sel := t.ed.Selection(); sel != "" {
				return tea.SetClipboard(sel)
			}
			return nil
		case "ctrl+x":
			if sel := t.ed.Selection(); sel != "" {
				t.ed.push("cut")
				t.ed.deleteSelection()
				t.ed.Changed = true
				return tea.Batch(tea.SetClipboard(sel), t.afterEdit())
			}
			return nil
		case "ctrl+v":
			return tea.ReadClipboard
		case "esc":
			t.ed.anchor = nil
			return nil
		}
	}
	if t.ed.Handle(msg) {
		return t.afterEdit()
	}
	return nil
}

func (t *queryTab) afterEdit() tea.Cmd {
	if !t.ed.Changed {
		return nil
	}
	t.ed.Changed = false
	var cmds []tea.Cmd
	cmds = append(cmds, t.prefetch())
	if t.savedAs != "" && t.ws.a.d.Cfg.Editor.AutoSaveQueries {
		if text := t.ed.Text(); text != t.lastAuto { // saved queries auto-save while editing
			t.lastAuto = text
			if err := t.ws.a.d.Queries.Update(t.savedAs, text); err != nil {
				cmds = append(cmds, t.ws.a.fail(err))
			}
		}
	}
	return tea.Batch(cmds...)
}

func (t *queryTab) resultsInput(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	rv := t.res[t.rc]
	if rv.grid != nil && t.resultEditKeys(rv.grid, msg) {
		return nil
	}
	key := keymap.FromMsg(k)
	switch key {
	case "[", "ctrl+left":
		t.rc = max(0, t.rc-1)
		return nil
	case "]", "ctrl+right":
		t.rc = min(len(t.res)-1, t.rc+1)
		return nil
	case "esc":
		t.focusResults = false
		return nil
	case "p":
		rv.pinned = !rv.pinned
		return t.ws.a.say("%s %s", rv.title, map[bool]string{true: "pinned: it stays when you run again", false: "unpinned"}[rv.pinned])
	}
	g := rv.grid
	if g == nil {
		return nil
	}
	extend := strings.HasPrefix(key, "shift+")
	switch strings.TrimPrefix(key, "shift+") {
	case "down", "j":
		g.Move(1, 0, extend)
	case "up", "k":
		g.Move(-1, 0, extend)
	case "left", "h":
		g.Move(0, -1, extend)
	case "right", "l":
		g.Move(0, 1, extend)
	case "pgdown":
		g.Page(1, extend)
	case "pgup":
		g.Page(-1, extend)
	case "home":
		g.GoCol(0, extend)
	case "end":
		g.GoCol(len(g.Cols)-1, extend)
	case "g":
		g.GoRow(0, false)
	case "G":
		g.GoRow(len(g.Rows)-1, false)
	case "/":
		t.ws.a.push(newForm("Find in this result", []formField{{Key: "q", Label: "Text", Kind: fText, Value: rv.find}}, func(v map[string]string) tea.Cmd {
			rv.find = v["q"]
			return t.findIn(rv, 1)
		}))
	case "n":
		return t.findIn(rv, 1)
	case "N":
		return t.findIn(rv, -1)
	case "c":
		return t.copyOut(g.CopyCells(FmtPlain, "result"), "cells")
	case "y":
		return t.copyOut(g.CopyRows(FmtCSV, "result"), "rows")
	case "enter":
		if g.Editable && g.StartEdit() {
			return nil
		}
		if v, ok := g.CursorValue(); ok {
			text := plain(v)
			t.ws.a.push(newQuickLook(g.Cols[g.col].Name, text))
		}
	}
	return nil
}

func (t *queryTab) findIn(rv *resultView, dir int) tea.Cmd {
	if rv.find == "" {
		return t.ws.a.say("press / to search this result")
	}
	if !rv.grid.Find(rv.find, dir) {
		return t.ws.a.say("%q is not in this result", rv.find)
	}
	return nil
}

func (t *queryTab) copyOut(text, what string) tea.Cmd {
	if text == "" {
		return nil
	}
	return tea.Batch(tea.SetClipboard(text), t.ws.a.say("copied %s", what))
}

func (t *queryTab) Action(name string) (bool, tea.Cmd) {
	if ok, cmd := t.resultAction(name); ok {
		return true, cmd
	}
	switch name {
	case "run_current":
		return true, t.runCurrent()
	case "run_all":
		return true, t.runAll()
	case "cancel_query":
		if t.running {
			return true, t.Cancel()
		}
		return false, nil
	case "toggle_comment":
		t.ed.ToggleComment()
		return true, t.afterEdit()
	case "block_comment":
		t.ed.ToggleBlockComment()
		return true, t.afterEdit()
	case "toggle_results":
		t.hideResults = !t.hideResults
		if t.hideResults {
			t.focusResults = false
		}
		return true, nil
	case "focus_down":
		if len(t.res) > 0 && !t.hideResults {
			t.focusResults = true
		}
		return true, nil
	case "focus_up":
		t.focusResults = false
		return true, nil
	case "add_to_queries":
		return true, t.addToQueries()
	case "save_sql_as":
		return true, t.saveAs()
	case "set_limit":
		return true, t.pickLimit()
	case "beautify", "uglify", "find", "find_replace", "select_next_occurrence", "select_all_occurrences":
		return true, t.ws.a.say("%s arrives in a later milestone", name)
	case "reload_tab":
		return true, nil
	}
	return false, nil
}

// Do lets the workspace route undo/redo to the editor.
func (t *queryTab) Do(name string) tea.Cmd {
	if rv := t.editableResult(); rv != nil && t.focusResults {
		switch name {
		case "undo":
			rv.grid.Undo()
		case "redo":
			rv.grid.Redo()
		}
		return nil
	}
	switch name {
	case "undo":
		t.ed.Undo()
	case "redo":
		t.ed.Redo()
	}
	return t.afterEdit()
}

func (t *queryTab) pickLimit() tea.Cmd {
	var items []pickItem
	for _, n := range []int{100, 500, 1000, 5000, 10000, 50000, 100000, 500000, 0} {
		label := fmt.Sprintf("%d rows", n)
		if n == 0 {
			label = "No limit"
		}
		if n == t.limit {
			label = "• " + label
		}
		items = append(items, pickItem{Label: label, Value: n})
	}
	t.ws.a.push(&pickerDlg{title: "Row limit for query results", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		t.limit = it.Value.(int)
		t.ws.a.d.State.SetRowLimit(t.ws.conn.Name, t.limit)
		return nil
	}})
	return nil
}

func (t *queryTab) addToQueries() tea.Cmd {
	text := t.ed.Selection()
	if strings.TrimSpace(text) == "" {
		if s, ok := t.ed.CurrentStatement(); ok {
			text = s.Text
		}
	}
	if strings.TrimSpace(text) == "" {
		return t.ws.a.say("nothing to save")
	}
	t.ws.a.push(newForm("Add to saved queries", []formField{
		{Key: "name", Label: "Name", Kind: fText},
		{Key: "keywords", Label: "Keywords", Kind: fText, Value: ""},
		{Key: "folder", Label: "Folder", Kind: fText},
	}, func(v map[string]string) tea.Cmd {
		var kws []string
		for _, k := range strings.Split(v["keywords"], ",") {
			if k = strings.TrimSpace(k); k != "" {
				kws = append(kws, k)
			}
		}
		q, err := t.ws.a.d.Queries.Save(strings.Trim(v["folder"], "/ "), v["name"], kws, text)
		if err != nil {
			return t.ws.a.fail(err)
		}
		return t.ws.a.say("saved %s", q.Name)
	}))
	return nil
}

func (t *queryTab) saveAs() tea.Cmd {
	t.ws.a.push(newForm("Save SQL to file", []formField{{Key: "path", Label: "File", Kind: fText, Value: "query.sql"}}, func(v map[string]string) tea.Cmd {
		path := strings.TrimSpace(v["path"])
		if path == "" {
			return t.ws.a.fail(fmt.Errorf("file name required"))
		}
		if err := writeFile(path, t.ed.Text()); err != nil {
			return t.ws.a.fail(err)
		}
		return t.ws.a.say("saved %s", path)
	}))
	return nil
}

// ---- view -------------------------------------------------------------------------------

func (t *queryTab) View(th Theme, W, H int) string {
	showRes := !t.hideResults && (len(t.res) > 0 || t.running)
	edH := H - 1
	resH := 0
	if showRes {
		resH = max(4, (H-1)*45/100)
		edH = H - 1 - resH
	}
	if edH < 3 {
		edH, resH = max(1, H-1), 0
	}
	t.ed.W, t.ed.H = W, edH
	editorFocused := !t.ws.focusSB && !(t.focusResults && resH > 0)
	parts := []string{t.ed.View(th, editorFocused), t.runBar(th, W)}
	if resH > 0 {
		parts = append(parts, t.resultsView(th, W, resH-1))
	}
	return strings.Join(parts, "\n")
}

func (t *queryTab) runBar(th Theme, W int) string {
	pre := keymap.Display(t.ws.a.d.Keys.Prefix)
	key := func(action string) string {
		for _, b := range t.ws.a.d.Keys.Bindings(keymap.Editor) {
			if b.Action == action {
				return pre + " " + keymap.Display(b.Key)
			}
		}
		return "?"
	}
	limit := "no limit"
	if t.limit > 0 {
		limit = fmt.Sprintf("limit %d", t.limit)
	}
	left := " " + th.chips(key("run_current"), "run", key("run_all"), "run all", key("set_limit"), limit)
	status := th.Dim.Render(t.status)
	if t.running {
		status = th.Warn.Render(fmt.Sprintf("⏳ running %s · %s cancels", time.Since(t.started).Round(100*time.Millisecond), key("cancel_query")))
	} else if t.status == "" && len(t.res) == 0 {
		status = th.Dim.Render("type SQL, then run the statement under the cursor")
	}
	gap := W - width(left) - width(status) - 1
	if gap < 1 {
		return th.HeaderCell.Render(fit(left, W))
	}
	return th.HeaderCell.Render(left + strings.Repeat(" ", gap) + status + " ")
}

// Hint is shown in the frame's bottom edge.
func (t *queryTab) Hint(th Theme) string {
	if t.focusResults && len(t.res) > 0 && !t.hideResults {
		return resultHint(th, t.curResult())
	}
	pre := keymap.Display(t.ws.a.d.Keys.Prefix)
	return th.chips(pre+" ↵", "run", pre+" M-j", "results", pre+" a", "save query", "C-a", "select all")
}

func (t *queryTab) resultsView(th Theme, W, H int) string {
	if len(t.res) == 0 {
		return pad(th.Dim.Render("  running…"), W, H+1)
	}
	var tabs []string
	for i, r := range t.res {
		label := " " + r.title + " "
		if r.isErr {
			label = " ✗ " + r.title + " "
		}
		if r.pinned {
			label = " ◆ " + label[1:]
		}
		switch {
		case i == t.rc && t.focusResults:
			tabs = append(tabs, th.TabActive.Render(label))
		case i == t.rc:
			tabs = append(tabs, th.TabActive.Faint(true).Render(label))
		default:
			tabs = append(tabs, th.TabIdle.Render(label))
		}
	}
	rv := t.res[t.rc]
	info := rv.took.Round(time.Microsecond).String()
	if rv.hasRows {
		info = fmt.Sprintf("%d rows · %s", rv.rows, info)
		if rv.trunc {
			info += " (limit reached)"
		}
	}
	bar := fit(strings.Join(tabs, ""), W-width(info)-2) + " " + th.Dim.Render(info) + " "
	lines := []string{fit(bar, W)}
	switch {
	case rv.isErr:
		for _, l := range wrapText(rv.msg, W-4) {
			lines = append(lines, "  "+th.Err.Render(l))
		}
	case rv.hasRows:
		rv.grid.W, rv.grid.H = W, H
		lines = append(lines, rv.grid.View(th, t.focusResults))
		if rv.msg != "" && H > 3 {
			lines = append(lines, th.Dim.Render(fit(" "+rv.msg, W)))
		}
	default:
		for _, l := range wrapText(rv.msg, W-4) {
			lines = append(lines, "  "+th.OK.Render(l))
		}
	}
	out := strings.Split(strings.Join(lines, "\n"), "\n")
	for len(out) < H+1 {
		out = append(out, strings.Repeat(" ", W))
	}
	return strings.Join(out[:H+1], "\n")
}

var _ = sort.Strings
