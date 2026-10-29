package web

import (
	"context"
	"net/http"
	"time"
	"webtuidb/internal/db"
)

// processLister is the process list of the session's side connection (opened on first use, closed with the
// session); if a second connection cannot be made the workspace's own is used.
func (ss *session) processLister() (db.ProcessLister, bool) {
	if _, ok := ss.o.Sess.(db.ProcessLister); !ok || !ss.o.Sess.Caps().ProcessList {
		return nil, false
	}
	ss.sideMu.Lock()
	defer ss.sideMu.Unlock()
	if ss.sideO == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if o, err := ss.srv.o.Connect(ctx, ss.conn); err == nil {
			ss.sideO = o
		}
	}
	if ss.sideO != nil {
		if pl, ok := ss.sideO.Sess.(db.ProcessLister); ok {
			return pl, true
		}
	}
	return ss.o.Sess.(db.ProcessLister), true
}

func (s *Server) processes(ss *session, r *http.Request) (any, error) {
	pl, ok := ss.processLister()
	if !ok {
		return map[string]any{"supported": false, "driver": ss.conn.Driver, "processes": []any{}}, nil
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	l, err := pl.Processes(ctx)
	if err != nil {
		return nil, bad("%v", err)
	}
	out := make([]map[string]any, len(l))
	for i, p := range l {
		out[i] = map[string]any{"id": p.ID, "user": p.User, "db": p.DB, "state": p.State, "elapsedMs": p.Elapsed.Milliseconds(), "query": p.Query}
	}
	return map[string]any{"supported": true, "processes": out}, nil
}

func (s *Server) killProcess(ss *session, r *http.Request) (any, error) {
	var req struct {
		ID        string
		Confirmed bool
		Password  string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	pl, ok := ss.processLister()
	if !ok {
		return nil, bad("not supported by %s", ss.conn.Driver)
	}
	// killing is never read-only, so safe mode gates it like a write
	if err := ss.check(ss.gate([]db.Statement{{Text: "KILL " + req.ID}}), req.Confirmed, req.Password); err != nil {
		return nil, err
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	if err := pl.Kill(ctx, req.ID); err != nil {
		return nil, bad("%v", err)
	}
	ss.record("kill process "+req.ID, "gui", 0, "")
	return nil, nil
}
