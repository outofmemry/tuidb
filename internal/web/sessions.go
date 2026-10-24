package web

import (
	"context"
	"crypto/subtle"
	"net/http"
	"sync"
	"time"

	"tuidb/internal/config"
	"tuidb/internal/db"
	"tuidb/internal/safemode"
	"tuidb/internal/ui"
)

const maxCachedResults = 50

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

	mu       sync.Mutex
	results  map[string]*cached
	order    []string
	plans    map[string][]db.Statement
	colCache map[db.ObjRef][]db.Column
	applying bool
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
	}
	s.mu.Lock()
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

func (ss *session) close() {
	select {
	case <-ss.stop:
	default:
		close(ss.stop)
	}
	ss.o.Close()
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
		s.mu.Unlock()
		if ss == nil {
			return nil, &apiErr{Code: http.StatusNotFound, Msg: "no such session (it was closed or the server restarted)"}
		}
		return h(ss, r)
	}
}

// ctx applies the configured query timeout on top of the request context.
func (ss *session) ctx(r *http.Request) (context.Context, context.CancelFunc) {
	if n := ss.srv.o.Cfg.General.QueryTimeoutSecs; n > 0 {
		return context.WithTimeout(r.Context(), time.Duration(n)*time.Second)
	}
	return context.WithCancel(r.Context())
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
	ss.colCache[ref] = cols
	ss.mu.Unlock()
	return cols, nil
}

func (ss *session) resetCaches() {
	ss.mu.Lock()
	ss.results, ss.order = map[string]*cached{}, nil
	ss.plans = map[string][]db.Statement{}
	ss.colCache = map[db.ObjRef][]db.Column{}
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
	c, p := ss.level.Gate(readOnly)
	return gateInfo{Confirm: c, Password: p}
}

// check enforces the gate on the server; the browser can never skip it. A password level on a
// connection with no stored password falls back to plain confirmation, like the TUI.
func (ss *session) check(g gateInfo, confirmed bool, password string) error {
	deny := func(msg string) error {
		return &apiErr{Code: http.StatusConflict, Msg: msg, Extra: map[string]any{"gate": g, "level": ss.level.Label()}}
	}
	if g.Password && ss.o.Password != "" {
		if password == "" {
			return deny(ss.level.Label() + ": enter the database password")
		}
		if subtle.ConstantTimeCompare([]byte(password), []byte(ss.o.Password)) != 1 {
			return deny("wrong password")
		}
		return nil
	}
	if (g.Confirm || g.Password) && !confirmed {
		return deny(ss.level.Label() + ": confirmation required")
	}
	return nil
}
