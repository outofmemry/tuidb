package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"webtuidb/internal/db"
	"webtuidb/internal/keymap"
	"webtuidb/internal/store"
)

type sbKind int

const (
	sbHeader sbKind = iota
	sbObject
	sbQuery
	sbHistory
	sbFolder // a Redis key prefix such as "user:"
	sbColumn // a column under an expanded table (obj is the table, label the column, text its type)
)

type sbRow struct {
	kind   sbKind
	text   string // header or folder text
	obj    db.Object
	hasObj bool // a folder that is also a key (user:1 and user:1:name)
	q      store.Query
	h      store.HistoryEntry
	star   bool
	depth  int
	group  string // the custom group whose section this row is in
	label  string // object name relative to its folder
	path   string // folder path, the key into sidebar.open
	open   bool
	count  int
}

type sidebar struct {
	w           *Workspace
	tab         int // 0 items, 1 queries, 2 history
	objs        []db.Object
	schemas     []string
	dbs         []string
	loading     bool
	open        map[string]bool // expanded Redis key folders
	loadSeq     int
	prefetchSeq int
	err         string
	q           lineEdit
	searching   bool
	schema      string
	sel, top    int
	queries     []store.Query
	hist        []store.HistoryEntry
	histLoad    bool
	cols        map[string]bool    // tables whose columns are shown (sidebar.show_columns), by objKey
	qsort       string             // saved-queries order: name | created | modified
	marks       map[string]bool    // multi-select (mark): objKey of the ticked tables
	colsPending map[db.ObjRef]bool // column fetches in flight for expanded tables
}

func newSidebar(w *Workspace) *sidebar {
	return &sidebar{w: w, loading: true, schema: w.sess.CurrentSchema()}
}

type sidebarLoadedMsg struct {
	seq     int
	objs    []db.Object
	schemas []string
	err     error
}

type dbListMsg struct {
	dbs []string
	err error
}

// prefetchMsg fires when the selection has rested on an object for a moment.
type prefetchMsg struct {
	seq int
	ref db.ObjRef
}

func (s *sidebar) load() tea.Cmd {
	s.marks = nil // marks are keyed schema.name: they must not outlive a reload or a database switch
	s.loading = true
	w, schema, hasSchemas := s.w, s.schema, s.w.caps.Schemas
	lazy, sysOK := w.a.d.Cfg.Sidebar.LazyLoad, w.a.d.Cfg.Sidebar.LoadSystemSchemas
	s.loadSeq++
	seq := s.loadSeq
	// Schemas and objects in parallel; the database list (only used by Open Anything) arrives on
	// its own so it never holds the sidebar back.
	items := w.async(-1, func(ctx context.Context, sess db.Session) tea.Msg {
		m := sidebarLoadedMsg{seq: seq}
		var schemaErr error
		var wg sync.WaitGroup
		if hasSchemas {
			wg.Add(1)
			go func() { defer wg.Done(); m.schemas, schemaErr = sess.Schemas(ctx) }()
		}
		if !lazy && hasSchemas { // lazy_load = false: every schema's objects up front
			wg.Wait()
			if schemaErr == nil {
				for _, sc := range m.schemas {
					if !sysOK && sc != schema && isSystemSchema(sc) {
						continue
					}
					os, err := sess.Objects(ctx, sc)
					if err != nil {
						m.err = err
						break
					}
					m.objs = append(m.objs, os...)
				}
			}
		} else {
			m.objs, m.err = sess.Objects(ctx, schema)
		}
		wg.Wait()
		if m.err == nil {
			m.err = schemaErr
		}
		return m
	})
	dbs := w.async(-1, func(ctx context.Context, sess db.Session) tea.Msg {
		names, err := sess.Databases(ctx)
		return dbListMsg{dbs: names, err: err}
	})
	return tea.Batch(items, dbs)
}

