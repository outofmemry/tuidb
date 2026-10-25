package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/keymap"
	"webtuidb/internal/store"
)

// Deps is everything the UI needs from the outside; tests build one with fakes.
type Deps struct {
	Cfg      config.Config
	Keys     *keymap.Map
	Conns    []config.Connection
	ConnPath string
	Hist     *store.History
	Queries  *store.Queries
	State    *store.State
	Connect  ConnectFunc
	NoTimers bool // tests: toasts stay until replaced, no timer commands are created
}

type App struct {
	d         *Deps
	th        Theme
	w, h      int
	welcome   *welcome
	wss       []*Workspace
	active    int
	askDB     bool // set by new_session: offer the database list once the connection opens
	inWS      bool // false: welcome screen
	overlays  []Overlay
	machine   keymap.Machine
	toast     string
	toastErr  bool
	toastSeq  int
	spin      int  // current spinner frame
	spinning  bool // a spinner tick is in flight
	nextID    int
	quit      bool
	deferred  []wsMsg // async results held back while a dialog is open
	startConn *config.Connection
}

func NewApp(d *Deps) *App {
	if d.Connect == nil {
		d.Connect = DefaultConnect
	}
	a := &App{d: d, th: NewTheme(d.Cfg.Theme), machine: keymap.Machine{Map: d.Keys}}
	a.welcome = newWelcome(a)
	return a
}

// StartWith makes the program connect to c as soon as it starts.
func (a *App) StartWith(c config.Connection) { a.startConn = &c }

// keepAliveEvery: idle connections are pinged so servers, firewalls and SSH tunnels do not drop them.
const keepAliveEvery = 30 * time.Second

func (a *App) keepAliveTick() tea.Cmd {
	if !a.d.Cfg.General.KeepAlive || a.d.NoTimers {
		return nil
	}
	return tea.Tick(keepAliveEvery, func(time.Time) tea.Msg { return keepAliveMsg{} })
}

func (a *App) Init() tea.Cmd {
	if a.startConn != nil {
		return tea.Batch(a.openConnection(*a.startConn), a.keepAliveTick())
	}
	if !a.d.Cfg.General.ReopenWorkspaces {
		return a.keepAliveTick()
	}
	// reopen what was open when webtuidb last quit (each in the database it was showing)
	var cmds []tea.Cmd
	for _, ws := range a.d.State.LastSession() {
		if c, ok := a.connByName(ws.Connection); ok {
			c.Database = orDefault(ws.Database, c.Database)
			cmds = append(cmds, a.openConnection(c))
		}
	}
	return tea.Batch(append(cmds, a.keepAliveTick())...)
}

// saveSession remembers the open workspaces for the next start.
func (a *App) saveSession() {
	var last []store.Workspace
	for _, w := range a.wss {
		last = append(last, store.Workspace{Connection: w.conn.Name, Database: w.sess.CurrentDatabase()})
	}
	a.d.State.SetLastSession(last)
}

// ---- messages ---------------------------------------------------------------------------

type wsMsg struct { // async result for workspace ws (and optionally one of its tabs)
	ws  int
	tab int
	gen int // workspace generation when the work started
	msg tea.Msg
}

type connectedMsg struct {
	name string
	o    *Opened
	err  error
}

type toastClearMsg struct{ seq int }

type keepAliveMsg struct{}

type testedMsg struct {
	name, info string
	err        error
}

// ---- helpers used by the other files ------------------------------------------------------

func (a *App) push(o Overlay) {
	a.overlays = append(a.overlays, o)
	a.machine = keymap.Machine{Map: a.d.Keys} // a pending prefix must not outlive a dialog
}

func (a *App) say(format string, args ...any) tea.Cmd {
	return a.notify(false, fmt.Sprintf(format, args...))
}

func (a *App) fail(err error) tea.Cmd { return a.notify(true, err.Error()) }

