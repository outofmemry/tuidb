package dynamodb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func testConn(t *testing.T) config.Connection {
	t.Helper()
	if os.Getenv("WEBTUIDB_INTEGRATION") != "1" {
		t.Skip("set WEBTUIDB_INTEGRATION=1 to run against the docker databases")
	}
	url := os.Getenv("WEBTUIDB_TEST_URL_DYNAMODB")
	if url == "" {
		url = "dynamodb://webtuidb:webtuidb@127.0.0.1:58000/?region=us-east-1"
	}
	c, err := config.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var seedOnce sync.Once

func open(t *testing.T) *session {
	t.Helper()
	c := testConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	d, err := drv{}.Open(ctx, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := d.(*session)
	t.Cleanup(func() { s.Close() })
	seedOnce.Do(func() { seed(t, s) })
	return s
}

// seed (re)creates the tables of testdata/seed/dynamodb.json.
func seed(t *testing.T, s *session) {
	t.Helper()
	raw, err := os.ReadFile("../../../testdata/seed/dynamodb.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Tables []struct {
			Name  string
			Spec  tableSpec
			Items []json.RawMessage
		}
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, tb := range f.Tables {
		if _, err := s.runAdmin(ctx, &adminStmt{op: "drop", table: tb.Name}, false); err != nil && !strings.Contains(err.Error(), "ResourceNotFound") {
			t.Fatal(err)
		}
		if _, err := s.runAdmin(ctx, &adminStmt{op: "create", table: tb.Name, create: tb.Spec}, false); err != nil {
			t.Fatal(err)
		}
		var reqs []types.WriteRequest
		for _, it := range tb.Items {
			j, err := parseJSON(string(it))
			if err != nil {
				t.Fatal(err)
			}
			av, err := fromJSON(j)
			if err != nil {
				t.Fatal(err)
			}
			reqs = append(reqs, types.WriteRequest{PutRequest: &types.PutRequest{Item: av.(*types.AttributeValueMemberM).Value}})
		}
		if err := s.batchWrite(ctx, tb.Name, reqs); err != nil {
			t.Fatal(err)
		}
	}
}

func exec(t *testing.T, s *session, stmt string) *db.Result {
	t.Helper()
	r, err := s.Exec(context.Background(), stmt, 0)
	if err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
	return r
}

// tempTable creates a table (keys: id S [, sk N]) that is dropped when the test ends.
func tempTable(t *testing.T, s *session, sortKey bool) string {
	t.Helper()
	name := fmt.Sprintf("webtuidb_test_%x", rand.Uint32())
	sp := tableSpec{Keys: []keySpec{{Name: "id", Type: "S"}}}
	if sortKey {
		sp.Keys = append(sp.Keys, keySpec{Name: "sk", Type: "N"})
	}
	exec(t, s, createText(name, sp))
	t.Cleanup(func() { s.Exec(context.Background(), "DROP TABLE "+quote(name), 0) })
	return name
}

func apply(t *testing.T, s *session, changes ...db.Change) {
	t.Helper()
	stmts, err := s.Plan(changes)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(context.Background(), stmts); err != nil {
		t.Fatalf("apply %v: %v", stmts, err)
	}
}

func rows(t *testing.T, s *session, q db.RowQuery) *db.Result {
	t.Helper()
	r, err := s.Rows(context.Background(), q)
	if err != nil {
		t.Fatalf("rows %+v: %v", q, err)
	}
	return r
}

// col returns one column of a result as text, in row order.
func col(r *db.Result, name string) []string {
	for i, c := range r.Columns {
		if c.Name == name {
			var out []string
			for _, row := range r.Rows {
				out = append(out, db.FormatValue(row[i]))
			}
			return out
		}
	}
	return nil
}

func eq(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func flt(col, op, val string) db.FilterSet {
	return db.FilterSet{Filters: []db.Filter{{Column: col, Op: op, Value: val, Enabled: true}}}
}

func ref(name string) db.ObjRef { return db.ObjRef{Name: name, Kind: db.KindTable} }

func TestOpenAndInfo(t *testing.T) {
	s := open(t)
	if !strings.Contains(s.ServerInfo(), "DynamoDB") {
		t.Errorf("info %q", s.ServerInfo())
	}
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := s.Caps()
	if !c.Transactions || !c.Document || c.Editor != "sql" || c.Dialect != "partiql" || c.DatabaseNoun != "region" || c.RenameDB || c.Triggers || c.ForeignKeys || c.Schemas {
		t.Errorf("caps %+v", c)
	}
	if _, ok := any(s).(db.ProcessLister); ok {
		t.Error("DynamoDB has no process list")
	}
}

func TestOpenErrors(t *testing.T) {
	c := testConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bad := func(mod func(*config.Connection), want string) {
		t.Helper()
		cc := c
		cc.Options = map[string]string{}
		mod(&cc)
		_, err := drv{}.Open(ctx, cc, nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err %v, want %q", err, want)
		}
	}
	bad(func(c *config.Connection) { c.Options["region"] = "mars" }, "not an AWS region")
	bad(func(c *config.Connection) { c.Password = "" }, "both the access key")
	bad(func(c *config.Connection) { c.Options["endpoint"] = "ftp://x" }, "want http(s)://")
	bad(func(c *config.Connection) { c.Port = 1; c.Options["region"] = "us-east-1" }, "ping")
	bad(func(c *config.Connection) { c.SSLMode = "bogus" }, "ssl_mode")
}

func TestRegions(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	dbs, err := s.Databases(ctx)
	if err != nil || !contains(dbs, "us-east-1") || !contains(dbs, "eu-west-1") {
		t.Fatalf("databases %v %v", dbs, err)
	}
	if s.CurrentDatabase() != "us-east-1" {
		t.Errorf("current %q", s.CurrentDatabase())
	}
	if err := s.UseDatabase(ctx, "eu-west-1"); err != nil || s.CurrentDatabase() != "eu-west-1" {
		t.Fatalf("use: %v %q", err, s.CurrentDatabase())
	}
	if err := s.UseDatabase(ctx, "nowhere"); err == nil {
		t.Error("bad region accepted")
	}
	if s.CurrentDatabase() != "eu-west-1" {
		t.Error("a failed switch changed the region")
	}
	if err := s.UseDatabase(ctx, "us-east-1"); err != nil {
		t.Fatal(err)
	}
	for _, op := range []db.AdminOp{db.OpCreateDatabase, db.OpDropDatabase, db.OpRenameDatabase} {
		if _, err := s.PlanAdmin(op, db.ObjRef{Name: "us-east-1"}, nil); err == nil {
			t.Errorf("%s accepted", op)
		}
	}
}

func TestObjectsColumnsIndexes(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	objs, err := s.Objects(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, o := range objs {
		if o.Kind != db.KindTable {
			t.Errorf("object %v is not a table", o)
		}
		found[o.Name] = o.Detail
	}
	if d, ok := found["webtuidb_users"]; !ok || !strings.Contains(d, "items") {
		t.Errorf("objects %v", found)
	}

	cols, err := s.Columns(ctx, ref("webtuidb_users"))
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]db.Column{}
	var names []string
	for _, c := range cols {
		by[c.Name] = c
		names = append(names, c.Name)
	}
	if names[0] != "id" || !by["id"].PK || by["id"].Type != "N" || by["id"].Extra != "partition key" || by["id"].Nullable {
		t.Errorf("id column %+v", by["id"])
	}
	for n, typ := range map[string]string{"name": "S", "age": "N", "active": "BOOL", "tags": "SS", "scores": "NS", "profile": "M", "history": "L", "avatar": "B", "nickname": "NULL", "balance": "N"} {
		if by[n].Type != typ || (n != "id" && !by[n].Nullable) || by[n].PK {
			t.Errorf("%s column %+v, want type %s", n, by[n], typ)
		}
	}
	if !strings.Contains(by["email"].Extra, "by_email") {
		t.Errorf("email extra %q", by["email"].Extra)
	}

	ocols, err := s.Columns(ctx, ref("webtuidb_orders"))
	if err != nil {
		t.Fatal(err)
	}
	if ocols[0].Name != "customer" || ocols[1].Name != "ts" || ocols[1].Extra != "sort key" || !ocols[1].PK {
		t.Errorf("orders columns %+v", ocols)
	}
	ev, _ := s.Columns(ctx, ref("webtuidb_events"))
	for _, c := range ev {
		if c.Name == "kind" && c.Type != "mixed" {
			t.Errorf("kind holds S and N, shown as %q", c.Type)
		}
	}

	ix, err := s.Indexes(ctx, ref("webtuidb_users"))
	if err != nil || len(ix) != 2 || !ix[0].Primary || ix[1].Name != "by_email" || ix[1].Algorithm != "GSI" || ix[1].Condition != "ALL" {
		t.Errorf("users indexes %+v %v", ix, err)
	}
	ix, _ = s.Indexes(ctx, ref("webtuidb_orders"))
	if len(ix) != 2 || ix[1].Algorithm != "LSI" || ix[1].Condition != "KEYS_ONLY" || strings.Join(ix[1].Columns, ",") != "customer,status" {
		t.Errorf("orders indexes %+v", ix)
	}
	if _, err := s.Columns(ctx, ref("webtuidb_no_such")); err == nil || !strings.Contains(err.Error(), "ResourceNotFound") {
		t.Errorf("missing table: %v", err)
	}
	if tr, _ := s.Triggers(ctx, ref("webtuidb_users")); tr != nil {
		t.Error("triggers")
	}
	if fk, _ := s.ForeignKeys(ctx, ref("webtuidb_users")); fk != nil {
		t.Error("foreign keys")
	}
}

func TestDDLAndScripts(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	ddl, err := s.DDL(ctx, ref("webtuidb_orders"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"TableName": "webtuidb_orders"`, "KeySchema", "LocalSecondaryIndexes", "by_status", "BillingMode", "PAY_PER_REQUEST", "StreamSpecification", "TimeToLive"} {
		if !strings.Contains(ddl, want) {
			t.Errorf("ddl lacks %q:\n%s", want, ddl)
		}
	}
	if !json.Valid([]byte(ddl)) {
		t.Errorf("ddl is not JSON:\n%s", ddl)
	}
	create, err := s.Script(ctx, db.ScriptCreate, ref("webtuidb_orders"))
	if err != nil || !strings.HasPrefix(create, `CREATE TABLE "webtuidb_orders" {`) {
		t.Fatalf("create script %q %v", create, err)
	}
	// the generated CREATE script runs as is: it clones the structure
	clone := fmt.Sprintf("webtuidb_test_%x", rand.Uint32())
	exec(t, s, strings.Replace(create, `"webtuidb_orders"`, quote(clone), 1))
	t.Cleanup(func() { s.Exec(context.Background(), "DROP TABLE "+quote(clone), 0) })
	if ix, _ := s.Indexes(ctx, ref(clone)); len(ix) != 2 || ix[1].Algorithm != "LSI" {
		t.Errorf("clone indexes %+v", ix)
	}
	for kind, want := range map[db.ScriptKind]string{db.ScriptDrop: `DROP TABLE "webtuidb_orders"`, db.ScriptTruncate: `TRUNCATE TABLE "webtuidb_orders"`, db.ScriptSelectTop: `SELECT * FROM "webtuidb_orders"`} {
		if got, err := s.Script(ctx, kind, ref("webtuidb_orders")); err != nil || got != want {
			t.Errorf("script %s = %q %v", kind, got, err)
		}
	}
}

func TestRowsBasic(t *testing.T) {
	s := open(t)
	r := rows(t, s, db.RowQuery{ObjRef: ref("webtuidb_users"), Sort: []db.Sort{{Column: "id"}}})
	if len(r.Rows) != 4 || len(r.Docs) != 4 || !r.HasRows {
		t.Fatalf("rows %d docs %d", len(r.Rows), len(r.Docs))
	}
	eq(t, "id", col(r, "id"), "1", "2", "3", "4")
	if r.Columns[0].Name != "id" || r.Columns[0].Type != "N" {
		t.Errorf("first column %+v", r.Columns[0])
	}
	// value normalisation
	eq(t, "name", col(r, "name"), "Ada Lovelace", "Alan Turing", "Grace Hopper", `O'Brien "Q"; DROP`)
	eq(t, "active", col(r, "active"), "true", "false", "true", "NULL")
	eq(t, "balance", col(r, "balance"), "NULL", "NULL", "12345678901234567890.123456789", "NULL")
	eq(t, "age", col(r, "age"), "36", "41", "85", "NULL")
	eq(t, "tags", col(r, "tags"), `["math","poet"]`, "NULL", "NULL", "NULL")
	eq(t, "scores", col(r, "scores"), "NULL", "[1.5,2.25,100]", "NULL", "NULL")
	eq(t, "avatar", col(r, "avatar"), "NULL", `\xdeadbeef`, "NULL", "NULL")
	eq(t, "history", col(r, "history"), "NULL", "NULL", "NULL", `[1,"two",{"three":3},[4]]`)
	eq(t, "profile", col(r, "profile"), "NULL", `{"address":{"zip":"SK9"},"city":"Wilmslow","langs":["en","de"]}`, "NULL", "NULL")
	for i, row := range r.Rows {
		for j, c := range row {
			switch c.(type) {
			case nil, string, []byte, bool, int64, uint64, float64, time.Time:
			default:
				t.Errorf("row %d column %s holds %T", i, r.Columns[j].Name, c)
			}
		}
	}
	if !json.Valid([]byte(r.Docs[1])) || !strings.Contains(r.Docs[1], `"$ns":[1.5,2.25,100]`) && !strings.Contains(r.Docs[1], `"$binary":"3q2+7w=="`) {
		t.Errorf("doc %s", r.Docs[1])
	}
	if !strings.Contains(r.Docs[2], `12345678901234567890.123456789`) {
		t.Errorf("number lost precision in the document: %s", r.Docs[2])
	}

	// limit + offset walk the pages
	p := rows(t, s, db.RowQuery{ObjRef: ref("webtuidb_users"), Sort: []db.Sort{{Column: "id"}}, Limit: 2, Offset: 1})
	if len(p.Rows) != 2 {
		t.Fatalf("page of %d", len(p.Rows))
	}
	all := rows(t, s, db.RowQuery{ObjRef: ref("webtuidb_users"), Limit: 3})
	if len(all.Rows) != 3 {
		t.Errorf("limit 3 returned %d", len(all.Rows))
	}
	rest := rows(t, s, db.RowQuery{ObjRef: ref("webtuidb_users"), Limit: 3, Offset: 3})
	if len(rest.Rows) != 1 {
		t.Errorf("offset 3 returned %d", len(rest.Rows))
	}
	none := rows(t, s, db.RowQuery{ObjRef: ref("webtuidb_users"), Limit: 3, Offset: 10})
	if len(none.Rows) != 0 {
		t.Errorf("offset past the end returned %d", len(none.Rows))
	}

	// projection
	pr := rows(t, s, db.RowQuery{ObjRef: ref("webtuidb_users"), Columns: []string{"id", "name"}, Sort: []db.Sort{{Column: "id", Desc: true}}})
	if len(pr.Columns) != 2 || pr.Columns[1].Name != "name" {
		t.Fatalf("projected columns %+v", pr.Columns)
	}
	eq(t, "projected id desc", col(pr, "id"), "4", "3", "2", "1")
	if strings.Contains(pr.Docs[0], "email") {
		t.Errorf("projection fetched everything: %s", pr.Docs[0])
	}
	// client-side sort of a non-key column; N sorts numerically, nulls first
	so := rows(t, s, db.RowQuery{ObjRef: ref("webtuidb_users"), Sort: []db.Sort{{Column: "age", Desc: true}}})
	eq(t, "age desc", col(so, "age"), "85", "41", "36", "NULL")
}

func TestRowsQueryAndFilters(t *testing.T) {
	s := open(t)
	ord := ref("webtuidb_orders")
	// partition key pinned: a Query, ordered by the sort key on the server
	r := rows(t, s, db.RowQuery{ObjRef: ord, Filters: flt("customer", "=", "c1"), Sort: []db.Sort{{Column: "ts", Desc: true}}})
	eq(t, "c1 ts desc", col(r, "ts"), "300", "200", "100")
	r = rows(t, s, db.RowQuery{ObjRef: ord, Filters: flt("customer", "=", "c1")})
	eq(t, "c1 ts asc", col(r, "ts"), "100", "200", "300")
	// partition + sort-key condition + a filter on another attribute
	fs := db.FilterSet{Filters: []db.Filter{{Column: "customer", Op: "=", Value: "c1", Enabled: true}, {Column: "ts", Op: ">=", Value: "200", Enabled: true}, {Column: "status", Op: "=", Value: "paid", Enabled: true}}}
	eq(t, "query+filter", col(rows(t, s, db.RowQuery{ObjRef: ord, Filters: fs}), "ts"), "300")
	fs = flt("customer", "=", "c1")
	fs.Filters = append(fs.Filters, db.Filter{Column: "ts", Op: "BETWEEN", Value: "150 AND 300", Enabled: true})
	eq(t, "between", col(rows(t, s, db.RowQuery{ObjRef: ord, Filters: fs}), "ts"), "200", "300")
	fs = flt("customer", "=", "c1")
	fs.Filters = append(fs.Filters, db.Filter{Column: "status", Op: "Has prefix", Value: "pa", Enabled: true})
	eq(t, "prefix", col(rows(t, s, db.RowQuery{ObjRef: ord, Filters: fs}), "ts"), "100", "300")
	// a disabled filter does nothing
	fs = db.FilterSet{Filters: []db.Filter{{Column: "customer", Op: "=", Value: "nobody"}}}
	if n := len(rows(t, s, db.RowQuery{ObjRef: ord, Filters: fs}).Rows); n != 5 {
		t.Errorf("disabled filter left %d rows", n)
	}

	users := ref("webtuidb_users")
	idsOf := func(fs db.FilterSet) []string {
		t.Helper()
		r := rows(t, s, db.RowQuery{ObjRef: users, Filters: fs, Sort: []db.Sort{{Column: "id"}}})
		return col(r, "id")
	}
	eq(t, "age > 40 (numeric)", idsOf(flt("age", ">", "40")), "2", "3")
	eq(t, "age = 9 not text-compared", idsOf(flt("age", "<", "9")))
	eq(t, "name = ", idsOf(flt("name", "=", "Alan Turing")), "2")
	eq(t, "name <>", idsOf(flt("name", "<>", "Alan Turing")), "1", "3", "4")
	eq(t, "Contains string", idsOf(flt("name", "Contains", "ovel")), "1")
	eq(t, "Contains set element", idsOf(flt("tags", "Contains", "poet")), "1")
	eq(t, "Contains number set element", idsOf(flt("scores", "Contains", "2.25")), "2")
	eq(t, "Not contains", idsOf(flt("name", "Not contains", "a")), "4")
	eq(t, "Has prefix", idsOf(flt("email", "Has prefix", "gr")), "3")
	eq(t, "IN numbers", idsOf(flt("id", "IN", "1, 3")), "1", "3")
	eq(t, "NOT IN", idsOf(flt("id", "NOT IN", "1, 3")), "2", "4")
	eq(t, "IS NULL (missing or NULL)", idsOf(flt("nickname", "IS NULL", "")), "1", "2", "3", "4")
	eq(t, "IS NOT NULL", idsOf(flt("nickname", "IS NOT NULL", "")))
	eq(t, "IS NOT NULL age", idsOf(flt("age", "IS NOT NULL", "")), "1", "2", "3")
	eq(t, "BOOL", idsOf(flt("active", "=", "true")), "1", "3")
	eq(t, "NOT BETWEEN", idsOf(flt("age", "NOT BETWEEN", "40 AND 50")), "1", "3", "4") // DynamoDB negations also match items without the attribute
	eq(t, "any column", idsOf(flt(db.AnyColumn, "Contains", "example.com")), "1", "2", "3", "4")
	eq(t, "any column =", idsOf(flt(db.AnyColumn, "=", "85")), "3")
	eq(t, "big decimal", idsOf(flt("balance", "=", "12345678901234567890.123456789")), "3")
	eq(t, "raw filter expression", idsOf(flt(db.RawColumn, "", "age < 40 AND attribute_exists(email)")), "1")
	or := db.FilterSet{Or: true, Filters: []db.Filter{{Column: "id", Op: "=", Value: "1", Enabled: true}, {Column: "id", Op: "=", Value: "4", Enabled: true}}}
	eq(t, "or", idsOf(or), "1", "4")
	and := db.FilterSet{Filters: []db.Filter{{Column: "age", Op: ">", Value: "30", Enabled: true}, {Column: "active", Op: "=", Value: "true", Enabled: true}}}
	eq(t, "and", idsOf(and), "1", "3")
	for _, f := range []db.Filter{{Column: "age", Op: "=", Value: "abc"}, {Column: "name", Op: "LIKE", Value: "A%"}, {Column: "name", Op: "Has suffix", Value: "x"}, {Column: "age", Op: "??", Value: "1"}, {Column: "id", Op: "BETWEEN", Value: "1"}} {
		f.Enabled = true
		if _, err := s.Rows(context.Background(), db.RowQuery{ObjRef: users, Filters: db.FilterSet{Filters: []db.Filter{f}}}); err == nil {
			t.Errorf("filter %+v accepted", f)
		}
	}
	if _, err := s.Rows(context.Background(), db.RowQuery{ObjRef: users, Filters: flt(db.RawColumn, "", "age <<< 3")}); err == nil {
		t.Error("bad raw expression accepted")
	}
}

func TestCount(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	ex, err := s.Count(ctx, db.RowQuery{ObjRef: ref("webtuidb_orders")}, false)
	if err != nil || ex.N != 5 || !ex.Exact {
		t.Errorf("exact %+v %v", ex, err)
	}
	est, err := s.Count(ctx, db.RowQuery{ObjRef: ref("webtuidb_orders")}, true)
	if err != nil || est.N != 5 { // 0 from DescribeTable falls back to counting
		t.Errorf("estimate %+v %v", est, err)
	}
	f, err := s.Count(ctx, db.RowQuery{ObjRef: ref("webtuidb_orders"), Filters: flt("customer", "=", "c2")}, true)
	if err != nil || f.N != 2 || !f.Exact {
		t.Errorf("filtered %+v %v", f, err)
	}
	f, err = s.Count(ctx, db.RowQuery{ObjRef: ref("webtuidb_users"), Filters: flt("age", ">", "40")}, false)
	if err != nil || f.N != 2 {
		t.Errorf("scan filtered %+v %v", f, err)
	}
	if _, err := s.Count(ctx, db.RowQuery{ObjRef: ref("webtuidb_no_such")}, false); err == nil {
		t.Error("count of a missing table")
	}
}

func TestBigTablePaging(t *testing.T) {
	s := open(t)
	name := tempTable(t, s, true)
	var reqs []types.WriteRequest
	pad := strings.Repeat("x", 3000)
	for i := 0; i < 600; i++ { // 1.8 MB: a Scan needs more than one page
		reqs = append(reqs, types.WriteRequest{PutRequest: &types.PutRequest{Item: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: fmt.Sprintf("p%d", i%3)}, "sk": &types.AttributeValueMemberN{Value: fmt.Sprint(i)}, "pad": &types.AttributeValueMemberS{Value: pad}}}})
	}
	if err := s.batchWrite(context.Background(), name, reqs); err != nil {
		t.Fatal(err)
	}
	if c, err := s.Count(context.Background(), db.RowQuery{ObjRef: ref(name)}, false); err != nil || c.N != 600 {
		t.Fatalf("count %+v %v", c, err)
	}
	r := rows(t, s, db.RowQuery{ObjRef: ref(name), Limit: 100, Offset: 550})
	if len(r.Rows) != 50 {
		t.Errorf("offset 550 of 600 gave %d rows", len(r.Rows))
	}
	r = rows(t, s, db.RowQuery{ObjRef: ref(name), Limit: 100, Offset: 350, Filters: flt("id", "=", "p1")})
	if len(r.Rows) != 0 { // 200 items in the partition
		t.Errorf("past the filtered end: %d rows", len(r.Rows))
	}
	r = rows(t, s, db.RowQuery{ObjRef: ref(name), Limit: 50, Offset: 150, Filters: flt("id", "=", "p1")})
	if len(r.Rows) != 50 {
		t.Errorf("filtered window: %d rows", len(r.Rows))
	}
	// exec without a limit follows NextToken to the end
	if res := exec(t, s, `SELECT id, sk FROM `+quote(name)); len(res.Rows) != 600 {
		t.Errorf("select all: %d rows", len(res.Rows))
	}
	res, err := s.Exec(context.Background(), `SELECT id, sk FROM `+quote(name), 250)
	if err != nil || len(res.Rows) != 250 || !res.Truncated {
		t.Errorf("limit 250: %d rows truncated=%v %v", len(res.Rows), res != nil && res.Truncated, err)
	}
	res, err = s.Exec(context.Background(), `SELECT id, sk FROM `+quote(name), 600)
	if err != nil || len(res.Rows) != 600 || res.Truncated {
		t.Errorf("limit equal to size: %d rows truncated=%v %v", len(res.Rows), res != nil && res.Truncated, err)
	}
}

func TestExec(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	r := exec(t, s, `-- comment first
SELECT id, name FROM "webtuidb_users" WHERE id = 2;`)
	if !r.HasRows || len(r.Rows) != 1 || db.FormatValue(r.Rows[0][1]) != "Alan Turing" || r.Columns[0].Name != "id" {
		t.Errorf("select %+v", r)
	}
	if r.Duration <= 0 {
		t.Error("no duration")
	}
	r = exec(t, s, `SELECT * FROM "webtuidb_users" WHERE id = 999`)
	if !r.HasRows || len(r.Rows) != 0 {
		t.Errorf("empty select %+v", r)
	}
	name := tempTable(t, s, false)
	r = exec(t, s, fmt.Sprintf(`INSERT INTO "%s" VALUE {'id': 'a', 'n': 1, 'doc': {'x': [1, 2]}, 'set': <<'p','q'>>}`, name))
	if r.HasRows || r.Message == "" {
		t.Errorf("insert result %+v", r)
	}
	r = exec(t, s, fmt.Sprintf(`SELECT * FROM "%s"`, name))
	if len(r.Rows) != 1 || r.Columns[0].Name != "id" {
		t.Fatalf("after insert %+v", r)
	}
	exec(t, s, fmt.Sprintf(`UPDATE "%s" SET n = 5 WHERE id = 'a'`, name))
	eq(t, "n", col(exec(t, s, fmt.Sprintf(`SELECT n FROM "%s"`, name)), "n"), "5")
	exec(t, s, fmt.Sprintf(`DELETE FROM "%s" WHERE id = 'a'`, name))
	if n := len(exec(t, s, fmt.Sprintf(`SELECT * FROM "%s"`, name)).Rows); n != 0 {
		t.Errorf("%d rows after delete", n)
	}
	for _, bad := range []string{`SELEC * FROM x`, `SELECT * FROM "webtuidb_no_such"`, `INSERT INTO "` + name + `" VALUE {'id': 'a'}; INSERT`, `CREATE TABLE`, `ALTER TABLE "x" MODIFY`} {
		if _, err := s.Exec(ctx, bad, 0); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	// a duplicate key is an error, not an overwrite
	exec(t, s, fmt.Sprintf(`INSERT INTO "%s" VALUE {'id': 'dup'}`, name))
	if _, err := s.Exec(ctx, fmt.Sprintf(`INSERT INTO "%s" VALUE {'id': 'dup', 'x': 1}`, name), 0); err == nil || !strings.Contains(err.Error(), "Duplicate") {
		t.Errorf("duplicate insert: %v", err)
	}
	if r := exec(t, s, ""); r.Message == "" {
		t.Error("empty statement")
	}
}

func TestExecCancel(t *testing.T) {
	s := open(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Exec(ctx, `SELECT * FROM "webtuidb_users"`, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("exec err %v", err)
	}
	if _, err := s.Rows(ctx, db.RowQuery{ObjRef: ref("webtuidb_users")}); !errors.Is(err, context.Canceled) {
		t.Errorf("rows err %v", err)
	}
	if _, err := s.Count(ctx, db.RowQuery{ObjRef: ref("webtuidb_users")}, false); !errors.Is(err, context.Canceled) {
		t.Errorf("count err %v", err)
	}
	if _, err := s.Objects(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Errorf("objects err %v", err)
	}
	// cancelling in the middle of a long read stops it
	name := tempTable(t, s, false)
	var reqs []types.WriteRequest
	pad := strings.Repeat("y", 100000)
	for i := 0; i < 300; i++ {
		reqs = append(reqs, types.WriteRequest{PutRequest: &types.PutRequest{Item: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: fmt.Sprint(i)}, "pad": &types.AttributeValueMemberS{Value: pad}}}})
	}
	if err := s.batchWrite(context.Background(), name, reqs); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := s.Exec(ctx, `SELECT * FROM `+quote(name), 0)
	if err == nil {
		t.Skip("the local server answered 30 MB inside 30ms")
	}
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
		t.Errorf("slow cancel: %v after %v", err, time.Since(start))
	}
}

func TestPlanApplyRoundTrip(t *testing.T) {
	s := open(t)
	name := tempTable(t, s, true)
	ctx := context.Background()
	r := ref(name)
	if _, err := s.Columns(ctx, r); err != nil { // loads the key schema the planner needs
		t.Fatal(err)
	}
	ins := func(set ...db.KeyVal) db.Change { return db.Change{Kind: db.Insert, ObjRef: r, Set: set} }
	kv := func(c string, v any) db.KeyVal { return db.KeyVal{Col: c, Val: v} }
	apply(t, s, ins(
		kv("id", "u1"), kv("sk", "7"),
		kv("txt", "plain 'quoted' text"), kv("num", "12345678901234567890.123456789"), kv("flag", "true"),
		kv("nul", db.Null), kv("empty", db.Empty), kv("bin", []byte{0, 1, 0xff}),
		kv("doc", `{"a": [1, "x", {"b": null}], "set": {"$ss": ["q", "p"]}, "raw": {"$binary": "AQI="}, "n": 1.50}`),
		kv("list", `[1, 2.5, "three", true, null]`),
		kv("skip", db.Default),
	))
	got := rows(t, s, db.RowQuery{ObjRef: r})
	if len(got.Rows) != 1 {
		t.Fatalf("%d rows", len(got.Rows))
	}
	row := map[string]any{}
	types_ := map[string]string{}
	for i, c := range got.Columns {
		row[c.Name], types_[c.Name] = got.Rows[0][i], c.Type
	}
	if row["id"] != "u1" || row["sk"] != int64(7) || row["txt"] != "plain 'quoted' text" || row["num"] != "12345678901234567890.123456789" ||
		row["flag"] != true || row["nul"] != nil || row["empty"] != "" || string(row["bin"].([]byte)) != "\x00\x01\xff" {
		t.Errorf("round trip: %#v", row)
	}
	for k, want := range map[string]string{"num": "N", "flag": "BOOL", "nul": "NULL", "empty": "S", "bin": "B", "doc": "M", "list": "L", "sk": "N"} {
		if types_[k] != want {
			t.Errorf("%s stored as %s, want %s", k, types_[k], want)
		}
	}
	if _, has := row["skip"]; has {
		t.Error("DEFAULT was stored")
	}
	if !strings.Contains(db.FormatValue(row["doc"]), `"$ss":["p","q"]`) || !strings.Contains(db.FormatValue(row["doc"]), `"$binary":"AQI="`) || !strings.Contains(db.FormatValue(row["doc"]), `"n":1.5`) {
		t.Errorf("nested document lost types: %v", row["doc"])
	}
	// edit the nested document again through its own text: the sets and binary survive
	apply(t, s, db.Change{Kind: db.Update, ObjRef: r, Key: []db.KeyVal{kv("id", "u1"), kv("sk", int64(7))}, Set: []db.KeyVal{kv("doc", db.FormatValue(row["doc"]))}})
	again := rows(t, s, db.RowQuery{ObjRef: r})
	if again.Docs[0] != got.Docs[0] {
		t.Errorf("document changed by an identity edit:\n%s\n%s", got.Docs[0], again.Docs[0])
	}

	// sets, typed by their column once the sample knows them
	apply(t, s, ins(kv("id", "u2"), kv("sk", int64(1)), kv("tags", `{"$ss": ["a", "b"]}`), kv("nums", `{"$ns": [1, 2.5]}`), kv("bins", `{"$bs": ["AQ==", "Ag=="]}`)))
	if _, err := s.loadMeta(ctx, name); err != nil {
		t.Fatal(err)
	}
	apply(t, s, db.Change{Kind: db.Update, ObjRef: r, Key: []db.KeyVal{kv("id", "u2"), kv("sk", int64(1))}, Set: []db.KeyVal{kv("tags", `["x","y","z"]`), kv("nums", `[3, 4]`)}})
	r2 := rows(t, s, db.RowQuery{ObjRef: r, Filters: flt("id", "=", "u2")})
	eq(t, "tags", col(r2, "tags"), `["x","y","z"]`)
	eq(t, "nums", col(r2, "nums"), `[3,4]`)
	eq(t, "bins", col(r2, "bins"), `["AQ==","Ag=="]`)

	// update: SET, REMOVE (DEFAULT), NULL, and a typed edit; the key identifies the item
	apply(t, s, db.Change{Kind: db.Update, ObjRef: r, Key: []db.KeyVal{kv("id", "u1"), kv("sk", int64(7))},
		Set: []db.KeyVal{kv("txt", "changed"), kv("num", "3.14"), kv("flag", "false"), kv("empty", db.Default), kv("nul", "x"), kv("added", "42")}})
	u := rows(t, s, db.RowQuery{ObjRef: r, Filters: flt("id", "=", "u1")})
	eq(t, "txt", col(u, "txt"), "changed")
	eq(t, "num", col(u, "num"), "3.14")
	eq(t, "flag", col(u, "flag"), "false")
	if col(u, "empty") != nil {
		t.Errorf("a removed attribute is still a column: %v", col(u, "empty"))
	}
	eq(t, "nul", col(u, "nul"), "x")
	eq(t, "added (number inferred)", col(u, "added"), "42")
	for i, c := range u.Columns {
		if c.Name == "added" && c.Type != "N" {
			t.Errorf("column %+v = %v", c, u.Rows[0][i])
		}
	}
	// updating an item that does not exist fails instead of creating it
	stmts, err := s.Plan([]db.Change{{Kind: db.Update, ObjRef: r, Key: []db.KeyVal{kv("id", "ghost"), kv("sk", int64(1))}, Set: []db.KeyVal{kv("x", "1")}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, stmts); err == nil {
		t.Error("update of a missing item succeeded")
	}
	if n, _ := s.Count(ctx, db.RowQuery{ObjRef: r}, false); n.N != 2 {
		t.Errorf("%d items after the failed update", n.N)
	}

	apply(t, s, db.Change{Kind: db.Delete, ObjRef: r, Key: []db.KeyVal{kv("id", "u1"), kv("sk", int64(7))}})
	if n, _ := s.Count(ctx, db.RowQuery{ObjRef: r}, false); n.N != 1 {
		t.Errorf("%d items after delete", n.N)
	}
}

func TestApplyAtomicity(t *testing.T) {
	s := open(t)
	name := tempTable(t, s, false)
	ctx := context.Background()
	r := ref(name)
	if _, err := s.Columns(ctx, r); err != nil {
		t.Fatal(err)
	}
	ins := func(id string) db.Change {
		return db.Change{Kind: db.Insert, ObjRef: r, Set: []db.KeyVal{{Col: "id", Val: id}}}
	}
	apply(t, s, ins("keep"))
	// the second statement is a duplicate: the first must not be applied either
	stmts, err := s.Plan([]db.Change{ins("new1"), ins("keep"), ins("new2")})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Apply(ctx, stmts)
	if err == nil || !strings.Contains(err.Error(), "nothing was applied") || !strings.Contains(err.Error(), "'keep'") {
		t.Errorf("duplicate: %v", err)
	}
	if n, _ := s.Count(ctx, db.RowQuery{ObjRef: r}, false); n.N != 1 {
		t.Errorf("%d items: a failed transaction left writes behind", n.N)
	}
	// more than 100 statements: refused before anything is sent
	var many []db.Change
	for i := 0; i < 101; i++ {
		many = append(many, ins(fmt.Sprintf("m%d", i)))
	}
	stmts, err = s.Plan(many)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, stmts); err == nil || !strings.Contains(err.Error(), "limit of 100") {
		t.Errorf("101 statements: %v", err)
	}
	if n, _ := s.Count(ctx, db.RowQuery{ObjRef: r}, false); n.N != 1 {
		t.Errorf("%d items after the refused commit", n.N)
	}
	if err := s.Apply(ctx, stmts[:100]); err != nil {
		t.Errorf("exactly 100: %v", err)
	}
	if n, _ := s.Count(ctx, db.RowQuery{ObjRef: r}, false); n.N != 101 {
		t.Errorf("%d items after 100 inserts", n.N)
	}
	if err := s.Apply(ctx, nil); err != nil {
		t.Errorf("empty apply: %v", err)
	}
	if err := s.Apply(ctx, []db.Statement{{Text: "INSERT INTO x VALUE {'id': ?}", Args: []any{"plain string"}}}); err == nil {
		t.Error("a parameter that is not an attribute was accepted")
	}
}

func TestPlanAdminAndTable(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	src := tempTable(t, s, true)
	r := ref(src)
	if _, err := s.Columns(ctx, r); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		apply(t, s, db.Change{Kind: db.Insert, ObjRef: r, Set: []db.KeyVal{{Col: "id", Val: fmt.Sprintf("i%d", i%4)}, {Col: "sk", Val: int64(i)}, {Col: "email", Val: fmt.Sprintf("u%d@x.io", i)}}})
	}

	// add a GSI through the structure editor path, then drop it
	cols, _ := s.Columns(ctx, r)
	old := &db.TableDef{Name: src, Columns: cols}
	for _, c := range old.Columns {
		if c.PK {
			old.PrimaryKey = append(old.PrimaryKey, c.Name)
		}
	}
	nw := *old
	nw.Columns = append([]db.Column(nil), cols...)
	for i := range nw.Columns {
		if nw.Columns[i].Name == "email" {
			nw.Columns[i].Type = "S"
		}
	}
	old.Columns = append([]db.Column(nil), nw.Columns...) // email was inferred as S already
	nw.Indexes = []db.Index{{Name: "by_email", Columns: []string{"email"}, Condition: "INCLUDE sk"}}
	stmts, err := s.PlanTable(old, &nw)
	if err != nil || len(stmts) != 1 || !strings.HasPrefix(stmts[0].Text, `ALTER TABLE "`+src+`" ADD INDEX {`) {
		t.Fatalf("plan add index: %+v %v", stmts, err)
	}
	if err := s.Apply(ctx, stmts); err != nil {
		t.Fatal(err)
	}
	ix, _ := s.Indexes(ctx, r)
	if len(ix) != 2 || ix[1].Name != "by_email" || ix[1].Condition != "INCLUDE sk" {
		t.Fatalf("indexes after add: %+v", ix)
	}
	old2 := nw
	old2.Indexes = []db.Index{ix[1]}
	nw2 := old2
	nw2.Indexes = nil
	stmts, err = s.PlanTable(&old2, &nw2)
	if err != nil || len(stmts) != 1 || !strings.Contains(stmts[0].Text, "DROP INDEX") {
		t.Fatalf("plan drop index: %+v %v", stmts, err)
	}
	if err := s.Apply(ctx, stmts); err != nil {
		t.Fatal(err)
	}
	if ix, _ = s.Indexes(ctx, r); len(ix) != 1 {
		t.Errorf("indexes after drop: %+v", ix)
	}
	// a changed index is dropped and rebuilt, in one Apply, waiting in between
	nw.Indexes = []db.Index{{Name: "by_email", Columns: []string{"email"}}}
	nw3 := nw
	nw3.Indexes = []db.Index{{Name: "by_email", Columns: []string{"email", "sk"}}}
	stmts, err = s.PlanTable(&nw, &nw3)
	if err != nil || len(stmts) != 2 {
		t.Fatalf("rebuild plan: %+v %v", stmts, err)
	}
	// nw has no index in the table yet: add it first, then rebuild
	first, err := s.PlanTable(old, &nw)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, append(first, stmts...)); err != nil {
		t.Fatal(err)
	}
	ix, _ = s.Indexes(ctx, r)
	if len(ix) != 2 || strings.Join(ix[1].Columns, ",") != "email,sk" {
		t.Errorf("indexes after rebuild: %+v", ix)
	}

	// clone without and with data
	st, err := s.PlanAdmin(db.OpClone, r, map[string]string{"new_name": src + "_c1"})
	if err != nil || len(st) != 1 || strings.Contains(st[0].Text, "WITH DATA") {
		t.Fatalf("clone plan %+v %v", st, err)
	}
	if err := s.Apply(ctx, st); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Exec(context.Background(), "DROP TABLE "+quote(src+"_c1"), 0) })
	if n, _ := s.Count(ctx, db.RowQuery{ObjRef: ref(src + "_c1")}, false); n.N != 0 {
		t.Errorf("structure-only clone has %d items", n.N)
	}
	if ix, _ := s.Indexes(ctx, ref(src+"_c1")); len(ix) != 2 {
		t.Errorf("clone indexes %+v", ix)
	}
	st, _ = s.PlanAdmin(db.OpClone, r, map[string]string{"new_name": src + "_c2", "with_data": "true"})
	if err := s.Apply(ctx, st); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Exec(context.Background(), "DROP TABLE "+quote(src+"_c2"), 0) })
	if n, _ := s.Count(ctx, db.RowQuery{ObjRef: ref(src + "_c2")}, false); n.N != 30 {
		t.Errorf("clone with data has %d items", n.N)
	}
	if _, err := s.PlanAdmin(db.OpClone, r, map[string]string{"new_name": src + "_c2"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("clone onto an existing table: %v", err)
	}
	if _, err := s.PlanAdmin(db.OpClone, r, nil); err == nil {
		t.Error("clone without a name")
	}
	if _, err := s.PlanAdmin(db.OpRenameObject, r, map[string]string{"new_name": "x"}); err == nil || !strings.Contains(err.Error(), "cannot rename") {
		t.Errorf("rename: %v", err)
	}

	// truncate empties the table (more than one batch) and keeps its structure
	st, _ = s.PlanAdmin(db.OpTruncate, ref(src+"_c2"), nil)
	if err := s.Apply(ctx, st); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Count(ctx, db.RowQuery{ObjRef: ref(src + "_c2")}, false); n.N != 0 {
		t.Errorf("%d items after truncate", n.N)
	}
	if _, err := s.Columns(ctx, ref(src+"_c2")); err != nil {
		t.Errorf("table gone after truncate: %v", err)
	}
	// drop
	st, _ = s.PlanAdmin(db.OpDropObject, ref(src+"_c2"), nil)
	if err := s.Apply(ctx, st); err != nil {
		t.Fatal(err)
	}
	if names, _ := s.tableNames(ctx); contains(names, src+"_c2") {
		t.Error("table still listed after drop")
	}
	// create from the structure editor, with a sort key and an index
	nt := fmt.Sprintf("webtuidb_test_%x", rand.Uint32())
	def := &db.TableDef{Name: nt, PrimaryKey: []string{"pk", "sk"}, Columns: []db.Column{
		{Name: "pk", Type: "S", PK: true}, {Name: "sk", Type: "N", PK: true}, {Name: "alt", Type: "B"}},
		Indexes: []db.Index{{Name: "by_alt", Columns: []string{"alt"}, Condition: "KEYS_ONLY"}}}
	st, err = s.PlanTable(nil, def)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, st); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Exec(context.Background(), "DROP TABLE "+quote(nt), 0) })
	cc, _ := s.Columns(ctx, ref(nt))
	if len(cc) != 3 || cc[0].Name != "pk" || cc[1].Extra != "sort key" || cc[2].Type != "B" {
		t.Errorf("created table columns %+v", cc)
	}
}

