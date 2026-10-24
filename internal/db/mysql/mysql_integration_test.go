package mysql

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func testConn(t *testing.T) config.Connection {
	t.Helper()
	if os.Getenv("WEBTUIDB_INTEGRATION") != "1" {
		t.Skip("set WEBTUIDB_INTEGRATION=1 to run against the docker databases")
	}
	url := os.Getenv("WEBTUIDB_TEST_URL_MYSQL")
	if url == "" {
		url = "mysql://webtuidb:webtuidb@127.0.0.1:53306/webtuidb"
	}
	c, err := config.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func open(t *testing.T, bootstrap ...string) *session {
	t.Helper()
	c := testConn(t)
	c.Bootstrap = bootstrap
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var d db.Driver = drv{}
	if c.Driver == config.MariaDB { // WEBTUIDB_TEST_URL_MYSQL=mariadb://... runs this suite through the MariaDB driver's configuration
		d = New(config.MariaDB, func(c *db.Capabilities) { c.Sequences = true })
	}
	s, err := d.Open(ctx, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s.(*session)
}

// tempTable returns a unique table name that is dropped when the test ends.
func tempTable(t *testing.T, s *session) string {
	t.Helper()
	name := fmt.Sprintf("webtuidb_test_%x", rand.Uint32())
	t.Cleanup(func() { s.db().Exec("DROP TABLE IF EXISTS " + q(name)) })
	return name
}

func exec(t *testing.T, s *session, stmt string) *db.Result {
	t.Helper()
	r, err := s.Exec(context.Background(), stmt, 0)
	if err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
	return r
}

func ref(name string) db.ObjRef { return db.ObjRef{Name: name, Kind: db.KindTable} }

func TestIntegrationOpen(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.ServerInfo(), "MySQL 8") && !strings.HasPrefix(s.ServerInfo(), "MariaDB") {
		t.Errorf("server info %q", s.ServerInfo())
	}
	dbs, err := s.Databases(ctx)
	if err != nil || !contains(dbs, "webtuidb") {
		t.Errorf("databases %v %v", dbs, err)
	}
	if s.CurrentDatabase() != "webtuidb" {
		t.Errorf("current db %q", s.CurrentDatabase())
	}
	if sc, _ := s.Schemas(ctx); sc != nil || s.Caps().Schemas {
		t.Error("mysql has no schemas")
	}
}

func isNow(def string) bool {
	return strings.EqualFold(strings.TrimSuffix(def, "()"), "CURRENT_TIMESTAMP")
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func TestIntegrationBadPassword(t *testing.T) {
	c := testConn(t)
	c.Password = "wrong"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := (drv{}).Open(ctx, c, nil); err == nil {
		t.Fatal("expected auth error")
	}
}

func TestIntegrationBootstrapEveryConnection(t *testing.T) {
	s := open(t, "SET @webtuidb_boot = 42")
	var wg sync.WaitGroup
	for range 4 { // SLEEP forces four distinct pooled connections
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.Exec(context.Background(), "SELECT @webtuidb_boot, SLEEP(0.3)", 0)
			if err != nil || r.Rows[0][0] != int64(42) {
				t.Errorf("bootstrap not applied: %v %v", r, err)
			}
		}()
	}
	wg.Wait()
}

func TestIntegrationCatalog(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	objs, err := s.Objects(ctx, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]db.Kind{}
	for _, o := range objs {
		kinds[o.Name] = o.Kind
	}
	for name, k := range map[string]db.Kind{"customers": db.KindTable, "orders": db.KindTable, "no_pk": db.KindTable, "audit_log": db.KindTable,
		"paid_orders": db.KindView, "customer_order_count": db.KindFunction, "touch_audit": db.KindProcedure} {
		if kinds[name] != k {
			t.Errorf("object %s: kind %q want %q", name, kinds[name], k)
		}
	}

	cols, err := s.Columns(ctx, ref("customers"))
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]db.Column{}
	for _, c := range cols {
		by[c.Name] = c
	}
	if len(cols) != 7 || cols[0].Name != "id" || !by["id"].PK || by["id"].Extra != "auto_increment" || !strings.HasPrefix(by["id"].Type, "int") {
		t.Errorf("customers columns %+v", cols)
	}
	// MariaDB reports int(11) and current_timestamp()
	if by["name"].Nullable || !by["email"].Nullable || by["balance"].Default != "0.00" || !isNow(by["created_at"].Default) || by["created_at"].Type != "timestamp" {
		t.Errorf("customers column details %+v", cols)
	}
	ocols, _ := s.Columns(ctx, ref("orders"))
	for _, c := range ocols {
		switch c.Name {
		case "status":
			if c.Default != "'new'" || !c.HasDef {
				t.Errorf("status default %q", c.Default)
			}
		case "customer_id":
			if c.FK == nil || c.FK.RefTable != "customers" {
				t.Errorf("customer_id fk %+v", c.FK)
			}
		}
	}

	idx, err := s.Indexes(ctx, ref("orders"))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]db.Index{}
	for _, i := range idx {
		names[i.Name] = i
	}
	if idx[0].Name != "PRIMARY" || !idx[0].Primary || !idx[0].Unique || names["orders_status_idx"].Columns[0] != "status" || names["orders_status_idx"].Unique || names["orders_status_idx"].Algorithm != "BTREE" {
		t.Errorf("orders indexes %+v", idx)
	}
	cidx, _ := s.Indexes(ctx, ref("customers"))
	if len(cidx) != 2 || cidx[1].Name != "email" || !cidx[1].Unique {
		t.Errorf("customers indexes %+v", cidx)
	}

	fks, err := s.ForeignKeys(ctx, ref("orders"))
	if err != nil || len(fks) != 1 || fks[0].Name != "fk_orders_customer" || fks[0].RefTable != "customers" || fks[0].RefSchema != "" ||
		fks[0].Columns[0] != "customer_id" || fks[0].RefColumns[0] != "id" || fks[0].OnDelete != "CASCADE" || fks[0].OnUpdate != "CASCADE" {
		t.Errorf("fks %+v %v", fks, err)
	}

	trg, err := s.Triggers(ctx, ref("orders"))
	if err != nil || len(trg) != 1 || trg[0].Name != "orders_audit" || trg[0].Timing != "AFTER" || trg[0].Event != "INSERT" || !strings.Contains(trg[0].Statement, "audit_log") {
		t.Errorf("triggers %+v %v", trg, err)
	}

	for _, o := range []db.ObjRef{ref("customers"), {Name: "paid_orders", Kind: db.KindView}, {Name: "customer_order_count", Kind: db.KindFunction}, {Name: "touch_audit", Kind: db.KindProcedure}} {
		d, err := s.DDL(ctx, o)
		if err != nil || !strings.HasPrefix(d, "CREATE") || !strings.HasSuffix(d, ";") {
			t.Errorf("ddl %s: %q %v", o.Name, d, err)
		}
	}
}

