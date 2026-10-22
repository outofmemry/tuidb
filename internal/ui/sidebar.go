package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"tuidb/internal/db"
	"tuidb/internal/keymap"
	"tuidb/internal/store"
)

type sbKind int

const (
	sbHeader sbKind = iota
	sbObject
	sbQuery
	sbHistory
	sbFolder // a Redis key prefix such as "user:"
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
	s.loading = true
	w, schema, hasSchemas := s.w, s.schema, s.w.caps.Schemas
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
		m.objs, m.err = sess.Objects(ctx, schema)
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
		s.err, s.objs, s.schemas = "", m.objs, m.schemas
		defer s.step(0)
		sort.SliceStable(s.objs, func(i, j int) bool {
			if s.objs[i].Kind != s.objs[j].Kind {
				return kindOrder(s.objs[i].Kind) < kindOrder(s.objs[j].Kind)
			}
			return strings.ToLower(s.objs[i].Name) < strings.ToLower(s.objs[j].Name)
		})
	case dbListMsg:
		if m.err == nil {
			s.dbs = m.dbs
		}
		return nil
	case prefetchMsg:
		return s.prefetch(m)
	case databasesMsg:
		if m.err != nil {
			return s.w.a.fail(m.err)
		}
		s.dbs = m.dbs
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
		s.sel, s.top = 0, 0
		s.w.forget(nil)
		return tea.Batch(s.load(), s.w.a.say("now using %s", m.name))
	}
	if cmd, ok := s.w.handleGlobal(msg); ok {
		return cmd
	}
	return nil
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
		for _, x := range s.queries {
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
			out = append(out, sbRow{kind: sbHeader, text: "Recent"})
			out = append(out, rec...)
		}
	}
	if s.w.caps.Editor == "redis" {
		if s.open == nil {
			s.open = map[string]bool{}
		}
		out = append(out, sbRow{kind: sbHeader, text: fmt.Sprintf("Keys (%d)", len(s.objs))})
		s.flattenKeys(buildKeyTree(s.objs), 0, &out)
		return out
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
			out = append(out, sbRow{kind: sbHeader, text: fmt.Sprintf("%s (%d)", title, counts[o.Kind])})
			section = o.Kind
		}
		out = append(out, sbRow{kind: sbObject, obj: o, star: pinned[objKey(o.ObjRef)]})
	}
	return out
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
	if s.searching {
		if k, ok := msg.(tea.KeyPressMsg); ok {
			switch k.Keystroke() {
			case "esc":
				s.searching = false
				s.q.Set("")
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
		}
	case "enter":
		return s.activate(false)
	case "o":
		return s.activate(true)
	case "m":
		return s.menu()
	case "p":
		return s.togglePin()
	case "N":
		return s.w.newTableDialog()
	case "s":
		return s.pickSchema()
	case "r":
		return s.load()
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
	case sbObject:
		return s.w.openObject(r.obj.ObjRef, newTab)
	case sbQuery:
		return s.w.newQueryTab(r.q.SQL)
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
	if r.kind != sbObject {
		return nil
	}
	ref := r.obj.ObjRef
	isTable := ref.Kind == db.KindTable || ref.Kind == db.KindView || ref.Kind == db.KindMatView
	items := []pickItem{{Label: "Open", Value: "open"}, {Label: "Open in new tab", Value: "new"}, {Label: "New table…", Value: "newtable"}}
	if isTable {
		items = append(items, pickItem{Label: "Open structure", Value: "structure"})
	}
	if ref.Kind == db.KindView || ref.Kind == db.KindMatView || ref.Kind == db.KindFunction || ref.Kind == db.KindProcedure {
		items = append(items, pickItem{Label: "Edit definition", Value: "edit"})
	}
	if ref.Kind == db.KindTable && s.w.caps.Triggers {
		items = append(items, pickItem{Label: "Triggers…", Value: "triggers"})
	}
	items = append(items, pickItem{Label: "Show DDL", Value: "ddl"}, pickItem{Label: "Copy name", Value: "name"},
		pickItem{Label: "Copy script: CREATE", Value: "script:create"}, pickItem{Label: "Copy script: DROP", Value: "script:drop"})
	if ref.Kind == db.KindTable {
		items = append(items, pickItem{Label: "Copy script: TRUNCATE", Value: "script:truncate"}, pickItem{Label: "Copy script: SELECT top 100", Value: "script:select100"},
			pickItem{Label: "Clone…", Value: "clone"}, pickItem{Label: "Rename…", Value: "rename"}, pickItem{Label: "Truncate…", Value: "truncate"})
	}
	items = append(items, pickItem{Label: "Delete…", Value: "drop"}, pickItem{Label: "Pin / unpin", Value: "pin"})
	s.w.a.push(&pickerDlg{title: ref.Name, items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		v := it.Value.(string)
		switch {
		case v == "open":
			return s.w.openObject(ref, false)
		case v == "new":
			return s.w.openObject(ref, true)
		case v == "structure":
			return s.w.openStructure(ref)
		case v == "newtable":
			return s.w.newTableDialog()
		case v == "ddl":
			return s.w.showDDL(ref)
		case v == "edit":
			return s.w.editDefinition(ref)
		case v == "triggers":
			return s.w.triggerMenu(ref)
		case v == "name":
			return tea.Batch(tea.SetClipboard(ref.Name), s.w.a.say("copied %s", ref.Name))
		case strings.HasPrefix(v, "script:"):
			kind := db.ScriptKind(strings.TrimPrefix(v, "script:"))
			return s.w.async(-1, func(ctx context.Context, sess db.Session) tea.Msg {
				text, err := sess.Script(ctx, kind, ref)
				return scriptMsg{text: text, err: err}
			})
		case v == "pin":
			return s.togglePin()
		case v == "clone":
			return s.cloneDialog(ref)
		case v == "rename":
			return s.renameDialog(ref)
		case v == "truncate":
			return s.adminOp(db.OpTruncate)
		case v == "drop":
			return s.adminOp(db.OpDropObject)
		}
		return nil
	}})
	return nil
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
	}, func(v map[string]string) tea.Cmd {
		name := strings.TrimSpace(v["name"])
		if name == "" {
			return s.w.a.fail(fmt.Errorf("a %s needs a name", noun))
		}
		return s.databaseAdmin(db.OpCreateDatabase, name, map[string]string{"encoding": v["encoding"], "collation": v["collation"]}, "created")
	}))
	return nil
}

