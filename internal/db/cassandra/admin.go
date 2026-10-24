package cassandra

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"tuidb/internal/db"
)

func stmts(texts ...string) []db.Statement {
	out := make([]db.Statement, len(texts))
	for i, t := range texts {
		out[i] = db.Statement{Text: t}
	}
	return out
}

var createHeader = regexp.MustCompile(`(?is)^\s*CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:"(?:[^"]|"")+"|\w+)(?:\.(?:"(?:[^"]|"")+"|\w+))?\s*\(`)

func (s *session) PlanAdmin(op db.AdminOp, ref db.ObjRef, opts map[string]string) ([]db.Statement, error) {
	switch op {
	case db.OpDropObject:
		t, err := s.dropTarget(ref)
		if err != nil {
			return nil, err
		}
		return stmts("DROP " + t), nil
	case db.OpTruncate:
		if opts["cascade"] == "true" {
			return nil, errors.New("Cassandra has no TRUNCATE ... CASCADE")
		}
		if ref.Kind != db.KindTable {
			return nil, fmt.Errorf("only tables can be truncated, not a %s", ref.Kind)
		}
		qn, err := s.qual(ref)
		if err != nil {
			return nil, err
		}
		return stmts("TRUNCATE " + qn), nil
	case db.OpClone:
		// Cassandra has no native table copy. The structure is cloned from the server's DESCRIBE (table options included, the
		// indexes left out: their names are keyspace-wide). Copying rows would mean reading the whole table through the client.
		if ref.Kind != db.KindTable {
			return nil, errors.New("only tables can be cloned")
		}
		if opts["new_name"] == "" {
			return nil, errors.New("clone needs a new name")
		}
		if opts["with_data"] == "true" {
			return nil, errors.New("Cassandra cannot copy a table's rows in one statement: clone the structure, then copy the rows with cqlsh COPY or a script")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		ddl, err := s.DDL(ctx, ref)
		if err != nil {
			return nil, err
		}
		parts := db.Split("cql", ddl)
		if len(parts) == 0 {
			return nil, errors.New("cannot clone: no DDL for " + ref.Name)
		}
		loc := createHeader.FindStringIndex(parts[0].Text)
		if loc == nil {
			return nil, errors.New("cannot clone: unexpected DDL " + ddl)
		}
		ks, _ := s.ksOf(ref)
		return stmts("CREATE TABLE " + q(ks) + "." + q(opts["new_name"]) + " (" + parts[0].Text[loc[1]:]), nil
	case db.OpRenameObject:
		return nil, errors.New("Cassandra cannot rename tables, views, types or functions: create the new one and copy the data")
	case db.OpCreateDatabase:
		t, err := createKeyspace(ref.Name, opts)
		return stmts(t), err
	case db.OpDropDatabase:
		return stmts("DROP KEYSPACE " + q(ref.Name)), nil
	case db.OpRenameDatabase:
		return nil, errors.New("Cassandra cannot rename a keyspace: create a new one and copy the tables")
	}
	return nil, fmt.Errorf("unknown admin operation %q", op)
}

// createKeyspace builds CREATE KEYSPACE. opts: strategy (SimpleStrategy | NetworkTopologyStrategy, default Simple),
// replication (a factor for Simple, "dc1:3,dc2:2" for NetworkTopology, default 1), durable_writes.
func createKeyspace(name string, opts map[string]string) (string, error) {
	strategy, repl := opts["strategy"], strings.TrimSpace(opts["replication"])
	if strategy == "" {
		strategy = "SimpleStrategy"
	}
	var cfg string
	switch strings.ToLower(strategy) {
	case "simplestrategy":
		if repl == "" {
			repl = "1"
		}
		if n, err := strconv.Atoi(repl); err != nil || n < 1 {
			return "", fmt.Errorf("SimpleStrategy needs a replication factor (a number), got %q", repl)
		}
		cfg = "'class': 'SimpleStrategy', 'replication_factor': " + repl
	case "networktopologystrategy":
		cfg = "'class': 'NetworkTopologyStrategy'"
		if repl == "" {
			return "", errors.New("NetworkTopologyStrategy needs replication per datacenter, like dc1:3,dc2:2")
		}
		for _, p := range strings.Split(repl, ",") {
			dc, n, ok := strings.Cut(strings.TrimSpace(p), ":")
			if i, err := strconv.Atoi(n); !ok || dc == "" || err != nil || i < 0 {
				return "", fmt.Errorf("bad replication %q: want datacenter:factor, like dc1:3", p)
			}
			cfg += ", " + quoteLit(dc) + ": " + n
		}
	default:
		return "", fmt.Errorf("unknown replication strategy %q (want SimpleStrategy or NetworkTopologyStrategy)", strategy)
	}
	t := "CREATE KEYSPACE " + q(name) + " WITH replication = {" + cfg + "}"
	if v := strings.ToLower(opts["durable_writes"]); v == "true" || v == "false" {
		t += " AND durable_writes = " + v
	}
	return t, nil
}

// keyRole reads a column's place in the primary key from Extra ("partition key", "clustering asc|desc").
func keyRole(c db.Column) (partition, clustering, desc bool) {
	e := strings.ToLower(c.Extra)
	return strings.Contains(e, "partition"), strings.Contains(e, "clustering"), strings.Contains(e, "desc")
}

func isStatic(c db.Column) bool { return strings.Contains(strings.ToLower(c.Extra), "static") }

// createTable builds CREATE TABLE plus one CREATE INDEX per index. The key is t.PrimaryKey (or the columns flagged PK) in order;
// columns whose Extra says "partition key" form the partition key and "clustering asc|desc" the clustering columns. Unmarked
// key columns follow Cassandra's own reading of PRIMARY KEY (a, b, c): the first is the partition key, the rest cluster.
func createTable(t *db.TableDef) ([]db.Statement, error) {
	if t.Name == "" || len(t.Columns) == 0 {
		return nil, errors.New("a table needs a name and at least one column")
	}
	by := map[string]db.Column{}
	var pk []string
	for _, c := range t.Columns {
		if c.Name == "" || strings.TrimSpace(c.Type) == "" {
			return nil, fmt.Errorf("column %q needs a name and a type", c.Name)
		}
		by[c.Name] = c
		if c.PK && len(t.PrimaryKey) == 0 {
			pk = append(pk, c.Name)
		}
	}
	if len(t.PrimaryKey) > 0 {
		pk = t.PrimaryKey
	}
	if len(pk) == 0 {
		return nil, errors.New("a Cassandra table needs a primary key: mark the partition key column first, then any clustering columns")
	}
	explicit := false
	for _, n := range pk {
		c, ok := by[n]
		if !ok {
			return nil, fmt.Errorf("primary key column %q is not a column of the table", n)
		}
		p, _, _ := keyRole(c)
		explicit = explicit || p
	}
	var part, clust, order []string
	for i, n := range pk {
		p, cl, desc := keyRole(by[n])
		switch {
		case p || (!explicit && !cl && i == 0):
			part = append(part, q(n))
		default:
			clust = append(clust, q(n))
			dir := "ASC"
			if desc {
				dir = "DESC"
			}
			order = append(order, q(n)+" "+dir)
		}
	}
	if len(part) == 0 {
		return nil, errors.New("a Cassandra table needs a partition key: the first primary key column cannot be a clustering column")
	}
	var b strings.Builder
	b.WriteString("CREATE TABLE " + qualifiedName(t.Schema, t.Name) + " (\n")
	for _, c := range t.Columns {
		b.WriteString("    " + q(c.Name) + " " + c.Type)
		if isStatic(c) {
			b.WriteString(" STATIC")
		}
		b.WriteString(",\n")
	}
	key := part[0]
	if len(part) > 1 {
		key = "(" + strings.Join(part, ", ") + ")"
	}
	if len(clust) > 0 {
		key += ", " + strings.Join(clust, ", ")
	}
	b.WriteString("    PRIMARY KEY (" + key + ")\n)")
	if strings.Contains(strings.Join(order, ","), "DESC") {
		b.WriteString(" WITH CLUSTERING ORDER BY (" + strings.Join(order, ", ") + ")")
	}
	out := stmts(b.String())
	for _, ix := range t.Indexes {
		st, err := createIndex(t.Schema, t.Name, ix)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

func qualifiedName(ks, name string) string {
	if ks == "" {
		return q(name)
	}
	return q(ks) + "." + q(name)
}

// createIndex: a secondary index, or a storage-attached one when Algorithm is SAI. A column written as "KEYS(m)" or
// "VALUES(m)" is passed through for collection indexes.
func createIndex(ks, table string, ix db.Index) (db.Statement, error) {
	if len(ix.Columns) != 1 {
		return db.Statement{}, fmt.Errorf("index %q: Cassandra indexes exactly one column", ix.Name)
	}
	target := ix.Columns[0]
	if !strings.Contains(target, "(") {
		target = q(target)
	}
	t := "CREATE INDEX "
	if strings.EqualFold(ix.Algorithm, "SAI") {
		t = "CREATE CUSTOM INDEX "
	}
	if ix.Name != "" {
		t += q(ix.Name) + " "
	}
	t += "ON " + qualifiedName(ks, table) + " (" + target + ")"
	switch strings.ToUpper(ix.Algorithm) {
	case "SAI":
		t += " USING 'StorageAttachedIndex'"
	case "SASI":
		return db.Statement{}, errors.New("SASI indexes are not offered: use SAI")
	}
	return db.Statement{Text: t}, nil
}

func sameType(a, b string) bool {
	norm := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), "")) }
	return norm(a) == norm(b)
}

