// explain.go: EXPLAIN: running it and showing the plan.

package ui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

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