func (s *sidebar) handle(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case sidebarLoadedMsg:
		if m.seq != s.loadSeq {
			return nil // an older load finished after a newer one started
		}
		s.loading = false
		if m.err != nil {
			s.err = m.err.Error()
			return s.w.a.fail(m.err)
		}
		keepID := s.selectedID() // the list is re-sorted: the cursor follows the item, not the position
		s.colsPending = nil
		s.err, s.objs, s.schemas = "", m.objs, nil
		for _, sc := range m.schemas {
			if s.keepSchema(sc) {
				s.schemas = append(s.schemas, sc)
			}
		}
		sort.SliceStable(s.objs, func(i, j int) bool {
			if s.objs[i].Kind != s.objs[j].Kind {
				return kindOrder(s.objs[i].Kind) < kindOrder(s.objs[j].Kind)
			}
			return strings.ToLower(s.objs[i].Name) < strings.ToLower(s.objs[j].Name)
		})
		s.selectID(keepID)
		return s.fetchMissingCols() // expanded tables whose columns a reload dropped
	case colsMetaMsg:
		delete(s.colsPending, m.ref)
		if m.err != nil && s.cols[objKey(m.ref)] {
			delete(s.cols, objKey(m.ref)) // fold it: "loading columns…" would never end
			return s.w.a.fail(fmt.Errorf("columns of %s: %w", m.ref.Name, m.err))
		}
		return nil
	case dbListMsg:
		if m.err == nil {
			s.dbs = m.dbs
		}
		return nil
	case prefetchMsg:
		return s.prefetch(m)
	case databasesMsg:
		if m.err != nil {
			s.w.a.askDB = false
			return s.w.a.fail(m.err)
		}
		s.dbs = m.dbs
		if s.w.a.askDB {
			s.w.a.askDB = false
			if len(m.dbs) < 2 {
				return nil
			}
		}
		return s.databasePicker(m.dbs)
	case dbSwitchedMsg:
		if m.err != nil {
			return s.w.a.fail(m.err)
		}
		for _, t := range s.w.tabs { // tabs belong to the previous database
			if c, ok := t.(interface{ Close() }); ok {
				c.Close()
			}
		}
		s.w.tabs, s.w.cur, s.w.focusSB = nil, 0, true
		s.w.m3.reset()
		s.sel, s.top = 0, 0
		s.w.forget(nil)
		return tea.Batch(s.load(), s.w.a.say("now using %s", m.name))
	}
	if cmd, ok := s.w.handleGlobal(msg); ok {
		return cmd
	}
	return nil
}

// keepSchema hides the server's own schemas unless [sidebar] load_system_schemas is set.
func (s *sidebar) keepSchema(name string) bool {
	return s.w.a.d.Cfg.Sidebar.LoadSystemSchemas || name == s.schema || !isSystemSchema(name)
}

func isSystemSchema(name string) bool {
	return name == "information_schema" || name == "crdb_internal" || strings.HasPrefix(name, "pg_")
}

func kindOrder(k db.Kind) int {
	switch k {
	case db.KindTable:
		return 0
	case db.KindView:
		return 1
	case db.KindMatView:
		return 2
	case db.KindFunction:
		return 3
	case db.KindProcedure:
		return 4
	case db.KindSequence:
		return 5
	}
	return 6
}

func objKey(o db.ObjRef) string {
	if o.Schema == "" {
		return o.Name
	}
	return o.Schema + "." + o.Name
}

func (s *sidebar) cfg() (showRecent, showFn, showSeq, showTypes bool, fuzzyMode bool) {
	c := s.w.a.d.Cfg.Sidebar
	return c.ShowRecent, c.ShowFunctions && s.w.caps.Functions, c.ShowSequences && s.w.caps.Sequences, c.ShowTypes, c.Matching != "contains"
}

// objStyle colors an object icon by kind so the sections read at a glance: tables in the
// accent, views in the keyword color, code in the number color, the rest dim.
func objStyle(th Theme, k db.Kind) lipgloss.Style {
	switch k {
	case db.KindView, db.KindMatView:
		return th.Keyword
	case db.KindFunction, db.KindProcedure:
		return th.Number
	case db.KindSequence, db.KindType:
		return th.Dim
	}
	return th.Accent
}

func objIcon(k db.Kind) string {
	switch k {
	case db.KindView:
		return "◫"
	case db.KindMatView:
		return "◩"
	case db.KindFunction, db.KindProcedure:
		return "ƒ"
	case db.KindSequence:
		return "#"
	case db.KindType:
		return "T"
	}
	return "▦"
}

