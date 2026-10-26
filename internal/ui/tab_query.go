package ui

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"webtuidb/internal/db"
	"webtuidb/internal/keymap"
	"webtuidb/internal/store"
)

type resultView struct {
	title     string
	grid      *Grid
	msg       string
	isErr     bool
	hasRows   bool
	took      time.Duration
	trunc     bool
	rows      int
	stmt      string
	pinned    bool       // survives the next run
	ref       *db.ObjRef // set when the result maps to one table and can be edited
	find      string
	raw       *db.Result // the rows as the server sent them: export and chart read these
	chart     bool       // show the chart instead of the grid
	limit     int        // row limit this result was fetched with when "load more" raised it
	matchCase bool       // the result search is case-sensitive
	msgView   bool       // show the message lines (notices, status) instead of the rows
}

type queryTab struct {
	ws           *Workspace
	id           int
	name         string
	ed           *Editor
	other        *Editor // split_down: the second editor of the stack; ed is always the focused one
	focusBottom  bool    // ed is the lower editor of the stack
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
	replace  int // >= 0: the next result replaces this index instead of starting a new set
	closed   bool
	override int               // row limit for the next fetch (load more); 0 = t.limit
	edFind   string            // last editor search text
	unbound  map[string]string // bound statement text -> the text as typed: history and the console never see variable values
	pinning  bool              // a pin or unpin is in flight
	pin      db.Session        // a dedicated connection (db.Pinner): every statement of this tab runs on it
	sum      *batchSummary     // a run of more than summaryOver statements keeps one summary instead of a result tab each
}

func newQueryTab(ws *Workspace, id int) *queryTab {
	c := ws.a.d.Cfg.Editor
	t := &queryTab{ws: ws, id: id, name: fmt.Sprintf("SQL %d", id), cols: map[string][]string{}, loading: map[string]bool{},
		limit: ws.a.d.State.RowLimit(ws.conn.Name, 1000)}
	t.ed = NewEditor(ws.caps.Dialect, EditorOpts{
		TabWidth: c.IndentWidth, UseTabs: c.Indent == "tabs", CloseBraces: c.CloseBraces, Uppercase: c.UppercaseKeywords,
		CompleteKey: c.CompleteKey, Highlight: c.HighlightCurrent, Autocomplete: c.Autocomplete, Wrap: c.Wrap, AddSpace: c.AddSpace,
	})
	t.ed.Complete = t.complete
	t.ed.OnFolds = func(f []int) { ws.a.d.State.SetFolds(t.foldKey(), f) }
	return t
}

// foldKey names this editor in the saved fold state.
func (t *queryTab) foldKey() string {
	if t.savedAs != "" {
		return t.savedAs
	}
	return fmt.Sprintf("%s/tab%d", t.ws.conn.Name, t.id)
}

func (t *queryTab) ID() int { return t.id }
func (t *queryTab) Title() string {
	pin := ""
	if t.pin != nil {
		pin = " [pinned]"
	}
	return t.name + pin + t.ed.vimBadge() + t.ed.blockBadge()
}
func (t *queryTab) Init() tea.Cmd { return nil }

func (t *queryTab) Context() string {
	if t.focusResults && !t.hideResults && len(t.res) > 0 {
		return keymap.Results
	}
	return keymap.Editor
}

// ---- autocomplete -----------------------------------------------------------------------

var noticeRe = regexp.MustCompile(`^(NOTICE|WARNING|INFO|LOG|DEBUG\d?): `)

var refRe = regexp.MustCompile(`(?i)\b(?:from|join|update|into|table)\s+((?:"[^"]+"|\w+)(?:\.(?:"[^"]+"|\w+))?)`)

