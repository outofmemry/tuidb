package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
	"webtuidb/internal/keymap"
)

// structure grid columns
const (
	scNum = iota
	scName
	scType
	scNull
	scDefault
	scPK
	scExtra
	scFK
)

// index grid columns
const (
	icName = iota
	icUnique
	icCols
	icAlgo
	icCond
)

type colMeta struct{ orig string }

// loadStructure fetches everything the Structure and Index views need in parallel, reusing cached
// columns; the DDL (several catalog queries on some engines) arrives separately so it never
// holds up the structure grid.
func (t *tableTab) loadStructure() tea.Cmd {
	ws, ref, caps := t.ws, t.ref, t.ws.caps
	cached := ws.meta[ref]
	t.cancelStructure() // a newer load replaces the old one; its late reply is dropped by seq
	t.structSeq++
	seq := t.structSeq
	main, c1 := ws.asyncCancel(t.id, func(ctx context.Context, sess db.Session) tea.Msg {
		var (
			m  = structureLoadedMsg{seq: seq}
			mu sync.Mutex
			wg sync.WaitGroup
		)
		run := func(f func() error) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := f(); err != nil {
					mu.Lock()
					if m.err == nil {
						m.err = err
					}
					mu.Unlock()
				}
			}()
		}
		if cached != nil {
			m.cols = cached
		} else {
			run(func() (err error) { m.cols, err = sess.Columns(ctx, ref); return })
		}
		if caps.Indexes {
			run(func() (err error) { m.idxs, err = sess.Indexes(ctx, ref); return })
		}
		if caps.ForeignKeys {
			run(func() error { m.fks, _ = sess.ForeignKeys(ctx, ref); return nil })
		}
		if caps.Triggers {
			run(func() error { m.trgs, _ = sess.Triggers(ctx, ref); return nil })
		}
		wg.Wait()
		return m
	})
	ddl, c2 := ws.asyncCancel(t.id, func(ctx context.Context, sess db.Session) tea.Msg {
		d, err := sess.DDL(ctx, ref)
		return ddlLoadedMsg{seq: seq, ddl: d, err: err}
	})
	t.structCancel = []context.CancelFunc{c1, c2}
	return tea.Batch(main, ddl)
}

// composeDDL is the DDL view text: the loaded DDL plus the table's triggers.
func (t *tableTab) composeDDL() {
	t.ddl = t.ddlBase
	var extra []string
	for _, tr := range t.loaded.trgs {
		extra = append(extra, fmt.Sprintf("-- trigger %s: %s %s\n%s", tr.Name, tr.Timing, tr.Event, tr.Statement))
	}
	if len(extra) > 0 {
		t.ddl += "\n\n" + strings.Join(extra, "\n\n")
	}
}

func yesNo(b bool) string {
	if b {
		return "YES"
	}
	return "NO"
}

func fkText(fk db.ForeignKey, col string) string {
	ref := ""
	for i, c := range fk.Columns {
		if c == col && i < len(fk.RefColumns) {
			ref = fk.RefColumns[i]
		}
	}
	return fmt.Sprintf("→ %s(%s)  upd:%s del:%s", fk.RefTable, ref, orDefault(fk.OnUpdate, "NO ACTION"), orDefault(fk.OnDelete, "NO ACTION"))
}

// typeChoices are the common column types offered by the Set Value menu, per engine.
func typeChoices(caps db.Capabilities) []string {
	if len(caps.Types) > 0 {
		return caps.Types
	}
	switch caps.Dialect {
	case "mysql":
		return strings.Fields("int bigint smallint tinyint varchar(255) char(36) text longtext decimal(10,2) double float date datetime timestamp time boolean json blob")
	case "clickhouse":
		return strings.Fields("UInt8 UInt16 UInt32 UInt64 Int8 Int16 Int32 Int64 Float32 Float64 String Date DateTime Bool UUID Nullable(String) Array(String)")
	}
	return strings.Fields("integer bigint smallint serial bigserial text varchar(255) boolean numeric(10,2) real double\\ precision date time timestamp timestamptz uuid json jsonb bytea")
}