// rows flattens the visible list for the current tab and search text.
func (s *sidebar) rows() []sbRow {
	q := s.q.Value()
	var out []sbRow
	switch s.tab {
	case 1:
		qs := append([]store.Query(nil), s.queries...)
		if s.qsort != "" {
			store.SortQueries(qs, s.qsort)
		}
		for _, x := range qs {
			if _, ok := matchName(q, x.Name, true); ok {
				out = append(out, sbRow{kind: sbQuery, q: x})
			}
		}
		return out
	case 2:
		for _, h := range s.hist {
			if strings.Contains(strings.ToLower(h.SQL), strings.ToLower(q)) {
				out = append(out, sbRow{kind: sbHistory, h: h})
			}
		}
		return out
	}
	showRecent, showFn, showSeq, showTypes, fz := s.cfg()
	visible := func(o db.Object) bool {
		switch o.Kind {
		case db.KindFunction, db.KindProcedure:
			return showFn
		case db.KindSequence:
			return showSeq
		case db.KindType:
			return showTypes
		}
		return true
	}
	byKey := map[string]db.Object{}
	for _, o := range s.objs {
		byKey[objKey(o.ObjRef)] = o
	}
	if q != "" { // search: one flat list ranked by score
		type sc struct {
			o db.Object
			s int
		}
		var hits []sc
		for _, o := range s.objs {
			if !visible(o) {
				continue
			}
			if score, ok := matchName(q, o.Name, fz); ok {
				hits = append(hits, sc{o, score})
			}
		}
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].s > hits[j].s })
		for _, h := range hits {
			out = append(out, sbRow{kind: sbObject, obj: h.o})
		}
		return out
	}
	pins := s.w.a.d.State.Pinned(s.w.conn.Name)
	pinned := map[string]bool{}
	if len(pins) > 0 {
		out = append(out, sbRow{kind: sbHeader, text: "★ Pinned"})
		for _, p := range pins {
			if o, ok := byKey[p]; ok {
				out = append(out, sbRow{kind: sbObject, obj: o, star: true})
				pinned[p] = true
			}
		}
	}
	if showRecent {
		var rec []sbRow
		for _, r := range s.w.a.d.State.Recent(s.w.conn.Name) {
			if o, ok := byKey[r]; ok && !pinned[r] {
				rec = append(rec, sbRow{kind: sbObject, obj: o})
			}
		}
		if len(rec) > 0 {
			out = appendSection(out, sbRow{kind: sbHeader, text: "Recent"})
			out = append(out, rec...)
		}
	}
	if s.w.caps.Editor == "redis" {
		if s.open == nil {
			s.open = map[string]bool{}
		}
		out = appendSection(out, sbRow{kind: sbHeader, text: fmt.Sprintf("Keys (%d)", len(s.objs))}) // keytree_test reads this
		s.flattenKeys(buildKeyTree(s.objs), 0, &out)
		return out
	}
	for _, g := range s.w.a.d.State.Groups(s.w.conn.Name) { // custom groups sit above the kind sections
		var rows []sbRow
		for _, k := range g.Items {
			if o, ok := byKey[k]; ok && visible(o) {
				rows = append(rows, sbRow{kind: sbObject, obj: o, star: pinned[k], group: g.Name})
			}
		}
		if len(rows) > 0 {
			out = appendSection(out, sbRow{kind: sbHeader, text: "▣ " + g.Name, count: len(rows)})
			out = append(out, rows...)
		}
	}
	counts := map[db.Kind]int{}
	for _, o := range s.objs {
		if visible(o) {
			counts[o.Kind]++
		}
	}
	section := db.Kind("")
	for _, o := range s.objs {
		if !visible(o) {
			continue
		}
		title := map[db.Kind]string{db.KindTable: "Tables", db.KindView: "Views", db.KindMatView: "Materialized views", db.KindFunction: "Functions",
			db.KindProcedure: "Procedures", db.KindSequence: "Sequences", db.KindType: "Types"}[o.Kind]
		if o.Kind != section {
			out = appendSection(out, sbRow{kind: sbHeader, text: title, count: counts[o.Kind]})
			section = o.Kind
		}
		out = append(out, sbRow{kind: sbObject, obj: o, star: pinned[objKey(o.ObjRef)]})
		out = s.columnRows(out, o)
	}
	return out
}

// columnRows lists the columns of an expanded table (sidebar.show_columns): fetched when first expanded.
func (s *sidebar) columnRows(out []sbRow, o db.Object) []sbRow {
	if !s.cols[objKey(o.ObjRef)] {
		return out
	}
	cols, ok := s.w.meta[o.ObjRef]
	if !ok {
		return append(out, sbRow{kind: sbHeader, text: "   loading columns…"})
	}
	cols = append([]db.Column(nil), cols...)
	if s.w.a.d.Cfg.Sidebar.SortColumns {
		sort.SliceStable(cols, func(i, j int) bool { return strings.ToLower(cols[i].Name) < strings.ToLower(cols[j].Name) })
	}
	for _, c := range cols {
		out = append(out, sbRow{kind: sbColumn, obj: o, label: c.Name, text: c.Type, depth: 1})
	}
	return out
}

// toggleColumns expands or folds the columns under the selected table.
func (s *sidebar) toggleColumns() tea.Cmd {
	r, ok := s.current()
	if !ok || r.kind != sbObject || !s.w.a.d.Cfg.Sidebar.ShowColumns {
		return nil
	}
	switch r.obj.Kind {
	case db.KindTable, db.KindView, db.KindMatView:
	default:
		return nil
	}
	if s.cols == nil {
		s.cols = map[string]bool{}
	}
	k := objKey(r.obj.ObjRef)
	s.cols[k] = !s.cols[k]
	if _, cached := s.w.meta[r.obj.ObjRef]; !s.cols[k] || cached {
		return nil
	}
	return s.fetchCols(r.obj.ObjRef)
}

