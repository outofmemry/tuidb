package mongo

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func open(t *testing.T) (*session, context.Context) {
	t.Helper()
	if os.Getenv("WEBTUIDB_INTEGRATION") != "1" {
		t.Skip("set WEBTUIDB_INTEGRATION=1")
	}
	url := os.Getenv("WEBTUIDB_TEST_URL_MONGODB")
	if url == "" {
		url = "mongodb://webtuidb:webtuidb@127.0.0.1:57017/webtuidb?authSource=admin"
	}
	c, err := config.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	d, err := db.Lookup("mongodb")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := d.Open(ctx, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	return sess.(*session), ctx
}

func tempColl(t *testing.T, s *session, ctx context.Context) string {
	t.Helper()
	name := "webtuidb_test_" + bson.NewObjectID().Hex()[16:]
	t.Cleanup(func() { s.client.Database("webtuidb").Collection(name).Drop(context.Background()) })
	return name
}

func exec(t *testing.T, s *session, ctx context.Context, stmt string, limit int) *db.Result {
	t.Helper()
	r, err := s.Exec(ctx, stmt, limit)
	if err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
	return r
}

func col(r *db.Result, name string) int {
	for i, c := range r.Columns {
		if c.Name == name {
			return i
		}
	}
	return -1
}

func TestIntegrationBasics(t *testing.T) {
	s, ctx := open(t)
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.ServerInfo(), "MongoDB ") {
		t.Errorf("info %q", s.ServerInfo())
	}
	if c := s.Caps(); c.Dialect != "mongo" || c.Editor != "mongo" || !c.Document || c.Transactions {
		t.Errorf("caps %+v", c)
	}
	dbs, err := s.Databases(ctx)
	if err != nil || !contains(dbs, "webtuidb") {
		t.Fatalf("databases %v %v", dbs, err)
	}
	if s.CurrentDatabase() != "webtuidb" {
		t.Errorf("current %q", s.CurrentDatabase())
	}
	objs, err := s.Objects(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]db.Kind{}
	detail := map[string]string{}
	for _, o := range objs {
		kinds[o.Name] = o.Kind
		detail[o.Name] = o.Detail
	}
	if kinds["customers"] != db.KindTable || kinds["orders"] != db.KindTable || kinds["paid_orders"] != db.KindView || detail["customers"] != "3" {
		t.Errorf("objects %+v", objs)
	}
	if _, ok := kinds["system.views"]; ok {
		t.Error("system collections must be hidden")
	}
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func TestIntegrationMetadata(t *testing.T) {
	s, ctx := open(t)
	cols, err := s.Columns(ctx, db.ObjRef{Name: "customers"})
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]string{}
	for _, c := range cols {
		types[c.Name] = c.Type
	}
	if cols[0].Name != "_id" || !cols[0].PK || cols[0].Nullable || types["_id"] != "int32" || types["name"] != "string" ||
		types["tags"] != "array" || types["address"] != "object" || types["created_at"] != "date" || types["balance"] != "mixed" || types["notes"] != "string" {
		t.Errorf("columns %+v", cols)
	}
	oc, _ := s.Columns(ctx, db.ObjRef{Name: "orders"})
	if oc[0].Type != "objectId" {
		t.Errorf("orders _id %+v", oc[0])
	}
	ix, err := s.Indexes(ctx, db.ObjRef{Name: "customers"})
	if err != nil {
		t.Fatal(err)
	}
	var email *db.Index
	for i := range ix {
		if ix[i].Name == "email_1" {
			email = &ix[i]
		}
	}
	if len(ix) != 2 || email == nil || !email.Unique || email.Algorithm != "btree" || len(email.Columns) != 1 || email.Columns[0] != "email" || !ix[0].Primary {
		t.Errorf("indexes %+v", ix)
	}
	if ix, err := s.Indexes(ctx, db.ObjRef{Name: "paid_orders", Kind: db.KindView}); err != nil || ix != nil {
		t.Errorf("view indexes %v %v", ix, err)
	}
	if v, _ := s.Triggers(ctx, db.ObjRef{}); v != nil {
		t.Error("triggers")
	}
	ddl, err := s.DDL(ctx, db.ObjRef{Name: "customers"})
	if err != nil || !strings.Contains(ddl, `db.customers.createIndex({"email": 1}, {"name": "email_1"`) || !strings.Contains(ddl, `"unique": true`) {
		t.Errorf("ddl %q %v", ddl, err)
	}
	vddl, err := s.DDL(ctx, db.ObjRef{Name: "paid_orders", Kind: db.KindView})
	if err != nil || !strings.HasPrefix(vddl, `db.createView("paid_orders", "orders", [{"$match": {"status": "paid"}}])`) {
		t.Errorf("view ddl %q %v", vddl, err)
	}
	for kind, want := range map[db.ScriptKind]string{
		db.ScriptDrop: "db.customers.drop()", db.ScriptTruncate: "db.customers.deleteMany({})", db.ScriptSelectTop: "db.customers.find({}).limit(100)", db.ScriptCreate: ddl} {
		if got, err := s.Script(ctx, kind, db.ObjRef{Name: "customers"}); err != nil || got != want {
			t.Errorf("script %s: %q %v", kind, got, err)
		}
	}
}

