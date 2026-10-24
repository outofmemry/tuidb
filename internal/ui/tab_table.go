package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"tuidb/internal/db"
	"tuidb/internal/keymap"
	"tuidb/internal/store"
)

const (
	viewData = iota
	viewStructure
	viewIndex
	viewDDL
	viewJSON // document engines only
	viewTree
)

var viewNames = map[int]string{viewData: "Data", viewStructure: "Structure", viewIndex: "Index", viewDDL: "DDL", viewJSON: "Json", viewTree: "Tree"}

// viewList is the footer order; Json and Tree exist only for document engines.
func (t *tableTab) viewList() []int {
	v := []int{viewData, viewStructure, viewIndex, viewDDL}
	if t.ws.caps.Document {
		v = append(v, viewJSON, viewTree)
	}
	return v
}

type tableTab struct {
	ws   *Workspace
	id   int
	ref  db.ObjRef
	view int

	grid        *Grid
	cols        []db.Column
	filters     db.FilterSet
	showFilters bool
	hidden      map[string]bool
	sort        []db.Sort
	limit       int
	offset      int
	count       db.Count
	counted     bool
	loading     bool
	err         string
	seq         int
	loadedRows  int
	paste       string // "rows" or "cells" while the clipboard read is pending
	findText    string
	cancels     []context.CancelFunc // stop every in-flight load: rows, count and columns
	committing  bool                 // a commit was sent and its reload has not landed yet
	closed      bool
	wantPKSort  bool // table.default_sort = pk_asc / pk_desc: applied once the key is known
	pkDesc      bool
	waitCols    bool // rows are waiting for the column metadata (the query needs the names)
	resCols     []db.ResultColumn
	fb          filterBar
	toTop       bool // next load is a different query: put the cursor back on row 1
	took        time.Duration

	// structure / index / ddl views (read-only until stage B)
	structGrid, idxGrid *Grid
	ddl                 string
	structLoaded        bool
	structErr           string
	ddlTop              int
	ddlBase             string // DDL as loaded; triggers are appended for display
	loaded              structureLoadedMsg
	structSeq           int
	structCancel        []context.CancelFunc
	fkEdits             map[string]*db.ForeignKey
	creating            bool
	docs                []string
	jsonCache           struct {
		docs  []string
		lines []string
		kinds [][]tokKind
	}
	tree    *docTree
	jsonTop int
}

func newTableTab(ws *Workspace, id int, ref db.ObjRef) *tableTab {
	t := &tableTab{ws: ws, id: id, ref: ref, grid: &Grid{}, limit: ws.a.d.Cfg.General.RowLimit, hidden: map[string]bool{}}
	t.grid.Editable = ref.Kind == db.KindTable
	t.grid.NoStripes = !ws.a.d.Cfg.Table.AlternatingRows
	cfg := ws.a.d.Cfg.Table
	if ts := ws.a.d.State.Table(t.stateKey()); ts.Sort != nil || len(ts.Filters.Filters) > 0 || len(ts.Hidden) > 0 {
		t.sort, t.filters, t.showFilters = ts.Sort, ts.Filters, ts.ShowBar
		for _, h := range ts.Hidden {
			t.hidden[h] = true
		}
	} else if cfg.DefaultSort == "pk_asc" || cfg.DefaultSort == "pk_desc" {
		t.wantPKSort, t.pkDesc = true, cfg.DefaultSort == "pk_desc"
	}
	switch cfg.FilterState { // "remember" keeps what the state file said
	case "show":
		t.showFilters = true
	case "hide":
		t.showFilters = false
	}
	return t
}

func (ws *Workspace) openStructure(ref db.ObjRef) tea.Cmd {
	cmd := ws.openObject(ref, false)
	if t, ok := ws.tab().(*tableTab); ok && t.ref == ref {
		return tea.Batch(cmd, t.setView(viewStructure))
	}
	return cmd
}

func (t *tableTab) stateKey() string {
	return store.TableKey(t.ws.conn.Name, t.ws.sess.CurrentDatabase(), t.ref.Schema+"."+t.ref.Name)
}

func (t *tableTab) saveState() {
	var hidden []string
	for h := range t.hidden {
		hidden = append(hidden, h)
	}
	t.ws.a.d.State.SetTable(t.stateKey(), store.TableState{Hidden: hidden, Filters: t.filters, ShowBar: t.showFilters, Sort: t.sort})
}

func (t *tableTab) ID() int       { return t.id }
func (t *tableTab) Dirty() bool   { return t.grid.HasPending() || t.structPending() || t.creating }
func (t *tableTab) Title() string { return t.ref.Name }

func (t *tableTab) Context() string {
	if t.view == viewData && t.showFilters && t.fb.focus {
		return keymap.FilterBar
	}
	switch t.view {
	case viewData:
		return keymap.Grid
	case viewDDL:
		return keymap.Structure
	}
	return keymap.Structure
}

func (t *tableTab) Init() tea.Cmd {
	if t.creating {
		return nil // nothing to browse until the table exists
	}
	return t.load()
}

