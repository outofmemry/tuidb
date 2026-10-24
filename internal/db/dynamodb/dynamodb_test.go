package dynamodb

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"tuidb/internal/config"
	"tuidb/internal/db"
)

func meta() *tableMeta {
	return &tableMeta{name: "t", hash: "pk", rng: "sk", attrs: map[string]string{"pk": "S", "sk": "N", "age": "N", "name": "S", "tags": "SS", "doc": "M", "n": "mixed"},
		names: []string{"pk", "sk", "age", "doc", "n", "name", "tags"}, indexOf: map[string][]string{}}
}

func N(s string) types.AttributeValue { return &types.AttributeValueMemberN{Value: s} }
func S(s string) types.AttributeValue { return &types.AttributeValueMemberS{Value: s} }

func TestCellNormalisation(t *testing.T) {
	for _, c := range []struct {
		av   types.AttributeValue
		want any
	}{
		{S("x"), "x"}, {N("42"), int64(42)}, {N("-7"), int64(-7)}, {N("1.50"), "1.50"}, {N("12345678901234567890123"), "12345678901234567890123"},
		{&types.AttributeValueMemberBOOL{Value: true}, true}, {&types.AttributeValueMemberNULL{Value: true}, nil},
		{&types.AttributeValueMemberSS{Value: []string{"a", "b"}}, `["a","b"]`},
		{&types.AttributeValueMemberNS{Value: []string{"1", "2.5"}}, `[1,2.5]`},
		{&types.AttributeValueMemberBS{Value: [][]byte{{1}, {2}}}, `["AQ==","Ag=="]`},
		{&types.AttributeValueMemberL{Value: []types.AttributeValue{N("1"), S("<&>"), &types.AttributeValueMemberSS{Value: []string{"q"}}}}, `[1,"<&>",{"$ss":["q"]}]`},
		{&types.AttributeValueMemberM{Value: map[string]types.AttributeValue{"b": N("0.10"), "a": &types.AttributeValueMemberB{Value: []byte{1, 2}}}}, `{"a":{"$binary":"AQI="},"b":0.10}`},
	} {
		if got := cell(c.av); got != c.want {
			t.Errorf("cell(%v) = %#v, want %#v", c.av, got, c.want)
		}
	}
	if b, ok := cell(&types.AttributeValueMemberB{Value: []byte{9}}).([]byte); !ok || b[0] != 9 {
		t.Error("binary cell")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	text := `{"a":[1,"x",{"b":null,"c":true}],"bin":{"$binary":"AQI="},"bs":{"$bs":["AQ=="]},"e":{},"n":123456789012345678901234567890.123456789,"nums":{"$ns":[1,2.50]},"set":{"$ss":["p","q"]}}`
	j, err := parseJSON(text)
	if err != nil {
		t.Fatal(err)
	}
	av, err := fromJSON(j)
	if err != nil {
		t.Fatal(err)
	}
	m := av.(*types.AttributeValueMemberM).Value
	for k, want := range map[string]string{"a": "L", "bin": "B", "n": "N", "set": "SS", "nums": "NS", "bs": "BS", "e": "M"} {
		if typeName(m[k]) != want {
			t.Errorf("%s is %s, want %s", k, typeName(m[k]), want)
		}
	}
	if m["n"].(*types.AttributeValueMemberN).Value != "123456789012345678901234567890.123456789" {
		t.Error("number lost digits")
	}
	if got := jsonText(av, false); got != text { // keys sorted, numbers exact
		t.Errorf("round trip:\n%s\n%s", got, text)
	}
	if got := itemJSON(m); !strings.Contains(got, `"set":{"$ss":["p","q"]}`) {
		t.Errorf("item json %s", got)
	}
	for _, bad := range []string{`{"$ss": []}`, `{"$ss": [1]}`, `{"$ns": ["x"]}`, `{"$ss": ["a","a"]}`, `{"$binary": "!!"}`, `{"$bs": [1]}`, `{"$ss": "a"}`} {
		j, err := parseJSON(bad)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fromJSON(j); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	for _, bad := range []string{``, `{`, `1 2`, `{} x`} {
		if _, err := parseJSON(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	if _, err := fromJSON(json2("1e999999999999")); err != nil { // DynamoDB, not us, rejects the range
		t.Errorf("range check belongs to the server: %v", err)
	}
}

func json2(s string) any { j, _ := parseJSON(s); return j }

func TestCoerce(t *testing.T) {
	ok := func(v any, typ, want string) {
		t.Helper()
		av, err := coerce(v, typ, "c")
		if err != nil {
			t.Errorf("coerce(%#v, %s): %v", v, typ, err)
			return
		}
		if got := typeName(av) + ":" + literal(av); got != want {
			t.Errorf("coerce(%#v, %q) = %s, want %s", v, typ, got, want)
		}
	}
	bad := func(v any, typ string) {
		t.Helper()
		if av, err := coerce(v, typ, "c"); err == nil {
			t.Errorf("coerce(%#v, %s) = %v, want an error", v, typ, av)
		}
	}
	ok("hi", "S", "S:'hi'")
	ok("123", "S", "S:'123'") // a string column keeps digits as text
	ok("12", "N", "N:12")
	ok(" 1.5e3 ", "N", "N:1.5e3")
	ok(int64(5), "N", "N:5")
	ok(float64(0.1), "N", "N:0.1")
	ok(int64(5), "S", "N:5")
	ok("true", "BOOL", "BOOL:true")
	ok("NO", "BOOL", "BOOL:false")
	ok(`\xdeadbeef`, "B", `B:'\xdeadbeef'`)
	ok("AQI=", "B", `B:'\x0102'`)
	ok([]byte{1}, "", `B:'\x01'`)
	ok(nil, "S", "NULL:NULL")
	ok(db.Null, "N", "NULL:NULL")
	ok(db.Empty, "S", "S:''")
	ok("", "NULL", "NULL:NULL")
	ok("[1, 2]", "L", "L:[1, 2]")
	ok(`{"a": "b"}`, "M", "M:{'a': 'b'}")
	ok(`["x","y"]`, "SS", "SS:<<'x', 'y'>>")
	ok(`[1, 2.5]`, "NS", "NS:<<1, 2.5>>")
	ok(`["AQ=="]`, "BS", `BS:<<'\x01'>>`)
	ok("12", "", "N:12")
	ok("true", "", "BOOL:true")
	ok("null", "mixed", "NULL:NULL")
	ok(`[1,"a"]`, "", "L:[1, 'a']")
	ok(`"quoted"`, "", "S:'quoted'")
	ok("O'Brien", "", "S:'O''Brien'")
	ok("12abc", "", "S:'12abc'")
	ok(true, "", "BOOL:true")
	bad("abc", "N")
	bad("1,5", "N")
	bad("", "N")
	bad("maybe", "BOOL")
	bad("zz", "B")
	bad(`\xzz`, "B")
	bad(`{"a":1}`, "L")
	bad(`[1]`, "M")
	bad(`[]`, "SS")
	bad(`[1]`, "SS")
	bad(`["a"]`, "NS")
	bad(`not json`, "SS")
	bad(db.Expr("age + 1"), "N")
	bad(struct{}{}, "S")
	if av, _ := coerce(db.Expr("NOW()"), "N", "c"); typeName(av) != "N" {
		t.Error("NOW() in a number column is epoch seconds")
	}
	if av, _ := coerce(db.Expr("NOW()"), "S", "c"); !strings.Contains(av.(*types.AttributeValueMemberS).Value, "T") {
		t.Error("NOW() in a string column is ISO 8601")
	}
}

func TestLiteral(t *testing.T) {
	m := &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{"it's": S("o'k"), "a": &types.AttributeValueMemberL{Value: []types.AttributeValue{N("1"), &types.AttributeValueMemberBOOL{Value: false}, &types.AttributeValueMemberNULL{Value: true}}}}}
	if got, want := literal(m), `{'a': [1, false, NULL], 'it''s': 'o''k'}`; got != want {
		t.Errorf("literal %s, want %s", got, want)
	}
}

func filt(col, op, val string) db.Filter {
	return db.Filter{Column: col, Op: op, Value: val, Enabled: true}
}

func TestBuildRead(t *testing.T) {
	m := meta()
	for _, c := range []struct {
		name   string
		fs     db.FilterSet
		query  bool
		key    string
		filter string
	}{
		{"no filter", db.FilterSet{}, false, "", ""},
		{"pk only", db.FilterSet{Filters: []db.Filter{filt("pk", "=", "a")}}, true, "#n0 = :v0", ""},
		{"pk and sort", db.FilterSet{Filters: []db.Filter{filt("pk", "=", "a"), filt("sk", ">=", "5")}}, true, "#n0 = :v0 AND #n1 >= :v1", ""},
		{"pk sort between", db.FilterSet{Filters: []db.Filter{filt("sk", "BETWEEN", "1 AND 9"), filt("pk", "=", "a")}}, true, "#n0 = :v0 AND #n1 BETWEEN :v1 AND :v2", ""},
		{"pk plus other", db.FilterSet{Filters: []db.Filter{filt("pk", "=", "a"), filt("age", ">", "3")}}, true, "#n0 = :v0", "#n1 > :v1"},
		{"pk not equal", db.FilterSet{Filters: []db.Filter{filt("pk", "<>", "a")}}, false, "", "#n0 <> :v0"},
		{"pk twice", db.FilterSet{Filters: []db.Filter{filt("pk", "=", "a"), filt("pk", "=", "b")}}, false, "", "#n0 = :v0 AND #n0 = :v1"},
		{"pk or", db.FilterSet{Or: true, Filters: []db.Filter{filt("pk", "=", "a"), filt("age", "=", "1")}}, false, "", "#n0 = :v0 OR #n1 = :v1"},
		{"two sort conds", db.FilterSet{Filters: []db.Filter{filt("pk", "=", "a"), filt("sk", ">", "1"), filt("sk", "<", "9")}}, false, "", "#n0 = :v0 AND #n1 > :v1 AND #n1 < :v2"},
		{"sort cond not allowed in key condition", db.FilterSet{Filters: []db.Filter{filt("pk", "=", "a"), filt("sk", "<>", "1")}}, false, "", "#n0 = :v0 AND #n1 <> :v1"},
		{"sort only", db.FilterSet{Filters: []db.Filter{filt("sk", "=", "1")}}, false, "", "#n0 = :v0"},
		{"raw", db.FilterSet{Filters: []db.Filter{filt("pk", "=", "a"), filt(db.RawColumn, "", "age < 3")}}, false, "", "#n0 = :v0 AND (#n1 < :v1)"},
		{"disabled", db.FilterSet{Filters: []db.Filter{{Column: "pk", Op: "=", Value: "a"}}}, false, "", ""},
		{"prefix on sort key is not N", db.FilterSet{Filters: []db.Filter{filt("pk", "=", "a"), filt("name", "Has prefix", "x")}}, true, "#n0 = :v0", "begins_with(#n1, :v1)"},
		{"any with query skips keys", db.FilterSet{Filters: []db.Filter{filt("pk", "=", "a"), filt(db.AnyColumn, "Contains", "x")}}, true, "#n0 = :v0", "(contains(#n1, :v1) OR contains(#n2, :v2) OR contains(#n3, :v3))"},
	} {
		p, err := buildRead(m, c.fs, nil, nil)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if p.query != c.query || p.keyCond != c.key || p.filter != c.filter {
			t.Errorf("%s: query=%v key=%q filter=%q, want %v %q %q", c.name, p.query, p.keyCond, p.filter, c.query, c.key, c.filter)
		}
		for ph := range p.e.names { // every placeholder must be used, or DynamoDB refuses the request
			if !strings.Contains(p.keyCond+" "+p.filter+" "+p.proj, ph+" ") && !strings.Contains(p.keyCond+" "+p.filter+" "+p.proj, ph+")") && !strings.HasSuffix(p.keyCond+" "+p.filter+" "+p.proj, ph) && !strings.Contains(p.keyCond+" "+p.filter+" "+p.proj, ph+",") {
				t.Errorf("%s: placeholder %s is unused", c.name, ph)
			}
		}
	}
	// types: N compares numerically, S stays text, the same literal is typed per column
	p, _ := buildRead(m, db.FilterSet{Filters: []db.Filter{filt("age", "=", "12"), filt("name", "=", "12")}}, nil, nil)
	if p.e.vals[":v0"].(*types.AttributeValueMemberN).Value != "12" || p.e.vals[":v1"].(*types.AttributeValueMemberS).Value != "12" {
		t.Errorf("values %v", p.e.vals)
	}
	// sort direction and projection
	p, _ = buildRead(m, db.FilterSet{Filters: []db.Filter{filt("pk", "=", "a")}}, []string{"pk", "age"}, []db.Sort{{Column: "sk", Desc: true}})
	if p.forward || p.proj != "#n0, #n1" {
		t.Errorf("forward=%v proj=%q", p.forward, p.proj)
	}
	p, _ = buildRead(m, db.FilterSet{}, nil, []db.Sort{{Column: "sk", Desc: true}})
	if !p.forward {
		t.Error("a Scan has no server-side order")
	}
	// IN, NOT IN, IS NULL, set contains
	p, _ = buildRead(m, db.FilterSet{Filters: []db.Filter{filt("age", "IN", "1, '2', 3"), filt("age", "NOT IN", "4"), filt("name", "IS NULL", ""), filt("tags", "Contains", "x"), filt("age", "NOT BETWEEN", "1 and 5")}}, nil, nil)
	want := "#n0 IN (:v0, :v1, :v2) AND NOT (#n0 IN (:v3)) AND (attribute_not_exists(#n1) OR attribute_type(#n1, :v4)) AND contains(#n2, :v5) AND NOT (#n0 BETWEEN :v6 AND :v7)"
	if p.filter != want {
		t.Errorf("filter %q\nwant   %q", p.filter, want)
	}
	if _, ok := p.e.vals[":v5"].(*types.AttributeValueMemberS); !ok {
		t.Error("contains on a string set takes a string")
	}
	for _, f := range []db.Filter{filt("age", "=", "x"), filt("age", "LIKE", "1"), filt("name", "Has suffix", "x"), filt("age", "BETWEEN", "1"), filt("age", "~", "1"), filt("age", "IN", "1,x")} {
		if _, err := buildRead(m, db.FilterSet{Filters: []db.Filter{f}}, nil, nil); err == nil {
			t.Errorf("filter %+v accepted", f)
		}
	}
}

func TestRawCond(t *testing.T) {
	for in, want := range map[string]string{
		`age < 40 AND begins_with(name, 'A')`:      `(#n0 < :v0 AND begins_with(#n1, :v1))`,
		`age>-5 or not attribute_exists(doc.city)`: `(#n0>:v0 OR NOT attribute_exists(#n1.#n2))`,
		`tags[0] = 'it''s' AND flag = true`:        `(#n0[0] = :v0 AND #n1 = :v1)`,
		`size(name) between 2 and 9`:               `(size(#n0) BETWEEN :v0 AND :v1)`,
		`x = null`:                                 `(#n0 = :v0)`,
		`a1 = 1.5e3`:                               `(#n0 = :v0)`,
		`age in (1, 2)`:                            `(#n0 IN (:v0, :v1))`,
		`end = 3`:                                  `(#n0 = :v0)`, // reserved words are fine: names go through placeholders
	} {
		e := newExpr()
		got, err := rawCond(e, in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		// dotted paths: the second segment is a word after '.', registered as its own name
		got = strings.ReplaceAll(got, "#n1.#n2", "#n1.#n2")
		if got != want {
			t.Errorf("%q = %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{`name = 'open`, `a[1 = 2`} {
		if _, err := rawCond(newExpr(), in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func testSession() *session {
	return &session{metas: map[string]*tableMeta{"t": meta()}}
}

func TestPlanStatements(t *testing.T) {
	s := testSession()
	r := db.ObjRef{Name: "t", Kind: db.KindTable}
	kv := func(c string, v any) db.KeyVal { return db.KeyVal{Col: c, Val: v} }
	for _, c := range []struct {
		name   string
		ch     db.Change
		text   string
		disp   string
		nargs  int
		argsOf []string
	}{
		{"insert", db.Change{Kind: db.Insert, ObjRef: r, Set: []db.KeyVal{kv("pk", "a"), kv("sk", "5"), kv("name", "O'Hara"), kv("age", db.Null), kv("tags", `["x"]`), kv("gone", db.Default)}},
			`INSERT INTO "t" VALUE {'pk': ?, 'sk': ?, 'name': ?, 'age': ?, 'tags': ?}`, `INSERT INTO "t" VALUE {'pk': 'a', 'sk': 5, 'name': 'O''Hara', 'age': NULL, 'tags': <<'x'>>}`, 5, []string{"S", "N", "S", "NULL", "SS"}},
		{"update", db.Change{Kind: db.Update, ObjRef: r, Key: []db.KeyVal{kv("pk", "a"), kv("sk", int64(5))}, Set: []db.KeyVal{kv("age", "7"), kv("name", db.Default), kv("doc", `{"k": [1]}`), kv("my\"col", "v")}},
			`UPDATE "t" SET "age" = ? SET "doc" = ? SET "my""col" = ? REMOVE "name" WHERE "pk" = ? AND "sk" = ?`,
			`UPDATE "t" SET "age" = 7 SET "doc" = {'k': [1]} SET "my""col" = 'v' REMOVE "name" WHERE "pk" = 'a' AND "sk" = 5`, 5, []string{"N", "M", "S", "S", "N"}},
		{"delete", db.Change{Kind: db.Delete, ObjRef: r, Key: []db.KeyVal{kv("sk", int64(5)), kv("pk", "a")}},
			`DELETE FROM "t" WHERE "pk" = ? AND "sk" = ?`, `DELETE FROM "t" WHERE "pk" = 'a' AND "sk" = 5`, 2, []string{"S", "N"}},
	} {
		st, err := s.Plan([]db.Change{c.ch})
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if st[0].Text != c.text || st[0].Display != c.disp || len(st[0].Args) != c.nargs {
			t.Errorf("%s:\n text %s\n disp %s\n args %d", c.name, st[0].Text, st[0].Display, len(st[0].Args))
		}
		for i, a := range st[0].Args {
			if typeName(a.(types.AttributeValue)) != c.argsOf[i] {
				t.Errorf("%s: arg %d is %s, want %s", c.name, i, typeName(a.(types.AttributeValue)), c.argsOf[i])
			}
		}
		if st[0].ReadOnly || db.IsReadOnly("partiql", st[0].Text) {
			t.Errorf("%s: classified read-only", c.name)
		}
	}
	for name, ch := range map[string]db.Change{
		"no key on insert": {Kind: db.Insert, ObjRef: r, Set: []db.KeyVal{kv("pk", "a")}},
		"null key":         {Kind: db.Insert, ObjRef: r, Set: []db.KeyVal{kv("pk", "a"), kv("sk", db.Null)}},
		"update key":       {Kind: db.Update, ObjRef: r, Key: []db.KeyVal{kv("pk", "a"), kv("sk", int64(1))}, Set: []db.KeyVal{kv("sk", "2")}},
		"partial key":      {Kind: db.Delete, ObjRef: r, Key: []db.KeyVal{kv("pk", "a")}},
		"no key":           {Kind: db.Delete, ObjRef: r},
		"empty update":     {Kind: db.Update, ObjRef: r, Key: []db.KeyVal{kv("pk", "a"), kv("sk", int64(1))}},
		"bad number":       {Kind: db.Update, ObjRef: r, Key: []db.KeyVal{kv("pk", "a"), kv("sk", int64(1))}, Set: []db.KeyVal{kv("age", "old")}},
		"bad kind":         {Kind: 9, ObjRef: r},
	} {
		if st, err := s.Plan([]db.Change{ch}); err == nil {
			t.Errorf("%s accepted: %+v", name, st)
		}
	}
	if _, err := (&session{metas: map[string]*tableMeta{}}).Plan([]db.Change{{Kind: db.Delete, ObjRef: r}}); err == nil {
		t.Error("planning without table structure")
	}
}

func TestParseAdmin(t *testing.T) {
	sp := tableSpec{Keys: []keySpec{{Name: "a", Type: "S"}, {Name: "b", Type: "N"}}, Billing: "PROVISIONED", RCU: 3, WCU: 4,
		Indexes: []indexSpec{{Name: "g", Keys: []keySpec{{Name: "c", Type: "B"}}, Projection: "INCLUDE", Include: []string{"x"}}}}
	st := createStatement(`we"ird name`, sp)
	for _, text := range []string{st.Text, st.Display} {
		a, err := parseAdmin(text)
		if err != nil || a.op != "create" || a.table != `we"ird name` || a.create.RCU != 3 || len(a.create.Indexes) != 1 || a.create.Indexes[0].Include[0] != "x" {
			t.Errorf("create parsed as %+v %v", a, err)
		}
		if _, err := createInput(a.table, a.create); err != nil {
			t.Error(err)
		}
	}
	for text, want := range map[string]adminStmt{
		`DROP TABLE "t"`:                       {op: "drop", table: "t"},
		`drop table t;`:                        {op: "drop", table: "t"},
		`-- c` + "\n" + `/* x */ TRUNCATE "t"`: {op: "truncate", table: "t"},
		`TRUNCATE TABLE "a-b.c"`:               {op: "truncate", table: "a-b.c"},
		`CREATE TABLE "n" LIKE "t"`:            {op: "like", table: "n", other: "t"},
		`CREATE TABLE n LIKE t WITH DATA`:      {op: "like", table: "n", other: "t", withData: true},
		`ALTER TABLE "t" DROP INDEX "i"`:       {op: "drop_index", table: "t", other: "i"},
		`ALTER TABLE "t" ADD INDEX {"name":"i","keys":[{"name":"x","type":"S"}]}`: {op: "add_index", table: "t", index: indexSpec{Name: "i", Keys: []keySpec{{Name: "x", Type: "S"}}}},
	} {
		a, err := parseAdmin(text)
		if err != nil || a == nil {
			t.Errorf("%q: %v", text, err)
			continue
		}
		if a.op != want.op || a.table != want.table || a.other != want.other || a.withData != want.withData || a.index.Name != want.index.Name {
			t.Errorf("%q = %+v, want %+v", text, a, want)
		}
	}
	for _, text := range []string{`SELECT * FROM "t"`, `INSERT INTO "t" VALUE {}`, `UPDATE "t" SET a = 1`, `DELETE FROM "t"`, ``, `(SELECT 1)`, `-- only a comment`, `CREATEX TABLE t`} {
		if a, err := parseAdmin(text); a != nil || err != nil {
			t.Errorf("%q is PartiQL, got %+v %v", text, a, err)
		}
	}
	for _, text := range []string{`DROP TABLE`, `DROP TABLE "t" CASCADE`, `DROP INDEX "i"`, `CREATE TABLE "t"`, `CREATE TABLE "t" {"keys": 1}`, `CREATE TABLE "t" {"nokeys": 1}`, `CREATE TABLE "t" {} x`,
		`CREATE TABLE "t" LIKE`, `CREATE TABLE "t" LIKE "s" WITH`, `CREATE VIEW v`, `ALTER TABLE "t" MODIFY x`, `ALTER TABLE "t" DROP INDEX`, `ALTER TABLE "t" ADD INDEX nope`, `DROP TABLE "unterminated`, `TRUNCATE TABLE "t" x`} {
		if a, err := parseAdmin(text); err == nil {
			t.Errorf("%q accepted as %+v", text, a)
		}
	}
}

func TestCreateInputValidation(t *testing.T) {
	k := func(n, ty string) keySpec { return keySpec{Name: n, Type: ty} }
	good := tableSpec{Keys: []keySpec{k("a", "S"), k("b", "N")}, Indexes: []indexSpec{
		{Name: "g", Keys: []keySpec{k("c", "S")}}, {Name: "l", Kind: "LSI", Keys: []keySpec{k("a", "S"), k("d", "B")}, Projection: "KEYS_ONLY"}}}
	in, err := createInput("t", good)
	if err != nil || len(in.AttributeDefinitions) != 4 || len(in.LocalSecondaryIndexes) != 1 || len(in.GlobalSecondaryIndexes) != 1 || in.ProvisionedThroughput != nil {
		t.Fatalf("good: %+v %v", in, err)
	}
	prov := tableSpec{Keys: []keySpec{k("a", "S")}, Billing: "PROVISIONED", RCU: 5, WCU: 2, Indexes: []indexSpec{{Name: "g", Keys: []keySpec{k("c", "S")}}}}
	if in, err = createInput("t", prov); err != nil || *in.GlobalSecondaryIndexes[0].ProvisionedThroughput.WriteCapacityUnits != 2 {
		t.Errorf("provisioned: %+v %v", in, err)
	}
	for name, sp := range map[string]tableSpec{
		"no keys":            {},
		"three keys":         {Keys: []keySpec{k("a", "S"), k("b", "S"), k("c", "S")}},
		"bad key type":       {Keys: []keySpec{k("a", "BOOL")}},
		"empty name":         {Keys: []keySpec{k("", "S")}},
		"wrong role":         {Keys: []keySpec{{Name: "a", Type: "S", Key: "RANGE"}}},
		"billing":            {Keys: []keySpec{k("a", "S")}, Billing: "FREE"},
		"provisioned no cap": {Keys: []keySpec{k("a", "S")}, Billing: "PROVISIONED"},
		"dup index":          {Keys: []keySpec{k("a", "S")}, Indexes: []indexSpec{{Name: "g", Keys: []keySpec{k("c", "S")}}, {Name: "g", Keys: []keySpec{k("d", "S")}}}},
		"type clash":         {Keys: []keySpec{k("a", "S")}, Indexes: []indexSpec{{Name: "g", Keys: []keySpec{k("a", "N")}}}},
		"lsi without range":  {Keys: []keySpec{k("a", "S")}, Indexes: []indexSpec{{Name: "l", Kind: "LSI", Keys: []keySpec{k("a", "S"), k("b", "S")}}}},
		"lsi other hash":     {Keys: []keySpec{k("a", "S"), k("b", "S")}, Indexes: []indexSpec{{Name: "l", Kind: "LSI", Keys: []keySpec{k("z", "S"), k("b", "S")}}}},
		"include nothing":    {Keys: []keySpec{k("a", "S")}, Indexes: []indexSpec{{Name: "g", Keys: []keySpec{k("c", "S")}, Projection: "INCLUDE"}}},
		"bad projection":     {Keys: []keySpec{k("a", "S")}, Indexes: []indexSpec{{Name: "g", Keys: []keySpec{k("c", "S")}, Projection: "SOME"}}},
	} {
		if _, err := createInput("t", sp); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func mkcol(name, typ string, pk bool) db.Column {
	return db.Column{Name: name, OrigName: name, Type: typ, PK: pk, Nullable: !pk}
}

func TestPlanTable(t *testing.T) {
	cols := []db.Column{mkcol("pk", "S", true), mkcol("sk", "N", true), mkcol("email", "S", false), mkcol("flag", "BOOL", false)}
	old := &db.TableDef{Name: "t", Columns: cols, PrimaryKey: []string{"pk", "sk"}}
	edit := func(mod func(d *db.TableDef)) (*db.TableDef, error) {
		d := *old
		d.Columns = append([]db.Column(nil), cols...)
		d.Indexes = append([]db.Index(nil), old.Indexes...)
		mod(&d)
		return &d, nil
	}
	// create
	st, err := planTable(nil, &db.TableDef{Name: "n", Columns: []db.Column{mkcol("id", "S", true)}})
	if err != nil || len(st) != 1 || st[0].Text != `CREATE TABLE "n" {"keys":[{"name":"id","type":"S","key":"HASH"}],"billing":"PAY_PER_REQUEST"}` {
		t.Errorf("create: %+v %v", st, err)
	}
	st, err = planTable(nil, &db.TableDef{Name: "n", Columns: []db.Column{mkcol("a", "S", true), mkcol("b", "n", true), mkcol("c", "B", false)},
		Indexes: []db.Index{{Name: "i", Columns: []string{"c"}, Algorithm: "GSI", Condition: "projection: INCLUDE (x, y)"}, {Name: "PRIMARY", Primary: true}}})
	if err != nil || !strings.Contains(st[0].Text, `"include":["x","y"]`) || !strings.Contains(st[0].Text, `"type":"N"`) || !strings.Contains(st[0].Display, "\n") {
		t.Errorf("create with index: %+v %v", st, err)
	}
	for name, def := range map[string]*db.TableDef{
		"no name":         {Columns: []db.Column{mkcol("id", "S", true)}},
		"no key":          {Name: "n", Columns: []db.Column{mkcol("id", "S", false)}},
		"three keys":      {Name: "n", Columns: []db.Column{mkcol("a", "S", true), mkcol("b", "S", true), mkcol("c", "S", true)}},
		"bool key":        {Name: "n", Columns: []db.Column{mkcol("id", "BOOL", true)}},
		"loose column":    {Name: "n", Columns: []db.Column{mkcol("id", "S", true), mkcol("x", "S", false)}},
		"index no column": {Name: "n", Columns: []db.Column{mkcol("id", "S", true)}, Indexes: []db.Index{{Name: "i", Columns: []string{"zz"}}}},
		"unique index":    {Name: "n", Columns: []db.Column{mkcol("id", "S", true), mkcol("x", "S", false)}, Indexes: []db.Index{{Name: "i", Columns: []string{"x"}, Unique: true}}},
		"index bad type":  {Name: "n", Columns: []db.Column{mkcol("id", "S", true), mkcol("x", "BOOL", false)}, Indexes: []db.Index{{Name: "i", Columns: []string{"x"}}}},
		"bad projection":  {Name: "n", Columns: []db.Column{mkcol("id", "S", true), mkcol("x", "S", false)}, Indexes: []db.Index{{Name: "i", Columns: []string{"x"}, Condition: "x > 1"}}},
		"bad algorithm":   {Name: "n", Columns: []db.Column{mkcol("id", "S", true), mkcol("x", "S", false)}, Indexes: []db.Index{{Name: "i", Columns: []string{"x"}, Algorithm: "btree"}}},
	} {
		if st, err := planTable(nil, def); err == nil {
			t.Errorf("create %s accepted: %+v", name, st)
		}
	}
	// no change
	same, _ := edit(func(*db.TableDef) {})
	if st, err := planTable(old, same); err != nil || len(st) != 0 {
		t.Errorf("no-op: %+v %v", st, err)
	}
	// add a GSI with a new attribute column
	nw, _ := edit(func(d *db.TableDef) {
		d.Columns = append(d.Columns, db.Column{Name: "city", Type: "S"})
		d.Indexes = append(d.Indexes, db.Index{Name: "by_city", Columns: []string{"city", "email"}})
	})
	st, err = planTable(old, nw)
	if err != nil || len(st) != 1 || !strings.HasPrefix(st[0].Text, `ALTER TABLE "t" ADD INDEX {"name":"by_city","keys":[{"name":"city","type":"S","key":"HASH"},{"name":"email","type":"S","key":"RANGE"}],"projection":"ALL"}`) {
		t.Errorf("add index: %+v %v", st, err)
	}
	// drop it again, unchanged keeps quiet, changed is drop + add
	withIdx := *old
	withIdx.Indexes = []db.Index{{Name: "by_email", Columns: []string{"email"}, Algorithm: "GSI", Condition: "ALL"}, {Name: "lsi", Columns: []string{"pk", "flag"}, Algorithm: "LSI", Condition: "KEYS_ONLY"}}
	keep := withIdx
	if st, err := planTable(&withIdx, &keep); err != nil || len(st) != 0 {
		t.Errorf("same indexes: %+v %v", st, err)
	}
	drop := withIdx
	drop.Indexes = withIdx.Indexes[1:]
	if st, err := planTable(&withIdx, &drop); err != nil || len(st) != 1 || st[0].Text != `ALTER TABLE "t" DROP INDEX "by_email"` {
		t.Errorf("drop index: %+v %v", st, err)
	}
	chg := withIdx
	chg.Indexes = []db.Index{{Name: "by_email", Columns: []string{"email", "pk"}, Algorithm: "GSI", Condition: "KEYS_ONLY"}, withIdx.Indexes[1]}
	if st, err := planTable(&withIdx, &chg); err != nil || len(st) != 2 || !strings.Contains(st[0].Text, "DROP INDEX") || !strings.Contains(st[1].Text, "ADD INDEX") {
		t.Errorf("changed index: %+v %v", st, err)
	}
	for name, mod := range map[string]func(d *db.TableDef){
		"rename column":    func(d *db.TableDef) { d.Columns[2].Name = "mail" },
		"retype column":    func(d *db.TableDef) { d.Columns[2].Type = "N" },
		"drop column":      func(d *db.TableDef) { d.Columns = d.Columns[:3] },
		"add loose column": func(d *db.TableDef) { d.Columns = append(d.Columns, db.Column{Name: "x", Type: "S"}) },
		"change key":       func(d *db.TableDef) { d.PrimaryKey = []string{"sk", "pk"} },
		"drop key":         func(d *db.TableDef) { d.PrimaryKey = []string{"pk"} },
		"rename table":     func(d *db.TableDef) { d.Name = "other" },
		"add lsi": func(d *db.TableDef) {
			d.Indexes = []db.Index{{Name: "l", Columns: []string{"pk", "email"}, Algorithm: "LSI"}}
		},
		"index on bool": func(d *db.TableDef) { d.Indexes = []db.Index{{Name: "g", Columns: []string{"flag"}}} },
	} {
		d, _ := edit(mod)
		if st, err := planTable(old, d); err == nil {
			t.Errorf("%s accepted: %+v", name, st)
		}
	}
	if _, err := planTable(&withIdx, &db.TableDef{Name: "t", Columns: cols, PrimaryKey: []string{"pk", "sk"}, Indexes: []db.Index{withIdx.Indexes[0]}}); err == nil {
		t.Error("dropping an LSI accepted")
	}
}

func TestParseProjection(t *testing.T) {
	for in, want := range map[string]string{"": "ALL", "ALL": "ALL", "all": "ALL", "KEYS_ONLY": "KEYS_ONLY", "projection: KEYS_ONLY": "KEYS_ONLY", "INCLUDE a, b": "INCLUDE a,b", "Projection:INCLUDE (a,b)": "INCLUDE a,b", "include x": "INCLUDE x"} {
		p, inc, err := parseProjection(in)
		if got := strings.TrimSpace(p + " " + strings.Join(inc, ",")); err != nil || got != want {
			t.Errorf("%q = %q %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"INCLUDE", "INCLUDE ()", "ALL x", "x > 1", "KEYS"} {
		if _, _, err := parseProjection(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestStripLead(t *testing.T) {
	for in, want := range map[string]string{"  SELECT 1 ; ": "SELECT 1", "-- a\n-- b\nSELECT 1;": "SELECT 1", "/* x */  /* y */SELECT 2": "SELECT 2", "-- only": "", "/* open": "", "SELECT '--' ": "SELECT '--'"} {
		if got := stripLead(in); got != want {
			t.Errorf("stripLead(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCaps(t *testing.T) {
	c := (&session{}).Caps()
	if !c.Indexes || !c.Transactions || !c.Document || c.Editor != "sql" || c.Dialect != "partiql" || c.DatabaseNoun != "region" ||
		c.RenameDB || c.Triggers || c.ForeignKeys || c.Views || c.MatViews || c.Functions || c.Sequences || c.Schemas || c.ProcessList || c.Users || c.ReorderColumns {
		t.Errorf("caps %+v", c)
	}
	if len(c.Types) != 3 {
		t.Errorf("types %v", c.Types)
	}
	if _, ok := db.DialectFor(c.Dialect); !ok {
		t.Error("partiql dialect is not registered")
	}
	if d, err := db.Lookup(config.DynamoDB); err != nil || d.Name() != "dynamodb" {
		t.Errorf("driver lookup: %v", err)
	}
}

func TestEndpointAndOpenValidation(t *testing.T) {
	for _, c := range []struct {
		conn config.Connection
		want string
	}{
		{config.Connection{}, ""},
		{config.Connection{Host: "localhost", Port: 8000}, "http://localhost:8000"},
		{config.Connection{Host: "localhost"}, "http://localhost"},
		{config.Connection{Host: "db.example.com", Port: 443, SSLMode: "require"}, "https://db.example.com:443"},
		{config.Connection{Host: "h", SSLMode: "disable"}, "http://h"},
		{config.Connection{Host: "h", Options: map[string]string{"scheme": "https"}}, "https://h"},
		{config.Connection{Host: "ignored", Options: map[string]string{"endpoint": "https://vpce.example:8443/"}}, "https://vpce.example:8443"},
		{config.Connection{Host: "::1", Port: 8000}, "http://[::1]:8000"},
	} {
		if got, err := endpointOf(c.conn); err != nil || got != c.want {
			t.Errorf("endpointOf(%+v) = %q %v, want %q", c.conn, got, err, c.want)
		}
	}
	for _, bad := range []string{"ftp://x", "localhost:8000", "http://", "://x"} {
		if _, err := endpointOf(config.Connection{Options: map[string]string{"endpoint": bad}}); err == nil {
			t.Errorf("endpoint %q accepted", bad)
		}
	}
	for name, c := range map[string]config.Connection{
		"region":        {Options: map[string]string{"region": "mars"}},
		"region in db":  {Database: "us_east_1"},
		"key only":      {User: "AK"},
		"secret only":   {Password: "x"},
		"endpoint":      {Options: map[string]string{"endpoint": "gopher://x"}},
		"ssl":           {Host: "h", SSLMode: "weird"},
		"ca not found":  {Host: "h", SSLMode: "verify-ca", SSLCA: "/no/such/ca.pem"},
		"cert mismatch": {Host: "h", SSLMode: "require", SSLCert: "/no/such.pem", SSLKey: "/no/such.key"},
	} {
		if _, err := (drv{}).Open(context.Background(), c, nil); err == nil {
			t.Errorf("%s: Open accepted %+v", name, c)
		}
	}
	for _, r := range []string{"us-east-1", "eu-central-2", "us-gov-west-1", "cn-north-1", "ap-southeast-4"} {
		if !regionRe.MatchString(r) {
			t.Errorf("region %s rejected", r)
		}
	}
	for _, r := range []string{"", "us", "US-EAST-1", "us-east", "us-east-1; DROP", "../x"} {
		if regionRe.MatchString(r) {
			t.Errorf("region %q accepted", r)
		}
	}
	if len(regions) < 25 {
		t.Error("region list")
	}
	for _, r := range regions {
		if !regionRe.MatchString(r) {
			t.Errorf("listed region %s is not valid", r)
		}
	}
}

func TestWrapKeepsCancellation(t *testing.T) {
	if !errors.Is(wrap("op", context.Canceled), context.Canceled) || !errors.Is(wrap("op", context.DeadlineExceeded), context.DeadlineExceeded) {
		t.Error("cancellation lost")
	}
	if wrap("op", nil) != nil {
		t.Error("nil")
	}
	if e := wrap("op", errors.New("boom")); !strings.Contains(e.Error(), "op: boom") {
		t.Error(e)
	}
}

func TestSortPage(t *testing.T) {
	res := &db.Result{Columns: []db.ResultColumn{{Name: "n"}, {Name: "s"}}, Rows: [][]any{{"10", "b"}, {int64(9), "a"}, {nil, "c"}, {"2.5", "a"}}, Docs: []string{"1", "2", "3", "4"}}
	sortPage(res, []db.Sort{{Column: "n"}}, []string{"N", "S"})
	if got := col2(res); got != "NULL 2.5 9 10" {
		t.Errorf("numeric asc: %s", got)
	}
	if strings.Join(res.Docs, "") != "3421" {
		t.Errorf("docs follow rows: %v", res.Docs)
	}
	sortPage(res, []db.Sort{{Column: "s", Desc: true}, {Column: "n"}}, []string{"N", "S"})
	if res.Rows[0][1] != "c" || res.Rows[1][1] != "b" || res.Rows[2][0] != "2.5" || res.Rows[3][0] != int64(9) {
		t.Errorf("two-key sort: %v", res.Rows)
	}
	sortPage(res, []db.Sort{{Column: "missing"}}, []string{"N", "S"}) // a column outside the page is ignored
}

func col2(r *db.Result) string {
	var out []string
	for _, row := range r.Rows {
		out = append(out, db.FormatValue(row[0]))
	}
	return strings.Join(out, " ")
}

func TestRender(t *testing.T) {
	items := []map[string]types.AttributeValue{
		{"b": N("1"), "pk": S("a"), "c": S("x")},
		{"pk": S("b"), "b": S("one"), "c": &types.AttributeValueMemberNULL{Value: true}},
		{"pk": S("c"), "c": S("z"), "a": N("2")},
	}
	r := render(items, meta())
	var names, typ []string
	for _, c := range r.Columns {
		names, typ = append(names, c.Name), append(typ, c.Type)
	}
	if strings.Join(names, ",") != "pk,a,b,c" || strings.Join(typ, ",") != "S,N,mixed,S" {
		t.Errorf("columns %v %v", names, typ)
	}
	if len(r.Docs) != 3 || len(r.Rows) != 3 || r.Rows[0][1] != nil || r.Rows[2][1] != int64(2) {
		t.Errorf("rows %v", r.Rows)
	}
	if r2 := render(nil, nil); !r2.HasRows || len(r2.Rows) != 0 {
		t.Error("empty render")
	}
}

func TestLearnKeepsKeyTypes(t *testing.T) {
	m := meta()
	m.learn([]map[string]types.AttributeValue{{"pk": N("1"), "new": S("x"), "age": S("old")}, {"new": N("1"), "zz": &types.AttributeValueMemberNULL{Value: true}}, {"zz": S("later"), "aa": N("1")}})
	if m.attrs["pk"] != "S" {
		t.Error("key type changed")
	}
	if m.attrs["new"] != "mixed" || m.attrs["age"] != "mixed" || m.attrs["zz"] != "S" || m.attrs["aa"] != "N" {
		t.Errorf("attrs %v", m.attrs)
	}
	if got := strings.Join(m.names, ","); got != "pk,sk,age,doc,n,name,tags,aa,new,zz" {
		t.Errorf("names %s", got)
	}
}

func TestPlanAdminErrors(t *testing.T) {
	s := testSession()
	r := db.ObjRef{Name: "t"}
	for name, c := range map[string]struct {
		op   db.AdminOp
		opts map[string]string
	}{
		"rename":        {db.OpRenameObject, map[string]string{"new_name": "x"}},
		"cascade":       {db.OpTruncate, map[string]string{"cascade": "true"}},
		"clone no name": {db.OpClone, nil},
		"create region": {db.OpCreateDatabase, nil},
		"drop region":   {db.OpDropDatabase, nil},
		"rename region": {db.OpRenameDatabase, nil},
		"unknown":       {"explode", nil},
	} {
		if st, err := s.PlanAdmin(c.op, r, c.opts); err == nil {
			t.Errorf("%s accepted: %+v", name, st)
		}
	}
	for op, want := range map[db.AdminOp]string{db.OpDropObject: `DROP TABLE "t"`, db.OpTruncate: `TRUNCATE TABLE "t"`} {
		if st, err := s.PlanAdmin(op, r, nil); err != nil || st[0].Text != want || st[0].ReadOnly || db.IsReadOnly("partiql", st[0].Text) {
			t.Errorf("%s: %+v %v", op, st, err)
		}
	}
	st, err := s.PlanAdmin(db.OpClone, r, map[string]string{"new_name": `c"x`, "with_data": "true"})
	if err != nil || st[0].Text != `CREATE TABLE "c""x" LIKE "t" WITH DATA` {
		t.Errorf("clone: %+v %v", st, err)
	}
	if a, err := parseAdmin(st[0].Text); err != nil || a.table != `c"x` || !a.withData {
		t.Errorf("clone does not parse back: %+v %v", a, err)
	}
}

func TestFilterQuotingAndTypes(t *testing.T) {
	if got := splitList(`'x,y', z, "a,b"`); len(got) != 3 || got[0] != "x,y" || got[1] != "z" || got[2] != "a,b" {
		t.Errorf("splitList = %q", got)
	}
	if lo, hi, err := splitBetween(`'A AND B' and 'Z'`); err != nil || lo != "'A AND B'" || hi != "'Z'" {
		t.Errorf("splitBetween = %q %q %v", lo, hi, err)
	}
	for _, op := range []string{"IN", "Has prefix", "="} {
		if _, err := buildRead(meta(), db.FilterSet{Filters: []db.Filter{filt("tags", op, "p")}}, nil, nil); err == nil {
			t.Errorf("%s on an SS column should be rejected", op)
		}
	}
	if _, err := coerce(map[string]any{"$binary": 123.0}, "", "c"); err == nil {
		t.Error("$binary with a number should be an error")
	}
	for u, want := range map[string]bool{"http://localhost:8000": true, "http://127.0.0.1:58000": true, "https://vpce-1.dynamodb.us-east-1.vpce.amazonaws.com": false, "": false} {
		if isLocalEndpoint(u) != want {
			t.Errorf("isLocalEndpoint(%q) != %v", u, want)
		}
	}
}
