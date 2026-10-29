package ui

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

// ---- editor transformations (M4 item 11) ------------------------------------------------------

// transform upper-cases, lower-cases or capitalises the selection (the word under the cursor without
// one) and keeps it selected. It returns the message to show.
func (e *Editor) transform(name string) string {
	if !e.HasSelection() && e.wordAtCursor() == "" {
		return "select some text or put the cursor on a word first"
	}
	a, _, _ := e.selRange()
	s := e.Selection()
	if s == "" { // never Replace("") : that wipes the whole buffer
		return "select some text or put the cursor on a word first"
	}
	switch name {
	case "upper_case":
		s = strings.ToUpper(s)
	case "lower_case":
		s = strings.ToLower(s)
	default: // capitalize: first letter of each word up, the rest down
		up := true
		s = strings.Map(func(r rune) rune {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				up = true
				return r
			}
			if up {
				up = false
				return unicode.ToUpper(r)
			}
			return unicode.ToLower(r)
		}, s)
	}
	e.Replace(s)
	e.anchor = &a
	return ""
}

// openComplete shows the autocomplete popup on demand, even with an empty prefix or autocomplete off.
func (e *Editor) openComplete() bool {
	e.ac.open = false
	if e.Complete == nil {
		return false
	}
	p, _ := e.prefixAtCursor()
	items := e.Complete(p)
	if len(items) == 0 {
		return false
	}
	if len(items) > 50 {
		items = items[:50]
	}
	e.ac.open, e.ac.items, e.ac.sel = true, items, 0
	return true
}

// ---- alias-aware autocomplete ----------------------------------------------------------------

var aliasRe = regexp.MustCompile(`(?i)\b(?:from|join)\s+((?:"[^"]+"|\w+)(?:\.(?:"[^"]+"|\w+))?)(?:\s+(?:as\s+)?(\w+))?`)

var notAlias = map[string]bool{"where": true, "on": true, "using": true, "join": true, "inner": true, "left": true, "right": true, "full": true, "outer": true,
	"cross": true, "natural": true, "group": true, "order": true, "limit": true, "having": true, "union": true, "except": true, "intersect": true, "offset": true,
	"window": true, "set": true, "select": true, "for": true, "returning": true, "lateral": true}

// aliasesIn maps each alias in the FROM / JOIN clauses of sql to its lower-case table name, schema-qualified as
// written (shop.customers), so c. completes the columns of the table the statement names.
// shortcut: comma-separated table lists ("from a x, b y") only resolve their first table.
func aliasesIn(sql string) map[string]string {
	out := map[string]string{}
	for _, m := range aliasRe.FindAllStringSubmatch(sql, -1) {
		if m[2] != "" && !notAlias[strings.ToLower(m[2])] {
			out[strings.ToLower(m[2])] = refKey(m[1])
		}
	}
	return out
}

// refKey is the lower-case, unquoted spelling of a table reference: "Shop"."Orders" -> shop.orders.
func refKey(ref string) string { return strings.ToLower(strings.ReplaceAll(ref, `"`, "")) }

// splitRefKey splits a refKey into schema ("" when unqualified) and table.
func splitRefKey(key string) (string, string) {
	if i := strings.LastIndex(key, "."); i >= 0 {
		return key[:i], key[i+1:]
	}
	return "", key
}

// aliasAtCursor resolves qual through the aliases of the statement under the cursor.
func (t *queryTab) aliasAtCursor(qual string) string {
	text := t.ed.Text()
	if s, ok := t.ed.CurrentStatement(); ok {
		text = s.Text
	}
	if tbl, ok := aliasesIn(text)[qual]; ok {
		return tbl
	}
	return qual
}

// ---- explain ---------------------------------------------------------------------------------

type explainMsg struct {
	stmt string
	res  *db.Result
	err  error
	took time.Duration
}

