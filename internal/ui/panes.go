// panes.go: Split panes: the pane groups, focusing, splitting and closing, and the editor/result split of a query tab.

package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"webtuidb/internal/ai"
	"webtuidb/internal/db"
	"webtuidb/internal/keymap"
	"webtuidb/internal/store"
)

// paneState is the per-workspace split-pane and back/forward state.
type paneState struct {
	// go back / forward: hist[:cur] is the path, hist[cur-1] the open object
	hist []db.ObjRef
	cur  int
	nav  bool // a back/forward is opening an object: do not record it

	// split pane: tabs stay in Workspace.tabs, grp says which group (0 left, 1 right) each is in.
	// Workspace.cur is always the active tab of the focused group (pane); otherID is the other group's.
	split   bool
	pane    int
	otherID int
	grp     map[int]int

	chat     []ai.Message // the assistant conversation lives as long as the workspace
	model    string       // model picked in the assistant for this session; "" = [ai] model
	provider string       // [ai.providers.<name>] picked for the chat; "" = the default
	curChat  *store.Chat  // the saved record of chat (nil until the first exchange)
}

func (m *paneState) reset() {
	*m = paneState{chat: m.chat, model: m.model, provider: m.provider, curChat: m.curChat}
}

func (m *paneState) place(t tabber) {
	if m.split {
		m.grp[t.ID()] = m.pane
	}
}

func (m *paneState) visit(ref db.ObjRef) {
	if m.nav || m.cur > 0 && m.hist[m.cur-1] == ref {
		return
	}
	m.hist = append(m.hist[:m.cur], ref)
	if len(m.hist) > 50 {
		m.hist = m.hist[1:]
	}
	m.cur = len(m.hist)
}

func (w *Workspace) pane(t tabber) int {
	if !w.panes.split {
		return 0
	}
	return w.panes.grp[t.ID()]
}

// groupIdx lists the indexes (into w.tabs) of the tabs in group g.
func (w *Workspace) groupIdx(g int) []int {
	var out []int
	for i, t := range w.tabs {
		if w.pane(t) == g {
			out = append(out, i)
		}
	}
	return out
}

func (w *Workspace) indexByID(id int) int {
	for i, t := range w.tabs {
		if t.ID() == id {
			return i
		}
	}
	return -1
}

func (w *Workspace) stepTab(d int) {
	idx := w.groupIdx(w.panes.pane)
	for p, i := range idx {
		if i == w.cur {
			w.selectTab(idx[(p+d+len(idx))%len(idx)])
			return
		}
	}
	w.selectTab(w.cur + d)
}

// fixSplit repairs the split after tabs moved or closed: an empty group closes the split, the active
// tab is in the focused group and otherID names a tab of the other one.
func (w *Workspace) fixSplit() {
	m := &w.panes
	if !m.split {
		return
	}
	l, r := w.groupIdx(m.pane), w.groupIdx(1-m.pane)
	if len(l) == 0 || len(r) == 0 {
		m.split, m.pane, m.grp = false, 0, nil
		return
	}
	if w.cur < 0 || w.cur >= len(w.tabs) || w.pane(w.tabs[w.cur]) != m.pane {
		w.cur = l[len(l)-1]
	}
	if o := w.indexByID(m.otherID); o < 0 || w.pane(w.tabs[o]) == m.pane {
		m.otherID = w.tabs[r[len(r)-1]].ID()
	}
}

func (w *Workspace) splitRight() tea.Cmd {
	t, m := w.tab(), &w.panes
	if t == nil {
		return w.a.say("open a tab first")
	}
	if !m.split {
		if len(w.tabs) < 2 {
			return w.a.say("open a second tab to split the pane")
		}
		m.split, m.grp = true, map[int]int{}
		for _, x := range w.tabs {
			m.grp[x.ID()] = 0
		}
		m.grp[t.ID()], m.pane = 1, 1
		m.otherID = -1
	} else { // move the active tab to the other group; an emptied group closes the split
		m.grp[t.ID()] = 1 - m.pane
		m.pane = 1 - m.pane
		m.otherID = -1
	}
	w.fixSplit()
	return nil
}

// closePane closes the focused split pane: its tabs join the other group (no tab is closed).
func (w *Workspace) closePane() tea.Cmd {
	m := &w.panes
	if !m.split {
		return w.a.say("there is one pane: nothing to close")
	}
	if o := w.indexByID(m.otherID); o >= 0 { // the other pane's active tab stays the active one
		w.cur = o
	}
	m.split, m.grp, m.pane, m.otherID = false, nil, 0, -1
	w.focusSB = false
	return nil
}

func (w *Workspace) cyclePane() tea.Cmd {
	m := &w.panes
	o := w.indexByID(m.otherID)
	if !m.split || o < 0 || w.tab() == nil {
		return w.a.say("there is one pane: %s \" splits it", keymap.Display(w.a.d.Keys.Prefix))
	}
	m.otherID, w.cur, m.pane = w.tab().ID(), o, 1-m.pane
	w.focusSB = false
	return nil
}

// splitFrames draws the two groups side by side.
func (w *Workspace) splitFrames(th Theme, W, H int) string {
	lw := (W - 1) / 2
	var frames []string
	for g, fw := range []int{lw, W - 1 - lw} {
		i := w.cur
		if w.panes.pane != g {
			i = w.indexByID(w.panes.otherID)
		}
		var t tabber
		if i >= 0 && i < len(w.tabs) {
			t = w.tabs[i]
		}
		mine := w.panes.pane == g
		frames = append(frames, w.paneFrame(th, t, g, fw, H, mine && !w.focusSB && !w.focusDetail, mine && !w.focusSB))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, frames[0], " ", frames[1])
}