// PlanTable creates a table, or diffs old -> new into ALTER TABLE statements. Cassandra can add and drop columns and rename
// primary key columns; it cannot change a column's type, its key role, nullability or static-ness, so those are errors, and so is
// the primary key itself. Indexes are created and dropped by name.
func (s *session) PlanTable(old, new *db.TableDef) ([]db.Statement, error) {
	if new == nil {
		return nil, errors.New("nothing to plan")
	}
	ks := new.Schema
	if ks == "" {
		ks, _ = s.ksOf(db.ObjRef{})
	}
	if old == nil {
		n := *new
		n.Schema = ks
		return createTable(&n)
	}
	tbl := qualifiedName(ks, old.Name)
	if new.Name != old.Name {
		return nil, errors.New("Cassandra cannot rename a table: create a new one and copy the data")
	}
	if len(new.ForeignKeys) > 0 {
		return nil, errors.New("Cassandra has no foreign keys")
	}
	oldBy := map[string]db.Column{}
	for _, c := range old.Columns {
		oldBy[c.Name] = c
	}
	kept := map[string]bool{}
	var out []db.Statement
	rename := map[string]string{} // old name -> new name, for the key comparison
	for _, c := range new.Columns {
		if c.OrigName == "" || oldBy[c.OrigName].Name == "" {
			if c.PK {
				return nil, fmt.Errorf("cannot add key column %s: the primary key of an existing table cannot change", c.Name)
			}
			if c.Default != "" || c.HasDef {
				return nil, fmt.Errorf("column %s: Cassandra has no column defaults", c.Name)
			}
			if strings.TrimSpace(c.Type) == "" {
				return nil, fmt.Errorf("column %q needs a type", c.Name)
			}
			t := "ALTER TABLE " + tbl + " ADD " + q(c.Name) + " " + c.Type
			if isStatic(c) {
				t += " STATIC"
			}
			out = append(out, db.Statement{Text: t})
			continue
		}
		o := oldBy[c.OrigName]
		kept[o.Name] = true
		switch {
		case !sameType(o.Type, c.Type):
			return nil, fmt.Errorf("column %s: Cassandra cannot change a column's type (%s to %s): add a new column, copy the data, drop the old one", o.Name, o.Type, c.Type)
		case o.PK != c.PK || o.Nullable != c.Nullable || isStatic(o) != isStatic(c):
			return nil, fmt.Errorf("column %s: Cassandra cannot change a column's key membership, NOT NULL or static setting", o.Name)
		case c.Default != "" || c.HasDef:
			return nil, fmt.Errorf("column %s: Cassandra has no column defaults", c.Name)
		}
		if c.Name != o.Name {
			if !o.PK {
				return nil, fmt.Errorf("column %s: Cassandra can only rename primary key columns", o.Name)
			}
			rename[o.Name] = c.Name
			out = append(out, db.Statement{Text: "ALTER TABLE " + tbl + " RENAME " + q(o.Name) + " TO " + q(c.Name)})
		}
	}
	for _, c := range old.Columns {
		if !kept[c.Name] {
			if c.PK {
				return nil, fmt.Errorf("cannot drop key column %s", c.Name)
			}
			out = append(out, db.Statement{Text: "ALTER TABLE " + tbl + " DROP " + q(c.Name)})
		}
	}
	if len(new.PrimaryKey) > 0 && len(old.PrimaryKey) > 0 {
		if len(new.PrimaryKey) != len(old.PrimaryKey) {
			return nil, errors.New("Cassandra cannot change the primary key: create a new table and copy the data")
		}
		for i, n := range old.PrimaryKey {
			if r, ok := rename[n]; ok {
				n = r
			}
			if new.PrimaryKey[i] != n {
				return nil, errors.New("Cassandra cannot change the primary key: create a new table and copy the data")
			}
		}
	}
	// Indexes go first and come last: Cassandra refuses to drop a column that is still indexed.
	var dropIx, createIx []db.Statement
	oldIx := map[string]db.Index{}
	for _, ix := range old.Indexes {
		oldIx[ix.Name] = ix
	}
	newIx := map[string]bool{}
	for _, ix := range new.Indexes {
		newIx[ix.Name] = true
		o, had := oldIx[ix.Name]
		if had && strings.Join(o.Columns, ",") == strings.Join(ix.Columns, ",") && strings.EqualFold(o.Algorithm, ix.Algorithm) {
			continue
		}
		if had { // changed: drop, then create again
			dropIx = append(dropIx, db.Statement{Text: "DROP INDEX " + qualifiedName(ks, ix.Name)})
		}
		st, err := createIndex(ks, old.Name, ix)
		if err != nil {
			return nil, err
		}
		createIx = append(createIx, st)
	}
	for _, ix := range old.Indexes {
		if !newIx[ix.Name] {
			dropIx = append(dropIx, db.Statement{Text: "DROP INDEX " + qualifiedName(ks, ix.Name)})
		}
	}
	return append(append(dropIx, out...), createIx...), nil
}
