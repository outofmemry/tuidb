package ui

import (
	"context"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"tuidb/internal/db"
	"tuidb/internal/keymap"
)

// A query result is editable only when it provably maps to one table: a plain
// "SELECT cols FROM table [WHERE|ORDER BY|LIMIT]" whose select list has no aliases or expressions
// and includes the table's whole primary key. Anything else stays read-only, because an edit
// built from a guess could change the wrong rows.
var (
	selectRe = regexp.MustCompile("(?is)^\\s*select\\s+(.+?)\\s+from\\s+((?:\"[^\"]+\"|`[^`]+`|\\w+)(?:\\.(?:\"[^\"]+\"|`[^`]+`|\\w+))?)\\s*(?:(?:where|order\\s+by|limit|offset)\\b.*)?;?\\s*$")
	listRe   = regexp.MustCompile("^\\s*(\\*|(?:\"[^\"]+\"|`[^`]+`|[\\w.]+)(?:\\s*,\\s*(?:\"[^\"]+\"|`[^`]+`|[\\w.]+))*)\\s*$")
	badRe    = regexp.MustCompile(`(?i)\b(join|union|intersect|except|group\s+by|having|distinct|into|over|for\s+update|with)\b|\(`)
)

// singleTable returns the schema and table of a simple single-table SELECT.
func singleTable(stmt string) (schema, table string, ok bool) {
	m := selectRe.FindStringSubmatch(stmt)
	// shortcut: any "(" or keyword like JOIN/WITH anywhere (even in a string) disables editing; safe, a little strict
	if m == nil || !listRe.MatchString(m[1]) || badRe.MatchString(stmt) {
		return "", "", false
	}
	unq := func(s string) string { return strings.Trim(s, "\"`") }
	parts := strings.Split(m[2], ".")
	if len(parts) == 2 {
		return unq(parts[0]), unq(parts[1]), true
	}
	return "", unq(parts[0]), true
}

type resultColsMsg struct {
	batch int
	idx   int
	ref   db.ObjRef
	cols  []db.Column
	err   error
}

// makeEditable looks up the table behind result idx and, when its key is in the result, unlocks editing.
func (t *queryTab) makeEditable(idx int) tea.Cmd {
	rv := t.res[idx]
	schema, table, ok := singleTable(rv.stmt)
	if !ok || !rv.hasRows || t.ws.caps.Editor != "sql" {
		return nil
	}
	var ref *db.ObjRef
	if schema == "" { // an unqualified name resolves against the session's schema, not whatever the sidebar shows
		schema = t.ws.sess.CurrentSchema()
	}
	for _, o := range t.ws.sb.objs {
		if o.Kind == db.KindTable && strings.EqualFold(o.Name, table) && (schema == "" || strings.EqualFold(o.Schema, schema)) {
			r := o.ObjRef
			ref = &r
			break
		}
	}
	if ref == nil {
		return nil
	}
	batch, r, caps := t.batch, *ref, t.ws.caps
	if cols, ok := t.ws.meta[r]; ok {
		return func() tea.Msg {
			return wsMsg{ws: t.ws.id, tab: t.id, gen: t.ws.gen, msg: resultColsMsg{batch: batch, idx: idx, ref: r, cols: cols}}
		}
	}
	return t.ws.async(t.id, func(ctx context.Context, sess db.Session) tea.Msg {
		cols, err := fetchColumns(ctx, sess, caps, r)
		return resultColsMsg{batch: batch, idx: idx, ref: r, cols: cols, err: err}
	})
}

func (t *queryTab) applyResultCols(m resultColsMsg) {
	if m.batch != t.batch || m.err != nil || m.idx >= len(t.res) {
		return
	}
	rv := t.res[m.idx]
	if rv.grid == nil {
		return
	}
	t.ws.meta[m.ref] = m.cols
	pk := map[string]bool{}
	nPK := 0
	for _, c := range m.cols {
		if c.PK {
			pk[c.Name] = true
			nPK++
		}
	}
	have := map[string]bool{}
	for i := range rv.grid.Cols {
		rv.grid.Cols[i].PK = pk[rv.grid.Cols[i].Name]
		if rv.grid.Cols[i].PK {
			have[rv.grid.Cols[i].Name] = true
		}
	}
	if nPK > 0 && len(have) == nPK { // the whole key is in the result
		ref := m.ref
		rv.ref, rv.grid.Editable = &ref, true
	}
}

