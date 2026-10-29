package web

import (
	"net/http"
	"strings"
	"webtuidb/internal/db"
)

// planTableEdit plans one structure edit of an existing table through the driver's PlanTable and stores
// the plan for /apply (which holds the safe-mode gate):
//
//	{ref, op: "add_fk", fk: {Name, Columns, RefSchema, RefTable, RefColumns, OnUpdate, OnDelete}}
//	{ref, op: "move_column", column, to}   // to = the new 0-based position; engines with ReorderColumns only
func (s *Server) planTableEdit(ss *session, r *http.Request) (any, error) {
	var req struct {
		Ref    db.ObjRef
		Op     string
		FK     db.ForeignKey
		Column string
		To     int
		Col    db.Column // alter_column: the new Name, Type, Nullable, Default and HasDef of Column
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if req.Ref.Name == "" {
		return nil, bad("ref.name is required")
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	old, err := ss.tableDef(ctx, req.Ref, req.Ref.Schema)
	if err != nil {
		return nil, bad("%v", err)
	}
	nw := old
	nw.Columns = append([]db.Column(nil), old.Columns...)
	for i := range nw.Columns {
		nw.Columns[i].OrigName = nw.Columns[i].Name // the planners keep a column only when its OrigName names an existing one; "" means a new column
	}
	nw.ForeignKeys = append([]db.ForeignKey(nil), old.ForeignKeys...)
	caps := ss.o.Sess.Caps()
	has := func(n string) int {
		for i, c := range old.Columns {
			if c.Name == n {
				return i
			}
		}
		return -1
	}
	switch req.Op {
	case "add_fk":
		fk := req.FK
		if !caps.ForeignKeys {
			return nil, bad("%s has no foreign keys", ss.conn.Driver)
		}
		if len(fk.Columns) == 0 || len(fk.Columns) != len(fk.RefColumns) || fk.RefTable == "" {
			return nil, bad("a foreign key needs matching columns and referenced columns, and a referenced table")
		}
		for _, c := range fk.Columns {
			if has(c) < 0 {
				return nil, bad("no column %q in %s", c, req.Ref.Name)
			}
		}
		if fk.Name == "" {
			fk.Name = "fk_" + req.Ref.Name + "_" + strings.Join(fk.Columns, "_")
		}
		for _, o := range old.ForeignKeys {
			if o.Name == fk.Name {
				return nil, bad("a foreign key named %q already exists", fk.Name)
			}
		}
		for _, f := range []*string{&fk.OnUpdate, &fk.OnDelete} {
			if *f == "" {
				*f = "NO ACTION"
			}
		}
		nw.ForeignKeys = append(nw.ForeignKeys, fk)
	case "alter_column":
		i := has(req.Column)
		if i < 0 || strings.TrimSpace(req.Col.Name) == "" || strings.TrimSpace(req.Col.Type) == "" {
			return nil, bad("no column %q, or the new name or type is empty", req.Column)
		}
		if j := has(req.Col.Name); j >= 0 && j != i {
			return nil, bad("there is already a column named %q", req.Col.Name)
		}
		c := &nw.Columns[i]
		c.Name, c.Type, c.Nullable = req.Col.Name, req.Col.Type, req.Col.Nullable
		c.Default, c.HasDef = req.Col.Default, req.Col.HasDef && req.Col.Default != "" // OrigName stays: Name != OrigName is a rename
	case "move_column":
		if !caps.ReorderColumns {
			return nil, bad("%s cannot reorder columns", ss.conn.Driver)
		}
		from := has(req.Column)
		if from < 0 || req.To < 0 || req.To >= len(old.Columns) {
			return nil, bad("no column %q or position %d out of range", req.Column, req.To)
		}
		if from == req.To {
			return nil, bad("the column is already there")
		}
		c := nw.Columns[from]
		nw.Columns = append(nw.Columns[:from:from], nw.Columns[from+1:]...)
		nw.Columns = append(nw.Columns[:req.To:req.To], append([]db.Column{c}, nw.Columns[req.To:]...)...)
	default:
		return nil, bad("unknown op %q", req.Op)
	}
	stmts, err := ss.o.Sess.PlanTable(&old, &nw)
	if err != nil {
		return nil, bad("%v", err)
	}
	if len(stmts) == 0 {
		return nil, bad("nothing to change")
	}
	return ss.storePlan(stmts, "", "")
}
