package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"webtuidb/internal/db"
	"webtuidb/internal/keymap"
	"webtuidb/internal/store"
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
	appliedSet  db.FilterSet // what the grid was last loaded with: the bar edits t.filters on every keystroke
	hasApplied  bool
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
	find        FindSpec
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
	t.grid.NoStripes, t.grid.DecodeSpatial = !ws.a.d.Cfg.Table.AlternatingRows, ws.a.d.Cfg.Table.DecodeSpatial
	cfg := ws.a.d.Cfg.Table
	if ts := ws.a.d.State.Table(t.stateKey()); ts.Sort != nil || len(ts.Filters.Filters) > 0 || len(ts.Hidden) > 0 {
		t.sort, t.filters, t.showFilters = ts.Sort, ts.Filters, ts.ShowBar
		t.markApplied()
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
	t.markApplied() // applied() never falls back to typed-but-unapplied filters
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
	t.ws.a.d.State.SetTable(t.stateKey(), store.TableState{Hidden: hidden, Filters: t.applied(), ShowBar: t.showFilters, Sort: t.sort, ColWidths: t.ws.a.d.State.Table(t.stateKey()).ColWidths})
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

// pruneHidden forgets hidden columns the table no longer has (renamed or dropped since the state was
// saved); left in, they miscount the visible columns and keep a keyless grid read-only.
func (t *tableTab) pruneHidden() {
	if len(t.hidden) == 0 || len(t.cols) == 0 {
		return
	}
	have := map[string]bool{}
	for _, c := range t.cols {
		have[c.Name] = true
	}
	pruned := false
	for h := range t.hidden {
		if !have[h] {
			delete(t.hidden, h)
			pruned = true
		}
	}
	if pruned {
		t.saveState()
	}
}

// hiddenCount is how many of the table's columns are hidden.
func (t *tableTab) hiddenCount() int {
	n := 0
	for _, c := range t.cols {
		if t.hidden[c.Name] {
			n++
		}
	}
	return n
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

// applied is the filter set the grid shows (typed-but-unapplied edits excluded); exports and
// bulk updates use it so they never act on rows the user cannot see.
func (t *tableTab) applied() db.FilterSet {
	if t.hasApplied {
		return t.appliedSet
	}
	return t.filters
}

// markApplied snapshots the filters the next load runs with.
func (t *tableTab) markApplied() {
	t.appliedSet = db.FilterSet{Filters: append([]db.Filter(nil), t.filters.Filters...), Or: t.filters.Or}
	t.hasApplied = true
}

func (t *tableTab) query() db.RowQuery {
	return db.RowQuery{ObjRef: t.ref, Filters: t.applied(), Sort: t.sort, Limit: t.limit, Offset: t.offset, Columns: t.visibleCols()}
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
		t.pruneHidden()
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
	for _, f := range t.applied().Active() {
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

func (t *tableTab) loadCount() tea.Cmd { return t.countCmd(len(t.applied().Active()) == 0) }

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
		t.pruneHidden()
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
		t.pruneHidden()
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
	first := len(t.grid.Cols) == 0
	t.grid.SetData(cols, res.Rows)
	if first {
		t.restoreWidths() // widths dragged with the mouse in an earlier session
	}
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
	t.grid.SortCol, t.grid.SortDesc, t.grid.Sorts = "", false, t.sort
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
	return hasPK || t.hiddenCount() == 0
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
			return t.ws.a.fail(fmt.Errorf("nothing pasted: the clipboard is empty or %s is read-only", t.ref.Name))
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
		g.EditKey(k)
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
		return t.findPrompt(g)
	case "n":
		return t.ws.a.findAgain(g, t.find, 1)
	case "N":
		return t.ws.a.findAgain(g, t.find, -1)
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

// findPrompt opens the find / replace form; n / N repeat the search.
func (t *tableTab) findPrompt(g *Grid) tea.Cmd {
	t.ws.a.findForm("Find in the loaded rows", g, &t.find, true)
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
	var save func([]byte) tea.Cmd
	if t.grid.Editable {
		save = func(b []byte) tea.Cmd { t.grid.SetCell(b); return nil }
	}
	t.ws.a.push(t.ws.a.newQuickLookValue(t.grid.Cols[t.grid.col].Name, v, save))
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
		case "copy_rows_as", "copy_cells_as":
			return true, t.structCopyAs()
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
			return true, t.ws.a.fail(fmt.Errorf("%s is read-only", t.ref.Name))
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
		t.ws.a.pushCopyAs("Copy rows as", CopyFormats, func(f CopyFormat) tea.Cmd { return t.copy(g.CopyRows(f, t.ref.Name), "rows") })
	case "copy_cells_as":
		t.ws.a.pushCopyAs("Copy selected cells as", CopyFormats, func(f CopyFormat) tea.Cmd { return t.copy(g.CopyCells(f, t.ref.Name), "cells") })
	case "copy_column":
		return true, t.copy(g.CopyColumn(), "column")
	case "column_menu":
		return true, t.columnMenu()
	case "set_value":
		return true, t.setValueMenu()
	case "paste_rows", "paste_cells":
		if !g.Editable {
			return true, t.ws.a.fail(fmt.Errorf("%s is read-only", t.ref.Name))
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
	case "filter_sql":
		return true, t.showFilterSQL()
	case "filter_clear_all":
		return true, t.clearFilters()
	case "filter_add_copy":
		if !t.showFilters {
			return true, nil
		}
		t.addFilterCopy()
		return true, nil
	case "find":
		return true, t.findPrompt(g)
	case "find_next":
		return true, t.ws.a.findAgain(g, t.find, 1)
	case "find_prev":
		return true, t.ws.a.findAgain(g, t.find, -1)
	case "filter_export":
		return true, t.exportFiltered()
	case "columns":
		return true, t.columnsPicker()
	case "sort_columns":
		return true, t.sortColumns()
	case "bulk_update":
		return true, t.bulkUpdate()
	case "hide_column":
		return true, t.hideColumn()
	case "show_all_columns":
		return true, t.showAllColumns()
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
		lim, _ := strconv.Atoi(strings.TrimSpace(v["limit"]))
		off, _ := strconv.Atoi(strings.TrimSpace(v["offset"]))
		return t.guard(func() tea.Cmd {
			t.limit, t.offset, t.toTop = lim, off, true
			return t.load()
		})
	}).checked(func(v map[string]string) error {
		lim, err1 := strconv.Atoi(strings.TrimSpace(v["limit"]))
		off, err2 := strconv.Atoi(strings.TrimSpace(v["offset"]))
		if err1 != nil || err2 != nil || lim < 1 || off < 0 {
			return fmt.Errorf("limit must be 1 or more, offset 0 or more")
		}
		return nil
	}))
	return nil
}

func (t *tableTab) copy(text, what string) tea.Cmd {
	if text == "" {
		return nil
	}
	return tea.Batch(setClip(text), t.ws.a.say("copied %s", what))
}

func (t *tableTab) setValueMenu() tea.Cmd {
	if !t.grid.Editable {
		return t.ws.a.fail(fmt.Errorf("%s is read-only", t.ref.Name))
	}
	items := []pickItem{{Label: "NULL", Value: db.Null}, {Label: "DEFAULT", Value: db.Default}, {Label: "EMPTY", Value: db.Empty}, {Label: "NOW()", Value: db.Expr("NOW()")}}
	if t.ws.caps.Dialect == "clickhouse" {
		items[3].Value = db.Expr("now()")
	}
	items = append(items, setValueExtras()...)
	t.ws.a.push(&pickerDlg{title: "Set value", items: items, onPick: func(it pickItem, _ bool) tea.Cmd { return t.ws.a.pickSetValue(t.grid, it.Value) }})
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
		return t.ws.executeWith(t.preimage(), stmts, func(err error) tea.Cmd {
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

// preimage is the CSV of the keys about to be changed, when general.log_original_values is on.
func (t *tableTab) preimage() string {
	if !t.ws.a.d.Cfg.General.LogOriginalValues {
		return ""
	}
	return db.Preimage(t.grid.Changes(t.ref))
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
	switch key {
	case "/":
		return t.searchField()
	case "enter":
		if !g.Editable || g.cur() == nil || g.cur().State == rowDeleted {
			return t.quickLook()
		}
		return editFieldInPopover(t.ws, g)
	case "p", "m": // PRETTY / MINIFY the JSON field in every selected row
		if !g.Editable {
			return t.ws.a.fail(fmt.Errorf("read-only: PRETTY / MINIFY stage an edit"))
		}
		n := g.MapColumn(func(v any) (any, bool) { return reformatJSON(v, key == "p") })
		if n == 0 {
			return t.ws.a.say("nothing to %s: the field is not JSON (or already is)", map[string]string{"p": "pretty-print", "m": "minify"}[key])
		}
		return t.ws.a.say("%s %d row(s); commit to apply", map[string]string{"p": "pretty-printed", "m": "minified"}[key], n)
	}
	return nil
}

// detailMove moves the grid cursor one field for the detail pane's keys; it reports whether key was one.
func detailMove(g *Grid, key string) bool {
	switch key {
	case "j", "down", "tab":
		g.MoveField(1)
	case "k", "up", "shift+tab":
		g.MoveField(-1)
	case "home", "g":
		g.MoveField(-len(g.Cols))
	case "end", "G":
		g.MoveField(len(g.Cols))
	default:
		return false
	}
	return true
}

// DetailInfo is the line the row detail pane shows for the current column: "type · nullable · default · key".
func (t *tableTab) DetailInfo() string {
	g := t.grid
	if g.col >= len(g.Cols) {
		return ""
	}
	for _, c := range t.cols {
		if c.Name == g.Cols[g.col].Name {
			return g.Cols[g.col].Name + ": " + columnInfo(c)
		}
	}
	return g.Cols[g.col].Name + ": " + g.Cols[g.col].Type
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
		out = append(out, fmt.Sprintf("%s%s: %s", mark, c.Name, detailText(r.Cur[i])), "")
	}
	return out
}

// ---- view -------------------------------------------------------------------------------

func (t *tableTab) setView(v int) tea.Cmd {
	if (v == viewJSON || v == viewTree) && !t.ws.caps.Document {
		return t.ws.a.fail(fmt.Errorf("Json and Tree views are for document engines (MongoDB, Redis)"))
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
	if !t.showFilters && (len(t.applied().Active()) > 0 || t.filtersEdited()) { // hidden bar: the filters still show as pills
		bar = []string{fit(" "+t.filterPills(th)+"   "+th.chips(t.keyFor("filters"), "edit"), W)}
	}
	t.grid.W, t.grid.H = W, max(2, H-len(bar))
	var out string
	if len(t.grid.Cols) > 0 && len(t.grid.Rows) == 0 && !t.loading {
		empty := th.Dim.Render("No rows match the filters") + "   " + th.chips(t.keyFor("filters"), "edit filters")
		if len(t.applied().Active()) == 0 {
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

// boundKey is the prefixed key bound to action in ctx (else global), as shown in hints ("C-d s"); "" when unbound,
// so hints follow a [keys] rebinding and drop an action that lost its key (T-P2-06; chips skips "").
func boundKey(km *keymap.Map, ctx, action string) string {
	for _, b := range km.Bindings(ctx) {
		if b.Action == action {
			return keymap.Display(km.Prefix) + " " + keymap.Display(b.Key)
		}
	}
	return ""
}

// filterPills renders the filters as pills (column bold, operator dim, value plain) joined by
// the dim and/or; disabled filters are struck through. They are the applied filters (the ones the
// rows came from), marked "not applied" when the bar holds other edits.
func (t *tableTab) filterPills(th Theme) string {
	bg := lipgloss.Color(th.pal["header_bg"])
	ap := t.applied()
	join := th.Dim.Render(" and ")
	if ap.Or {
		join = th.Dim.Render(" or ")
	}
	var pills []string
	for _, f := range ap.Filters {
		col := f.Column
		switch col {
		case db.AnyColumn:
			col = "any column"
		case db.RawColumn:
			col, f.Op = "sql", "" // raw SQL has no operator
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
	s := th.Section.Render("Filters") + "  " + strings.Join(pills, join)
	if t.filtersEdited() {
		s += th.Warn.Render(" · not applied")
	}
	return s
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
	km := t.ws.a.d.Keys
	switch {
	case t.view == viewData && t.showFilters && t.fb.focus:
		return th.chips("↵", "apply", "Tab", "field", "Space", "pick", "M-n", "add", "Esc", "hide")
	case t.view == viewData && t.ref.Kind != db.KindTable: // a view is read-only: nothing to edit or commit (T-P3-14)
		return th.chips("↵", "quick look", "hjkl", "move", boundKey(km, keymap.Grid, "filters"), "filter")
	case t.view == viewData:
		return th.chips("↵", "edit", "hjkl", "move", boundKey(km, keymap.Grid, "add_row"), "add row", boundKey(km, keymap.Grid, "commit"), "commit", boundKey(km, keymap.Grid, "filters"), "filter")
	case t.view == viewStructure || t.view == viewIndex:
		return th.chips("↵", "edit/toggle", boundKey(km, keymap.Structure, "add_row"), "add", "Del", "remove", boundKey(km, keymap.Structure, "commit"), "commit")
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
