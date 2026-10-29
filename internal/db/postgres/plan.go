package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"webtuidb/internal/db"
)

var ident = db.PG.Quote

func qname(schema, name string) string {
	return db.PG.QualifiedName(db.ObjRef{Schema: schema, Name: name})
}

func stmt(text string) db.Statement { return db.Statement{Text: text} }

func opt(opts map[string]string, key string) bool {
	switch strings.ToLower(opts[key]) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func kindWord(k db.Kind) (string, error) {
	switch k {
	case db.KindTable, "":
		return "TABLE", nil
	case db.KindView:
		return "VIEW", nil
	case db.KindMatView:
		return "MATERIALIZED VIEW", nil
	case db.KindFunction:
		return "FUNCTION", nil
	case db.KindProcedure:
		return "PROCEDURE", nil
	case db.KindSequence:
		return "SEQUENCE", nil
	case db.KindType:
		return "TYPE", nil
	}
	return "", fmt.Errorf("unsupported object kind %q", k)
}

func (s *session) PlanAdmin(op db.AdminOp, ref db.ObjRef, opts map[string]string) ([]db.Statement, error) {
	newName := strings.TrimSpace(opts["new_name"])
	switch op {
	case db.OpCreateDatabase:
		t := "CREATE DATABASE " + ident(ref.Name)
		if enc := opts["encoding"]; enc != "" {
			t += " ENCODING " + lit(enc)
		}
		if col := opts["collation"]; col != "" {
			t += " LC_COLLATE " + lit(col) + " LC_CTYPE " + lit(col)
		}
		if opts["encoding"] != "" || opts["collation"] != "" {
			t += " TEMPLATE template0" // template1's encoding/locale would otherwise have to match
		}
		return []db.Statement{stmt(t)}, nil
	case db.OpDropDatabase:
		return []db.Statement{stmt("DROP DATABASE " + ident(ref.Name))}, nil
	case db.OpCreateSchema, db.OpDropSchema:
		name := ref.Schema
		if name == "" {
			name = ref.Name
		}
		if name == "" {
			return nil, errors.New("schema name is required")
		}
		if op == db.OpCreateSchema {
			return []db.Statement{stmt("CREATE SCHEMA " + ident(name))}, nil
		}
		t := "DROP SCHEMA " + ident(name)
		if opt(opts, "cascade") {
			t += " CASCADE"
		}
		return []db.Statement{stmt(t)}, nil
	case db.OpRenameDatabase:
		if newName == "" {
			return nil, errors.New("rename database: new_name is required")
		}
		return []db.Statement{stmt("ALTER DATABASE " + ident(ref.Name) + " RENAME TO " + ident(newName))}, nil
	}
	ref = s.ref(ref)
	word, err := kindWord(ref.Kind)
	if err != nil {
		return nil, err
	}
	qn := db.PG.QualifiedName(ref)
	switch op {
	case db.OpDropObject:
		t := "DROP " + word + " " + qn
		if opt(opts, "cascade") {
			t += " CASCADE"
		}
		return []db.Statement{stmt(t)}, nil
	case db.OpTruncate:
		if word != "TABLE" {
			return nil, fmt.Errorf("cannot truncate a %s", strings.ToLower(word))
		}
		t := "TRUNCATE TABLE " + qn
		if opt(opts, "restart_identity") {
			t += " RESTART IDENTITY"
		}
		if opt(opts, "cascade") {
			t += " CASCADE"
		}
		return []db.Statement{stmt(t)}, nil
	case db.OpClone:
		if word != "TABLE" || newName == "" {
			return nil, errors.New("clone: needs a table and new_name")
		}
		nq := qname(ref.Schema, newName)
		out := []db.Statement{stmt("CREATE TABLE " + nq + " (LIKE " + qn + " INCLUDING ALL)")}
		if opt(opts, "with_data") {
			out = append(out, stmt("INSERT INTO "+nq+" OVERRIDING SYSTEM VALUE SELECT * FROM "+qn), stmt(resyncSequences(ref.Schema, newName, nq)))
		}
		return out, nil
	case db.OpRenameObject:
		if newName == "" {
			return nil, errors.New("rename: new_name is required")
		}
		return []db.Statement{stmt("ALTER " + word + " " + qn + " RENAME TO " + ident(newName))}, nil
	}
	return nil, fmt.Errorf("unsupported operation %q", op)
}

// resyncSequences gives a clone its own sequence for every serial column (LIKE copies the source's
// nextval default) and moves each identity/serial sequence past the copied rows.
func resyncSequences(schema, name, nq string) string {
	return `DO $$ DECLARE r record; sq text; BEGIN
FOR r IN SELECT a.attname AS n, a.attidentity AS i, pg_get_expr(d.adbin, d.adrelid) AS e
  FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
  WHERE a.attrelid = ` + lit(nq) + `::regclass AND a.attnum > 0 AND NOT a.attisdropped LOOP
  sq := NULL;
  IF r.i <> '' THEN sq := pg_get_serial_sequence(` + lit(nq) + `, r.n);
  ELSIF r.e LIKE 'nextval(%' THEN
    sq := format('%I.%I', ` + lit(schema) + `, ` + lit(name) + ` || '_' || r.n || '_seq');
    EXECUTE format('CREATE SEQUENCE %s OWNED BY %s.%I', sq, ` + lit(nq) + `::regclass, r.n);
    EXECUTE format('ALTER TABLE %s ALTER COLUMN %I SET DEFAULT nextval(%L)', ` + lit(nq) + `::regclass, r.n, sq);
  END IF;
  IF sq IS NOT NULL THEN
    EXECUTE format('SELECT setval(%L, coalesce(max(%I), 1), max(%I) IS NOT NULL) FROM %s', sq, r.n, r.n, ` + lit(nq) + `::regclass);
  END IF;
END LOOP; END $$`
}

func lit(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func quoteList(cols []string) string {
	qs := make([]string, len(cols))
	for i, c := range cols {
		qs[i] = ident(c)
	}
	return strings.Join(qs, ", ")
}

// columnSQL is a column definition as the table has it (DDL view): a serial column keeps its nextval default.
func columnSQL(c db.Column) string {
	t := ident(c.Name) + " " + c.Type
	switch {
	case strings.HasPrefix(c.Extra, "GENERATED"): // identity and stored generated columns carry their own clause
		t += " " + c.Extra
	case c.HasDef || c.Default != "":
		t += " DEFAULT " + c.Default
	}
	if !c.Nullable {
		t += " NOT NULL"
	}
	return t
}

// newColumnSQL is columnSQL for CREATE TABLE / ADD COLUMN planned from a definition (schema diff, designer):
// a serial column is spelled serial again so it gets its own sequence instead of sharing the source's.
func newColumnSQL(c db.Column) string {
	if c.Extra == "serial" {
		if st, ok := map[string]string{"integer": "serial", "bigint": "bigserial", "smallint": "smallserial"}[c.Type]; ok {
			c.Type, c.Default, c.HasDef = st, "", false
		}
	}
	return columnSQL(c)
}

func fkSQL(fk db.ForeignKey) string {
	t := ""
	if fk.Name != "" {
		t = "CONSTRAINT " + ident(fk.Name) + " "
	}
	t += "FOREIGN KEY (" + quoteList(fk.Columns) + ") REFERENCES " + qname(fk.RefSchema, fk.RefTable) + " (" + quoteList(fk.RefColumns) + ")"
	if fk.OnUpdate != "" {
		t += " ON UPDATE " + fk.OnUpdate
	}
	if fk.OnDelete != "" {
		t += " ON DELETE " + fk.OnDelete
	}
	return t
}

// indexSQL renders CREATE INDEX; entries that are not table columns are expressions and stay raw.
func indexSQL(t *db.TableDef, ix db.Index) string {
	isCol := map[string]bool{}
	for _, c := range t.Columns {
		isCol[c.Name] = true
	}
	cols := make([]string, len(ix.Columns))
	for i, c := range ix.Columns {
		if cols[i] = c; isCol[c] {
			cols[i] = ident(c)
		}
	}
	s := "CREATE "
	if ix.Unique {
		s += "UNIQUE "
	}
	s += "INDEX "
	if ix.Name != "" {
		s += ident(ix.Name) + " "
	}
	s += "ON " + qname(t.Schema, t.Name)
	if ix.Algorithm != "" {
		s += " USING " + ix.Algorithm
	}
	s += " (" + strings.Join(cols, ", ") + ")"
	if ix.Condition != "" {
		s += " WHERE " + ix.Condition
	}
	return s
}

func pkOf(t *db.TableDef) []string {
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

func (s *session) PlanTable(old, new *db.TableDef) ([]db.Statement, error) {
	if new == nil {
		return nil, errors.New("no table definition")
	}
	n := *new
	if n.Schema == "" {
		n.Schema = s.CurrentSchema()
	}
	if old == nil {
		return planCreate(&n)
	}
	o := *old
	if o.Schema == "" {
		o.Schema = n.Schema
	}
	cons, pk, err := s.constraintNames(&o)
	if err != nil {
		return nil, err
	}
	return planAlter(&o, &n, cons, pk)
}

// constraintNames maps the indexes behind UNIQUE/EXCLUDE constraints to the constraint name and returns
// the real primary key constraint name; they are not droppable (or named) the way a plain index is.
func (s *session) constraintNames(t *db.TableDef) (map[string]string, string, error) {
	cons, pk := map[string]string{}, ""
	if s.p() == nil { // no connection (unit tests): fall back to the default names
		return cons, pk, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err := s.query(ctx, `SELECT ic.relname::text, k.conname::text, k.contype::text FROM pg_constraint k JOIN pg_class ic ON ic.oid = k.conindid
		WHERE k.conrelid = $1::regclass AND k.contype IN ('p','u','x')`, []any{qname(t.Schema, t.Name)}, func(r pgx.Rows) error {
		var ix, con, typ string
		if err := r.Scan(&ix, &con, &typ); err != nil {
			return err
		}
		if typ == "p" {
			pk = con
		} else {
			cons[ix] = con
		}
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("list constraints of %s: %w", t.Name, err)
	}
	return cons, pk, nil
}

func planCreate(t *db.TableDef) ([]db.Statement, error) {
	if strings.TrimSpace(t.Name) == "" || len(t.Columns) == 0 {
		return nil, errors.New("a table needs a name and at least one column")
	}
	var parts []string
	for _, c := range t.Columns {
		if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Type) == "" {
			return nil, fmt.Errorf("column %q needs a name and a type", c.Name)
		}
		parts = append(parts, "  "+newColumnSQL(c))
	}
	if pk := pkOf(t); len(pk) > 0 {
		parts = append(parts, "  PRIMARY KEY ("+quoteList(pk)+")")
	}
	for _, fk := range t.ForeignKeys {
		parts = append(parts, "  "+fkSQL(fk))
	}
	out := []db.Statement{stmt("CREATE TABLE " + qname(t.Schema, t.Name) + " (\n" + strings.Join(parts, ",\n") + "\n)")}
	for _, ix := range t.Indexes {
		if !ix.Primary {
			out = append(out, stmt(indexSQL(t, ix)))
		}
	}
	return out, nil
}

func same(a, b []string) bool {
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

func action(a string) string {
	if a = strings.ToUpper(strings.TrimSpace(a)); a == "" {
		return "NO ACTION"
	}
	return a
}

func fkEqual(a, b db.ForeignKey) bool {
	return same(a.Columns, b.Columns) && same(a.RefColumns, b.RefColumns) && a.RefTable == b.RefTable &&
		(a.RefSchema == "" || b.RefSchema == "" || a.RefSchema == b.RefSchema) &&
		action(a.OnUpdate) == action(b.OnUpdate) && action(a.OnDelete) == action(b.OnDelete)
}

func algo(a string) string {
	if a = strings.ToLower(strings.TrimSpace(a)); a == "" {
		return "btree"
	}
	return a
}

func indexEqual(a, b db.Index) bool {
	return same(a.Columns, b.Columns) && a.Unique == b.Unique && algo(a.Algorithm) == algo(b.Algorithm) &&
		strings.TrimSpace(a.Condition) == strings.TrimSpace(b.Condition)
}

func defOf(c db.Column) string {
	if c.HasDef || c.Default != "" {
		return strings.TrimSpace(c.Default)
	}
	return ""
}

// planAlter orders statements so dependents go first: FKs/PK/indexes drop, columns drop/rename/alter/add,
// then PK/indexes/FKs are added and the table is renamed last (everything before uses the old name).
func planAlter(old, new *db.TableDef, cons map[string]string, pkName string) ([]db.Statement, error) {
	var out []db.Statement
	tbl := qname(old.Schema, old.Name)
	alter := func(clause string) { out = append(out, stmt("ALTER TABLE "+tbl+" "+clause)) }

	oldCols := map[string]db.Column{}
	for _, c := range old.Columns {
		oldCols[c.Name] = c
	}
	kept := map[string]bool{}
	renamed := map[string]string{} // old name -> new name
	for _, c := range new.Columns {
		if c.OrigName == "" {
			continue
		}
		if _, ok := oldCols[c.OrigName]; !ok {
			return nil, fmt.Errorf("column %q was renamed from %q, which does not exist", c.Name, c.OrigName)
		}
		kept[c.OrigName] = true
		renamed[c.OrigName] = c.Name
	}

	newFK := map[string]db.ForeignKey{}
	for _, fk := range new.ForeignKeys {
		newFK[fk.Name] = fk
	}
	for _, fk := range old.ForeignKeys {
		if nf, ok := newFK[fk.Name]; !ok || !fkEqual(fk, nf) {
			alter("DROP CONSTRAINT " + ident(fk.Name))
		}
	}

	oldPK := append([]string(nil), pkOf(old)...)
	for i, c := range oldPK {
		if nn, ok := renamed[c]; ok {
			oldPK[i] = nn
		}
	}
	newPK := pkOf(new)
	pkChanged := !same(oldPK, newPK)
	if pkChanged && len(oldPK) > 0 {
		name := old.Name + "_pkey"
		for _, ix := range old.Indexes {
			if ix.Primary {
				name = ix.Name
			}
		}
		if pkName != "" {
			name = pkName
		}
		alter("DROP CONSTRAINT " + ident(name))
	}

	newIx := map[string]db.Index{}
	for _, ix := range new.Indexes {
		newIx[ix.Name] = ix
	}
	dropped := map[string]bool{}
	for _, ix := range old.Indexes {
		if nx, ok := newIx[ix.Name]; !ix.Primary && (!ok || !indexEqual(ix, nx)) {
			if con, ok := cons[ix.Name]; ok { // backing index of a UNIQUE/EXCLUDE constraint
				alter("DROP CONSTRAINT " + ident(con))
			} else {
				out = append(out, stmt("DROP INDEX "+qname(old.Schema, ix.Name)))
			}
			dropped[ix.Name] = true
		}
	}

	for _, c := range old.Columns {
		if !kept[c.Name] {
			alter("DROP COLUMN " + ident(c.Name))
		}
	}
	for _, c := range new.Columns {
		if c.OrigName != "" && c.OrigName != c.Name {
			alter("RENAME COLUMN " + ident(c.OrigName) + " TO " + ident(c.Name))
		}
	}
	for _, c := range new.Columns {
		if c.OrigName == "" {
			if strings.TrimSpace(c.Type) == "" {
				return nil, fmt.Errorf("column %q needs a type", c.Name)
			}
			alter("ADD COLUMN " + newColumnSQL(c))
			continue
		}
		oc, col := oldCols[c.OrigName], ident(c.Name)
		if !strings.EqualFold(strings.TrimSpace(oc.Extra), strings.TrimSpace(c.Extra)) {
			return nil, fmt.Errorf("column %q: editing identity/generated attributes is not supported", c.Name)
		}
		if !strings.EqualFold(strings.TrimSpace(oc.Type), strings.TrimSpace(c.Type)) {
			alter("ALTER COLUMN " + col + " TYPE " + c.Type + " USING " + col + "::" + c.Type)
		}
		if oc.Nullable != c.Nullable {
			if c.Nullable {
				alter("ALTER COLUMN " + col + " DROP NOT NULL")
			} else {
				alter("ALTER COLUMN " + col + " SET NOT NULL")
			}
		}
		if od, nd := defOf(oc), defOf(c); od != nd {
			if nd == "" {
				alter("ALTER COLUMN " + col + " DROP DEFAULT")
			} else {
				alter("ALTER COLUMN " + col + " SET DEFAULT " + nd)
			}
		}
	}

	if pkChanged && len(newPK) > 0 {
		alter("ADD PRIMARY KEY (" + quoteList(newPK) + ")")
	}
	oldIx := map[string]bool{}
	for _, ix := range old.Indexes {
		oldIx[ix.Name] = !dropped[ix.Name]
	}
	cur := &db.TableDef{Schema: old.Schema, Name: old.Name, Columns: new.Columns} // still under the old name
	for _, ix := range new.Indexes {
		if !ix.Primary && !(ix.Name != "" && oldIx[ix.Name]) {
			out = append(out, stmt(indexSQL(cur, ix)))
		}
	}
	oldFK := map[string]db.ForeignKey{}
	for _, fk := range old.ForeignKeys {
		oldFK[fk.Name] = fk
	}
	for _, fk := range new.ForeignKeys {
		if of, ok := oldFK[fk.Name]; !ok || fk.Name == "" || !fkEqual(of, fk) {
			alter("ADD " + fkSQL(fk))
		}
	}
	if new.Name != old.Name {
		alter("RENAME TO " + ident(new.Name))
	}
	return out, nil
}
