package mysql

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"webtuidb/internal/db"
)

var q = db.MySQL.Quote

func qualified(schema, name string) string {
	return db.MySQL.QualifiedName(db.ObjRef{Schema: schema, Name: name})
}

var kindKeyword = map[db.Kind]string{db.KindTable: "TABLE", db.KindView: "VIEW", db.KindFunction: "FUNCTION", db.KindProcedure: "PROCEDURE", db.KindSequence: "SEQUENCE"}

func (s *session) Script(ctx context.Context, kind db.ScriptKind, ref db.ObjRef) (string, error) {
	name := db.MySQL.QualifiedName(ref)
	switch kind {
	case db.ScriptCreate:
		return s.DDL(ctx, ref)
	case db.ScriptDrop:
		kw, ok := kindKeyword[ref.Kind]
		if !ok {
			return "", fmt.Errorf("cannot drop a %s", ref.Kind)
		}
		return "DROP " + kw + " " + name + ";", nil
	case db.ScriptTruncate:
		return strings.Join(s.emptyTable(ref), ";\n") + ";", nil
	case db.ScriptSelectTop:
		return "SELECT * FROM " + name + " LIMIT 100;", nil
	}
	return "", fmt.Errorf("unknown script kind %q", kind)
}

var identRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func (s *session) PlanAdmin(op db.AdminOp, ref db.ObjRef, opts map[string]string) ([]db.Statement, error) {
	name := db.MySQL.QualifiedName(ref)
	newName := opts["new_name"]
	switch op {
	case db.OpDropObject:
		kw, ok := kindKeyword[ref.Kind]
		if !ok {
			return nil, fmt.Errorf("cannot drop a %s", ref.Kind)
		}
		return stmts("DROP " + kw + " " + name), nil
	case db.OpTruncate:
		if opts["cascade"] == "true" {
			return nil, fmt.Errorf("%s has no TRUNCATE ... CASCADE: empty the child tables first", s.engine())
		}
		return stmts(s.emptyTable(ref)...), nil
	case db.OpClone:
		if ref.Kind != db.KindTable {
			return nil, errors.New("only tables can be cloned")
		}
		if newName == "" {
			return nil, errors.New("clone needs a new name")
		}
		dst := qualified(ref.Schema, newName)
		out := stmts("CREATE TABLE " + dst + " LIKE " + name)
		if opts["with_data"] == "true" {
			cols, err := s.copyColumns(ref)
			if err != nil {
				return nil, err
			}
			ins := "INSERT INTO " + dst + " SELECT * FROM " + name
			if cols != "" {
				ins = "INSERT INTO " + dst + " (" + cols + ") SELECT " + cols + " FROM " + name
			}
			out = append(out, stmts(ins)...)
		}
		return out, nil
	case db.OpRenameObject:
		if ref.Kind != db.KindTable && ref.Kind != db.KindView && ref.Kind != db.KindSequence { // RENAME TABLE also renames a MariaDB sequence
			return nil, fmt.Errorf("%s cannot rename a %s", s.engine(), ref.Kind)
		}
		if newName == "" {
			return nil, errors.New("rename needs a new name")
		}
		return stmts("RENAME TABLE " + name + " TO " + qualified(ref.Schema, newName)), nil
	case db.OpCreateDatabase:
		t := "CREATE DATABASE " + q(ref.Name)
		for _, o := range [][2]string{{"encoding", " CHARACTER SET "}, {"collation", " COLLATE "}} {
			if v := opts[o[0]]; v != "" {
				if !identRe.MatchString(v) {
					return nil, fmt.Errorf("bad %s %q", o[0], v)
				}
				t += o[1] + v
			}
		}
		return stmts(t), nil
	case db.OpDropDatabase:
		return stmts("DROP DATABASE " + q(ref.Name)), nil
	case db.OpRenameDatabase:
		return nil, fmt.Errorf("%s cannot rename a database: create a new one and move the tables with RENAME TABLE", s.engine())
	}
	return nil, fmt.Errorf("unknown admin operation %q", op)
}