// goHist re-opens the previous (d=-1) or next (d=1) object.
func (w *Workspace) goHist(d int) tea.Cmd {
	m := &w.panes
	n := m.cur + d
	if n < 1 || n > len(m.hist) {
		return w.a.say("nothing to go %s to", map[int]string{-1: "back", 1: "forward"}[d])
	}
	m.cur, m.nav = n, true
	defer func() { m.nav = false }()
	return w.openObject(m.hist[n-1], false)
}

func (w *Workspace) toolsAction(name string) tea.Cmd {
	switch name {
	case "users":
		return w.usersAction()
	case "diagram":
		return w.diagramAction("")
	case "metrics":
		return w.metricsAction()
	case "backup", "restore":
		return w.backupAction(name == "restore")
	case "split_right":
		return w.splitRight()
	case "close_pane":
		return w.closePane()
	case "assistant_new":
		return w.assistantNew()
	case "assistant_clear":
		return w.assistantClear()
	case "assistant_history":
		return w.assistantHistory()
	case "next_pane", "prev_pane": // two groups: next and previous are the same move
		return w.cyclePane()
	case "assistant":
		return w.assistantAction()
	case "plugins":
		return w.pluginsAction("")
	case "go_back":
		return w.goHist(-1)
	case "go_forward":
		return w.goHist(1)
	case "mcp_log":
		return w.mcpLog()
	}
	return nil
}

// paneDlg is a big scrollable text window with its own keys (diagram, metrics board).
type paneDlg struct {
	title, hint string
	lines       func(th Theme, W int) []string
	keys        map[string]func() tea.Cmd
	top, left   int
	done        bool // a key handler sets this to close the window
	onClose     func()
}

func (d *paneDlg) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}
	ks := k.Keystroke()
	if f, ok := d.keys[ks]; ok {
		cmd := f()
		if d.done {
			d.close()
			return nil, cmd
		}
		return d, cmd
	}
	switch ks {
	case "esc", "q":
		d.close()
		return nil, nil
	case "down", "j":
		d.top++
	case "up", "k":
		d.top = max(0, d.top-1)
	case "pgdown", "space":
		d.top += 10
	case "pgup":
		d.top = max(0, d.top-10)
	case "home", "g":
		d.top, d.left = 0, 0
	case "end", "G":
		d.top = 1 << 30
	case "right", "l":
		d.left += 8
	case "left", "h":
		d.left = max(0, d.left-8)
	}
	return d, nil
}

func (d *paneDlg) close() {
	if d.onClose != nil {
		d.onClose()
	}
}

func (d *paneDlg) View(th Theme, maxW, maxH int) string {
	w, vis := maxW-2, max(4, maxH-6)
	all := d.lines(th, w-4)
	d.top = clamp(d.top, 0, max(0, len(all)-vis))
	body := all[d.top:min(len(all), d.top+vis)]
	out := make([]string, len(body))
	for i, l := range body {
		out[i] = fit(ansi.TruncateLeft(l, d.left, ""), w-4)
	}
	hint := d.hint
	if len(all) > vis {
		hint += fmt.Sprintf("   (%d-%d/%d)", d.top+1, d.top+len(body), len(all))
	}
	return box(th, d.title, out, hint, w)
}

// splitDown stacks a second editor under the first in this tab; again, it closes the unfocused one.
func (t *queryTab) splitDown() tea.Cmd {
	if t.other == nil {
		c := t.ws.a.d.Cfg.Editor
		e := NewEditor(t.ws.caps.Dialect, EditorOpts{TabWidth: c.IndentWidth, UseTabs: c.Indent == "tabs", CloseBraces: c.CloseBraces, Uppercase: c.UppercaseKeywords,
			CompleteKey: c.CompleteKey, Highlight: c.HighlightCurrent, Autocomplete: c.Autocomplete, Wrap: c.Wrap, AddSpace: c.AddSpace})
		e.Complete = t.complete
		t.ed, t.other, t.focusBottom = e, t.ed, true
		t.savedAs, t.lastAuto = "", "" // auto-save must not write the new pane over the saved query
		return nil
	}
	drop := func() tea.Cmd {
		t.other, t.focusBottom = nil, false
		return t.ws.a.say("closed the other editor pane")
	}
	if strings.TrimSpace(t.other.Text()) != "" {
		t.ws.a.push(&confirmDlg{title: "Close the other editor pane?", lines: []string{"Its text is dropped."}, yes: "Close", danger: true, onYes: drop})
		return nil
	}
	return drop()
}

// swapPane moves the focus to the other editor of a split tab.
func (t *queryTab) swapPane() {
	t.ed, t.other = t.other, t.ed
	t.savedAs, t.lastAuto = "", ""
	t.focusBottom = !t.focusBottom
}

// editorsView draws the editor area: one editor, or two stacked with a rule between.
func (t *queryTab) editorsView(th Theme, W, H int, focused bool) string {
	if t.other == nil || H < 5 {
		t.ed.W, t.ed.H = W, H
		return t.ed.View(th, focused)
	}
	hTop := (H - 1) / 2
	top, bot := t.ed, t.other
	if t.focusBottom {
		top, bot = t.other, t.ed
	}
	top.W, top.H, bot.W, bot.H = W, hTop, W, H-1-hTop
	return top.View(th, focused && !t.focusBottom) + "\n" + th.Border.Render(strings.Repeat("─", W)) + "\n" + bot.View(th, focused && t.focusBottom)
}
