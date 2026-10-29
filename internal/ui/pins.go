// pins.go: Session-pinned tabs.

package ui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

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
