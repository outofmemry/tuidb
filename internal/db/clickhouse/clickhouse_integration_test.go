package clickhouse

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"tuidb/internal/config"
	"tuidb/internal/db"
)

func open(t *testing.T) db.Session {
	t.Helper()
	if os.Getenv("TUIDB_INTEGRATION") != "1" {
		t.Skip("set TUIDB_INTEGRATION=1")
	}
	url := os.Getenv("TUIDB_TEST_URL_CLICKHOUSE")
	if url == "" {
		url = "clickhouse://tuidb:tuidb@127.0.0.1:59000/tuidb"
	}
	c, err := config.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	s, err := driver{}.Open(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func tmpName() string { return fmt.Sprintf("tuidb_test_%d", rand.Intn(1e9)) }

// tmpTable creates a MergeTree table and drops it at the end.
func tmpTable(t *testing.T, s db.Session, cols string) string {
	t.Helper()
	n := tmpName()
	ctx := context.Background()
	if _, err := s.Exec(ctx, "CREATE TABLE "+n+" ("+cols+") ENGINE = MergeTree ORDER BY id", 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Exec(ctx, "DROP TABLE IF EXISTS "+n, 0) })
	return n
}

func TestSeedMetadata(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if !strings.HasPrefix(s.ServerInfo(), "ClickHouse 24.") {
		t.Errorf("info %q", s.ServerInfo())
	}
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if s.CurrentDatabase() != "tuidb" {
		t.Errorf("db %q", s.CurrentDatabase())
	}
	dbs, err := s.Databases(ctx)
	if err != nil || !contains(dbs, "tuidb") {
		t.Errorf("%v %v", dbs, err)
	}
	objs, err := s.Objects(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]db.Kind{}
	for _, o := range objs {
		kinds[o.Name] = o.Kind
	}
	if kinds["customers"] != db.KindTable || kinds["events"] != db.KindTable || kinds["paid_orders"] != db.KindView {
		t.Errorf("%v", kinds)
	}
	cols, err := s.Columns(ctx, db.ObjRef{Name: "customers"})
	if err != nil || len(cols) != 6 {
		t.Fatalf("%+v %v", cols, err)
	}
	if !cols[0].PK || cols[0].Type != "UInt32" || !cols[2].Nullable || cols[2].Type != "Nullable(String)" || cols[4].Default != "0" || !cols[4].HasDef {
		t.Errorf("%+v", cols)
	}
	ddl, err := s.DDL(ctx, db.ObjRef{Name: "customers"})
	if err != nil || !strings.Contains(ddl, "CREATE TABLE tuidb.customers") {
		t.Errorf("%q %v", ddl, err)
	}
	if ddl, err = s.DDL(ctx, db.ObjRef{Name: "paid_orders", Kind: db.KindView}); err != nil || !strings.Contains(ddl, "VIEW") {
		t.Errorf("%q %v", ddl, err)
	}
	if ix, _ := s.Indexes(ctx, db.ObjRef{Name: "customers"}); ix != nil {
		t.Error("indexes")
	}
	for kind, want := range map[db.ScriptKind]string{
		db.ScriptDrop: "DROP TABLE `customers`;", db.ScriptTruncate: "TRUNCATE TABLE `customers`;",
		db.ScriptSelectTop: "SELECT * FROM `customers` LIMIT 100;"} {
		if got, err := s.Script(ctx, kind, db.ObjRef{Name: "customers", Kind: db.KindTable}); got != want || err != nil {
			t.Errorf("%s: %q %v", kind, got, err)
		}
	}
	if got, _ := s.Script(ctx, db.ScriptDrop, db.ObjRef{Name: "paid_orders", Kind: db.KindView}); got != "DROP VIEW `paid_orders`;" {
		t.Errorf("%q", got)
	}
	if got, err := s.Script(ctx, db.ScriptCreate, db.ObjRef{Name: "orders"}); err != nil || !strings.Contains(got, "orders") {
		t.Errorf("%q %v", got, err)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestRowsAndCount(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	ref := db.ObjRef{Name: "customers"}
	res, err := s.Rows(ctx, db.RowQuery{ObjRef: ref, Sort: []db.Sort{{Column: "id"}}})
	if err != nil || len(res.Rows) != 3 {
		t.Fatalf("%+v %v", res, err)
	}
	r := res.Rows[0]
	if r[0] != uint64(1) || r[1] != "Ann Lee" || r[3] != "vip" || r[4] != "120.50" {
		t.Errorf("%#v", r)
	}
	if _, ok := r[5].(time.Time); !ok {
		t.Errorf("created_at %#v", r[5])
	}
	if res.Rows[2][2] != nil || res.Columns[2].Type != "Nullable(String)" {
		t.Errorf("null / type: %#v %v", res.Rows[2][2], res.Columns[2])
	}

	f := func(col, op, val string) db.FilterSet {
		return db.FilterSet{Filters: []db.Filter{{Column: col, Op: op, Value: val, Enabled: true}}}
	}
	res, err = s.Rows(ctx, db.RowQuery{ObjRef: ref, Filters: f("name", "Contains", "100%_"), Sort: []db.Sort{{Column: "id", Desc: true}}})
	if err != nil || len(res.Rows) != 1 || res.Rows[0][1] != "Cy 100%_done" {
		t.Errorf("contains: %+v %v", res, err)
	}
	res, err = s.Rows(ctx, db.RowQuery{ObjRef: ref, Filters: f(db.AnyColumn, "Contains", "o'neil")})
	if err != nil || len(res.Rows) != 0 { // LIKE is case sensitive in ClickHouse
		t.Errorf("any: %+v %v", res, err)
	}
	res, err = s.Rows(ctx, db.RowQuery{ObjRef: ref, Filters: f(db.AnyColumn, "Contains", "O'Neil")})
	if err != nil || len(res.Rows) != 1 {
		t.Errorf("any: %+v %v", res, err)
	}
	// string args against an integer column
	for _, fs := range []db.FilterSet{f("id", "=", "2"), f("id", "IN", "2, 3"), f("id", "BETWEEN", "2 AND 3"), f("id", ">", "1")} {
		if res, err = s.Rows(ctx, db.RowQuery{ObjRef: ref, Filters: fs}); err != nil || len(res.Rows) == 0 {
			t.Errorf("%+v: %+v %v", fs, res, err)
		}
	}
	res, err = s.Rows(ctx, db.RowQuery{ObjRef: ref, Sort: []db.Sort{{Column: "id"}}, Columns: []string{"id", "name"}, Limit: 1, Offset: 1})
	if err != nil || len(res.Rows) != 1 || res.Rows[0][0] != uint64(2) || len(res.Columns) != 2 {
		t.Errorf("paging: %+v %v", res, err)
	}
	if res, err = s.Rows(ctx, db.RowQuery{ObjRef: db.ObjRef{Name: "paid_orders"}}); err != nil || len(res.Rows) != 2 {
		t.Errorf("view: %+v %v", res, err)
	}

	cnt, err := s.Count(ctx, db.RowQuery{ObjRef: ref}, true)
	if err != nil || cnt.N != 3 || cnt.Exact {
		t.Errorf("estimate: %+v %v", cnt, err)
	}
	cnt, err = s.Count(ctx, db.RowQuery{ObjRef: ref, Filters: f("name", "Has prefix", "Bob")}, true)
	if err != nil || cnt.N != 1 || !cnt.Exact {
		t.Errorf("filtered: %+v %v", cnt, err)
	}
	cnt, err = s.Count(ctx, db.RowQuery{ObjRef: db.ObjRef{Name: "events"}}, false)
	if err != nil || cnt.N != 50 || !cnt.Exact {
		t.Errorf("exact: %+v %v", cnt, err)
	}
	cnt, err = s.Count(ctx, db.RowQuery{ObjRef: db.ObjRef{Name: "paid_orders"}}, true) // views have no estimate
	if err != nil || cnt.N != 2 || !cnt.Exact {
		t.Errorf("view: %+v %v", cnt, err)
	}
}

func TestExec(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	res, err := s.Exec(ctx, "SELECT number, toString(number) AS s, [number] AS a, (number, 'x') AS tp, toDecimal64(number, 3) AS d, "+
		"toUUID('61f0c404-5cb3-11e7-907b-a6006ad3dba0') AS u, toIPv4('1.2.3.4') AS ip, NULL AS nl, toInt8(-1) AS i FROM numbers(10)", 4)
	if err != nil || !res.HasRows || !res.Truncated || len(res.Rows) != 4 {
		t.Fatalf("%+v %v", res, err)
	}
	r := res.Rows[2]
	want := []any{uint64(2), "2", "[2]", `[2,"x"]`, "2.000", "61f0c404-5cb3-11e7-907b-a6006ad3dba0", "1.2.3.4", nil, int64(-1)}
	for i := range want {
		if r[i] != want[i] {
			t.Errorf("col %d (%s): %#v, want %#v", i, res.Columns[i].Type, r[i], want[i])
		}
	}
	if res, err = s.Exec(ctx, "SELECT number FROM numbers(1000000000)", 5); err != nil || len(res.Rows) != 5 || !res.Truncated {
		t.Errorf("huge truncated: %v", err)
	}
	if res, err = s.Exec(ctx, "SELECT 1;", 0); err != nil || res.Truncated || len(res.Rows) != 1 {
		t.Errorf("trailing semicolon: %+v %v", res, err)
	}
	if res, err = s.Exec(ctx, "SHOW TABLES", 0); err != nil || !res.HasRows {
		t.Errorf("show: %+v %v", res, err)
	}
	if _, err = s.Exec(ctx, "SELECT nope FROM customers", 0); err == nil {
		t.Error("want error")
	}

	n := tmpTable(t, s, "id UInt32, v String")
	res, err = s.Exec(ctx, "INSERT INTO "+n+" VALUES (1,'a'),(2,'b'),(3,'c')", 0)
	if err != nil || res.HasRows || res.Affected != 3 || res.Message != "3 rows affected" {
		t.Errorf("insert: %+v %v", res, err)
	}
	// mutation must be visible straight away
	if res, err = s.Exec(ctx, "ALTER TABLE "+n+" DELETE WHERE id = 1", 0); err != nil || res.HasRows {
		t.Errorf("delete: %+v %v", res, err)
	}
	if res, _ = s.Exec(ctx, "SELECT count() FROM "+n, 0); res.Rows[0][0] != uint64(2) {
		t.Errorf("mutation not synchronous: %v", res.Rows)
	}
}

func TestCancel(t *testing.T) {
	s := open(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	start := time.Now()
	_, err := s.Exec(ctx, "SELECT sleep(3)", 0)
	if err == nil || time.Since(start) > 1500*time.Millisecond {
		t.Errorf("cancel: %v after %v", err, time.Since(start))
	}
	// the session must stay usable
	if _, err := s.Exec(context.Background(), "SELECT 1", 0); err != nil {
		t.Errorf("after cancel: %v", err)
	}
	// metadata works while a query runs
	go s.Exec(context.Background(), "SELECT sleep(2)", 0)
	time.Sleep(100 * time.Millisecond)
	st := time.Now()
	if _, err := s.Objects(context.Background(), ""); err != nil || time.Since(st) > time.Second {
		t.Errorf("objects while busy: %v %v", err, time.Since(st))
	}
}

func TestPlanApplyRows(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	n := tmpTable(t, s, "id UInt32, name String, note Nullable(String), d Date, ts DateTime DEFAULT now()")
	ref := db.ObjRef{Name: n, Kind: db.KindTable}
	apply := func(ch ...db.Change) {
		t.Helper()
		st, err := s.Plan(ch)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Apply(ctx, st); err != nil {
			t.Fatalf("%v: %v", st, err)
		}
	}
	apply(
		db.Change{Kind: db.Insert, ObjRef: ref, Set: []db.KeyVal{{Col: "id", Val: "7"}, {Col: "name", Val: "it's"}, {Col: "note", Val: nil}, {Col: "d", Val: "2024-01-02"}, {Col: "ts", Val: db.Default}}},
		db.Change{Kind: db.Insert, ObjRef: ref, Set: []db.KeyVal{{Col: "id", Val: "8"}, {Col: "name", Val: db.Empty}, {Col: "note", Val: "n"}, {Col: "d", Val: db.Expr("today()")}}},
	)
	res, _ := s.Exec(ctx, "SELECT id, name, note, toString(d) FROM "+n+" ORDER BY id", 0)
	if len(res.Rows) != 2 || res.Rows[0][1] != "it's" || res.Rows[0][2] != nil || res.Rows[0][3] != "2024-01-02" || res.Rows[1][1] != "" {
		t.Fatalf("insert: %#v", res.Rows)
	}
	apply(db.Change{Kind: db.Update, ObjRef: ref, Key: []db.KeyVal{{Col: "id", Val: "7"}}, Set: []db.KeyVal{{Col: "name", Val: "new"}, {Col: "note", Val: db.Null}, {Col: "d", Val: "2025-05-05"}}})
	res, _ = s.Exec(ctx, "SELECT name, toString(d) FROM "+n+" WHERE id = 7", 0)
	if res.Rows[0][0] != "new" || res.Rows[0][1] != "2025-05-05" {
		t.Errorf("update: %#v", res.Rows)
	}
	apply(db.Change{Kind: db.Delete, ObjRef: ref, Key: []db.KeyVal{{Col: "id", Val: "8"}, {Col: "note", Val: "n"}}})
	if res, _ = s.Exec(ctx, "SELECT count() FROM "+n, 0); res.Rows[0][0] != uint64(1) {
		t.Errorf("delete: %v", res.Rows)
	}
}

func TestPlanTableAndAdmin(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	n := tmpName()
	t.Cleanup(func() {
		for _, x := range []string{n, n + "_c", n + "_r"} {
			s.Exec(ctx, "DROP TABLE IF EXISTS "+x, 0)
		}
	})
	def := &db.TableDef{Name: n, PrimaryKey: []string{"id"}, Columns: []db.Column{
		{Name: "id", Type: "UInt32"}, {Name: "a", Type: "String", Default: "'x'", HasDef: true}, {Name: "b", Type: "Int32"}}}
	run := func(st []db.Statement, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Apply(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	run(s.PlanTable(nil, def))
	cols, _ := s.Columns(ctx, db.ObjRef{Name: n})
	if len(cols) != 3 || !cols[0].PK || cols[1].Default != "'x'" {
		t.Fatalf("%+v", cols)
	}
	if _, err := s.Exec(ctx, "INSERT INTO "+n+" (id, a, b) VALUES (1, 'q', 5)", 0); err != nil {
		t.Fatal(err)
	}

	old := &db.TableDef{Name: n, PrimaryKey: []string{"id"}, Columns: cols}
	nw := &db.TableDef{Name: n, PrimaryKey: []string{"id"}, Columns: []db.Column{
		cols[0],
		{Name: "n", Type: "Float64", Default: "1.5", HasDef: true}, // new, second
		{Name: "b", OrigName: "b", Type: "Int64", Nullable: true},  // moved up, changed
		{Name: "a2", OrigName: "a", Type: "String"},                // renamed, default removed
	}}
	run(s.PlanTable(old, nw))
	cols, _ = s.Columns(ctx, db.ObjRef{Name: n})
	var got []string
	for _, c := range cols {
		got = append(got, c.Name+":"+c.Type+":"+c.Default)
	}
	if w := "id:UInt32: n:Float64:1.5 b:Nullable(Int64): a2:String:"; strings.Join(got, " ") != w {
		t.Errorf("alter result %q, want %q", strings.Join(got, " "), w)
	}
	if res, _ := s.Exec(ctx, "SELECT n, b, a2 FROM "+n, 0); res.Rows[0][0] != 1.5 || res.Rows[0][1] != int64(5) || res.Rows[0][2] != "q" {
		t.Errorf("data after alter: %#v", res.Rows)
	}
	old = &db.TableDef{Name: n, PrimaryKey: []string{"id"}, Columns: cols}
	drop := *old
	drop.Columns = cols[:3]
	run(s.PlanTable(old, &drop))
	if cols, _ = s.Columns(ctx, db.ObjRef{Name: n}); len(cols) != 3 {
		t.Errorf("drop: %+v", cols)
	}
	pk := *old
	pk.PrimaryKey = []string{"id", "b"}
	if _, err := s.PlanTable(old, &pk); err == nil {
		t.Error("pk change must fail")
	}

	ref := db.ObjRef{Name: n, Kind: db.KindTable}
	run(s.PlanAdmin(db.OpClone, ref, map[string]string{"new_name": n + "_c", "with_data": "true"}))
	if res, _ := s.Exec(ctx, "SELECT count() FROM "+n+"_c", 0); res.Rows[0][0] != uint64(1) {
		t.Errorf("clone data: %v", res.Rows)
	}
	run(s.PlanAdmin(db.OpRenameObject, db.ObjRef{Name: n + "_c"}, map[string]string{"new_name": n + "_r"}))
	run(s.PlanAdmin(db.OpTruncate, db.ObjRef{Name: n + "_r"}, nil))
	if res, _ := s.Exec(ctx, "SELECT count() FROM "+n+"_r", 0); res.Rows[0][0] != uint64(0) {
		t.Errorf("truncate: %v", res.Rows)
	}
	run(s.PlanAdmin(db.OpDropObject, db.ObjRef{Name: n + "_r"}, nil))
	if _, err := s.Exec(ctx, "SELECT 1 FROM "+n+"_r", 0); err == nil {
		t.Error("dropped table still there")
	}
}

func TestDatabasesAdminAndUse(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	d := tmpName()
	t.Cleanup(func() {
		s.Exec(ctx, "DROP DATABASE IF EXISTS "+d, 0)
		s.Exec(ctx, "DROP DATABASE IF EXISTS "+d+"_r", 0)
	})
	run := func(st []db.Statement, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Apply(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	run(s.PlanAdmin(db.OpCreateDatabase, db.ObjRef{Name: d}, nil))
	run(s.PlanAdmin(db.OpRenameDatabase, db.ObjRef{Name: d}, map[string]string{"new_name": d + "_r"}))
	dbs, _ := s.Databases(ctx)
	if !contains(dbs, d+"_r") || contains(dbs, d) {
		t.Errorf("rename: %v", dbs)
	}
	if err := s.UseDatabase(ctx, d+"_r"); err != nil {
		t.Fatal(err)
	}
	if s.CurrentDatabase() != d+"_r" {
		t.Errorf("current %q", s.CurrentDatabase())
	}
	if _, err := s.Exec(ctx, "CREATE TABLE x (id UInt8) ENGINE = Memory", 0); err != nil {
		t.Fatal(err)
	}
	objs, err := s.Objects(ctx, "")
	if err != nil || len(objs) != 1 || objs[0].Name != "x" {
		t.Errorf("objects after use: %+v %v", objs, err)
	}
	if err := s.UseDatabase(ctx, "tuidb"); err != nil {
		t.Fatal(err)
	}
	if err := s.UseDatabase(ctx, "no_such_db_xyz"); err == nil {
		t.Error("use of missing db must fail")
	}
	if s.CurrentDatabase() != "tuidb" {
		t.Errorf("current after failed use: %q", s.CurrentDatabase())
	}
	run(s.PlanAdmin(db.OpDropDatabase, db.ObjRef{Name: d + "_r"}, nil))
}

func TestProcesses(t *testing.T) {
	s := open(t)
	pl := s.(db.ProcessLister)
	ctx := context.Background()
	qctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := s.Exec(qctx, "SELECT sleepEachRow(1) FROM numbers(5) SETTINGS max_block_size = 1", 0)
		done <- err
	}()
	var id string
	for i := 0; i < 20 && id == ""; i++ {
		time.Sleep(100 * time.Millisecond)
		ps, err := pl.Processes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			if strings.Contains(p.Query, "sleepEachRow") {
				id = p.ID
				if p.User != "tuidb" || p.DB != "tuidb" || p.State != "Running" {
					t.Errorf("%+v", p)
				}
			}
		}
	}
	if id == "" {
		t.Fatal("running query not listed")
	}
	if err := pl.Kill(ctx, id); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Error("killed query should error")
		}
	case <-time.After(5 * time.Second):
		t.Error("killed query still running")
	}
}

// USE switches the whole session (P1-60), and overlapping switches keep opts, conn and cur paired (P1-61).
func TestExecUseSwitchesSession(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	r, err := s.Exec(ctx, "USE system", 0)
	if err != nil || r.Message != "Database changed" || s.CurrentDatabase() != "system" {
		t.Fatalf("use: %+v %v current %s", r, err, s.CurrentDatabase())
	}
	for i := 0; i < 5; i++ { // every pooled connection now resolves unqualified names in system
		r, err := s.Exec(ctx, "SELECT currentDatabase()", 0)
		if err != nil || r.Rows[0][0] != "system" {
			t.Fatalf("currentDatabase %v %v", r, err)
		}
	}
	if _, err := s.Exec(ctx, "USE tuidb_test_missing_db", 0); err == nil || s.CurrentDatabase() != "system" {
		t.Errorf("bad USE: %v current %s", err, s.CurrentDatabase())
	}
	done := make(chan error, 2)
	for _, name := range []string{"tuidb", "system"} {
		go func() { done <- s.UseDatabase(ctx, name) }()
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	cs := s.(*session)
	cs.mu.RLock()
	opt, cur := cs.opts.Auth.Database, cs.cur
	cs.mu.RUnlock()
	if opt != cur {
		t.Errorf("opts database %q, current %q", opt, cur)
	}
	if r, err := s.Exec(ctx, "SELECT currentDatabase()", 0); err != nil || r.Rows[0][0] != cur {
		t.Errorf("conn database %v %v, current %q", r, err, cur)
	}
}