func (t *tableTab) buildStructure(m structureLoadedMsg) {
	t.loaded = m
	t.fkEdits = map[string]*db.ForeignKey{}
	fkOf := map[string]string{}
	for _, fk := range m.fks {
		for _, c := range fk.Columns {
			fkOf[c] = fkText(fk, c)
		}
	}
	pk := map[string]bool{}
	for _, c := range m.cols {
		pk[c.Name] = c.PK
	}
	var rows [][]any
	for i, c := range m.cols {
		var def any
		if c.HasDef || c.Default != "" {
			def = c.Default
		}
		var fk any
		if v, ok := fkOf[c.Name]; ok {
			fk = v
		}
		rows = append(rows, []any{int64(i + 1), c.Name, c.Type, yesNo(c.Nullable), def, yesNo(c.PK), c.Extra, fk})
	}
	sg := &Grid{Editable: !t.readOnlyKind()}
	sg.SetData([]gridCol{{Name: "#"}, {Name: "column_name"}, {Name: "data_type"}, {Name: "is_nullable"}, {Name: "column_default"}, {Name: "primary_key"}, {Name: "extra"}, {Name: "foreign_key"}}, rows)
	for i, c := range m.cols {
		sg.Rows[i].Meta = colMeta{orig: c.Name}
	}
	types := typeChoices(t.ws.caps)
	sg.NewRow = func() []any {
		return []any{int64(len(sg.Rows) + 1), "new_column", types[0], "YES", nil, "NO", "", nil}
	}
	t.structGrid = sg

	ig := &Grid{Editable: !t.readOnlyKind() && t.ws.caps.Indexes}
	var irows [][]any
	var metas []colMeta
	for _, ix := range m.idxs {
		if ix.Primary {
			continue // the primary key is edited with the primary_key column
		}
		irows = append(irows, []any{ix.Name, yesNo(ix.Unique), strings.Join(ix.Columns, ", "), ix.Algorithm, ix.Condition})
		metas = append(metas, colMeta{orig: ix.Name})
	}
	ig.SetData([]gridCol{{Name: "index_name"}, {Name: "is_unique"}, {Name: "column_name"}, {Name: "algorithm"}, {Name: "condition"}}, irows)
	for i := range metas {
		ig.Rows[i].Meta = metas[i]
	}
	ig.NewRow = func() []any {
		return []any{fmt.Sprintf("%s_idx%d", t.ref.Name, len(ig.Rows)+1), "NO", "", "", ""}
	}
	t.idxGrid = ig

	t.composeDDL()
	t.ddlTop = 0
}

// readOnlyKind: views and materialized views have no editable structure.
func (t *tableTab) readOnlyKind() bool { return t.ref.Kind != db.KindTable }

func (t *tableTab) structPending() bool {
	return (t.structGrid != nil && t.structGrid.HasPending()) || (t.idxGrid != nil && t.idxGrid.HasPending())
}

// ---- TableDef from the edited grids -----------------------------------------------------

func cellStr(v any) string {
	if v == nil || v == db.Null || v == db.Empty {
		return ""
	}
	return strings.TrimSpace(db.FormatValue(v))
}

func (t *tableTab) oldDef() *db.TableDef {
	if t.creating {
		return nil
	}
	m := t.loaded
	d := &db.TableDef{Schema: t.ref.Schema, Name: t.ref.Name}
	for _, c := range m.cols {
		c.OrigName = c.Name
		d.Columns = append(d.Columns, c)
		if c.PK {
			d.PrimaryKey = append(d.PrimaryKey, c.Name)
		}
	}
	for _, ix := range m.idxs {
		if !ix.Primary {
			d.Indexes = append(d.Indexes, ix)
		}
	}
	d.ForeignKeys = m.fks
	return d
}

