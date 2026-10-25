package mariadb

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

// These tests drive the registered "mariadb" driver through the db.Session interface only, against
// the docker-compose mariadb service (WEBTUIDB_INTEGRATION=1). The shared MySQL scenarios run against
// the same server from internal/db/mysql with WEBTUIDB_TEST_URL_MYSQL=mariadb://...

func testConn(t *testing.T) config.Connection {
	t.Helper()
	if os.Getenv("WEBTUIDB_INTEGRATION") != "1" {
		t.Skip("set WEBTUIDB_INTEGRATION=1 to run against the docker databases")
	}
	url := os.Getenv("WEBTUIDB_TEST_URL_MARIADB")
	if url == "" {
		url = "mariadb://webtuidb:webtuidb@127.0.0.1:53307/webtuidb"
	}
	c, err := config.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func openConn(t *testing.T, c config.Connection) db.Session {
	t.Helper()
	d, err := db.Lookup(config.MariaDB)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s, err := d.Open(ctx, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func open(t *testing.T, bootstrap ...string) db.Session {
	c := testConn(t)
	c.Bootstrap = bootstrap
	return openConn(t, c)
}

func q(name string) string { return db.MySQL.Quote(name) }

func ref(name string) db.ObjRef { return db.ObjRef{Name: name, Kind: db.KindTable} }

// temp registers cleanup for a uniquely named object and returns its name.
func temp(t *testing.T, s db.Session, kind string) string {
	t.Helper()
	name := fmt.Sprintf("webtuidb_test_%x", rand.Uint32())
	t.Cleanup(func() { s.Exec(context.Background(), "DROP "+kind+" IF EXISTS "+q(name), 0) })
	return name
}

func exec(t *testing.T, s db.Session, stmt string) *db.Result {
	t.Helper()
	r, err := s.Exec(context.Background(), stmt, 0)
	if err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
	return r
}

func scalar(t *testing.T, s db.Session, stmt string) any {
	t.Helper()
	return exec(t, s, stmt).Rows[0][0]
}

func apply(t *testing.T, s db.Session, st []db.Statement, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(context.Background(), st); err != nil {
		for _, x := range st {
			t.Log(x.Text)
		}
		t.Fatal(err)
	}
}

func tableDef(t *testing.T, s db.Session, name string) *db.TableDef {
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

func column(def *db.TableDef, name string) db.Column {
	for _, c := range def.Columns {
		if c.Name == name {
			return c
		}
	}
	return db.Column{}
}

func TestIntegrationOpen(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.ServerInfo(), "MariaDB 1") {
		t.Errorf("server info %q", s.ServerInfo())
	}
	c := s.Caps()
	if !c.Sequences || !c.Transactions || !c.ProcessList || c.Schemas || c.Dialect != "mysql" || c.Editor != "sql" || len(c.Types) == 0 {
		t.Errorf("caps %+v", c)
	}
	if dbs, err := s.Databases(ctx); err != nil || !contains(dbs, "webtuidb") {
		t.Errorf("databases %v %v", dbs, err)
	}
	if s.CurrentDatabase() != "webtuidb" {
		t.Errorf("current db %q", s.CurrentDatabase())
	}
	if sc, _ := s.Schemas(ctx); sc != nil || s.CurrentSchema() != "" {
		t.Error("mariadb has no schemas")
	}
	bad := testConn(t)
	bad.Password = "wrong"
	d, _ := db.Lookup(config.MariaDB)
	if _, err := d.Open(ctx, bad, nil); err == nil {
		t.Error("expected auth error")
	}
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func TestIntegrationBootstrapEveryConnection(t *testing.T) {
	s := open(t, "SET @webtuidb_boot = 42")
	done := make(chan error, 4)
	for range 4 { // SLEEP forces four distinct pooled connections
		go func() {
			r, err := s.Exec(context.Background(), "SELECT @webtuidb_boot, SLEEP(0.3)", 0)
			if err == nil && r.Rows[0][0] != int64(42) {
				err = fmt.Errorf("bootstrap not applied: %v", r.Rows)
			}
			done <- err
		}()
	}
	for range 4 {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
}

func TestIntegrationCatalog(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	objs, err := s.Objects(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]db.Kind{}
	detail := map[string]string{}
	for _, o := range objs {
		kinds[o.Name], detail[o.Name] = o.Kind, o.Detail
	}
	for name, k := range map[string]db.Kind{"customers": db.KindTable, "orders": db.KindTable, "devices": db.KindTable, "prices": db.KindTable,
		"paid_orders": db.KindView, "customer_order_count": db.KindFunction, "touch_audit": db.KindProcedure, "invoice_seq": db.KindSequence} {
		if kinds[name] != k {
			t.Errorf("object %s: kind %q want %q", name, kinds[name], k)
		}
	}
	if detail["invoice_seq"] != "" {
		t.Errorf("a sequence has no row estimate, got %q", detail["invoice_seq"])
	}

	dev := tableDef(t, s, "devices")
	for col, want := range map[string]string{"id": "uuid", "ip4": "inet4", "ip6": "inet6", "doc": "json"} {
		if got := column(dev, col).Type; got != want {
			t.Errorf("devices.%s type %q want %q", col, got, want)
		}
	}
	if d := column(dev, "id"); d.Default != "uuid()" || !d.HasDef || !d.PK || d.Nullable {
		t.Errorf("uuid column %+v", d)
	}
	if d := column(dev, "label"); d.Default != "'it''s new'" || !d.Nullable {
		t.Errorf("quoted default %+v", d)
	}
	if d := column(dev, "seen_at"); d.Default != "current_timestamp()" {
		t.Errorf("current_timestamp default %+v", d)
	}
	if d := column(dev, "seq_no"); !strings.HasPrefix(d.Default, "nextval(") {
		t.Errorf("nextval default %+v", d)
	}
	if d := column(dev, "secret"); d.Extra != "INVISIBLE" || d.HasDef || d.Default != "" {
		t.Errorf("invisible column (default NULL is no default) %+v", d)
	}
	if m := column(tableDef(t, s, "customers"), "meta"); m.Type != "json" || !m.Nullable {
		t.Errorf("JSON alias reads back as %+v", m)
	}
	if got := column(tableDef(t, s, "customers"), "balance").Default; got != "0.00" {
		t.Errorf("decimal default %q", got)
	}

	ix, err := s.Indexes(ctx, ref("orders"))
	if err != nil || len(ix) < 3 || !ix[0].Primary {
		t.Errorf("indexes %+v %v", ix, err)
	}
	fks, _ := s.ForeignKeys(ctx, ref("orders"))
	if len(fks) != 1 || fks[0].OnDelete != "CASCADE" || fks[0].RefTable != "customers" {
		t.Errorf("fks %+v", fks)
	}
	cols, _ := s.Columns(ctx, ref("orders"))
	if cols[1].FK == nil {
		t.Errorf("customer_id has no FK: %+v", cols[1])
	}
	trg, err := s.Triggers(ctx, ref("orders"))
	if err != nil || len(trg) != 1 || trg[0].Name != "orders_audit" || trg[0].Timing != "AFTER" || trg[0].Event != "INSERT" {
		t.Errorf("triggers %+v %v", trg, err)
	}

	for _, c := range []struct {
		ref  db.ObjRef
		want string
	}{
		{ref("devices"), "`id` uuid NOT NULL DEFAULT uuid()"},
		{ref("prices"), "WITH SYSTEM VERSIONING"},
		{db.ObjRef{Name: "invoice_seq", Kind: db.KindSequence}, "CREATE SEQUENCE `invoice_seq` start with 1000"},
		{db.ObjRef{Name: "paid_orders", Kind: db.KindView}, "VIEW `paid_orders`"},
		{db.ObjRef{Name: "customer_order_count", Kind: db.KindFunction}, "FUNCTION `customer_order_count`"},
		{db.ObjRef{Name: "touch_audit", Kind: db.KindProcedure}, "PROCEDURE `touch_audit`"},
	} {
		if ddl, err := s.DDL(ctx, c.ref); err != nil || !strings.Contains(ddl, c.want) || !strings.HasSuffix(ddl, ";") {
			t.Errorf("DDL %s: %q %v", c.ref.Name, ddl, err)
		}
	}
	if _, err := s.DDL(ctx, db.ObjRef{Name: "nope", Kind: db.KindSequence}); err == nil {
		t.Error("DDL of a missing sequence succeeded")
	}
}

func TestIntegrationScripts(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	seq := db.ObjRef{Name: "invoice_seq", Kind: db.KindSequence}
	for _, c := range []struct {
		kind db.ScriptKind
		ref  db.ObjRef
		want string
	}{
		{db.ScriptDrop, seq, "DROP SEQUENCE `invoice_seq`;"},
		{db.ScriptSelectTop, seq, "SELECT * FROM `invoice_seq` LIMIT 100;"},
		{db.ScriptDrop, ref("customers"), "DROP TABLE `customers`;"},
		{db.ScriptTruncate, ref("customers"), "TRUNCATE TABLE `customers`;"},
	} {
		if got, err := s.Script(ctx, c.kind, c.ref); err != nil || got != c.want {
			t.Errorf("%s %s: %q %v", c.kind, c.ref.Name, got, err)
		}
	}
	if got, err := s.Script(ctx, db.ScriptCreate, seq); err != nil || !strings.HasPrefix(got, "CREATE SEQUENCE `invoice_seq`") {
		t.Errorf("create sequence: %q %v", got, err)
	}
	// the system-versioned table was listed above, so TRUNCATE is replaced by what works
	if _, err := s.Objects(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Script(ctx, db.ScriptTruncate, ref("prices")); got != "DELETE FROM `prices`;\nDELETE HISTORY FROM `prices`;" {
		t.Errorf("truncate versioned: %q", got)
	}
}

func TestIntegrationRowsAndCount(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	r, err := s.Rows(ctx, db.RowQuery{ObjRef: ref("devices"), Sort: []db.Sort{{Column: "seq_no"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 2 || len(r.Columns) != 7 { // the INVISIBLE column is not part of SELECT *
		t.Fatalf("devices rows %d cols %d", len(r.Rows), len(r.Columns))
	}
	row := r.Rows[0]
	if id, _ := row[0].(string); len(id) != 36 || row[1] != "10.0.0.1" || row[2] != "::1" || row[6] != int64(1000) {
		t.Errorf("devices row %#v", row)
	}
	if row[5] != `{"os": "linux"}` || r.Rows[1][5] != nil {
		t.Errorf("json column %#v / %#v", row[5], r.Rows[1][5])
	}
	// "Any column" filters cast every column to text, including UUID and INET6
	fe80 := db.FilterSet{Filters: []db.Filter{{Column: db.AnyColumn, Op: "Contains", Value: "fe80", Enabled: true}}}
	rf, err := s.Rows(ctx, db.RowQuery{ObjRef: ref("devices"), Filters: fe80})
	if err != nil || len(rf.Rows) != 1 || rf.Rows[0][1] != "192.168.1.7" {
		t.Errorf("any-column filter: %v %v", rf, err)
	}
	if c, err := s.Count(ctx, db.RowQuery{ObjRef: ref("devices"), Filters: fe80}, true); err != nil || c.N != 1 || !c.Exact {
		t.Errorf("filtered count %+v %v", c, err)
	}
	if c, err := s.Count(ctx, db.RowQuery{ObjRef: ref("customers")}, true); err != nil || c.N != 3 {
		t.Errorf("estimate count %+v %v", c, err)
	}
	if c, err := s.Count(ctx, db.RowQuery{ObjRef: ref("prices")}, false); err != nil || c.N != 2 || !c.Exact {
		t.Errorf("versioned count (current rows only) %+v %v", c, err)
	}
	// a sequence is browsable like a one-row table
	rs, err := s.Rows(ctx, db.RowQuery{ObjRef: db.ObjRef{Name: "invoice_seq", Kind: db.KindSequence}})
	if err != nil || len(rs.Rows) != 1 || rs.Columns[0].Name != "next_not_cached_value" {
		t.Errorf("sequence rows: %+v %v", rs, err)
	}
}

func TestIntegrationExec(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	seq := temp(t, s, "SEQUENCE")
	exec(t, s, "CREATE SEQUENCE "+q(seq)+" START WITH 10 INCREMENT BY 5 CACHE 1")
	if scalar(t, s, "SELECT NEXTVAL("+q(seq)+")") != int64(10) || scalar(t, s, "SELECT NEXT VALUE FOR "+q(seq)) != int64(15) {
		t.Error("sequence values")
	}
	if scalar(t, s, "SELECT LASTVAL("+q(seq)+")") != int64(15) {
		t.Error("lastval")
	}

	tbl := temp(t, s, "TABLE")
	exec(t, s, "CREATE TABLE "+q(tbl)+" (id INT AUTO_INCREMENT PRIMARY KEY, u UUID DEFAULT UUID(), ip INET6, v VARCHAR(10))")
	r := exec(t, s, "INSERT INTO "+q(tbl)+" (ip, v) VALUES ('::1', 'a'), ('2001:db8::2', 'b') RETURNING id, ip, v")
	if !r.HasRows || len(r.Rows) != 2 || r.Rows[1][1] != "2001:db8::2" || r.Rows[0][2] != "a" {
		t.Errorf("insert returning: %+v", r)
	}
	r = exec(t, s, "DELETE FROM "+q(tbl)+" WHERE v = 'a' RETURNING v")
	if len(r.Rows) != 1 || r.Rows[0][0] != "a" {
		t.Errorf("delete returning: %+v", r)
	}
	r = exec(t, s, "SELECT u FROM "+q(tbl))
	if u, ok := r.Rows[0][0].(string); !ok || len(u) != 36 || r.Columns[0].Type == "" {
		t.Errorf("uuid value %#v (%s)", r.Rows[0][0], r.Columns[0].Type)
	}
	r = exec(t, s, "UPDATE "+q(tbl)+" SET v = 'z'")
	if r.HasRows || r.Affected != 1 || r.Message != "1 row affected" {
		t.Errorf("update: %+v", r)
	}
	if _, err := s.Exec(ctx, "INSERT INTO "+q(tbl)+" (ip) VALUES ('not an ip')", 0); err == nil {
		t.Error("bad INET6 accepted")
	}
	if _, err := s.Exec(ctx, "SELEKT 1", 0); err == nil {
		t.Error("syntax error expected")
	}
	r, err := s.Exec(ctx, "SELECT seq FROM seq_1_to_50", 7)
	if err != nil || len(r.Rows) != 7 || !r.Truncated {
		t.Errorf("limit on the SEQUENCE engine: %+v %v", r, err)
	}
	if r = exec(t, s, "SELECT 1"); r.Rows[0][0] != int64(1) {
		t.Errorf("after truncate: %v", r.Rows)
	}

	// JSON is LONGTEXT with a CHECK: invalid documents are refused, valid ones round-trip as text
	exec(t, s, "ALTER TABLE "+q(tbl)+" ADD COLUMN j JSON")
	if _, err := s.Exec(ctx, "INSERT INTO "+q(tbl)+" (j) VALUES ('{bad')", 0); err == nil {
		t.Error("invalid JSON accepted")
	}
	exec(t, s, "UPDATE "+q(tbl)+" SET j = '{\"a\": [1, 2]}'")
	if got := scalar(t, s, "SELECT JSON_VALUE(j, '$.a[1]') FROM "+q(tbl)); got != "2" {
		t.Errorf("json_value %#v", got)
	}
}

func TestIntegrationCancel(t *testing.T) {
	s := open(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	start := time.Now()
	if _, err := s.Exec(ctx, "SELECT SLEEP(5)", 0); err == nil || time.Since(start) > 1500*time.Millisecond {
		t.Fatalf("cancel: err=%v after %v", err, time.Since(start))
	}
	for range 6 {
		if r, err := s.Exec(context.Background(), "SELECT 1", 0); err != nil || r.Rows[0][0] != int64(1) {
			t.Fatalf("after cancel: %v %v", r, err)
		}
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if _, err := s.Rows(ctx, db.RowQuery{ObjRef: ref("customers")}); err == nil {
		t.Error("cancelled Rows succeeded")
	}
}

func TestIntegrationProcesses(t *testing.T) {
	s := open(t)
	pl, ok := s.(db.ProcessLister)
	if !ok {
		t.Fatal("no process list")
	}
	// MariaDB's SHOW FULL PROCESSLIST has a ninth column (Progress)
	ps, err := pl.Processes(context.Background())
	if err != nil || len(ps) == 0 {
		t.Fatalf("processes %v %v", ps, err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.Exec(context.Background(), "SELECT SLEEP(10) /* webtuidb-kill-me */", 0)
		done <- err
	}()
	var id string
	for i := 0; i < 40 && id == ""; i++ {
		time.Sleep(100 * time.Millisecond)
		ps, _ = pl.Processes(context.Background())
		for _, p := range ps {
			if strings.Contains(p.Query, "webtuidb-kill-me") && p.User == "webtuidb" {
				id = p.ID
			}
		}
	}
	if id == "" {
		t.Fatal("sleeping query not listed")
	}
	if err := pl.Kill(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("killed query still running")
	}
	if err := pl.Kill(context.Background(), "1; DROP TABLE x"); err == nil {
		t.Error("bad id accepted")
	}
}

func TestIntegrationPlanApplyRows(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	tbl := temp(t, s, "TABLE")
	exec(t, s, "CREATE TABLE "+q(tbl)+" (id UUID PRIMARY KEY DEFAULT UUID(), name VARCHAR(20), ip INET6, amount DECIMAL(10,2) CHECK (amount >= 0), n INT DEFAULT 5, j JSON)")
	o := ref(tbl)
	plan := func(changes ...db.Change) {
		t.Helper()
		st, err := s.Plan(changes)
		apply(t, s, st, err)
	}
	// the uuid key comes from its default expression
	plan(db.Change{Kind: db.Insert, ObjRef: o, Set: []db.KeyVal{{Col: "id", Val: db.Default}, {Col: "name", Val: "it's"}, {Col: "ip", Val: "fe80::1"},
		{Col: "amount", Val: "12.50"}, {Col: "n", Val: db.Default}, {Col: "j", Val: `{"k": "v"}`}}})
	id := scalar(t, s, "SELECT id FROM "+q(tbl)).(string)
	plan(db.Change{Kind: db.Update, ObjRef: o, Key: []db.KeyVal{{Col: "id", Val: id}}, Set: []db.KeyVal{{Col: "name", Val: db.Empty}, {Col: "ip", Val: "::2"}, {Col: "amount", Val: db.Null}}})
	r := exec(t, s, "SELECT name, ip, amount, n, j FROM "+q(tbl))
	if r.Rows[0][0] != "" || r.Rows[0][1] != "::2" || r.Rows[0][2] != nil || r.Rows[0][3] != int64(5) || r.Rows[0][4] != `{"k": "v"}` {
		t.Errorf("after update: %#v", r.Rows)
	}
	// a CHECK failure in the second statement rolls the first one back
	st, err := s.Plan([]db.Change{
		{Kind: db.Update, ObjRef: o, Key: []db.KeyVal{{Col: "id", Val: id}}, Set: []db.KeyVal{{Col: "name", Val: "changed"}}},
		{Kind: db.Update, ObjRef: o, Key: []db.KeyVal{{Col: "id", Val: id}}, Set: []db.KeyVal{{Col: "amount", Val: "-1"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, st); err == nil || !strings.Contains(err.Error(), "statement 2") {
		t.Fatalf("expected a constraint error on statement 2, got %v", err)
	}
	if got := scalar(t, s, "SELECT name FROM "+q(tbl)); got != "" {
		t.Errorf("first statement was not rolled back: %q", got)
	}
	plan(db.Change{Kind: db.Delete, ObjRef: o, Key: []db.KeyVal{{Col: "id", Val: id}}})
	if scalar(t, s, "SELECT COUNT(*) FROM "+q(tbl)) != int64(0) {
		t.Error("delete failed")
	}

	// a table without a primary key: the whole old row identifies it
	npk := temp(t, s, "TABLE")
	exec(t, s, "CREATE TABLE "+q(npk)+" (a INT, b VARCHAR(5))")
	exec(t, s, "INSERT INTO "+q(npk)+" VALUES (1, 'x'), (1, NULL), (2, 'y')")
	plan(db.Change{Kind: db.Update, ObjRef: ref(npk), Key: []db.KeyVal{{Col: "a", Val: "1"}, {Col: "b", Val: db.Null}}, Set: []db.KeyVal{{Col: "b", Val: "n"}}})
	if got := scalar(t, s, "SELECT GROUP_CONCAT(IFNULL(b, '-') ORDER BY b) FROM "+q(npk)); got != "n,x,y" {
		t.Errorf("no-pk update: %v", got)
	}
}

// System-versioned tables: row edits keep history, structure edits need history kept, TRUNCATE is refused by the server.
func TestIntegrationSystemVersioned(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	tbl := temp(t, s, "TABLE")
	exec(t, s, "CREATE TABLE "+q(tbl)+" (id INT PRIMARY KEY, item VARCHAR(30) NOT NULL, price DECIMAL(10,2) NOT NULL) WITH SYSTEM VERSIONING")
	exec(t, s, "INSERT INTO "+q(tbl)+" VALUES (1, 'pen', 1.50), (2, 'ink', 7.25)")
	o := ref(tbl)
	st, err := s.Plan([]db.Change{{Kind: db.Update, ObjRef: o, Key: []db.KeyVal{{Col: "id", Val: "1"}}, Set: []db.KeyVal{{Col: "price", Val: "1.75"}}},
		{Kind: db.Delete, ObjRef: o, Key: []db.KeyVal{{Col: "id", Val: "2"}}}})
	apply(t, s, st, err)
	hist := func() int64 { return scalar(t, s, "SELECT COUNT(*) FROM "+q(tbl)+" FOR SYSTEM_TIME ALL").(int64) }
	if scalar(t, s, "SELECT COUNT(*) FROM "+q(tbl)) != int64(1) || hist() != 3 {
		t.Fatalf("row edits: current %v history %d", scalar(t, s, "SELECT COUNT(*) FROM "+q(tbl)), hist())
	}

	// the sidebar load tells the session which tables are versioned
	objs, err := s.Objects(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, ob := range objs {
		found = found || ob.Name == tbl && ob.Kind == db.KindTable
	}
	if !found {
		t.Fatalf("versioned table not listed as a table")
	}

	old := tableDef(t, s, tbl)
	if got := len(old.Columns); got != 3 { // row_start/row_end are hidden
		t.Fatalf("columns %+v", old.Columns)
	}
	if st, err := s.PlanTable(old, tableDef(t, s, tbl)); err != nil || len(st) != 0 {
		t.Errorf("self diff: %v %v", st, err)
	}
	next := tableDef(t, s, tbl)
	next.Columns[1].OrigName, next.Columns[1].Name = "item", "title"
	next.Columns[2].Type = "decimal(12,3)"
	next.Columns = append(next.Columns, db.Column{Name: "note", Type: "varchar(10)", Nullable: true})
	next.Indexes = append(next.Indexes, db.Index{Name: "title_idx", Columns: []string{"title"}})
	st, err = s.PlanTable(old, next)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(st[0].Text, "SET STATEMENT system_versioning_alter_history=KEEP FOR ALTER TABLE") {
		t.Errorf("statement %q", st[0].Text)
	}
	apply(t, s, st, nil)
	if got := tableDef(t, s, tbl); got.Columns[1].Name != "title" || got.Columns[2].Type != "decimal(12,3)" || len(got.Columns) != 4 || len(got.Indexes) != 2 || hist() != 3 {
		t.Errorf("after alter: %+v, history %d", got.Columns, hist())
	}
	drop := tableDef(t, s, tbl)
	drop.Columns = drop.Columns[:3]
	st, _ = s.PlanTable(tableDef(t, s, tbl), drop)
	apply(t, s, st, nil)
	if len(tableDef(t, s, tbl).Columns) != 3 {
		t.Error("drop column failed")
	}

	// clone (with data: current rows), rename, then empty including history
	clone, moved := tbl+"_c", tbl+"_m"
	t.Cleanup(func() {
		s.Exec(ctx, "DROP TABLE IF EXISTS "+q(clone), 0)
		s.Exec(ctx, "DROP TABLE IF EXISTS "+q(moved), 0)
	})
	run := func(op db.AdminOp, r db.ObjRef, opts map[string]string) {
		t.Helper()
		st, err := s.PlanAdmin(op, r, opts)
		apply(t, s, st, err)
	}
	run(db.OpClone, o, map[string]string{"new_name": clone, "with_data": "true"})
	if scalar(t, s, "SELECT COUNT(*) FROM "+q(clone)) != int64(1) {
		t.Error("clone lost rows")
	}
	run(db.OpRenameObject, ref(clone), map[string]string{"new_name": moved})
	if scalar(t, s, "SELECT COUNT(*) FROM "+q(moved)) != int64(1) {
		t.Error("rename lost rows")
	}
	if _, err := s.Exec(ctx, "TRUNCATE TABLE "+q(tbl), 0); err == nil {
		t.Skip("this server can truncate system-versioned tables; the workaround is not needed")
	}
	run(db.OpTruncate, o, nil)
	if scalar(t, s, "SELECT COUNT(*) FROM "+q(tbl)) != int64(0) || hist() != 0 {
		t.Errorf("truncate left rows: current %v history %d", scalar(t, s, "SELECT COUNT(*) FROM "+q(tbl)), hist())
	}
	run(db.OpDropObject, o, nil)
}

func TestIntegrationPlanTableMariaDB(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	seq := temp(t, s, "SEQUENCE")
	exec(t, s, "CREATE SEQUENCE "+q(seq))
	tbl := temp(t, s, "TABLE")
	create := &db.TableDef{Name: tbl,
		Columns: []db.Column{
			{Name: "id", Type: "uuid", Default: "uuid()"},
			{Name: "doc", Type: "json", Nullable: true},
			{Name: "ip", Type: "inet6", Nullable: true},
			{Name: "n", Type: "bigint", Nullable: true, Default: "nextval(" + q(seq) + ")"},
			{Name: "hidden", Type: "varchar(10)", Nullable: true, Default: "'x'", Extra: "INVISIBLE"},
			{Name: "stamp", Type: "timestamp", Default: "current_timestamp()", Extra: "on update current_timestamp()"},
			{Name: "label", Type: "varchar(10)", Nullable: true, Default: "'it''s'"},
			{Name: "g", Type: "int", Nullable: true, Extra: "GENERATED ALWAYS AS (length(`label`)) STORED"},
		},
		PrimaryKey: []string{"id"}}
	st, err := s.PlanTable(nil, create)
	apply(t, s, st, err)

	loaded := tableDef(t, s, tbl)
	if got := column(loaded, "doc"); got.Type != "json" {
		t.Errorf("json column reads back as %q", got.Type)
	}
	if got := column(loaded, "hidden"); got.Extra != "INVISIBLE" || got.Default != "'x'" {
		t.Errorf("invisible column %+v", got)
	}
	if got := column(loaded, "g"); !strings.HasPrefix(got.Extra, "GENERATED ALWAYS AS (") || !strings.HasSuffix(got.Extra, "STORED") {
		t.Errorf("generated column %+v", got)
	}
	if st, err := s.PlanTable(loaded, tableDef(t, s, tbl)); err != nil || len(st) != 0 {
		t.Fatalf("self diff: %v %v", st, err)
	}

	// edit every kind of column; the JSON column must keep refusing bad documents
	next := tableDef(t, s, tbl)
	col := func(name string) *db.Column {
		for i := range next.Columns {
			if next.Columns[i].Name == name {
				return &next.Columns[i]
			}
		}
		return nil
	}
	col("doc").Nullable = false
	col("doc").Default = "'{}'"
	col("ip").Type = "inet4"
	col("n").Nullable = false
	col("n").Default = "5"
	col("hidden").Type = "varchar(40)"
	col("label").Default = ""
	col("label").OrigName, col("label").Name = "label", "caption"
	col("g").Extra = "GENERATED ALWAYS AS (length(`caption`) + 1) STORED"
	st, err = s.PlanTable(loaded, next)
	apply(t, s, st, err)
	got := tableDef(t, s, tbl)
	if d := column(got, "doc"); d.Type != "json" || d.Nullable || d.Default != "'{}'" {
		t.Errorf("doc %+v", d)
	}
	if d := column(got, "ip"); d.Type != "inet4" {
		t.Errorf("ip %+v", d)
	}
	if d := column(got, "hidden"); d.Extra != "INVISIBLE" || d.Type != "varchar(40)" {
		t.Errorf("hidden %+v", d)
	}
	if d := column(got, "caption"); d.HasDef || d.Default != "" {
		t.Errorf("caption %+v", d)
	}
	if _, err := s.Exec(ctx, "INSERT INTO "+q(tbl)+" (doc) VALUES ('{bad')", 0); err == nil {
		t.Error("the JSON check was lost by the structure edit")
	}
	exec(t, s, "INSERT INTO "+q(tbl)+" (doc, ip, caption) VALUES ('{\"a\":1}', '10.1.2.3', 'abc')")
	if r := exec(t, s, "SELECT g, n FROM "+q(tbl)); r.Rows[0][0] != int64(4) || r.Rows[0][1] != int64(5) {
		t.Errorf("generated/default values %v", r.Rows)
	}
	if st, err := s.PlanTable(got, tableDef(t, s, tbl)); err != nil || len(st) != 0 {
		t.Errorf("self diff after edit: %v %v", st, err)
	}

	// a default that is an expression over a sequence survives a retype of that column
	again := tableDef(t, s, tbl)
	for i := range again.Columns {
		if again.Columns[i].Name == "n" {
			again.Columns[i].Default = "nextval(" + q(seq) + ")"
			again.Columns[i].Type = "int"
		}
	}
	st, err = s.PlanTable(got, again)
	apply(t, s, st, err)
	exec(t, s, "INSERT INTO "+q(tbl)+" (caption) VALUES ('q')")
	if got := scalar(t, s, "SELECT MIN(n) FROM "+q(tbl)); got != int64(1) {
		t.Errorf("nextval default: %v", got)
	}
}

func TestIntegrationPlanAdminSequences(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	name := temp(t, s, "SEQUENCE")
	moved := name + "_m"
	t.Cleanup(func() { s.Exec(ctx, "DROP SEQUENCE IF EXISTS "+q(moved), 0) })
	exec(t, s, "CREATE SEQUENCE "+q(name)+" START WITH 7")
	seq := db.ObjRef{Name: name, Kind: db.KindSequence}
	run := func(op db.AdminOp, r db.ObjRef, opts map[string]string) {
		t.Helper()
		st, err := s.PlanAdmin(op, r, opts)
		apply(t, s, st, err)
	}
	run(db.OpRenameObject, seq, map[string]string{"new_name": moved})
	seq.Name = moved
	if scalar(t, s, "SELECT NEXTVAL("+q(moved)+")") != int64(7) {
		t.Error("renamed sequence lost its state")
	}
	has := func() bool {
		objs, _ := s.Objects(ctx, "")
		for _, o := range objs {
			if o.Name == moved && o.Kind == db.KindSequence {
				return true
			}
		}
		return false
	}
	if !has() {
		t.Fatal("sequence not listed")
	}
	run(db.OpDropObject, seq, nil)
	if has() {
		t.Error("sequence still listed after drop")
	}
	if _, err := s.PlanAdmin(db.OpTruncate, seq, nil); err != nil {
		t.Logf("truncate of a sequence is refused by the server, planned as %v", err)
	}
}

func TestIntegrationPlanAdminTablesAndDatabases(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	tbl := temp(t, s, "TABLE")
	exec(t, s, "CREATE TABLE "+q(tbl)+" (id INT PRIMARY KEY AUTO_INCREMENT, v VARCHAR(5))")
	exec(t, s, "INSERT INTO "+q(tbl)+" (v) VALUES ('a'),('b')")
	run := func(op db.AdminOp, r db.ObjRef, opts map[string]string) {
		t.Helper()
		st, err := s.PlanAdmin(op, r, opts)
		apply(t, s, st, err)
	}
	clone := tbl + "_c"
	t.Cleanup(func() { s.Exec(ctx, "DROP TABLE IF EXISTS "+q(clone), 0) })
	run(db.OpClone, ref(tbl), map[string]string{"new_name": clone, "with_data": "true"})
	if scalar(t, s, "SELECT COUNT(*) FROM "+q(clone)) != int64(2) {
		t.Error("clone with data lost rows")
	}
	run(db.OpTruncate, ref(tbl), nil)
	if scalar(t, s, "SELECT COUNT(*) FROM "+q(tbl)) != int64(0) {
		t.Error("truncate left rows")
	}
	run(db.OpDropObject, ref(tbl), nil)
	if _, err := s.Exec(ctx, "SELECT 1 FROM "+q(tbl), 0); err == nil {
		t.Error("table still exists")
	}

	name := fmt.Sprintf("webtuidb_test_%x", rand.Uint32())
	t.Cleanup(func() { s.Exec(ctx, "DROP DATABASE IF EXISTS "+q(name), 0) })
	run(db.OpCreateDatabase, db.ObjRef{Name: name}, map[string]string{"encoding": "utf8mb4", "collation": "utf8mb4_unicode_ci"})
	if dbs, _ := s.Databases(ctx); !contains(dbs, name) {
		t.Fatal("new database not listed")
	}
	if err := s.UseDatabase(ctx, name); err != nil || s.CurrentDatabase() != name {
		t.Fatalf("use database: %v, current %q", err, s.CurrentDatabase())
	}
	exec(t, s, "CREATE TABLE only_here (a INT)")
	exec(t, s, "CREATE SEQUENCE seq_here")
	objs, _ := s.Objects(ctx, "")
	if len(objs) != 2 {
		t.Errorf("objects of the new database %+v", objs)
	}
	if err := s.UseDatabase(ctx, "webtuidb_does_not_exist"); err == nil || s.CurrentDatabase() != name {
		t.Errorf("bad database: %v, current %q", err, s.CurrentDatabase())
	}
	if err := s.UseDatabase(ctx, "webtuidb"); err != nil {
		t.Fatal(err)
	}
	run(db.OpDropDatabase, db.ObjRef{Name: name}, nil)
	if _, err := s.PlanAdmin(db.OpRenameDatabase, db.ObjRef{Name: "x"}, map[string]string{"new_name": "y"}); err == nil || !strings.Contains(err.Error(), "MariaDB") {
		t.Errorf("rename database should explain itself: %v", err)
	}
}

func TestIntegrationApplyDDLStopsAndReports(t *testing.T) {
	s := open(t)
	tbl := temp(t, s, "TABLE")
	err := s.Apply(context.Background(), []db.Statement{
		{Text: "CREATE TABLE " + q(tbl) + " (id INT)"},
		{Text: "ALTER TABLE " + q(tbl) + " ADD COLUMN nope NOT_A_TYPE"},
		{Text: "ALTER TABLE " + q(tbl) + " ADD COLUMN never INT"},
	})
	if err == nil || !strings.Contains(err.Error(), "statement 2") || !strings.Contains(err.Error(), "1 of 3 statements were already applied") || !strings.Contains(err.Error(), "MariaDB cannot roll back DDL") {
		t.Fatalf("error %v", err)
	}
	if cols := tableDef(t, s, tbl).Columns; len(cols) != 1 {
		t.Errorf("columns after partial batch %+v", cols)
	}
}