// ---- loading ----------------------------------------------------------------------------

type tableLoadedMsg struct {
	seq  int
	res  *db.Result
	err  error
	took time.Duration
}

type tableCountMsg struct {
	seq int
	n   db.Count
	err error
}

type structureLoadedMsg struct {
	seq  int
	cols []db.Column
	idxs []db.Index
	fks  []db.ForeignKey
	trgs []db.Trigger
	err  error
}

type ddlLoadedMsg struct {
	seq int
	ddl string
	err error
}

func (t *tableTab) visibleCols() []string {
	if len(t.hidden) == 0 || len(t.cols) == 0 {
		return nil
	}
	var out []string
	for _, c := range t.cols {
		if !t.hidden[c.Name] {
			out = append(out, c.Name)
		}
	}
	return out
}

func (t *tableTab) query() db.RowQuery {
	return db.RowQuery{ObjRef: t.ref, Filters: t.filters, Sort: t.sort, Limit: t.limit, Offset: t.offset, Columns: t.visibleCols()}
}

// load fetches rows, count and (unless cached) column metadata in parallel: a remote server costs
// one round trip of wall time instead of four in a row. Rows only wait for the columns when the
// query itself needs their names (hidden columns, an Any-column filter).
func (t *tableTab) load() tea.Cmd {
	t.cancelLoads() // one load at a time: holding the sort or page key must not pile up queries
	t.loading, t.err, t.counted = true, "", false
	t.grid.Editable = false // the rows are about to be replaced; typing into them would be lost
	t.seq++
	if t.cols == nil {
		t.cols = t.ws.meta[t.ref]
	}
	if t.cols != nil {
		t.applyDefaultSort()
		return tea.Batch(t.loadRows(), t.loadCount())
	}
	if t.needColsFirst() || t.wantPKSort {
		t.waitCols = true
		return t.loadCols()
	}
	return tea.Batch(t.loadCols(), t.loadRows(), t.loadCount())
}

// applyDefaultSort turns table.default_sort into a real sort once the primary key is known.
func (t *tableTab) applyDefaultSort() {
	if !t.wantPKSort || t.cols == nil {
		return
	}
	t.wantPKSort = false
	for _, c := range t.cols {
		if c.PK {
			t.sort = append(t.sort, db.Sort{Column: c.Name, Desc: t.pkDesc})
		}
	}
}

func (t *tableTab) needColsFirst() bool {
	if len(t.hidden) > 0 {
		return true
	}
	for _, f := range t.filters.Active() {
		if f.Column == db.AnyColumn {
			return true
		}
	}
	return false
}

func (t *tableTab) loadCols() tea.Cmd {
	ref, caps, seq := t.ref, t.ws.caps, t.seq
	cmd, cancel := t.ws.asyncCancel(t.id, func(ctx context.Context, sess db.Session) tea.Msg {
		cols, err := fetchColumns(ctx, sess, caps, ref)
		return colsMetaMsg{ref: ref, cols: cols, err: err, seq: seq}
	})
	t.cancels = append(t.cancels, cancel)
	return cmd
}

func (t *tableTab) loadRows() tea.Cmd {
	seq, q := t.seq, t.query()
	cmd, cancel := t.ws.asyncCancel(t.id, func(ctx context.Context, sess db.Session) tea.Msg {
		start := time.Now()
		res, err := sess.Rows(ctx, q)
		return tableLoadedMsg{seq: seq, res: res, err: err, took: time.Since(start)}
	})
	t.cancels = append(t.cancels, cancel)
	return cmd
}

func (t *tableTab) cancelStructure() {
	for _, c := range t.structCancel {
		c()
	}
	t.structCancel = nil
}

func (t *tableTab) cancelLoads() {
	for _, c := range t.cancels {
		c()
	}
	t.cancels = nil
}

// Close stops whatever the tab is loading; results for it are dropped by the tab lookup.
func (t *tableTab) Close() {
	t.closed = true
	t.cancelLoads()
	t.cancelStructure()
}

func (t *tableTab) Busy() bool { return t.loading }

// CancelWork stops a running load (prefix . from any pane).
func (t *tableTab) CancelWork() tea.Cmd {
	if !t.loading || len(t.cancels) == 0 {
		return nil
	}
	t.cancelLoads()
	t.loading, t.committing, t.seq = false, false, t.seq+1
	return t.ws.a.say("load of %s cancelled", t.ref.Name)
}

func (t *tableTab) loadCount() tea.Cmd { return t.countCmd(len(t.filters.Active()) == 0) }

// loadCountExact replaces an estimated row count with a real COUNT.
func (t *tableTab) loadCountExact() tea.Cmd {
	if t.counted && t.count.Exact {
		return t.ws.a.say("the count is already exact: %d", t.count.N)
	}
	return tea.Batch(t.countCmd(false), t.ws.a.say("counting rows…"))
}