func (t *tableTab) newDef() (*db.TableDef, error) {
	d := &db.TableDef{Schema: t.ref.Schema, Name: t.ref.Name}
	seen := map[string]bool{}
	live := map[string]bool{} // fkEdits keys (original column names) whose column still exists
	for _, r := range t.structGrid.Rows {
		if r.State == rowDeleted {
			continue
		}
		name, typ := cellStr(r.Cur[scName]), cellStr(r.Cur[scType])
		if name == "" || typ == "" {
			return nil, fmt.Errorf("every column needs a name and a data type")
		}
		if seen[strings.ToLower(name)] {
			return nil, fmt.Errorf("duplicate column name %q", name)
		}
		seen[strings.ToLower(name)] = true
		live[fkKey(r, name)] = true
		c := db.Column{Name: name, Type: typ, Nullable: cellStr(r.Cur[scNull]) == "YES", Extra: cellStr(r.Cur[scExtra])}
		if r.State != rowNew {
			if m, ok := r.Meta.(colMeta); ok {
				c.OrigName = m.orig
			}
		}
		if def, ok := r.Cur[scDefault].(string); ok && strings.TrimSpace(def) != "" {
			c.Default, c.HasDef = strings.TrimSpace(def), true
		}
		if cellStr(r.Cur[scPK]) == "YES" {
			c.PK = true
			d.PrimaryKey = append(d.PrimaryKey, name)
		}
		d.Columns = append(d.Columns, c)
	}
	if len(d.Columns) == 0 {
		return nil, fmt.Errorf("a table needs at least one column")
	}
	for _, r := range t.idxGrid.Rows {
		if r.State == rowDeleted {
			continue
		}
		name := cellStr(r.Cur[icName])
		var cols []string
		for _, c := range strings.Split(cellStr(r.Cur[icCols]), ",") {
			if c = strings.TrimSpace(c); c != "" {
				cols = append(cols, c)
			}
		}
		if name == "" || len(cols) == 0 {
			return nil, fmt.Errorf("every index needs a name and at least one column")
		}
		ix := db.Index{Name: name, Columns: cols, Unique: cellStr(r.Cur[icUnique]) == "YES"}
		if r.State != rowNew { // keep the attributes the grid does not edit
			ix.Algorithm, ix.Condition = cellStr(r.Cur[icAlgo]), cellStr(r.Cur[icCond])
		}
		d.Indexes = append(d.Indexes, ix)
	}
	// foreign keys: the loaded ones, with the per-column edits applied
	for _, fk := range t.loaded.fks {
		keep := true
		for _, c := range fk.Columns {
			if _, edited := t.fkEdits[c]; edited {
				keep = false
			}
		}
		if keep {
			d.ForeignKeys = append(d.ForeignKeys, fk)
		}
	}
	for k, fk := range t.fkEdits {
		if fk != nil && live[k] {
			d.ForeignKeys = append(d.ForeignKeys, *fk)
		}
	}
	return d, nil
}

func (t *tableTab) structStatements() ([]db.Statement, error) {
	nd, err := t.newDef()
	if err != nil {
		return nil, err
	}
	return t.ws.sess.PlanTable(t.oldDef(), nd)
}

func (t *tableTab) commitStructure() tea.Cmd {
	if !t.structPending() && !t.creating {
		return t.ws.a.say("no structure changes")
	}
	stmts, err := t.structStatements()
	if err != nil {
		return t.ws.a.fail(err)
	}
	if len(stmts) == 0 {
		return t.ws.a.say("no structure changes")
	}
	name := t.ref.Name
	return t.ws.execute(stmts, func(err error) tea.Cmd {
		if err != nil {
			return nil
		}
		t.creating = false
		t.cols, t.structLoaded, t.fkEdits = nil, false, map[string]*db.ForeignKey{}
		t.ws.forget(&t.ref)
		return tea.Batch(t.ws.sb.load(), t.loadStructure(), t.load(), t.ws.a.say("structure of %s saved", name))
	})
}

func (t *tableTab) discardStructure() {
	if t.creating {
		t.structGrid.DiscardPending()
		return
	}
	t.buildStructure(t.loaded)
}

// ---- views ------------------------------------------------------------------------------

func (t *tableTab) structView(th Theme, W, H int) string {
	if !t.structLoaded && !t.creating {
		return pad(th.Dim.Render("  loading structure…"), W, H)
	}
	if t.structErr != "" {
		return pad(th.Err.Render("  ✗ ")+t.structErr, W, H)
	}
	switch t.view {
	case viewStructure:
		pk := func(r *gridRow) bool { return cellStr(r.Cur[scPK]) == "YES" }
		if t.creating {
			H--
			head := fit(" "+th.OK.Render("+")+" "+th.Bold.Render("new table "+t.ref.Name)+
				th.Dim.Render("  ·  add columns with the add-row key, then commit to create it"), W)
			return head + "\n" + railView(th, t.structGrid, W, H, pk)
		}
		return railView(th, t.structGrid, W, H, pk)
	case viewIndex:
		if !t.ws.caps.Indexes {
			return pad(th.Dim.Render("  "+t.ws.conn.Driver+" has no Index tab"), W, H)
		}
		if len(t.idxGrid.Rows) == 0 {
			t.idxGrid.W, t.idxGrid.H = W, H
			return pad(th.Dim.Render("  no indexes  ·  add one with the add-row key"), W, H)
		}
		return railView(th, t.idxGrid, W, H, nil)
	}
	if t.ddl == "" {
		return pad(th.Dim.Render("  loading DDL…"), W, H)
	}
	lines := strings.Split(strings.TrimRight(t.ddl, "\n"), "\n")
	t.ddlTop = clamp(t.ddlTop, 0, max(0, len(lines)-H))
	return codeView(th, lines, lexLines(t.ws.caps.Dialect, lines), t.ddlTop, W, H)
}