func (t *queryTab) complete(prefix string) []Candidate {
	var all []Candidate
	qual := ""
	word := prefix
	if i := strings.LastIndex(prefix, "."); i >= 0 {
		qual, word = strings.ToLower(strings.Trim(prefix[:i], `"`)), prefix[i+1:]
		qual = t.aliasAtCursor(qual) // "c." where c is an alias of customers
	}
	if qual != "" {
		if t.ws.caps.Dialect == "mongo" {
			if ms := mongoMethodCandidates(qual); ms != nil {
				return RankCandidates(word, ms, 50)
			}
		}
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
	cfg := t.ws.a.d.Cfg.Editor
	for _, o := range t.ws.sb.objs {
		fn := o.Kind == db.KindFunction || o.Kind == db.KindProcedure
		if fn && !cfg.CompleteFunctions || !fn && !cfg.CompleteTables {
			continue
		}
		add(o.Name, string(o.Kind))
		if cfg.PrefixSchema && o.Schema != "" {
			all[len(all)-1].Insert = o.Schema + "." + o.Name
		}
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
				c := Candidate{Text: k, Kind: "saved", Insert: q.SQL}
				if sn, ok := strings.CutPrefix(k, "snippet:"); ok { // a saved query keyworded snippet:<trigger> expands from <trigger>
					c.Text, c.Kind = sn, "snippet"
				}
				all = append(all, c)
			}
		}
	}
	switch {
	case !cfg.CompleteKeywords:
	case t.ws.caps.Dialect == "redis":
		for k := range redisCommands {
			add(k, "keyword")
		}
	case t.ws.caps.Dialect == "mongo":
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
	ws, tab, batch, gen := t.ws.id, t.id, t.batch, t.ws.gen
	return tea.Tick(time.Duration(secs)*time.Second, func(time.Time) tea.Msg {
		return wsMsg{ws: ws, tab: tab, gen: gen, msg: queryTimeoutMsg{batch}}
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
	if t.ws.a.d.Cfg.Editor.QueryParams {
		if names := t.paramNames(stmts); len(names) > 0 {
			return t.askVars(stmts, t.paramFormat(), func(vals map[string]string, format string) tea.Cmd { return t.runBound(t.bindAs(stmts, vals, format)) })
		}
	}
	return t.runBound(stmts)
}

func (t *queryTab) runBound(stmts []db.Stmt) tea.Cmd {
	var gated []db.Statement
	for _, s := range stmts {
		gated = append(gated, db.Statement{Text: s.Text})
	}
	go_ := func() tea.Cmd { return t.ws.gate(gated, func() tea.Cmd { return t.start(stmts) }) }
	for _, r := range t.res { // an edited result is replaced by the run: ask first, like a table reload
		if !r.pinned && r.grid != nil && r.grid.HasPending() {
			t.ws.a.push(&confirmDlg{title: "Discard pending result edits?", lines: []string{"Running again replaces the results; uncommitted edits in them will be lost."}, yes: "Discard", danger: true, onYes: go_})
			return nil
		}
	}
	return go_()
}

func (t *queryTab) Busy() bool { return t.running }

// CancelWork stops the running statement (prefix . from any pane).
func (t *queryTab) CancelWork() tea.Cmd {
	if !t.running {
		return nil
	}
	return t.Cancel()
}

func (t *queryTab) start(stmts []db.Stmt) tea.Cmd {
	if t.running || t.closed {
		return nil
	}
	var keep []*resultView // pinned results stay; everything else is replaced (result_mode = append keeps all)
	for _, r := range t.res {
		if r.pinned || t.ws.a.d.Cfg.Editor.ResultMode == "append" {
			keep = append(keep, r)
		}
	}
	t.replace, t.sum = -1, nil
	if len(stmts) > summaryOver {
		t.sum = &batchSummary{total: len(stmts)}
	}
	t.running, t.queue, t.res, t.rc, t.status, t.firstErr = true, stmts, keep, max(0, len(keep)-1), "", -1
	t.override = 0
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
	ws, sess, batch, tab, limit, gen := t.ws, t.sess(), t.batch, t.id, t.limit, t.ws.gen
	if t.override > 0 {
		limit = t.override
	}
	return func() tea.Msg {
		start := time.Now()
		res, err := sess.Exec(ctx, s.Text, limit)
		return wsMsg{ws: ws.id, tab: tab, gen: gen, msg: queryDoneMsg{batch: batch, stmt: s, res: res, err: err, took: time.Since(start)}}
	}
}

// Close cancels a running statement when the tab goes away.
func (t *queryTab) Close() {
	t.closed = true
	if t.pin != nil {
		t.pin.Close()
		t.pin = nil
	}
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
	t.override, t.replace = 0, -1
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
	case explainMsg:
		return t.showExplain(m)
	case resultColsMsg:
		t.applyResultCols(m)
	case colsLoadedMsg:
		t.cols[m.name] = m.cols
		delete(t.loading, m.name)
	case queryDoneMsg:
		if m.batch != t.batch {
			return nil
		}
		t.ws.record(t.typed(m.stmt.Text), "editor", m.took, m.err)
		if m.res != nil {
			for _, l := range strings.Split(m.res.Message, "\n") {
				if noticeRe.MatchString(l) {
					t.ws.log("-- "+l, 0, nil) // RAISE NOTICE lines belong in the console too
				}
			}
		}
		if t.sum != nil && t.replace < 0 {
			return t.summaryStep(m)
		}
		rv := t.toResult(m)
		if t.replace >= 0 && t.replace < len(t.res) { // a committed result is refreshed in place
			rv.title, rv.pinned, rv.chart = t.res[t.replace].title, t.res[t.replace].pinned, t.res[t.replace].chart
			rv.limit, t.override = t.override, 0
			t.res[t.replace] = rv
			t.running, t.queue, t.rc, t.replace = false, nil, t.replace, -1
			t.focusResults = true
			return t.makeEditable(t.rc)
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
		t.status = tookStr(time.Since(t.started)) // the run bar adds the statement and failure counts
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
			rv.msg = plural(int(r.Affected), "row") + " affected"
		}
		return rv
	}
	cols := make([]gridCol, len(r.Columns))
	for i, c := range r.Columns {
		cols[i] = gridCol{Name: c.Name, Type: c.Type}
	}
	rv.hasRows, rv.rows, rv.raw = true, len(r.Rows), r
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
				return setClip(sel)
			}
			return nil
		case "ctrl+x":
			if sel := t.ed.Selection(); sel != "" {
				t.ed.push("cut")
				t.ed.deleteSelection()
				t.ed.Changed = true
				return tea.Batch(setClip(sel), t.afterEdit())
			}
			return nil
		case "ctrl+v":
			return tea.ReadClipboard
		case "esc":
			t.ed.anchor, t.ed.blk = nil, nil
			t.ed.vimEsc()
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
	t.rc = min(max(t.rc, 0), len(t.res)-1)
	rv := t.res[t.rc]
	if rv.grid != nil && rv.grid.Editing() { // paste into the open cell editor
		switch m := msg.(type) {
		case tea.PasteMsg:
			rv.grid.edit.Handle(m)
			return nil
		case tea.ClipboardMsg:
			rv.grid.edit.Handle(tea.PasteMsg{Content: m.Content})
			return nil
		case tea.KeyPressMsg:
			if m.Keystroke() == "ctrl+v" {
				return tea.ReadClipboard
			}
		}
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
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
	case "m":
		rv.msgView = !rv.msgView
		return nil
	case "s":
		if rv.msgView {
			return t.saveMessage(rv)
		}
	case "b":
		if rv.chart && rv.raw != nil {
			return t.ws.addToBoard(rv)
		}
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
		return t.findPrompt(rv)
	case "n":
		return t.findIn(rv, 1)
	case "N":
		return t.findIn(rv, -1)
	case "c":
		return t.copyOut(g.CopyCells(FmtPlain, "result"), "cells")
	case "y":
		return t.copyOut(g.CopyRows(FmtCSV, "result"), "rows")
	case "e":
		if g.Editable {
			g.StartEdit()
		}
	case "enter":
		if g.Editable && g.cur() != nil && g.cur().State != rowDeleted {
			return editCellInPopover(t.ws, g)
		}
		if v, ok := g.CursorValue(); ok {
			t.ws.a.push(t.ws.a.newQuickLookValue(g.Cols[g.col].Name, v, nil))
		}
	}
	return nil
}