// fetchCols loads the columns of ref for the expanded view (one fetch per table at a time).
func (s *sidebar) fetchCols(ref db.ObjRef) tea.Cmd {
	if s.colsPending[ref] {
		return nil
	}
	if s.colsPending == nil {
		s.colsPending = map[db.ObjRef]bool{}
	}
	s.colsPending[ref] = true
	caps := s.w.caps
	return s.w.async(-1, func(ctx context.Context, sess db.Session) tea.Msg {
		cols, err := fetchColumns(ctx, sess, caps, ref)
		return colsMetaMsg{ref: ref, cols: cols, err: err}
	})
}

// fetchMissingCols re-requests the columns of every expanded table whose metadata was dropped
// (a commit or a reload clears it), so the rows never stay on "loading columns…".
func (s *sidebar) fetchMissingCols() tea.Cmd {
	var cmds []tea.Cmd
	for _, o := range s.objs {
		if !s.cols[objKey(o.ObjRef)] {
			continue
		}
		if _, ok := s.w.meta[o.ObjRef]; !ok {
			cmds = append(cmds, s.fetchCols(o.ObjRef))
		}
	}
	return tea.Batch(cmds...)
}

// rowID names the row under the cursor independent of its position.
func rowID(r sbRow) string {
	return fmt.Sprintf("%d|%s|%s|%s|%s|%s", r.kind, objKey(r.obj.ObjRef), r.label, r.path, r.q.Path, r.h.ID)
}

// selectedID is the identity of the selected row ("" when none).
func (s *sidebar) selectedID() string {
	if r, ok := s.current(); ok {
		return rowID(r)
	}
	return ""
}

// selectID moves the cursor onto the row with that identity, else onto the nearest selectable row.
func (s *sidebar) selectID(id string) {
	if id != "" {
		for i, r := range s.rows() {
			if r.kind != sbHeader && rowID(r) == id {
				s.sel = i
				return
			}
		}
	}
	s.step(0)
}

// appendSection adds a section header, after a blank spacer row when a section came before it.
// The spacer is a header too, so the cursor skips it.
func appendSection(out []sbRow, h sbRow) []sbRow {
	if len(out) > 0 {
		out = append(out, sbRow{kind: sbHeader})
	}
	return append(out, h)
}

func (s *sidebar) current() (sbRow, bool) {
	rows := s.rows()
	if s.sel >= 0 && s.sel < len(rows) && rows[s.sel].kind != sbHeader {
		return rows[s.sel], true
	}
	return sbRow{}, false
}

// step moves the selection by d rows, skipping headers; d==0 snaps onto the nearest item at or after sel.
func (s *sidebar) step(d int) {
	rows := s.rows()
	if d == 0 {
		d = 1
		if s.sel >= 0 && s.sel < len(rows) && rows[s.sel].kind != sbHeader {
			return
		}
		for i := s.sel; i >= 0 && i < len(rows); i++ {
			if rows[i].kind != sbHeader {
				s.sel = i
				return
			}
		}
		return
	}
	for i := s.sel + d; i >= 0 && i < len(rows); i += d {
		if rows[i].kind != sbHeader {
			s.sel = i
			return
		}
	}
}

func (s *sidebar) switchTab(t int) tea.Cmd {
	s.tab, s.sel, s.top = t, 0, 0
	s.q.Set("")
	s.searching = false
	switch t {
	case 1:
		s.queries, _ = s.w.a.d.Queries.List()
	case 2:
		s.hist, _ = s.w.a.d.Hist.List(s.w.conn.Name, "", 500)
	}
	s.step(0)
	return nil
}

func (s *sidebar) input(msg tea.Msg) tea.Cmd {
	return tea.Batch(s.handleInput(msg), s.fetchMissingCols())
}

func (s *sidebar) handleInput(msg tea.Msg) tea.Cmd {
	if s.searching {
		if k, ok := msg.(tea.KeyPressMsg); ok {
			switch k.Keystroke() {
			case "esc":
				id := s.selectedID() // sel indexes the filtered list: keep the same item selected
				s.searching = false
				s.q.Set("")
				s.selectID(id)
				return nil
			case "enter":
				s.searching = false
				return nil
			case "down":
				s.step(1)
				return nil
			case "up":
				s.step(-1)
				return nil
			}
		}
		if s.q.Handle(msg) {
			s.sel, s.top = 0, 0
			s.step(0)
		}
		return nil
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	before := s.sel
	cmd := s.key(keymap.FromMsg(k))
	if s.sel != before {
		return tea.Batch(cmd, s.schedulePrefetch())
	}
	return cmd
}

// schedulePrefetch loads the column metadata of the highlighted table once the cursor rests on it,
// so pressing Enter only has to fetch rows. Only metadata: rows of an expensive view are never
// queried just because the cursor passed over it.
func (s *sidebar) schedulePrefetch() tea.Cmd {
	r, ok := s.current()
	if !ok || r.kind != sbObject || s.w.a.d.NoTimers {
		return nil
	}
	switch r.obj.Kind {
	case db.KindTable, db.KindView, db.KindMatView:
	default:
		return nil
	}
	if _, cached := s.w.meta[r.obj.ObjRef]; cached {
		return nil
	}
	s.prefetchSeq++
	seq, ref, id, gen := s.prefetchSeq, r.obj.ObjRef, s.w.id, s.w.gen
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return wsMsg{ws: id, tab: -1, gen: gen, msg: prefetchMsg{seq: seq, ref: ref}} })
}

