package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
	"webtuidb/internal/keymap"
)

// fields of one filter row
const (
	ffCheck = iota
	ffColumn
	ffOp
	ffValue
)

type filterBar struct {
	focus bool
	sel   int // filter row
	field int
	edit  lineEdit // value editor of the selected row
}

func (t *tableTab) defaultFilter() db.Filter {
	c := t.ws.a.d.Cfg.Table
	f := db.Filter{Op: c.FilterDefaultOperator, Enabled: true}
	if f.Op == "contains" {
		f.Op = "Contains"
	}
	switch c.FilterDefaultColumn {
	case "raw":
		f.Column = db.RawColumn
	case "any":
		f.Column = db.AnyColumn
	default:
		f.Column = db.AnyColumn
		for _, col := range t.cols {
			if col.PK {
				f.Column = col.Name
				break
			}
		}
	}
	return f
}

func (t *tableTab) filterHeight() int {
	if !t.showFilters {
		return 0
	}
	return min(len(t.filters.Filters), 5) + 1
}

// openFilters shows the bar (adding a first filter if needed) and focuses its value field.
func (t *tableTab) openFilters() {
	t.showFilters = true
	if len(t.filters.Filters) == 0 {
		t.filters.Filters = append(t.filters.Filters, t.defaultFilter())
	}
	t.fb.focus, t.fb.sel, t.fb.field = true, clamp(t.fb.sel, 0, len(t.filters.Filters)-1), ffValue
	t.syncEdit()
}

func (t *tableTab) syncEdit() {
	if t.fb.sel < len(t.filters.Filters) {
		t.fb.edit = newLine(t.filters.Filters[t.fb.sel].Value)
	}
}

func (t *tableTab) applyFilters() tea.Cmd {
	return t.guard(func() tea.Cmd {
		t.markApplied()
		t.offset, t.toTop = 0, true
		t.saveState()
		return t.load()
	})
}

func (t *tableTab) columnPicker(onPick func(col string)) {
	items := []pickItem{{Label: "Any column", Value: db.AnyColumn}, {Label: "Raw SQL", Value: db.RawColumn}}
	for _, c := range t.filterColumns() {
		items = append(items, pickItem{Label: c.Name, Detail: c.Type, Value: c.Name})
	}
	t.ws.a.push(&pickerDlg{title: "Filter column", filter: true, items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		onPick(it.Value.(string))
		return nil
	}})
}

func (t *tableTab) operatorPicker(onPick func(op string)) {
	var items []pickItem
	for _, op := range db.Operators(t.ws.caps.Dialect) {
		items = append(items, pickItem{Label: op, Value: op})
	}
	t.ws.a.push(&pickerDlg{title: "Filter operator", filter: true, items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		onPick(it.Value.(string))
		return nil
	}})
}