func TestPlanErrors(t *testing.T) {
	s := open(t)
	if _, err := s.Plan([]db.Change{{Kind: db.Insert, ObjRef: ref("webtuidb_no_such"), Set: []db.KeyVal{{Col: "id", Val: "1"}}}}); err == nil {
		t.Error("plan for a missing table")
	}
	u := ref("webtuidb_users")
	if _, err := s.Columns(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]db.Change{
		"insert without key":  {Kind: db.Insert, ObjRef: u, Set: []db.KeyVal{{Col: "name", Val: "x"}}},
		"insert null key":     {Kind: db.Insert, ObjRef: u, Set: []db.KeyVal{{Col: "id", Val: db.Null}}},
		"insert bad number":   {Kind: db.Insert, ObjRef: u, Set: []db.KeyVal{{Col: "id", Val: "abc"}}},
		"update key attr":     {Kind: db.Update, ObjRef: u, Key: []db.KeyVal{{Col: "id", Val: int64(1)}}, Set: []db.KeyVal{{Col: "id", Val: "9"}}},
		"update without key":  {Kind: db.Update, ObjRef: u, Set: []db.KeyVal{{Col: "name", Val: "x"}}},
		"update without set":  {Kind: db.Update, ObjRef: u, Key: []db.KeyVal{{Col: "id", Val: int64(1)}}},
		"delete without key":  {Kind: db.Delete, ObjRef: u},
		"delete null key":     {Kind: db.Delete, ObjRef: u, Key: []db.KeyVal{{Col: "id", Val: nil}}},
		"bad typed edit":      {Kind: db.Update, ObjRef: u, Key: []db.KeyVal{{Col: "id", Val: int64(1)}}, Set: []db.KeyVal{{Col: "age", Val: "old"}}},
		"bad set element":     {Kind: db.Update, ObjRef: u, Key: []db.KeyVal{{Col: "id", Val: int64(1)}}, Set: []db.KeyVal{{Col: "tags", Val: `[1]`}}},
		"empty set":           {Kind: db.Update, ObjRef: u, Key: []db.KeyVal{{Col: "id", Val: int64(1)}}, Set: []db.KeyVal{{Col: "tags", Val: `[]`}}},
		"expression":          {Kind: db.Update, ObjRef: u, Key: []db.KeyVal{{Col: "id", Val: int64(1)}}, Set: []db.KeyVal{{Col: "age", Val: db.Expr("age + 1")}}},
		"bad boolean":         {Kind: db.Update, ObjRef: u, Key: []db.KeyVal{{Col: "id", Val: int64(1)}}, Set: []db.KeyVal{{Col: "active", Val: "maybe"}}},
		"object as list type": {Kind: db.Update, ObjRef: u, Key: []db.KeyVal{{Col: "id", Val: int64(1)}}, Set: []db.KeyVal{{Col: "history", Val: `{"a": 1}`}}},
	} {
		if st, err := s.Plan([]db.Change{c}); err == nil {
			t.Errorf("%s accepted: %+v", name, st)
		}
	}
}

