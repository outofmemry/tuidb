package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"tuidb/internal/config"
	"tuidb/internal/db"
	"tuidb/internal/keymap"
	"tuidb/internal/safemode"
	"tuidb/internal/store"
)

// tabber is one tab in a workspace: a table (data/structure/index/ddl) or a SQL editor.
type tabber interface {
	ID() int
	Title() string
	Dirty() bool
	Context() string // keymap context of the focused part
	Init() tea.Cmd
	Input(msg tea.Msg) tea.Cmd  // plain keys and paste while focused
	Handle(msg tea.Msg) tea.Cmd // async results addressed to this tab
	Action(name string) (bool, tea.Cmd)
	View(th Theme, w, h int) string
}

// detailer is implemented by tabs that can show the row-detail pane.
type detailer interface{ Detail() []string }

type consoleLine struct {
	at  time.Time
	sql string
	err bool
	dur time.Duration
}

type Workspace struct {
	a       *App
	id      int
	conn    config.Connection
	o       *Opened
	sess    db.Session
	caps    db.Capabilities
	level   safemode.Level
	sb      *sidebar
	tabs    []tabber
	cur     int
	nextTab int

	focusSB     bool
	showSB      bool
	showDetail  bool
	showConsole bool
	console     []consoleLine
	W, H        int
	closed      bool
	applying    bool // statements are being sent: edits and a second commit are refused meanwhile
	// meta caches column metadata per object, so reopening a table or its structure skips that
	// round trip. Only touched on the UI loop; cleared by reload, reconnect and schema changes.
	meta map[db.ObjRef][]db.Column
	lost bool // the last keep-alive ping failed
}

type pingMsg struct{ err error }

// ping checks the connection in the background; a failure is shown once, success clears it.
func (w *Workspace) ping() tea.Cmd {
	return w.async(-1, func(ctx context.Context, sess db.Session) tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return pingMsg{err: sess.Ping(ctx)}
	})
}

func newWorkspace(a *App, id int, c config.Connection, o *Opened) *Workspace {
	lvl, _ := safemode.Parse(c.SafeMode)
	w := &Workspace{a: a, id: id, conn: c, o: o, sess: o.Sess, caps: o.Sess.Caps(), level: lvl, showSB: true, focusSB: true, meta: map[db.ObjRef][]db.Column{}}
	w.sb = newSidebar(w)
	return w
}

func (w *Workspace) init() tea.Cmd { return tea.Batch(w.sb.load(), w.warmUp()) }

// warmUp opens a few pooled connections in the background right after connecting, so the first
// parallel loads (rows, count, columns) do not each pay a fresh connection handshake.
func (w *Workspace) warmUp() tea.Cmd {
	sess := w.sess
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var wg sync.WaitGroup
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); sess.Ping(ctx) }()
		}
		wg.Wait()
		return nil
	}
}

// colsMetaMsg carries column metadata (with foreign keys merged in) for ref.
type colsMetaMsg struct {
	ref  db.ObjRef
	cols []db.Column
	err  error
	seq  int // the table tab's load sequence that asked for it (0 for prefetches)
}

// fetchColumns loads columns and foreign keys concurrently and merges them.
func fetchColumns(ctx context.Context, sess db.Session, caps db.Capabilities, ref db.ObjRef) ([]db.Column, error) {
	var (
		cols []db.Column
		fks  []db.ForeignKey
		err  error
		wg   sync.WaitGroup
	)
	if caps.ForeignKeys {
		wg.Add(1)
		go func() { defer wg.Done(); fks, _ = sess.ForeignKeys(ctx, ref) }()
	}
	cols, err = sess.Columns(ctx, ref)
	wg.Wait()
	if err != nil {
		return nil, err
	}
	return mergeFKs(cols, fks), nil
}

// mergeFKs fills Column.FK where the driver did not.
func mergeFKs(cols []db.Column, fks []db.ForeignKey) []db.Column {
	for i := range cols {
		for j := range fks {
			for _, c := range fks[j].Columns {
				if c == cols[i].Name && cols[i].FK == nil {
					cols[i].FK = &fks[j]
				}
			}
		}
	}
	return cols
}

// forget drops cached metadata: one object, or everything when ref is nil.
func (w *Workspace) forget(ref *db.ObjRef) {
	if ref == nil {
		w.meta = map[db.ObjRef][]db.Column{}
		return
	}
	delete(w.meta, *ref)
}