func (a *App) notify(isErr bool, text string) tea.Cmd {
	a.toast, a.toastErr = strings.ReplaceAll(text, "\n", " "), isErr
	a.toastSeq++
	if a.d.NoTimers {
		return nil
	}
	seq := a.toastSeq
	return tea.Tick(6*time.Second, func(time.Time) tea.Msg { return toastClearMsg{seq} })
}

func (a *App) errorDialog(title string, err error) {
	a.push(&textViewDlg{title: "✗ " + title, danger: true, lines: strings.Split(err.Error(), "\n")})
}

func (a *App) ws() *Workspace {
	if a.inWS && a.active >= 0 && a.active < len(a.wss) {
		return a.wss[a.active]
	}
	return nil
}

func (a *App) wsByID(id int) *Workspace {
	for _, w := range a.wss {
		if w.id == id {
			return w
		}
	}
	return nil
}

func (a *App) saveConns() error { return config.SaveConnections(a.d.ConnPath, a.d.Conns) }

// openConnection connects asynchronously and opens a workspace when done.
func (a *App) openConnection(c config.Connection) tea.Cmd {
	connect := a.d.Connect
	return tea.Batch(a.say("connecting to %s…", c.Name), func() tea.Msg {
		o, err := connect(context.Background(), c)
		return connectedMsg{name: c.Name, o: o, err: err}
	})
}

func (a *App) connByName(name string) (config.Connection, bool) {
	for _, c := range a.d.Conns {
		if c.Name == name {
			return c, true
		}
	}
	return config.Connection{}, false
}

// ---- update -----------------------------------------------------------------------------

// spinFrames animate the health dot and "loading…" labels while something runs.
var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type spinMsg struct{}

func spinTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return spinMsg{} })
}

func (a *App) spinner() string { return spinFrames[a.spin%len(spinFrames)] }

func (a *App) anyBusy() bool {
	for _, w := range a.wss {
		if w.busy() || w.applying || w.sb.loading {
			return true
		}
	}
	return false
}

// Update runs the message, then starts the spinner if that made a workspace busy; the
// spinner keeps ticking only while something is still running.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(spinMsg); ok {
		a.spin++
		if a.anyBusy() {
			return a, spinTick()
		}
		a.spinning = false
		return a, nil
	}
	m, cmd := a.update(msg)
	if !a.spinning && !a.d.NoTimers && a.anyBusy() {
		a.spinning = true
		cmd = tea.Batch(cmd, spinTick())
	}
	return m, cmd
}

