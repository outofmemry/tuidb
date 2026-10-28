package ui

import (
	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/menus"
)

// The data grid, query result and filter bar context menus (M14 check 4). They are the same lists the web builds
// (menus.Grid, menus.Result, menus.FilterBar); every entry is a keymap action, so the choice runs through App.run.

// gridMenu is right click on a data tab cell: menus.Grid.
func (t *tableTab) gridMenu() tea.Cmd {
	t.ws.focusSB, t.ws.focusDetail = false, false
	t.ws.a.pushMenu("Cell menu", menus.Grid, menus.Env{Editable: t.grid.Editable}, func(it menus.Item) tea.Cmd { return t.ws.a.run(it.Action) })
	return nil
}

// filterBarMenu is right click on the filter bar: menus.FilterBar. Add filter opens the bar on a new row.
func (t *tableTab) filterBarMenu() tea.Cmd {
	t.ws.a.pushMenu("Filters", menus.FilterBar, menus.Env{}, func(it menus.Item) tea.Cmd {
		if it.Action == "filter_add" {
			if !t.showFilters {
				t.openFilters()
			}
			t.filters.Filters = append(t.filters.Filters, t.defaultFilter())
			t.fb.sel, t.fb.focus, t.fb.field = len(t.filters.Filters)-1, true, ffValue
			t.syncEdit()
			return nil
		}
		return t.ws.a.run(it.Action)
	})
	return nil
}

// resultMenu is right click on a query result: menus.Result.
func (t *queryTab) resultMenu() tea.Cmd {
	rv := t.curResult()
	if rv == nil {
		return t.ws.a.say("nothing here: run a query first")
	}
	t.focusResults = true
	editable := t.editableResult() != nil
	t.ws.a.pushMenu("Result menu", menus.Result, menus.Env{Editable: editable}, func(it menus.Item) tea.Cmd {
		switch it.Action {
		case "pin":
			rv.pinned = !rv.pinned
			return t.ws.a.say("%s %s", rv.title, map[bool]string{true: "pinned: it stays when you run again", false: "unpinned"}[rv.pinned])
		case "add_to_board":
			if rv.raw == nil {
				return t.ws.a.say("nothing to chart: this result has no rows")
			}
			return t.ws.addToBoard(rv)
		}
		return t.ws.a.run(it.Action)
	})
	return nil
}
