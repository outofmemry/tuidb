package web

import (
	"net/http"
	"webtuidb/internal/db"
)

func (s *Server) filtersSQL(ss *session, r *http.Request) (any, error) {
	var req struct {
		Ref     db.ObjRef
		Filters db.FilterSet
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	d, ok := db.DialectFor(ss.o.Sess.Caps().Dialect)
	if !ok {
		return nil, bad("no SQL dialect for %s", ss.conn.Driver)
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	cols, err := ss.columns(ctx, req.Ref)
	if err != nil {
		return nil, bad("%v", err)
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	w, err := db.WhereText(d, req.Filters, names)
	if err != nil {
		return nil, bad("%v", err)
	}
	return map[string]any{"where": w}, nil
}
