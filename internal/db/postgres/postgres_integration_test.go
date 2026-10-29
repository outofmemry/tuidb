package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func openTest(t *testing.T, bootstrap ...string) db.Session {
	t.Helper()
	if os.Getenv("WEBTUIDB_INTEGRATION") != "1" {
		t.Skip("set WEBTUIDB_INTEGRATION=1")
	}
	url := os.Getenv("WEBTUIDB_TEST_URL_POSTGRES")
	if url == "" {
		url = "postgresql://webtuidb:webtuidb@127.0.0.1:55432/webtuidb"
	}
	c, err := config.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	c.Bootstrap = bootstrap
	d, err := db.Lookup("postgres")
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.Open(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func tempName() string { return fmt.Sprintf("webtuidb_test_%06d", rand.Intn(1000000)) }

// must panics on error, which fails the running test (cleanups still run).
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func has[T comparable](xs []T, x T) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestIntegrationMetadata(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.ServerInfo(), "PostgreSQL 1") {
		t.Errorf("server info %q", s.ServerInfo())
	}
	if dbs := must(s.Databases(ctx)); !has(dbs, "webtuidb") {
		t.Errorf("databases %v", dbs)
	}
	if s.CurrentDatabase() != "webtuidb" || s.CurrentSchema() != "public" {
		t.Errorf("current %s.%s", s.CurrentDatabase(), s.CurrentSchema())
	}
	schemas := must(s.Schemas(ctx))
	if !has(schemas, "public") || !has(schemas, "app") || has(schemas, "pg_catalog") || has(schemas, "information_schema") {
		t.Errorf("schemas %v", schemas)
	}

	objs := must(s.Objects(ctx, "public"))
	byName := map[string]db.Object{}
	for _, o := range objs {
		byName[o.Name] = o
	}
	for name, kind := range map[string]db.Kind{"customers": db.KindTable, "orders": db.KindTable, "no_pk": db.KindTable, "audit_log": db.KindTable,
		"paid_orders": db.KindView, "order_totals": db.KindMatView, "ticket_seq": db.KindSequence,
		"customer_order_count": db.KindFunction, "touch_audit": db.KindProcedure} {
		if o, ok := byName[name]; !ok || o.Kind != kind || o.Schema != "public" {
			t.Errorf("object %s: %+v", name, o)
		}
	}
	if d := byName["customer_order_count"].Detail; !strings.Contains(d, "p_customer_id integer") {
		t.Errorf("function detail %q", d)
	}
	if app := must(s.Objects(ctx, "app")); len(app) != 1 || app[0].Name != "settings" {
		t.Errorf("app objects %+v", app)
	}
	if sys := must(s.Objects(ctx, "pg_catalog")); len(sys) < 50 {
		t.Errorf("pg_catalog objects by name: %d", len(sys))
	}

	cols := must(s.Columns(ctx, db.ObjRef{Name: "customers", Kind: db.KindTable}))
	if len(cols) != 7 || cols[0].Name != "id" || !cols[0].PK || cols[0].Nullable || cols[0].Extra != "serial" || !strings.HasPrefix(cols[0].Default, "nextval(") {
		t.Errorf("customers id: %+v", cols[0])
	}
	if c := cols[4]; c.Name != "balance" || c.Type != "numeric(10,2)" || c.Default != "0" || !c.Nullable {
		t.Errorf("balance %+v", c)
	}
	oc := must(s.Columns(ctx, db.ObjRef{Name: "orders", Kind: db.KindTable}))
	if oc[1].FK == nil || oc[1].FK.RefTable != "customers" {
		t.Errorf("orders.customer_id fk: %+v", oc[1])
	}
	if vc := must(s.Columns(ctx, db.ObjRef{Name: "order_totals", Kind: db.KindMatView})); len(vc) != 2 {
		t.Errorf("matview columns %+v", vc)
	}

	idx := must(s.Indexes(ctx, db.ObjRef{Name: "orders", Kind: db.KindTable}))
	m := map[string]db.Index{}
	for _, ix := range idx {
		m[ix.Name] = ix
	}
	if ix := m["orders_status_idx"]; ix.Algorithm != "btree" || len(ix.Columns) != 1 || ix.Columns[0] != "status" || ix.Unique {
		t.Errorf("orders_status_idx %+v", ix)
	}
	if ix := m["orders_pkey"]; !ix.Primary || !ix.Unique {
		t.Errorf("orders_pkey %+v", ix)
	}
	if ix := must(s.Indexes(ctx, db.ObjRef{Name: "customers"})); len(ix) != 2 {
		t.Errorf("customers indexes %+v", ix)
	}

	tr := must(s.Triggers(ctx, db.ObjRef{Name: "orders", Kind: db.KindTable}))
	if len(tr) != 1 || tr[0].Name != "orders_audit" || tr[0].Timing != "AFTER" || tr[0].Event != "INSERT" || !strings.Contains(tr[0].Statement, "log_order()") {
		t.Errorf("triggers %+v", tr)
	}
	fks := must(s.ForeignKeys(ctx, db.ObjRef{Name: "orders", Kind: db.KindTable}))
	if len(fks) != 1 || fks[0].RefTable != "customers" || fks[0].RefSchema != "public" || fks[0].Columns[0] != "customer_id" || fks[0].RefColumns[0] != "id" ||
		fks[0].OnDelete != "CASCADE" || fks[0].OnUpdate != "CASCADE" {
		t.Errorf("fks %+v", fks)
	}

	ddl := func(name string, k db.Kind, want ...string) {
		t.Helper()
		out := must(s.DDL(ctx, db.ObjRef{Name: name, Kind: k}))
		for _, w := range want {
			if !strings.Contains(out, w) {
				t.Errorf("DDL %s missing %q:\n%s", name, w, out)
			}
		}
	}
	ddl("orders", db.KindTable, `CREATE TABLE "public"."orders"`, `"id" integer DEFAULT nextval('orders_id_seq'::regclass) NOT NULL`, `"total" numeric(10,2) NOT NULL`, "PRIMARY KEY (id)",
		"FOREIGN KEY (customer_id) REFERENCES customers(id) ON UPDATE CASCADE ON DELETE CASCADE", "CREATE INDEX orders_status_idx ON public.orders USING btree (status);")
	ddl("customers", db.KindTable, "UNIQUE (email)")
	ddl("paid_orders", db.KindView, `CREATE OR REPLACE VIEW "public"."paid_orders" AS`, "status = 'paid'")
	ddl("order_totals", db.KindMatView, "CREATE MATERIALIZED VIEW", "sum(total)")
	ddl("customer_order_count", db.KindFunction, "CREATE OR REPLACE FUNCTION public.customer_order_count(p_customer_id integer)")
	ddl("touch_audit", db.KindProcedure, "CREATE OR REPLACE PROCEDURE public.touch_audit(IN p text)")
	ddl("ticket_seq", db.KindSequence, `CREATE SEQUENCE "public"."ticket_seq"`)

	for kind, want := range map[db.ScriptKind]string{db.ScriptDrop: `DROP TABLE "public"."customers";`, db.ScriptTruncate: `TRUNCATE TABLE "public"."customers";`,
		db.ScriptSelectTop: `SELECT * FROM "public"."customers" LIMIT 100;`} {
		if got := must(s.Script(ctx, kind, db.ObjRef{Name: "customers", Kind: db.KindTable})); got != want {
			t.Errorf("script %s: %q", kind, got)
		}
	}
	if got := must(s.Script(ctx, db.ScriptCreate, db.ObjRef{Schema: "app", Name: "settings", Kind: db.KindTable})); !strings.Contains(got, `CREATE TABLE "app"."settings"`) {
		t.Errorf("create script %s", got)
	}
}

func TestIntegrationRows(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	cust := db.ObjRef{Name: "customers", Kind: db.KindTable}

	res := must(s.Rows(ctx, db.RowQuery{ObjRef: cust, Sort: []db.Sort{{Column: "id", Desc: true}}, Limit: 2, Offset: 1}))
	if len(res.Rows) != 2 || res.Rows[0][0] != int64(2) || res.Rows[1][0] != int64(1) || res.Columns[0].Type != "int4" {
		t.Errorf("rows %+v cols %+v", res.Rows, res.Columns)
	}
	// types: numeric stays a string, timestamptz is text with its offset, jsonb is text, NULL is nil
	r := res.Rows[1]
	if r[4] != "120.50" || r[6] != `{"tags": ["a", "b"], "tier": "gold"}` {
		t.Errorf("row 1: %#v", r)
	}
	if v, ok := r[5].(string); !ok || !strings.Contains(v, "+") {
		t.Errorf("created_at %#v", r[5])
	}
	if res.Rows[0][2] != "bob@example.com" || res.Rows[0][3] != nil || res.Rows[0][6] != nil {
		t.Errorf("row 2: %#v", res.Rows[0])
	}

	flt := func(f db.Filter) *db.Result {
		f.Enabled = true
		return must(s.Rows(ctx, db.RowQuery{ObjRef: cust, Filters: db.FilterSet{Filters: []db.Filter{f}}, Sort: []db.Sort{{Column: "id"}}}))
	}
	if r := flt(db.Filter{Column: "id", Op: "=", Value: "2"}); len(r.Rows) != 1 || r.Rows[0][1] != "Bob O'Neil" {
		t.Errorf("id = '2': %+v", r.Rows)
	}
	if r := flt(db.Filter{Column: "name", Op: "Contains", Value: "100%_"}); len(r.Rows) != 1 || r.Rows[0][0] != int64(3) {
		t.Errorf("contains with wildcards: %+v", r.Rows)
	}
	if r := flt(db.Filter{Column: "name", Op: "Contains", Value: "o'n"}); len(r.Rows) != 1 {
		t.Errorf("contains quote: %+v", r.Rows)
	}
	if r := flt(db.Filter{Column: db.AnyColumn, Op: "Contains", Value: "VIP"}); len(r.Rows) != 1 || r.Rows[0][0] != int64(1) {
		t.Errorf("any column: %+v", r.Rows)
	}
	if r := flt(db.Filter{Column: "id", Op: "IN", Value: "1, 3"}); len(r.Rows) != 2 {
		t.Errorf("IN: %+v", r.Rows)
	}
	if r := flt(db.Filter{Column: "balance", Op: "BETWEEN", Value: "1 AND 200"}); len(r.Rows) != 2 {
		t.Errorf("BETWEEN: %+v", r.Rows)
	}
	if r := flt(db.Filter{Column: "email", Op: "IS NULL"}); len(r.Rows) != 1 {
		t.Errorf("IS NULL: %+v", r.Rows)
	}
	if r := flt(db.Filter{Column: db.RawColumn, Value: "balance > 100"}); len(r.Rows) != 1 {
		t.Errorf("raw: %+v", r.Rows)
	}

	n := must(s.Count(ctx, db.RowQuery{ObjRef: cust}, false))
	if n.N != 3 || !n.Exact {
		t.Errorf("exact %+v", n)
	}
	est := must(s.Count(ctx, db.RowQuery{ObjRef: cust}, true))
	if est.N < 0 || (!est.Exact && est.N != 3 && est.N != 0) { // never analyzed -> exact; analyzed -> catalog
		t.Errorf("estimate %+v", est)
	}
	f := db.FilterSet{Filters: []db.Filter{{Column: "id", Op: ">", Value: "1", Enabled: true}}}
	if n := must(s.Count(ctx, db.RowQuery{ObjRef: cust, Filters: f}, true)); n.N != 2 || !n.Exact {
		t.Errorf("filtered estimate must be exact: %+v", n)
	}
	if n := must(s.Count(ctx, db.RowQuery{ObjRef: db.ObjRef{Name: "paid_orders", Kind: db.KindView}}, true)); n.N != 2 || !n.Exact {
		t.Errorf("view count %+v", n)
	}
}

func TestIntegrationExec(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	res := must(s.Exec(ctx, "SELECT generate_series(1, 5) AS n", 3))
	if !res.HasRows || len(res.Rows) != 3 || !res.Truncated || res.Columns[0].Name != "n" || res.Columns[0].Type != "int4" {
		t.Errorf("limit: %+v", res)
	}
	res = must(s.Exec(ctx, "SELECT generate_series(1, 3)", 3))
	if len(res.Rows) != 3 || res.Truncated {
		t.Errorf("exact limit: %+v", res)
	}
	// connection must still be healthy after a truncated read
	for i := 0; i < 6; i++ {
		must(s.Exec(ctx, "SELECT 1", 0))
	}

	res = must(s.Exec(ctx, `SELECT 1::int2, 2::int8, 1.50::numeric, 'x'::text, true, NULL, '\x6869'::bytea, '2024-01-02'::date, now(), '{"a":1}'::json, ARRAY[1,2], gen_random_uuid(), 1.5::float4`, 0))
	r := res.Rows[0]
	if r[0] != int64(1) || r[1] != int64(2) || r[2] != "1.50" || r[3] != "x" || r[4] != true || r[5] != nil || string(r[6].([]byte)) != "hi" || r[9] != `{"a":1}` || r[10] != "{1,2}" || r[12] != 1.5 {
		t.Errorf("values: %#v", r)
	}
	if _, ok := r[7].(time.Time); !ok {
		t.Errorf("date %T", r[7])
	}
	if _, ok := r[8].(string); !ok {
		t.Errorf("timestamptz %T", r[8])
	}
	if res.Columns[4].Type != "bool" || res.Columns[2].Type != "numeric" || res.Columns[11].Type != "uuid" {
		t.Errorf("types %+v", res.Columns)
	}

	name := tempName()
	t.Cleanup(func() { s.Exec(context.Background(), "DROP TABLE IF EXISTS "+name, 0) })
	if res = must(s.Exec(ctx, "CREATE TABLE "+name+" (a int)", 0)); res.HasRows || res.Message != "CREATE TABLE" {
		t.Errorf("create: %+v", res)
	}
	if res = must(s.Exec(ctx, "INSERT INTO "+name+" SELECT generate_series(1,3)", 0)); res.HasRows || res.Affected != 3 || res.Message != "3 rows affected" {
		t.Errorf("insert: %+v", res)
	}
	if res = must(s.Exec(ctx, "DELETE FROM "+name+" WHERE a = 1", 0)); res.Message != "1 row affected" {
		t.Errorf("delete: %+v", res)
	}
	if res = must(s.Exec(ctx, "DO $$ BEGIN RAISE NOTICE 'hello %', 42; END $$", 0)); !strings.Contains(res.Message, "NOTICE: hello 42") {
		t.Errorf("notice: %+v", res)
	}
	if _, err := s.Exec(ctx, "SELECT * FROM no_such_table", 0); err == nil || !strings.Contains(err.Error(), "no_such_table") {
		t.Errorf("error: %v", err)
	}
	res = must(s.Exec(ctx, "SELECT 'a'::text AS x", 0))
	if res.Duration <= 0 {
		t.Error("duration not set")
	}
}

func TestIntegrationCancel(t *testing.T) {
	s := openTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	start := time.Now()
	_, err := s.Exec(ctx, "SELECT pg_sleep(5)", 0)
	if el := time.Since(start); err == nil || !errors.Is(err, context.Canceled) || el > 1500*time.Millisecond {
		t.Errorf("cancel: err=%v after %v", err, el)
	}
	// the pool recovers
	for i := 0; i < 6; i++ {
		must(s.Exec(context.Background(), "SELECT 1", 0))
	}
	// metadata works while a long query runs
	lctx, lcancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := s.Exec(lctx, "SELECT pg_sleep(5)", 0); done <- err }()
	time.Sleep(200 * time.Millisecond)
	st := time.Now()
	must(s.Objects(context.Background(), "public"))
	if time.Since(st) > time.Second {
		t.Error("metadata blocked by running query")
	}
	pl, ok := s.(db.ProcessLister)
	if !ok {
		t.Fatal("no ProcessLister")
	}
	var pid string
	for _, p := range must(pl.Processes(context.Background())) {
		if strings.Contains(p.Query, "pg_sleep(5)") {
			pid = p.ID
			if p.User != "webtuidb" || p.DB != "webtuidb" || p.State != "active" || p.Elapsed <= 0 {
				t.Errorf("process %+v", p)
			}
		}
	}
	if pid == "" {
		t.Fatal("running query not in process list")
	}
	if err := pl.Kill(context.Background(), pid); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "canceling statement") {
			t.Errorf("killed query: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("kill did not stop the query")
	}
	lcancel()
}