func (t *queryTab) findPrompt(rv *resultView) tea.Cmd {
	if rv.grid == nil {
		return t.ws.a.say("nothing to search in this result")
	}
	var f *formDlg
	title := func() string {
		return "Find in this result · match case " + map[bool]string{true: "on", false: "off"}[rv.matchCase]
	}
	f = newForm(title(), []formField{{Key: "q", Label: "Text", Kind: fText, Value: rv.find}}, func(v map[string]string) tea.Cmd {
		rv.find = v["q"]
		return t.findIn(rv, 1)
	})
	f.hotHint = "Alt+c: match case"
	f.hotkeys = map[string]func(map[string]string) tea.Cmd{"alt+c": func(map[string]string) tea.Cmd { rv.matchCase = !rv.matchCase; f.title = title(); return nil }}
	t.ws.a.push(f)
	return nil
}

func (t *queryTab) findIn(rv *resultView, dir int) tea.Cmd {
	if rv.find == "" {
		return t.ws.a.say("press / to search this result")
	}
	if !rv.grid.FindCase(rv.find, dir, rv.matchCase) {
		return t.ws.a.say("%q is not in this result", rv.find)
	}
	return nil
}

func (t *queryTab) copyOut(text, what string) tea.Cmd {
	if text == "" {
		return nil
	}
	return tea.Batch(setClip(text), t.ws.a.say("copied %s", what))
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
	case "fold":
		t.ed.ToggleFold()
		return true, nil
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
	case "beautify", "uglify":
		return true, t.reformat(name == "beautify")
	case "find":
		if rv := t.curResult(); t.focusResults && rv != nil {
			return true, t.findPrompt(rv)
		}
		return true, t.findInEditor()
	case "find_replace":
		return true, t.replaceInEditor()
	case "find_next", "find_prev":
		return true, t.findStep(map[string]int{"find_next": 1, "find_prev": -1}[name])
	case "block_select":
		return true, t.ws.a.say("%s", t.ed.toggleBlock())
	case "split_down":
		return true, t.splitDown()
	case "next_pane", "prev_pane":
		if t.other == nil {
			return false, nil
		}
		t.swapPane()
		return true, nil
	case "variables":
		return true, t.editVars()
	case "chart", "load_more", "copy_rows", "copy_cells", "copy_rows_as":
		return true, t.resultOnly(name)
	case "select_next_occurrence", "select_all_occurrences", "vim_mode", "invisibles":
		return true, t.ws.a.say("%s", t.ed.extra(name))
	case "reload_tab":
		return true, nil
	case "wrap_lines":
		t.ed.Opts.Wrap = !t.ed.Opts.Wrap
		t.ed.left = 0
		return true, t.ws.a.say("line wrap %s", map[bool]string{true: "on", false: "off"}[t.ed.Opts.Wrap])
	case "explain":
		return true, t.explain()
	case "complete":
		if !t.ed.openComplete() {
			return true, t.ws.a.say("nothing to complete here")
		}
		return true, nil
	case "upper_case", "lower_case", "capitalize":
		if m := t.ed.transform(name); m != "" {
			return true, t.ws.a.say("%s", m)
		}
		return true, t.afterEdit()
	case "use_selection_find":
		q := t.ed.Selection()
		if q == "" {
			q = t.ed.wordAtCursor()
		}
		if q == "" {
			return true, t.ws.a.say("put the cursor on a word or select some text first")
		}
		t.edFind = q
		return true, t.ws.a.say("find text is now %q (the find action jumps to the next match)", q)
	case "jump_to_selection":
		t.ed.scroll()
		t.focusResults = false
		return true, nil
	case "cleanup_variables":
		n := len(t.ws.vars)
		t.ws.vars = nil
		return true, t.ws.a.say("forgot %s", plural(n, "remembered variable"))
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
		folder := strings.Trim(v["folder"], "/ ")
		return t.ws.a.ifNoQuery(folder, v["name"], func() tea.Cmd {
			q, err := t.ws.a.d.Queries.Save(folder, v["name"], kws, text)
			if err != nil {
				return t.ws.a.fail(err)
			}
			if text == t.ed.Text() { // the whole buffer was saved: keep auto-saving it
				t.savedAs, t.lastAuto = q.Path, text
			}
			return t.ws.a.say("saved %s", q.Name)
		})
	}))
	return nil
}