func (w *Workspace) close() {
	if !w.closed {
		w.closed = true
		for i := len(w.tabs) - 1; i >= 0; i-- {
			w.closeTab(i) // cancels running statements
		}
		w.o.Close()
	}
}

func (w *Workspace) resize(W, H int) { w.W, w.H = W, H }

func (w *Workspace) tab() tabber {
	if w.cur >= 0 && w.cur < len(w.tabs) {
		return w.tabs[w.cur]
	}
	return nil
}

func (w *Workspace) dirty() bool {
	for _, t := range w.tabs {
		if t.Dirty() {
			return true
		}
	}
	return false
}

func (w *Workspace) context() string {
	if w.focusSB || w.tab() == nil {
		return keymap.Sidebar
	}
	return w.tab().Context()
}

// ---- async plumbing ---------------------------------------------------------------------

// async runs f off the UI loop and routes its message back to this workspace (and tab, if tab>=0).
// The session is captured here, on the UI loop: a reconnect swaps w.sess, and a closure that read
// it later from its own goroutine would race with that.
func (w *Workspace) async(tab int, f func(ctx context.Context, sess db.Session) tea.Msg) tea.Cmd {
	id, sess := w.id, w.sess
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return wsMsg{ws: id, tab: tab, msg: f(ctx, sess)}
	}
}

func (w *Workspace) handle(m wsMsg) tea.Cmd {
	if cm, ok := m.msg.(colsMetaMsg); ok && cm.err == nil && cm.cols != nil {
		w.meta[cm.ref] = cm.cols
	}
	if m.tab < 0 {
		return w.sb.handle(m.msg)
	}
	for _, t := range w.tabs {
		if t.ID() == m.tab {
			return t.Handle(m.msg)
		}
	}
	return nil // tab closed meanwhile
}

func (w *Workspace) log(sql string, dur time.Duration, err error) {
	w.console = append(w.console, consoleLine{at: time.Now(), sql: sql, err: err != nil, dur: dur})
	if len(w.console) > 1000 {
		w.console = w.console[len(w.console)-1000:]
	}
}

// record writes an executed statement to the console and, when it changed something or came from
// the user, to the per-connection history.
func (w *Workspace) record(sql, source string, dur time.Duration, err error) {
	w.log(sql, dur, err)
	if err == nil && strings.TrimSpace(sql) != "" {
		w.a.d.Hist.Append(w.conn.Name, store.HistoryEntry{Database: w.sess.CurrentDatabase(), SQL: sql, Duration: dur, Source: source})
		w.sb.hist = nil // reload lazily
	}
}

// ---- safe mode --------------------------------------------------------------------------

// gate asks for confirmation / password according to the safe-mode level, then runs run.
func (w *Workspace) gate(stmts []db.Statement, run func() tea.Cmd) tea.Cmd {
	readOnly := true
	var lines []string
	for _, s := range stmts {
		if !s.ReadOnly && !db.IsReadOnly(w.caps.Dialect, s.Text) {
			readOnly = false
		}
		lines = append(lines, s.Shown())
	}
	confirm, password := w.level.Gate(readOnly)
	switch {
	case password:
		pw := w.o.Password
		f := newForm(w.level.Label()+": enter the database password", []formField{
			{Key: "pw", Label: "Password", Kind: fSecret},
			{Key: "sql", Label: "", Kind: fLabel, Value: truncLines(lines, 3)},
		}, func(map[string]string) tea.Cmd { return run() })
		f.validate = func(v map[string]string) error {
			if v["pw"] != pw {
				return fmt.Errorf("wrong password")
			}
			return nil
		}
		f.submit = "Enter: send"
		w.a.push(f)
		return nil
	case confirm:
		w.a.push(&confirmDlg{title: w.level.Label() + ": send to the server?", lines: lines, yes: "Send", onYes: run})
		return nil
	}
	return run()
}

func truncLines(l []string, n int) string {
	if len(l) <= n {
		return strings.Join(l, " | ")
	}
	return strings.Join(l[:n], " | ") + fmt.Sprintf(" … +%d more", len(l)-n)
}