func TestIntegrationRowsAndCount(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	r, err := s.Rows(ctx, db.RowQuery{ObjRef: ref("customers"), Sort: []db.Sort{{Column: "id", Desc: true}}, Limit: 2, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 2 || r.Rows[0][0] != int64(2) || r.Rows[1][0] != int64(1) || r.Truncated {
		t.Fatalf("rows %v", r.Rows)
	}
	ann := r.Rows[1]
	json := `{"tags": ["a", "b"], "tier": "gold"}` // MySQL normalizes JSON; MariaDB keeps the text as inserted
	if s.maria {
		json = `{"tier": "gold", "tags": ["a","b"]}`
	}
	if ann[3] != "vip" || ann[4] != "120.50" || ann[6] != json {
		t.Errorf("ann %#v", ann)
	}
	if _, ok := ann[5].(time.Time); !ok {
		t.Errorf("created_at is %T", ann[5])
	}
	if r.Rows[0][2] != "bob@example.com" || r.Rows[0][3] != nil {
		t.Errorf("bob %#v", r.Rows[0])
	}
	if r.Columns[0].Type != "INT" || r.Columns[4].Type != "DECIMAL" {
		t.Errorf("types %+v", r.Columns)
	}

	any100 := db.FilterSet{Filters: []db.Filter{{Column: db.AnyColumn, Op: "Contains", Value: "100%_done", Enabled: true}}}
	r, err = s.Rows(ctx, db.RowQuery{ObjRef: ref("customers"), Filters: any100})
	if err != nil || len(r.Rows) != 1 || r.Rows[0][1] != "Cy 100%_done" {
		t.Errorf("any-column filter: %v %v", r, err)
	}
	bob := db.FilterSet{Filters: []db.Filter{{Column: "name", Op: "=", Value: "Bob O'Neil", Enabled: true}}}
	r, err = s.Rows(ctx, db.RowQuery{ObjRef: ref("customers"), Filters: bob, Columns: []string{"id", "name"}})
	if err != nil || len(r.Rows) != 1 || len(r.Columns) != 2 {
		t.Errorf("= filter: %v %v", r, err)
	}

	c, err := s.Count(ctx, db.RowQuery{ObjRef: ref("customers")}, false)
	if err != nil || c.N != 3 || !c.Exact {
		t.Errorf("exact count %+v %v", c, err)
	}
	c, err = s.Count(ctx, db.RowQuery{ObjRef: ref("customers")}, true)
	if err != nil || c.N != 3 {
		t.Errorf("estimate count %+v %v", c, err)
	}
	c, err = s.Count(ctx, db.RowQuery{ObjRef: ref("customers"), Filters: any100}, true)
	if err != nil || c.N != 1 || !c.Exact {
		t.Errorf("filtered count %+v %v", c, err)
	}
	c, err = s.Count(ctx, db.RowQuery{ObjRef: db.ObjRef{Name: "paid_orders", Kind: db.KindView}}, true)
	if err != nil || c.N != 2 || !c.Exact {
		t.Errorf("view count %+v %v", c, err)
	}
}

func TestIntegrationExecValues(t *testing.T) {
	s := open(t, "SET SESSION sql_mode = ''") // allows zero dates
	asJSON := "CAST('{\"a\":1}' AS JSON)"
	if s.maria { // MariaDB has no CAST AS JSON and stores the text unchanged
		asJSON = "'{\"a\": 1}'"
	}
	r := exec(t, s, "SELECT CAST(1 AS DECIMAL(10,2)), CAST(-1 AS SIGNED), CAST(1 AS UNSIGNED), 1.5e0, 'x', X'01ff', DATE '2024-01-02', TIMESTAMP '2024-01-02 03:04:05', NULL, "+asJSON+", TIME '12:30:00'")
	want := []any{"1.00", int64(-1), uint64(1), 1.5, "x", []byte{1, 0xff}, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), nil, `{"a": 1}`, "12:30:00"}
	if s.maria { // MariaDB types CAST(.. AS UNSIGNED) as an unsigned INT, which the driver reads as int64
		want[2], want[9] = int64(1), `{"a": 1}`
	}
	for i, w := range want {
		if fmt.Sprintf("%#v", r.Rows[0][i]) != fmt.Sprintf("%#v", w) {
			t.Errorf("col %d (%s): got %#v want %#v", i, r.Columns[i].Type, r.Rows[0][i], w)
		}
	}

	tbl := tempTable(t, s)
	exec(t, s, "CREATE TABLE "+q(tbl)+" (id INT PRIMARY KEY, flag TINYINT(1), big BIGINT UNSIGNED, d DATE, dt DATETIME, bin VARBINARY(4), txt TEXT, e ENUM('a','b'), st SET('x','y'), bt BIT(8))")
	exec(t, s, "INSERT INTO "+q(tbl)+" VALUES (1, 1, 18446744073709551615, '0000-00-00', '2024-05-06 07:08:09', 'ab', 'hello', 'b', 'x,y', b'101')")
	r = exec(t, s, "SELECT * FROM "+q(tbl))
	want = []any{int64(1), int64(1), uint64(18446744073709551615), "0000-00-00", time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC), []byte("ab"), "hello", "b", "x,y", uint64(5)}
	for i, w := range want {
		if fmt.Sprintf("%#v", r.Rows[0][i]) != fmt.Sprintf("%#v", w) {
			t.Errorf("col %d (%s): got %#v want %#v", i, r.Columns[i].Type, r.Rows[0][i], w)
		}
	}
}

