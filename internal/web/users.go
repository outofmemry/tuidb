package web

import (
	"net/http"
	"webtuidb/internal/db"
)

func (ss *session) userAdmin() (db.UserAdmin, error) {
	if ua, ok := ss.o.Sess.(db.UserAdmin); ok && ss.o.Sess.Caps().Users {
		return ua, nil
	}
	return nil, bad("not supported by %s", ss.conn.Driver)
}

func (s *Server) users(ss *session, r *http.Request) (any, error) {
	ua, err := ss.userAdmin()
	if err != nil {
		return map[string]any{"supported": false, "driver": ss.conn.Driver, "users": []db.User{}}, nil
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	l, err := ua.Users(ctx)
	if err != nil {
		return nil, bad("%v", err)
	}
	if l == nil {
		l = []db.User{}
	}
	return map[string]any{"supported": true, "users": l}, nil
}

// usersPlan only plans: the client sends the statements through /apply, which holds the safe-mode gate.
func (s *Server) usersPlan(ss *session, r *http.Request) (any, error) {
	var ch db.UserChange
	if err := decode(r, &ch); err != nil {
		return nil, err
	}
	ua, err := ss.userAdmin()
	if err != nil {
		return nil, err
	}
	stmts, err := ua.PlanUser(ch)
	if err != nil {
		return nil, bad("%v", err)
	}
	return ss.storePlan(stmts, "", "")
}