func (s *sidebar) prefetch(m prefetchMsg) tea.Cmd {
	if m.seq != s.prefetchSeq { // the cursor moved on
		return nil
	}
	if _, cached := s.w.meta[m.ref]; cached {
		return nil
	}
	caps := s.w.caps
	return s.w.async(-1, func(ctx context.Context, sess db.Session) tea.Msg {
		cols, err := fetchColumns(ctx, sess, caps, m.ref)
		return colsMetaMsg{ref: m.ref, cols: cols, err: err}
	})
}

func (s *sidebar) key(k string) tea.Cmd {
	switch k {
	case "down", "j":
		s.step(1)
	case "up", "k":
		s.step(-1)
	case "pgdown":
		for i := 0; i < 10; i++ {
			s.step(1)
		}
	case "pgup":
		for i := 0; i < 10; i++ {
			s.step(-1)
		}
	case "home", "g":
		s.sel = 0
		s.step(0)
	case "end", "G":
		s.sel = len(s.rows()) - 1
	case "/":
		s.searching = true
	case "esc":
		s.q.Set("")
		s.marks = nil
	case "1":
		return s.switchTab(0)
	case "2":
		return s.switchTab(1)
	case "3":
		return s.switchTab(2)
	case "right", "l":
		if r, ok := s.current(); ok && r.kind == sbFolder {
			s.open[r.path] = true
			return nil
		}
		return s.activate(false)
	case "left", "h":
		if r, ok := s.current(); ok && r.kind == sbFolder && r.open {
			s.open[r.path] = false
		}
	case "space":
		if r, ok := s.current(); ok && r.kind == sbFolder {
			s.open[r.path] = !s.open[r.path]
		} else if ok && r.kind == sbObject {
			return s.toggleColumns()
		}
	case "enter":
		return s.activate(false)
	case "o":
		return s.activate(true)
	case "m":
		return s.menu()
	case "+":
		return s.newMenu()
	case "p":
		return s.togglePin()
	case "N":
		return s.w.newTableDialog()
	case "s":
		return s.pickSchema()
	case "r":
		return s.load()
	case "S":
		return s.sortOrSave()
	case "i":
		return s.importFile()
	case "f":
		return s.openFile()
	}
	return nil
}

func (s *sidebar) activate(newTab bool) tea.Cmd {
	r, ok := s.current()
	if !ok {
		return nil
	}
	switch r.kind {
	case sbFolder:
		if r.hasObj && !newTab { // a key that is also a prefix: Enter opens it, h/l fold
			return s.w.openObject(r.obj.ObjRef, false)
		}
		s.open[r.path] = !s.open[r.path]
	case sbObject, sbColumn:
		return s.w.openObject(r.obj.ObjRef, newTab)
	case sbQuery:
		return s.w.openSaved(r.q)
	case sbHistory:
		return s.w.newQueryTab(r.h.SQL)
	}
	return nil
}

func (s *sidebar) togglePin() tea.Cmd {
	r, ok := s.current()
	if !ok || r.kind != sbObject {
		return nil
	}
	on, err := s.w.a.d.State.TogglePin(s.w.conn.Name, objKey(r.obj.ObjRef))
	if err != nil {
		return s.w.a.fail(err)
	}
	if on {
		return s.w.a.say("pinned %s", r.obj.Name)
	}
	return s.w.a.say("unpinned %s", r.obj.Name)
}

func (s *sidebar) pickSchema() tea.Cmd {
	if !s.w.caps.Schemas {
		return nil
	}
	var items []pickItem
	for _, sc := range s.schemas {
		d := ""
		if sc == s.schema {
			d = "current"
		}
		items = append(items, pickItem{Label: sc, Detail: d, Value: sc})
	}
	s.w.a.push(&pickerDlg{title: "Schema", filter: true, items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		s.schema, s.sel, s.top = it.Value.(string), 0, 0
		return s.load()
	}})
	return nil
}

func (s *sidebar) action(name string) (bool, tea.Cmd) {
	switch name {
	case "sidebar_search":
		s.searching = true
		return true, nil
	case "delete_item":
		return true, s.adminOp(db.OpDropObject)
	case "truncate":
		return true, s.adminOp(db.OpTruncate)
	case "mark":
		return true, s.toggleMark()
	case "new_schema":
		return true, s.newSchema()
	}
	return false, nil
}