// explain runs the plan-only EXPLAIN of the selection or current statement and shows it as a tree.
func (t *queryTab) explain() tea.Cmd {
	text := strings.TrimSpace(t.ed.Selection())
	if text == "" {
		s, ok := t.ed.CurrentStatement()
		if !ok {
			return t.ws.a.say("nothing to explain")
		}
		text = strings.TrimSpace(s.Text)
	}
	q := db.ExplainStatement(t.ws.caps.ExplainMode(), strings.TrimRight(text, "; \n"))
	if q == "" {
		return t.ws.a.say("explain is not available for %s", t.ws.conn.Driver)
	}
	sess := t.sess()
	return t.ws.async(t.id, func(ctx context.Context, _ db.Session) tea.Msg {
		start := time.Now()
		res, err := sess.Exec(ctx, q, 0)
		return explainMsg{stmt: q, res: res, err: err, took: time.Since(start)}
	})
}

func (t *queryTab) showExplain(m explainMsg) tea.Cmd {
	t.ws.record(m.stmt, "editor", m.took, m.err)
	if m.err != nil {
		return t.ws.a.fail(m.err)
	}
	nodes, err := db.ExplainTree(t.ws.caps.ExplainMode(), m.res)
	if err != nil {
		return t.ws.a.fail(err)
	}
	var lines []string
	for _, n := range nodes {
		l := strings.Repeat("   ", n.Depth) + "└ " + n.Label
		if n.Cost != "" {
			l += "   cost " + n.Cost
		}
		if n.Rows != "" {
			l += "   rows " + n.Rows
		}
		lines = append(lines, l)
	}
	t.ws.a.push(&textViewDlg{title: "Explain", lines: lines})
	return nil
}

// ---- session-pinned tab ---------------------------------------------------------------------

// sess is the session this tab runs on: its pinned connection, else the workspace's.
func (t *queryTab) sess() db.Session {
	if t.pin != nil {
		return t.pin
	}
	return t.ws.sess
}

// togglePin pins one dedicated connection to this tab (BEGIN, SET and temp tables then carry over
// between statements) or releases it.
// shortcut: a reconnect or database switch leaves the pinned connection on the old server; unpin and pin again.
func (t *queryTab) togglePin() tea.Cmd {
	if t.pinning {
		return t.ws.a.say("still pinning or releasing the connection")
	}
	if t.running {
		return t.ws.a.say("wait for the running query before pinning or releasing the connection")
	}
	if t.pin != nil && t.inTx {
		t.ws.a.push(&confirmDlg{title: "This tab has an open transaction. Release the connection and roll back?", lines: []string{"Everything since BEGIN is rolled back. COMMIT first to keep it."},
			yes: "Roll back and release", danger: true, onYes: func() tea.Cmd { t.inTx = false; return t.togglePin() }})
		return nil
	}
	if t.pin != nil {
		old := t.pin
		t.pin, t.pinning, t.autoPin = nil, true, false
		return liveCmd(func() func() tea.Cmd {
			old.Close() // waits on the pool; keep it off the UI loop
			return func() tea.Cmd {
				t.pinning = false
				return t.ws.a.say("connection released")
			}
		})
	}
	p, ok := t.ws.sess.(db.Pinner)
	if !ok {
		return t.ws.a.say("%s cannot pin a connection to a tab", t.ws.conn.Driver)
	}
	t.pinning = true
	secs := t.ws.a.d.Cfg.General.QueryTimeoutSecs
	if secs <= 0 {
		secs = 30
	}
	return liveCmd(func() func() tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(secs)*time.Second)
		defer cancel()
		s, err := p.Pin(ctx)
		return func() tea.Cmd {
			t.pinning = false
			if err != nil {
				return t.ws.a.fail(err)
			}
			if t.closed {
				s.Close()
				return nil
			}
			t.pin = s
			return t.ws.a.say("this tab now keeps one connection: BEGIN and SET carry over (pin again to release it)")
		}
	})
}

// ---- the "taking too long" prompt, transactions and file tabs on close -------------------------

// removeOverlay takes o off the dialog stack wherever it is, and replays the results that waited behind it.
func (a *App) removeOverlay(o Overlay) tea.Cmd {
	i := slices.Index(a.overlays, o)
	if i < 0 {
		return nil
	}
	a.overlays = slices.Delete(a.overlays, i, i+1)
	if len(a.overlays) > 0 || len(a.deferred) == 0 {
		return nil
	}
	q := a.deferred
	a.deferred = nil
	var cmds []tea.Cmd
	for _, m := range q {
		cmds = append(cmds, func() tea.Msg { return m })
	}
	return tea.Batch(cmds...)
}

