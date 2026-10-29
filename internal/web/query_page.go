package web

import (
	"net/http"
	"time"
	"webtuidb/internal/db"
)

// execPage fetches rows offset..offset+limit of one read-only SELECT (db.PageStatement) so "load more"
// does not re-run the statement with a doubled limit. supported=false: the caller falls back to /exec.
func (s *Server) execPage(ss *session, r *http.Request) (any, error) {
	var req struct {
		Text          string
		Limit, Offset int
		Pin           string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	req.Limit = ss.clampLimit(req.Limit)
	if ss.o.Sess.Caps().NoSubquery { // CQL and PartiQL reject "SELECT * FROM (<stmt>)"; say so instead of sending it
		return map[string]any{"supported": false, "reason": "load more is not available for this engine"}, nil
	}
	stmt, ok := db.PageStatement(ss.o.Sess.Caps().Dialect, req.Text, req.Limit, req.Offset)
	if !ok {
		return map[string]any{"supported": false}, nil
	}
	sess, err := ss.pinned(req.Pin)
	if err != nil {
		return nil, err
	}
	ctx, cancel := ss.ctxNoTimeout(r)
	defer cancel()
	start := time.Now()
	res, err := sess.Exec(ctx, stmt, 0)
	if err != nil {
		return nil, bad("%v", err)
	}
	ss.record(stmt, "editor", time.Since(start), "")
	out := toJSON(res, nil)
	// pages are never editable: editableResult would cache a result nobody uses and evict live ones
	return map[string]any{"supported": true, "result": out, "more": len(res.Rows) >= req.Limit}, nil
}
