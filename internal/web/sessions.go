package web

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"sync"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/safemode"
	"webtuidb/internal/ui"
)

const (
	maxCachedResults = 50
	maxSessions      = 32
	idleSessionTTL   = time.Hour
)

// cached is a result the browser may edit: the original driver values stay on the server so
// bytes, times and decimals round-trip without a JSON type system.
type cached struct {
	ref  db.ObjRef // zero Name = not editable
	cols []db.Column
	res  *db.Result
}

type session struct {
	id    string
	conn  config.Connection
	o     *ui.Opened
	level safemode.Level
	srv   *Server
	stop  chan struct{}
	once  sync.Once      // close runs once
	live  sync.WaitGroup // in-flight handlers; close waits for them

	mu       sync.Mutex
	lastUsed time.Time
	busy     int               // handlers currently running; the reaper leaves such a session alone
	gen      int               // bumped whenever colCache is cleared, so a stale fetch is not stored
	planSrc  map[string]string // plan id -> result id it was planned from
	planOrig map[string]string // plan id -> CSV pre-image (log_original_values)
	jobs     map[string]*job   // background imports and searches
	spooled  int64             // bytes reserved by import spools in flight (guarded by mu)
	jobDisk  int64             // per-session temp-file budget in bytes; 0 = defaultJobDisk
	results  map[string]*cached
	order    []string
	plans    map[string][]db.Statement
	colCache map[db.ObjRef][]db.Column
	applying bool
	notices  map[string]string // notice id -> the result's message
	noticeQ  []string
	sideMu   sync.Mutex
	sideO    *ui.Opened // second connection for the process list, so it answers while the workspace is busy (M7)
	pinMu    sync.Mutex
	pins     map[string]db.Session // query tab id -> its dedicated connection (db.Pinner)
	pinUsed  map[string]time.Time  // query tab id -> last statement (guarded by pinMu)
	pinTx    map[string]pinState   // query tab id -> how it was pinned and whether a transaction is open (guarded by pinMu)
}

// putNotice remembers a result's server notices (kept apart from results so they never evict an editable one).
func (ss *session) putNotice(msg string) string {
	if _, ns := noticeLines(msg); len(ns) == 0 {
		return ""
	}
	id := randID(6)
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.notices == nil {
		ss.notices = map[string]string{}
	}
	ss.notices[id] = msg
	ss.noticeQ = append(ss.noticeQ, id)
	for len(ss.noticeQ) > maxCachedResults {
		delete(ss.notices, ss.noticeQ[0])
		ss.noticeQ = ss.noticeQ[1:]
	}
	return id
}

func (s *Server) findConn(name string) (config.Connection, bool) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	for _, c := range s.o.Conns {
		if c.Name == name {
			return c, true
		}
	}
	return config.Connection{}, false
}

func (s *Server) newSession(ctx context.Context, c config.Connection) (*session, error) {
	o, err := s.o.Connect(ctx, c)
	if err != nil {
		return nil, err
	}
	def := c.SafeMode
	if def == "" {
		def = s.o.Cfg.General.DefaultSafeMode
	}
	lvl, _ := safemode.Parse(def) // unknown value parses to Safe2: fail closed
	ss := &session{
		id: randID(8), conn: c, o: o, level: lvl, srv: s, stop: make(chan struct{}),
		results: map[string]*cached{}, plans: map[string][]db.Statement{}, colCache: map[db.ObjRef][]db.Column{},
		planSrc: map[string]string{}, planOrig: map[string]string{}, jobs: map[string]*job{}, lastUsed: time.Now(),
	}
	s.mu.Lock()
	if len(s.sessions) >= maxSessions {
		s.mu.Unlock()
		o.Close()
		return nil, fmt.Errorf("too many open sessions (%d); disconnect one first", maxSessions)
	}
	s.sessions[ss.id] = ss
	s.mu.Unlock()
	if s.o.Cfg.General.KeepAlive {
		go ss.keepAlive()
	}
	return ss, nil
}

func (ss *session) keepAlive() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			ss.o.Sess.Ping(ctx)
			cancel()
		case <-ss.stop:
			return
		}
	}
}