// execute runs stmts through Apply, logging each; used by commit and admin operations.
func (w *Workspace) execute(stmts []db.Statement, then func(err error) tea.Cmd) tea.Cmd {
	if w.applying {
		return w.a.say("a change is still being sent; wait for it to finish")
	}
	return w.gate(stmts, func() tea.Cmd {
		w.applying = true
		return w.async(-2, func(ctx context.Context, sess db.Session) tea.Msg {
			start := time.Now()
			err := sess.Apply(ctx, stmts)
			return applyDoneMsg{stmts: stmts, err: err, dur: time.Since(start), then: then}
		})
	})
}

type applyDoneMsg struct {
	stmts []db.Statement
	err   error
	dur   time.Duration
	then  func(err error) tea.Cmd
}

// ---- tabs -------------------------------------------------------------------------------

func (w *Workspace) addTab(t tabber) tea.Cmd {
	w.tabs = append(w.tabs, t)
	w.cur = len(w.tabs) - 1
	w.focusSB = false
	return t.Init()
}

func (w *Workspace) newID() int { w.nextTab++; return w.nextTab }

func (w *Workspace) closeTab(i int) {
	if i < 0 || i >= len(w.tabs) {
		return
	}
	if c, ok := w.tabs[i].(interface{ Close() }); ok {
		c.Close()
	}
	w.tabs = append(w.tabs[:i], w.tabs[i+1:]...)
	w.cur = clamp(w.cur, 0, len(w.tabs)-1)
	if len(w.tabs) == 0 {
		w.focusSB = true
	}
}

func (w *Workspace) selectTab(i int) {
	if len(w.tabs) > 0 {
		w.cur = (i%len(w.tabs) + len(w.tabs)) % len(w.tabs)
		w.focusSB = false
	}
}

// openObject opens a table/view in a tab following TablePlus's reuse rule: a clean table tab
// is replaced unless newTab is set.
func (w *Workspace) openObject(ref db.ObjRef, newTab bool) tea.Cmd {
	switch ref.Kind {
	case db.KindTable, db.KindView, db.KindMatView:
	case db.KindFunction, db.KindProcedure:
		return w.editDefinition(ref)
	default:
		return w.showDDL(ref)
	}
	key := ref.Schema + "." + ref.Name
	w.a.d.State.Touch(w.conn.Name, key)
	for _, t := range w.tabs { // already open: just switch
		if tt, ok := t.(*tableTab); ok && tt.ref == ref && !newTab {
			w.selectTab(indexOf(w.tabs, t))
			return nil
		}
	}
	nt := newTableTab(w, w.newID(), ref)
	if c, ok := w.tab().(*tableTab); ok && !newTab && !c.Dirty() {
		w.tabs[w.cur] = nt
		w.focusSB = false
		return nt.Init()
	}
	return w.addTab(nt)
}

func indexOf(ts []tabber, t tabber) int {
	for i := range ts {
		if ts[i] == t {
			return i
		}
	}
	return 0
}

func (w *Workspace) showDDL(ref db.ObjRef) tea.Cmd {
	return w.async(-2, func(ctx context.Context, sess db.Session) tea.Msg {
		d, err := sess.DDL(ctx, ref)
		return ddlShownMsg{ref: ref, ddl: d, err: err}
	})
}

type ddlShownMsg struct {
	ref db.ObjRef
	ddl string
	err error
}

func (w *Workspace) newQueryTab(text string) tea.Cmd {
	t := newQueryTab(w, w.newID())
	if text != "" {
		t.ed.SetText(text)
	}
	return w.addTab(t)
}

// ---- messages without a tab -------------------------------------------------------------

func (w *Workspace) handleGlobal(msg tea.Msg) (tea.Cmd, bool) {
	if cmd, ok := w.handleDefinitions(msg); ok {
		return cmd, true
	}
	switch m := msg.(type) {
	case applyDoneMsg:
		w.applying = false
		for _, s := range m.stmts {
			w.record(s.Shown(), "gui", m.dur/time.Duration(max(1, len(m.stmts))), m.err)
		}
		if m.err != nil {
			w.a.errorDialog("Statement failed (nothing was committed)", m.err)
		}
		if m.then != nil {
			return m.then(m.err), true
		}
		return nil, true
	case pingMsg:
		switch {
		case m.err != nil && !w.lost:
			w.lost = true
			return w.a.fail(fmt.Errorf("lost connection to %s (%v): press %s R to reconnect", w.conn.Name, m.err, keymap.Display(w.a.d.Keys.Prefix))), true
		case m.err == nil && w.lost:
			w.lost = false
			return w.a.say("%s is reachable again", w.conn.Name), true
		}
		return nil, true
	case scriptMsg:
		if m.err != nil {
			return w.a.fail(m.err), true
		}
		return tea.Batch(tea.SetClipboard(m.text), w.a.say("script copied to the clipboard")), true
	case ddlShownMsg:
		if m.err != nil {
			return w.a.fail(m.err), true
		}
		w.a.push(&textViewDlg{title: m.ref.Name, lines: strings.Split(m.ddl, "\n"), actions: []viewAction{{Key: "c", Label: "c: copy", Do: func() tea.Cmd { return tea.SetClipboard(m.ddl) }}}})
		return nil, true
	}
	return nil, false
}