func (t *tableTab) countCmd(estimate bool) tea.Cmd {
	seq, q := t.seq, t.query()
	q.Limit, q.Offset, q.Sort = 0, 0, nil
	cmd, cancel := t.ws.asyncCancel(t.id, func(ctx context.Context, sess db.Session) tea.Msg {
		n, err := sess.Count(ctx, q, estimate)
		return tableCountMsg{seq: seq, n: n, err: err}
	})
	t.cancels = append(t.cancels, cancel)
	return cmd
}

func (t *tableTab) Handle(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case colsMetaMsg:
		if m.err != nil {
			if m.seq == t.seq && (t.loading || t.waitCols) {
				t.loading, t.waitCols, t.err = false, false, m.err.Error()
			}
			return nil
		}
		t.cols = m.cols
		t.applyDefaultSort()
		if t.waitCols && m.seq == t.seq {
			t.waitCols = false
			return tea.Batch(t.loadRows(), t.loadCount())
		}
		t.applyCols()
	case tableLoadedMsg:
		if m.seq != t.seq {
			return nil
		}
		t.loading, t.committing = false, false
		if m.err != nil {
			t.err = m.err.Error()
			if len(t.filters.Active()) > 0 {
				t.showFilters = true // a saved filter is the likely cause: make it visible so it can be fixed or cleared
			}
			t.ws.log(fmt.Sprintf("-- browse %s: %v", objKey(t.ref), m.err), m.took, m.err)
			return nil
		}
		t.took = m.took
		t.ws.log(t.describeQuery(), m.took, nil)
		t.fill(m.res)
	case tableCountMsg:
		if m.seq == t.seq && m.err == nil {
			t.count, t.counted = m.n, true
			// an estimate is only worth showing for big tables; small ones get the exact number
			if over := t.ws.a.d.Cfg.General.EstimateCountOver; !m.n.Exact && over > 0 && m.n.N < over {
				return t.countCmd(false)
			}
		}
	case structureLoadedMsg:
		if m.seq != t.structSeq {
			return nil
		}
		t.structLoaded = true
		if m.err != nil {
			t.structErr = m.err.Error()
			return nil
		}
		t.structErr = ""
		t.cols = mergeFKs(m.cols, m.fks)
		t.ws.meta[t.ref] = t.cols
		t.buildStructure(m)
		t.applyCols()
	case ddlLoadedMsg:
		if m.seq != t.structSeq {
			return nil
		}
		if m.err == nil {
			t.ddlBase = m.ddl
		} else {
			t.ddlBase = "-- " + m.err.Error()
		}
		t.composeDDL()
	default:
		if cmd, ok := t.ws.handleGlobal(msg); ok {
			return cmd
		}
	}
	return nil
}

func (t *tableTab) describeQuery() string {
	s := "-- browse " + objKey(t.ref)
	if n := len(t.filters.Active()); n > 0 {
		s += fmt.Sprintf(" where %d filter(s)", n)
	}
	for _, o := range t.sort {
		s += " order by " + o.Column
		if o.Desc {
			s += " desc"
		}
	}
	return s + fmt.Sprintf(" limit %d offset %d", t.limit, t.offset)
}

func (t *tableTab) fill(res *db.Result) {
	pk := map[string]bool{}
	types := map[string]string{}
	for _, c := range t.cols {
		pk[c.Name], types[c.Name] = c.PK, c.Type
	}
	cols := make([]gridCol, len(res.Columns))
	for i, c := range res.Columns {
		typ := c.Type
		if typ == "" {
			typ = types[c.Name]
		}
		cols[i] = gridCol{Name: c.Name, Type: typ, PK: pk[c.Name]}
	}
	t.grid.SetData(cols, res.Rows)
	t.resCols = res.Columns
	// Rows are identified by their primary key: with a key column hidden (or, for a table
	// without a key, any column hidden) an edit could hit other rows, so the grid turns read-only.
	// Until the column metadata has arrived the key is unknown, so the grid starts read-only.
	t.grid.Editable = t.ref.Kind == db.KindTable && t.cols != nil && t.keyVisible(res.Columns)
	if t.toTop {
		t.grid.ToTop()
		t.toTop = false
	}
	t.grid.RowOffset = t.offset
	t.grid.SortCol, t.grid.SortDesc = "", false
	if len(t.sort) > 0 {
		t.grid.SortCol, t.grid.SortDesc = t.sort[0].Column, t.sort[0].Desc
	}
	t.loadedRows = len(res.Rows)
	t.docs, t.tree = res.Docs, nil
}

// applyCols puts column metadata that arrived after the rows onto the grid (key icons, types,
// editability) without reloading the data.
func (t *tableTab) applyCols() {
	if t.cols == nil || len(t.grid.Cols) == 0 {
		return
	}
	by := map[string]db.Column{}
	for _, c := range t.cols {
		by[c.Name] = c
	}
	for i := range t.grid.Cols {
		if c, ok := by[t.grid.Cols[i].Name]; ok {
			t.grid.Cols[i].PK = c.PK
			if t.grid.Cols[i].Type == "" {
				t.grid.Cols[i].Type = c.Type
			}
		}
	}
	t.grid.Editable = !t.loading && t.ref.Kind == db.KindTable && t.keyVisible(t.resCols)
}