// close is idempotent. It waits (bounded) for in-flight handlers so they do not lose the pool.
func (ss *session) close() {
	ss.once.Do(func() {
		close(ss.stop)
		done := make(chan struct{})
		go func() { ss.live.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
		ss.releasePins()
		ss.sideMu.Lock()
		if ss.sideO != nil {
			ss.sideO.Close()
		}
		ss.sideMu.Unlock()
		ss.o.Close()
		ss.mu.Lock()
		for _, j := range ss.jobs {
			j.mu.Lock()
			j.removeFile()
			j.mu.Unlock()
		}
		ss.mu.Unlock()
	})
}

// reapIdle closes sessions unused for ttl, so reloaded tabs do not leak pools forever.
func (s *Server) reapIdle(ctx context.Context, ttl time.Duration) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.reapOnce(ttl)
		}
	}
}

func (s *Server) reapOnce(ttl time.Duration) {
	var old, live []*session
	s.mu.Lock()
	for id, ss := range s.sessions {
		ss.mu.Lock()
		idle := ss.busy == 0 && time.Since(ss.lastUsed) > ttl && !ss.jobsRunning()
		ss.mu.Unlock()
		if idle {
			delete(s.sessions, id)
			old = append(old, ss)
		} else {
			live = append(live, ss)
		}
	}
	s.mu.Unlock()
	for _, ss := range live {
		ss.sweepPins(pinTTL) // an active session must not keep a forgotten BEGIN open; closing is network I/O, so outside s.mu
	}
	for _, ss := range old {
		go ss.close()
	}
}

// jobsRunning reports an unfinished background job (caller holds ss.mu).
func (ss *session) jobsRunning() bool {
	for _, j := range ss.jobs {
		j.mu.Lock()
		d := j.done
		j.mu.Unlock()
		if !d {
			return true
		}
	}
	return false
}

func (s *Server) closeAll() {
	s.mu.Lock()
	all := s.sessions
	s.sessions = map[string]*session{}
	s.mu.Unlock()
	for _, ss := range all {
		ss.close()
	}
}

type sessHandler func(ss *session, r *http.Request) (any, error)

func (s *Server) withSession(h sessHandler) handler {
	return func(r *http.Request) (any, error) {
		s.mu.Lock()
		ss := s.sessions[r.PathValue("id")]
		if ss != nil {
			ss.live.Add(1) // under s.mu: close removes the entry first, then waits
		}
		s.mu.Unlock()
		if ss == nil {
			return nil, &apiErr{Code: http.StatusNotFound, Msg: "no such session (it was closed or the server restarted)"}
		}
		defer ss.live.Done()
		ss.mu.Lock()
		ss.lastUsed = time.Now()
		ss.busy++
		ss.mu.Unlock()
		defer func() {
			ss.mu.Lock()
			ss.busy--
			ss.lastUsed = time.Now() // idle time counts from the end of a long request
			ss.mu.Unlock()
		}()
		return h(ss, r)
	}
}