func TestIntegrationApplyAndTable(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	name := tempName()
	ref := db.ObjRef{Name: name, Kind: db.KindTable}
	t.Cleanup(func() {
		for _, n := range []string{name, name + "_c", name + "_r"} {
			s.Exec(context.Background(), "DROP TABLE IF EXISTS "+n+" CASCADE", 0)
		}
	})
	apply := func(sts []db.Statement, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Apply(ctx, sts); err != nil {
			t.Fatalf("%v\n%v", err, sts)
		}
	}

	apply(s.PlanTable(nil, &db.TableDef{Name: name,
		Columns: []db.Column{{Name: "id", Type: "integer", Extra: "GENERATED ALWAYS AS IDENTITY"}, {Name: "label", Type: "text", Nullable: true}, {Name: "day", Type: "date", Nullable: true},
			{Name: "amount", Type: "numeric(10,2)", Nullable: true, Default: "0", HasDef: true}},
		PrimaryKey:  []string{"id"},
		Indexes:     []db.Index{{Name: name + "_label_idx", Columns: []string{"label"}, Algorithm: "btree"}},
		ForeignKeys: []db.ForeignKey{{Name: name + "_fk", Columns: []string{"id"}, RefTable: "customers", RefColumns: []string{"id"}, OnDelete: "CASCADE"}}}))
	cols := must(s.Columns(ctx, ref))
	if len(cols) != 4 || cols[0].Extra != "GENERATED ALWAYS AS IDENTITY" || !cols[0].PK || cols[3].Default != "0" {
		t.Errorf("created columns %+v", cols)
	}
	if ix := must(s.Indexes(ctx, ref)); len(ix) != 2 {
		t.Errorf("created indexes %+v", ix)
	}

	// Plan + Apply: string args against typed columns (int key, date), Expr, Default, NULL.
	apply(s.Plan([]db.Change{{Kind: db.Insert, ObjRef: ref, Set: []db.KeyVal{{Col: "id", Val: db.Default}, {Col: "label", Val: "it's"}, {Col: "day", Val: "2024-01-02"}, {Col: "amount", Val: db.Default}}}}))
	// the identity table's FK needs customer 1, which exists; key tests use a table without identity below
	apply(s.PlanTable(nil, &db.TableDef{Name: name + "_c", Columns: []db.Column{{Name: "id", Type: "integer"}, {Name: "label", Type: "text", Nullable: true}, {Name: "day", Type: "date", Nullable: true}, {Name: "ts", Type: "timestamptz", Nullable: true}}, PrimaryKey: []string{"id"}}))
	cref := db.ObjRef{Name: name + "_c", Kind: db.KindTable}
	apply(s.Plan([]db.Change{
		{Kind: db.Insert, ObjRef: cref, Set: []db.KeyVal{{Col: "id", Val: "7"}, {Col: "label", Val: "it's"}, {Col: "day", Val: "2024-01-02"}, {Col: "ts", Val: db.Expr("now()")}}},
		{Kind: db.Insert, ObjRef: cref, Set: []db.KeyVal{{Col: "id", Val: "8"}, {Col: "label", Val: db.Null}}},
		{Kind: db.Insert, ObjRef: cref, Set: []db.KeyVal{{Col: "id", Val: "9"}, {Col: "label", Val: db.Empty}}},
	}))
	apply(s.Plan([]db.Change{{Kind: db.Update, ObjRef: cref, Key: []db.KeyVal{{Col: "id", Val: "7"}}, Set: []db.KeyVal{{Col: "day", Val: "2025-03-04"}, {Col: "label", Val: "upd"}}}}))
	res := must(s.Rows(ctx, db.RowQuery{ObjRef: cref, Sort: []db.Sort{{Column: "id"}}}))
	if len(res.Rows) != 3 || res.Rows[0][1] != "upd" || res.Rows[0][2].(time.Time).Format("2006-01-02") != "2025-03-04" || res.Rows[1][1] != nil || res.Rows[2][1] != "" {
		t.Errorf("after update: %+v", res.Rows)
	}
	apply(s.Plan([]db.Change{{Kind: db.Delete, ObjRef: cref, Key: []db.KeyVal{{Col: "id", Val: "8"}, {Col: "label", Val: nil}}}}))
	if n := must(s.Count(ctx, db.RowQuery{ObjRef: cref}, false)); n.N != 2 {
		t.Errorf("after delete: %+v", n)
	}
	// atomic: a failing statement rolls back the earlier ones
	err := s.Apply(ctx, []db.Statement{{Text: "DELETE FROM " + name + "_c"}, {Text: "SELECT 1/0"}})
	if err == nil {
		t.Fatal("expected failure")
	}
	if n := must(s.Count(ctx, db.RowQuery{ObjRef: cref}, false)); n.N != 2 {
		t.Errorf("rollback failed: %+v", n)
	}

	// PlanTable alter on the temp table
	old := &db.TableDef{Schema: "public", Name: name, Columns: must(s.Columns(ctx, ref)), Indexes: must(s.Indexes(ctx, ref)), ForeignKeys: must(s.ForeignKeys(ctx, ref))}
	for i := range old.Columns {
		old.Columns[i].OrigName = old.Columns[i].Name
		if old.Columns[i].PK {
			old.PrimaryKey = append(old.PrimaryKey, old.Columns[i].Name)
		}
	}
	nw := &db.TableDef{Schema: "public", Name: name + "_r", PrimaryKey: []string{"id"}}
	nw.Columns = append(nw.Columns, old.Columns[0])
	nw.Columns = append(nw.Columns, db.Column{Name: "title", OrigName: "label", Type: "varchar(40)", Nullable: false, Default: "'none'", HasDef: true}) // rename + type + not null + default
	nw.Columns = append(nw.Columns, db.Column{Name: "extra", Type: "integer", Nullable: true})                                                          // add; "day" and "amount" dropped
	nw.Indexes = []db.Index{{Name: name + "_extra_idx", Columns: []string{"extra"}, Unique: true}}
	apply(s.PlanTable(old, nw))
	rref := db.ObjRef{Name: name + "_r", Kind: db.KindTable}
	cols = must(s.Columns(ctx, rref))
	if len(cols) != 3 || cols[1].Name != "title" || cols[1].Type != "character varying(40)" || cols[1].Nullable || cols[1].Default != "'none'::character varying" || cols[2].Name != "extra" {
		t.Errorf("altered columns %+v", cols)
	}
	if ix := must(s.Indexes(ctx, rref)); len(ix) != 2 || ix[1].Name != name+"_extra_idx" || !ix[1].Unique {
		t.Errorf("altered indexes %+v", ix)
	}
	if fk := must(s.ForeignKeys(ctx, rref)); len(fk) != 0 {
		t.Errorf("fk should be gone: %+v", fk)
	}

	// PlanAdmin clone / truncate / drop on the data table
	apply(s.PlanAdmin(db.OpClone, cref, map[string]string{"new_name": name + "_r2", "with_data": "true"}))
	t.Cleanup(func() { s.Exec(context.Background(), "DROP TABLE IF EXISTS "+name+"_r2", 0) })
	if n := must(s.Count(ctx, db.RowQuery{ObjRef: db.ObjRef{Name: name + "_r2"}}, false)); n.N != 2 {
		t.Errorf("clone rows %+v", n)
	}
	apply(s.PlanAdmin(db.OpTruncate, cref, nil))
	if n := must(s.Count(ctx, db.RowQuery{ObjRef: cref}, false)); n.N != 0 {
		t.Errorf("truncate %+v", n)
	}
	apply(s.PlanAdmin(db.OpRenameObject, db.ObjRef{Name: name + "_r2", Kind: db.KindTable}, map[string]string{"new_name": name + "_r3"}))
	apply(s.PlanAdmin(db.OpDropObject, db.ObjRef{Name: name + "_r3", Kind: db.KindTable}, nil))
	apply(s.PlanAdmin(db.OpDropObject, cref, map[string]string{"cascade": "true"}))
	for _, o := range must(s.Objects(ctx, "public")) {
		if strings.HasPrefix(o.Name, name) && o.Name != name && o.Name != name+"_r" && o.Kind != db.KindSequence {
			t.Errorf("leftover %s", o.Name)
		}
	}
}