// keyVisible says whether the loaded columns still identify rows safely.
func (t *tableTab) keyVisible(shown []db.ResultColumn) bool {
	have := map[string]bool{}
	for _, c := range shown {
		have[c.Name] = true
	}
	hasPK := false
	for _, c := range t.cols {
		if c.PK {
			hasPK = true
			if !have[c.Name] {
				return false
			}
		}
	}
	return hasPK || len(t.hidden) == 0
}

// ---- input ------------------------------------------------------------------------------

// guard runs then immediately, or after the user confirms losing pending edits.
func (t *tableTab) guard(then func() tea.Cmd) tea.Cmd {
	if !t.grid.HasPending() {
		return then()
	}
	t.ws.a.push(&confirmDlg{title: "Discard pending changes?", lines: []string{"This changes what is loaded; uncommitted edits in " + t.ref.Name + " will be lost."}, yes: "Discard", danger: true, onYes: then})
	return nil
}

func (t *tableTab) Input(msg tea.Msg) tea.Cmd {
	if t.view >= viewJSON {
		return t.docInput(msg)
	}
	if t.view != viewData {
		return t.structInput(msg)
	}
	g := t.grid
	if t.showFilters && t.fb.focus {
		return t.filterInput(msg)
	}
	if cm, ok := msg.(tea.ClipboardMsg); ok {
		mode := t.paste
		t.paste = ""
		data := ParseClip(cm.Content)
		var done bool
		switch mode {
		case "rows":
			done = g.PasteRows(data)
		case "cells":
			done = g.PasteCells(data)
		default:
			return nil
		}
		if !done {
			return t.ws.a.say("nothing pasted: the clipboard is empty or %s is read-only", t.ref.Name)
		}
		return t.ws.a.say("pasted %d row(s)", len(data))
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
	key = strings.TrimPrefix(key, "shift+")
	switch key {
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
	case "home":
		g.GoCol(0, extend)
	case "end":
		g.GoCol(len(g.Cols)-1, extend)
	case "g":
		g.GoRow(0, false)
	case "G":
		g.GoRow(len(g.Rows)-1, false)
	case "esc":
		g.ClearSelection()
	case "enter": // a popover: cells can hold a whole JSON document
		if !g.Editable || g.cur() == nil || g.cur().State == rowDeleted {
			return t.quickLook()
		}
		return editCellInPopover(t.ws, g)
	case "space":
		t.ws.showDetail = !t.ws.showDetail
	case "s":
		return t.cycleSort()
	case "/":
		return t.findPrompt(g, &t.findText)
	case "n":
		return t.findNext(g, t.findText, 1)
	case "N":
		return t.findNext(g, t.findText, -1)
	case ">":
		t.resizeCol(2)
	case "<":
		t.resizeCol(-2)
	case "delete", "backspace":
		g.DeleteRows()
	case "i":
		g.AddRow()
	case "e":
		g.StartEdit()
	}
	return nil
}

// findPrompt asks for text and jumps to its first match in the loaded rows; n / N repeat.
func (t *tableTab) findPrompt(g *Grid, last *string) tea.Cmd {
	t.ws.a.push(newForm("Find in the loaded rows", []formField{{Key: "q", Label: "Text", Kind: fText, Value: *last}}, func(v map[string]string) tea.Cmd {
		*last = v["q"]
		return t.findNext(g, *last, 1)
	}))
	return nil
}

func (t *tableTab) findNext(g *Grid, text string, dir int) tea.Cmd {
	if text == "" {
		return t.ws.a.say("press / to search the loaded rows")
	}
	if !g.Find(text, dir) {
		return t.ws.a.say("%q is not in the loaded rows", text)
	}
	return nil
}

func (t *tableTab) resizeCol(d int) {
	if t.grid.col < len(t.grid.Cols) {
		c := &t.grid.Cols[t.grid.col]
		c.Width = clamp(c.Width+d, 4, 120)
	}
}

func (t *tableTab) cycleSort() tea.Cmd {
	g := t.grid
	if g.col >= len(g.Cols) {
		return nil
	}
	name := g.Cols[g.col].Name
	return t.guard(func() tea.Cmd { return t.sortBy(name) })
}

func (t *tableTab) sortBy(name string) tea.Cmd {
	switch {
	case len(t.sort) == 0 || t.sort[0].Column != name:
		t.sort = []db.Sort{{Column: name}}
	case !t.sort[0].Desc:
		t.sort = []db.Sort{{Column: name, Desc: true}}
	default:
		t.sort = nil
	}
	t.offset, t.toTop = 0, true
	t.saveState()
	return t.load()
}

func (t *tableTab) quickLook() tea.Cmd {
	v, ok := t.grid.CursorValue()
	if !ok {
		return nil
	}
	text := plain(v)
	if b, ok := v.([]byte); ok {
		text = hexDump(b)
	}
	t.ws.a.push(newQuickLook(t.grid.Cols[t.grid.col].Name, text))
	return nil
}

// ---- actions ----------------------------------------------------------------------------

func (t *tableTab) Action(name string) (bool, tea.Cmd) {
	// A prefix action while a cell editor is open saves that edit first (like clicking elsewhere);
	// otherwise add_row & co would act on a half-typed cell.
	if t.view == viewData && t.grid.Editing() {
		t.grid.FinishEdit(true)
	}
	if g := t.editGrid(); t.view != viewData && g != nil && g.Editing() {
		g.FinishEdit(true)
	}
	g := t.grid
	switch name {
	case "show_data":
		return true, t.setView(viewData)
	case "show_structure":
		return true, t.setView(viewStructure)
	case "show_index":
		return true, t.setView(viewIndex)
	case "show_ddl":
		return true, t.setView(viewDDL)
	case "show_json":
		return true, t.setView(viewJSON)
	case "show_tree":
		return true, t.setView(viewTree)
	}
	if t.view != viewData {
		if t.view == viewDDL || t.view >= viewJSON {
			return false, nil
		}
		switch name {
		case "add_row", "delete_rows", "set_value":
			if t.editGrid() == nil {
				return true, t.ws.a.say("the structure is still loading")
			}
		}
		switch name {
		case "add_row":
			return true, t.structAdd(t.editGrid())
		case "delete_rows":
			t.editGrid().DeleteRows()
			return true, nil
		case "set_value":
			return true, t.structSetValue()
		}
		return false, nil
	}
	switch name {
	case "add_row":
		if !g.AddRow() {
			return true, t.ws.a.say("%s is read-only", t.ref.Name)
		}
	case "duplicate_rows":
		g.DuplicateRows()
	case "delete_rows":
		g.DeleteRows()
	case "copy_rows":
		return true, t.copy(g.CopyRows(FmtCSV, t.ref.Name), "rows")
	case "copy_cells":
		return true, t.copy(g.CopyCells(FmtPlain, t.ref.Name), "cells")
	case "copy_rows_as":
		var items []pickItem
		for _, f := range CopyFormats {
			items = append(items, pickItem{Label: string(f), Value: f})
		}
		t.ws.a.push(&pickerDlg{title: "Copy rows as", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
			return t.copy(g.CopyRows(it.Value.(CopyFormat), t.ref.Name), "rows")
		}})
	case "set_value":
		return true, t.setValueMenu()
	case "paste_rows", "paste_cells":
		if !g.Editable {
			return true, t.ws.a.say("%s is read-only", t.ref.Name)
		}
		t.paste = strings.TrimPrefix(name, "paste_")
		return true, tea.ReadClipboard
	case "quick_look":
		return true, t.quickLook()
	case "filters":
		if t.showFilters && !t.fb.focus {
			t.fb.focus = true
		} else if t.showFilters {
			t.fb.focus, t.showFilters = false, false
		} else {
			t.openFilters()
		}
	case "apply_filters":
		return true, t.applyFilters()
	case "columns":
		return true, t.columnsPicker()
	case "quick_filter":
		return true, t.quickFilter()
	case "follow_fk":
		return true, t.followFK()
	case "count_exact":
		return true, t.loadCountExact()
	case "page_settings":
		return true, t.pageSettings()
	case "first_row":
		g.GoRow(0, false)
	case "last_row":
		g.GoRow(len(g.Rows)-1, false)
	case "next_page":
		if t.loadedRows >= t.limit {
			return true, t.guard(func() tea.Cmd {
				t.offset += t.limit
				t.toTop = true
				return t.load()
			})
		}
	case "prev_page":
		if t.offset > 0 {
			return true, t.guard(func() tea.Cmd {
				t.offset = max(0, t.offset-t.limit)
				t.toTop = true
				return t.load()
			})
		}
	default:
		return false, nil
	}
	return true, nil
}

