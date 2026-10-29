// layout.go: Layout: zen mode, hints, panels toggle and the frame.

package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"webtuidb/internal/keymap"
)

// termState is the App state for terminal focus (finish signals), the last key (passcode idle lock) and
// the mouse (drag in progress, double click detection).
type termState struct {
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

// frame is frame() with the workspace's cue colour on the border. It notes the focused frame's hints
// for the status line (focusHint).
func (w *Workspace) frame(th Theme, title, hint, content string, W, H int, focused bool) string {
	if focused && hint != "" && width(hint) <= W-4 { // frameEdge drops a hint that does not fit
		w.focusHint = squash(ansi.Strip(hint))
	}
	return frameCue(th, w.cue(), title, hint, content, W, H, focused)
}

// squash collapses runs of spaces to one, so rendered chips ("  C-d s   commit") compare as "C-d s commit".
func squash(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// focusLabel names the focused pane for the status line's pill ("" on the start page).
func (w *Workspace) focusLabel() string {
	switch {
	case w.focusDetail:
		return "ROW DETAILS"
	case w.focusSB && w.sb.tab == 1:
		return "SAVED QUERIES"
	case w.focusSB && w.sb.tab == 2:
		return "HISTORY"
	}
	switch w.context() {
	case keymap.Console:
		return "CONSOLE"
	case keymap.Sidebar:
		return "OBJECTS"
	case keymap.Grid:
		return "DATA"
	case keymap.Editor:
		return "EDITOR"
	case keymap.Results:
		return "RESULTS"
	case keymap.Structure:
		return "STRUCTURE"
	case keymap.FilterBar:
		return "FILTER"
	}
	return ""
}

// dropShown removes the key/label pairs the focused frame's border already shows; help always stays.
func dropShown(pairs []string, shown string) []string {
	if shown == "" {
		return pairs
	}
	shown = " " + shown + " "
	var out []string
	for i := 0; i+1 < len(pairs); i += 2 {
		last := i+2 >= len(pairs)
		if !last && strings.Contains(shown, " "+squash(keyGlyph(pairs[i])+" "+pairs[i+1])+" ") {
			continue
		}
		out = append(out, pairs[i], pairs[i+1])
	}
	return out
}