// ---- input & actions --------------------------------------------------------------------

func (w *Workspace) input(msg tea.Msg) tea.Cmd {
	if w.focusSB || w.tab() == nil {
		return w.sb.input(msg)
	}
	if w.applying {
		return nil
	}
	return w.tab().Input(msg)
}

// mutating actions are refused while statements are in flight, so edits typed in that window
// cannot be wiped by the reload that follows the commit.
var mutating = map[string]bool{"commit": true, "discard": true, "undo": true, "redo": true, "add_row": true, "duplicate_rows": true,
	"delete_rows": true, "set_value": true, "paste_rows": true, "paste_cells": true, "reload_tab": true, "delete_item": true, "truncate": true}

func (w *Workspace) action(name string) tea.Cmd {
	if w.applying && mutating[name] {
		return w.a.say("a change is still being sent; wait for it to finish")
	}
	// tab-level first: it knows its own focus (grid vs editor vs structure)
	if t := w.tab(); t != nil && !w.focusSB {
		if ok, cmd := t.Action(name); ok {
			return cmd
		}
	}
	if w.focusSB {
		if ok, cmd := w.sb.action(name); ok {
			return cmd
		}
	}
	switch name {
	case "new_query_tab":
		return w.newQueryTab("")
	case "new_tab":
		if w.a.d.Cfg.General.NewTabOpens == "empty" {
			w.focusSB = true
			return nil
		}
		return w.newQueryTab("")
	case "close_tab":
		if t := w.tab(); t != nil && t.Dirty() {
			i := w.cur
			w.a.push(&confirmDlg{title: "Close " + t.Title() + " with unsaved changes?", lines: []string{"Pending edits in this tab will be lost."}, yes: "Close", danger: true,
				onYes: func() tea.Cmd { w.closeTab(i); return nil }})
			return nil
		}
		w.closeTab(w.cur)
	case "next_tab":
		w.selectTab(w.cur + 1)
	case "prev_tab":
		w.selectTab(w.cur - 1)
	case "toggle_sidebar":
		w.showSB = !w.showSB
		if !w.showSB {
			w.focusSB = false
		}
	case "toggle_row_detail":
		w.showDetail = !w.showDetail
	case "toggle_console":
		w.showConsole = !w.showConsole
	case "focus_left":
		if w.showSB {
			w.focusSB = true
		}
	case "focus_right":
		if w.tab() != nil {
			w.focusSB = false
		}
	case "commit":
		return w.commit()
	case "preview":
		return w.preview()
	case "discard":
		return w.discard()
	case "undo", "redo", "reload_tab":
		if t, ok := w.tab().(interface{ Do(string) tea.Cmd }); ok {
			return t.Do(name)
		}
	case "triggers":
		if t, ok := w.tab().(*tableTab); ok {
			return w.triggerMenu(t.ref)
		}
		return w.a.say("open a table first: triggers belong to a table")
	case "reload":
		w.forget(nil)
		return tea.Batch(w.sb.load(), w.reloadTab())
	case "reconnect":
		return w.reconnect()
	case "disconnect":
		return w.disconnect()
	case "safe_mode":
		return w.pickSafeMode()
	case "open_anything":
		return w.openAnything()
	case "open_database":
		return w.pickDatabase()
	case "search_in_database", "process_list", "export", "import":
		return w.a.say("%s arrives in milestone 2", name)
	case "users", "diagram", "metrics", "backup", "restore", "split_right", "next_pane", "prev_pane":
		return w.a.say("%s arrives in milestone 3", name)
	default:
		if strings.HasPrefix(name, "tab_") {
			var n int
			fmt.Sscanf(name, "tab_%d", &n)
			if n >= 1 && n <= len(w.tabs) {
				w.selectTab(n - 1)
			}
			return nil
		}
		return w.a.say("%q is not available here", name)
	}
	return nil
}

