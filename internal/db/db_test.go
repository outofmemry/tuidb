package db

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplit(t *testing.T) {
	for _, c := range []struct {
		dialect, in string
		want        []string
	}{
		{"postgres", "select 1; select 2;", []string{"select 1", "select 2"}},
		{"postgres", "select ';' as a; -- c;\nselect \"x;y\"", []string{"select ';' as a", "-- c;\nselect \"x;y\""}},
		{"postgres", "create function f() returns int as $$ begin; return 1; end $$ language plpgsql; select 1", []string{"create function f() returns int as $$ begin; return 1; end $$ language plpgsql", "select 1"}},
		{"postgres", "select $1; select $tag$a;b$tag$", []string{"select $1", "select $tag$a;b$tag$"}},
		{"postgres", "select 'it''s;ok'", []string{"select 'it''s;ok'"}},
		{"mysql", `select 'a\';b'; select 2`, []string{`select 'a\';b'`, "select 2"}},
		{"mysql", "select 1 # c;\n; select 2", []string{"select 1 # c;", "select 2"}},
		{"postgres", "/* x; */ ; -- only comment\n", nil},
		{"postgres", "select /* ; */ 1", []string{"select /* ; */ 1"}},
		{"redis", "GET a\n\n# note\nSET b 1\n", []string{"GET a", "SET b 1"}},
		{"mongo", "db.a.find({x:';'});db.b.find()", []string{"db.a.find({x:';'})", "db.b.find()"}},
		{"mongo", `db.links.find({url: /^https?:\/\//}); db.links.deleteMany({})`, []string{`db.links.find({url: /^https?:\/\//})`, "db.links.deleteMany({})"}},
		{"mongo", `db.users.find({name: /a;b/})`, []string{`db.users.find({name: /a;b/})`}},
		{"mysql", "SELECT `x\\` FROM t; DELETE FROM t;", []string{"SELECT `x\\` FROM t", "DELETE FROM t"}},
		{"mysql", "  DELIMITER $$\nCREATE PROCEDURE p() BEGIN SELECT 1; END$$\nDELIMITER ;\nSELECT 2;", []string{"CREATE PROCEDURE p() BEGIN SELECT 1; END", "SELECT 2"}},
	} {
		var got []string
		for _, s := range Split(c.dialect, c.in) {
			got = append(got, s.Text)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %q:\n got  %q\n want %q", c.dialect, c.in, got, c.want)
		}
	}
}

func TestSplitOffsetsAndLabel(t *testing.T) {
	in := "select 1;\n-- name: Recent orders\nselect 2;"
	st := Split("postgres", in)
	if len(st) != 2 || st[1].Label != "Recent orders" {
		t.Fatalf("%+v", st)
	}
	for _, s := range st {
		if in[s.Start:s.End] != s.Text {
			t.Errorf("offsets wrong for %+v", s)
		}
	}
	if s, _ := StatementAt(st, 3); s.Text != "select 1" {
		t.Errorf("cursor in first: %q", s.Text)
	}
	if s, _ := StatementAt(st, len(in)); !strings.HasPrefix(s.Text, "-- name") {
		t.Errorf("cursor at end: %q", s.Text)
	}
	if s, _ := StatementAt(st, 9); s.Text != "select 1" {
		t.Errorf("cursor right after ';' picks previous: %q", s.Text)
	}
}