// copyColumns lists the table's columns without the generated ones, which INSERT cannot set.
func (s *session) copyColumns(ref db.ObjRef) (string, error) {
	if s.db() == nil { // no connection (unit tests): copy everything
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cols, err := s.loadColumns(ctx, ref)
	if err != nil {
		return "", err
	}
	var names []string
	for _, c := range cols {
		if !strings.HasPrefix(strings.ToUpper(c.Extra), "GENERATED ALWAYS AS") {
			names = append(names, c.Name)
		}
	}
	if len(names) == 0 {
		return "", errors.New("clone: the table has no copyable columns")
	}
	return colList(names), nil
}

func stmts(texts ...string) []db.Statement {
	out := make([]db.Statement, len(texts))
	for i, t := range texts {
		out[i] = db.Statement{Text: t}
	}
	return out
}

var (
	simpleDefault = regexp.MustCompile(`(?i)^(?:'(?:[^'\\]|\\.|'')*'|-?\d+(?:\.\d+)?(?:e[+-]?\d+)?|null|true|false|(?:current_timestamp|now|localtime|localtimestamp)(?:\(\d*\))?|b'[01]*'|0x[0-9a-f]+)$`)
)

// defaultSQL parenthesizes expression defaults: MySQL only accepts them as DEFAULT (expr).
func defaultSQL(d string) string {
	if simpleDefault.MatchString(d) {
		return d
	}
	return "(" + d + ")"
}

func columnSQL(c db.Column) (string, error) {
	if c.Name == "" || strings.TrimSpace(c.Type) == "" {
		return "", fmt.Errorf("column %q needs a name and a type", c.Name)
	}
	t := q(c.Name) + " " + c.Type
	if strings.HasPrefix(strings.ToUpper(c.Extra), "GENERATED ALWAYS AS") { // the AS clause goes first; no DEFAULT allowed
		t += " " + c.Extra
		if !c.Nullable {
			t += " NOT NULL"
		}
		return t, nil
	}
	if c.Nullable {
		t += " NULL"
	} else {
		t += " NOT NULL"
	}
	if c.Default != "" {
		t += " DEFAULT " + defaultSQL(c.Default)
	}
	if c.Extra != "" {
		t += " " + c.Extra
	}
	return t, nil
}

func colList(cols []string) string {
	qs := make([]string, len(cols))
	for i, c := range cols {
		qs[i] = q(c)
	}
	return strings.Join(qs, ", ")
}

var keyPart = regexp.MustCompile(`^(.*?)(\(\d+\))?( DESC)?$`)

func indexSQL(ix db.Index) (string, error) {
	if len(ix.Columns) == 0 {
		return "", fmt.Errorf("index %q has no columns", ix.Name)
	}
	kw := "INDEX"
	switch strings.ToUpper(ix.Algorithm) {
	case "FULLTEXT":
		kw = "FULLTEXT INDEX"
	case "SPATIAL":
		kw = "SPATIAL INDEX"
	default:
		if ix.Unique {
			kw = "UNIQUE INDEX"
		}
	}
	if ix.Name != "" {
		kw += " " + q(ix.Name)
	}
	parts := make([]string, len(ix.Columns))
	for i, c := range ix.Columns {
		if parts[i] = c; !strings.HasPrefix(c, "(") { // "(expr)" is a functional key part, emitted raw
			m := keyPart.FindStringSubmatch(c) // "col", "col(10)", "col DESC"
			parts[i] = q(m[1]) + m[2] + m[3]
		}
	}
	return kw + " (" + strings.Join(parts, ", ") + ")", nil
}

var fkRules = map[string]bool{"": true, "NO ACTION": true, "RESTRICT": true, "CASCADE": true, "SET NULL": true, "SET DEFAULT": true}

func fkSQL(fk db.ForeignKey) (string, error) {
	on := func(kind, rule string) (string, error) {
		rule = strings.ToUpper(rule)
		if !fkRules[rule] {
			return "", fmt.Errorf("bad foreign key rule %q", rule)
		}
		if rule == "" {
			return "", nil
		}
		return " ON " + kind + " " + rule, nil
	}
	if len(fk.Columns) == 0 || len(fk.Columns) != len(fk.RefColumns) || fk.RefTable == "" {
		return "", fmt.Errorf("foreign key %q needs matching columns and a referenced table", fk.Name)
	}
	t := "FOREIGN KEY (" + colList(fk.Columns) + ") REFERENCES " + qualified(fk.RefSchema, fk.RefTable) + " (" + colList(fk.RefColumns) + ")"
	if fk.Name != "" {
		t = "CONSTRAINT " + q(fk.Name) + " " + t
	}
	for _, r := range [][2]string{{"DELETE", fk.OnDelete}, {"UPDATE", fk.OnUpdate}} {
		s, err := on(r[0], r[1])
		if err != nil {
			return "", err
		}
		t += s
	}
	return t, nil
}

func (s *session) PlanTable(old, new *db.TableDef) ([]db.Statement, error) {
	out, err := planTable(old, new)
	if err == nil && old != nil && s.isVersioned(old.Schema, old.Name) {
		// MariaDB refuses every ALTER of a system-versioned table unless history is kept (error 4119).
		for i := range out {
			out[i].Text = "SET STATEMENT system_versioning_alter_history=KEEP FOR " + out[i].Text
		}
	}
	return out, err
}

// emptyTable is TRUNCATE, which a system-versioned table refuses (error 4137): its rows are deleted
// and then their history is purged, so the table ends up as empty as after a TRUNCATE.
func (s *session) emptyTable(ref db.ObjRef) []string {
	name := db.MySQL.QualifiedName(ref)
	if s.isVersioned(ref.Schema, ref.Name) {
		return []string{"DELETE FROM " + name, "DELETE HISTORY FROM " + name}
	}
	return []string{"TRUNCATE TABLE " + name}
}

func primaryKey(t *db.TableDef) []string {
	if len(t.PrimaryKey) > 0 {
		return t.PrimaryKey
	}
	var pk []string
	for _, c := range t.Columns {
		if c.PK {
			pk = append(pk, c.Name)
		}
	}
	return pk
}

func planTable(old, new *db.TableDef) ([]db.Statement, error) {
	if new == nil || new.Name == "" {
		return nil, errors.New("table needs a name")
	}
	if old == nil {
		return planCreate(new)
	}
	tbl := qualified(old.Schema, old.Name)
	renames := map[string]string{} // old column name -> new name
	kept := map[string]bool{}
	oldCols := map[string]db.Column{}
	for _, c := range old.Columns {
		oldCols[c.Name] = c
	}
	for _, c := range new.Columns {
		if c.OrigName != "" {
			if _, ok := oldCols[c.OrigName]; !ok {
				return nil, fmt.Errorf("column %q was renamed from unknown column %q", c.Name, c.OrigName)
			}
			kept[c.OrigName] = true
			renames[c.OrigName] = c.Name
		}
	}
	mapNames := func(names []string) []string {
		out := make([]string, len(names))
		for i, n := range names {
			if r, ok := renames[n]; ok {
				n = r
			}
			out[i] = n
		}
		return out
	}

	var fkDrops, clauses []string
	newFK := map[string]db.ForeignKey{}
	for _, fk := range new.ForeignKeys {
		newFK[fk.Name] = fk
	}
	oldFK := map[string]db.ForeignKey{}
	for _, fk := range old.ForeignKeys {
		oldFK[fk.Name] = fk
		if n, ok := newFK[fk.Name]; !ok || !sameFK(fk, n, mapNames) {
			fkDrops = append(fkDrops, "DROP FOREIGN KEY "+q(fk.Name))
		}
	}
	newIx := map[string]db.Index{}
	for _, ix := range new.Indexes {
		if !ix.Primary {
			newIx[ix.Name] = ix
		}
	}
	oldIx := map[string]db.Index{}
	for _, ix := range old.Indexes {
		if ix.Primary {
			continue
		}
		oldIx[ix.Name] = ix
		if n, ok := newIx[ix.Name]; !ok || !sameIndex(ix, n, mapNames) {
			clauses = append(clauses, "DROP INDEX "+q(ix.Name))
		}
	}
	for _, c := range old.Columns {
		if !kept[c.Name] {
			clauses = append(clauses, "DROP COLUMN "+q(c.Name))
		}
	}

	var cur []string // simulated column order, so positions are only emitted where needed
	for _, c := range old.Columns {
		if kept[c.Name] {
			cur = append(cur, renames[c.Name])
		}
	}
	for i, c := range new.Columns {
		def, err := columnSQL(c)
		if err != nil {
			return nil, err
		}
		pos := ""
		if i == 0 {
			pos = " FIRST"
		} else {
			pos = " AFTER " + q(new.Columns[i-1].Name)
		}
		if c.OrigName == "" {
			if i >= len(cur) {
				pos = ""
			}
			clauses = append(clauses, "ADD COLUMN "+def+pos)
			cur = append(cur[:i], append([]string{c.Name}, cur[i:]...)...)
			continue
		}
		moved := cur[i] != c.Name
		if moved {
			j := indexOf(cur, c.Name)
			cur = append(cur[:j], cur[j+1:]...)
			cur = append(cur[:i], append([]string{c.Name}, cur[i:]...)...)
		}
		oc := oldCols[c.OrigName]
		changed := oc.Type != c.Type || oc.Nullable != c.Nullable || oc.Default != c.Default || oc.Extra != c.Extra
		switch {
		case c.OrigName != c.Name:
			clauses = append(clauses, "CHANGE COLUMN "+q(c.OrigName)+" "+def+movePos(moved, pos))
		case changed || moved:
			clauses = append(clauses, "MODIFY COLUMN "+def+movePos(moved, pos))
		}
	}

	if oldPK, newPK := mapNames(primaryKey(old)), primaryKey(new); !equalStrings(oldPK, newPK) {
		if len(oldPK) > 0 {
			clauses = append(clauses, "DROP PRIMARY KEY")
		}
		if len(newPK) > 0 {
			clauses = append(clauses, "ADD PRIMARY KEY ("+colList(newPK)+")")
		}
	}
	for _, ix := range new.Indexes {
		if o, ok := oldIx[ix.Name]; ix.Primary || ok && sameIndex(o, ix, mapNames) {
			continue
		}
		t, err := indexSQL(ix)
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, "ADD "+t)
	}
	var fkAdds []string
	for _, fk := range new.ForeignKeys {
		if o, ok := oldFK[fk.Name]; ok && sameFK(o, fk, mapNames) {
			continue
		}
		t, err := fkSQL(fk)
		if err != nil {
			return nil, err
		}
		fkAdds = append(fkAdds, "ADD "+t)
	}
	clauses = append(clauses, fkAdds...)
	if new.Name != old.Name || new.Schema != old.Schema {
		clauses = append(clauses, "RENAME TO "+qualified(new.Schema, new.Name))
	}

	var out []db.Statement
	alter := func(parts []string) {
		if len(parts) > 0 {
			out = append(out, db.Statement{Text: "ALTER TABLE " + tbl + "\n  " + strings.Join(parts, ",\n  ")})
		}
	}
	// A dropped and re-added foreign key of the same name must be two ALTERs.
	alter(fkDrops)
	alter(clauses)
	return out, nil
}