func TestIntegrationRows(t *testing.T) {
	s, ctx := open(t)
	ref := db.ObjRef{Name: "customers", Kind: db.KindTable}
	f := func(c, op, v string) db.Filter { return db.Filter{Column: c, Op: op, Value: v, Enabled: true} }

	r, err := s.Rows(ctx, db.RowQuery{ObjRef: ref, Sort: []db.Sort{{Column: "_id"}}})
	if err != nil || len(r.Rows) != 3 || len(r.Docs) != 3 || r.Columns[0].Name != "_id" {
		t.Fatalf("rows %+v %v", r, err)
	}
	id, adr, name := col(r, "_id"), col(r, "address"), col(r, "name")
	if r.Rows[0][id] != int64(1) || r.Rows[0][adr] != `{"city":"Oslo","zip":"0150"}` || r.Rows[1][name] != "Bob O'Neil" {
		t.Errorf("cells %+v", r.Rows)
	}
	if _, ok := r.Rows[0][col(r, "created_at")].(time.Time); !ok {
		t.Errorf("created_at %#v", r.Rows[0][col(r, "created_at")])
	}
	if r.Rows[0][col(r, "balance")] != 120.5 || r.Rows[0][col(r, "tags")] != `["a","b"]` || r.Rows[2][col(r, "extra")] != `{"nested":[1,2,{"deep":true}]}` || r.Rows[1][col(r, "notes")] != nil {
		t.Errorf("cells %+v", r.Rows)
	}
	if !strings.HasPrefix(r.Docs[0], `{"_id":1,"name":"Ann Lee"`) {
		t.Errorf("doc %s", r.Docs[0])
	}

	r, err = s.Rows(ctx, db.RowQuery{ObjRef: ref, Filters: db.FilterSet{Filters: []db.Filter{f("_id", ">=", "2")}}, Sort: []db.Sort{{Column: "_id", Desc: true}}, Limit: 1})
	if err != nil || len(r.Rows) != 1 || r.Rows[0][0] != int64(3) {
		t.Errorf("filter/sort/limit %+v %v", r, err)
	}
	r, _ = s.Rows(ctx, db.RowQuery{ObjRef: ref, Sort: []db.Sort{{Column: "_id"}}, Limit: 1, Offset: 1})
	if len(r.Rows) != 1 || r.Rows[0][0] != int64(2) {
		t.Errorf("offset %+v", r)
	}
	cases := []struct {
		fl   db.Filter
		want int
	}{
		{f("name", "Contains", "100%_"), 1},
		{f("name", "Has prefix", "ann"), 1},
		{f("name", "Has suffix", "done"), 1},
		{f("name", "Not contains", "o"), 1},
		{f("name", "LIKE", "Bob%"), 1},
		{f("name", "LIKE", "bob%"), 0},
		{f("notes", "IS NULL", ""), 2},
		{f("notes", "IS NOT NULL", ""), 1},
		{f("_id", "IN", "1,3"), 2},
		{f("_id", "NOT IN", "1,3"), 1},
		{f("_id", "BETWEEN", "2 AND 3"), 2},
		{f("balance", "=", "5"), 1},
		{f(db.AnyColumn, "Contains", "oslo"), 0}, // address is an object, not a string field
		{f(db.AnyColumn, "Contains", "example.com"), 2},
		{f(db.AnyColumn, "Not contains", "example.com"), 1}, // no column contains it: only _id 3
		{f(db.RawColumn, "", `{"_id": {"$gt": 1}}`), 2},
		{f(db.RawColumn, "", `{_id: 2, name: /^bob/i}`), 1},
	}
	for _, c := range cases {
		q := db.RowQuery{ObjRef: ref, Filters: db.FilterSet{Filters: []db.Filter{c.fl}}}
		r, err := s.Rows(ctx, q)
		if err != nil || len(r.Rows) != c.want {
			t.Errorf("%+v: %d rows, want %d (%v)", c.fl, len(r.Rows), c.want, err)
		}
		if n, err := s.Count(ctx, q, true); err != nil || n.N != int64(c.want) || !n.Exact {
			t.Errorf("count %+v: %+v %v", c.fl, n, err)
		}
	}
	or := db.FilterSet{Or: true, Filters: []db.Filter{f("_id", "=", "1"), f("_id", "=", "3")}}
	if r, _ := s.Rows(ctx, db.RowQuery{ObjRef: ref, Filters: or}); len(r.Rows) != 2 {
		t.Errorf("or: %d", len(r.Rows))
	}
	if n, err := s.Count(ctx, db.RowQuery{ObjRef: ref}, true); err != nil || n.N != 3 || n.Exact {
		t.Errorf("estimate %+v %v", n, err)
	}
	if n, err := s.Count(ctx, db.RowQuery{ObjRef: ref}, false); err != nil || n.N != 3 || !n.Exact {
		t.Errorf("exact %+v %v", n, err)
	}
	// objects ids come back as hex, view works like a collection
	r, err = s.Rows(ctx, db.RowQuery{ObjRef: db.ObjRef{Name: "paid_orders", Kind: db.KindView}})
	if err != nil || len(r.Rows) != 2 {
		t.Fatalf("view rows %+v %v", r, err)
	}
	if h, ok := r.Rows[0][0].(string); !ok || len(h) != 24 || r.Columns[0].Type != "objectId" {
		t.Errorf("objectId cell %#v %v", r.Rows[0][0], r.Columns[0])
	}
	// filter by objectId hex
	q := db.RowQuery{ObjRef: db.ObjRef{Name: "orders"}, Filters: db.FilterSet{Filters: []db.Filter{f("_id", "=", r.Rows[0][0].(string))}}}
	if rr, err := s.Rows(ctx, q); err != nil || len(rr.Rows) != 1 {
		t.Errorf("objectid filter %v %v", rr, err)
	}
}

