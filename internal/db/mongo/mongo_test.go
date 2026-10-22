package mongo

import (
	"context"
	"crypto/tls"
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"tuidb/internal/config"
	"tuidb/internal/db"
)

func TestCell(t *testing.T) {
	oid := bson.NewObjectID()
	tm := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	cases := []struct {
		in, want any
	}{
		{nil, nil}, {int32(3), int64(3)}, {int64(4), int64(4)}, {2.5, 2.5}, {"s", "s"}, {true, true},
		{oid, oid.Hex()}, {bson.NewDateTimeFromTime(tm), tm},
		{bson.D{{Key: "a", Value: int32(1)}}, `{"a":1}`},
		{bson.A{int32(1), "x"}, `[1,"x"]`},
	}
	for _, c := range cases {
		got := cell(c.in)
		if gt, ok := got.(time.Time); ok {
			got = gt.UTC()
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("cell(%#v) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

func TestJSLit(t *testing.T) {
	oid, _ := bson.ObjectIDFromHex("5f0000000000000000000001")
	d := bson.D{{Key: "_id", Value: oid}, {Key: "n", Value: 5.0}, {Key: "m", Value: int64(7)}, {Key: "s", Value: `a"b<`}, {Key: "arr", Value: bson.A{int32(1), nil}}, {Key: "x", Value: rawJS("new Date()")}}
	want := `{"_id": {"$oid":"5f0000000000000000000001"}, "n": {"$numberDouble": "5.0"}, "m": {"$numberLong": "7"}, "s": "a\"b<", "arr": [1, null], "x": new Date()}`
	if got := jsLit(d); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestCoerceSet(t *testing.T) {
	oid, _ := bson.ObjectIDFromHex("5f0000000000000000000001")
	cases := []struct {
		v    any
		typ  string
		want any
		bad  bool
	}{
		{"5", "int32", int32(5), false},
		{"5", "int64", int64(5), false},
		{"1.5", "double", 1.5, false},
		{"true", "bool", true, false},
		{"abc", "int32", nil, true},
		{"5f0000000000000000000001", "objectId", oid, false},
		{"7", "string", "7", false},
		{"7", "mixed", int32(7), false},
		{"hello", "mixed", "hello", false},
		{"null", "", nil, false},
		{`{"a":1}`, "object", bson.D{{Key: "a", Value: int32(1)}}, false},
		{db.Null, "string", nil, false},
		{db.Empty, "int32", "", false},
		{nil, "string", nil, false},
	}
	for _, c := range cases {
		got, err := coerceSet(c.v, c.typ, "f")
		if (err != nil) != c.bad || !reflect.DeepEqual(got, c.want) {
			t.Errorf("coerceSet(%#v,%s) = %#v, %v; want %#v bad=%v", c.v, c.typ, got, err, c.want, c.bad)
		}
	}
	d, err := coerceSet("2024-01-02 03:04:05", "date", "f")
	if err != nil || d.(bson.DateTime).Time().UTC().Format(time.RFC3339) != "2024-01-02T03:04:05Z" {
		t.Errorf("date: %v %v", d, err)
	}
	if v, _ := coerceSet(db.Expr("NOW()"), "date", "f"); v != rawJS("new Date()") {
		t.Errorf("NOW(): %v", v)
	}
}

func TestLikeRegex(t *testing.T) {
	cases := map[string]string{"a%": `^a.*$`, "_x": `^.x$`, `100\%`: `^100%$`, "a.b": `^a\.b$`}
	for in, want := range cases {
		if got := likeRegex(in); got != want {
			t.Errorf("likeRegex(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildFilter(t *testing.T) {
	ci := &colInfo{names: []string{"_id", "name", "age"}, types: map[string]string{"_id": "int32", "name": "string", "age": "int32", "m": "mixed"}}
	raw := func(s string) (bson.D, error) { return bson.D{{Key: "raw", Value: s}}, nil }
	f := func(col, op, val string) db.Filter { return db.Filter{Column: col, Op: op, Value: val, Enabled: true} }
	cases := []struct {
		name string
		fs   db.FilterSet
		want bson.D
	}{
		{"none", db.FilterSet{}, bson.D{}},
		{"eq coerced", db.FilterSet{Filters: []db.Filter{f("age", "=", "5")}}, bson.D{{Key: "age", Value: bson.D{{Key: "$eq", Value: int32(5)}}}}},
		{"eq bad stays string", db.FilterSet{Filters: []db.Filter{f("age", "=", "x")}}, bson.D{{Key: "age", Value: bson.D{{Key: "$eq", Value: "x"}}}}},
		{"mixed eq", db.FilterSet{Filters: []db.Filter{f("m", "=", "5")}}, bson.D{{Key: "m", Value: bson.D{{Key: "$in", Value: bson.A{int64(5), "5"}}}}}},
		{"in", db.FilterSet{Filters: []db.Filter{f("age", "IN", "1, '2'")}}, bson.D{{Key: "age", Value: bson.D{{Key: "$in", Value: bson.A{int32(1), int32(2)}}}}}},
		{"null", db.FilterSet{Filters: []db.Filter{f("name", "IS NULL", "")}}, bson.D{{Key: "name", Value: nil}}},
		{"between", db.FilterSet{Filters: []db.Filter{f("age", "BETWEEN", "1 AND 3")}}, bson.D{{Key: "age", Value: bson.D{{Key: "$gte", Value: int32(1)}, {Key: "$lte", Value: int32(3)}}}}},
		{"contains", db.FilterSet{Filters: []db.Filter{f("name", "Contains", "a.b")}}, bson.D{{Key: "name", Value: bson.Regex{Pattern: `a\.b`, Options: "i"}}}},
		{"raw", db.FilterSet{Filters: []db.Filter{f(db.RawColumn, "", "x")}}, bson.D{{Key: "raw", Value: "x"}}},
		{"or", db.FilterSet{Or: true, Filters: []db.Filter{f("age", "=", "1"), f("age", "=", "2")}}, bson.D{{Key: "$or", Value: bson.A{
			bson.D{{Key: "age", Value: bson.D{{Key: "$eq", Value: int32(1)}}}}, bson.D{{Key: "age", Value: bson.D{{Key: "$eq", Value: int32(2)}}}}}}}},
		{"any", db.FilterSet{Filters: []db.Filter{f(db.AnyColumn, "=", "1")}}, bson.D{{Key: "$or", Value: bson.A{
			bson.D{{Key: "_id", Value: bson.D{{Key: "$eq", Value: int32(1)}}}},
			bson.D{{Key: "name", Value: bson.D{{Key: "$eq", Value: "1"}}}},
			bson.D{{Key: "age", Value: bson.D{{Key: "$eq", Value: int32(1)}}}}}}}},
	}
	for _, c := range cases {
		got, err := buildFilter(c.fs, ci, raw)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %#v err %v\nwant %#v", c.name, got, err, c.want)
		}
	}
	if _, err := buildFilter(db.FilterSet{Filters: []db.Filter{f("a", "BETWEEN", "1")}}, ci, raw); err == nil {
		t.Error("BETWEEN without AND must fail")
	}
}

func TestTLSConfig(t *testing.T) {
	for _, m := range []string{"", "disable", "prefer"} {
		if c, err := tlsConfig(config.Connection{SSLMode: m}); c != nil || err != nil {
			t.Errorf("%q: %v %v", m, c, err)
		}
	}
	c, err := tlsConfig(config.Connection{SSLMode: "require", Host: "h"})
	if err != nil || !c.InsecureSkipVerify || c.ServerName != "h" {
		t.Errorf("require: %+v %v", c, err)
	}
	c, _ = tlsConfig(config.Connection{SSLMode: "verify-full"})
	if c.InsecureSkipVerify || c.MinVersion != tls.VersionTLS12 {
		t.Errorf("verify-full: %+v", c)
	}
	c, _ = tlsConfig(config.Connection{SSLMode: "verify-ca"})
	if !c.InsecureSkipVerify || c.VerifyConnection == nil {
		t.Errorf("verify-ca: %+v", c)
	}
	if _, err := tlsConfig(config.Connection{SSLMode: "bogus"}); err == nil {
		t.Error("bogus mode must fail")
	}
	if _, err := tlsConfig(config.Connection{SSLMode: "require", SSLCA: "/nonexistent"}); err == nil {
		t.Error("missing CA must fail")
	}
}

func TestShellValues(t *testing.T) {
	s := &session{cur: "x"}
	sh, err := s.newShell(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Close()
	f, err := sh.eval(`[ObjectId('5f0000000000000000000001'), ISODate('2024-01-02T03:04:05Z'), NumberLong(5), NumberInt(6), /ab/i, {a: [1, 2.5]}]`)
	if err != nil {
		t.Fatal(err)
	}
	arr := f.V.(bson.A)
	if _, ok := arr[0].(bson.ObjectID); !ok {
		t.Errorf("objectid: %#v", arr[0])
	}
	if dt, ok := arr[1].(bson.DateTime); !ok || dt.Time().UTC().Year() != 2024 {
		t.Errorf("date: %#v", arr[1])
	}
	if arr[2] != int64(5) || arr[3] != int32(6) {
		t.Errorf("numbers: %#v %#v", arr[2], arr[3])
	}
	if r, ok := arr[4].(bson.Regex); !ok || r.Pattern != "ab" || r.Options != "i" {
		t.Errorf("regex: %#v", arr[4])
	}
	if _, err := sh.eval(`db.`); err == nil {
		t.Error("syntax error expected")
	}
	if _, err := sh.eval(`foo.bar`); err == nil {
		t.Error("reference error expected")
	}
}

func TestShellInterrupt(t *testing.T) {
	s := &session{cur: "x"}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sh, _ := s.newShell(ctx)
	defer sh.Close()
	start := time.Now()
	if _, err := sh.eval(`while(true){}`); err == nil || time.Since(start) > 2*time.Second {
		t.Errorf("expected interrupt, got %v after %v", err, time.Since(start))
	}
}

func TestPlan(t *testing.T) {
	s := &session{cur: "d", cache: map[string]*colInfo{"d.customers": {types: map[string]string{"_id": "int32", "n": "double", "name": "string", "when": "date"}}}}
	ref := db.ObjRef{Name: "customers"}
	st, err := s.Plan([]db.Change{
		{Kind: db.Update, ObjRef: ref, Key: []db.KeyVal{{Col: "_id", Val: "1"}}, Set: []db.KeyVal{{Col: "name", Val: "x"}, {Col: "n", Val: "5"}, {Col: "when", Val: "2024-01-02T03:04:05Z"}, {Col: "gone", Val: db.Default}, {Col: "e", Val: db.Empty}}},
		{Kind: db.Delete, ObjRef: ref, Key: []db.KeyVal{{Col: "_id", Val: "2"}}},
		{Kind: db.Insert, ObjRef: ref, Set: []db.KeyVal{{Col: "_id", Val: "9"}, {Col: "name", Val: db.Null}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`db.customers.updateOne({"_id": 1}, {"$set": {"name": "x", "n": {"$numberDouble": "5.0"}, "when": {"$date":"2024-01-02T03:04:05Z"}, "e": ""}, "$unset": {"gone": ""}})`,
		`db.customers.deleteOne({"_id": 2})`,
		`db.customers.insertOne({"_id": 9, "name": null})`,
	}
	for i, w := range want {
		if st[i].Text != w || st[i].Display != w {
			t.Errorf("stmt %d:\n got %s\nwant %s", i, st[i].Text, w)
		}
	}
	if _, err := s.Plan([]db.Change{{Kind: db.Delete, ObjRef: ref}}); err == nil {
		t.Error("delete without key must fail")
	}
	if _, err := s.Plan([]db.Change{{Kind: db.Update, ObjRef: ref, Key: []db.KeyVal{{Col: "_id", Val: "1"}}, Set: []db.KeyVal{{Col: "n", Val: "zz"}}}}); err == nil {
		t.Error("bad double must fail")
	}
}

func TestPlanAdmin(t *testing.T) {
	s := &session{}
	ref := db.ObjRef{Name: "my coll"}
	cases := []struct {
		op   db.AdminOp
		opts map[string]string
		want string
	}{
		{db.OpDropObject, nil, `db.getCollection("my coll").drop()`},
		{db.OpTruncate, nil, `db.getCollection("my coll").deleteMany({})`},
		{db.OpClone, map[string]string{"new_name": "c2", "with_data": "true"}, `db.getCollection("my coll").aggregate([{"$match": {}}, {"$out": "c2"}])`},
		{db.OpClone, map[string]string{"new_name": "c2"}, `db.createCollection("c2")`},
		{db.OpRenameObject, map[string]string{"new_name": "c2"}, `db.getCollection("my coll").renameCollection("c2")`},
		{db.OpDropDatabase, nil, `db.getSiblingDB("my coll").dropDatabase()`},
		{db.OpCreateDatabase, map[string]string{"collection": "c"}, `db.getSiblingDB("my coll").createCollection("c")`},
	}
	for _, c := range cases {
		st, err := s.PlanAdmin(c.op, ref, c.opts)
		if err != nil || st[0].Text != c.want {
			t.Errorf("%s: %v %v, want %s", c.op, st, err, c.want)
		}
	}
	if _, err := s.PlanAdmin(db.OpCreateDatabase, ref, nil); err == nil {
		t.Error("create_database without collection must fail")
	}
	if _, err := s.PlanAdmin(db.OpRenameDatabase, ref, map[string]string{"new_name": "x"}); err == nil {
		t.Error("rename_database must fail")
	}
	if _, err := s.PlanTable(nil, &db.TableDef{}); err == nil {
		t.Error("PlanTable must fail")
	}
}

func TestRenderTypes(t *testing.T) {
	docs := []bson.D{
		{{Key: "a", Value: int32(1)}, {Key: "_id", Value: int32(1)}, {Key: "m", Value: "s"}, {Key: "n", Value: nil}},
		{{Key: "_id", Value: int32(2)}, {Key: "m", Value: int32(2)}, {Key: "n", Value: "x"}},
	}
	r := render(docs, nil, nil)
	got := map[string]string{}
	for _, c := range r.Columns {
		got[c.Name] = c.Type
	}
	if r.Columns[0].Name != "_id" || got["m"] != "mixed" || got["n"] != "string" || got["a"] != "int32" || len(r.Docs) != 2 {
		t.Errorf("%+v %+v", r.Columns, got)
	}
}