// movePos keeps the position clause only for columns that actually moved.
func movePos(moved bool, pos string) string {
	if moved {
		return pos
	}
	return ""
}

func planCreate(t *db.TableDef) ([]db.Statement, error) {
	if len(t.Columns) == 0 {
		return nil, errors.New("table needs at least one column")
	}
	var parts []string
	for _, c := range t.Columns {
		def, err := columnSQL(c)
		if err != nil {
			return nil, err
		}
		parts = append(parts, def)
	}
	if pk := primaryKey(t); len(pk) > 0 {
		parts = append(parts, "PRIMARY KEY ("+colList(pk)+")")
	}
	for _, ix := range t.Indexes {
		if ix.Primary {
			continue
		}
		s, err := indexSQL(ix)
		if err != nil {
			return nil, err
		}
		parts = append(parts, s)
	}
	for _, fk := range t.ForeignKeys {
		s, err := fkSQL(fk)
		if err != nil {
			return nil, err
		}
		parts = append(parts, s)
	}
	return stmts("CREATE TABLE " + qualified(t.Schema, t.Name) + " (\n  " + strings.Join(parts, ",\n  ") + "\n)"), nil
}

func sameIndex(a, b db.Index, mapOld func([]string) []string) bool {
	special := func(x db.Index) string {
		u := strings.ToUpper(x.Algorithm)
		if u == "FULLTEXT" || u == "SPATIAL" {
			return u
		}
		return ""
	}
	return a.Unique == b.Unique && special(a) == special(b) && equalStrings(mapOld(a.Columns), b.Columns)
}

func sameFK(a, b db.ForeignKey, mapOld func([]string) []string) bool {
	rule := func(r string) string {
		if r = strings.ToUpper(r); r == "" {
			return "NO ACTION"
		}
		return r
	}
	return a.RefTable == b.RefTable && a.RefSchema == b.RefSchema && rule(a.OnDelete) == rule(b.OnDelete) && rule(a.OnUpdate) == rule(b.OnUpdate) &&
		equalStrings(mapOld(a.Columns), b.Columns) && equalStrings(a.RefColumns, b.RefColumns)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