func TestIntegrationExec(t *testing.T) {
	s, ctx := open(t)
	r := exec(t, s, ctx, `db.orders.find({status: "paid"}).sort({total: -1})`, 0)
	if len(r.Rows) != 2 || r.Rows[0][col(r, "total")] != 99.99 || r.Truncated || r.Columns[0].Name != "_id" {
		t.Errorf("find %+v", r)
	}
	r = exec(t, s, ctx, `db.orders.find()`, 2)
	if len(r.Rows) != 2 || !r.Truncated || !r.HasRows || r.Duration == 0 {
		t.Errorf("limit %+v", r)
	}
	r = exec(t, s, ctx, `db.orders.find().limit(1);`, 5)
	if len(r.Rows) != 1 || r.Truncated {
		t.Errorf("cursor limit %+v", r)
	}
	r = exec(t, s, ctx, `db.customers.findOne({_id: 1})`, 0)
	if len(r.Rows) != 1 || r.Rows[0][col(r, "name")] != "Ann Lee" {
		t.Errorf("findOne %+v", r)
	}
	r = exec(t, s, ctx, `db.customers.find({_id: {$in: [1, 2]}}, {name: 1, _id: 0}).toArray()`, 0)
	if len(r.Rows) != 2 || len(r.Columns) != 1 || r.Columns[0].Name != "name" {
		t.Errorf("projection %+v", r)
	}
	r = exec(t, s, ctx, `db.orders.aggregate([{$group: {_id: "$status", n: {$sum: 1}}}, {$sort: {_id: 1}}])`, 0)
	if len(r.Rows) != 2 || r.Rows[0][0] != "new" || r.Rows[1][col(r, "n")] != int64(2) {
		t.Errorf("aggregate %+v", r)
	}
	r = exec(t, s, ctx, `db.customers.countDocuments({balance: {$lt: 10}})`, 0)
	if len(r.Rows) != 1 || r.Rows[0][0] != int64(2) {
		t.Errorf("count %+v", r)
	}
	r = exec(t, s, ctx, `db.orders.distinct("status")`, 0)
	if len(r.Rows) != 2 || r.Rows[0][0] != "new" || r.Rows[1][0] != "paid" {
		t.Errorf("distinct %+v", r)
	}
	r = exec(t, s, ctx, `db.getCollection('customers').find({_id: NumberInt(1)}).count()`, 0)
	if r.Rows[0][0] != int64(1) {
		t.Errorf("getCollection/count %+v", r)
	}
	r = exec(t, s, ctx, `db.customers.find({created_at: {$gte: ISODate("2024-01-01")}})`, 0)
	if len(r.Rows) != 1 {
		t.Errorf("ISODate %+v", r)
	}
	r = exec(t, s, ctx, `db.getName()`, 0)
	if r.Rows[0][0] != "webtuidb" {
		t.Errorf("getName %+v", r)
	}
	r = exec(t, s, ctx, `db.getCollectionNames()`, 0)
	if len(r.Rows) < 3 {
		t.Errorf("names %+v", r)
	}
	r = exec(t, s, ctx, `show collections`, 0)
	if r.Columns[0].Name != "collection" || len(r.Rows) < 3 {
		t.Errorf("show collections %+v", r)
	}
	r = exec(t, s, ctx, `show dbs;`, 0)
	if r.Columns[0].Name != "database" || len(r.Rows) < 1 {
		t.Errorf("show dbs %+v", r)
	}
	r = exec(t, s, ctx, `db.getIndexes ? db.customers.getIndexes() : 0`, 0)
	if len(r.Rows) != 2 {
		t.Errorf("getIndexes %+v", r)
	}
	r = exec(t, s, ctx, `db.customers.find({_id: 1}).forEach(printjson)`, 0)
	if r.HasRows || !strings.Contains(r.Message, `"Ann Lee"`) {
		t.Errorf("printjson %+v", r)
	}
	// raw commands
	r = exec(t, s, ctx, `{"ping": 1}`, 0)
	if len(r.Rows) != 1 || r.Rows[0][col(r, "ok")] != 1.0 {
		t.Errorf("ping %+v", r)
	}
	r = exec(t, s, ctx, `{"find": "customers", "filter": {}, "sort": {"_id": 1}}`, 2)
	if len(r.Rows) != 2 || !r.Truncated {
		t.Errorf("find cmd %+v", r)
	}
	// errors
	if _, err := s.Exec(ctx, `db.customers.insertOne({_id: 1})`, 0); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("dup key: %v", err)
	}
	if _, err := s.Exec(ctx, `db.customers.fnd()`, 0); err == nil {
		t.Error("bad method must fail")
	}
	// use switches database
	r = exec(t, s, ctx, `use admin`, 0)
	if s.CurrentDatabase() != "admin" || r.Message != "switched to db admin" {
		t.Errorf("use %+v %s", r, s.CurrentDatabase())
	}
	r = exec(t, s, ctx, `db.getName()`, 0)
	if r.Rows[0][0] != "admin" {
		t.Errorf("db after use %+v", r)
	}
	exec(t, s, ctx, `use webtuidb`, 0)
}

