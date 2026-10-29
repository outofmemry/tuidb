// connections.go: Actions that work with or without a workspace, and closing other connections.

package ui

import (
	tea "charm.land/bubbletea/v2"
)

// runAppAction handles the actions that work with or without a workspace. It reports whether it took the action.
func (a *App) runAppAction(action string) (tea.Cmd, bool) {
	switch action {
	case "open_file":
		return a.openFile(), true
	case "open_recent":
		return a.openRecent(), true
	case "settings":
		return a.settingsDialog(), true
	case "close_other_connections":
		return a.closeOthers(), true
	}
	return nil, false
}

func (a *App) closeOthers() tea.Cmd {
	cur := a.ws()
	if cur == nil || len(a.wss) < 2 {
		return a.say("no other connection is open")
	}
	dirty, applying := 0, 0
	for _, w := range a.wss {
		if w == cur {
			continue
		}
		if w.applying { // closing would cancel a commit half way
			applying++
		} else if w.dirty() {
			dirty++
		}
	}
	if applying > 0 {
		return a.say("%d connection(s) still sending a change: wait for it to finish before closing the others", applying)
	}
	do := func() tea.Cmd {
		n := len(a.wss) - 1
		for _, w := range a.wss {
			if w != cur {
				w.close()
			}
		}
		a.wss, a.active = []*Workspace{cur}, 0
		return a.say("closed %s", plural(n, "connection"))
	}
	if dirty > 0 {
		a.push(&confirmDlg{title: "Close the other connections?", lines: []string{plural(dirty, "of them") + " have unsaved changes that will be lost."}, yes: "Close", danger: true, onYes: do})
		return nil
	}
	a.push(&confirmDlg{title: "Close the other connections?", lines: []string{"Keeps " + cur.conn.Name + " and closes the " + plural(len(a.wss)-1, "other") + "."}, yes: "Close", onYes: do})
	return nil
}
