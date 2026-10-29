// structure_actions.go: Structure editor actions: moving columns, reordering, the row form.

package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// structReordered: the existing columns are no longer in the order they were loaded in.
func (t *tableTab) structReordered() bool {
	if t.structGrid == nil || t.creating {
		return false
	}
	var cur []string
	for _, r := range t.structGrid.Rows {
		if m, ok := r.Meta.(colMeta); ok && r.State != rowNew && r.State != rowDeleted {
			cur = append(cur, m.orig)
		}
	}
	i := 0
	for _, c := range t.loaded.cols {
		if i < len(cur) && cur[i] == c.Name {
			i++
		}
	}
	return i != len(cur) // loaded order, minus deleted columns, must contain cur in sequence
}

// moveColumn is alt+up / alt+down on the structure grid where the engine can place columns (MySQL, ClickHouse).
func (t *tableTab) moveColumn(g *Grid, d int) tea.Cmd {
	if t.view != viewStructure || !g.Editable {
		return nil
	}
	if !t.ws.caps.ReorderColumns {
		return t.ws.a.say("%s cannot reorder columns", t.ws.conn.Driver)
	}
	j := g.row + d
	if j < 0 || j >= len(g.Rows) {
		return nil
	}
	g.snapshot()
	g.Rows[g.row], g.Rows[j] = g.Rows[j], g.Rows[g.row]
	for _, i := range []int{g.row, j} { // the # column follows the position
		g.own(i).Cur[scNum] = int64(i + 1)
	}
	g.row = j
	g.scroll()
	return nil
}

// structRowForm is space on a structure row: name, type, null, default, primary key and extra in one dialog.
func (t *tableTab) structRowForm(g *Grid) tea.Cmd {
	if t.view != viewStructure || g.cur() == nil {
		return nil
	}
	if !g.Editable || g.cur().State == rowDeleted {
		return t.ws.a.say("%s cannot be edited", t.ref.Name)
	}
	row := g.row
	r := g.Rows[row]
	f := newForm("Column "+cellStr(r.Cur[scName]), []formField{
		{Key: "name", Label: "Name", Kind: fText, Value: cellStr(r.Cur[scName])},
		{Key: "type", Label: "Data type", Kind: fText, Value: cellStr(r.Cur[scType])},
		{Key: "null", Label: "Nullable", Kind: fBool, Value: fmt.Sprint(cellStr(r.Cur[scNull]) == "YES")},
		{Key: "default", Label: "Default", Kind: fText, Value: cellStr(r.Cur[scDefault]), Hint: "empty: none"},
		{Key: "pk", Label: "Primary key", Kind: fBool, Value: fmt.Sprint(cellStr(r.Cur[scPK]) == "YES")},
		{Key: "extra", Label: "Extra", Kind: fText, Value: cellStr(r.Cur[scExtra])},
	}, func(v map[string]string) tea.Cmd {
		if row >= len(g.Rows) {
			return nil
		}
		set := func(col int, val any) {
			if !sameValue(g.Rows[row].Cur[col], val) {
				g.row, g.col = row, col
				g.SetCell(val)
			}
		}
		var def any
		if d := strings.TrimSpace(v["default"]); d != "" {
			def = d
		}
		yn := func(b string) string { return map[string]string{"true": "YES", "false": "NO"}[b] }
		set(scName, strings.TrimSpace(v["name"]))
		set(scType, strings.TrimSpace(v["type"]))
		set(scNull, yn(v["null"]))
		set(scDefault, def)
		set(scPK, yn(v["pk"]))
		set(scExtra, strings.TrimSpace(v["extra"]))
		return nil
	})
	f.validate = func(v map[string]string) error {
		if strings.TrimSpace(v["name"]) == "" || strings.TrimSpace(v["type"]) == "" {
			return fmt.Errorf("a column needs a name and a data type")
		}
		return nil
	}
	f.hotHint = "foreign key: Enter on its cell"
	t.ws.a.push(f)
	return nil
}