func TestSafeModeClassification(t *testing.T) {
	s := open(t)
	d := s.Caps().Dialect
	for stmt, want := range map[string]bool{
		`SELECT * FROM "t"`: true,
		`select * from "t" where a = 'x; DELETE FROM t'`:    true,
		`-- DELETE FROM t` + "\n" + `SELECT 1`:              true,
		`INSERT INTO "t" VALUE {'id': 1}`:                   false,
		`UPDATE "t" SET a = 1 WHERE id = 1`:                 false,
		`DELETE FROM "t" WHERE id = 1`:                      false,
		`DROP TABLE "t"`:                                    false,
		`TRUNCATE TABLE "t"`:                                false,
		`CREATE TABLE "t" {"keys": []}`:                     false,
		`ALTER TABLE "t" DROP INDEX "i"`:                    false,
		`SELECT * FROM "t"; DELETE FROM "t" WHERE id = 1`:   false,
		`SELECT * FROM "t" WHERE a = '--'; DELETE FROM "t"`: false,
		`/* SELECT */ DELETE FROM "t" WHERE id = 1`:         false,
		`EXISTS(SELECT * FROM "t" WHERE id = 1)`:            false,
		"":                                                  true,
	} {
		if got := db.IsReadOnly(d, stmt); got != want {
			t.Errorf("IsReadOnly(%q) = %v, want %v", stmt, got, want)
		}
	}
}

func TestSeedIsComplete(t *testing.T) {
	s := open(t)
	for _, n := range []string{"webtuidb_users", "webtuidb_orders", "webtuidb_events"} {
		c, err := s.Count(context.Background(), db.RowQuery{ObjRef: ref(n)}, false)
		if err != nil || c.N == 0 {
			t.Errorf("%s: %+v %v", n, c, err)
		}
	}
}