func TestIsReadOnly(t *testing.T) {
	for _, c := range []struct {
		d, q string
		want bool
	}{
		{"postgres", "SELECT * FROM t", true},
		{"postgres", "  -- hi\n select 1", true},
		{"postgres", "(select 1)", true},
		{"postgres", "select * into new_t from t", false},
		{"postgres", "WITH a AS (SELECT 1) SELECT * FROM a", true},
		{"postgres", "WITH a AS (DELETE FROM t RETURNING *) SELECT * FROM a", false},
		{"postgres", "EXPLAIN SELECT 1", true},
		{"postgres", "EXPLAIN ANALYZE DELETE FROM t", false},
		{"postgres", "EXPLAIN ANALYZE SELECT 1", true},
		{"mysql", "SHOW TABLES", true},
		{"mysql", "DESCRIBE t", true},
		{"mysql", "UPDATE t SET a=1", false},
		{"mysql", "select 'delete' as a", true},
		{"clickhouse", "SELECT 1", true},
		{"clickhouse", "ALTER TABLE t DELETE WHERE 1", false},
		{"postgres", "", true},
		{"postgres", "DROP TABLE t", false},
		{"redis", "get foo", true},
		{"redis", "SET foo 1", false},
		{"redis", "CONFIG GET maxmemory", true},
		{"redis", "CONFIG SET maxmemory 1", false},
		{"redis", "FLUSHALL", false},
		{"mongo", "db.users.find({a:1}).limit(5)", true},
		{"mongo", "db.users.insertOne({a:1})", false},
		{"mongo", "db.users.aggregate([{$match:{}},{$out:'x'}])", false},
		{"mongo", "db.users.aggregate([{$match:{}}])", true},
		{"mongo", `{"find":"users"}`, true},
		{"mongo", `{"drop":"users"}`, false},
		{"mongo", "show collections", true},
	} {
		if got := IsReadOnly(c.d, c.q); got != c.want {
			t.Errorf("IsReadOnly(%s, %q) = %v, want %v", c.d, c.q, got, c.want)
		}
	}
}