func (t *queryTab) curResult() *resultView {
	if t.rc >= 0 && t.rc < len(t.res) {
		return t.res[t.rc]
	}
	return nil
}

func (t *queryTab) editableResult() *resultView {
	if rv := t.curResult(); rv != nil && rv.grid != nil && rv.grid.Editable && rv.ref != nil {
		return rv
	}
	return nil
}

// resultEditKeys handles typing into a result cell; it reports whether the key was consumed.
func (t *queryTab) resultEditKeys(g *Grid, msg tea.Msg) bool {
	if !g.Editing() {
		return false
	}
	if p, ok := msg.(tea.PasteMsg); ok {
		g.edit.Handle(p)
		return true
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return true
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
	return true
}

// Dirty / Statements / Commit / Discard make results behave like a table tab for the workspace commit keys.
func (t *queryTab) Dirty() bool {
	for _, r := range t.res {
		if r.grid != nil && r.grid.HasPending() {
			return true
		}
	}
	return false
}

func (t *queryTab) Statements() ([]db.Statement, error) {
	rv := t.editableResult()
	if rv == nil {
		return nil, nil
	}
	return t.ws.sess.Plan(rv.grid.Changes(*rv.ref))
}

func (t *queryTab) Commit() tea.Cmd {
	rv := t.editableResult()
	if rv == nil || !rv.grid.HasPending() {
		return t.ws.a.say("no pending result edits")
	}
	for _, r := range t.res {
		if r != rv && r.grid != nil && r.grid.HasPending() {
			return t.ws.a.say("another result has edits too: commit one result at a time (switch with [ and ])")
		}
	}
	if t.running {
		return t.ws.a.say("a query is running; wait for it")
	}
	stmts, err := t.Statements()
	if err != nil {
		return t.ws.a.fail(err)
	}
	idx := t.rc
	return t.ws.execute(stmts, func(err error) tea.Cmd {
		if err != nil || t.closed {
			return nil
		}
		// refresh only this result in place so other results (and their edits) stay
		rv.grid.DiscardPending()
		t.replace, t.running, t.queue, t.batch = idx, true, []db.Stmt{{Text: rv.stmt}}, t.batch+1
		t.started = time.Now()
		t.focusResults = true
		return tea.Batch(t.next(), t.ws.a.say("committed %d statement(s)", len(stmts)))
	})
}

func (t *queryTab) Discard() {
	if rv := t.editableResult(); rv != nil {
		rv.grid.DiscardPending()
	}
}

// resultAction runs grid editing actions on an editable result.
func (t *queryTab) resultAction(name string) (bool, tea.Cmd) {
	rv := t.editableResult()
	if rv == nil || !t.focusResults {
		return false, nil
	}
	g := rv.grid
	switch name {
	case "add_row":
		g.AddRow()
	case "duplicate_rows":
		g.DuplicateRows()
	case "delete_rows":
		g.DeleteRows()
	case "set_value":
		items := []pickItem{{Label: "NULL", Value: db.Null}, {Label: "DEFAULT", Value: db.Default}, {Label: "EMPTY", Value: db.Empty}}
		t.ws.a.push(&pickerDlg{title: "Set value", items: items, onPick: func(it pickItem, _ bool) tea.Cmd { g.SetCell(it.Value); return nil }})
	case "paste_rows", "paste_cells":
		return true, t.ws.a.say("paste into the table tab; results take typed edits")
	default:
		return false, nil
	}
	return true, nil
}

func resultHint(th Theme, rv *resultView) string {
	if rv != nil && rv.grid != nil && rv.grid.Editable {
		return th.chips("↵", "edit", "[ ]", "result tab", keymap.Display("ctrl+d")+" s", "commit", "Esc", "editor")
	}
	return th.chips("↵", "quick look", "[ ]", "result tab", "p", "pin", "/", "find", "Esc", "editor")
}