func TestIntegrationDatabases(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	name := tempName()
	t.Cleanup(func() {
		s.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name, 0)
		s.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+"_x", 0)
	})
	apply := func(sts []db.Statement, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Apply(ctx, sts); err != nil {
			t.Fatal(err)
		}
	}
	apply(s.PlanAdmin(db.OpCreateDatabase, db.ObjRef{Name: name}, map[string]string{"encoding": "UTF8"}))
	if !has(must(s.Databases(ctx)), name) {
		t.Fatal("created database not listed")
	}
	apply(s.PlanAdmin(db.OpRenameDatabase, db.ObjRef{Name: name}, map[string]string{"new_name": name + "_x"}))
	if err := s.UseDatabase(ctx, name+"_x"); err != nil {
		t.Fatal(err)
	}
	if s.CurrentDatabase() != name+"_x" || len(must(s.Objects(ctx, "public"))) != 0 {
		t.Errorf("use database: %s", s.CurrentDatabase())
	}
	if err := s.UseDatabase(ctx, "webtuidb"); err != nil {
		t.Fatal(err)
	}
	if len(must(s.Objects(ctx, "public"))) == 0 {
		t.Error("switch back lost the objects")
	}
	apply(s.PlanAdmin(db.OpDropDatabase, db.ObjRef{Name: name + "_x"}, nil))
	if has(must(s.Databases(ctx)), name+"_x") {
		t.Error("dropped database still listed")
	}
	if err := s.UseDatabase(ctx, "no_such_db_"+name); err == nil || s.CurrentDatabase() != "webtuidb" {
		t.Errorf("bad database: %v / %s", err, s.CurrentDatabase())
	}
}