// highlight colors one line using lexer (or jsonKinds) kinds, one Render call per run of equal kind.
func highlight(th Theme, line string, kinds []tokKind) string {
	var sb strings.Builder
	r := []rune(line)
	kind := func(i int) tokKind {
		if i < len(kinds) {
			return kinds[i]
		}
		return tkText
	}
	for i := 0; i < len(r); {
		j := i
		for j < len(r) && kind(j) == kind(i) {
			j++
		}
		st := th.Cell
		switch kind(i) {
		case tkKeyword, tkBool:
			st = th.Keyword
		case tkString:
			st = th.String
		case tkComment:
			st = th.Comment
		case tkNumber:
			st = th.Number
		case tkKey:
			st = th.Accent
		case tkPunct:
			st = th.Dim
		case tkNullLit:
			st = th.Null
		}
		sb.WriteString(st.Render(string(r[i:j])))
		i = j
	}
	return sb.String()
}

// codeView is a read-only code pane: an editor-style dim line-number gutter, then highlighted text.
func codeView(th Theme, lines []string, kinds [][]tokKind, top, W, H int) string {
	gw := max(3, len(fmt.Sprint(len(lines)))) + 1
	out := make([]string, H)
	for i := range out {
		if li := top + i; li < len(lines) {
			out[i] = th.Dim.Render(padLeft(fmt.Sprint(li+1), gw-1)+" ") + " " + highlight(th, lines[li], kinds[li])
		}
		out[i] = fit(out[i], W)
	}
	return strings.Join(out, "\n")
}

// railView draws g with a two-cell rail on its left: a symbol for each pending row (+ new,
// ✗ deleted, ● modified) and ⚷ on primary-key columns, so neither relies on the row tint alone.
func railView(th Theme, g *Grid, W, H int, pk func(r *gridRow) bool) string {
	g.W, g.H = max(0, W-2), H
	lines := strings.Split(g.View(th, true), "\n")
	for i, l := range lines {
		rail := th.HeaderCell.Render("  ")
		if ri := g.top + i - 1; i > 0 { // line 0 is the header; g.top is settled by View
			rail = "  "
			if ri < len(g.Rows) {
				r := g.Rows[ri]
				state, key := " ", " "
				switch {
				case r.State == rowNew:
					state = th.OK.Render("+")
				case r.State == rowDeleted:
					state = th.Err.Render("✗")
				case len(r.Dirty) > 0:
					state = th.Warn.Render("●")
				}
				if pk != nil && pk(r) {
					key = th.Warn.Render("⚷")
				}
				rail = state + key
			}
		}
		lines[i] = rail + l
	}
	return strings.Join(lines, "\n")
}

// ---- input ------------------------------------------------------------------------------

func (t *tableTab) editGrid() *Grid {
	if t.view == viewIndex {
		return t.idxGrid
	}
	return t.structGrid
}

// cellEditable says whether the column under the cursor takes free text (toggles and FKs are special).
func (t *tableTab) textCol(g *Grid) bool {
	if t.view == viewIndex {
		return g.col == icName || g.col == icCols
	}
	return g.col == scName || g.col == scType || g.col == scDefault
}