func (w *Workspace) reloadTab() tea.Cmd {
	if t, ok := w.tab().(interface{ Do(string) tea.Cmd }); ok {
		return t.Do("reload_tab")
	}
	return nil
}

func (w *Workspace) commit() tea.Cmd {
	if t, ok := w.tab().(interface{ Commit() tea.Cmd }); ok {
		return t.Commit()
	}
	return w.a.say("nothing to commit here")
}

func (w *Workspace) preview() tea.Cmd {
	t, ok := w.tab().(interface {
		Statements() ([]db.Statement, error)
	})
	if !ok {
		return w.a.say("nothing to preview here")
	}
	stmts, err := t.Statements()
	if err != nil {
		return w.a.fail(err)
	}
	if len(stmts) == 0 {
		return w.a.say("no pending changes")
	}
	var lines []string
	for _, s := range stmts {
		lines = append(lines, s.Shown()+";", "")
	}
	w.a.push(&textViewDlg{title: fmt.Sprintf("Preview: %d statement(s)", len(stmts)), lines: lines,
		actions: []viewAction{{Key: "enter", Label: "Enter: commit", Do: w.commit}}})
	return nil
}

func (w *Workspace) discard() tea.Cmd {
	if t, ok := w.tab().(interface{ Discard() }); ok {
		t.Discard()
		return w.a.say("changes discarded")
	}
	return nil
}

func (w *Workspace) reconnect() tea.Cmd {
	c := w.conn
	c.Database = w.sess.CurrentDatabase() // come back to the database that is open now, not the configured one
	idx := w.a.active
	w.a.push(&confirmDlg{title: "Reconnect " + c.Name + "?", lines: []string{"The connection is reopened; tabs stay, unsent edits are kept."}, yes: "Reconnect", onYes: func() tea.Cmd {
		connect := w.a.d.Connect
		return func() tea.Msg {
			o, err := connect(context.Background(), c)
			return reconnectedMsg{ws: w.id, idx: idx, o: o, err: err}
		}
	}})
	return nil
}

type reconnectedMsg struct {
	ws, idx int
	o       *Opened
	err     error
}

func (w *Workspace) disconnect() tea.Cmd {
	w.a.push(&confirmDlg{title: "Disconnect " + w.conn.Name + "?", lines: []string{"All of its tabs are closed."}, yes: "Disconnect", danger: w.dirty(), onYes: func() tea.Cmd {
		w.close()
		for i, x := range w.a.wss {
			if x == w {
				w.a.wss = append(w.a.wss[:i], w.a.wss[i+1:]...)
				break
			}
		}
		w.a.active = clamp(w.a.active, 0, len(w.a.wss)-1)
		w.a.inWS = len(w.a.wss) > 0
		return nil
	}})
	return nil
}

func (w *Workspace) pickSafeMode() tea.Cmd {
	var items []pickItem
	desc := map[safemode.Level]string{
		safemode.Silent: "send queries with no warning", safemode.Alert1: "confirm every query", safemode.Alert2: "confirm all but SELECT/EXPLAIN/SHOW",
		safemode.Safe1: "password for every query", safemode.Safe2: "password for all but SELECT/EXPLAIN/SHOW",
	}
	for _, l := range safemode.All {
		mark := "  "
		if l == w.level {
			mark = "• "
		}
		items = append(items, pickItem{Label: mark + l.Label(), Detail: desc[l], Value: l})
	}
	apply := func(l safemode.Level) tea.Cmd {
		w.level = l
		// the level lives in the connection (as in TablePlus), so config and toolbar cannot disagree
		for i := range w.a.d.Conns {
			if w.a.d.Conns[i].Name == w.conn.Name {
				w.a.d.Conns[i].SafeMode = string(l)
				w.conn.SafeMode = string(l)
				if err := w.a.saveConns(); err != nil {
					return w.a.fail(err)
				}
			}
		}
		return w.a.say("safe mode: %s", l.Label())
	}
	w.a.push(&pickerDlg{title: "Safe mode for " + w.conn.Name, items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		l := it.Value.(safemode.Level)
		// leaving a password-protected level needs the password, like TablePlus
		if w.level == safemode.Safe1 || w.level == safemode.Safe2 {
			f := newForm("Enter the database password to change safe mode", []formField{{Key: "pw", Label: "Password", Kind: fSecret}},
				func(map[string]string) tea.Cmd { return apply(l) })
			f.validate = func(v map[string]string) error {
				if v["pw"] != w.o.Password {
					return fmt.Errorf("wrong password")
				}
				return nil
			}
			w.a.push(f)
			return nil
		}
		return apply(l)
	}})
	return nil
}