func TestIntegrationExecLimitAndWrite(t *testing.T) {
	s := open(t)
	r, err := s.Exec(context.Background(), "SELECT * FROM customers ORDER BY id", 2)
	if err != nil || len(r.Rows) != 2 || !r.Truncated || !r.HasRows || r.Duration <= 0 {
		t.Fatalf("limit: %+v %v", r, err)
	}
	r, _ = s.Exec(context.Background(), "SELECT * FROM customers", 3)
	if len(r.Rows) != 3 || r.Truncated {
		t.Errorf("exact limit: %+v", r)
	}
	// a truncated read closes its connection; the pool must keep working
	if r = exec(t, s, "SELECT 1"); r.Rows[0][0] != int64(1) {
		t.Errorf("after truncate: %v", r.Rows)
	}

	tbl := tempTable(t, s)
	r = exec(t, s, "CREATE TABLE "+q(tbl)+" (a INT)")
	if r.HasRows || r.Affected != 0 {
		t.Errorf("create: %+v", r)
	}
	r = exec(t, s, "INSERT INTO "+q(tbl)+" VALUES (1),(2),(3)")
	if r.HasRows || r.Affected != 3 || r.Message != "3 rows affected" {
		t.Errorf("insert: %+v", r)
	}
	r = exec(t, s, "UPDATE "+q(tbl)+" SET a = 9 WHERE a = 1")
	if r.Affected != 1 || r.Message != "1 row affected" {
		t.Errorf("update: %+v", r)
	}
	if _, err := s.Exec(context.Background(), "SELEKT 1", 0); err == nil {
		t.Error("syntax error expected")
	}
}