// closeSlowPrompt closes the tab's "taking too long" prompt: the batch finished or was cancelled.
func (t *queryTab) closeSlowPrompt() tea.Cmd {
	if t.slowDlg == nil {
		return nil
	}
	d := t.slowDlg
	t.slowDlg = nil
	return t.ws.a.removeOverlay(d)
}

// passesSlowPrompt: a statement result for the tab whose "taking too long" prompt is on top is handled at once
// instead of waiting behind the prompt, so the batch goes on, the time is the statement's own and the prompt
// closes when the batch is done.
func (a *App) passesSlowPrompt(m wsMsg) bool {
	if _, ok := m.msg.(queryDoneMsg); !ok || len(a.overlays) == 0 {
		return false
	}
	top, ok := a.overlays[len(a.overlays)-1].(*pickerDlg)
	w := a.wsByID(m.ws)
	if !ok || w == nil {
		return false
	}
	for _, t := range w.tabs {
		if q, ok := t.(*queryTab); ok && q.ID() == m.tab {
			return q.slowDlg == top
		}
	}
	return false
}

// fileDirty: the tab was opened from a .sql file and its text differs from the file.
// shortcut: with split_down the file is compared with the focused editor only.
func (t *queryTab) fileDirty() bool { return t.file != "" && t.ed.Text() != t.fileText }

// writeBack saves the editor to the file the tab came from.
func (t *queryTab) writeBack() error {
	text := t.ed.Text()
	if err := writeFile(t.file, text); err != nil {
		return err
	}
	t.fileText = text
	return nil
}

// askClose asks before a tab with something to lose closes: a modified file offers to save, an open transaction
// warns that it is rolled back, anything else is the generic unsaved-changes question. done closes the tab.
func (w *Workspace) askClose(t tabber, done func() tea.Cmd) {
	q, _ := t.(*queryTab)
	if q != nil && q.fileDirty() {
		q.askSave(func() tea.Cmd {
			if q.inTx {
				w.askClose(t, done)
				return nil
			}
			return done()
		})
		return
	}
	if q != nil && q.inTx {
		w.a.push(&confirmDlg{title: "This tab has an open transaction. Close it and roll back?", lines: []string{"Everything since BEGIN is rolled back. COMMIT first to keep it."},
			yes: "Roll back and close", danger: true, onYes: done})
		return
	}
	w.a.push(&confirmDlg{title: "Close " + t.Title() + " with unsaved changes?", lines: []string{"Pending edits in this tab will be lost."}, yes: "Close", danger: true, onYes: done})
}

// askSave offers Save / Don't save / Cancel for a modified file tab; then runs after Save or Don't save.
func (t *queryTab) askSave(then func() tea.Cmd) {
	name := filepath.Base(t.file)
	t.ws.a.push(&pickerDlg{title: "Save changes to " + name + "?", hint: "Enter: choose   Esc: cancel",
		items: []pickItem{{Label: "Save", Value: "save"}, {Label: "Don't save", Value: "discard"}, {Label: "Cancel", Value: "cancel"}},
		onPick: func(it pickItem, _ bool) tea.Cmd {
			switch it.Value.(string) {
			case "save":
				if err := t.writeBack(); err != nil {
					return t.ws.a.fail(err)
				}
			case "discard":
				t.fileText = t.ed.Text() // asked once: quitting does not ask again
			default:
				return nil
			}
			return then()
		}})
}

// m5Action runs the actions that belong to no tab: palette entries and the ones with a hint.
func (w *Workspace) m5Action(name string) (tea.Cmd, bool) {
	switch name {
	case "pin_connection":
		if q, ok := w.tab().(*queryTab); ok {
			return q.togglePin(), true
		}
		return w.a.say("open an SQL editor to pin its connection"), true
	case "explain":
		return w.a.say("explain works in an SQL editor"), true
	case "bulk_update":
		return w.a.say("bulk update works on a filtered table grid"), true
	case "connection_details":
		return w.connectionDetails(), true
	case "schema_diff":
		return w.schemaDiff(), true
	case "data_diff":
		return w.dataDiff(), true
	case "fake_rows":
		return w.fakeRows(), true
	}
	return nil, false
}