func (w *Workspace) pickDatabase() tea.Cmd {
	return w.async(-1, func(ctx context.Context, sess db.Session) tea.Msg {
		dbs, err := sess.Databases(ctx)
		return databasesMsg{dbs: dbs, err: err}
	})
}

type databasesMsg struct {
	dbs []string
	err error
}

func (w *Workspace) openDatabase(name string) tea.Cmd {
	if name != w.sess.CurrentDatabase() && w.dirty() {
		w.a.push(&confirmDlg{title: "Switching database closes all tabs", lines: []string{"There are unsaved changes in open tabs; they will be lost."}, yes: "Switch", danger: true,
			onYes: func() tea.Cmd { return w.switchDatabase(name) }})
		return nil
	}
	return w.switchDatabase(name)
}

func (w *Workspace) switchDatabase(name string) tea.Cmd {
	return w.async(-1, func(ctx context.Context, sess db.Session) tea.Msg {
		return dbSwitchedMsg{name: name, err: sess.UseDatabase(ctx, name)}
	})
}

type dbSwitchedMsg struct {
	name string
	err  error
}

// ---- Open Anything ----------------------------------------------------------------------

func (w *Workspace) openAnything() tea.Cmd {
	type target struct {
		kind string
		ref  db.ObjRef
		name string
		q    store.Query
	}
	var all []pickItem
	for _, o := range w.sb.objs {
		all = append(all, pickItem{Label: o.Name, Detail: string(o.Kind) + schemaSuffix(o.Schema), Value: target{kind: "object", ref: o.ObjRef}})
	}
	qs, _ := w.a.d.Queries.List()
	for _, q := range qs {
		all = append(all, pickItem{Label: q.Name, Detail: "saved query", Value: target{kind: "query", q: q}})
	}
	for _, name := range w.sb.dbs {
		all = append(all, pickItem{Label: name, Detail: "database", Value: target{kind: "database", name: name}})
	}
	for _, f := range []struct{ label, act string }{
		{"New SQL editor", "new_query_tab"}, {"Open connection", "open_connection"}, {"Open database", "open_database"},
		{"Safe mode", "safe_mode"}, {"Key bindings", "help"}, {"Reload", "reload"}, {"Process list", "process_list"}, {"Disconnect", "disconnect"},
	} {
		all = append(all, pickItem{Label: f.label, Detail: "feature", Value: target{kind: "action", name: f.act}})
	}
	p := &pickerDlg{title: "Open anything  (^name anchors the start, name$ the end)", filter: true, items: all}
	p.dynamic = func(q string) []pickItem {
		type sc struct {
			it pickItem
			s  int
		}
		var hits []sc
		for _, it := range all {
			if s, ok := matchName(q, it.Label, true); ok {
				hits = append(hits, sc{it, s})
			}
		}
		for i := 1; i < len(hits); i++ {
			for j := i; j > 0 && hits[j].s > hits[j-1].s; j-- {
				hits[j], hits[j-1] = hits[j-1], hits[j]
			}
		}
		var out []pickItem
		for _, h := range hits {
			out = append(out, h.it)
		}
		return out
	}
	p.items = p.dynamic("")
	p.onPick = func(it pickItem, ctrl bool) tea.Cmd {
		t := it.Value.(target)
		switch t.kind {
		case "object":
			return w.openObject(t.ref, ctrl)
		case "query":
			return w.newQueryTab(t.q.SQL)
		case "database":
			return w.openDatabase(t.name)
		}
		return w.a.run(t.name)
	}
	p.hint = "Enter: open   Ctrl/Alt+Enter: new tab   Esc: close"
	w.a.push(p)
	return nil
}

func schemaSuffix(s string) string {
	if s == "" {
		return ""
	}
	return "  " + s
}

