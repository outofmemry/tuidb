package web

import (
	"context"
	"strings"
	"time"
	"webtuidb/internal/db"
)

// tableInfo says which table an editable query result reads, so the client can send /plan edits for it.
type tableInfo struct {
	Schema string   `json:"schema"`
	Name   string   `json:"name"`
	PK     []string `json:"pk"`
}

// editableResult marks out editable when stmt is a plain single-table SELECT on a table with a primary key
// whose columns are all in the select list (db.EditableSelect + the pk check). The result is cached like a
// table page, so /plan works on it with the same resultId and row numbers.
func (ss *session) editableResult(ctx context.Context, run db.Session, stmt string, res *db.Result, out *resultJSON) {
	sess := ss.o.Sess
	// a statement that ran on a pinned connection may see temp tables or another database: not editable
	if !res.HasRows || ss.conn.ReadOnly || run != sess {
		return
	}
	schema, name, ok := db.EditableSelect(stmt, sess.Caps().Dialect)
	if !ok {
		return
	}
	if schema == "" {
		schema = sess.CurrentSchema()
	}
	ref := db.ObjRef{Schema: schema, Name: name, Kind: db.KindTable}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	isTable := false
	if objs, err := sess.Objects(ctx, schema); err == nil {
		for _, o := range objs {
			if o.Name == name && o.Kind == db.KindTable {
				isTable = true
			}
		}
	}
	if !isTable {
		return
	}
	cols, err := ss.columns(ctx, ref)
	if err != nil {
		return
	}
	have := map[string]bool{}
	for _, c := range res.Columns {
		have[c.Name] = true
	}
	pk := []string{}
	for _, c := range cols {
		if c.PK {
			if !have[c.Name] {
				return
			}
			pk = append(pk, c.Name)
		}
	}
	if len(pk) == 0 {
		return
	}
	*out = toJSON(res, cols)
	out.Editable = true
	out.ResultID = ss.put(&cached{ref: ref, cols: cols, res: res})
	out.Table = &tableInfo{Schema: schema, Name: name, PK: pk}
}

// checkSyntax enforces a Mongo query tab's syntax choice: "command" is a raw {...} document, "shell" is
// db.collection.method(...). Empty (or any other dialect) accepts both, as the driver tells them apart by the "{".
func checkSyntax(dialect, syntax, text string) error {
	if dialect != "mongo" || syntax == "" {
		return nil
	}
	cmd := strings.HasPrefix(strings.TrimSpace(text), "{")
	switch syntax {
	case "command":
		if !cmd {
			return bad("command syntax runs a {...} document; switch the tab to shell syntax to run db.collection.method(...)")
		}
	case "shell":
		if cmd {
			return bad("shell syntax runs db.collection.method(...); switch the tab to command syntax to run a {...} document")
		}
	default:
		return bad("unknown syntax %q (shell or command)", syntax)
	}
	return nil
}