func TestIntegrationCancel(t *testing.T) {
	s := open(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	start := time.Now()
	_, err := s.Exec(ctx, "SELECT SLEEP(5)", 0)
	if err == nil || time.Since(start) > 1500*time.Millisecond {
		t.Fatalf("cancel: err=%v after %v", err, time.Since(start))
	}
	// the pool keeps working, repeatedly
	for range 6 {
		if r, err := s.Exec(context.Background(), "SELECT 1", 0); err != nil || r.Rows[0][0] != int64(1) {
			t.Fatalf("after cancel: %v %v", r, err)
		}
	}
	// cancelling a table browse stops it too
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if _, err := s.Rows(ctx, db.RowQuery{ObjRef: ref("customers")}); err == nil {
		t.Error("cancelled Rows succeeded")
	}
}

func TestIntegrationProcesses(t *testing.T) {
	s := open(t)
	ps, err := s.Processes(context.Background())
	if err != nil || len(ps) == 0 {
		t.Fatalf("processes %v %v", ps, err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.Exec(context.Background(), "SELECT SLEEP(10) /* webtuidb-kill-me */", 0)
		done <- err
	}()
	var id string
	for range 40 {
		time.Sleep(100 * time.Millisecond)
		ps, _ = s.Processes(context.Background())
		for _, p := range ps {
			if strings.Contains(p.Query, "webtuidb-kill-me") {
				id = p.ID
			}
		}
		if id != "" {
			break
		}
	}
	if id == "" {
		t.Fatal("sleeping query not listed")
	}
	if err := s.Kill(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done: // KILL QUERY makes SLEEP return early (with a result or an error)
	case <-time.After(3 * time.Second):
		t.Fatal("killed query still running")
	}
	if err := s.Kill(context.Background(), "1; DROP TABLE x"); err == nil {
		t.Error("bad id accepted")
	}
}

func TestIntegrationPlanApply(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	tbl := tempTable(t, s)
	exec(t, s, "CREATE TABLE "+q(tbl)+" (id INT PRIMARY KEY, name VARCHAR(20), d DATE, amount DECIMAL(10,2), made DATETIME, n INT DEFAULT 5)")
	o := ref(tbl)
	apply := func(changes ...db.Change) {
		t.Helper()
		st, err := s.Plan(changes)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Apply(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	// string args against integer / date / decimal columns
	apply(db.Change{Kind: db.Insert, ObjRef: o, Set: []db.KeyVal{{Col: "id", Val: "7"}, {Col: "name", Val: "it's"}, {Col: "d", Val: "2024-01-02"}, {Col: "amount", Val: "12.50"},
		{Col: "made", Val: db.Expr("NOW()")}, {Col: "n", Val: db.Default}}})
	apply(db.Change{Kind: db.Update, ObjRef: o, Key: []db.KeyVal{{Col: "id", Val: "7"}}, Set: []db.KeyVal{{Col: "name", Val: db.Empty}, {Col: "amount", Val: db.Null}, {Col: "d", Val: "2025-03-04"}}})
	r := exec(t, s, "SELECT id, name, d, amount, n FROM "+q(tbl))
	if len(r.Rows) != 1 || r.Rows[0][0] != int64(7) || r.Rows[0][1] != "" || r.Rows[0][2] != time.Date(2025, 3, 4, 0, 0, 0, 0, time.UTC) || r.Rows[0][3] != nil || r.Rows[0][4] != int64(5) {
		t.Errorf("after update: %#v", r.Rows)
	}
	apply(db.Change{Kind: db.Delete, ObjRef: o, Key: []db.KeyVal{{Col: "id", Val: "7"}}})
	if r = exec(t, s, "SELECT COUNT(*) FROM "+q(tbl)); r.Rows[0][0] != int64(0) {
		t.Errorf("after delete: %v", r.Rows)
	}

	// a failing statement rolls back the earlier ones
	st, _ := s.Plan([]db.Change{
		{Kind: db.Insert, ObjRef: o, Set: []db.KeyVal{{Col: "id", Val: "1"}}},
		{Kind: db.Insert, ObjRef: o, Set: []db.KeyVal{{Col: "id", Val: "1"}}},
	})
	if err := s.Apply(ctx, st); err == nil {
		t.Fatal("duplicate key accepted")
	}
	if r = exec(t, s, "SELECT COUNT(*) FROM "+q(tbl)); r.Rows[0][0] != int64(0) {
		t.Errorf("not rolled back: %v", r.Rows)
	}
}

// tableDef loads the editable structure the way the UI does.
func tableDef(t *testing.T, s *session, name string) *db.TableDef {
	t.Helper()
	ctx := context.Background()
	cols, err := s.Columns(ctx, ref(name))
	if err != nil {
		t.Fatal(err)
	}
	idx, _ := s.Indexes(ctx, ref(name))
	fks, _ := s.ForeignKeys(ctx, ref(name))
	def := &db.TableDef{Name: name, Columns: cols, Indexes: idx, ForeignKeys: fks}
	for _, c := range cols {
		if c.PK {
			def.PrimaryKey = append(def.PrimaryKey, c.Name)
		}
	}
	return def
}

func TestIntegrationPlanTable(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	tbl := tempTable(t, s)
	renamed := tbl + "_r"
	t.Cleanup(func() { s.db().Exec("DROP TABLE IF EXISTS " + q(renamed)) })

	create := &db.TableDef{Name: tbl,
		Columns: []db.Column{
			{Name: "id", Type: "int", Extra: "auto_increment"},
			{Name: "label", Type: "varchar(30)", Nullable: true, Default: "'x'"},
			{Name: "cust", Type: "int", Nullable: true},
			{Name: "stamp", Type: "timestamp", Default: "CURRENT_TIMESTAMP", Extra: "on update CURRENT_TIMESTAMP"},
		},
		PrimaryKey:  []string{"id"},
		Indexes:     []db.Index{{Name: "label_idx", Columns: []string{"label"}}},
		ForeignKeys: []db.ForeignKey{{Name: "fk_" + tbl, Columns: []string{"cust"}, RefTable: "customers", RefColumns: []string{"id"}, OnDelete: "SET NULL", OnUpdate: "CASCADE"}}}
	st, err := s.PlanTable(nil, create)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, st); err != nil {
		t.Fatalf("%s: %v", st[0].Text, err)
	}

	// the structure we created reads back, and diffing it with itself plans nothing
	loaded := tableDef(t, s, tbl)
	if got := loaded.Columns[3]; !isNow(got.Default) || !strings.EqualFold(strings.Replace(got.Extra, "()", "", 1), "on update CURRENT_TIMESTAMP") {
		t.Errorf("stamp column %+v", got)
	}
	if loaded.Columns[1].Default != "'x'" || len(loaded.ForeignKeys) != 1 || loaded.ForeignKeys[0].OnDelete != "SET NULL" {
		t.Errorf("loaded %+v", loaded)
	}
	if st, err := s.PlanTable(loaded, tableDef(t, s, tbl)); err != nil || len(st) != 0 {
		t.Errorf("self diff: %v %v", st, err)
	}

	// alter: rename + retype label, add a column first, move stamp up, new index, drop FK, rename table
	next := tableDef(t, s, tbl)
	next.Columns = []db.Column{
		{Name: "added", Type: "int", Nullable: true, Default: "7"},
		next.Columns[0],
		next.Columns[3],
		next.Columns[1],
		next.Columns[2],
	}
	next.Columns[3].Name, next.Columns[3].Type, next.Columns[3].Default = "title", "varchar(80)", "'y'"
	next.Indexes = []db.Index{{Name: "label_idx", Columns: []string{"title"}}, {Name: "added_idx", Columns: []string{"added"}, Unique: true}}
	next.ForeignKeys = nil
	next.Name = renamed
	st, err = s.PlanTable(loaded, next)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, st); err != nil {
		for _, x := range st {
			t.Log(x.Text)
		}
		t.Fatal(err)
	}
	got := tableDef(t, s, renamed)
	var names []string
	for _, c := range got.Columns {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "added,id,stamp,title,cust" {
		t.Errorf("column order %v", names)
	}
	if got.Columns[3].Type != "varchar(80)" || got.Columns[3].Default != "'y'" || got.Columns[0].Default != "7" || len(got.ForeignKeys) != 0 {
		t.Errorf("altered %+v", got)
	}
	var ix []string
	for _, i := range got.Indexes {
		ix = append(ix, fmt.Sprintf("%s:%v:%v", i.Name, i.Columns, i.Unique))
	}
	if strings.Join(ix, " ") != "PRIMARY:[id]:true added_idx:[added]:true label_idx:[title]:false" {
		t.Errorf("indexes %v", ix)
	}

	// add a foreign key back and change the primary key
	again := tableDef(t, s, renamed)
	again.ForeignKeys = []db.ForeignKey{{Name: "fk2_" + tbl, Columns: []string{"cust"}, RefTable: "customers", RefColumns: []string{"id"}}}
	again.Columns[4].Nullable = true
	st, err = s.PlanTable(got, again)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, st); err != nil {
		t.Fatal(err)
	}
	if fks := tableDef(t, s, renamed).ForeignKeys; len(fks) != 1 || fks[0].Name != "fk2_"+tbl {
		t.Errorf("fks %+v", fks)
	}

	// dropping a column
	drop := tableDef(t, s, renamed)
	drop.Columns = drop.Columns[:4]
	drop.ForeignKeys = nil
	st, _ = s.PlanTable(tableDef(t, s, renamed), drop)
	if err := s.Apply(ctx, st); err != nil {
		t.Fatal(err)
	}
	if cols := tableDef(t, s, renamed).Columns; len(cols) != 4 {
		t.Errorf("after drop %+v", cols)
	}
}

func TestIntegrationPlanAdmin(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	tbl := tempTable(t, s)
	exec(t, s, "CREATE TABLE "+q(tbl)+" (id INT PRIMARY KEY AUTO_INCREMENT, v VARCHAR(5))")
	exec(t, s, "INSERT INTO "+q(tbl)+" (v) VALUES ('a'),('b')")
	run := func(op db.AdminOp, r db.ObjRef, opts map[string]string) {
		t.Helper()
		st, err := s.PlanAdmin(op, r, opts)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Apply(ctx, st); err != nil {
			t.Fatalf("%s: %v", st[0].Text, err)
		}
	}
	count := func(name string) int64 {
		return exec(t, s, "SELECT COUNT(*) FROM "+q(name)).Rows[0][0].(int64)
	}
	clone, moved := tbl+"_c", tbl+"_m"
	t.Cleanup(func() {
		s.db().Exec("DROP TABLE IF EXISTS " + q(clone))
		s.db().Exec("DROP TABLE IF EXISTS " + q(moved))
	})
	run(db.OpClone, ref(tbl), map[string]string{"new_name": clone})
	if count(clone) != 0 {
		t.Error("structure-only clone has rows")
	}
	run(db.OpDropObject, ref(clone), nil)
	run(db.OpClone, ref(tbl), map[string]string{"new_name": clone, "with_data": "true"})
	if count(clone) != 2 {
		t.Error("clone with data lost rows")
	}
	run(db.OpRenameObject, ref(clone), map[string]string{"new_name": moved})
	if count(moved) != 2 {
		t.Error("rename lost rows")
	}
	run(db.OpTruncate, ref(tbl), nil)
	if count(tbl) != 0 {
		t.Error("truncate left rows")
	}
	run(db.OpDropObject, ref(tbl), nil)
	if _, err := s.Exec(ctx, "SELECT 1 FROM "+q(tbl), 0); err == nil {
		t.Error("table still exists")
	}
}

func TestIntegrationDatabases(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	name := fmt.Sprintf("webtuidb_test_%x", rand.Uint32())
	t.Cleanup(func() { s.db().Exec("DROP DATABASE IF EXISTS " + q(name)) })
	st, err := s.PlanAdmin(db.OpCreateDatabase, db.ObjRef{Name: name}, map[string]string{"encoding": "utf8mb4", "collation": "utf8mb4_unicode_ci"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, st); err != nil {
		t.Fatal(err)
	}
	if dbs, _ := s.Databases(ctx); !contains(dbs, name) {
		t.Fatal("new database not listed")
	}
	if err := s.UseDatabase(ctx, name); err != nil {
		t.Fatal(err)
	}
	if s.CurrentDatabase() != name {
		t.Errorf("current %q", s.CurrentDatabase())
	}
	// every pooled connection is on the new database, not just one
	for range 3 {
		if r := exec(t, s, "SELECT DATABASE(), SLEEP(0.1)"); r.Rows[0][0] != name {
			t.Errorf("connection on %v", r.Rows[0][0])
		}
	}
	exec(t, s, "CREATE TABLE only_here (a INT)")
	if objs, _ := s.Objects(ctx, ""); len(objs) != 1 || objs[0].Name != "only_here" {
		t.Errorf("objects %+v", objs)
	}
	if _, err := s.Exec(ctx, "USE `webtuidb`", 0); err != nil || s.CurrentDatabase() != "webtuidb" {
		t.Errorf("USE statement: %v, current %q", err, s.CurrentDatabase())
	}
	if _, err := s.Rows(ctx, db.RowQuery{ObjRef: db.ObjRef{Schema: name, Name: "only_here"}}); err != nil {
		t.Errorf("schema-qualified browse: %v", err)
	}
	if err := s.UseDatabase(ctx, "webtuidb_does_not_exist"); err == nil || s.CurrentDatabase() != "webtuidb" {
		t.Errorf("bad database: %v, current %q", err, s.CurrentDatabase())
	}
	st, _ = s.PlanAdmin(db.OpDropDatabase, db.ObjRef{Name: name}, nil)
	if err := s.Apply(ctx, st); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationScripts(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	want := map[db.ScriptKind]string{
		db.ScriptDrop:      "DROP TABLE `customers`;",
		db.ScriptTruncate:  "TRUNCATE TABLE `customers`;",
		db.ScriptSelectTop: "SELECT * FROM `customers` LIMIT 100;",
	}
	for k, w := range want {
		if got, err := s.Script(ctx, k, ref("customers")); err != nil || got != w {
			t.Errorf("%s: %q %v", k, got, err)
		}
	}
	if got, err := s.Script(ctx, db.ScriptCreate, ref("customers")); err != nil || !strings.HasPrefix(got, "CREATE TABLE `customers`") {
		t.Errorf("create: %q %v", got, err)
	}
	if got, _ := s.Script(ctx, db.ScriptDrop, db.ObjRef{Name: "paid_orders", Kind: db.KindView}); got != "DROP VIEW `paid_orders`;" {
		t.Errorf("drop view: %q", got)
	}
}

// Comments, collations, generated columns and functional indexes survive a structure edit and a copy.
func TestIntegrationPlanTableKeepsAttributes(t *testing.T) {
	s := open(t)
	if s.maria {
		t.Skip("MySQL-only syntax (functional index, NOT NULL generated column); internal/db/mariadb covers the MariaDB equivalent")
	}
	ctx := context.Background()
	tbl := tempTable(t, s)
	copied := tbl + "_c"
	t.Cleanup(func() { s.db().Exec("DROP TABLE IF EXISTS " + q(copied)) })
	exec(t, s, "CREATE TABLE "+q(tbl)+` (id INT PRIMARY KEY, a VARCHAR(10) COLLATE utf8mb4_bin COMMENT 'it''s a \\ note',
		g VARCHAR(30) AS (concat(a, 'x''y')) VIRTUAL COMMENT 'gen', h INT AS (length(a) + 1) STORED NOT NULL, INDEX fx ((lower(a))))`)
	loaded := tableDef(t, s, tbl)
	if st, err := s.PlanTable(loaded, tableDef(t, s, tbl)); err != nil || len(st) != 0 {
		t.Fatalf("self diff: %v %v", st, err)
	}
	next := tableDef(t, s, tbl)
	next.Columns[1].Type = strings.Replace(next.Columns[1].Type, "(10)", "(20)", 1)
	next.Columns[2].Type = strings.Replace(next.Columns[2].Type, "(30)", "(40)", 1)
	next.Columns[3].Type = "bigint"
	next.Indexes[1].Unique = true // forces DROP INDEX + ADD of the functional index
	st, err := s.PlanTable(loaded, next)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, st); err != nil {
		t.Fatalf("%v\n%s", err, st[0].Text)
	}
	check := func(name string) {
		t.Helper()
		got := tableDef(t, s, name)
		c := got.Columns
		if c[1].Type != "varchar(20) COLLATE utf8mb4_bin" || c[1].Extra != `COMMENT 'it''s a \\ note'` {
			t.Errorf("%s a: %+v", name, c[1])
		}
		if c[2].Extra != "GENERATED ALWAYS AS (concat(`a`,_utf8mb4'x\\'y')) VIRTUAL COMMENT 'gen'" || c[2].Type != "varchar(40)" {
			t.Errorf("%s g: %+v", name, c[2])
		}
		if c[3].Extra != "GENERATED ALWAYS AS ((length(`a`) + 1)) STORED" || c[3].Type != "bigint" || c[3].Nullable {
			t.Errorf("%s h: %+v", name, c[3])
		}
		if ix := got.Indexes[1]; ix.Name != "fx" || !ix.Unique || strings.Join(ix.Columns, ",") != "(lower(`a`))" {
			t.Errorf("%s index: %+v", name, ix)
		}
	}
	check(tbl)
	exec(t, s, "INSERT INTO "+q(tbl)+" (id, a) VALUES (1, 'q')")
	if r := exec(t, s, "SELECT g, h FROM "+q(tbl)); r.Rows[0][0] != "qx'y" || r.Rows[0][1] != int64(2) {
		t.Errorf("generated values %v", r.Rows)
	}

	cp := tableDef(t, s, tbl)
	cp.Name = copied
	if st, err = s.PlanTable(nil, cp); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, st); err != nil {
		t.Fatalf("%v\n%s", err, st[0].Text)
	}
	check(copied)
}

func TestIntegrationApplyDDLStopsAndReports(t *testing.T) {
	s := open(t)
	tbl := tempTable(t, s)
	err := s.Apply(context.Background(), []db.Statement{
		{Text: "CREATE TABLE " + q(tbl) + " (id INT)"},
		{Text: "ALTER TABLE " + q(tbl) + " ADD COLUMN nope NOT_A_TYPE"},
		{Text: "ALTER TABLE " + q(tbl) + " ADD COLUMN never INT"},
	})
	if err == nil || !strings.Contains(err.Error(), "statement 2") || !strings.Contains(err.Error(), "1 of 3 statements were already applied") {
		t.Fatalf("error %v", err)
	}
	if cols := tableDef(t, s, tbl).Columns; len(cols) != 1 {
		t.Errorf("columns after partial batch %+v", cols)
	}
}

func TestIntegrationUsers(t *testing.T) {
	us, err := open(t).Users(context.Background())
	if err != nil || len(us) == 0 {
		t.Fatalf("users %v %v", us, err)
	}
}