// ---- view -------------------------------------------------------------------------------

func (w *Workspace) statusParts(th Theme) []string {
	c := w.conn
	parts := []string{th.StatusKey.Render(" " + c.Name + " ")}
	if c.Tag != "" || c.Color != "" {
		parts = append(parts, th.TagStyle(c.Tag, c.Color).Render(" "+strings.ToUpper(orDefault(c.Tag, "●"))+" "))
	}
	where := w.sess.CurrentDatabase()
	if w.caps.Schemas && w.sb.schema != "" {
		where += "." + w.sb.schema
	}
	parts = append(parts, th.StatusBar.Render(th.DriverStyle(c.Driver).Background(lipgloss.Color(th.pal["status_bg"])).Render(c.Driver)+" "+where))
	bg := lipgloss.Color(th.pal["status_bg"])
	if w.level == safemode.Silent {
		parts = append(parts, th.StatusBar.Foreground(lipgloss.Color(th.pal["dim"])).Render("safe: off"))
	} else {
		parts = append(parts, th.Warn.Background(bg).Bold(true).Render("⚠ "+w.level.Label()))
	}
	if w.dirty() {
		parts = append(parts, th.Warn.Background(bg).Bold(true).Render("● unsaved"))
	}
	if w.applying {
		parts = append(parts, th.Accent.Background(bg).Render("sending…"))
	}
	if len(w.a.wss) > 1 {
		parts = append(parts, th.StatusBar.Render(fmt.Sprintf("workspace %d/%d", w.a.active+1, len(w.a.wss))))
	}
	return parts
}

func (w *Workspace) view(th Theme, W, H int) string {
	sbW, detW, conH := 0, 0, 0
	if w.showSB {
		sbW = clamp(W/4, 24, 40)
	}
	if w.showDetail {
		detW = clamp(W/4, 30, 46)
	}
	if w.showConsole {
		conH = clamp(H/4, 5, 12)
	}
	mainH := H - conH
	mainW := W - sbW - detW
	if mainW < 20 {
		mainW, sbW, detW = W, 0, 0
	}
	var cols []string
	if sbW > 0 {
		cols = append(cols, frame(th, w.sb.title(), w.sb.hint(th), w.sb.view(th, sbW-2, mainH-2, w.focusSB), sbW, mainH, w.focusSB))
	}
	mainTitle := w.conn.Name
	var mainHint string
	if t := w.tab(); t != nil {
		mainTitle = t.Title()
		if h, ok := t.(interface{ Hint(Theme) string }); ok {
			mainHint = h.Hint(th)
		}
	}
	cols = append(cols, frame(th, mainTitle, mainHint, w.mainView(th, mainW-2, mainH-2), mainW, mainH, !w.focusSB))
	if detW > 0 {
		cols = append(cols, frame(th, "Row details", "", w.detailView(th, detW-2, mainH-2), detW, mainH, false))
	}
	out := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	if conH > 0 {
		out += "\n" + frame(th, fmt.Sprintf("Console · %d statements", len(w.console)), th.chips(keymap.Display(w.a.d.Keys.Prefix)+" C-c", "hide"), w.consoleView(th, W-2, conH-2), W, conH, false)
	}
	return out
}

func borderCol(th Theme, h int) string {
	return strings.TrimRight(strings.Repeat(th.Border.Render("│")+"\n", h), "\n")
}

func (w *Workspace) mainView(th Theme, W, H int) string {
	var lines []string
	// tab bar: "1 ▦ customers ●" per tab; the active one is highlighted, dimmed while the sidebar has focus
	var bar strings.Builder
	for i, t := range w.tabs {
		icon := "▦"
		if _, ok := t.(*queryTab); ok {
			icon = "≡"
		}
		title := t.Title()
		if t.Dirty() {
			title += " ●"
		}
		label := fmt.Sprintf(" %d %s %s ", i+1, icon, fit(title, min(width(title), 24)))
		switch {
		case i == w.cur && !w.focusSB:
			bar.WriteString(th.TabActive.Render(label))
		case i == w.cur:
			bar.WriteString(th.TabActive.Faint(true).Render(label))
		default:
			bar.WriteString(th.TabIdle.Render(label))
		}
		bar.WriteString(" ")
	}
	if len(w.tabs) > 0 {
		lines = append(lines, th.TabIdle.Faint(true).Render(fit(bar.String(), W)))
	}
	var body string
	bodyH := H - len(lines)
	if t := w.tab(); t != nil {
		body = t.View(th, W, bodyH)
	} else {
		body = w.emptyView(th, W, bodyH)
	}
	lines = append(lines, padLines(strings.Split(body, "\n"), W, bodyH)...)
	return strings.Join(lines, "\n")
}

