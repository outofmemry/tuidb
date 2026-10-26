package ui

import (
	"context"
	"regexp"
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

// aliasesIn maps each alias (and bare table name) in the FROM / JOIN clauses of sql to its lower-case table name.
// shortcut: comma-separated table lists ("from a x, b y") only resolve their first table.
func aliasesIn(sql string) map[string]string {
	out := map[string]string{}
	for _, m := range aliasRe.FindAllStringSubmatch(sql, -1) {
		tbl := strings.ToLower(strings.Trim(m[1], `"`))
		if i := strings.LastIndex(tbl, "."); i >= 0 {
			tbl = strings.Trim(tbl[i+1:], `"`)
		}
		if m[2] != "" && !notAlias[strings.ToLower(m[2])] {
			out[strings.ToLower(m[2])] = tbl
		}
	}
	return out
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
	if t.pin != nil {
		old := t.pin
		t.pin, t.pinning = nil, true
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