func (t *tableTab) filterInput(msg tea.Msg) tea.Cmd {
	fb := &t.fb
	fs := &t.filters.Filters
	if len(*fs) == 0 {
		fb.focus, t.showFilters = false, false
		return nil
	}
	fb.sel = clamp(fb.sel, 0, len(*fs)-1)
	f := &(*fs)[fb.sel]
	if p, ok := msg.(tea.PasteMsg); ok && fb.field == ffValue {
		fb.edit.Handle(p)
		f.Value = fb.edit.Value()
		return nil
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch ks := keymap.FromMsg(k); ks {
	case "esc":
		fb.focus, t.showFilters = false, false
		t.saveState()
		return nil
	case "enter":
		switch fb.field {
		case ffColumn:
			t.columnPicker(func(c string) { f.Column = c })
		case ffOp:
			t.operatorPicker(func(o string) { f.Op = o })
		default:
			return t.applyFilters()
		}
	case "tab":
		fb.field = (fb.field + 1) % 4
		if fb.field == ffValue {
			t.syncEdit()
		}
	case "shift+tab":
		fb.field = (fb.field + 3) % 4
		if fb.field == ffValue {
			t.syncEdit()
		}
	case "down":
		fb.sel = min(fb.sel+1, len(*fs)-1)
		t.syncEdit()
	case "up":
		fb.sel = max(fb.sel-1, 0)
		t.syncEdit()
	case "alt+n": // add a filter as a copy of the current one
		nf := *f
		nf.Value = ""
		*fs = append((*fs)[:fb.sel+1], append([]db.Filter{nf}, (*fs)[fb.sel+1:]...)...)
		fb.sel++
		fb.field = ffValue
		t.syncEdit()
	case "alt+d", "alt+x":
		*fs = append((*fs)[:fb.sel], (*fs)[fb.sel+1:]...)
		if len(*fs) == 0 {
			fb.focus, t.showFilters = false, false
			return t.applyFilters()
		}
		fb.sel = clamp(fb.sel, 0, len(*fs)-1)
		t.syncEdit()
	case "alt+o":
		t.filters.Or = !t.filters.Or
	case "alt+c":
		t.filters.Filters = nil
		fb.focus, t.showFilters = false, false
		return t.applyFilters()
	case "alt+s":
		return t.showFilterSQL()
	default:
		switch fb.field {
		case ffCheck:
			if ks == "space" {
				f.Enabled = !f.Enabled
			}
		case ffColumn:
			if ks == "space" || ks == "right" || ks == "left" {
				t.columnPicker(func(c string) { f.Column = c })
			}
		case ffOp:
			if ks == "space" || ks == "right" || ks == "left" {
				t.operatorPicker(func(o string) { f.Op = o })
			}
		case ffValue:
			if fb.edit.Handle(k) {
				f.Value = fb.edit.Value()
			}
		}
	}
	return nil
}

// inlineArgs substitutes placeholders left to right, so "?" dialects keep their argument order.
func inlineArgs(d db.Dialect, sql string, args []any) string {
	pos := 0
	for i, a := range args {
		p := d.Param(i + 1)
		j := strings.Index(sql[pos:], p)
		if j < 0 {
			break
		}
		lit := d.Literal(a)
		sql = sql[:pos+j] + lit + sql[pos+j+len(p):]
		pos += j + len(lit)
	}
	return sql
}

func (t *tableTab) showFilterSQL() tea.Cmd {
	d, ok := db.DialectFor(t.ws.caps.Dialect)
	if !ok {
		return t.ws.a.say("filter SQL is only shown for SQL engines")
	}
	var names []string
	for _, c := range t.cols {
		names = append(names, c.Name)
	}
	sql, err := db.WhereText(d, t.filters, names)
	if err != nil {
		return t.ws.a.fail(err)
	}
	lines := []string{"WHERE " + sql}
	if sql == "" {
		lines = []string{"(no active filters)"}
	}
	t.ws.a.push(&textViewDlg{title: "Filter SQL", lines: lines, actions: []viewAction{{Key: "c", Label: "c: copy", Do: func() tea.Cmd { return setClip(strings.Join(lines, "\n")) }}}})
	return nil
}

func (t *tableTab) filterView(th Theme, W int) []string {
	if !t.showFilters {
		return nil
	}
	join := "AND"
	if t.filters.Or {
		join = "OR"
	}
	head := " " + th.Section.Render("Filters ("+join+")") + "   " + th.chips("↵", "apply", "Tab", "field", "M-n", "add", "M-d", "remove", "M-o", "and/or", "M-c", "clear", "M-s", "sql", "Esc", "hide")
	lines := []string{fit(head, W)}
	fs := t.filters.Filters
	start := 0
	if t.fb.sel >= 5 {
		start = t.fb.sel - 4
	}
	for i := start; i < min(len(fs), start+5); i++ {
		f := fs[i]
		col := f.Column
		switch col {
		case db.AnyColumn:
			col = "Any column"
		case db.RawColumn:
			col = "Raw SQL"
		}
		chk := " ○ " // off: the row is kept but not applied
		if f.Enabled {
			chk = " ✓ "
		}
		focus := func(field int, s string) string { // dropdowns look like buttons, the focused one selected
			if t.fb.focus && i == t.fb.sel && t.fb.field == field {
				return th.SelCell.Render(s)
			}
			return th.TabIdle.Render(s)
		}
		val := f.Value
		valW := max(8, W-width(col)-width(f.Op)-16)
		var valStr string
		switch {
		case t.fb.focus && i == t.fb.sel && t.fb.field == ffValue:
			valStr = t.fb.edit.View(valW, th, true)
		case val == "":
			valStr = th.Dim.Render(fit("value…", valW))
		case !f.Enabled:
			valStr = th.Dim.Strikethrough(true).Render(fit(val, valW))
		default:
			valStr = fit(val, valW)
		}
		if f.Op == "IS NULL" || f.Op == "IS NOT NULL" {
			valStr = th.Dim.Render(fit("(no value)", valW))
		}
		mark := "  "
		if i == t.fb.sel && t.fb.focus {
			mark = th.Accent.Render("▸") + " "
		}
		line := mark + focus(ffCheck, chk) + " " + focus(ffColumn, " "+col+" ▾ ") + " " + focus(ffOp, " "+f.Op+" ▾ ") + " " + valStr
		lines = append(lines, fit(line, W))
	}
	return lines
}

// quickFilter filters on the value under the cursor.
func (t *tableTab) quickFilter() tea.Cmd {
	g := t.grid
	v, ok := g.CursorValue()
	if !ok || g.col >= len(g.Cols) {
		return nil
	}
	col := g.Cols[g.col].Name
	val := plain(v)
	type qf struct{ op, val string }
	opts := []pickItem{{Label: "= " + val, Value: qf{"=", val}}, {Label: "<> " + val, Value: qf{"<>", val}},
		{Label: "Contains " + val, Value: qf{"Contains", val}}, {Label: "Has prefix " + val, Value: qf{"Has prefix", val}},
		{Label: "IN (" + val + ")", Value: qf{"IN", val}}, {Label: "IS NULL", Value: qf{"IS NULL", ""}}, {Label: "IS NOT NULL", Value: qf{"IS NOT NULL", ""}}}
	if v == nil {
		opts = opts[5:]
	}
	t.ws.a.push(&pickerDlg{title: "Quick filter: " + col, items: opts, onPick: func(it pickItem, _ bool) tea.Cmd {
		q := it.Value.(qf)
		t.filters.Filters = []db.Filter{{Column: col, Op: q.op, Value: q.val, Enabled: true}}
		t.showFilters, t.fb.focus, t.fb.sel = true, false, 0
		return t.applyFilters()
	}})
	return nil
}

func (t *tableTab) columnsPicker() tea.Cmd {
	if len(t.cols) == 0 {
		return nil
	}
	items := make([]checkItem, len(t.cols))
	for i, c := range t.cols {
		items[i] = checkItem{Label: c.Name, Checked: !t.hidden[c.Name]}
	}
	t.ws.a.push(&checklistDlg{title: "Visible columns", items: items, onOK: func(items []checkItem) tea.Cmd {
		visible := 0
		for _, it := range items {
			if it.Checked {
				visible++
			}
		}
		if visible == 0 {
			return t.ws.a.fail(fmt.Errorf("at least one column must stay visible"))
		}
		return t.guard(func() tea.Cmd {
			t.hidden = map[string]bool{}
			for _, it := range items {
				if !it.Checked {
					t.hidden[it.Label] = true
				}
			}
			t.saveState()
			return t.load()
		})
	}})
	return nil
}

// followFK opens the table the foreign key under the cursor points to, filtered to the referenced row.
func (t *tableTab) followFK() tea.Cmd {
	g := t.grid
	v, ok := g.CursorValue()
	if !ok || g.col >= len(g.Cols) || v == nil {
		return t.ws.a.say("nothing to follow here")
	}
	name := g.Cols[g.col].Name
	for _, c := range t.cols {
		if c.Name != name || c.FK == nil || len(c.FK.RefColumns) == 0 {
			continue
		}
		fk := c.FK
		idx := 0
		for i, col := range fk.Columns {
			if col == name {
				idx = i
			}
		}
		ref := db.ObjRef{Schema: fk.RefSchema, Name: fk.RefTable, Kind: db.KindTable}
		nt := newTableTab(t.ws, t.ws.newID(), ref)
		nt.filters = db.FilterSet{Filters: []db.Filter{{Column: fk.RefColumns[min(idx, len(fk.RefColumns)-1)], Op: "=", Value: plain(v), Enabled: true}}}
		nt.showFilters = true
		return t.ws.addTab(nt)
	}
	return t.ws.a.say("%s is not a foreign key", name)
}
