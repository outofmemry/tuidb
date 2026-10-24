package cassandra

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"tuidb/internal/db"
)

func admin(t *testing.T, s *session, op db.AdminOp, ref db.ObjRef, opts map[string]string) {
	t.Helper()
	st, err := s.PlanAdmin(op, ref, opts)
	if err != nil {
		t.Fatalf("%s %v: %v", op, ref, err)
	}
	if err := s.Apply(context.Background(), st); err != nil {
		t.Fatalf("%s %v: %v (%v)", op, ref, err, st)
	}
}

func TestIntegrationKeyspaceAdmin(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	ks := fmt.Sprintf("tuidb_ks_%x", rand.Uint32())
	t.Cleanup(func() { s.query("DROP KEYSPACE IF EXISTS " + q(ks)).Exec() })
	admin(t, s, db.OpCreateDatabase, db.ObjRef{Name: ks}, map[string]string{"strategy": "SimpleStrategy", "replication": "1", "durable_writes": "false"})
	dbs, _ := s.Databases(ctx)
	if !contains(dbs, ks) {
		t.Fatalf("created keyspace missing from %v", dbs)
	}
	r := exec(t, s, "SELECT durable_writes, replication FROM system_schema.keyspaces WHERE keyspace_name = '"+ks+"'")
	if r.Rows[0][0] != false || !strings.Contains(db.FormatValue(r.Rows[0][1]), "SimpleStrategy") {
		t.Errorf("keyspace options %v", r.Rows)
	}
	// open it, create a table in it and look at it
	if err := s.UseDatabase(ctx, ks); err != nil {
		t.Fatal(err)
	}
	exec(t, s, "CREATE TABLE t (a int PRIMARY KEY)")
	if objs, _ := s.Objects(ctx, ""); len(objs) != 1 || objs[0].Name != "t" {
		t.Errorf("objects %v", objs)
	}
	if err := s.UseDatabase(ctx, "tuidb"); err != nil {
		t.Fatal(err)
	}
	admin(t, s, db.OpDropDatabase, db.ObjRef{Name: ks}, nil)
	if dbs, _ := s.Databases(ctx); contains(dbs, ks) {
		t.Errorf("dropped keyspace still listed")
	}
	// NetworkTopologyStrategy against a datacenter that does not exist is accepted by the statement, rejected by the server
	st, err := s.PlanAdmin(db.OpCreateDatabase, db.ObjRef{Name: ks + "n"}, map[string]string{"strategy": "NetworkTopologyStrategy", "replication": "datacenter1:1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.query("DROP KEYSPACE IF EXISTS " + q(ks+"n")).Exec() })
	if err := s.Apply(ctx, st); err != nil {
		t.Errorf("NetworkTopologyStrategy: %v", err)
	}
	for _, op := range []db.AdminOp{db.OpRenameDatabase, db.OpRenameObject} {
		if _, err := s.PlanAdmin(op, ref("x"), map[string]string{"new_name": "y"}); err == nil {
			t.Errorf("%s must be unsupported", op)
		}
	}
}

func TestIntegrationTableAdmin(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	src := tempTable(t, s)
	dst, dst2 := tempTable(t, s), tempTable(t, s)
	exec(t, s, "CREATE TABLE "+q(src)+" (a int, b text, c int, s text STATIC, PRIMARY KEY ((a), b)) WITH CLUSTERING ORDER BY (b DESC) AND gc_grace_seconds = 12345 AND comment = 'x;y'")
	exec(t, s, "CREATE INDEX ON "+q(src)+" (c)")
	exec(t, s, "INSERT INTO "+q(src)+" (a, b, c) VALUES (1, 'x', 1)")
	// clone: structure and table options, not the indexes (their names are keyspace-wide) and not the rows
	admin(t, s, db.OpClone, ref(src), map[string]string{"new_name": dst})
	ddl, err := s.DDL(ctx, ref(dst))
	if err != nil || !strings.Contains(ddl, "gc_grace_seconds = 12345") || !strings.Contains(ddl, "CLUSTERING ORDER BY (b DESC)") || !strings.Contains(ddl, "comment = 'x;y'") || !strings.Contains(ddl, "s text static") {
		t.Errorf("clone ddl %q %v", ddl, err)
	}
	if idx, _ := s.Indexes(ctx, ref(dst)); len(idx) != 0 {
		t.Errorf("clone must not copy indexes: %v", idx)
	}
	if r := exec(t, s, "SELECT count(*) FROM "+q(dst)); r.Rows[0][0] != int64(0) {
		t.Error("clone without data has rows")
	}
	if _, err := s.PlanAdmin(db.OpClone, ref(src), map[string]string{"new_name": dst2, "with_data": "true"}); err == nil || !strings.Contains(err.Error(), "copy") {
		t.Errorf("clone with data must say why not: %v", err)
	}
	if _, err := s.PlanAdmin(db.OpClone, ref("nope"), map[string]string{"new_name": dst2}); err == nil {
		t.Error("clone of a missing table")
	}
	// truncate empties, drop removes
	admin(t, s, db.OpTruncate, ref(src), nil)
	if r := exec(t, s, "SELECT count(*) FROM "+q(src)); r.Rows[0][0] != int64(0) {
		t.Errorf("truncate left %v", r.Rows)
	}
	admin(t, s, db.OpDropObject, ref(dst), nil)
	if objs, _ := s.Objects(ctx, ""); func() bool {
		for _, o := range objs {
			if o.Name == dst {
				return true
			}
		}
		return false
	}() {
		t.Error("dropped table still listed")
	}
}

func TestIntegrationDropObjects(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	base := tempTable(t, s)
	exec(t, s, "CREATE TYPE "+q(base+"_ty")+" (x int)")
	exec(t, s, "CREATE TABLE "+q(base)+" (id int, v int, PRIMARY KEY (id))")
	exec(t, s, "CREATE MATERIALIZED VIEW "+q(base+"_mv")+" AS SELECT * FROM "+q(base)+" WHERE v IS NOT NULL AND id IS NOT NULL PRIMARY KEY (v, id)")
	exec(t, s, "CREATE FUNCTION "+q(base+"_f")+" (a int) CALLED ON NULL INPUT RETURNS int LANGUAGE java AS 'return a;'")
	exec(t, s, "CREATE FUNCTION "+q(base+"_f")+" (a int, b int) CALLED ON NULL INPUT RETURNS int LANGUAGE java AS 'return a;'") // overloaded
	exec(t, s, "CREATE FUNCTION "+q(base+"_st")+" (s int, a int) CALLED ON NULL INPUT RETURNS int LANGUAGE java AS 'return s;'")
	exec(t, s, "CREATE AGGREGATE "+q(base+"_agg")+" (int) SFUNC "+q(base+"_st")+" STYPE int INITCOND 0")
	t.Cleanup(func() {
		for _, d := range []string{"AGGREGATE " + q(base+"_agg"), "FUNCTION " + q(base+"_f"), "FUNCTION " + q(base+"_st"), "TYPE " + q(base+"_ty")} {
			s.query("DROP " + d).Exec()
		}
	})
	objs, err := s.Objects(ctx, "") // loads the aggregate set the drop statements rely on
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]db.Object{}
	for _, o := range objs {
		have[o.Name] = o
	}
	for _, n := range []string{base, base + "_mv", base + "_ty", base + "_f(int)", base + "_f(int, int)", base + "_agg(int)"} {
		if _, ok := have[n]; !ok {
			t.Fatalf("%s not listed in %v", n, have)
		}
	}
	// drop in dependency order, each through PlanAdmin with the reference the sidebar holds
	for _, n := range []string{base + "_mv", base + "_agg(int)", base + "_f(int, int)", base + "_f(int)", base + "_st(int, int)", base + "_ty", base} {
		o := have[n]
		if o.Name == "" {
			o = db.Object{ObjRef: db.ObjRef{Name: n, Kind: db.KindFunction}}
		}
		admin(t, s, db.OpDropObject, o.ObjRef, nil)
	}
	objs, _ = s.Objects(ctx, "")
	for _, o := range objs {
		if strings.HasPrefix(o.Name, base) {
			t.Errorf("%s survived its drop", o.Name)
		}
	}
}

func TestIntegrationPlanTable(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	name := tempTable(t, s)
	apply := func(old, new *db.TableDef) {
		t.Helper()
		st, err := s.PlanTable(old, new)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Apply(ctx, st); err != nil {
			t.Fatalf("%v: %v", texts(st), err)
		}
	}
	def := &db.TableDef{Name: name, PrimaryKey: []string{"a", "b", "c"}, Columns: []db.Column{
		mkcol("a", "int", "partition key"), mkcol("b", "text", "partition key"), mkcol("c", "int", "clustering desc"),
		mkcol("s", "text", "static"), mkcol("m", "map<text, int>", ""), mkcol("x", "text", "")},
		Indexes: []db.Index{{Name: name + "_x", Columns: []string{"x"}}, {Name: name + "_sai", Columns: []string{"x"}, Algorithm: "SAI"}, {Name: name + "_keys", Columns: []string{"KEYS(m)"}}}}
	apply(nil, def)
	cols, err := s.Columns(ctx, ref(name))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range cols {
		got = append(got, c.Name+":"+c.Type+":"+c.Extra)
	}
	eq(t, "created columns", got, []string{"a:int:partition key", "b:text:partition key", "c:int:clustering desc", "s:text:static", "m:map<text, int>:", "x:text:"})
	idx, _ := s.Indexes(ctx, ref(name))
	algos := map[string]string{}
	for _, ix := range idx {
		algos[ix.Name] = ix.Algorithm + "/" + strings.Join(ix.Columns, ",")
	}
	if algos[name+"_x"] != "/x" || algos[name+"_sai"] != "SAI/x" || !strings.HasPrefix(algos[name+"_keys"], "/keys(m)") {
		t.Errorf("indexes %v", algos)
	}

	// the structure editor's round trip: what Columns/Indexes return, edited, diffs into ALTERs
	old := &db.TableDef{Name: name, PrimaryKey: []string{"a", "b", "c"}, Columns: cols, Indexes: idx}
	n := &db.TableDef{Name: name, PrimaryKey: []string{"a2", "b", "c"}}
	for _, c := range cols {
		switch c.Name {
		case "x": // dropped, together with the two indexes on it (Cassandra refuses to drop an indexed column)
		case "a":
			c.Name = "a2" // renamed
			n.Columns = append(n.Columns, c)
		default:
			n.Columns = append(n.Columns, c)
		}
	}
	n.Columns = append(n.Columns, db.Column{Name: "added", Type: "list<int>", Nullable: true})
	for _, ix := range idx {
		if ix.Columns[0] != "x" {
			n.Indexes = append(n.Indexes, ix) // the KEYS(m) index stays
		}
	}
	apply(old, n)
	cols, err = s.Columns(ctx, ref(name))
	if err != nil {
		t.Fatal(err)
	}
	got = nil
	for _, c := range cols {
		got = append(got, c.Name)
	}
	eq(t, "altered columns", got, []string{"a2", "b", "c", "s", "added", "m"})
	if idx, _ = s.Indexes(ctx, ref(name)); len(idx) != 1 || idx[0].Name != name+"_keys" {
		t.Errorf("indexes after alter %+v", idx)
	}
	// reloaded as the UI does after a save, the definition has nothing left to change
	now := &db.TableDef{Name: name, PrimaryKey: n.PrimaryKey, Columns: cols, Indexes: idx}
	if st, err := s.PlanTable(now, now); err != nil || len(st) != 0 {
		t.Errorf("idempotent diff: %v %v", st, err)
	}
	// and what Cassandra cannot do is refused before it is sent
	bad := *now
	bad.Columns = append([]db.Column(nil), cols...)
	bad.Columns[4].Type = "text" // "added": list<int> -> text
	if _, err := s.PlanTable(now, &bad); err == nil {
		t.Error("retype must fail")
	}
}
