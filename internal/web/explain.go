package web

import (
	"net/http"
	"webtuidb/internal/db"
)

// explain plans (never runs) one statement: ExplainStatement is plan-only, so no safe-mode gate is
// needed, but the text must be a single statement and the engine must have an EXPLAIN.
func (s *Server) explain(ss *session, r *http.Request) (any, error) {
	var req struct {
		Text    string
		Current bool // explain the statement at Offset (a byte offset, like /exec) instead of requiring one statement
		Offset  int
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	dialect := ss.o.Sess.Caps().Dialect
	st := db.Split(dialect, req.Text)
	if req.Current {
		cur, ok := db.StatementAt(st, req.Offset)
		if !ok {
			return nil, bad("no statement at the cursor")
		}
		st = []db.Stmt{cur}
	}
	if len(st) != 1 {
		return nil, bad("explain needs exactly one statement")
	}
	mode := ss.o.Sess.Caps().ExplainMode()
	stmt := db.ExplainStatement(mode, st[0].Text)
	if stmt == "" {
		return nil, bad("EXPLAIN is not supported by %s", ss.conn.Driver)
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	res, err := ss.o.Sess.Exec(ctx, stmt, 0)
	if err != nil {
		return nil, bad("%v", err)
	}
	nodes, err := db.ExplainTree(mode, res)
	if err != nil {
		return nil, bad("%v", err)
	}
	type nodeJSON struct {
		Depth int    `json:"depth"`
		Label string `json:"label"`
		Cost  string `json:"cost,omitempty"`
		Rows  string `json:"rows,omitempty"`
	}
	out := make([]nodeJSON, len(nodes))
	for i, n := range nodes {
		out[i] = nodeJSON{n.Depth, n.Label, n.Cost, n.Rows}
	}
	return map[string]any{"statement": stmt, "nodes": out}, nil
}
