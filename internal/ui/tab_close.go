// tab_close.go: Closing a tab: overlays, the slow-query prompt, unsaved file and the ask-to-save prompts.

package ui

import (
	"path/filepath"
	"slices"

	tea "charm.land/bubbletea/v2"
)

// removeOverlay takes o off the dialog stack wherever it is, and replays the results that waited behind it.
func (a *App) removeOverlay(o Overlay) tea.Cmd {
	i := slices.Index(a.overlays, o)
	if i < 0 {
		return nil
	}
	a.overlays = slices.Delete(a.overlays, i, i+1)
	if len(a.overlays) > 0 || len(a.deferred) == 0 {
		return nil
	}
	q := a.deferred
	a.deferred = nil
	var cmds []tea.Cmd
	for _, m := range q {
		cmds = append(cmds, func() tea.Msg { return m })
	}
	return tea.Batch(cmds...)
}

// closeSlowPrompt closes the tab's "taking too long" prompt: the batch finished or was cancelled.
func (t *queryTab) closeSlowPrompt() tea.Cmd {
	if t.slowDlg == nil {
		return nil
	}
	d := t.slowDlg
	t.slowDlg = nil
	return t.ws.a.removeOverlay(d)
}

// passesSlowPrompt: a statement result for the tab whose "taking too long" prompt is on top is handled at once
// instead of waiting behind the prompt, so the batch goes on, the time is the statement's own and the prompt
// closes when the batch is done.
func (a *App) passesSlowPrompt(m wsMsg) bool {
	if _, ok := m.msg.(queryDoneMsg); !ok || len(a.overlays) == 0 {
		return false
	}
	top, ok := a.overlays[len(a.overlays)-1].(*pickerDlg)
	w := a.wsByID(m.ws)
	if !ok || w == nil {
		return false
	}
	for _, t := range w.tabs {
		if q, ok := t.(*queryTab); ok && q.ID() == m.tab {
			return q.slowDlg == top
		}
	}
	return false
}

// fileDirty: the tab was opened from a .sql file and its text differs from the file.
// shortcut: with split_down the file is compared with the focused editor only.
func (t *queryTab) fileDirty() bool { return t.file != "" && t.ed.Text() != t.fileText }

// writeBack saves the editor to the file the tab came from.
func (t *queryTab) writeBack() error {
	text := t.ed.Text()
	if err := writeFile(t.file, text); err != nil {
		return err
	}
	t.fileText = text
	return nil
}

// askClose asks before a tab with something to lose closes: a modified file offers to save, an open transaction
// warns that it is rolled back, anything else is the generic unsaved-changes question. done closes the tab.
func (w *Workspace) askClose(t tabber, done func() tea.Cmd) {
	q, _ := t.(*queryTab)
	if q != nil && q.fileDirty() {
		q.askSave(func() tea.Cmd {
			if q.inTx {
				w.askClose(t, done)
				return nil
			}
			return done()
		})
		return
	}
	if q != nil && q.inTx {
		w.a.push(&confirmDlg{title: "This tab has an open transaction. Close it and roll back?", lines: []string{"Everything since BEGIN is rolled back. COMMIT first to keep it."},
			yes: "Roll back and close", danger: true, onYes: done})
		return
	}
	w.a.push(&confirmDlg{title: "Close " + t.Title() + " with unsaved changes?", lines: []string{"Pending edits in this tab will be lost."}, yes: "Close", danger: true, onYes: done})
}

// askSave offers Save / Don't save / Cancel for a modified file tab; then runs after Save or Don't save.
func (t *queryTab) askSave(then func() tea.Cmd) {
	name := filepath.Base(t.file)
	t.ws.a.push(&pickerDlg{title: "Save changes to " + name + "?", hint: "Enter: choose   Esc: cancel",
		items: []pickItem{{Label: "Save", Value: "save"}, {Label: "Don't save", Value: "discard"}, {Label: "Cancel", Value: "cancel"}},
		onPick: func(it pickItem, _ bool) tea.Cmd {
			switch it.Value.(string) {
			case "save":
				if err := t.writeBack(); err != nil {
					return t.ws.a.fail(err)
				}
			case "discard":
				t.fileText = t.ed.Text() // asked once: quitting does not ask again
			default:
				return nil
			}
			return then()
		}})
}

// paletteAction runs the actions that belong to no tab: palette entries and the ones with a hint.
func (w *Workspace) paletteAction(name string) (tea.Cmd, bool) {
	switch name {
	case "pin_connection":
		if q, ok := w.tab().(*queryTab); ok {
			return q.togglePin(), true
		}
		return w.a.say("open an SQL editor to pin its connection"), true
	case "explain":
		return w.a.say("explain works in an SQL editor"), true
	case "bulk_update":
		return w.a.say("bulk update works on a filtered table grid"), true
	case "connection_details":
		return w.connectionDetails(), true
	case "schema_diff":
		return w.schemaDiff(), true
	case "data_diff":
		return w.dataDiff(), true
	case "fake_rows":
		return w.fakeRows(), true
	}
	return nil, false
}