// menu is the item context menu.
func (s *sidebar) menu() tea.Cmd {
	r, ok := s.current()
	if !ok {
		return nil
	}
	if r.kind == sbQuery {
		return s.queryMenu(r.q)
	}
	if r.kind == sbHistory {
		return s.historyMenu(r.h)
	}
	if r.kind != sbObject {
		return nil
	}
	return s.objectMenu(r)
}

// databasePicker lists databases: Enter opens one, Ctrl/Alt+Enter opens its menu (open, rename, drop).
func (s *sidebar) databasePicker(dbs []string) tea.Cmd {
	cur := s.w.sess.CurrentDatabase()
	noun := s.w.caps.DatabaseNoun
	items := []pickItem{{Label: "＋ New " + noun + "…", Value: "\x00new"}}
	for _, d := range dbs {
		mark := ""
		if d == cur {
			mark = "current"
		}
		items = append(items, pickItem{Label: d, Detail: mark, Value: d})
	}
	s.w.a.push(&pickerDlg{title: "Open " + noun, filter: true, items: items, hint: "Enter: open   Ctrl/Alt+Enter: rename or drop   Esc: cancel",
		onPick: func(it pickItem, ctrl bool) tea.Cmd {
			name := it.Value.(string)
			switch {
			case name == "\x00new":
				return s.newDatabase()
			case ctrl:
				return s.databaseMenu(name)
			}
			return s.w.openDatabase(name)
		}})
	return nil
}

func (s *sidebar) newDatabase() tea.Cmd {
	noun := s.w.caps.DatabaseNoun
	s.w.a.push(newForm("New "+noun, []formField{
		{Key: "name", Label: "Name", Kind: fText},
		{Key: "encoding", Label: "Encoding", Kind: fText, Show: func(map[string]string) bool { return s.w.caps.Dialect == "postgres" || s.w.caps.Dialect == "mysql" }},
		{Key: "collation", Label: "Collation", Kind: fText, Show: func(map[string]string) bool { return s.w.caps.Dialect == "postgres" || s.w.caps.Dialect == "mysql" }},
		{Key: "strategy", Label: "Replication strategy", Kind: fText, Value: "SimpleStrategy", Show: func(map[string]string) bool { return s.w.caps.Dialect == "cql" }},
		{Key: "replication", Label: "Replication (factor, or dc1:3,dc2:2)", Kind: fText, Value: "1", Show: func(map[string]string) bool { return s.w.caps.Dialect == "cql" }},
	}, func(v map[string]string) tea.Cmd {
		name := strings.TrimSpace(v["name"])
		if name == "" {
			return s.w.a.fail(fmt.Errorf("a %s needs a name", noun))
		}
		return s.databaseAdmin(db.OpCreateDatabase, name, map[string]string{"encoding": v["encoding"], "collation": v["collation"], "strategy": v["strategy"], "replication": v["replication"]}, "created")
	}))
	return nil
}

func (s *sidebar) databaseAdmin(op db.AdminOp, name string, opts map[string]string, done string) tea.Cmd {
	stmts, err := s.w.sess.PlanAdmin(op, db.ObjRef{Name: name}, opts)
	if err != nil {
		return s.w.a.fail(err)
	}
	return s.w.execute(stmts, func(err error) tea.Cmd {
		if err != nil {
			return nil
		}
		return tea.Batch(s.w.a.say("%s %s", name, done), s.w.pickDatabase())
	})
}

type scriptMsg struct {
	text string
	err  error
}

func (s *sidebar) adminOp(op db.AdminOp) tea.Cmd {
	if refs := s.markedRefs(); len(refs) > 0 { // marked tables win over the one under the cursor
		return s.adminMany(op, refs)
	}
	r, ok := s.current()
	if !ok || r.kind != sbObject {
		return nil
	}
	ref := r.obj.ObjRef
	opts := map[string]string{}
	run := func() tea.Cmd {
		stmts, err := s.w.sess.PlanAdmin(op, ref, opts)
		if err != nil {
			return s.w.a.fail(err)
		}
		return s.w.execute(stmts, func(err error) tea.Cmd {
			if err != nil {
				return nil
			}
			s.w.closeTabsFor(ref, op == db.OpDropObject)
			return tea.Batch(s.load(), s.w.a.say("%s %s done", op, ref.Name))
		})
	}
	verb := map[db.AdminOp]string{db.OpDropObject: "Delete", db.OpTruncate: "Truncate"}[op]
	f := newForm(verb+" "+ref.Name+"?", []formField{
		{Key: "cascade", Label: "Cascade", Kind: fBool, Value: "false"},
		{Key: "restart", Label: "Restart identity", Kind: fBool, Value: "false", Show: func(map[string]string) bool { return op == db.OpTruncate && s.w.caps.Dialect == "postgres" }},
		{Key: "warn", Label: "", Kind: fLabel, Value: "This cannot be undone. Type the name to confirm."},
		{Key: "confirm", Label: "Type its name", Kind: fText},
	}, func(v map[string]string) tea.Cmd {
		opts["cascade"], opts["restart_identity"] = v["cascade"], v["restart"]
		return run()
	})
	f.validate = func(v map[string]string) error {
		if v["confirm"] != ref.Name {
			return fmt.Errorf("type %q to confirm", ref.Name)
		}
		return nil
	}
	f.idx = 3 // start on the confirmation field
	f.submit = "Enter: " + strings.ToLower(verb)
	s.w.a.push(f)
	return nil
}