func TestIntegrationWritesAndPlan(t *testing.T) {
	s, ctx := open(t)
	name := tempColl(t, s, ctx)
	ref := db.ObjRef{Name: name, Kind: db.KindTable}
	r := exec(t, s, ctx, `db.`+name+`.insertMany([{_id: 1, name: "a", n: 1.5, when: ISODate("2024-01-02T03:04:05Z"), ok: true}, {_id: 2, name: "b", n: 2.5, when: ISODate("2024-01-03T00:00:00Z"), ok: false}])`, 0)
	if r.HasRows || r.Message != "inserted 2 documents" || r.Affected != 2 {
		t.Errorf("insertMany %+v", r)
	}
	r = exec(t, s, ctx, `db.`+name+`.insertOne({_id: 3, name: "c"})`, 0)
	if r.Message != "insertedId: 3" {
		t.Errorf("insertOne %+v", r)
	}
	r = exec(t, s, ctx, `db.`+name+`.updateOne({_id: 3}, {$set: {name: "cc"}})`, 0)
	if r.Message != "matched 1, modified 1" || r.Affected != 1 {
		t.Errorf("updateOne %+v", r)
	}
	r = exec(t, s, ctx, `db.`+name+`.updateMany({_id: {$gt: 0}}, {$set: {z: 1}})`, 0)
	if r.Message != "matched 3, modified 3" {
		t.Errorf("updateMany %+v", r)
	}
	r = exec(t, s, ctx, `db.`+name+`.updateOne({_id: 9}, {$set: {name: "n"}}, {upsert: true})`, 0)
	if !strings.Contains(r.Message, "upsertedId: 9") {
		t.Errorf("upsert %+v", r)
	}
	exec(t, s, ctx, `db.`+name+`.deleteOne({_id: 9})`, 0)
	r = exec(t, s, ctx, `db.`+name+`.createIndex({name: 1}, {unique: true})`, 0)
	if r.Message != "created index name_1" {
		t.Errorf("createIndex %+v", r)
	}
	if ix, _ := s.Indexes(ctx, ref); len(ix) != 2 {
		t.Errorf("indexes after create %+v", ix)
	}
	exec(t, s, ctx, `db.`+name+`.dropIndex("name_1")`, 0)
	if ix, _ := s.Indexes(ctx, ref); len(ix) != 1 {
		t.Errorf("indexes after drop %+v", ix)
	}

	// Plan + Apply through the GUI path
	cols, err := s.Columns(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]string{}
	for _, c := range cols {
		types[c.Name] = c.Type
	}
	if types["_id"] != "int32" || types["n"] != "double" || types["when"] != "date" || types["ok"] != "bool" {
		t.Fatalf("types %v", types)
	}
	st, err := s.Plan([]db.Change{
		{Kind: db.Update, ObjRef: ref, Key: []db.KeyVal{{Col: "_id", Val: "1"}}, Set: []db.KeyVal{{Col: "name", Val: "A2"}, {Col: "n", Val: "7"}, {Col: "when", Val: "2025-05-06 07:08:09"}, {Col: "ok", Val: "false"}, {Col: "z", Val: db.Default}}},
		{Kind: db.Update, ObjRef: ref, Key: []db.KeyVal{{Col: "_id", Val: "2"}}, Set: []db.KeyVal{{Col: "name", Val: db.Null}}},
		{Kind: db.Insert, ObjRef: ref, Set: []db.KeyVal{{Col: "_id", Val: "4"}, {Col: "name", Val: "d"}, {Col: "extra", Val: `{"k": [1, 2]}`}}},
		{Kind: db.Delete, ObjRef: ref, Key: []db.KeyVal{{Col: "_id", Val: "3"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, st); err != nil {
		t.Fatal(err)
	}
	q := db.RowQuery{ObjRef: ref, Sort: []db.Sort{{Column: "_id"}}}
	rr, err := s.Rows(ctx, q)
	if err != nil || len(rr.Rows) != 3 {
		t.Fatalf("rows %+v %v", rr, err)
	}
	row := rr.Rows[0]
	if row[col(rr, "name")] != "A2" || row[col(rr, "n")] != 7.0 || row[col(rr, "ok")] != false || row[col(rr, "z")] != nil ||
		row[col(rr, "when")].(time.Time).UTC().Format(time.RFC3339) != "2025-05-06T07:08:09Z" {
		t.Errorf("updated row %+v", row)
	}
	if rr.Rows[1][col(rr, "name")] != nil || rr.Rows[2][col(rr, "extra")] != `{"k":[1,2]}` {
		t.Errorf("rows %+v", rr.Rows)
	}
	if rr.Columns[col(rr, "n")].Type != "double" {
		t.Errorf("n type %v", rr.Columns)
	}

	// ObjectId keys
	oname := tempColl(t, s, ctx)
	oref := db.ObjRef{Name: oname}
	exec(t, s, ctx, `db.`+oname+`.insertOne({a: 1})`, 0)
	rr, _ = s.Rows(ctx, db.RowQuery{ObjRef: oref})
	hex := rr.Rows[0][0].(string)
	st, err = s.Plan([]db.Change{{Kind: db.Update, ObjRef: oref, Key: []db.KeyVal{{Col: "_id", Val: hex}}, Set: []db.KeyVal{{Col: "a", Val: "2"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, st); err != nil {
		t.Fatal(err)
	}
	rr, _ = s.Rows(ctx, db.RowQuery{ObjRef: oref})
	if rr.Rows[0][col(rr, "a")] != int64(2) {
		t.Errorf("objectid update %+v", rr.Rows)
	}
	if st, _ := s.Plan([]db.Change{{Kind: db.Insert, ObjRef: oref, Set: []db.KeyVal{{Col: "a", Val: "3"}}}}); s.Apply(ctx, st) != nil {
		t.Error("insert without _id")
	}
	if n, _ := s.Count(ctx, db.RowQuery{ObjRef: oref}, false); n.N != 2 {
		t.Errorf("count after insert %+v", n)
	}

	// PlanAdmin clone / truncate / drop / rename
	clone := tempColl(t, s, ctx)
	for _, withData := range []string{"true", ""} {
		st, err = s.PlanAdmin(db.OpClone, ref, map[string]string{"new_name": clone, "with_data": withData})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Apply(ctx, st); err != nil {
			t.Fatal(err)
		}
		n, _ := s.Count(ctx, db.RowQuery{ObjRef: db.ObjRef{Name: clone}}, false)
		if (withData == "true") != (n.N == 3) {
			t.Errorf("clone with_data=%q copied %d", withData, n.N)
		}
		runAdmin(t, s, ctx, db.OpDropObject, db.ObjRef{Name: clone}, nil)
	}
	runAdmin(t, s, ctx, db.OpTruncate, ref, nil)
	if n, _ := s.Count(ctx, db.RowQuery{ObjRef: ref}, false); n.N != 0 {
		t.Errorf("truncate left %d", n.N)
	}
	runAdmin(t, s, ctx, db.OpRenameObject, ref, map[string]string{"new_name": clone})
	objs, _ := s.Objects(ctx, "")
	var gotNew, gotOld bool
	for _, o := range objs {
		gotNew = gotNew || o.Name == clone
		gotOld = gotOld || o.Name == name
	}
	if !gotNew || gotOld {
		t.Errorf("rename: new=%v old=%v", gotNew, gotOld)
	}
	runAdmin(t, s, ctx, db.OpDropObject, db.ObjRef{Name: clone}, nil)
	objs, _ = s.Objects(ctx, "")
	for _, o := range objs {
		if o.Name == clone {
			t.Error("drop did not remove collection")
		}
	}
}

// TestIntegrationRawFilterIsInert proves that filter-bar text cannot run shell code (P1-44).
func TestIntegrationRawFilterIsInert(t *testing.T) {
	s, ctx := open(t)
	name := tempColl(t, s, ctx)
	exec(t, s, ctx, `db.`+name+`.insertMany([{_id: 1, status: "paid", d: ISODate("2024-01-01")}, {_id: 2, status: "new"}])`, 0)
	ref := db.ObjRef{Name: name}
	raw := func(v string) db.RowQuery {
		return db.RowQuery{ObjRef: ref, Filters: db.FilterSet{Filters: []db.Filter{{Column: db.RawColumn, Value: v, Enabled: true}}}}
	}
	for _, b := range []string{"{a: db." + name + ".drop()}", "db.dropDatabase`x`", "new db." + name + ".drop", "{a: (function(){return 1})()}", `{$where: "1"}`,
		"{a: db." + name + ".deleteMany({})}", "{a: `${db." + name + ".drop()}`}"} {
		if _, err := s.Rows(ctx, raw(b)); err == nil {
			t.Errorf("%s: accepted", b)
		}
		if _, err := s.Count(ctx, raw(b), false); err == nil {
			t.Errorf("count %s: accepted", b)
		}
	}
	if n, _ := s.Count(ctx, db.RowQuery{ObjRef: ref}, false); n.N != 2 {
		t.Errorf("documents changed: %d", n.N)
	}
	if names, _ := s.client.Database("webtuidb").ListCollectionNames(ctx, bson.D{}); !contains(names, "customers") || !contains(names, name) {
		t.Errorf("collections changed: %v", names)
	}
	r, err := s.Rows(ctx, raw(`{status: 'paid', _id: {$ne: ObjectId('507f1f77bcf86cd799439011')}, d: ISODate('2024-01-01')}`))
	if err != nil || len(r.Rows) != 1 {
		t.Errorf("relaxed filter: %+v %v", r, err)
	}
	if r, err := s.Rows(ctx, raw(`{status: 'paid', _id: ObjectId('507f1f77bcf86cd799439011'), d: ISODate('2024-01-01')}`)); err != nil || len(r.Rows) != 0 {
		t.Errorf("relaxed filter with ObjectId: %+v %v", r, err)
	}
}

func TestIntegrationCursorsAndTypes(t *testing.T) {
	s, ctx := open(t)
	name := tempColl(t, s, ctx)
	exec(t, s, ctx, `db.`+name+`.insertMany([{_id: 1, n: 1}, {_id: 2, n: 5}, {_id: 3, n: 3}, {_id: 4, n: 2}, {_id: 5, n: 4}])`, 0)

	// .sort()/.skip() on an aggregate cursor
	r := exec(t, s, ctx, `db.`+name+`.aggregate([{$match: {}}]).skip(1).sort({n: -1}).limit(2)`, 0)
	if len(r.Rows) != 2 || r.Rows[0][col(r, "n")] != int64(4) || r.Rows[1][col(r, "n")] != int64(3) {
		t.Errorf("aggregate sort/skip %+v", r.Rows)
	}

	// raw find command: later batches are read, and no server cursor is left open
	cmd := `{"find": "` + name + `", "sort": {"_id": 1}, "batchSize": 2}`
	if r := exec(t, s, ctx, cmd, 0); len(r.Rows) != 5 || r.Truncated {
		t.Errorf("all batches %d %v", len(r.Rows), r.Truncated)
	}
	if r := exec(t, s, ctx, cmd, 3); len(r.Rows) != 3 || !r.Truncated || r.Rows[2][0] != int64(3) {
		t.Errorf("limited %+v", r)
	}
	if r := exec(t, s, ctx, cmd, 5); len(r.Rows) != 5 || r.Truncated {
		t.Errorf("exact limit %d %v", len(r.Rows), r.Truncated)
	}
	if r := exec(t, s, ctx, cmd, 1); len(r.Rows) != 1 || !r.Truncated { // first batch suffices: the cursor must be killed
		t.Errorf("limit 1 %+v", r)
	}
	cur, err := s.client.Database("admin").Aggregate(ctx, bson.A{
		bson.D{{Key: "$currentOp", Value: bson.D{{Key: "idleCursors", Value: true}}}},
		bson.D{{Key: "$match", Value: bson.D{{Key: "cursor.originatingCommand.find", Value: name}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var left []bson.D
	if err := cur.All(ctx, &left); err != nil || len(left) != 0 {
		t.Errorf("open cursors %v %v", left, err)
	}

	// Decimal128 and Binary cells round-trip through a grid edit
	exec(t, s, ctx, `db.`+name+`.insertOne({_id: 9, dec: NumberDecimal("1.10"), bin: BinData(0, "AAE=")})`, 0)
	ref := db.ObjRef{Name: name}
	if _, err := s.Columns(ctx, ref); err != nil {
		t.Fatal(err)
	}
	st, err := s.Plan([]db.Change{{Kind: db.Update, ObjRef: ref, Key: []db.KeyVal{{Col: "_id", Val: "9"}},
		Set: []db.KeyVal{{Col: "dec", Val: "12345678901234567890.123456789"}, {Col: "bin", Val: `\x0203ff`}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, st); err != nil {
		t.Fatal(err)
	}
	var got bson.D
	if err := s.client.Database("webtuidb").Collection(name).FindOne(ctx, bson.D{{Key: "_id", Value: 9}}).Decode(&got); err != nil {
		t.Fatal(err)
	}
	dec, _ := lookup(got, "dec")
	bin, _ := lookup(got, "bin")
	if d, ok := dec.(bson.Decimal128); !ok || d.String() != "12345678901234567890.123456789" {
		t.Errorf("decimal %#v", dec)
	}
	if b, ok := bin.(bson.Binary); !ok || string(b.Data) != "\x02\x03\xff" {
		t.Errorf("binary %#v", bin)
	}
}

func runAdmin(t *testing.T, s *session, ctx context.Context, op db.AdminOp, ref db.ObjRef, opts map[string]string) {
	t.Helper()
	st, err := s.PlanAdmin(op, ref, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, st); err != nil {
		t.Fatalf("%s: %v", st[0].Text, err)
	}
}

func TestIntegrationDatabaseAdmin(t *testing.T) {
	s, ctx := open(t)
	dbn := "webtuidb_test_" + bson.NewObjectID().Hex()[16:]
	t.Cleanup(func() { s.client.Database(dbn).Drop(context.Background()) })
	runAdmin(t, s, ctx, db.OpCreateDatabase, db.ObjRef{Name: dbn}, map[string]string{"collection": "first"})
	if dbs, _ := s.Databases(ctx); !contains(dbs, dbn) {
		t.Errorf("database not created: %v", dbs)
	}
	runAdmin(t, s, ctx, db.OpDropDatabase, db.ObjRef{Name: dbn}, nil)
	if dbs, _ := s.Databases(ctx); contains(dbs, dbn) {
		t.Errorf("database not dropped")
	}
	if s.CurrentDatabase() != "webtuidb" {
		t.Errorf("current database changed to %s", s.CurrentDatabase())
	}
}

func TestIntegrationCancel(t *testing.T) {
	s, _ := open(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	start := time.Now()
	_, err := s.Exec(ctx, `db.customers.find({$where: "sleep(5000) || true"}).toArray()`, 0)
	if err == nil || time.Since(start) > 1500*time.Millisecond {
		t.Errorf("cancel: err=%v after %v", err, time.Since(start))
	}
	ctx, cancel = context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	start = time.Now()
	if _, err = s.Exec(ctx, `while(true){}`, 0); err == nil || time.Since(start) > 1500*time.Millisecond {
		t.Errorf("js cancel: err=%v after %v", err, time.Since(start))
	}
	if err := s.Ping(context.Background()); err != nil {
		t.Errorf("session unusable after cancel: %v", err)
	}
}

func TestIntegrationProcesses(t *testing.T) {
	s, ctx := open(t)
	if _, err := s.Processes(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Kill(ctx, "x"); err == nil {
		t.Error("bad id accepted")
	}
}