func (t *queryTab) saveAs() tea.Cmd {
	t.ws.a.push(newForm("Save SQL to file", []formField{{Key: "path", Label: "File", Kind: fText, Value: "query.sql"}}, func(v map[string]string) tea.Cmd {
		path := strings.TrimSpace(v["path"])
		if path == "" {
			return t.ws.a.fail(fmt.Errorf("file name required"))
		}
		text := t.ed.Text()
		return t.ws.a.ifFree(func() tea.Cmd {
			if err := writeFile(path, text); err != nil {
				return t.ws.a.fail(err)
			}
			return t.ws.a.say("saved %s", path)
		}, path)
	}))
	return nil
}

// ---- view -------------------------------------------------------------------------------

func (t *queryTab) View(th Theme, W, H int) string {
	showRes := !t.hideResults // an empty results pane says how to fill it
	edH := H - 1
	resH := 0
	if showRes {
		resH = max(4, (H-1)*45/100)
		edH = H - 1 - resH
	}
	if edH < 3 {
		edH, resH = max(1, H-1), 0
	}
	editorFocused := !t.ws.focusSB && !(t.focusResults && resH > 0)
	parts := []string{t.editorsView(th, W, edH, editorFocused), t.runBar(th, W)}
	if resH > 0 {
		parts = append(parts, t.resultsView(th, W, resH-1))
	}
	return strings.Join(parts, "\n")
}