func (t *tableTab) structInput(msg tea.Msg) tea.Cmd {
	if t.view == viewDDL {
		return t.ddlInput(msg)
	}
	g := t.editGrid()
	if g == nil {
		return nil
	}
	if g.Editing() {
		if p, ok := msg.(tea.PasteMsg); ok {
			g.edit.Handle(p)
			return nil
		}
		k, ok := msg.(tea.KeyPressMsg)
		if !ok {
			return nil
		}
		switch k.Keystroke() {
		case "enter":
			g.FinishEdit(true)
		case "tab":
			g.FinishEdit(true)
			g.Move(0, 1, false)
		case "esc":
			g.FinishEdit(false)
		default:
			g.edit.Handle(k)
		}
		return nil
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	key := keymap.FromMsg(k)
	extend := strings.HasPrefix(key, "shift+")
	switch strings.TrimPrefix(key, "shift+") {
	case "down", "j":
		g.Move(1, 0, extend)
	case "up", "k":
		g.Move(-1, 0, extend)
	case "left", "h":
		g.Move(0, -1, extend)
	case "right", "l":
		g.Move(0, 1, extend)
	case "pgdown":
		g.Page(1, extend)
	case "pgup":
		g.Page(-1, extend)
	case "esc":
		g.ClearSelection()
	case "c":
		return t.copy(g.CopyCells(FmtPlain, t.ref.Name), "cells")
	case "enter", "e":
		return t.structActivate(g)
	case "i":
		return t.structAdd(g)
	case "delete", "backspace":
		g.DeleteRows()
	}
	return nil
}

func (t *tableTab) structAdd(g *Grid) tea.Cmd {
	if g == nil || !g.AddRow() {
		return t.ws.a.say("%s cannot be edited", t.ref.Name)
	}
	g.GoCol(1-boolInt(t.view == viewIndex), false)
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// structActivate is Enter on a structure/index cell: toggle YES/NO, open the FK form, or edit text.
func (t *tableTab) structActivate(g *Grid) tea.Cmd {
	if !g.Editable {
		return t.ws.a.say("%s cannot be edited", t.ref.Name)
	}
	if t.view == viewIndex {
		switch g.col {
		case icUnique:
			t.toggleYesNo(g)
		case icName, icCols:
			g.StartEdit()
		}
		return nil
	}
	switch g.col {
	case scNull, scPK:
		t.toggleYesNo(g)
	case scFK:
		return t.fkForm(g)
	case scName, scType, scDefault:
		g.StartEdit()
	}
	return nil
}

func (t *tableTab) toggleYesNo(g *Grid) {
	v, _ := g.CursorValue()
	if cellStr(v) == "YES" {
		g.SetCell("NO")
	} else {
		g.SetCell("YES")
	}
}

// structSetValue is the alt+enter menu on the structure grid.
func (t *tableTab) structSetValue() tea.Cmd {
	g := t.editGrid()
	if g == nil || !g.Editable {
		return t.ws.a.say("%s cannot be edited", t.ref.Name)
	}
	if t.view == viewStructure {
		switch g.col {
		case scType:
			var items []pickItem
			for _, ty := range typeChoices(t.ws.caps) {
				items = append(items, pickItem{Label: strings.ReplaceAll(ty, `\ `, " "), Value: strings.ReplaceAll(ty, `\ `, " ")})
			}
			t.ws.a.push(&pickerDlg{title: "Data type", filter: true, items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
				g.SetCell(it.Value.(string))
				return nil
			}})
			return nil
		case scDefault:
			now := t.ws.sess.Caps().Dialect
			_ = now
			items := []pickItem{{Label: "(none)", Value: nil}, {Label: "NULL", Value: "NULL"}, {Label: "CURRENT_TIMESTAMP", Value: "CURRENT_TIMESTAMP"},
				{Label: "0", Value: "0"}, {Label: "''  (empty string)", Value: "''"}, {Label: "true", Value: "true"}, {Label: "false", Value: "false"}}
			if t.ws.caps.Dialect == "postgres" {
				items = append(items, pickItem{Label: "gen_random_uuid()", Value: "gen_random_uuid()"}, pickItem{Label: "now()", Value: "now()"})
			}
			t.ws.a.push(&pickerDlg{title: "Default value", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
				g.SetCell(it.Value)
				return nil
			}})
			return nil
		case scNull, scPK:
			t.toggleYesNo(g)
			return nil
		case scFK:
			return t.fkForm(g)
		}
	}
	return t.structActivate(g)
}

func (t *tableTab) ddlInput(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch keymap.FromMsg(k) {
	case "down", "j":
		t.ddlTop++
	case "up", "k":
		t.ddlTop = max(0, t.ddlTop-1)
	case "pgdown", "space":
		t.ddlTop += 15
	case "pgup":
		t.ddlTop = max(0, t.ddlTop-15)
	case "g", "home":
		t.ddlTop = 0
	case "G", "end":
		t.ddlTop = 1 << 30
	case "c":
		return tea.Batch(tea.SetClipboard(t.ddl), t.ws.a.say("copied DDL"))
	case "e":
		if t.ref.Kind == db.KindView || t.ref.Kind == db.KindMatView {
			return t.ws.editDefinition(t.ref)
		}
		return t.ws.a.say("tables are changed in the Structure view; e edits views")
	}
	return nil
}

// fkKey is the key of a column's FK edit: its original name, so a rename keeps the edit attached.
func fkKey(r *gridRow, name string) string {
	if m, ok := r.Meta.(colMeta); ok && r.State != rowNew {
		return m.orig
	}
	return name
}