func (s *sidebar) databaseMenu(name string) tea.Cmd {
	items := []pickItem{{Label: "Open", Value: "open"}, {Label: "Rename…", Value: "rename"}, {Label: "Drop…", Value: "drop"}}
	s.w.a.push(&pickerDlg{title: name, items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		switch it.Value.(string) {
		case "open":
			return s.w.openDatabase(name)
		case "rename":
			if name == s.w.sess.CurrentDatabase() {
				return s.w.a.say("the open database cannot be renamed; open another one first")
			}
			s.w.a.push(newForm("Rename "+name, []formField{{Key: "new_name", Label: "New name", Kind: fText, Value: name}}, func(v map[string]string) tea.Cmd {
				return s.databaseAdmin(db.OpRenameDatabase, name, map[string]string{"new_name": strings.TrimSpace(v["new_name"])}, "renamed")
			}))
		case "drop":
			if name == s.w.sess.CurrentDatabase() {
				return s.w.a.say("the open database cannot be dropped; open another one first")
			}
			f := newForm("Drop "+name+"?", []formField{
				{Key: "warn", Label: "", Kind: fLabel, Value: "This deletes the database and everything in it."},
				{Key: "confirm", Label: "Type its name", Kind: fText},
			}, func(map[string]string) tea.Cmd { return s.databaseAdmin(db.OpDropDatabase, name, nil, "dropped") })
			f.validate = func(v map[string]string) error {
				if v["confirm"] != name {
					return fmt.Errorf("type %q to confirm", name)
				}
				return nil
			}
			f.submit = "Enter: drop"
			s.w.a.push(f)
		}
		return nil
	}})
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