func (t *queryTab) runBar(th Theme, W int) string {
	key := t.editorKey
	limit := "no limit"
	if t.limit > 0 {
		limit = fmt.Sprintf("limit %d", t.limit)
	}
	left := th.chips(key("run_current"), "run", key("run_all"), "run all", key("set_limit"), limit)
	if t.ws.a.d.Cfg.Editor.RunDefault == "all" { // [editor] run_default: the first chip is what the run button does
		left = th.chips(key("run_all"), "run", key("run_current"), "run current", key("set_limit"), limit)
	}
	status := ""
	switch {
	case t.running:
		status = th.Warn.Render("⏳ running "+tookStr(time.Since(t.started))) + "  " + th.chips(key("cancel_query"), "cancel")
	case t.status == "cancelled":
		status = th.Warn.Render("⚠ cancelled")
	case t.status != "":
		failed, count := 0, len(t.res)
		for _, r := range t.res {
			if r.isErr {
				failed++
			}
		}
		if t.sum != nil { // a summarised run: the counts are the statements', not the tabs'
			failed, count = t.sum.fail, t.sum.done
		}
		switch {
		case failed == 0:
			status = th.OK.Render("✓ ") + th.Dim.Render(plural(count, "statement")+" · "+t.status)
		case count == 1:
			status = th.Err.Render("✗ failed") + th.Dim.Render(" · "+t.status)
		default:
			status = th.Err.Render(fmt.Sprintf("✗ %d of %d failed", failed, count)) + th.Dim.Render(" · "+t.status)
		}
	}
	// a hairline between the keys and the status separates the editor from the results
	if status != "" {
		status = " " + status
	}
	gap := W - width(left) - width(status) - 2
	if gap < 1 {
		return fit(left, W)
	}
	return left + " " + th.Border.Render(strings.Repeat("─", gap)) + status + " "
}

// editorKey is the prefixed key bound to an editor action, as shown in hints ("C-d ↵").
func (t *queryTab) editorKey(action string) string {
	for _, b := range t.ws.a.d.Keys.Bindings(keymap.Editor) {
		if b.Action == action {
			return strings.Replace(keymap.Display(t.ws.a.d.Keys.Prefix)+" "+keymap.Display(b.Key), "Enter", "↵", 1)
		}
	}
	return "?"
}

