package web

import (
	"context"
	"net/http"
	"time"
	"webtuidb/internal/db"
)

const maxPins = 8

// pinTTL is how long a pinned connection may sit unused before it is released (an open transaction holds locks).
var pinTTL = 10 * time.Minute

func (ss *session) sweepPins(ttl time.Duration) {
	ss.pinMu.Lock()
	var old []db.Session
	for tab, p := range ss.pins {
		if time.Since(ss.pinUsed[tab]) > ttl {
			old = append(old, p)
			delete(ss.pins, tab)
			delete(ss.pinUsed, tab)
			delete(ss.pinTx, tab)
		}
	}
	ss.pinMu.Unlock()
	for _, p := range old {
		p.Close()
		ss.srv.con.add(ss.conn.Name, "-- a pinned query tab idle for "+ttl.String()+" was closed; anything it had open (BEGIN, temp tables) was rolled back")
	}
}

func isPinner(s db.Session) bool { _, ok := s.(db.Pinner); return ok }

// pinned is the session a query tab runs on: its dedicated connection (made on first use) when the tab
// is pinned, else the pool. BEGIN, SET and temp tables then carry over between the tab's statements.
// shortcut: a database switch leaves a pinned connection on the old database; unpin the tab and run again.
func (ss *session) pinned(tab string) (db.Session, error) {
	if tab == "" {
		return ss.o.Sess, nil
	}
	ss.pinMu.Lock()
	defer ss.pinMu.Unlock()
	if ss.pinUsed == nil {
		ss.pinUsed = map[string]time.Time{}
	}
	ss.pinUsed[tab] = time.Now()
	if p := ss.pins[tab]; p != nil {
		return p, nil
	}
	pn, ok := ss.o.Sess.(db.Pinner)
	if !ok {
		return nil, bad("%s cannot pin a connection to a tab", ss.conn.Driver)
	}
	if len(ss.pins) >= maxPins {
		return nil, bad("%d tabs are already pinned; unpin one first", maxPins)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, err := pn.Pin(ctx)
	if err != nil {
		return nil, bad("%v", err)
	}
	if ss.pins == nil {
		ss.pins = map[string]db.Session{}
	}
	ss.pins[tab] = p
	return p, nil
}

// pinState: auto = BEGIN pinned the tab (the pin is released again at COMMIT / ROLLBACK); tx = a transaction is
// open, guessed from the statements for drivers that cannot say (db.TxReporter).
type pinState struct{ auto, tx bool }

// autoPin gives an unpinned tab its own connection when the batch starts with BEGIN / START TRANSACTION, so the
// transaction carries over to the tab's next run instead of autocommitting on another pooled connection.
func (ss *session) autoPin(tab, pin string, stmts []db.Stmt) (string, bool) {
	if pin != "" || tab == "" || len(stmts) == 0 || !isPinner(ss.o.Sess) || db.TxVerb(ss.o.Sess.Caps().Dialect, stmts[0].Text) != 1 {
		return pin, false
	}
	return tab, true
}

// afterRun records what ran on a pinned tab and answers whether it is still pinned and holds a transaction; a pin
// that BEGIN made is released once the transaction is over.
func (ss *session) afterRun(pin string, auto bool, ran []string) (pinned, inTx bool) {
	if pin == "" {
		return false, false
	}
	ss.pinMu.Lock()
	p, st := ss.pins[pin], ss.pinTx[pin]
	st.auto = st.auto || auto
	ss.pinMu.Unlock()
	if p == nil {
		return false, false
	}
	st.tx = db.InTx(p, db.TxAfter(ss.o.Sess.Caps().Dialect, st.tx, ran))
	if st.auto && !st.tx {
		ss.releasePin(pin)
		return false, false
	}
	ss.pinMu.Lock()
	if ss.pinTx == nil {
		ss.pinTx = map[string]pinState{}
	}
	ss.pinTx[pin] = st
	ss.pinMu.Unlock()
	return true, st.tx
}

func (ss *session) releasePin(tab string) {
	ss.pinMu.Lock()
	p := ss.pins[tab]
	delete(ss.pins, tab)
	delete(ss.pinTx, tab)
	ss.pinMu.Unlock()
	if p != nil {
		p.Close() // ends an open transaction
	}
}

func (ss *session) releasePins() {
	ss.pinMu.Lock()
	ps := ss.pins
	ss.pins, ss.pinTx = nil, nil
	ss.pinMu.Unlock()
	for _, p := range ps {
		p.Close()
	}
}

// pinTab: POST {tab} pins the tab now (so the toggle shows the truth), DELETE releases it.
func (s *Server) pinTab(ss *session, r *http.Request) (any, error) {
	if r.Method == http.MethodDelete {
		ss.releasePin(r.PathValue("tab"))
		return nil, nil
	}
	_, err := ss.pinned(r.PathValue("tab"))
	return nil, err
}