func (a *App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = m.Width, m.Height
		for _, w := range a.wss {
			w.resize(a.w, a.h-2)
		}
		return a, nil
	case runActionMsg:
		return a, a.run(m.action)
	case keepAliveMsg:
		var cmds []tea.Cmd
		for _, w := range a.wss {
			cmds = append(cmds, w.ping())
		}
		return a, tea.Batch(append(cmds, a.keepAliveTick())...)
	case testedMsg:
		if m.err != nil {
			a.toast = ""
			a.errorDialog("Connection test failed: "+m.name, m.err)
			return a, nil
		}
		return a, a.say("%s: connection works (%s)", m.name, m.info)
	case toastClearMsg:
		if m.seq == a.toastSeq {
			a.toast = ""
		}
		return a, nil
	case connectedMsg:
		if m.err != nil {
			a.askDB = false
			a.toast = ""
			a.errorDialog("Connection failed", m.err)
			return a, nil
		}
		c, ok := a.connByName(m.name)
		if !ok {
			m.o.Close()
			return a, nil
		}
		a.nextID++
		w := newWorkspace(a, a.nextID, c, m.o)
		w.resize(a.w, a.h-2)
		a.wss = append(a.wss, w)
		a.active, a.inWS = len(a.wss)-1, true
		cmds := []tea.Cmd{a.say("connected to %s", c.Name), w.init()}
		if a.askDB { // new_session: databasesMsg shows the picker only when there is a choice
			cmds = append(cmds, w.pickDatabase())
		}
		return a, tea.Batch(cmds...)
	case reconnectedMsg:
		w := a.wsByID(m.ws)
		if m.err != nil {
			a.errorDialog("Reconnect failed", m.err)
			return a, nil
		}
		if w == nil {
			m.o.Close()
			return a, nil
		}
		old := w.o
		w.o, w.sess, w.caps = m.o, m.o.Sess, m.o.Sess.Caps()
		w.forget(nil) // also bumps the generation: in-flight results from the old session are dropped
		old.Close()
		cmds := []tea.Cmd{a.say("reconnected to %s", w.conn.Name), w.sb.load()}
		for _, t := range w.tabs {
			if tt, ok := t.(*tableTab); ok {
				cmds = append(cmds, tt.load())
			}
		}
		return a, tea.Batch(cmds...)
	case wsMsg:
		if len(a.overlays) > 0 { // results that would open a dialog over the current one wait until it closes
			a.deferred = append(a.deferred, m)
			return a, nil
		}
		if w := a.wsByID(m.ws); w != nil {
			return a, w.handle(m)
		}
		return a, nil
	case tea.InterruptMsg, tea.QuitMsg: // Ctrl+C as a signal, SIGTERM, SIGHUP: same teardown as quit
		if a.quit {
			return a, nil // already torn down: tea.Quit itself arrives here as a QuitMsg
		}
		a.saveSession()
		for _, w := range a.wss {
			w.close()
		}
		a.quit = true
		return a, tea.Quit
	case tea.KeyPressMsg:
		return a, a.key(m)
	case tea.MouseWheelMsg:
		// the wheel is three arrow keys (the terminal convention), so every pane and dialog
		// scrolls without knowing about the mouse; horizontal ticks move one column
		code, n := tea.KeyDown, 3
		switch m.Button {
		case tea.MouseWheelUp:
			code = tea.KeyUp
		case tea.MouseWheelLeft:
			code, n = tea.KeyLeft, 1
		case tea.MouseWheelRight:
			code, n = tea.KeyRight, 1
		}
		var cmds []tea.Cmd
		for i := 0; i < n; i++ {
			cmds = append(cmds, a.key(tea.KeyPressMsg{Code: code}))
		}
		return a, tea.Batch(cmds...)
	case tea.ClipboardMsg:
		if len(a.overlays) == 0 {
			return a, a.forward(m)
		}
		return a, nil
	case tea.PasteMsg:
		if len(a.overlays) > 0 {
			return a, a.updateTop(m)
		}
		return a, a.forward(m)
	}
	return a, nil
}

func (a *App) updateTop(msg tea.Msg) tea.Cmd {
	top := len(a.overlays) - 1
	o, cmd := a.overlays[top].Update(msg)
	// The callback may have pushed a follow-up dialog above this one (pick -> form):
	// drop only the one that closed, never truncate.
	if o == nil {
		a.overlays = append(a.overlays[:top], a.overlays[top+1:]...)
	} else {
		a.overlays[top] = o
	}
	if len(a.overlays) == 0 && len(a.deferred) > 0 {
		q := a.deferred
		a.deferred = nil
		cmds := []tea.Cmd{cmd}
		for _, m := range q {
			m := m
			cmds = append(cmds, func() tea.Msg { return m })
		}
		return tea.Batch(cmds...)
	}
	return cmd
}

// forward hands a key to the focused widget.
func (a *App) forward(msg tea.Msg) tea.Cmd {
	if w := a.ws(); w != nil {
		return w.input(msg)
	}
	return a.welcome.input(msg)
}

func (a *App) context() string {
	if w := a.ws(); w != nil {
		return w.context()
	}
	return keymap.Global
}

func (a *App) key(k tea.KeyPressMsg) tea.Cmd {
	if len(a.overlays) > 0 {
		return a.updateTop(k)
	}
	res := a.machine.Feed(a.context(), keymap.FromMsg(k))
	switch {
	case res.Action != "":
		return a.run(res.Action)
	case res.Unknown:
		return a.notify(true, "no binding for that key after the prefix (C-d ? lists them)")
	case res.Pending:
		return nil
	case res.Passthrough:
		if keymap.FromMsg(k) == a.d.Keys.Prefix { // prefix twice: deliver the raw key
			return a.forward(k)
		}
		return a.forward(k)
	}
	return nil
}