// fkForm edits the foreign key of the column under the cursor (create, change or delete).
func (t *tableTab) fkForm(g *Grid) tea.Cmd {
	r := g.cur()
	if r == nil {
		return nil
	}
	col := cellStr(r.Cur[scName])
	if col == "" {
		return nil
	}
	var cur *db.ForeignKey
	key := fkKey(r, col)
	if e, edited := t.fkEdits[key]; edited {
		cur = e
	} else {
		for i := range t.loaded.fks {
			for _, c := range t.loaded.fks[i].Columns {
				if c == key {
					cur = &t.loaded.fks[i]
				}
			}
		}
	}
	var tables []string
	for _, o := range t.ws.sb.objs {
		if o.Kind == db.KindTable {
			tables = append(tables, o.Name)
		}
	}
	if len(tables) == 0 {
		return t.ws.a.say("no tables to reference")
	}
	acts := []string{"NO ACTION", "RESTRICT", "CASCADE", "SET NULL", "SET DEFAULT"}
	v := map[string]string{"table": tables[0], "cols": "id", "upd": "NO ACTION", "del": "NO ACTION", "remove": "false"}
	if cur != nil {
		v["table"], v["cols"] = cur.RefTable, strings.Join(cur.RefColumns, ", ")
		v["upd"], v["del"] = orDefault(cur.OnUpdate, "NO ACTION"), orDefault(cur.OnDelete, "NO ACTION")
		if !contains(tables, cur.RefTable) {
			tables = append(tables, cur.RefTable)
		}
	}
	f := newForm("Foreign key on "+col, []formField{
		{Key: "table", Label: "Referenced table", Kind: fChoice, Value: v["table"], Options: tables},
		{Key: "cols", Label: "Referenced columns", Kind: fText, Value: v["cols"]},
		{Key: "upd", Label: "On update", Kind: fChoice, Value: v["upd"], Options: acts},
		{Key: "del", Label: "On delete", Kind: fChoice, Value: v["del"], Options: acts},
		{Key: "remove", Label: "Delete this key", Kind: fBool, Value: "false", Show: func(map[string]string) bool { return cur != nil }},
	}, func(vals map[string]string) tea.Cmd {
		if vals["remove"] == "true" {
			t.fkEdits[key] = nil
			g.SetCell(nil)
			return nil
		}
		var refCols []string
		for _, c := range strings.Split(vals["cols"], ",") {
			if c = strings.TrimSpace(c); c != "" {
				refCols = append(refCols, c)
			}
		}
		fk := &db.ForeignKey{Columns: []string{col}, RefSchema: t.ref.Schema, RefTable: vals["table"], RefColumns: refCols, OnUpdate: vals["upd"], OnDelete: vals["del"]}
		if cur != nil {
			fk.Name = cur.Name
			fk.Columns = cur.Columns
		}
		t.fkEdits[key] = fk
		g.SetCell(fkText(*fk, col))
		return nil
	})
	f.validate = func(vals map[string]string) error {
		if vals["remove"] != "true" && strings.TrimSpace(vals["cols"]) == "" {
			return fmt.Errorf("name the referenced column(s)")
		}
		return nil
	}
	t.ws.a.push(f)
	return nil
}

// ---- new table --------------------------------------------------------------------------

func (ws *Workspace) newTableDialog() tea.Cmd {
	if ws.caps.Editor != "sql" {
		return ws.a.say("%s has no table designer; use the editor to create keys/collections", ws.conn.Driver)
	}
	ws.a.push(newForm("New table", []formField{{Key: "name", Label: "Table name", Kind: fText}}, func(v map[string]string) tea.Cmd {
		name := strings.TrimSpace(v["name"])
		if name == "" {
			return ws.a.fail(fmt.Errorf("a table needs a name"))
		}
		ref := db.ObjRef{Schema: ws.sb.schema, Name: name, Kind: db.KindTable}
		t := newTableTab(ws, ws.newID(), ref)
		t.creating, t.structLoaded, t.view = true, true, viewStructure
		t.loaded = structureLoadedMsg{}
		t.buildStructure(t.loaded)
		t.structGrid.Editable = true
		t.structGrid.AddRow() // a first column to start from
		t.structGrid.Rows[0].Cur[scName], t.structGrid.Rows[0].Cur[scType], t.structGrid.Rows[0].Cur[scNull], t.structGrid.Rows[0].Cur[scPK] = "id", typeChoices(ws.caps)[0], "NO", "YES"
		return ws.addTab(t)
	}))
	return nil
}
