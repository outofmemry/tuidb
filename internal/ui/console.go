// console.go: The console: lines, input, selection, actions and view.

package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/keymap"
	"webtuidb/internal/menus"
	"webtuidb/internal/store"
)

type consoleState struct {
	sel        int // index into the visible lines; -1 follows the newest
	top        int
	find       string
	q          lineEdit
	searching  bool
	errorsOnly bool
}

// consoleDrawn reports whether the console pane has room on screen (layoutOf hides it in a short terminal).
func (w *Workspace) consoleDrawn() bool {
	_, _, conH, _, _ := layoutOf(w.showSB, w.showDetail, w.showConsole, w.a.w, w.a.bodyH())
	return conH > 0
}

// consoleFocused is true only while the console both has the keys and is drawn: a pane that vanished on a
// resize must not keep swallowing them (P2-78).
func (w *Workspace) consoleFocused() bool {
	if w.focusConsole && !w.consoleDrawn() {
		w.focusConsole = false
	}
	return w.focusConsole && w.showConsole
}

// consoleLines is what the pane shows: every statement, or only the failed ones.
func (w *Workspace) consoleLines() []store.ConsoleLine {
	all := w.console.Lines()
	if !w.con.errorsOnly {
		return all
	}
	var out []store.ConsoleLine
	for _, l := range all {
		if l.Failed {
			out = append(out, l)
		}
	}
	return out
}

func (w *Workspace) consoleSel(n int) int {
	if w.con.sel < 0 || w.con.sel >= n {
		return n - 1
	}
	return w.con.sel
}

// consoleMatch reports whether line l contains the find text (case-insensitive).
func (w *Workspace) consoleMatch(l store.ConsoleLine) bool {
	return w.con.find != "" && strings.Contains(strings.ToLower(l.SQL), strings.ToLower(w.con.find))
}

// consoleStep moves to the next (d=1) or previous (d=-1) match, wrapping; it reports whether there is one.
func (w *Workspace) consoleStep(d int) bool {
	ls := w.consoleLines()
	n := len(ls)
	for i := 1; i <= n; i++ {
		j := ((w.consoleSel(n)+d*i)%n + n) % n
		if w.consoleMatch(ls[j]) {
			w.con.sel = j
			return true
		}
	}
	return false
}