// run executes a prefix action. Workspace-only actions are ignored on the welcome screen.
func (a *App) run(action string) tea.Cmd {
	switch action {
	case "quit":
		return a.confirmQuit()
	case "help":
		a.showHelp()
		return nil
	case "workspaces":
		return a.pickWorkspace()
	case "open_connection":
		return a.pickConnection(false)
	case "new_session":
		return a.pickConnection(true)
	case "switch_session":
		return a.pickWorkspace()
	case "next_workspace", "prev_workspace":
		if len(a.wss) > 0 {
			d := 1
			if action == "prev_workspace" {
				d = -1
			}
			a.active = (a.active + d + len(a.wss)) % len(a.wss)
			a.inWS = true
		}
		return nil
	case "new_tab":
		if a.ws() == nil {
			a.inWS = false
			return nil
		}
	}
	if w := a.ws(); w != nil {
		return w.action(action)
	}
	return a.welcome.action(action)
}

func (a *App) confirmQuit() tea.Cmd {
	quit := func() tea.Cmd {
		a.saveSession()
		for _, w := range a.wss {
			w.close()
		}
		a.quit = true
		return tea.Quit
	}
	for _, w := range a.wss {
		if w.applying {
			a.push(&confirmDlg{title: "A commit is still running. Quit anyway?", lines: []string{"Quitting now cuts the running statements off."}, yes: "Quit", danger: true, onYes: quit})
			return nil
		}
	}
	for _, w := range a.wss {
		if w.dirty() {
			a.push(&confirmDlg{title: "Quit with uncommitted changes?", lines: []string{"Pending edits will be lost."}, yes: "Quit", danger: true, onYes: quit})
			return nil
		}
	}
	return quit()
}

// pickConnection lists saved connections; askDB makes the new workspace offer its database list next.
func (a *App) pickConnection(askDB bool) tea.Cmd {
	var items []pickItem
	for _, c := range a.d.Conns {
		items = append(items, pickItem{Label: c.Name, Detail: c.Driver + " " + c.Host, Value: c})
	}
	a.push(&pickerDlg{title: "Open connection", filter: true, items: items, empty: "no saved connections",
		onPick: func(it pickItem, _ bool) tea.Cmd {
			a.askDB = askDB
			return a.openConnection(it.Value.(config.Connection))
		}})
	return nil
}

func (a *App) pickWorkspace() tea.Cmd {
	var items []pickItem
	for i, w := range a.wss {
		detail := w.conn.Driver
		if i == a.active && a.inWS {
			detail += "  ● current"
		}
		items = append(items, pickItem{Label: w.conn.Name + " / " + w.sess.CurrentDatabase(), Detail: detail, Value: i})
	}
	items = append(items, pickItem{Label: "‹ connection list ›", Value: -1})
	a.push(&pickerDlg{title: "Workspaces", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		if i := it.Value.(int); i >= 0 {
			a.active, a.inWS = i, true
		} else {
			a.inWS = false
		}
		return nil
	}})
	return nil
}

// ---- view -------------------------------------------------------------------------------