// emptyView is shown in a workspace with no tabs: where you are and what to do next.
func (w *Workspace) emptyView(th Theme, W, H int) string {
	pre := keymap.Display(w.a.d.Keys.Prefix)
	k := func(action string) string { return pre + " " + keymap.Display(firstKey(w.a.d.Keys, action)) }
	var lines []string
	lines = append(lines, "",
		"   "+th.Title.Render(w.conn.Name)+"  "+th.DriverStyle(w.conn.Driver).Render(w.conn.Driver)+th.Dim.Render("  "+w.sess.ServerInfo()),
		"   "+th.Dim.Render(w.sess.CurrentDatabase()+"  ·  "+fmt.Sprintf("%d objects", len(w.sb.objs))),
		"",
		"   "+th.Section.Render("Get started"),
		"   "+th.Hints("↵", "open the highlighted table in the sidebar"),
		"   "+th.Hints("/", "find a table by name  (in the sidebar)"),
		"   "+th.Hints(k("new_query_tab"), "open a SQL editor"),
		"   "+th.Hints(k("open_anything"), "open anything: tables, saved queries, features"),
		"",
		"   "+th.Section.Render("Then"),
		"   "+th.Hints("↵", "edit a cell  ·  edits stay local until you commit"),
		"   "+th.Hints(k("preview"), "preview the SQL")+"   "+th.Hints(k("commit"), "commit")+"   "+th.Hints(k("discard"), "discard"),
		"   "+th.Hints(k("filters"), "filter rows")+"   "+th.Hints(k("show_structure"), "structure")+"   "+th.Hints(k("safe_mode"), "safe mode"),
		"",
		"   "+th.Dim.Render(pre+" ? lists every key for the pane you are in  ·  Esc cancels the prefix  ·  "+pre+" "+pre+" sends it literally"))
	for len(lines) < H {
		lines = append(lines, "")
	}
	return strings.Join(lines[:H], "\n")
}

func ansiTrunc(s string, w int) string { return fit(s, w) }

// firstKey is the key bound to action in any context (grid keys are shown in the empty-state hints too).
func firstKey(m *keymap.Map, action string) string {
	for _, b := range m.All() {
		if b.Action == action && !b.NoPrefix {
			return b.Key
		}
	}
	return "?"
}

func (w *Workspace) detailView(th Theme, W, H int) string {
	var lines []string
	d, ok := w.tab().(detailer)
	if !ok || len(d.Detail()) == 0 {
		lines = append(lines, "", th.Dim.Render("  select a row"))
	}
	if ok {
		for _, l := range d.Detail() {
			if l == "" {
				continue
			}
			name, val, _ := strings.Cut(l, ": ")
			mark := " "
			if strings.HasPrefix(name, "*") {
				mark, name = th.Warn.Render("*"), name[1:]
			}
			lines = append(lines, mark+th.Accent.Render(strings.TrimSpace(name)))
			if val == "NULL" {
				lines = append(lines, "  "+th.Null.Render("NULL"))
			} else {
				for _, wl := range wrapText(val, W-3) {
					lines = append(lines, "  "+wl)
				}
			}
		}
	}
	return strings.Join(padLines(lines, W, H), "\n")
}

func (w *Workspace) consoleView(th Theme, W, H int) string {
	var lines []string
	start := max(0, len(w.console)-H)
	for _, c := range w.console[start:] {
		mark := th.OK.Render("✓")
		if c.err {
			mark = th.Err.Render("✗")
		}
		sql := strings.Join(strings.Fields(c.sql), " ")
		lines = append(lines, " "+th.Dim.Render(c.at.Format("15:04:05"))+" "+mark+" "+sql+th.Dim.Render("  "+c.dur.Round(time.Millisecond).String()))
	}
	if len(lines) == 0 {
		lines = []string{th.Dim.Render("  every statement sent to the server appears here")}
	}
	return strings.Join(padLines(lines, W, H), "\n")
}