func TestWhere(t *testing.T) {
	fs := FilterSet{Filters: []Filter{
		{Column: "id", Op: "=", Value: "5", Enabled: true},
		{Column: "name", Op: "Contains", Value: "50%_x", Enabled: true},
		{Column: "ignored", Op: "=", Value: "1", Enabled: false},
		{Column: "id", Op: "IN", Value: "1, '2' ,3", Enabled: true},
		{Column: "n", Op: "BETWEEN", Value: "1 and 10", Enabled: true},
		{Column: "z", Op: "IS NULL", Enabled: true},
		{Column: RawColumn, Value: "total > 100", Enabled: true},
	}}
	sql, args, err := fs.Where(PG, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := `"id" = $1 AND CAST("name" AS TEXT) ILIKE $2 AND "id" IN ($3, $4, $5) AND "n" BETWEEN $6 AND $7 AND "z" IS NULL AND (total > 100)`
	if sql != want {
		t.Errorf("sql:\n got  %s\n want %s", sql, want)
	}
	if !reflect.DeepEqual(args, []any{"5", `%50\%\_x%`, "1", "2", "3", "1", "10"}) {
		t.Errorf("args %#v", args)
	}
	my, _, _ := FilterSet{Filters: []Filter{{Column: "a", Op: "Has prefix", Value: "x", Enabled: true}}}.Where(MySQL, nil, 1)
	if my != "`a` LIKE ?" {
		t.Errorf("mysql: %s", my)
	}
	any1, a, _ := FilterSet{Filters: []Filter{{Column: AnyColumn, Op: "Contains", Value: "x", Enabled: true}}}.Where(PG, []string{"a", "b"}, 1)
	if any1 != `(CAST("a" AS TEXT) ILIKE $1 OR CAST("b" AS TEXT) ILIKE $2)` || len(a) != 2 {
		t.Errorf("any: %s %v", any1, a)
	}
	or, _, _ := FilterSet{Or: true, Filters: []Filter{{Column: "a", Op: "=", Value: "1", Enabled: true}, {Column: "b", Op: "=", Value: "2", Enabled: true}}}.Where(MySQL, nil, 1)
	if or != "`a` = ? OR `b` = ?" {
		t.Errorf("or: %s", or)
	}
	if _, _, err := (FilterSet{Filters: []Filter{{Column: "a", Op: "BETWEEN", Value: "1", Enabled: true}}}).Where(PG, nil, 1); err == nil {
		t.Error("BETWEEN with one value must fail")
	}
	if _, _, err := (FilterSet{Filters: []Filter{{Column: "a", Op: "??", Enabled: true}}}).Where(PG, nil, 1); err == nil {
		t.Error("unknown op must fail")
	}
}

func TestBuildSelectCount(t *testing.T) {
	q := RowQuery{ObjRef: ObjRef{Schema: "public", Name: "orders"}, Limit: 300, Offset: 600,
		Sort:    []Sort{{Column: "id", Desc: true}, {Column: "name"}},
		Filters: FilterSet{Filters: []Filter{{Column: "id", Op: ">", Value: "3", Enabled: true}}}}
	sql, args, err := BuildSelect(PG, q, nil)
	if err != nil || sql != `SELECT * FROM "public"."orders" WHERE "id" > $1 ORDER BY "id" DESC, "name" LIMIT 300 OFFSET 600` || len(args) != 1 {
		t.Errorf("%s %v %v", sql, args, err)
	}
	q.Columns = []string{"id", "name"}
	q.ObjRef.Schema = ""
	if sql, _, _ = BuildSelect(MySQL, q, nil); !strings.HasPrefix(sql, "SELECT `id`, `name` FROM `orders` WHERE `id` > ?") {
		t.Errorf("%s", sql)
	}
	if sql, _, _ = BuildCount(MySQL, q, nil); sql != "SELECT COUNT(*) FROM `orders` WHERE `id` > ?" {
		t.Errorf("%s", sql)
	}
}

func TestPlanSQL(t *testing.T) {
	ref := ObjRef{Schema: "public", Name: "customers", Kind: KindTable}
	ch := []Change{
		{Kind: Update, ObjRef: ref, Key: []KeyVal{{"id", "7"}}, Set: []KeyVal{{"name", "O'Neil"}, {"nick", Null}, {"seen", Expr("NOW()")}, {"bio", Empty}}},
		{Kind: Insert, ObjRef: ref, Set: []KeyVal{{"name", "Ann"}, {"id", Default}}},
		{Kind: Delete, ObjRef: ref, Key: []KeyVal{{"id", "9"}, {"deleted_at", nil}}},
		{Kind: Insert, ObjRef: ref},
	}
	got, err := PlanSQL(PG, ch)
	if err != nil {
		t.Fatal(err)
	}
	wantText := []string{
		`UPDATE "public"."customers" SET "name" = $1, "nick" = NULL, "seen" = NOW(), "bio" = $2 WHERE "id" = $3`,
		`INSERT INTO "public"."customers" ("name", "id") VALUES ($1, DEFAULT)`,
		`DELETE FROM "public"."customers" WHERE "id" = $1 AND "deleted_at" IS NULL`,
		`INSERT INTO "public"."customers" DEFAULT VALUES`,
	}
	for i, w := range wantText {
		if got[i].Text != w {
			t.Errorf("[%d] text:\n got  %s\n want %s", i, got[i].Text, w)
		}
	}
	if d := got[0].Display; d != `UPDATE "public"."customers" SET "name" = 'O''Neil', "nick" = NULL, "seen" = NOW(), "bio" = '' WHERE "id" = '7'` {
		t.Errorf("display: %s", d)
	}
	if !reflect.DeepEqual(got[0].Args, []any{"O'Neil", "", "7"}) {
		t.Errorf("args %#v", got[0].Args)
	}
	chs, _ := PlanSQL(ClickHouse, ch[:3])
	if chs[0].Text != "ALTER TABLE `public`.`customers` UPDATE `name` = ?, `nick` = NULL, `seen` = NOW(), `bio` = ? WHERE `id` = ?" ||
		chs[1].Text != "INSERT INTO `public`.`customers` (`name`) VALUES (?)" ||
		chs[2].Text != "ALTER TABLE `public`.`customers` DELETE WHERE `id` = ? AND `deleted_at` IS NULL" {
		t.Errorf("clickhouse: %q", []string{chs[0].Text, chs[1].Text, chs[2].Text})
	}
	if m, _ := PlanSQL(MySQL, ch[3:]); m[0].Text != "INSERT INTO `public`.`customers` () VALUES ()" {
		t.Errorf("mysql empty insert: %s", m[0].Text)
	}
	if _, err := PlanSQL(PG, []Change{{Kind: Update, ObjRef: ref, Set: []KeyVal{{"a", "1"}}}}); err == nil {
		t.Error("update without key must fail")
	}
	if _, err := PlanSQL(PG, []Change{{Kind: Delete, ObjRef: ref}}); err == nil {
		t.Error("delete without key must fail")
	}
}

// Statements the independent review proved can write while an older classifier called them read-only.
func TestIsReadOnlyAdversarial(t *testing.T) {
	for _, c := range []struct {
		d, q string
		want bool
	}{
		// MongoDB: allow-list, so anything unknown is a write
		{"mongo", `db.runCommand({drop:"c"})`, false},
		{"mongo", `db.runCommand({insert:"c", documents:[{a:1}]})`, false},
		{"mongo", `db.adminCommand({dropDatabase:1})`, false},
		{"mongo", `db.c["deleteMany"]({})`, false},
		{"mongo", `db.getCollection("c")["drop"]()`, false},
		{"mongo", `db.c.aggregate([{"\u0024out":"o"}])`, false},
		{"mongo", `db.c.aggregate([{$match:{}},{$out:"o"}])`, false},
		{"mongo", `{"aggregate":"c","pipeline":[{"$out":"o"}],"cursor":{}}`, false},
		{"mongo", `{"drop":"c"}`, false},
		{"mongo", `db.c.find().forEach(function(d){ db.o.insert(d) })`, false},
		{"mongo", `db.c.find().map(d => d)`, false},
		{"mongo", `db.grantRolesToUser("u", ["root"])`, false},
		{"mongo", `db.c.convertToCapped(100)`, false},
		{"mongo", `db.c.find({$where: "sleep(1000)"})`, false},
		{"mongo", `x = db.c.find()`, false},
		{"mongo", `db.c.find({a:1}).sort({a:1}).limit(5).toArray()`, true},
		{"mongo", `db.getCollection("c").find({_id: ObjectId("507f1f77bcf86cd799439011")})`, true},
		{"mongo", `db.c.aggregate([{$match:{a:{$in:[1,2]}}},{$group:{_id:"$a"}}])`, true},
		{"mongo", `db.getCollectionNames()`, true},
		{"mongo", `show collections`, true},
		{"mongo", `{"find":"c","filter":{"a":1}}`, true},
		// PostgreSQL: lexical rules that decide what is a comment or string
		{"postgres", `SELECT 6 # 3 INTO webtuidb_review_t`, false},                 // # is an operator, not a comment
		{"postgres", `SELECT '\' AS a, 1 AS b INTO webtuidb_review_t -- '`, false}, // '\' is a complete string
		{"postgres", `SELECT $$'$$ AS a, 1 AS b INTO webtuidb_review_t`, false},    // dollar quote hides the apostrophe
		{"postgres", `SELECT E'\'' AS a INTO webtuidb_review_t`, false},
		{"postgres", `/* a /* b */ INTO */ SELECT 1`, true}, // nested comment: INTO is inside it
		{"postgres", `SELECT 'INTO' AS x, "into" FROM t`, true},
		{"postgres", `SELECT $q$ INTO $q$ AS x`, true},
		// MySQL: executable comments are code
		{"mysql", `SELECT 1 /*! INTO @webtuidb_x */`, false},
		{"mysql", `SELECT 1 /*!50000 INTO @webtuidb_x */`, false},
		{"mysql", `SELECT 1 /*+ MAX_EXECUTION_TIME(1) */`, true},
		{"mysql", `SELECT 1 -- INTO @x`, true},
		{"mysql", `SELECT 1 --1 INTO @x`, false}, // --1 is arithmetic in MySQL, so INTO is real code
		{"mysql", "SELECT 1 # INTO @x", true},
		{"mysql", `SELECT 'it\'s INTO' AS x`, true},
		// MariaDB: reading a sequence's next value changes it
		{"mysql", `SELECT NEXTVAL(s)`, false},
		{"mysql", "select nextval(`s`), 1", false},
		{"mysql", `SELECT NEXT VALUE FOR s`, false},
		{"mysql", `SELECT SETVAL(s, 100)`, false},
		{"mysql", `WITH x AS (SELECT 1) SELECT NEXTVAL(s) FROM x`, false},
		{"mysql", `SELECT LASTVAL(s)`, true},
		{"mysql", `SELECT 'NEXTVAL(s)' AS x /* NEXTVAL */ -- NEXTVAL`, true},
		{"mysql", `SELECT 1 /*M!100100 INTO @webtuidb_x */`, false}, // MariaDB executable comment
		{"mysql", `SET STATEMENT max_statement_time=1 FOR DELETE FROM t`, false},
		{"mysql", `ANALYZE DELETE FROM t`, false},
		{"mysql", `DELETE FROM t RETURNING id`, false},
		// ClickHouse: replace() is a function
		{"clickhouse", `WITH 1 AS x SELECT replace('abc','a','b')`, true},
		{"clickhouse", `WITH x AS (SELECT 1) INSERT INTO t SELECT * FROM x`, false},
		{"postgres", `WITH x AS (SELECT 1) REPLACE INTO t VALUES (1)`, false},
	} {
		if got := IsReadOnly(c.d, c.q); got != c.want {
			t.Errorf("IsReadOnly(%s, %q) = %v, want %v", c.d, c.q, got, c.want)
		}
	}
}

func TestSplitFollowsServerLexing(t *testing.T) {
	for _, c := range []struct {
		d, in string
		want  []string
	}{
		// E'' strings: the backslash escapes the quote, so the ';' stays inside the string
		{"postgres", `INSERT INTO t VALUES (E'it\'s; DROP TABLE users; --')`, []string{`INSERT INTO t VALUES (E'it\'s; DROP TABLE users; --')`}},
		// plain strings: '\' is a complete string, so the ';' really ends the statement
		{"postgres", `SELECT '\'; SELECT 2`, []string{`SELECT '\'`, `SELECT 2`}},
		// nested comments in PostgreSQL swallow the ';' ...
		{"postgres", `/* a /* b */ DROP TABLE x; */ SELECT 1`, []string{`/* a /* b */ DROP TABLE x; */ SELECT 1`}},
		// ... but MySQL comments do not nest, so DROP is real code there
		{"mysql", `/* a /* b */ SELECT 1; SELECT 2 */`, []string{`/* a /* b */ SELECT 1`, `SELECT 2 */`}},
		{"clickhouse", "SELECT 1 # ; DROP TABLE x\n; SELECT 2", []string{"SELECT 1 # ; DROP TABLE x", "SELECT 2"}},
	} {
		var got []string
		for _, s := range Split(c.d, c.in) {
			got = append(got, s.Text)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %q:\n got  %q\n want %q", c.d, c.in, got, c.want)
		}
	}
}

func TestClickHouseQuotesBackslashes(t *testing.T) {
	// A name ending in a backslash must not swallow the closing backtick.
	if got := ClickHouse.Quote(`x\`); got != "`x\\\\`" {
		t.Errorf("got %q", got)
	}
	if got := ClickHouse.Quote("a`b"); got != "`a\\`b`" {
		t.Errorf("got %q", got)
	}
	sql := "SELECT 1 AS " + ClickHouse.Quote(`x\`) + ", 2 AS " + ClickHouse.Quote(" UNION ALL SELECT 99 AS v -- ")
	if n := len(Split("clickhouse", sql)); n != 1 {
		t.Errorf("must stay one statement: %d", n)
	}
	if got := MySQL.Quote("a`b"); got != "`a``b`" {
		t.Errorf("mysql keeps doubled backticks: %q", got)
	}
	if got := MySQL.Literal(`a\b'c`); got != `'a\\b''c'` {
		t.Errorf("mysql literal %q", got)
	}
	if got := PG.Literal(`a\b`); got != `'a\b'` {
		t.Errorf("postgres standard strings keep the backslash: %q", got)
	}
}

func TestLikeFiltersCastNonTextColumns(t *testing.T) {
	fs := FilterSet{Filters: []Filter{{Column: "qty", Op: "Contains", Value: "5", Enabled: true}}}
	if sql, _, _ := fs.Where(PG, nil, 1); sql != `CAST("qty" AS TEXT) ILIKE $1` {
		t.Errorf("postgres: %s", sql)
	}
	if sql, _, _ := fs.Where(ClickHouse, nil, 1); sql != "toString(`qty`) LIKE ?" {
		t.Errorf("clickhouse: %s", sql)
	}
	if sql, _, _ := fs.Where(MySQL, nil, 1); sql != "`qty` LIKE ?" {
		t.Errorf("mysql casts on its own: %s", sql)
	}
	eq := FilterSet{Filters: []Filter{{Column: "qty", Op: "=", Value: "5", Enabled: true}}}
	if sql, _, _ := eq.Where(PG, nil, 1); sql != `"qty" = $1` {
		t.Errorf("non-LIKE operators are untouched: %s", sql)
	}
}

func TestMongoPlainDocument(t *testing.T) {
	for in, want := range map[string]bool{
		`{status: 'paid', _id: ObjectId('507f1f77bcf86cd799439011')}`: true,
		`{"a": {"$gt": 5}, "d": ISODate("2024-01-01")}`:               true,
		`{a: db.webtuidb_review_c.deleteMany({}).deletedCount}`:       false,
		`{a: (function(){ return 1 })()}`:                             false,
		`{$where: "sleep(1)"}`:                                        false,
		`{a: x["y"]()}`:                                               false,
		`{a: eval("1")}`:                                              false,
		`{a: 1} ; db.c.drop()`:                                        false,
	} {
		if got := MongoPlainDocument(in); got != want {
			t.Errorf("MongoPlainDocument(%s) = %v, want %v", in, got, want)
		}
	}
	// command documents can no longer smuggle code behind a read-only first key
	if IsReadOnly("mongo", `{find: "c", filter: {a: db.o.deleteMany({}).deletedCount}}`) {
		t.Error("code inside a read command must make it not read-only")
	}
}

// Second review round: identifiers with $, JavaScript comment rules, EXECUTE, sentinels, casts.
func TestClassifierSecondRound(t *testing.T) {
	for _, c := range []struct {
		d, q string
		want bool
	}{
		{"postgres", `WITH a$$b$$ AS (SELECT 1), y AS (DELETE FROM t RETURNING *) SELECT 1 FROM y`, false},
		{"postgres", `SELECT 1 AS a$$b$$ INTO x`, false},
		{"postgres", `SELECT $$'$$ AS a`, true},
		{"postgres", `EXPLAIN ANALYZE EXECUTE d`, false},
		{"postgres", `EXPLAIN ANALYZE SELECT 1`, true},
		{"postgres", `WITH x AS (SELECT 1) SELECT * FROM x WHERE a = (CALL f())`, false},
		{"mongo", `db.c.find((x=1, x--, db.c.drop()))`, false},
		{"mongo", "db.c.find() // db.c.drop()", true},
		{"mongo", "db.c.find`${db.c.deleteMany({})}`", false},
		{"mongo", "db.c.find({a: `x`})", false}, // any template literal is refused: it can run code
		{"mysql", "SELECT 1 -- INTO @x", true},
	} {
		if got := IsReadOnly(c.d, c.q); got != c.want {
			t.Errorf("IsReadOnly(%s, %q) = %v, want %v", c.d, c.q, got, c.want)
		}
	}
	if MongoPlainDocument("{a: `x`}") {
		t.Error("plain documents have no template literals")
	}
}

func TestSplitSecondRound(t *testing.T) {
	for _, c := range []struct {
		d, in string
		want  []string
	}{
		{"postgres", `SELECT a$$b$$ FROM t; SELECT 2`, []string{`SELECT a$$b$$ FROM t`, `SELECT 2`}},
		{"mysql", `SELECT 1--1; DROP TABLE t`, []string{`SELECT 1--1`, `DROP TABLE t`}},
		{"mongo", "db.a.find() // x; y\n; db.b.find()", []string{"db.a.find() // x; y", "db.b.find()"}},
		{"mongo", "x--; db.b.find()", []string{"x--", "db.b.find()"}},
	} {
		var got []string
		for _, s := range Split(c.d, c.in) {
			got = append(got, s.Text)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %q:\n got  %q\n want %q", c.d, c.in, got, c.want)
		}
	}
}

func TestFormatValueSentinels(t *testing.T) {
	if FormatValue(Default) != "DEFAULT" || FormatValue(Empty) != "" || FormatValue(Null) != "NULL" || FormatValue(Expr("now()")) != "now()" {
		t.Errorf("%q %q %q", FormatValue(Default), FormatValue(Empty), FormatValue(Null))
	}
}

func TestAnyColumnCastsOnlyForLike(t *testing.T) {
	gt := FilterSet{Filters: []Filter{{Column: AnyColumn, Op: ">", Value: "9", Enabled: true}}}
	if sql, _, _ := gt.Where(PG, []string{"qty"}, 1); sql != `("qty" > $1)` {
		t.Errorf("comparisons stay native: %s", sql)
	}
	like := FilterSet{Filters: []Filter{{Column: AnyColumn, Op: "Contains", Value: "x", Enabled: true}}}
	if sql, _, _ := like.Where(PG, []string{"qty"}, 1); sql != `(CAST("qty" AS TEXT) ILIKE $1)` {
		t.Errorf("LIKE casts: %s", sql)
	}
}

func TestSplitMySQLDelimiter(t *testing.T) {
	in := "DELIMITER $$\nCREATE PROCEDURE p() BEGIN SELECT 1; SELECT 2; END$$\nDROP PROCEDURE q$$\nDELIMITER ;\nSELECT 3; SELECT 4"
	var got []string
	for _, s := range Split("mysql", in) {
		got = append(got, s.Text)
	}
	want := []string{"CREATE PROCEDURE p() BEGIN SELECT 1; SELECT 2; END", "DROP PROCEDURE q", "SELECT 3", "SELECT 4"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestReviewFixesDB(t *testing.T) {
	if IsReadOnly("postgres", "SELECT 1; DELETE FROM t") || !IsReadOnly("postgres", "SELECT 1;") {
		t.Error("interior ';' must not be read-only, trailing one may be")
	}
	if IsReadOnly("mongo", `db.links.find({url: /^https?:\/\//}); db.links.deleteMany({})`) {
		t.Error("regex literal hid a write")
	}
	for _, c := range []struct {
		in   string
		want []string
	}{
		{`'x,y', 2`, []string{"x,y", "2"}},
		{`'it''s', "a""b"`, []string{"it's", `a"b`}},
		{`1, '2' ,3`, []string{"1", "2", "3"}},
	} {
		if got := splitList(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitList(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for _, c := range []struct {
		d    Dialect
		want string
	}{{PG, "DEFAULT VALUES"}, {MySQL, "() VALUES ()"}, {Dialect{Name: "sqlite", Quote: PG.Quote}, "DEFAULT VALUES"}} {
		st, err := planOne(c.d, Change{Kind: Insert, ObjRef: ObjRef{Name: "t"}})
		if err != nil || !strings.HasSuffix(st.Text, c.want) {
			t.Errorf("%s empty insert: %q %v", c.d.Name, st.Text, err)
		}
	}
	if _, err := planOne(ClickHouse, Change{Kind: Insert, ObjRef: ObjRef{Name: "t"}}); err == nil {
		t.Error("clickhouse empty insert should error")
	}
}

func TestSplitBetweenNoPanicOnFoldedCase(t *testing.T) {
	if lo, hi, err := splitBetween("ɐɐɐɐɐɐ and z"); err != nil || lo != "ɐɐɐɐɐɐ" || hi != "z" {
		t.Errorf("%q %q %v", lo, hi, err)
	}
	if lo, hi, err := splitBetween("1 AnD 2"); err != nil || lo != "1" || hi != "2" {
		t.Errorf("%q %q %v", lo, hi, err)
	}
}

func TestWhereText(t *testing.T) {
	fs := FilterSet{Filters: []Filter{{Column: "a", Op: "=", Value: "o'x", Enabled: true}, {Column: "n", Op: "IN", Value: "1,2", Enabled: true}}}
	got, err := WhereText(PG, fs, nil)
	if err != nil || got != `"a" = 'o''x' AND "n" IN ('1', '2')` {
		t.Errorf("%q %v", got, err)
	}
}