// pageSettings is the Limit / Offset box under the grid.
func (t *tableTab) pageSettings() tea.Cmd {
	t.ws.a.push(newForm("Rows per page and offset", []formField{
		{Key: "limit", Label: "Limit", Kind: fText, Value: strconv.Itoa(t.limit)},
		{Key: "offset", Label: "Offset", Kind: fText, Value: strconv.Itoa(t.offset)},
	}, func(v map[string]string) tea.Cmd {
		lim, err1 := strconv.Atoi(strings.TrimSpace(v["limit"]))
		off, err2 := strconv.Atoi(strings.TrimSpace(v["offset"]))
		if err1 != nil || err2 != nil || lim < 1 || off < 0 {
			return t.ws.a.fail(fmt.Errorf("limit must be 1 or more, offset 0 or more"))
		}
		return t.guard(func() tea.Cmd {
			t.limit, t.offset, t.toTop = lim, off, true
			return t.load()
		})
	}))
	return nil
}

func (t *tableTab) copy(text, what string) tea.Cmd {
	if text == "" {
		return nil
	}
	return tea.Batch(tea.SetClipboard(text), t.ws.a.say("copied %s", what))
}

func (t *tableTab) setValueMenu() tea.Cmd {
	if !t.grid.Editable {
		return t.ws.a.say("%s is read-only", t.ref.Name)
	}
	items := []pickItem{{Label: "NULL", Value: db.Null}, {Label: "DEFAULT", Value: db.Default}, {Label: "EMPTY", Value: db.Empty}, {Label: "NOW()", Value: db.Expr("NOW()")}}
	if t.ws.caps.Dialect == "clickhouse" {
		items[3].Value = db.Expr("now()")
	}
	t.ws.a.push(&pickerDlg{title: "Set value", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		t.grid.SetCell(it.Value)
		return nil
	}})
	return nil
}

