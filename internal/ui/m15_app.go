package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/keymap"
)

// appM15 is the App state added in M15: terminal focus (finish signals), the last key (passcode idle lock) and
// the mouse (drag in progress, double click detection).
type appM15 struct {
	blurred bool
	lastKey time.Time
	mouse   mouseState
}

// zen reports whether the active workspace hides its panels and the status line (toggle_panels).
func (a *App) zen() bool {
	w := a.ws()
	return w != nil && w.zen
}

// bodyH is the height of the workspace body: everything but the top bar and (outside zen) the status line.
func (a *App) bodyH() int {
	if a.zen() {
		return a.h - 1
	}
	return a.h - 2
}

// configuredHints is the status line from [general] hints: those actions only, in that order. nil = not set.
// config rejects names that are not actions, so an entry is skipped here only when it has no key in this context.
func (a *App) configuredHints() []string {
	if len(a.d.Cfg.General.Hints) == 0 {
		return nil
	}
	pre := keymap.Display(a.d.Keys.Prefix)
	all := a.d.Keys.Bindings(a.context())
	var pairs []string
	for _, name := range a.d.Cfg.General.Hints {
		for _, b := range all {
			if b.Action != name {
				continue
			}
			key := keymap.Display(b.Key)
			if !b.NoPrefix {
				key = pre + " " + key
			}
			pairs = append(pairs, key, hintLabel(b.Desc))
			break
		}
	}
	return pairs
}

// hintLabel shortens a binding description to a status-line label (the text before " (" or ",").
func hintLabel(desc string) string {
	if i := strings.IndexAny(desc, "(,"); i > 0 {
		desc = desc[:i]
	}
	return strings.TrimSpace(desc)
}

// ---- toggle_panels (zen) -----------------------------------------------------------------------

// togglePanels hides the sidebar, console and row detail (and, through App.zen, the status line); again restores
// what was shown.
func (w *Workspace) togglePanels() tea.Cmd {
	if !w.zen {
		w.zenSaved = [3]bool{w.showSB, w.showConsole, w.showDetail}
		w.showSB, w.showConsole, w.showDetail = false, false, false
		w.focusSB, w.focusDetail, w.focusConsole = false, false, false
		w.zen = true
		return nil
	}
	w.showSB, w.showConsole, w.showDetail = w.zenSaved[0], w.zenSaved[1], w.zenSaved[2]
	w.zen = false
	return nil
}

// ---- move_tab_left / move_tab_right ------------------------------------------------------------

// moveTab swaps the active tab with its neighbour inside its pane group (d is -1 or 1).
func (w *Workspace) moveTab(d int) tea.Cmd {
	idx := w.groupIdx(w.m3.pane)
	for p, i := range idx {
		if i != w.cur {
			continue
		}
		q := p + d
		if q < 0 || q >= len(idx) {
			return w.a.say("this tab is already the %s one", map[int]string{-1: "first", 1: "last"}[d])
		}
		j := idx[q]
		w.tabs[i], w.tabs[j] = w.tabs[j], w.tabs[i]
		w.cur = j
		return nil
	}
	return w.a.say("no tab to move")
}

// ---- production cue (M15.46) -------------------------------------------------------------------------

// cue is the colour that marks a production or staging workspace ("" = none): its pane borders take the tag colour,
// so the warning is on the whole screen, not only in the status line.
func (w *Workspace) cue() string {
	if t := w.conn.Tag; t == "production" || t == "staging" {
		if c := w.a.tagColor(w.conn); c != "" {
			return c
		}
		return w.a.th.pal["tag."+t]
	}
	return ""
}

// frame is frame() with the workspace's cue colour on the border.
func (w *Workspace) frame(th Theme, title, hint, content string, W, H int, focused bool) string {
	return frameCue(th, w.cue(), title, hint, content, W, H, focused)
}
