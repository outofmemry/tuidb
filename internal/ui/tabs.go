// tabs.go: The tab bar: titles, rename, close many, saving and restoring open tabs.

package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/store"
)

// tabTitle is the name in the tab bar; a tab renamed with rename_tab keeps its name.
func (w *Workspace) tabTitle(t tabber) string {
	if n := w.names[t.ID()]; n != "" {
		return n
	}
	return t.Title()
}

func (w *Workspace) tabAction(name string) tea.Cmd {
	switch name {
	case "clear_console":
		w.console.Clear()
		return w.a.say("console cleared")
	case "rename_tab":
		t := w.tab()
		if t == nil {
			return w.a.say("no tab to rename")
		}
		f := newForm("Rename tab", []formField{{Key: "name", Label: "Name (empty restores it)", Kind: fText, Value: w.tabTitle(t)}}, func(v map[string]string) tea.Cmd {
			if w.names == nil {
				w.names = map[int]string{}
			}
			if n := strings.TrimSpace(v["name"]); n != "" {
				w.names[t.ID()] = n
			} else {
				delete(w.names, t.ID())
			}
			return nil
		})
		w.a.push(f)
		return nil
	}
	cur := w.tab()
	if cur == nil {
		return w.a.say("no tabs open")
	}
	grp := w.pane(cur)
	switch name {
	case "close_other_tabs":
		return w.closeMany(func(i int) bool { return w.tabs[i] == cur || w.pane(w.tabs[i]) != grp })
	case "close_tabs_right":
		return w.closeMany(func(i int) bool { return i <= w.cur || w.pane(w.tabs[i]) != grp })
	}
	return w.closeMany(func(int) bool { return false }) // close_all_tabs
}

// closeMany closes every tab keep rejects, asking first when some of them have unsaved changes.
func (w *Workspace) closeMany(keep func(i int) bool) tea.Cmd {
	var drop []tabber
	dirty := 0
	for i, t := range w.tabs {
		if !keep(i) {
			drop = append(drop, t)
			if t.Dirty() {
				dirty++
			}
		}
	}
	if len(drop) == 0 {
		return w.a.say("no tabs to close")
	}
	cur := w.tab()
	do := func() tea.Cmd {
		for _, t := range drop {
			w.closeTab(indexOf(w.tabs, t))
		}
		if i := indexOf(w.tabs, cur); i >= 0 {
			w.cur = i
		}
		return w.a.say("closed %s", plural(len(drop), "tab"))
	}
	if dirty > 0 {
		w.a.push(&confirmDlg{title: "Close " + plural(dirty, "tab") + " with unsaved changes?", lines: []string{"Pending edits in them will be lost."}, yes: "Close", danger: true, onYes: do})
		return nil
	}
	return do()
}

// saveTabs stores the editor tabs for the next connect ([editor] auto_save_tabs = none | first | all).
func (w *Workspace) saveTabs() {
	mode := w.a.d.Cfg.Editor.AutoSaveTabs
	if mode != "first" && mode != "all" {
		return
	}
	var out []store.OpenTab
	for _, t := range w.tabs {
		q, ok := t.(*queryTab)
		if !ok || strings.TrimSpace(q.ed.Text()) == "" {
			continue
		}
		out = append(out, store.OpenTab{Kind: "sql", Name: w.names[q.id], Text: q.ed.Text(), SavedAs: q.savedAs})
		if mode == "first" {
			break
		}
	}
	if err := w.a.d.State.SetOpenTabs(w.conn.Name, out); err != nil {
		w.a.toast, w.a.toastErr = "could not remember the open tabs: "+err.Error(), true
	}
}

// restoreTabs reopens what saveTabs stored.
func (w *Workspace) restoreTabs() tea.Cmd {
	if m := w.a.d.Cfg.Editor.AutoSaveTabs; m != "first" && m != "all" {
		return nil
	}
	var cmds []tea.Cmd
	for _, ot := range w.a.d.State.OpenTabs(w.conn.Name) {
		if ot.Kind != "sql" {
			continue
		}
		cmds = append(cmds, w.newQueryTab(ot.Text))
		if ot.SavedAs != "" { // keep auto-saving into the saved query, unless it is gone or shared (read-only)
			if q, err := w.a.d.Queries.Read(ot.SavedAs); err == nil && !q.Shared {
				if t, ok := w.tab().(*queryTab); ok {
					t.savedAs, t.lastAuto = q.Path, ot.Text
				}
			}
		}
		if ot.Name != "" {
			if w.names == nil {
				w.names = map[int]string{}
			}
			w.names[w.tab().ID()] = ot.Name
		}
	}
	return tea.Batch(cmds...)
}

// moveTab swaps the active tab with its neighbour inside its pane group (d is -1 or 1).
func (w *Workspace) moveTab(d int) tea.Cmd {
	idx := w.groupIdx(w.panes.pane)
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