func (s *sidebar) cloneDialog(ref db.ObjRef) tea.Cmd {
	withData := "true"
	if s.w.caps.Dialect == "cql" { // Cassandra cannot copy rows: default to structure only
		withData = "false"
	}
	s.w.a.push(newForm("Clone "+ref.Name, []formField{
		{Key: "new_name", Label: "New name", Kind: fText, Value: ref.Name + "_clone"},
		{Key: "with_data", Label: "Duplicate content", Kind: fBool, Value: withData},
	}, func(v map[string]string) tea.Cmd {
		stmts, err := s.w.sess.PlanAdmin(db.OpClone, ref, map[string]string{"new_name": v["new_name"], "with_data": v["with_data"]})
		if err != nil {
			return s.w.a.fail(err)
		}
		return s.w.execute(stmts, func(err error) tea.Cmd {
			if err != nil {
				return nil
			}
			return s.load()
		})
	}))
	return nil
}

func (s *sidebar) renameDialog(ref db.ObjRef) tea.Cmd {
	s.w.a.push(newForm("Rename "+ref.Name, []formField{{Key: "new_name", Label: "New name", Kind: fText, Value: ref.Name}},
		func(v map[string]string) tea.Cmd {
			stmts, err := s.w.sess.PlanAdmin(db.OpRenameObject, ref, map[string]string{"new_name": v["new_name"]})
			if err != nil {
				return s.w.a.fail(err)
			}
			return s.w.execute(stmts, func(err error) tea.Cmd {
				if err != nil {
					return nil
				}
				s.w.closeTabsFor(ref, true)
				return s.load()
			})
		}))
	return nil
}

func (w *Workspace) closeTabsFor(ref db.ObjRef, closeThem bool) {
	w.forget(&ref) // its structure (or existence) just changed
	for i := len(w.tabs) - 1; i >= 0; i-- {
		if t, ok := w.tabs[i].(*tableTab); ok && t.ref == ref && closeThem {
			w.closeTab(i)
		}
	}
}

// title is the sidebar's tab strip, set into the frame's top edge like the main pane's tabs.
func (s *sidebar) title(th Theme) string {
	return th.segmented([]string{"Objects", "Queries", "History"}, nil, s.tab)
}

func (s *sidebar) hint(th Theme) string {
	switch s.tab {
	case 1:
		return th.chips("↵", "open", "S", "sort", "i", "import .sql", "f", "open file")
	case 2:
		return th.chips("↵", "open", "S", "save as", "i", "import")
	}
	if len(s.marks) > 0 {
		return th.chips(fmt.Sprint(len(s.marks)), "marked", "↵", "open", "m", "menu")
	}
	return th.chips("↵", "open", "/", "find", "m", "menu")
}