func (s *sidebar) queryMenu(q store.Query) tea.Cmd {
	items := []pickItem{{Label: "Open in new tab", Value: "open"}, {Label: "Run", Value: "run"}, {Label: "Copy", Value: "copy"}, {Label: "Delete…", Value: "delete"}}
	s.w.a.push(&pickerDlg{title: q.Name, items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		switch it.Value.(string) {
		case "open":
			return s.w.newQueryTab(q.SQL)
		case "run":
			cmd := s.w.newQueryTab(q.SQL)
			if t, ok := s.w.tab().(*queryTab); ok {
				return tea.Batch(cmd, t.runAll())
			}
			return cmd
		case "copy":
			return tea.SetClipboard(q.SQL)
		case "delete":
			s.w.a.push(&confirmDlg{title: "Delete saved query " + q.Name + "?", yes: "Delete", danger: true, onYes: func() tea.Cmd {
				if err := s.w.a.d.Queries.Delete(q.Path); err != nil {
					return s.w.a.fail(err)
				}
				s.queries, _ = s.w.a.d.Queries.List()
				return nil
			}})
		}
		return nil
	}})
	return nil
}

func (s *sidebar) adminOp(op db.AdminOp) tea.Cmd {
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
	s.w.a.push(newForm("Clone "+ref.Name, []formField{
		{Key: "new_name", Label: "New name", Kind: fText, Value: ref.Name + "_clone"},
		{Key: "with_data", Label: "Duplicate content", Kind: fBool, Value: "true"},
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

func (s *sidebar) title() string {
	t := []string{"Objects", "Saved queries", "History"}[s.tab]
	sub := s.w.sess.CurrentDatabase()
	if s.w.caps.Schemas {
		sub += "." + s.schema
	}
	return t + " · " + sub
}

func (s *sidebar) hint(th Theme) string {
	if s.tab != 0 {
		return th.chips("↵", "open", "m", "menu")
	}
	return th.chips("↵", "open", "/", "find", "m", "menu")
}

func (s *sidebar) view(th Theme, W, H int, focused bool) string {
	keys := []string{"1", "2", "3"}
	if W < 36 {
		keys = nil // the key numbers do not fit in a narrow sidebar; the hint line still says 1-3
	}
	head := []string{th.segmented([]string{"Objects", "Queries", "History"}, keys, s.tab)}
	if s.searching || s.q.Value() != "" {
		head = append(head, th.Accent.Render("/")+s.q.View(W-2, th, s.searching))
	} else if s.w.caps.Schemas && s.tab == 0 {
		head = append(head, th.Dim.Render(fit(" schema "+s.schema+"  (s to switch)", W)))
	} else {
		head = append(head, "")
	}
	rows := s.rows()
	listH := max(1, H-len(head))
	s.sel = clamp(s.sel, 0, max(0, len(rows)-1))
	if s.sel < s.top {
		s.top = s.sel
	}
	if s.sel >= s.top+listH {
		s.top = s.sel - listH + 1
	}
	var body []string
	switch {
	case s.loading:
		body = append(body, th.Dim.Render(" loading…"))
	case s.err != "":
		body = append(body, th.Err.Render(fit(" "+s.err, W)))
	case len(rows) == 0 && s.q.Value() != "":
		body = append(body, th.Dim.Render(" nothing matches"))
	case len(rows) == 0:
		body = append(body, th.Dim.Render(" nothing here yet"))
	}
	for i := s.top; i < min(len(rows), s.top+listH) && len(body) < listH; i++ {
		r := rows[i]
		var line string
		switch r.kind {
		case sbHeader:
			line = th.Section.Render(fit(" "+r.text, W))
		case sbObject:
			mark := " "
			if r.star {
				mark = th.Warn.Render("★")
			}
			detail := ""
			if r.obj.Detail != "" {
				detail = th.Dim.Render(" " + r.obj.Detail)
			}
			name := r.obj.Name
			if r.label != "" {
				name = r.label
			}
			line = fit(" "+strings.Repeat("  ", r.depth)+mark+th.Accent.Render(objIcon(r.obj.Kind))+" "+name+detail, W)
		case sbFolder:
			arrow := "▸"
			if r.open {
				arrow = "▾"
			}
			line = fit(" "+strings.Repeat("  ", r.depth)+th.Accent.Render(arrow)+" "+r.text+th.Dim.Render(fmt.Sprintf("  %d", r.count)), W)
		case sbQuery:
			line = fit(" ƒ "+r.q.Name, W)
		case sbHistory:
			line = fit(" "+r.h.Time.Format("01-02 15:04")+" "+strings.Join(strings.Fields(r.h.SQL), " "), W)
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