func (a *App) View() tea.View {
	v := tea.NewView(a.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion // for the wheel; shift-drag still selects terminal text
	v.WindowTitle = "webtuidb"
	return v
}

func (a *App) render() string {
	if a.w < 20 || a.h < 6 {
		return fmt.Sprintf("webtuidb needs at least 20×6 cells (this terminal is %d×%d)", a.w, a.h)
	}
	bodyH := a.h - 2
	var body string
	if w := a.ws(); w != nil {
		body = w.view(a.th, a.w, bodyH)
	} else {
		body = a.welcome.view(a.th, a.w, bodyH)
	}
	lines := strings.Split(body, "\n")
	if a.machine.Pending() {
		wk := a.whichKey(a.w)
		lines = append(lines[:max(0, len(lines)-len(wk))], wk...)
	}
	out := a.topBar() + "\n" + strings.Join(lines, "\n") + "\n" + a.statusLine()
	for _, o := range a.overlays {
		out = composite(out, o.View(a.th, a.w, a.h), a.w, a.h)
	}
	return out
}

// whichKey is the panel shown while the prefix is pending: the bindings of the focused pane,
// then the global ones, laid out in columns that fit the width. tab_1..9 collapse to one entry.
func (a *App) whichKey(w int) []string {
	ctx := a.context()
	type entry struct{ key, desc string }
	var local, global []entry
	tabs := false
	for _, b := range a.d.Keys.Bindings(ctx) {
		if strings.HasPrefix(b.Action, "tab_") {
			tabs = true
			continue
		}
		e := entry{keymap.Display(b.Key), b.Desc}
		if b.Context == keymap.Global {
			global = append(global, e)
		} else {
			local = append(local, e)
		}
	}
	if tabs {
		global = append(global, entry{"1-9", "go to tab"})
	}
	byDesc := func(es []entry) { sort.SliceStable(es, func(i, j int) bool { return es[i].desc < es[j].desc }) }
	byDesc(local)
	byDesc(global)
	colW := 0
	for _, e := range append(append([]entry{}, local...), global...) {
		colW = max(colW, width(e.key)+1+width(e.desc)+3)
	}
	colW = clamp(colW, 24, 40)
	cols := max(1, w/colW)
	maxRows := max(4, a.h/4)
	layout := func(es []entry, title string) []string {
		if len(es) == 0 {
			return nil
		}
		rows := (len(es) + cols - 1) / cols
		if rows > maxRows {
			rows = maxRows
		}
		out := []string{a.th.Popup.Render(fit(" "+a.th.Section.Render(title), w))}
		shown := 0
		for r := 0; r < rows; r++ {
			var sb strings.Builder
			for c := 0; c < cols; c++ {
				i := c*rows + r
				if i < len(es) {
					e := es[i]
					sb.WriteString(" " + a.th.chip(e.key) + strings.Repeat(" ", max(1, 11-width(e.key))) + fit(e.desc, colW-13) + " ")
					shown++
				}
			}
			out = append(out, a.th.Popup.Render(fit(sb.String(), w)))
		}
		if shown < len(es) {
			out = append(out, a.th.Popup.Render(fit(a.th.Dim.Render(fmt.Sprintf("  … %d more: %s ? lists them all", len(es)-shown, keymap.Display(a.d.Keys.Prefix))), w)))
		}
		return out
	}
	title := strings.ToUpper(ctx[:1]) + ctx[1:]
	pre := keymap.Display(a.d.Keys.Prefix)
	out := []string{
		a.th.Popup.Render(a.th.Border.Render(strings.Repeat("─", w))),
		a.th.Popup.Render(fit(a.th.Prefix.Render(" "+pre+" … ")+a.th.Popup.Bold(true).Render(" press a key")+a.th.Popup.Foreground(lipgloss.Color(a.th.pal["dim"])).Render("   Esc cancels  ·  "+pre+" again sends it literally"), w)),
	}
	out = append(out, layout(local, title)...)
	out = append(out, layout(global, "Global")...)
	return out
}

// topBar is the first line: the wordmark, then where you are (connection ▸ database ▸ tab)
// with the connection's badges, and the workspace state on the right.
func (a *App) topBar() string {
	th := a.th
	bar := th.StatusBar
	l := th.TabActive.Render(" ◆ webtuidb ") + bar.Render(" ")
	var right string
	if w := a.ws(); w != nil {
		left, rightParts := w.crumbs(th)
		l += strings.Join(left, bar.Foreground(lipgloss.Color(th.pal["dim"])).Render(" ▸ "))
		right = strings.Join(rightParts, bar.Render("  "))
	} else {
		l += bar.Render(fmt.Sprintf("%d connections", len(a.d.Conns)))
	}
	if right != "" {
		right += bar.Render(" ")
	}
	gap := a.w - width(l) - width(right)
	if gap < 1 {
		right, gap = "", 0
		l = bar.Render(fit(l, a.w))
	}
	return l + bar.Render(strings.Repeat(" ", gap)) + right
}

// statusLine is the last line: the prefix badge and the table context on the left, the keys
// for the focused pane (or the latest toast) on the right.
func (a *App) statusLine() string {
	var l string
	if a.machine.Pending() {
		l = a.th.Prefix.Render(" PREFIX ")
	}
	if w := a.ws(); w != nil {
		if c := w.statusContext(); c != "" {
			l += a.th.StatusBar.Foreground(lipgloss.Color(a.th.pal["dim"])).Render(" " + c + " ")
		}
	}
	var right string
	switch {
	case a.toast != "":
		st := a.th.StatusBar
		icon := "✓ "
		if a.toastErr {
			st, icon = st.Foreground(lipgloss.Color(a.th.pal["error"])).Bold(true), "✗ "
		} else {
			st = st.Foreground(lipgloss.Color(a.th.pal["ok"]))
		}
		right = st.Render(" " + icon + a.toast + " ")
	default:
		// hints give way to the context: drop pairs from the end until both fit (help stays)
		pairs := a.contextHintPairs()
		for {
			right = a.th.StatusBar.Render(" " + a.renderHints(pairs) + " ")
			if len(pairs) <= 2 || a.w-width(l)-width(right) >= 0 {
				break
			}
			pairs = append(pairs[:len(pairs)-4], pairs[len(pairs)-2:]...)
		}
	}
	gap := a.w - width(l) - width(right)
	if gap < 0 {
		right = fit(right, max(0, a.w-width(l)))
		gap = 0
	}
	return l + a.th.StatusBar.Render(strings.Repeat(" ", gap)) + right
}

// contextHints are the three or four most useful keys for the focused pane, shown on the right of
// the status line so the next step is always visible.
// contextHintPairs are key/label pairs for the focused pane; the last pair is always help.
func (a *App) contextHintPairs() []string {
	pre := keymap.Display(a.d.Keys.Prefix)
	k := func(action string) string {
		for _, b := range a.d.Keys.Bindings(a.context()) {
			if b.Action == action {
				return pre + " " + keymap.Display(b.Key)
			}
		}
		return pre + " ?"
	}
	var pairs []string
	switch a.context() {
	case keymap.Grid:
		pairs = []string{"↵", "edit", k("commit"), "commit", k("filters"), "filter", k("new_query_tab"), "sql"}
	case keymap.Editor:
		pairs = []string{k("run_current"), "run", k("run_all"), "run all", k("cancel_query"), "cancel"}
	case keymap.Results:
		pairs = []string{"↵", "quick look", "[ ]", "result tabs", "Esc", "editor"}
	case keymap.Sidebar:
		pairs = []string{"↵", "open", "/", "find", "m", "menu", k("new_query_tab"), "sql"}
	case keymap.FilterBar:
		pairs = []string{"↵", "apply", "Tab", "field", "Esc", "hide"}
	case keymap.Structure:
		pairs = []string{"↵", "edit", k("add_row"), "add", k("commit"), "commit"}
	default:
		if a.ws() == nil {
			pairs = []string{"↵", "connect", "n", "new"}
		}
	}
	return append(pairs, pre+" ?", "help")
}

func (a *App) renderHints(pairs []string) string {
	bar := lipgloss.NewStyle().Background(lipgloss.Color(a.th.pal["status_bg"]))
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, a.th.chip(pairs[i])+bar.Foreground(lipgloss.Color(a.th.pal["dim"])).Render(" "+pairs[i+1]))
	}
	return strings.Join(parts, bar.Render("  "))
}