// Do implements undo/redo/reload for the workspace actions.
func (t *tableTab) Do(name string) tea.Cmd {
	if t.view == viewStructure || t.view == viewIndex {
		if g := t.editGrid(); g != nil {
			switch name {
			case "undo":
				g.Undo()
				return nil
			case "redo":
				g.Redo()
				return nil
			}
		}
		if name == "undo" || name == "redo" {
			return nil
		}
		if name == "reload_tab" { // the structure views reload the structure, not the rows
			if t.structPending() {
				t.ws.a.push(&confirmDlg{title: "Reload and discard pending changes?", yes: "Reload", danger: true, onYes: func() tea.Cmd { t.structLoaded, t.cols = false, nil; t.ws.forget(&t.ref); return t.loadStructure() }})
				return nil
			}
			t.structLoaded, t.cols = false, nil
			t.ws.forget(&t.ref)
			return t.loadStructure()
		}
	}
	switch name {
	case "undo":
		t.grid.Undo()
	case "redo":
		t.grid.Redo()
	case "reload_tab":
		if t.Dirty() {
			t.ws.a.push(&confirmDlg{title: "Reload and discard pending changes?", yes: "Reload", danger: true, onYes: func() tea.Cmd { return t.load() }})
			return nil
		}
		t.cols, t.structLoaded = nil, false
		t.ws.forget(&t.ref)
		return t.load()
	}
	return nil
}

func (t *tableTab) Statements() ([]db.Statement, error) {
	if t.view == viewStructure || t.view == viewIndex {
		if t.structGrid == nil || !t.structLoaded {
			return nil, fmt.Errorf("the structure is still loading")
		}
		return t.structStatements()
	}
	return t.ws.sess.Plan(t.grid.Changes(t.ref))
}

func (t *tableTab) Discard() {
	if t.view == viewStructure || t.view == viewIndex {
		t.discardStructure()
		return
	}
	t.grid.DiscardPending()
}

func (t *tableTab) Commit() tea.Cmd {
	if t.view == viewStructure || t.view == viewIndex {
		return t.commitStructure()
	}
	if !t.grid.HasPending() {
		return t.ws.a.say("no pending changes")
	}
	if t.loading || t.committing {
		return t.ws.a.say("still loading; wait before committing again")
	}
	stmts, err := t.Statements()
	if err != nil {
		return t.ws.a.fail(err)
	}
	send := func() tea.Cmd {
		return t.ws.execute(stmts, func(err error) tea.Cmd {
			if err != nil || t.closed {
				return nil
			}
			t.committing = true     // until the reload lands (the apply itself is covered by w.applying)
			t.grid.DiscardPending() // the server has them now; a second commit must not resend the INSERTs
			return tea.Batch(t.load(), t.ws.a.say("committed %d statement(s)", len(stmts)))
		})
	}
	if t.keylessWrites() {
		t.ws.a.push(&confirmDlg{title: t.ref.Name + " has no primary key", yes: "Commit anyway", danger: true, onYes: send,
			lines: []string{"Rows are identified by all of their values, so every row identical to an edited or deleted one is changed or deleted too."}})
		return nil
	}
	return send()
}

// keylessWrites: an update or delete is pending on a table without a primary key.
func (t *tableTab) keylessWrites() bool {
	for _, c := range t.cols {
		if c.PK {
			return false
		}
	}
	if t.ws.caps.Document { // documents are keyed by _id / the redis key
		return false
	}
	for _, ch := range t.grid.Changes(t.ref) {
		if ch.Kind != db.Insert {
			return true
		}
	}
	return false
}

// DetailCol is the field the detail pane's cursor is on: the grid's current column.
func (t *tableTab) DetailCol() int { return t.grid.col }

// DetailKey handles a key while the detail pane has focus: j/k move between fields, Enter edits
// the field in the popover (or quick-looks it when the table is read-only).
func (t *tableTab) DetailKey(key string) tea.Cmd {
	g := t.grid
	if detailMove(g, key) {
		return nil
	}
	if key == "enter" {
		if !g.Editable || g.cur() == nil || g.cur().State == rowDeleted {
			return t.quickLook()
		}
		return editCellInPopover(t.ws, g)
	}
	return nil
}