func (s *sidebar) view(th Theme, W, H int, focused bool) string {
	var head []string
	where := s.w.sess.CurrentDatabase()
	if s.w.caps.Schemas {
		where += "." + s.schema
	}
	switch {
	case s.searching || s.q.Value() != "":
		head = append(head, " "+th.Accent.Render("/")+" "+s.q.View(W-4, th, s.searching))
	case s.tab == 0 && s.w.caps.Schemas:
		head = append(head, fit(" "+th.Bold.Render(where)+th.Dim.Render("  s switch schema"), W))
	case s.tab == 0:
		head = append(head, fit(" "+th.Bold.Render(where), W))
	case s.tab == 1:
		head = append(head, fit(" "+th.Bold.Render("Saved queries")+th.Dim.Render(fmt.Sprintf(" · %d", len(s.queries))), W))
	default:
		head = append(head, fit(" "+th.Bold.Render(s.w.conn.Name)+th.Dim.Render("  newest first"), W))
	}
	head = append(head, "")
	rows := s.rows()
	listH := max(1, H-len(head))
	s.sel = clamp(s.sel, 0, max(0, len(rows)-1))
	if s.sel < s.top {
		s.top = s.sel
	}
	if s.sel >= s.top+listH {
		s.top = s.sel - listH + 1
	}
	pk := func(action string) string { // a prefixed key for the empty-state chips
		return keymap.Display(s.w.a.d.Keys.Prefix) + " " + keymap.Display(firstKey(s.w.a.d.Keys, action))
	}
	var body []string
	switch {
	case s.loading:
		body = append(body, th.Warn.Render(" "+s.w.a.spinner())+th.Dim.Render(" loading…"))
	case s.err != "":
		body = append(body, " "+th.Err.Bold(true).Render("✗ Could not load"))
		for _, l := range wrapText(s.err, max(8, W-3)) {
			body = append(body, "   "+l)
		}
		body = append(body, "", " "+th.chips("r", "reload"))
	case len(rows) == 0 && s.q.Value() != "":
		body = append(body, th.Dim.Render(" Nothing matches"), "", " "+th.chips("Esc", "clear search"))
	case len(rows) == 0 && s.tab == 1:
		body = append(body, th.Dim.Render(" No saved queries yet"), "", " "+th.chips(pk("new_query_tab"), "new query"),
			" "+th.Dim.Render("then ")+th.chip(pk("add_to_queries"))+th.Dim.Render(" saves it"))
	case len(rows) == 0 && s.tab == 2:
		body = append(body, th.Dim.Render(" Nothing run yet"), "", " "+th.chips(pk("new_query_tab"), "new query"))
	case len(rows) == 0:
		body = append(body, th.Dim.Render(" Nothing here yet"), "", " "+th.chips("N", "new table"))
	}
	for i := s.top; i < min(len(rows), s.top+listH) && len(body) < listH; i++ {
		r := rows[i]
		var line string
		switch r.kind {
		case sbHeader:
			count := ""
			if r.count > 0 {
				count = th.Dim.Render(fmt.Sprintf(" · %d", r.count))
			}
			line = fit(" "+th.Section.Render(r.text)+count, W)
		case sbObject:
			mark := " "
			if r.star {
				mark = th.Warn.Render("★")
			}
			if s.marks[objKey(r.obj.ObjRef)] {
				mark = th.OK.Render("✓")
			}
			detail := ""
			if r.obj.Detail != "" {
				detail = th.Dim.Render(" " + r.obj.Detail)
			}
			name := r.obj.Name
			if r.label != "" {
				name = r.label
			}
			arrow := ""
			if s.w.a.d.Cfg.Sidebar.ShowColumns && r.obj.Kind != db.KindFunction && r.obj.Kind != db.KindProcedure && r.obj.Kind != db.KindSequence && r.obj.Kind != db.KindType {
				arrow = th.Dim.Render("▸ ")
				if s.cols[objKey(r.obj.ObjRef)] {
					arrow = th.Dim.Render("▾ ")
				}
			}
			if !s.w.a.d.Cfg.Sidebar.LazyLoad && r.obj.Schema != "" && s.w.caps.Schemas {
				name += th.Dim.Render(" " + r.obj.Schema)
			}
			line = fit(" "+strings.Repeat("  ", r.depth)+mark+arrow+objStyle(th, r.obj.Kind).Render(objIcon(r.obj.Kind))+" "+name+detail, W)
		case sbColumn:
			line = fit("     "+th.Dim.Render("· ")+r.label+th.Dim.Render("  "+r.text), W)
		case sbFolder:
			arrow := "▸"
			if r.open {
				arrow = "▾"
			}
			line = fit(" "+strings.Repeat("  ", r.depth)+th.Accent.Render(arrow)+" "+r.text+th.Dim.Render(fmt.Sprintf("  %d", r.count)), W)
		case sbQuery:
			name := th.Bold.Render(r.q.Name)
			if r.q.Folder != "" {
				name = th.Dim.Render(r.q.Folder+"/") + name
			}
			kw := ""
			if len(r.q.Keywords) > 0 {
				kw = th.Dim.Render("  " + strings.Join(r.q.Keywords, ", "))
			}
			icon := th.Accent.Render("≡")
			if r.q.Shared {
				icon = th.Dim.Render("⇄")
			}
			line = fit(" "+icon+" "+name+kw, W)
		case sbHistory:
			when := r.h.Time.Format("Jan 02")
			if r.h.Time.Format(time.DateOnly) == time.Now().Format(time.DateOnly) { // today: the time is enough
				when = r.h.Time.Format("15:04")
			}
			line = fit(" "+th.Dim.Render(fit(when, 6))+" "+strings.Join(strings.Fields(r.h.SQL), " "), W)
		}
		if i == s.sel && r.kind != sbHeader {
			st := th.SelRow
			if !focused {
				st = th.SelRow.Faint(true)
			}
			line = st.Render(stripANSI(line))
		}
		body = append(body, line)
	}
	return strings.Join(padLines(append(head, body...), W, H), "\n")
}
