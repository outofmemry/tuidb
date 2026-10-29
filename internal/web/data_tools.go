package web

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"
	"webtuidb/internal/db"
	"webtuidb/internal/tools"
)

// tableDefs reads every table of a schema as the editable structure the planners work on.
func (ss *session) tableDefs(r *http.Request, schema string) ([]db.TableDef, error) {
	ctx, cancel := ss.ctx(r)
	defer cancel()
	objs, err := ss.o.Sess.Objects(ctx, schema)
	if err != nil {
		return nil, err
	}
	var out []db.TableDef
	for _, o := range objs {
		if o.Kind != db.KindTable {
			continue
		}
		d, err := ss.tableDef(ctx, o.ObjRef, schema)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// tableDef reads one table's structure.
func (ss *session) tableDef(ctx context.Context, ref db.ObjRef, schema string) (db.TableDef, error) {
	sess := ss.o.Sess
	cols, err := sess.Columns(ctx, ref)
	if err != nil {
		return db.TableDef{}, fmt.Errorf("%s: %w", ref.Name, err)
	}
	d := db.TableDef{Schema: schema, Name: ref.Name, Columns: cols}
	for _, c := range cols {
		if c.PK {
			d.PrimaryKey = append(d.PrimaryKey, c.Name)
		}
	}
	if sess.Caps().Indexes { // an engine without index metadata has none to diff
		if d.Indexes, err = sess.Indexes(ctx, ref); err != nil { // missing indexes would plan DROP INDEX
			return d, fmt.Errorf("%s indexes: %w", ref.Name, err)
		}
	}
	if sess.Caps().ForeignKeys {
		if d.ForeignKeys, err = sess.ForeignKeys(ctx, ref); err != nil {
			return d, fmt.Errorf("%s foreign keys: %w", ref.Name, err)
		}
	}
	return d, nil
}

// schemaDiff plans the statements that turn schema `from` into schema `to` of this connection
// ({from, to}, default: the current schema for both). The plan is stored: send its planId to /apply,
// which holds the safe-mode gate, to run it against `from`.
func (s *Server) schemaDiff(ss *session, r *http.Request) (any, error) {
	var req struct{ From, To string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	sess := ss.o.Sess
	if req.From == "" {
		req.From = sess.CurrentSchema()
	}
	if req.To == "" {
		req.To = sess.CurrentSchema()
	}
	if req.From == req.To {
		return nil, bad("pick two different schemas to compare")
	}
	if sess.Caps().Schemas { // an unknown schema lists no tables, which would plan a DROP for every one on the other side
		known, err := sess.Schemas(r.Context())
		if err != nil {
			return nil, bad("%v", err)
		}
		for _, n := range []string{req.From, req.To} {
			if !slices.Contains(known, n) {
				return nil, bad("schema %q does not exist", n)
			}
		}
	}
	from, err := ss.tableDefs(r, req.From)
	if err != nil {
		return nil, bad("%v", err)
	}
	to, err := ss.tableDefs(r, req.To)
	if err != nil {
		return nil, bad("%v", err)
	}
	tools.Rebase(to, req.From) // the statements run against `from`, so a table is the same table in both
	var quote []func(string) string
	if d, ok := db.DialectFor(sess.Caps().Dialect); ok {
		quote = append(quote, d.Quote)
	}
	stmts, err := tools.SchemaDiff(from, to, sess.PlanTable, quote...)
	if err != nil {
		return nil, bad("%v", err)
	}
	return ss.storePlan(stmts, "", "")
}

type dataDiffReq struct {
	From, To db.ObjRef
	PK       []string // default: the primary key of From
	Columns  []string // default: the columns both tables have
}

// dataDiff compares two tables of this connection and plans the changes that turn From into To.
// shortcut: each side is read up to the connection's row limit (the response says when that cut it short); stream both sorted by key to lift it.
func (s *Server) dataDiff(ss *session, r *http.Request) (any, error) {
	var req dataDiffReq
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if req.From.Name == "" || req.To.Name == "" {
		return nil, bad("from.name and to.name are required")
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	fc, err := ss.columns(ctx, req.From)
	if err != nil {
		return nil, bad("%v", err)
	}
	tc, err := ss.columns(ctx, req.To)
	if err != nil {
		return nil, bad("%v", err)
	}
	if len(req.PK) == 0 {
		for _, c := range fc {
			if c.PK {
				req.PK = append(req.PK, c.Name)
			}
		}
	}
	if len(req.Columns) == 0 {
		in := map[string]bool{}
		for _, c := range tc {
			in[c.Name] = true
		}
		for _, c := range fc {
			if in[c.Name] {
				req.Columns = append(req.Columns, c.Name)
			}
		}
	}
	if len(req.Columns) == 0 {
		return nil, bad("the tables have no column in common")
	}
	limit := ss.rowLimit()
	if limit <= 0 {
		limit = 100000
	}
	var sort []db.Sort
	for _, k := range req.PK {
		sort = append(sort, db.Sort{Column: k})
	}
	read := func(ref db.ObjRef) (*db.Result, error) {
		return ss.o.Sess.Rows(ctx, db.RowQuery{ObjRef: ref, Sort: sort, Columns: req.Columns, Limit: limit})
	}
	a, err := read(req.From)
	if err != nil {
		return nil, bad("%v", err)
	}
	b, err := read(req.To)
	if err != nil {
		return nil, bad("%v", err)
	}
	// A table cut at the row limit would show its unread rows as missing and plan DELETEs for them.
	truncated := len(a.Rows) >= limit || len(b.Rows) >= limit
	if truncated {
		return nil, bad("a table has %d rows or more, the row limit: raise the limit or diff a smaller table", limit)
	}
	changes, err := tools.DataDiffChecked(a, b, req.PK)
	if err != nil {
		return nil, bad("%v", err)
	}
	for i := range changes {
		changes[i].ObjRef = req.From
	}
	if len(changes) == 0 {
		return map[string]any{"changes": 0, "truncated": truncated, "statements": []stmtJSON{}}, nil
	}
	stmts, err := ss.o.Sess.Plan(changes)
	if err != nil {
		return nil, bad("%v", err)
	}
	plan, _ := ss.storePlan(stmts, "", "")
	out := plan.(map[string]any)
	out["changes"], out["truncated"] = len(changes), truncated
	return out, nil
}

// bulkUpdate plans UPDATE ref SET ... WHERE filters ({ref, filters, set: [{col, val}]}); an empty
// filter set is refused. A null val sets NULL; strings are literals.
func (s *Server) bulkUpdate(ss *session, r *http.Request) (any, error) {
	var req struct {
		Ref     db.ObjRef
		Filters db.FilterSet
		Set     []db.KeyVal
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if req.Ref.Name == "" || len(req.Set) == 0 {
		return nil, bad("ref.name and at least one column to set are required")
	}
	st, err := tools.BulkUpdate(req.Ref, req.Filters, req.Set, ss.o.Sess.Caps().Dialect)
	if err != nil {
		return nil, bad("%v", err)
	}
	return ss.storePlan([]db.Statement{st}, "", "")
}

const maxFakeRows = 5000

// fakeRows plans n INSERTs of generated values ({ref, n, seed}); the same seed gives the same rows.
func (s *Server) fakeRows(ss *session, r *http.Request) (any, error) {
	var req struct {
		Ref  db.ObjRef
		N    int
		Seed int64
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if req.Ref.Name == "" || req.N < 1 || req.N > maxFakeRows {
		return nil, bad("ref.name is required and n must be 1..%d", maxFakeRows)
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	cols, err := ss.columns(ctx, req.Ref)
	if err != nil {
		return nil, bad("%v", err)
	}
	if req.Seed == 0 {
		req.Seed = time.Now().UnixNano()
	}
	changes := tools.FakeRows(cols, req.N, req.Seed)
	for i := range changes {
		changes[i].ObjRef = req.Ref
	}
	stmts, err := ss.o.Sess.Plan(changes)
	if err != nil {
		return nil, bad("%v", err)
	}
	plan, _ := ss.storePlan(stmts, "", "")
	out := plan.(map[string]any)
	out["seed"] = req.Seed
	return out, nil
}