func (w *Workspace) consoleInput(msg tea.Msg) tea.Cmd {
	c := &w.con
	if c.searching {
		if p, ok := msg.(tea.PasteMsg); ok {
			c.q.Handle(p)
			return nil
		}
		k, ok := msg.(tea.KeyPressMsg)
		if !ok {
			return nil
		}
		switch k.Keystroke() {
		case "esc":
			c.searching = false
		case "enter":
			c.searching, c.find = false, c.q.Value()
			c.sel = max(0, w.consoleSel(len(w.consoleLines()))-1) // so the first step lands on the current line if it matches
			if c.find != "" && !w.consoleStep(1) {
				return w.a.say("%q is not in the console", c.find)
			}
		default:
			c.q.Handle(k)
		}
		return nil
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	ls := w.consoleLines()
	cur := w.consoleSel(len(ls))
	key := keymap.FromMsg(k)
	if act, ok := w.a.d.Keys.Lookup(keymap.Console, key); ok && strings.HasPrefix(act, "console_") {
		return w.consoleKeyAction(act)
	}
	switch k.Keystroke() {
	case "esc":
		if c.find != "" {
			c.find = ""
		} else {
			w.focusConsole = false
		}
	case "down", "j":
		c.sel = min(cur+1, len(ls)-1)
	case "up", "k":
		c.sel = max(cur-1, 0)
	case "pgdown":
		c.sel = min(cur+8, len(ls)-1)
	case "pgup":
		c.sel = max(cur-8, 0)
	case "home", "g":
		c.sel = 0
	case "end", "G":
		c.sel = -1
	}
	return nil
}

// consoleKeyAction runs a console_* action: the keys come from the keymap, so [keys] can rebind them.
func (w *Workspace) consoleKeyAction(act string) tea.Cmd {
	c := &w.con
	switch act {
	case "console_find":
		c.searching = true
		c.q.Set(c.find)
	case "console_next":
		if !w.consoleStep(1) {
			return w.a.say("no match: press %s to find", keymap.Display(firstKey(w.a.d.Keys, "console_find")))
		}
	case "console_prev":
		if !w.consoleStep(-1) {
			return w.a.say("no match: press %s to find", keymap.Display(firstKey(w.a.d.Keys, "console_find")))
		}
	case "console_copy":
		return w.consoleAction("copy")
	case "console_errors":
		return w.consoleAction("errors_only")
	case "console_menu":
		return w.consoleMenu()
	}
	return nil
}

func (w *Workspace) consoleMenu() tea.Cmd {
	w.a.pushMenu("Console", menus.Console, menus.Env{}, w.consoleDo)
	return nil
}

func (w *Workspace) consoleDo(it menus.Item) tea.Cmd { return w.consoleAction(it.Action) }

func (w *Workspace) consoleAction(act string) tea.Cmd {
	c := &w.con
	switch act {
	case "find":
		c.searching = true
		c.q.Set(c.find)
		w.focusConsole, w.showConsole = true, true
	case "copy":
		ls := w.consoleLines()
		if len(ls) == 0 {
			return w.a.say("the console is empty")
		}
		sql := ls[w.consoleSel(len(ls))].SQL
		return tea.Batch(setClip(sql), w.a.say("copied the line"))
	case "errors_only":
		c.errorsOnly, c.sel = !c.errorsOnly, -1
		if c.errorsOnly {
			return w.a.say("console: failed statements only")
		}
		return w.a.say("console: all statements")
	case "clear_console":
		w.console.Clear()
		c.sel = -1
		return w.a.say("console cleared")
	}
	return nil
}

// consoleTitle and consoleHint are the frame's edge texts.
func (w *Workspace) consoleTitle() string {
	t := fmt.Sprintf("Console · %d statements", len(w.console.Lines()))
	if w.con.errorsOnly {
		t += " · errors only"
	}
	if w.con.find != "" {
		t += " · /" + w.con.find
	}
	return t
}

func (w *Workspace) consoleHint(th Theme) string {
	if w.focusConsole {
		k := func(a string) string { return keymap.Display(firstKey(w.a.d.Keys, a)) }
		return th.chips(k("console_find"), "find", k("console_next")+"/"+k("console_prev"), "next", k("console_copy"), "copy", k("console_errors"), "errors", k("console_menu"), "menu", "Esc", "back")
	}
	return th.chips(keymapDisplay(w.a, "toggle_console"), "hide")
}

// consoleView draws the pane: the newest H lines, or a window around the cursor while it has the focus.
func (w *Workspace) consoleView(th Theme, W, H int) string {
	ls := w.consoleLines()
	prompt := w.con.searching
	if prompt {
		H--
	}
	var lines []string
	cur := w.consoleSel(len(ls))
	top := max(0, len(ls)-H)
	if w.focusConsole {
		c := &w.con
		if cur < c.top {
			c.top = cur
		}
		if cur >= c.top+H {
			c.top = cur - H + 1
		}
		c.top = clamp(c.top, 0, max(0, len(ls)-H))
		top = c.top
	}
	for i := top; i < min(len(ls), top+H); i++ {
		l := ls[i]
		mark := th.OK.Render("✓")
		if l.Failed {
			mark = th.Err.Render("✗")
		}
		sql := strings.Join(strings.Fields(l.SQL), " ")
		var shown string
		if w.consoleMatch(l) {
			shown = markText(th, sql, w.con.find)
		} else {
			shown = highlight(th, sql, lexLines(w.caps.Dialect, []string{sql})[0])
		}
		line := " " + th.Dim.Render(l.At.Format("15:04:05")) + " " + mark + " " + shown + th.Dim.Render("  "+l.Dur.Round(time.Millisecond).String())
		if w.focusConsole && i == cur {
			line = th.SelRow.Render(fit(line, W))
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		msg := "  every statement sent to the server appears here"
		if w.con.errorsOnly {
			msg = "  no failed statements"
		}
		lines = []string{th.Dim.Render(msg)}
	}
	out := padLines(lines, W, H)
	if prompt {
		out = append(out, th.Accent.Render("/ ")+w.con.q.View(W-2, th, true))
	}
	return strings.Join(out, "\n")
}

// markText highlights every case-insensitive occurrence of q in s.
func markText(th Theme, s, q string) string {
	if q == "" {
		return s
	}
	ls, lq := strings.ToLower(s), strings.ToLower(q)
	var sb strings.Builder
	for {
		i := strings.Index(ls, lq)
		if i < 0 || len(ls) != len(s) { // lowercasing changed the byte length: give up on the highlight
			sb.WriteString(s)
			break
		}
		sb.WriteString(s[:i])
		sb.WriteString(th.Accent.Bold(true).Reverse(true).Render(s[i : i+len(q)]))
		s, ls = s[i+len(q):], ls[i+len(q):]
	}
	return sb.String()
}

func keymapDisplay(a *App, action string) string {
	return keymap.Display(a.d.Keys.Prefix) + " " + keymap.Display(firstKey(a.d.Keys, action))
}