// ctxNoTimeout is ctx without the hard query_timeout_secs: exec handlers end when the client cancels
// (the keep-waiting prompt), the session stops, or a generous server cap passes (the larger of ten
// times query_timeout_secs and one hour), so a vanished client cannot leave a query running forever.
func (ss *session) ctxNoTimeout(r *http.Request) (context.Context, context.CancelFunc) {
	limit := time.Hour
	if n := ss.srv.o.Cfg.General.QueryTimeoutSecs; n > 0 && time.Duration(n)*10*time.Second > limit {
		limit = time.Duration(n) * 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), limit)
	go func() {
		select {
		case <-ss.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// ctx applies the configured query timeout on top of the request context.
func (ss *session) ctx(r *http.Request) (context.Context, context.CancelFunc) {
	if n := ss.srv.o.Cfg.General.QueryTimeoutSecs; n > 0 {
		return context.WithTimeout(r.Context(), time.Duration(n)*time.Second)
	}
	return context.WithCancel(r.Context())
}

// clampLimit bounds a client-supplied row limit by the configured one: 0 or negative means "the
// configured limit", never "unlimited" (a request must not materialise a whole table).
func (ss *session) clampLimit(n int) int {
	if rl := ss.rowLimit(); n <= 0 || (rl > 0 && n > rl) {
		return rl
	}
	return n
}

// readOnlyViolation is the one place that enforces connection.read_only for the web UI: drivers that
// do not guard themselves (Redis, Mongo, ClickHouse, ...) would otherwise run the write.
func (ss *session) readOnlyViolation(stmts []db.Statement) error {
	if !ss.conn.ReadOnly {
		return nil
	}
	dialect := ss.o.Sess.Caps().Dialect
	for _, st := range stmts {
		if !st.ReadOnly && !db.IsReadOnly(dialect, st.Text) {
			return &apiErr{Code: http.StatusForbidden, Msg: "this connection is read-only; data-changing statements are refused"}
		}
	}
	return nil
}

func (ss *session) rowLimit() int {
	st := ss.srv.o.State
	if st == nil {
		return ss.srv.o.Cfg.General.RowLimit
	}
	return st.RowLimit(ss.conn.Name, ss.srv.o.Cfg.General.RowLimit)
}

// put stores a result and evicts the oldest beyond maxCachedResults.
func (ss *session) put(c *cached) string {
	id := randID(6)
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.results[id] = c
	ss.order = append(ss.order, id)
	for len(ss.order) > maxCachedResults {
		delete(ss.results, ss.order[0])
		ss.order = ss.order[1:]
	}
	return id
}

func (ss *session) result(id string) (*cached, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if c := ss.results[id]; c != nil {
		return c, nil
	}
	return nil, bad("unknown or evicted result %q; reload the table", id)
}

// columns returns a table's columns with foreign keys merged in, cached per object.
func (ss *session) columns(ctx context.Context, ref db.ObjRef) ([]db.Column, error) {
	ss.mu.Lock()
	cols, ok := ss.colCache[ref]
	gen := ss.gen
	ss.mu.Unlock()
	if ok {
		return cols, nil
	}
	cols, err := ss.o.Sess.Columns(ctx, ref)
	if err != nil {
		return nil, err
	}
	if ss.o.Sess.Caps().ForeignKeys {
		if fks, err := ss.o.Sess.ForeignKeys(ctx, ref); err == nil {
			for _, fk := range fks {
				fk := fk
				for i := range cols {
					for _, n := range fk.Columns {
						if cols[i].Name == n && len(fk.Columns) == 1 {
							cols[i].FK = &fk
						}
					}
				}
			}
		}
	}
	ss.mu.Lock()
	if ss.gen == gen { // a cache reset mid-fetch means these may belong to the old database
		ss.colCache[ref] = cols
	}
	ss.mu.Unlock()
	return cols, nil
}

func (ss *session) resetCaches() {
	ss.mu.Lock()
	ss.results, ss.order = map[string]*cached{}, nil
	ss.plans, ss.planSrc, ss.planOrig = map[string][]db.Statement{}, map[string]string{}, map[string]string{}
	ss.colCache = map[db.ObjRef][]db.Column{}
	ss.gen++
	ss.mu.Unlock()
}

func (ss *session) clearColCache() {
	ss.mu.Lock()
	ss.colCache = map[db.ObjRef][]db.Column{}
	ss.gen++
	ss.mu.Unlock()
}

// ---- safe mode ----------------------------------------------------------------------

type gateInfo struct {
	Confirm  bool `json:"confirm"`
	Password bool `json:"password"`
}

// gate mirrors ui.Workspace.gate: a batch is read-only only if every statement is.
func (ss *session) gate(stmts []db.Statement) gateInfo {
	readOnly := true
	dialect := ss.o.Sess.Caps().Dialect
	for _, st := range stmts {
		if !st.ReadOnly && !db.IsReadOnly(dialect, st.Text) {
			readOnly = false
		}
	}
	c, p := ss.lvl().Gate(readOnly)
	if p && ss.o.Password == "" { // nothing to verify: fall back to confirmation, like check and the TUI
		c, p = true, false
	}
	return gateInfo{Confirm: c, Password: p}
}

// check enforces the gate on the server; the browser can never skip it. A password level on a
// connection with no stored password falls back to plain confirmation, like the TUI.
func (ss *session) check(g gateInfo, confirmed bool, password string) error {
	lvl := ss.lvl()
	deny := func(msg string) error {
		return &apiErr{Code: http.StatusConflict, Msg: msg, Extra: map[string]any{"gate": g, "level": lvl.Label()}}
	}
	if g.Password && ss.o.Password != "" {
		if password == "" {
			return deny(lvl.Label() + ": enter the database password")
		}
		if subtle.ConstantTimeCompare([]byte(password), []byte(ss.o.Password)) != 1 {
			return deny("wrong password")
		}
		return nil
	}
	if (g.Confirm || g.Password) && !confirmed {
		return deny(lvl.Label() + ": confirmation required")
	}
	return nil
}

// lvl reads the safe-mode level under the lock (setSafeMode writes it from another request).
func (ss *session) lvl() safemode.Level {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.level
}