// detailMove moves the grid cursor one field for the detail pane's keys; it reports whether key was one.
func detailMove(g *Grid, key string) bool {
	switch key {
	case "j", "down", "tab":
		g.Move(0, 1, false)
	case "k", "up", "shift+tab":
		g.Move(0, -1, false)
	case "home", "g":
		g.GoCol(0, false)
	case "end", "G":
		g.GoCol(len(g.Cols)-1, false)
	default:
		return false
	}
	return true
}

func (t *tableTab) Detail() []string {
	r := t.grid.cur()
	if r == nil {
		return nil
	}
	var out []string
	for i, c := range t.grid.Cols {
		mark := " "
		if r.Dirty[i] {
			mark = "*"
		}
		out = append(out, fmt.Sprintf("%s%s: %s", mark, c.Name, cellText(r.Cur[i])), "")
	}
	return out
}

// ---- view -------------------------------------------------------------------------------

func (t *tableTab) setView(v int) tea.Cmd {
	if (v == viewJSON || v == viewTree) && !t.ws.caps.Document {
		return t.ws.a.say("Json and Tree views are for document engines (MongoDB, Redis)")
	}
	t.view = v
	if v >= viewJSON {
		return nil
	}
	if v != viewData && !t.structLoaded {
		return t.loadStructure()
	}
	return nil
}

func (t *tableTab) View(th Theme, W, H int) string {
	body := t.bodyView(th, W, H-1)
	return body + "\n" + t.footer(th, W)
}

func (t *tableTab) bodyView(th Theme, W, H int) string {
	switch {
	case t.err != "" && t.view == viewData: // a failed row load must not hide the structure / DDL views
		msg := append(t.filterView(th, W), "", "  "+th.Err.Bold(true).Render("✗ Could not load "+t.ref.Name))
		for _, l := range wrapText(t.err, max(10, W-6)) {
			msg = append(msg, "    "+l)
		}
		msg = append(msg, "")
		if len(t.filters.Active()) > 0 {
			msg = append(msg, "    "+th.Dim.Render("A filter is active and may be the cause."),
				"    "+th.chips(t.keyFor("filters"), "edit it", "M-c", "clear all (in the bar)", t.keyFor("reload_tab"), "reload"))
		} else {
			msg = append(msg, "    "+th.chips(t.keyFor("reload_tab"), "reload"))
		}
		return pad(strings.Join(msg, "\n"), W, H)
	case t.loading && len(t.grid.Cols) == 0 && t.view == viewData:
		return pad("\n  "+th.Warn.Render(t.ws.a.spinner())+th.Dim.Render(" loading "+t.ref.Name+"…"), W, H)
	}
	switch t.view {
	case viewJSON:
		return t.jsonView(th, W, H)
	case viewTree:
		if t.tree == nil {
			t.tree = newDocTree(t.docs)
		}
		return t.tree.view(th, W, H)
	}
	if t.view != viewData {
		return t.structView(th, W, H)
	}
	bar := t.filterView(th, W)
	if !t.showFilters && len(t.filters.Active()) > 0 { // hidden bar: the filters still show as pills
		bar = []string{fit(" "+t.filterPills(th)+"   "+th.chips(t.keyFor("filters"), "edit"), W)}
	}
	t.grid.W, t.grid.H = W, max(2, H-len(bar))
	var out string
	if len(t.grid.Cols) > 0 && len(t.grid.Rows) == 0 && !t.loading {
		empty := th.Dim.Render("No rows match the filters") + "   " + th.chips(t.keyFor("filters"), "edit filters")
		if len(t.filters.Active()) == 0 {
			empty = th.Dim.Render("No rows yet")
			if t.grid.Editable {
				empty += "   " + th.chips(t.keyFor("add_row"), "add a row")
			}
		}
		lines := append(strings.Split(t.grid.View(th, true), "\n")[:1], "", "  "+empty)
		out = pad(strings.Join(lines, "\n"), W, t.grid.H)
	} else {
		out = t.grid.View(th, !t.ws.focusSB && !t.fb.focus)
	}
	if len(bar) > 0 {
		out = strings.Join(bar, "\n") + "\n" + out
	}
	return out
}

func pad(s string, W, H int) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		lines = append(lines, fit(l, W))
	}
	for len(lines) < H {
		lines = append(lines, strings.Repeat(" ", W))
	}
	return strings.Join(lines, "\n")
}

// keyFor is the prefixed key bound to action, as shown in chips ("C-d f"); "" when unbound.
func (t *tableTab) keyFor(action string) string {
	k := firstKey(t.ws.a.d.Keys, action)
	if k == "?" {
		return ""
	}
	return keymap.Display(t.ws.a.d.Keys.Prefix) + " " + keymap.Display(k)
}