func TestIntegrationBootstrap(t *testing.T) {
	if os.Getenv("WEBTUIDB_INTEGRATION") != "1" {
		t.Skip("set WEBTUIDB_INTEGRATION=1")
	}
	c, _ := config.ParseURL("postgresql://webtuidb:webtuidb@127.0.0.1:55432/webtuidb")
	c.Bootstrap = []string{"SET search_path TO app"}
	d, _ := db.Lookup("postgres")
	s, err := d.Open(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.CurrentSchema() != "app" {
		t.Errorf("schema %q", s.CurrentSchema())
	}
	res := must(s.Exec(context.Background(), "SELECT k FROM settings", 0))
	if len(res.Rows) != 1 || res.Rows[0][0] != "theme" {
		t.Errorf("bootstrap search_path: %+v", res.Rows)
	}
	c.Bootstrap = []string{"SELECT nope()"}
	if _, err := d.Open(context.Background(), c, nil); err == nil {
		t.Error("failing bootstrap must fail Open")
	}
	c.Bootstrap, c.SSLMode = nil, "verify-identity"
	if _, err := d.Open(context.Background(), c, nil); err == nil {
		t.Error("verify-identity must fail")
	}
}

func TestIntegrationSerialDDL(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	name := tempName()
	t.Cleanup(func() {
		s.Exec(context.Background(), "DROP TABLE IF EXISTS "+name+" CASCADE", 0)
		s.Exec(context.Background(), "DROP SEQUENCE IF EXISTS "+name+"_s", 0)
	})
	must(s.Exec(ctx, "CREATE SEQUENCE "+name+"_s", 0))
	must(s.Exec(ctx, "CREATE TABLE "+name+" (id serial PRIMARY KEY, n integer NOT NULL DEFAULT nextval('"+name+"_s'))", 0))
	ddl := must(s.Script(ctx, db.ScriptCreate, db.ObjRef{Name: name, Kind: db.KindTable}))
	if !strings.Contains(ddl, `"id" integer DEFAULT nextval('`+name+`_id_seq'::regclass) NOT NULL`) || !strings.Contains(ddl, "CREATE SEQUENCE IF NOT EXISTS ") ||
		!strings.Contains(ddl, " OWNED BY ") || !strings.Contains(ddl, "DEFAULT nextval('"+name+"_s'::regclass)") {
		t.Fatalf("ddl:\n%s", ddl)
	}
	// the copy must run where the serial's sequence does not exist (dropping the table drops it)
	must(s.Exec(ctx, "DROP TABLE "+name, 0))
	must(s.Exec(ctx, ddl, 0))
	must(s.Exec(ctx, "INSERT INTO "+name+" DEFAULT VALUES", 0))
	if r := must(s.Exec(ctx, "SELECT id FROM "+name, 0)); r.Rows[0][0] != int64(1) {
		t.Errorf("rows %v", r.Rows)
	}
}

func TestIntegrationTimestamptzRoundTrip(t *testing.T) {
	s := openTest(t, "SET TIME ZONE 'Pacific/Chatham'") // +13:45, unlike any client zone
	ctx := context.Background()
	name := tempName()
	ref := db.ObjRef{Name: name, Kind: db.KindTable}
	t.Cleanup(func() { s.Exec(context.Background(), "DROP TABLE IF EXISTS "+name, 0) })
	must(s.Exec(ctx, "CREATE TABLE "+name+" (id int, ts timestamptz)", 0))
	must(s.Exec(ctx, "INSERT INTO "+name+" VALUES (1, '2024-01-02 03:04:05.123456+00')", 0))
	v := must(s.Rows(ctx, db.RowQuery{ObjRef: ref})).Rows[0][1]
	if got := db.FormatValue(v); got != "2024-01-02 16:49:05.123456+13:45" {
		t.Errorf("shown %q", got)
	}
	// the shown value used as a row key matches the row it came from
	st := must(s.Plan([]db.Change{{Kind: db.Delete, ObjRef: ref, Key: []db.KeyVal{{Col: "id", Val: int64(1)}, {Col: "ts", Val: v}}}}))
	if err := s.Apply(ctx, st); err != nil {
		t.Fatal(err)
	}
	if n := must(s.Count(ctx, db.RowQuery{ObjRef: ref}, false)); n.N != 0 {
		t.Errorf("row not matched by its timestamptz key: %+v", n)
	}
}

func TestIntegrationEstimateZeroReltuples(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	name := tempName()
	ref := db.ObjRef{Name: name, Kind: db.KindTable}
	t.Cleanup(func() { s.Exec(context.Background(), "DROP TABLE IF EXISTS "+name, 0) })
	must(s.Exec(ctx, "CREATE TABLE "+name+" (id int) WITH (autovacuum_enabled = false)", 0))
	must(s.Exec(ctx, "ANALYZE "+name, 0)) // empty: reltuples = 0
	must(s.Exec(ctx, "INSERT INTO "+name+" VALUES (1), (2), (3)", 0))
	if n := must(s.Count(ctx, db.RowQuery{ObjRef: ref}, true)); n.N != 3 || !n.Exact {
		t.Errorf("count %+v", n)
	}
}

func TestIntegrationUsers(t *testing.T) {
	ua, ok := openTest(t).(db.UserAdmin)
	if !ok {
		t.Fatal("no UserAdmin")
	}
	us, err := ua.Users(context.Background())
	if err != nil || len(us) == 0 {
		t.Fatalf("users %v %v", us, err)
	}
}

// A pooled connection the server dropped must not fail the first query after it.
func TestIntegrationRetryAfterDroppedConnection(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, err := s.Exec(ctx, "SELECT 1", 10); err != nil { // leaves an idle pooled connection
		t.Fatal(err)
	}
	killer := openTest(t)
	if _, err := killer.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND datname = current_database()", 10); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Exec(ctx, "SELECT 2", 10); err != nil {
		t.Errorf("first query after the drop: %v", err)
	}
}