// tookStr rounds a duration to what a person reads: 455µs, 1.5ms, 1.23s.
func tookStr(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return d.Round(time.Microsecond).String()
	case d < time.Second:
		return d.Round(100 * time.Microsecond).String()
	}
	return d.Round(10 * time.Millisecond).String()
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
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
		msg := th.Dim.Render("run a statement with ") + th.chip(t.editorKey("run_current"))
		if t.running {
			msg = th.Warn.Render("⏳ ") + th.Dim.Render("running…")
		}
		return emptyState(msg, W, H+1)
	}
	var tabs []string
	for i, r := range t.res {
		st := th.TabIdle
		switch {
		case i == t.rc && t.focusResults:
			st = th.TabActive
		case i == t.rc:
			st = th.TabActive.Faint(true)
		}
		label := st.Render(" " + r.title + " ")
		if r.isErr { // the mark keeps the tab's background; on an idle tab it also takes the error color
			mark := st.Render(" ✗")
			if i != t.rc {
				mark = th.Err.Inherit(st).Render(" ✗")
			}
			label = mark + label
		}
		if r.pinned {
			mark := st.Render(" ◆")
			if i != t.rc {
				mark = th.Warn.Inherit(st).Render(" ◆")
			}
			label = mark + label
		}
		tabs = append(tabs, label)
	}
	t.rc = min(max(t.rc, 0), len(t.res)-1)
	rv := t.res[t.rc]
	info := th.Dim.Render(tookStr(rv.took))
	if rv.hasRows {
		info = th.Dim.Render(plural(rv.rows, "row") + " · " + tookStr(rv.took))
		if rv.trunc {
			info = th.Warn.Render("⚠ limit reached") + th.Dim.Render(" · ") + info
		}
	}
	bar := fit(strings.Join(tabs, " "), W-width(info)-2) + " " + info + " "
	lines := []string{fit(bar, W)}
	switch {
	case rv.isErr:
		lines = append(lines, "")
		lines = append(lines, errorLines(th, rv.msg, W)...)
	case rv.msgView:
		lines = append(lines, "")
		for _, l := range wrapText(orDefault(rv.msg, "(no messages)"), W-6) {
			lines = append(lines, "  "+l)
		}
		lines = append(lines, "", "  "+th.Dim.Render("m: back to the rows   s: save as text"))
	case rv.hasRows && rv.chart:
		lines = append(lines, chartLines(th, rv, W, H)...)
	case rv.hasRows:
		rv.grid.W, rv.grid.H = W, H
		lines = append(lines, rv.grid.View(th, t.focusResults))
		if rv.msg != "" && H > 3 {
			first, rest, more := strings.Cut(rv.msg, "\n")
			if more {
				first += fmt.Sprintf(" (+%d more lines: m)", strings.Count(rest, "\n")+1)
			}
			lines = append(lines, th.Dim.Render(fit(" "+first, W)))
		}
	default:
		lines = append(lines, "")
		for i, l := range wrapText(rv.msg, W-6) {
			if i == 0 {
				lines = append(lines, "  "+th.OK.Render("✓ ")+l+th.Dim.Render(" · "+tookStr(rv.took)))
			} else {
				lines = append(lines, "    "+l)
			}
		}
	}
	out := strings.Split(strings.Join(lines, "\n"), "\n")
	for len(out) < H+1 {
		out = append(out, strings.Repeat(" ", W))
	}
	return strings.Join(out[:H+1], "\n")
}

// emptyState centers a one-line message a little above the middle of a W x H area.
func emptyState(msg string, W, H int) string {
	return lipgloss.Place(W, H, lipgloss.Center, 0.4, msg)
}

var (
	sqlStateRe = regexp.MustCompile(`\s*\((SQLSTATE \w+)\)\s*$`)
	errHintRe  = regexp.MustCompile(`(?i)^\s*(line \d|hint|detail|position|where|context|\^)`)
)

// errorLines renders a failed statement: the first line bold in the error color, the rest dim,
// position / hint lines indented further, and a trailing "(SQLSTATE x)" moved to its own dim line.
func errorLines(th Theme, msg string, W int) []string {
	head, rest, _ := strings.Cut(strings.TrimSpace(msg), "\n")
	code := ""
	if m := sqlStateRe.FindStringSubmatchIndex(head); m != nil {
		code, head = head[m[2]:m[3]], head[:m[0]]
	}
	bold := th.Err.Bold(true)
	var out []string
	for i, l := range wrapText(head, W-6) {
		lead := "    "
		if i == 0 {
			lead = "  " + bold.Render("✗") + " "
		}
		out = append(out, lead+bold.Render(l))
	}
	if rest != "" {
		for _, l := range strings.Split(rest, "\n") {
			ind := "    "
			if errHintRe.MatchString(l) {
				ind = "      "
			}
			for _, w := range wrapText(strings.TrimRight(l, " \t\r"), W-len(ind)-2) {
				out = append(out, ind+th.Dim.Render(w))
			}
		}
	}
	if code != "" {
		out = append(out, "    "+th.Dim.Render(code))
	}
	return out
}

var _ = sort.Strings
