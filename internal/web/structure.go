package web

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"webtuidb/internal/db"
)

func (s *Server) datatypes(ss *session, r *http.Request) (any, error) {
	t := db.DataTypesOf(ss.o.Sess)
	if t == nil {
		t = []string{}
	}
	return map[string]any{"types": t}, nil
}

// structurePlan plans a whole-table definition change ({old, new}; old null = create) through the driver's
// PlanTable, the call the terminal UI's structure editor makes, and stores it for /apply.
func (s *Server) structurePlan(ss *session, r *http.Request) (any, error) {
	var req struct {
		Old *db.TableDef
		New *db.TableDef
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if req.New == nil || strings.TrimSpace(req.New.Name) == "" {
		return nil, bad("new.Name is required")
	}
	if len(req.New.Columns) == 0 {
		return nil, bad("a table needs at least one column")
	}
	for _, c := range req.New.Columns {
		if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Type) == "" {
			return nil, bad("every column needs a name and a data type")
		}
		if !validColumnType(strings.TrimSpace(c.Type)) {
			return nil, bad("invalid data type %q for column %q", c.Type, c.Name)
		}
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	old := req.Old
	if live := ss.liveTableDef(ctx, req.Old, req.New); live != nil {
		old = live // never trust the client's copy of what the table looks like now
	}
	stmts, err := ss.o.Sess.PlanTable(old, req.New)
	if err != nil {
		return nil, bad("%v", err)
	}
	if len(stmts) == 0 {
		return nil, bad("no structure changes")
	}
	return ss.storePlan(stmts, "", "")
}

// liveTableDef is the table's current definition read from the server (what the terminal UI's oldDef holds),
// for the table the edit starts from (old.Name, else new.Name); nil when that table does not exist.
func (ss *session) liveTableDef(ctx context.Context, old, nw *db.TableDef) *db.TableDef {
	sess := ss.o.Sess
	schema, name := nw.Schema, nw.Name
	if old != nil && old.Name != "" {
		schema, name = old.Schema, old.Name
	}
	if schema == "" {
		schema = sess.CurrentSchema()
	}
	exists := false
	if objs, err := sess.Objects(ctx, schema); err == nil {
		for _, o := range objs {
			if o.Name == name && o.Kind == db.KindTable {
				exists = true
			}
		}
	}
	if !exists {
		return nil
	}
	ref := db.ObjRef{Schema: schema, Name: name, Kind: db.KindTable}
	cols, err := sess.Columns(ctx, ref)
	if err != nil {
		return nil
	}
	d := &db.TableDef{Schema: schema, Name: name}
	for _, c := range cols {
		c.OrigName = c.Name
		c.FK = nil
		d.Columns = append(d.Columns, c)
		if c.PK {
			d.PrimaryKey = append(d.PrimaryKey, c.Name)
		}
	}
	caps := sess.Caps()
	if caps.Indexes {
		if ixs, err := sess.Indexes(ctx, ref); err == nil {
			for _, ix := range ixs {
				if !ix.Primary {
					d.Indexes = append(d.Indexes, ix)
				}
			}
		}
	}
	if caps.ForeignKeys {
		if fks, err := sess.ForeignKeys(ctx, ref); err == nil {
			d.ForeignKeys = fks
		}
	}
	return d
}

var columnTypeRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_ ]*(\(\s*\d+(\s*,\s*\d+)?\s*\))?(\[\])?$`)

// validColumnType accepts a plain SQL type name with optional length / precision and array suffix, so a
// client cannot smuggle SQL into CREATE TABLE / ALTER TABLE through a type.
func validColumnType(t string) bool { return len(t) <= 64 && columnTypeRE.MatchString(t) }