// filterPills renders the filters as pills (column bold, operator dim, value plain) joined by
// the dim and/or; disabled filters are struck through.
func (t *tableTab) filterPills(th Theme) string {
	bg := lipgloss.Color(th.pal["header_bg"])
	join := th.Dim.Render(" and ")
	if t.filters.Or {
		join = th.Dim.Render(" or ")
	}
	var pills []string
	for _, f := range t.filters.Filters {
		col := f.Column
		switch col {
		case db.AnyColumn:
			col = "any column"
		case db.RawColumn:
			col = "sql"
		}
		if !f.Enabled {
			pills = append(pills, th.Dim.Background(bg).Strikethrough(true).Render(" "+strings.TrimSpace(col+" "+f.Op+" "+f.Value)+" "))
			continue
		}
		p := th.Bold.Background(bg).Render(" "+col) + th.Dim.Background(bg).Render(" "+f.Op)
		if f.Value != "" {
			p += lipgloss.NewStyle().Background(bg).Render(" " + f.Value)
		}
		pills = append(pills, p+lipgloss.NewStyle().Background(bg).Render(" "))
	}
	return th.Section.Render("Filters") + "  " + strings.Join(pills, join)
}

// pendingRows counts the rows with uncommitted edits, inserts or deletes.
func (t *tableTab) pendingRows() int {
	n := 0
	for _, r := range t.grid.Rows {
		if r.State != rowClean || len(r.Dirty) > 0 {
			n++
		}
	}
	return n
}

func (t *tableTab) footer(th Theme, W int) string {
	var names, keys []string
	active := 0
	for i, id := range t.viewList() {
		names = append(names, viewNames[id])
		keys = append(keys, t.keyFor(map[int]string{viewData: "show_data", viewStructure: "show_structure", viewIndex: "show_index", viewDDL: "show_ddl", viewJSON: "show_json", viewTree: "show_tree"}[id]))
		if id == t.view {
			active = i
		}
	}
	left := th.segmented(names, keys, active)
	right := ""
	if t.view == viewData && t.err == "" { // after a failed load the old row range would mislead
		var parts []string
		if t.loading {
			parts = append(parts, th.Warn.Render(t.ws.a.spinner())+th.Dim.Render(" loading"))
		}
		if n := t.pendingRows(); n > 0 {
			parts = append(parts, th.Warn.Render(fmt.Sprintf("● %d pending", n)))
		}
		if len(t.hidden) > 0 {
			parts = append(parts, th.Dim.Render(fmt.Sprintf("%d hidden col(s)", len(t.hidden))))
		}
		total := th.Dim.Render("…")
		if t.counted {
			total = th.Bold.Render(strconv.FormatInt(t.count.N, 10))
			if !t.count.Exact {
				total = th.Dim.Render("≈") + total // an estimate from the planner; C-d # counts exactly
			}
		}
		first := t.offset + 1
		if t.loadedRows == 0 {
			first = 0
		}
		parts = append(parts, th.Dim.Render("rows ")+th.Bold.Render(fmt.Sprintf("%d–%d", first, t.offset+t.loadedRows))+th.Dim.Render(" of ")+total)
		if d := t.took.Round(time.Millisecond); d > 0 {
			parts = append(parts, th.Dim.Render(d.String()))
		}
		if t.offset > 0 || t.loadedRows >= t.limit {
			parts = append(parts, th.chips(t.keyFor("page_settings"), "pages"))
		}
		right = strings.Join(parts, th.Dim.Render(" · "))
	}
	gap := W - width(left) - width(right) - 1
	if gap < 1 { // not enough room for the keys: names only, the frame hint still lists the keys
		left = th.segmented(names, nil, active)
		gap = W - width(left) - width(right) - 1
	}
	if gap < 1 {
		return fit(left+" "+right, W)
	}
	return left + strings.Repeat(" ", gap) + right + " "
}

// Hint is shown in the frame's bottom edge: the keys that matter for the current view.
func (t *tableTab) Hint(th Theme) string {
	pre := keymap.Display(t.ws.a.d.Keys.Prefix)
	switch {
	case t.view == viewData && t.showFilters && t.fb.focus:
		return th.chips("↵", "apply", "Tab", "field", "M-n", "add", "Esc", "hide")
	case t.view == viewData:
		return th.chips("↵", "edit", "hjkl", "move", pre+" i", "add row", pre+" s", "commit", pre+" f", "filter")
	case t.view == viewStructure || t.view == viewIndex:
		return th.chips("↵", "edit/toggle", pre+" i", "add", "Del", "remove", pre+" s", "commit")
	case t.view == viewTree:
		return th.chips("↵", "fold", "E/C", "expand/collapse all")
	}
	if t.view == viewDDL && (t.ref.Kind == db.KindView || t.ref.Kind == db.KindMatView) {
		return th.chips("j/k", "scroll", "c", "copy", "e", "edit view")
	}
	return th.chips("j/k", "scroll", "c", "copy")
}

func hexDump(b []byte) string {
	var sb strings.Builder
	for i := 0; i < len(b); i += 16 {
		end := min(i+16, len(b))
		fmt.Fprintf(&sb, "%08x  % x\n", i, b[i:end])
	}
	return sb.String()
}

// prettyValue indents JSON values; anything else is shown as is.
func prettyValue(s string) string {
	t := strings.TrimSpace(s)
	if len(t) < 2 || (t[0] != '{' && t[0] != '[') {
		return s
	}
	if out, ok := indentJSON(t); ok {
		return out
	}
	return s
}
