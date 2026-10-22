package clickhouse

import (
	"errors"
	"fmt"
	"strings"

	"tuidb/internal/db"
)

func (s *session) PlanAdmin(op db.AdminOp, ref db.ObjRef, opts map[string]string) ([]db.Statement, error) {
	name := dialect.QualifiedName(ref)
	newName := func() (string, error) {
		n := opts["new_name"]
		if n == "" {
			return "", errors.New("new_name is required")
		}
		return dialect.Quote(n), nil
	}
	var texts []string
	switch op {
	case db.OpDropObject:
		texts = []string{"DROP " + dropWord(ref) + " " + name}
	case db.OpTruncate:
		texts = []string{"TRUNCATE TABLE " + name}
	case db.OpClone:
		n, err := newName()
		if err != nil {
			return nil, err
		}
		texts = []string{"CREATE TABLE " + n + " AS " + name}
		if opts["with_data"] == "true" {
			texts = append(texts, "INSERT INTO "+n+" SELECT * FROM "+name)
		}
	case db.OpRenameObject:
		n, err := newName()
		if err != nil {
			return nil, err
		}
		texts = []string{"RENAME TABLE " + name + " TO " + n}
	case db.OpCreateDatabase:
		texts = []string{"CREATE DATABASE " + dialect.Quote(ref.Name)}
	case db.OpDropDatabase:
		texts = []string{"DROP DATABASE " + dialect.Quote(ref.Name)}
	case db.OpRenameDatabase: // only Atomic databases support this; the server says so otherwise
		n, err := newName()
		if err != nil {
			return nil, err
		}
		texts = []string{"RENAME DATABASE " + dialect.Quote(ref.Name) + " TO " + n}
	default:
		return nil, fmt.Errorf("clickhouse does not support %q", op)
	}
	return stmts(texts...), nil
}

func stmts(texts ...string) []db.Statement {
	out := make([]db.Statement, len(texts))
	for i, t := range texts {
		out[i] = db.Statement{Text: t}
	}
	return out
}

// effType applies the Nullable flag to the type text.
func effType(c db.Column) string {
	t := c.Type
	isNull := strings.HasPrefix(t, "Nullable(") && strings.HasSuffix(t, ")")
	switch {
	case c.Nullable && !isNull && !strings.HasPrefix(t, "Array(") && !strings.HasPrefix(t, "Map(") &&
		!strings.HasPrefix(t, "Tuple(") && !strings.HasPrefix(t, "LowCardinality("):
		return "Nullable(" + t + ")"
	case !c.Nullable && isNull:
		return t[len("Nullable(") : len(t)-1]
	}
	return t
}

func colDef(c db.Column) string {
	d := dialect.Quote(c.Name) + " " + effType(c)
	if c.HasDef || c.Default != "" {
		d += " DEFAULT " + c.Default
	}
	return d
}

var errKey = errors.New("ClickHouse sorting key cannot be altered")

func (s *session) PlanTable(old, nw *db.TableDef) ([]db.Statement, error) {
	if len(nw.ForeignKeys) > 0 {
		return nil, errors.New("ClickHouse has no foreign keys")
	}
	if old == nil {
		return createTable(nw)
	}
	if len(nw.Indexes) > 0 || len(old.Indexes) > 0 {
		return nil, errors.New("ClickHouse indexes cannot be edited here")
	}
	if strings.Join(old.PrimaryKey, ",") != strings.Join(nw.PrimaryKey, ",") {
		return nil, errKey
	}
	tbl := dialect.QualifiedName(db.ObjRef{Schema: old.Schema, Name: old.Name})
	alter := "ALTER TABLE " + tbl + " "
	var out []string

	oldByName := map[string]db.Column{}
	for _, c := range old.Columns {
		oldByName[c.Name] = c
	}
	kept := map[string]bool{}
	for _, c := range nw.Columns {
		if c.OrigName != "" {
			kept[c.OrigName] = true
		}
	}
	for _, c := range old.Columns {
		if !kept[c.Name] {
			out = append(out, alter+"DROP COLUMN "+dialect.Quote(c.Name))
		}
	}
	// cur tracks the column order as the statements below will leave it.
	var cur []string
	for _, c := range old.Columns {
		if kept[c.Name] {
			cur = append(cur, c.Name)
		}
	}
	for _, c := range nw.Columns {
		if c.OrigName != "" && c.OrigName != c.Name {
			out = append(out, alter+"RENAME COLUMN "+dialect.Quote(c.OrigName)+" TO "+dialect.Quote(c.Name))
			for i := range cur {
				if cur[i] == c.OrigName {
					cur[i] = c.Name
				}
			}
		}
	}
	for i, c := range nw.Columns {
		pos := ""
		if i == 0 {
			pos = " FIRST"
		} else if i < len(cur) {
			pos = " AFTER " + dialect.Quote(nw.Columns[i-1].Name)
		}
		if c.OrigName == "" {
			out = append(out, alter+"ADD COLUMN "+colDef(c)+pos)
			cur = append(cur[:i], append([]string{c.Name}, cur[i:]...)...)
			continue
		}
		moved := cur[i] != c.Name
		o := oldByName[c.OrigName]
		changed := effType(o) != effType(c) || (o.HasDef || o.Default != "") != (c.HasDef || c.Default != "") || o.Default != c.Default
		if !moved && !changed {
			continue
		}
		if !moved {
			pos = ""
		} else {
			for j := range cur {
				if cur[j] == c.Name {
					cur = append(cur[:j], cur[j+1:]...)
					break
				}
			}
			cur = append(cur[:i], append([]string{c.Name}, cur[i:]...)...)
		}
		out = append(out, alter+"MODIFY COLUMN "+colDef(c)+pos)
		if !c.HasDef && c.Default == "" && (o.HasDef || o.Default != "") {
			out = append(out, alter+"MODIFY COLUMN "+dialect.Quote(c.Name)+" REMOVE DEFAULT")
		}
	}
	if nw.Name != old.Name {
		out = append(out, "RENAME TABLE "+tbl+" TO "+dialect.QualifiedName(db.ObjRef{Schema: nw.Schema, Name: nw.Name}))
	}
	return stmts(out...), nil
}

func createTable(t *db.TableDef) ([]db.Statement, error) {
	if len(t.Indexes) > 0 {
		return nil, errors.New("ClickHouse indexes cannot be created here")
	}
	if t.Name == "" || len(t.Columns) == 0 {
		return nil, errors.New("a table needs a name and at least one column")
	}
	defs := make([]string, len(t.Columns))
	pk := t.PrimaryKey
	for i, c := range t.Columns {
		defs[i] = "  " + colDef(c)
		if len(t.PrimaryKey) == 0 && c.PK {
			pk = append(pk, c.Name)
		}
	}
	order := "tuple()"
	if len(pk) > 0 {
		q := make([]string, len(pk))
		for i, c := range pk {
			q[i] = dialect.Quote(c)
		}
		order = "(" + strings.Join(q, ", ") + ")"
	}
	return stmts("CREATE TABLE " + dialect.QualifiedName(db.ObjRef{Schema: t.Schema, Name: t.Name}) +
		" (\n" + strings.Join(defs, ",\n") + "\n) ENGINE = MergeTree ORDER BY " + order), nil
}
