// grid_sort.go: Sorting, hiding and filtering columns of a table grid.

package ui

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

// sortColumns is `sort_columns`: tick the columns, then pick asc / desc for each; the ticked order is the sort priority.
func (t *tableTab) sortColumns() tea.Cmd {
	if len(t.cols) == 0 {
		return t.ws.a.say("the columns are still loading")
	}
	var items []checkItem
	desc := map[string]bool{}
	seen := map[string]bool{}
	have := map[string]bool{}
	for _, c := range t.cols {
		have[c.Name] = true
	}
	for _, s := range t.sort { // the current sort keeps its priority at the top
		if !have[s.Column] { // the column was renamed or dropped since it was saved
			continue
		}
		items = append(items, checkItem{Label: s.Column, Checked: true})
		desc[s.Column], seen[s.Column] = s.Desc, true
	}
	for _, c := range t.cols {
		if !seen[c.Name] {
			items = append(items, checkItem{Label: c.Name})
		}
	}
	t.ws.a.push(&checklistDlg{title: "Sort by (ticked columns, in this order)", items: items, onOK: func(items []checkItem) tea.Cmd {
		var ticked []checkItem
		for _, it := range items {
			if it.Checked {
				ticked = append(ticked, it)
			}
		}
		sort.SliceStable(ticked, func(i, j int) bool { return ticked[i].Seq < ticked[j].Seq }) // the order they were ticked in
		var picked []string
		for _, it := range ticked {
			picked = append(picked, it.Label)
		}
		apply := func(sort []db.Sort) tea.Cmd {
			return t.guard(func() tea.Cmd {
				t.sort, t.offset, t.toTop = sort, 0, true
				t.saveState()
				return t.load()
			})
		}
		if len(picked) == 0 {
			return apply(nil)
		}
		var fields []formField
		for _, c := range picked {
			v := "asc"
			if desc[c] {
				v = "desc"
			}
			fields = append(fields, formField{Key: c, Label: c, Kind: fChoice, Options: []string{"asc", "desc"}, Value: v})
		}
		t.ws.a.push(newForm("Direction", fields, func(v map[string]string) tea.Cmd {
			var out []db.Sort
			for _, c := range picked {
				out = append(out, db.Sort{Column: c, Desc: v[c] == "desc"})
			}
			return apply(out)
		}))
		return nil
	}})
	return nil
}

func (t *tableTab) hideColumn() tea.Cmd {
	g := t.grid
	if g.col >= len(g.Cols) || len(t.cols) == 0 {
		return t.ws.a.say("no column to hide")
	}
	name := g.Cols[g.col].Name
	if len(t.cols)-t.hiddenCount() <= 1 {
		return t.ws.a.say("at least one column must stay visible")
	}
	return t.guard(func() tea.Cmd {
		t.hidden[name] = true
		t.saveState()
		return tea.Batch(t.load(), t.ws.a.say("hid %s (show all columns brings it back)", name))
	})
}

func (t *tableTab) showAllColumns() tea.Cmd {
	if len(t.hidden) == 0 {
		return t.ws.a.say("every column is already shown")
	}
	return t.guard(func() tea.Cmd {
		t.hidden = map[string]bool{}
		t.saveState()
		return t.load()
	})
}

// filterColumns is the column list of the filter bar in table.filter_column_sort order.
func (t *tableTab) filterColumns() []db.Column {
	cols := append([]db.Column(nil), t.cols...)
	if t.ws.a.d.Cfg.Table.FilterColumnSort == "name" {
		sort.SliceStable(cols, func(i, j int) bool { return strings.ToLower(cols[i].Name) < strings.ToLower(cols[j].Name) })
	}
	return cols
}
