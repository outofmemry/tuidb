package cassandra

import (
	"context"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"gopkg.in/inf.v0"

	"tuidb/internal/config"
	"tuidb/internal/db"
)

func unitSession() *session {
	return &session{ks: "ks", types: map[string]map[string]gocql.TypeInfo{}, aggs: map[string]bool{"agg(int)": true}}
}

func texts(st []db.Statement) []string {
	var out []string
	for _, s := range st {
		out = append(out, s.Text)
	}
	return out
}

func TestCaps(t *testing.T) {
	c := (&session{}).Caps()
	if c.Dialect != "cql" || c.Editor != "sql" || c.DatabaseNoun != "keyspace" || c.Transactions || c.RenameDB || c.Schemas || c.ForeignKeys ||
		c.ProcessList || c.Sequences || !c.MatViews || !c.Functions || !c.Triggers || !c.Indexes || len(c.Types) == 0 {
		t.Errorf("caps %+v", c)
	}
	if _, ok := db.DialectFor("cql"); !ok {
		t.Error("cql dialect not registered")
	}
	if (drv{}).Name() != config.Cassandra {
		t.Error("driver name")
	}
	if d, err := db.Lookup("cassandra"); err != nil || d.Name() != "cassandra" {
		t.Errorf("not registered: %v", err)
	}
	if config.DefaultPort(config.Cassandra) != 9042 {
		t.Error("default port")
	}
	cn, err := config.ParseURL("cassandra://u:p@h:9043/ks?consistency=one")
	if err != nil || cn.Driver != config.Cassandra || cn.Port != 9043 || cn.Database != "ks" || cn.Options["consistency"] != "one" {
		t.Errorf("url %+v %v", cn, err)
	}
}

func TestParseOptions(t *testing.T) {
	o, err := parseOptions(map[string]string{"Consistency": "local_one", "local_dc": "dc1", "keyspace": "k", "timeout": "3s", "protocol_version": "4"})
	if err != nil || o.cons != gocql.LocalOne || o.localDC != "dc1" || o.keyspace != "k" || o.timeout != 3*time.Second || o.proto != 4 {
		t.Errorf("%+v %v", o, err)
	}
	if o, _ := parseOptions(nil); o.cons != gocql.LocalQuorum {
		t.Errorf("default consistency %v", o.cons)
	}
	for _, bad := range []map[string]string{{"consistency": "sometimes"}, {"timeout": "soon"}, {"protocol_version": "x"}, {"colour": "red"}} {
		if _, err := parseOptions(bad); err == nil {
			t.Errorf("%v must be rejected", bad)
		}
	}
}

func TestTLSConfig(t *testing.T) {
	for mode, wantNil := range map[string]bool{"": true, "disable": true, "require": false, "prefer": false, "verify-ca": false, "verify-full": false} {
		tc, err := tlsConfig(config.Connection{Host: "h", SSLMode: mode})
		if err != nil || (tc == nil) != wantNil {
			t.Errorf("%q: %v %v", mode, tc, err)
		}
	}
	if tc, _ := tlsConfig(config.Connection{Host: "h", SSLMode: "require"}); !tc.InsecureSkipVerify {
		t.Error("require does not verify")
	}
	if tc, _ := tlsConfig(config.Connection{Host: "h", SSLMode: "verify-full"}); tc.InsecureSkipVerify || tc.ServerName != "h" {
		t.Error("verify-full verifies the host name")
	}
	if _, err := tlsConfig(config.Connection{SSLMode: "verify-ca", SSLCA: "/no/such/ca.pem"}); err == nil {
		t.Error("missing CA file")
	}
	if _, err := tlsConfig(config.Connection{SSLMode: "bogus"}); err == nil {
		t.Error("unknown mode")
	}
}

func TestUseAndCommandRegexps(t *testing.T) {
	for in, want := range map[string]string{"USE ks": "ks", "use MyKs;": "myks", `USE "MyKs"`: "MyKs", ` use  "a""b" ; `: `a"b`} {
		m := useRe.FindStringSubmatch(in)
		if m == nil || keyspaceName(m) != want {
			t.Errorf("%q -> %v", in, m)
		}
	}
	for _, no := range []string{"USE", "USE a b", "SELECT 1", "USEFUL ks", "USE ks; DROP KEYSPACE ks"} {
		if useRe.MatchString(no) {
			t.Errorf("%q is not a USE", no)
		}
	}
	if m := cmdRe.FindStringSubmatch("Consistency quorum;"); m == nil || m[2] != "quorum" {
		t.Errorf("consistency: %v", m)
	}
}

func TestSignatures(t *testing.T) {
	if got := signature("f", []string{"int", "frozen<list<text>>"}); got != "f(int, frozen<list<text>>)" {
		t.Error(got)
	}
	if n, a := splitSignature("f(int, map<int, text>)"); n != "f" || a != "int, map<int, text>" {
		t.Error(n, a)
	}
	if n, a := splitSignature("plain"); n != "plain" || a != "" {
		t.Error(n, a)
	}
	got := splitTop("int, map<int, text>, tuple<a, b>, text")
	if strings.Join(got, "|") != "int|map<int, text>|tuple<a, b>|text" {
		t.Error(got)
	}
	if strings.Join(splitTop("int"), "|") != "int" {
		t.Error("single")
	}
	if body("a $$ b") != "'a $$ b'" || body("x") != "$$x$$" || quoteLit("it's") != "'it''s'" {
		t.Error("quoting")
	}
}

func TestDropTarget(t *testing.T) {
	s := unitSession()
	for ref, want := range map[db.ObjRef]string{
		{Name: "t", Kind: db.KindTable}:                         `TABLE "ks"."t"`,
		{Name: "v", Kind: db.KindMatView}:                       `MATERIALIZED VIEW "ks"."v"`,
		{Name: "ty", Kind: db.KindType}:                         `TYPE "ks"."ty"`,
		{Name: "f(int, text)", Kind: db.KindFunction}:           `FUNCTION "ks"."f"(int, text)`,
		{Name: "agg(int)", Kind: db.KindFunction}:               `AGGREGATE "ks"."agg"(int)`,
		{Schema: "other", Name: "t", Kind: db.KindTable}:        `TABLE "other"."t"`,
		{Name: `we"ird`, Kind: db.KindTable}:                    `TABLE "ks"."we""ird"`,
		{Name: "noargs()", Kind: db.KindFunction}:               `FUNCTION "ks"."noargs"()`,
		{Name: "bare", Kind: db.KindFunction, Schema: "other2"}: `FUNCTION "other2"."bare"`,
	} {
		if got, err := s.dropTarget(ref); err != nil || got != want {
			t.Errorf("%+v: %q %v want %q", ref, got, err, want)
		}
	}
	if _, err := s.dropTarget(db.ObjRef{Name: "x", Kind: db.KindSequence}); err == nil {
		t.Error("sequences do not exist")
	}
	if _, err := (&session{}).dropTarget(db.ObjRef{Name: "x", Kind: db.KindTable}); err == nil {
		t.Error("no keyspace selected")
	}
}

func TestPlanAdminStatements(t *testing.T) {
	s := unitSession()
	one := func(op db.AdminOp, ref db.ObjRef, opts map[string]string) string {
		t.Helper()
		st, err := s.PlanAdmin(op, ref, opts)
		if err != nil || len(st) != 1 {
			t.Fatalf("%s: %v %v", op, st, err)
		}
		return st[0].Text
	}
	tbl := db.ObjRef{Name: "t", Kind: db.KindTable}
	if got := one(db.OpDropObject, tbl, nil); got != `DROP TABLE "ks"."t"` {
		t.Error(got)
	}
	if got := one(db.OpTruncate, tbl, nil); got != `TRUNCATE "ks"."t"` {
		t.Error(got)
	}
	if got := one(db.OpDropDatabase, db.ObjRef{Name: "k2"}, nil); got != `DROP KEYSPACE "k2"` {
		t.Error(got)
	}
	if got := one(db.OpCreateDatabase, db.ObjRef{Name: "k2"}, nil); got != `CREATE KEYSPACE "k2" WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}` {
		t.Error(got)
	}
	if got := one(db.OpCreateDatabase, db.ObjRef{Name: "k2"}, map[string]string{"strategy": "SimpleStrategy", "replication": "3", "durable_writes": "false"}); got !=
		`CREATE KEYSPACE "k2" WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 3} AND durable_writes = false` {
		t.Error(got)
	}
	if got := one(db.OpCreateDatabase, db.ObjRef{Name: "k2"}, map[string]string{"strategy": "networktopologystrategy", "replication": "dc1:3, dc'2:1"}); got !=
		`CREATE KEYSPACE "k2" WITH replication = {'class': 'NetworkTopologyStrategy', 'dc1': 3, 'dc''2': 1}` {
		t.Error(got)
	}
	for name, c := range map[string]struct {
		op   db.AdminOp
		opts map[string]string
	}{
		"rename table":          {db.OpRenameObject, map[string]string{"new_name": "x"}},
		"rename keyspace":       {db.OpRenameDatabase, map[string]string{"new_name": "x"}},
		"truncate cascade":      {db.OpTruncate, map[string]string{"cascade": "true"}},
		"clone with data":       {db.OpClone, map[string]string{"new_name": "x", "with_data": "true"}},
		"clone without a name":  {db.OpClone, nil},
		"bad strategy":          {db.OpCreateDatabase, map[string]string{"strategy": "Fancy"}},
		"bad factor":            {db.OpCreateDatabase, map[string]string{"replication": "many"}},
		"zero factor":           {db.OpCreateDatabase, map[string]string{"replication": "0"}},
		"NTS without dcs":       {db.OpCreateDatabase, map[string]string{"strategy": "NetworkTopologyStrategy"}},
		"NTS bad dc":            {db.OpCreateDatabase, map[string]string{"strategy": "NetworkTopologyStrategy", "replication": "dc1"}},
		"unknown operation":     {db.AdminOp("explode"), nil},
		"drop of a sequence":    {db.OpDropObject, nil},
		"clone of a non-table":  {db.OpClone, map[string]string{"new_name": "x"}},
		"NTS negative replicas": {db.OpCreateDatabase, map[string]string{"strategy": "NetworkTopologyStrategy", "replication": "dc1:-1"}},
		"truncate of a view":    {db.OpTruncate, nil},
	} {
		ref := tbl
		switch name {
		case "drop of a sequence":
			ref.Kind = db.KindSequence
		case "clone of a non-table":
			ref.Kind = db.KindType
		case "truncate of a view":
			ref.Kind = db.KindMatView
		}
		if st, err := s.PlanAdmin(c.op, ref, c.opts); err == nil {
			t.Errorf("%s must be an error, got %v", name, st)
		}
	}
	if !createHeader.MatchString(`CREATE TABLE ks."Odd""Name" (`+"\n  id int") || !createHeader.MatchString("CREATE TABLE IF NOT EXISTS t (id int PRIMARY KEY)") {
		t.Error("clone header regexp")
	}
}

func mkcol(name, typ, extra string) db.Column {
	return db.Column{Name: name, OrigName: name, Type: typ, Extra: extra, PK: strings.Contains(extra, "partition") || strings.Contains(extra, "clustering"), Nullable: !strings.Contains(extra, "partition") && !strings.Contains(extra, "clustering")}
}

func TestCreateTable(t *testing.T) {
	s := unitSession()
	plan := func(d *db.TableDef) []string {
		t.Helper()
		st, err := s.PlanTable(nil, d)
		if err != nil {
			t.Fatal(err)
		}
		return texts(st)
	}
	// flagged roles, composite partition key, descending clustering, static column
	got := plan(&db.TableDef{Name: "t", Columns: []db.Column{mkcol("a", "int", "partition key"), mkcol("b", "text", "partition key"), mkcol("c", "timestamp", "clustering desc"),
		mkcol("d", "int", "clustering asc"), mkcol("s", "text", "static"), mkcol("v", "map<text, int>", "")}, PrimaryKey: []string{"a", "b", "c", "d"}})
	want := "CREATE TABLE \"ks\".\"t\" (\n    \"a\" int,\n    \"b\" text,\n    \"c\" timestamp,\n    \"d\" int,\n    \"s\" text STATIC,\n    \"v\" map<text, int>,\n" +
		"    PRIMARY KEY ((\"a\", \"b\"), \"c\", \"d\")\n) WITH CLUSTERING ORDER BY (\"c\" DESC, \"d\" ASC)"
	if len(got) != 1 || got[0] != want {
		t.Errorf("create:\n%s\nwant\n%s", got, want)
	}
	// unflagged key: the first column is the partition key, the rest cluster (PRIMARY KEY (a, b, c))
	got = plan(&db.TableDef{Schema: "other", Name: "t", Columns: []db.Column{{Name: "a", Type: "int", PK: true}, {Name: "b", Type: "int", PK: true}, {Name: "c", Type: "int", PK: true}, {Name: "x", Type: "text"}}})
	if len(got) != 1 || !strings.Contains(got[0], `CREATE TABLE "other"."t"`) || !strings.Contains(got[0], `PRIMARY KEY ("a", "b", "c")`) || strings.Contains(got[0], "CLUSTERING ORDER") {
		t.Errorf("unflagged: %v", got)
	}
	// indexes follow the table: a plain one, an SAI one and a collection one
	got = plan(&db.TableDef{Name: "t", Columns: []db.Column{{Name: "a", Type: "int", PK: true}, {Name: "x", Type: "text"}, {Name: "m", Type: "map<text,int>"}}, Indexes: []db.Index{
		{Name: "x_idx", Columns: []string{"x"}}, {Name: "x_sai", Columns: []string{"x"}, Algorithm: "SAI"}, {Columns: []string{"KEYS(m)"}}}})
	if len(got) != 4 || got[1] != `CREATE INDEX "x_idx" ON "ks"."t" ("x")` || got[2] != `CREATE CUSTOM INDEX "x_sai" ON "ks"."t" ("x") USING 'StorageAttachedIndex'` ||
		got[3] != `CREATE INDEX ON "ks"."t" (KEYS(m))` {
		t.Errorf("indexes: %v", got)
	}
	for name, d := range map[string]*db.TableDef{
		"no columns":          {Name: "t"},
		"no name":             {Columns: []db.Column{{Name: "a", Type: "int", PK: true}}},
		"no key":              {Name: "t", Columns: []db.Column{{Name: "a", Type: "int"}}},
		"key not a column":    {Name: "t", Columns: []db.Column{{Name: "a", Type: "int"}}, PrimaryKey: []string{"zz"}},
		"clustering first":    {Name: "t", Columns: []db.Column{mkcol("a", "int", "clustering asc")}, PrimaryKey: []string{"a"}},
		"column without type": {Name: "t", Columns: []db.Column{{Name: "a", PK: true}}},
		"two-column index":    {Name: "t", Columns: []db.Column{{Name: "a", Type: "int", PK: true}}, Indexes: []db.Index{{Name: "i", Columns: []string{"a", "b"}}}},
		"SASI":                {Name: "t", Columns: []db.Column{{Name: "a", Type: "int", PK: true}}, Indexes: []db.Index{{Name: "i", Columns: []string{"a"}, Algorithm: "SASI"}}},
	} {
		if st, err := s.PlanTable(nil, d); err == nil {
			t.Errorf("%s must fail, got %v", name, st)
		}
	}
	if _, err := s.PlanTable(nil, nil); err == nil {
		t.Error("nil table")
	}
}

func TestAlterTable(t *testing.T) {
	s := unitSession()
	base := func() *db.TableDef {
		return &db.TableDef{Name: "t", PrimaryKey: []string{"id", "ck"}, Columns: []db.Column{mkcol("id", "int", "partition key"), mkcol("ck", "int", "clustering asc"), mkcol("a", "text", ""), mkcol("b", "int", ""), mkcol("st", "text", "static")},
			Indexes: []db.Index{{Name: "a_idx", Columns: []string{"a"}}, {Name: "b_idx", Columns: []string{"b"}}}}
	}
	edit := func(f func(n *db.TableDef)) ([]string, error) {
		n := base()
		f(n)
		st, err := s.PlanTable(base(), n)
		return texts(st), err
	}
	got, err := edit(func(n *db.TableDef) {
		n.Columns[0].Name = "pk"                                                                                                   // rename a key column
		n.PrimaryKey = []string{"pk", "ck"}                                                                                        //
		n.Columns = append(n.Columns[:3], n.Columns[4:]...)                                                                        // drop b
		n.Columns = append(n.Columns, db.Column{Name: "z", Type: "list<int>", Nullable: true})                                     // add z
		n.Columns = append(n.Columns, db.Column{Name: "zs", Type: "int", Extra: "static"})                                         // add static zs
		n.Indexes = []db.Index{{Name: "a_idx", Columns: []string{"a"}, Algorithm: "SAI"}, {Name: "z_idx", Columns: []string{"z"}}} // a_idx changes, b_idx goes, z_idx new
	})
	want := []string{`DROP INDEX "ks"."a_idx"`, `DROP INDEX "ks"."b_idx"`, `ALTER TABLE "ks"."t" DROP "b"`, `ALTER TABLE "ks"."t" RENAME "id" TO "pk"`, `ALTER TABLE "ks"."t" ADD "z" list<int>`, `ALTER TABLE "ks"."t" ADD "zs" int STATIC`,
		`CREATE CUSTOM INDEX "a_idx" ON "ks"."t" ("a") USING 'StorageAttachedIndex'`, `CREATE INDEX "z_idx" ON "ks"."t" ("z")`}
	if err != nil || strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("diff: %v\n%s\nwant\n%s", err, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got, err := edit(func(n *db.TableDef) {}); err != nil || len(got) != 0 {
		t.Errorf("no change: %v %v", got, err)
	}
	if got, err := edit(func(n *db.TableDef) { n.Columns[2].Type = "TEXT" }); err != nil || len(got) != 0 { // spelling only
		t.Errorf("same type, other case: %v %v", got, err)
	}
	for name, f := range map[string]func(n *db.TableDef){
		"retype":           func(n *db.TableDef) { n.Columns[3].Type = "bigint" },
		"rename regular":   func(n *db.TableDef) { n.Columns[2].Name = "renamed" },
		"nullability":      func(n *db.TableDef) { n.Columns[2].Nullable = false },
		"default":          func(n *db.TableDef) { n.Columns[2].Default, n.Columns[2].HasDef = "'x'", true },
		"clustering order": func(n *db.TableDef) { n.Columns[1].Extra = "clustering desc" },
		"static toggle":    func(n *db.TableDef) { n.Columns[2].Extra = "static" },
		"key membership":   func(n *db.TableDef) { n.Columns[2].PK = true },
		"drop key column":  func(n *db.TableDef) { n.Columns = n.Columns[:1] },
		"add key column":   func(n *db.TableDef) { n.Columns = append(n.Columns, db.Column{Name: "k2", Type: "int", PK: true}) },
		"new column default": func(n *db.TableDef) {
			n.Columns = append(n.Columns, db.Column{Name: "z", Type: "int", HasDef: true, Default: "1"})
		},
		"new column no type":  func(n *db.TableDef) { n.Columns = append(n.Columns, db.Column{Name: "z"}) },
		"reorder primary key": func(n *db.TableDef) { n.PrimaryKey = []string{"ck", "id"} },
		"shorter key":         func(n *db.TableDef) { n.PrimaryKey = []string{"id"} },
		"rename table":        func(n *db.TableDef) { n.Name = "u" },
		"foreign key":         func(n *db.TableDef) { n.ForeignKeys = []db.ForeignKey{{Name: "fk"}} },
		"two-column index":    func(n *db.TableDef) { n.Indexes = append(n.Indexes, db.Index{Name: "x", Columns: []string{"a", "b"}}) },
	} {
		if got, err := edit(f); err == nil {
			t.Errorf("%s must fail, got %v", name, got)
		}
	}
}

func TestGoValue(t *testing.T) {
	i := func(ti gocql.Type) gocql.TypeInfo { return gocql.NewNativeType(4, ti, "") }
	ok := func(ty gocql.Type, in, want any) {
		t.Helper()
		got, err := goValue(i(ty), in)
		if err != nil {
			t.Errorf("%v %v: %v", ty, in, err)
			return
		}
		switch w := want.(type) {
		case *big.Int:
			if got.(*big.Int).Cmp(w) != 0 {
				t.Errorf("%v %v: %v", ty, in, got)
			}
		case *inf.Dec:
			if got.(*inf.Dec).Cmp(w) != 0 {
				t.Errorf("%v %v: %v", ty, in, got)
			}
		case []byte:
			if string(got.([]byte)) != string(w) {
				t.Errorf("%v %v: %v", ty, in, got)
			}
		case net.IP:
			if !got.(net.IP).Equal(w) {
				t.Errorf("%v %v: %v", ty, in, got)
			}
		default:
			if got != want {
				t.Errorf("%v %v: got %#v want %#v", ty, in, got, want)
			}
		}
	}
	ok(gocql.TypeInt, " 42 ", int64(42))
	ok(gocql.TypeBigInt, "-9000000000", int64(-9000000000))
	ok(gocql.TypeInt, int64(7), int64(7))
	ok(gocql.TypeCounter, "5", int64(5))
	ok(gocql.TypeVarint, "123456789012345678901234567890", func() *big.Int { n, _ := new(big.Int).SetString("123456789012345678901234567890", 10); return n }())
	ok(gocql.TypeBoolean, "TRUE", true)
	ok(gocql.TypeBoolean, true, true)
	ok(gocql.TypeFloat, "0.5", float32(0.5))
	ok(gocql.TypeFloat, 0.5, float32(0.5))
	ok(gocql.TypeDouble, "2.25", 2.25)
	ok(gocql.TypeDecimal, "1.50", inf.NewDec(150, 2))
	ok(gocql.TypeDecimal, int64(3), inf.NewDec(3, 0))
	ok(gocql.TypeText, "ünï", "ünï")
	ok(gocql.TypeText, db.Expr("x"), db.Expr("x"))
	ok(gocql.TypeBlob, "0xCAFE", []byte{0xca, 0xfe})
	ok(gocql.TypeBlob, `\xcafe`, []byte{0xca, 0xfe})
	ok(gocql.TypeBlob, []byte{1}, []byte{1})
	ok(gocql.TypeInet, "::1", net.ParseIP("::1"))
	ok(gocql.TypeTime, "01:02:03.5", time.Hour+2*time.Minute+3*time.Second+500*time.Millisecond)
	ok(gocql.TypeDate, "2024-01-02", "2024-01-02")
	ts := time.Date(2024, 1, 2, 3, 4, 5, 600000000, time.UTC)
	for _, in := range []string{"2024-01-02 03:04:05.6", "2024-01-02T03:04:05.6", "2024-01-02 03:04:05.6+0000", "2024-01-02T03:04:05.6Z", "2024-01-02 03:04:05.6+00:00"} {
		ok(gocql.TypeTimestamp, in, ts)
	}
	ok(gocql.TypeTimestamp, "2024-01-02", time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC))
	if got, _ := goValue(i(gocql.TypeTimestamp), "2024-01-02 03:04:05+0200"); !got.(time.Time).Equal(time.Date(2024, 1, 2, 1, 4, 5, 0, time.UTC)) {
		t.Errorf("zone offset: %v", got)
	}
	ok(gocql.TypeTimestamp, ts, ts)
	if got, err := goValue(nil, "x"); err != nil || got != "x" {
		t.Error("unknown column type passes through")
	}
	for ty, bad := range map[gocql.Type]string{gocql.TypeInt: "1.5", gocql.TypeVarint: "x", gocql.TypeBoolean: "ja", gocql.TypeFloat: "x", gocql.TypeDouble: "x", gocql.TypeDecimal: "x",
		gocql.TypeTimestamp: "later", gocql.TypeTime: "25:61", gocql.TypeBlob: "xyz", gocql.TypeInet: "300.1.1.1", gocql.TypeUUID: "nope"} {
		if _, err := goValue(i(ty), bad); err == nil {
			t.Errorf("%v %q must fail", ty, bad)
		}
	}
}

func TestNormalizeValues(t *testing.T) {
	text := gocql.NewNativeType(4, gocql.TypeText, "")
	intT := gocql.NewNativeType(4, gocql.TypeInt, "")
	dateT := gocql.NewNativeType(4, gocql.TypeDate, "")
	dec := new(inf.Dec).SetUnscaled(12345).SetScale(2)
	uid, _ := gocql.ParseUUID("123e4567-e89b-12d3-a456-426614174000")
	for _, c := range []struct {
		ti   gocql.TypeInfo
		in   any
		want any
	}{
		{intT, int(5), int64(5)}, {gocql.NewNativeType(4, gocql.TypeTinyInt, ""), int8(-3), int64(-3)}, {gocql.NewNativeType(4, gocql.TypeSmallInt, ""), int16(9), int64(9)},
		{gocql.NewNativeType(4, gocql.TypeFloat, ""), float32(0.1), 0.1}, {text, "x", "x"}, {gocql.NewNativeType(4, gocql.TypeDecimal, ""), dec, "123.45"},
		{gocql.NewNativeType(4, gocql.TypeVarint, ""), big.NewInt(7), int64(7)},
		{gocql.NewNativeType(4, gocql.TypeVarint, ""), new(big.Int).Lsh(big.NewInt(1), 100), "1267650600228229401496703205376"},
		{gocql.NewNativeType(4, gocql.TypeUUID, ""), uid, "123e4567-e89b-12d3-a456-426614174000"}, {gocql.NewNativeType(4, gocql.TypeInet, ""), net.ParseIP("10.1.2.3"), "10.1.2.3"},
		{dateT, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), "2024-01-02"},
		{gocql.NewNativeType(4, gocql.TypeTimestamp, ""), time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 3600)), time.Date(2024, 1, 2, 2, 4, 5, 0, time.UTC)},
		{gocql.NewNativeType(4, gocql.TypeTime, ""), 3*time.Hour + 4*time.Minute, "03:04:00"},
		{gocql.NewNativeType(4, gocql.TypeDuration, ""), gocql.Duration{Months: 1, Days: 2, Nanoseconds: 3}, "1mo2d3ns"},
		{gocql.NewNativeType(4, gocql.TypeDuration, ""), gocql.Duration{}, "0s"},
		{gocql.NewNativeType(4, gocql.TypeBlob, ""), []byte{1, 2}, []byte{1, 2}},
	} {
		got := normalize(c.ti, c.in)
		if b, ok := c.want.([]byte); ok {
			if string(got.([]byte)) != string(b) {
				t.Errorf("%T: %v", c.in, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("%T %v: got %#v want %#v", c.in, c.in, got, c.want)
		}
	}
	// collections, tuples, UDTs and vectors become JSON text, dates and blobs inside them in their CQL spelling
	if got := normalize(gocql.TupleTypeInfo{Elems: []gocql.TypeInfo{intT, text}}, []any{1, "a"}); got != `[1,"a"]` {
		t.Errorf("tuple %v", got)
	}
	if got := normalize(gocql.TupleTypeInfo{Elems: []gocql.TypeInfo{intT, text}}, []any{nil, "a"}); got != `[null,"a"]` {
		t.Errorf("tuple with NULL %v", got)
	}
	if got := normalize(gocql.UDTTypeInfo{Name: "u", Elements: []gocql.UDTField{{Name: "d", Type: dateT}, {Name: "n", Type: intT}}},
		map[string]any{"d": time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)}); got != `{"d":"2024-01-02","n":null}` {
		t.Errorf("udt %v", got)
	}
	if got := normalize(gocql.VectorType{SubType: gocql.NewNativeType(4, gocql.TypeFloat, ""), Dimensions: 2}, []float32{0.5, 1}); got != `[0.5,1]` {
		t.Errorf("vector %v", got)
	}
	if typeName(gocql.TupleTypeInfo{Elems: []gocql.TypeInfo{intT, text}}) != "tuple<int, text>" || typeName(gocql.UDTTypeInfo{Name: "addr"}) != "addr" ||
		typeName(gocql.VectorType{SubType: intT, Dimensions: 3}) != "vector<int, 3>" || typeName(text) != "text" {
		t.Error("type names")
	}
}

func TestPlanRejectsKeyEdits(t *testing.T) {
	// rejected before the column types are read, so no server is needed
	_, err := unitSession().Plan([]db.Change{{Kind: db.Update, ObjRef: db.ObjRef{Name: "t", Kind: db.KindTable},
		Key: []db.KeyVal{{Col: "id", Val: int64(1)}}, Set: []db.KeyVal{{Col: "id", Val: "2"}}}})
	if err == nil || !strings.Contains(err.Error(), "primary key column id") {
		t.Fatalf("got %v", err)
	}
}

func TestScriptKinds(t *testing.T) {
	s := unitSession()
	for _, c := range []struct {
		kind db.ScriptKind
		obj  db.Kind
	}{{db.ScriptTruncate, db.KindMatView}, {db.ScriptTruncate, db.KindFunction}, {db.ScriptSelectTop, db.KindType}, {db.ScriptSelectTop, db.KindFunction}} {
		if got, err := s.Script(context.Background(), c.kind, db.ObjRef{Name: "x", Kind: c.obj}); err == nil {
			t.Errorf("%s of a %s: %q, want an error", c.kind, c.obj, got)
		}
	}
	if got, err := s.Script(context.Background(), db.ScriptSelectTop, db.ObjRef{Name: "v", Kind: db.KindMatView}); err != nil || got != `SELECT * FROM "ks"."v" LIMIT 100;` {
		t.Errorf("select of a view: %q %v", got, err)
	}
}

func TestBatchable(t *testing.T) {
	ins := db.Statement{Text: `INSERT INTO "t" ("a") VALUES (?)`}
	upd := db.Statement{Text: `update "t" set "a" = ? where "id" = ?`}
	cnt := db.Statement{Text: `UPDATE "t" SET "n" = "n" + ? WHERE "id" = ?`}
	ddl := db.Statement{Text: `ALTER TABLE "t" ADD x int`}
	if !batchable([]db.Statement{ins, upd}) || batchable([]db.Statement{ins}) || batchable([]db.Statement{ins, cnt}) || batchable([]db.Statement{ins, ddl}) || batchable(nil) {
		t.Error("batchable")
	}
	if batchable(make([]db.Statement, maxBatch+1)) {
		t.Error("too many")
	}
	if !counterRe.MatchString(cnt.Text) || counterRe.MatchString(upd.Text) {
		t.Error("counter detection")
	}
}

func TestLiteral(t *testing.T) {
	uid, _ := gocql.ParseUUID("123e4567-e89b-12d3-a456-426614174000")
	for _, c := range []struct {
		in   any
		want string
	}{{"it's", "'it''s'"}, {[]byte{0xca, 0xfe}, "0xcafe"}, {int64(5), "5"}, {true, "true"}, {uid, "123e4567-e89b-12d3-a456-426614174000"},
		{net.ParseIP("::1"), "'::1'"}, {time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), "'2024-01-02 03:04:05.000+0000'"}, {inf.NewDec(150, 2), "1.50"}, {float32(0.5), "0.5"}} {
		in, want := c.in, c.want
		if got := literal(in); got != want {
			t.Errorf("%T: %q want %q", in, got, want)
		}
	}
}

func TestFilterChecks(t *testing.T) {
	if err := checkFilters(db.FilterSet{Filters: []db.Filter{{Column: "a", Op: "=", Enabled: true}, {Column: "b", Op: "=", Enabled: true}}, Or: true}); err == nil {
		t.Error("OR")
	}
	if err := checkFilters(db.FilterSet{Filters: []db.Filter{{Column: "a", Op: "=", Enabled: true}}, Or: true}); err != nil {
		t.Error("a single filter with OR set is fine")
	}
	if err := checkFilters(db.FilterSet{Filters: []db.Filter{{Column: db.AnyColumn, Op: "=", Enabled: true}}}); err == nil {
		t.Error("any column")
	}
	if err := checkFilters(db.FilterSet{Filters: []db.Filter{{Column: db.AnyColumn, Op: "=", Enabled: false}}}); err != nil {
		t.Error("disabled filters are ignored")
	}
	types := map[string]gocql.TypeInfo{"n": gocql.NewNativeType(4, gocql.TypeInt, ""), "s": gocql.NewNativeType(4, gocql.TypeText, "")}
	fl := db.FilterSet{Filters: []db.Filter{{Column: "n", Op: "IN", Value: "1, 2", Enabled: true}, {Column: db.RawColumn, Value: "x = 1", Enabled: true}, {Column: "s", Op: "=", Value: "a", Enabled: true}}}
	args := []any{"1", " 2", "a"}
	if err := filterArgs(fl, types, args); err != nil || args[0] != int64(1) || args[1] != int64(2) || args[2] != "a" {
		t.Errorf("args %v %v", args, err)
	}
	if err := filterArgs(db.FilterSet{Filters: []db.Filter{{Column: "n", Op: "=", Value: "x", Enabled: true}}}, types, []any{"x"}); err == nil || !strings.Contains(err.Error(), "filter on n") {
		t.Errorf("bad int: %v", err)
	}
	text, args2, err := db.BuildSelect(db.CQL, db.RowQuery{ObjRef: db.ObjRef{Schema: "ks", Name: "t"}, Filters: db.FilterSet{Filters: []db.Filter{{Column: db.RawColumn, Value: "a = 1 AND b = 2", Enabled: true}, {Column: "n", Op: ">", Value: "3", Enabled: true}}}, Limit: 5}, nil)
	if err != nil || text != `SELECT * FROM "ks"."t" WHERE a = 1 AND b = 2 AND "n" > ? LIMIT 5` || len(args2) != 1 {
		t.Errorf("select: %s %v %v", text, args2, err)
	}
}

func TestReviewFixes(t *testing.T) {
	// CAS-01: a delete and an insert on one table must not share a batch timestamp
	del := db.Statement{Text: `DELETE FROM "ks"."t" WHERE "id" = ?`}
	ins := db.Statement{Text: `INSERT INTO "ks"."t" ("id") VALUES (?)`}
	other := db.Statement{Text: `INSERT INTO "ks"."u" ("id") VALUES (?)`}
	if batchable([]db.Statement{del, ins}) || !batchable([]db.Statement{del, other}) || !batchable([]db.Statement{ins, other}) {
		t.Error("delete+insert on one table must not batch")
	}
	// CAS-02: an overflowing counter delta is rejected, not clamped
	s := unitSession()
	s.types["ks.t"] = map[string]gocql.TypeInfo{"n": gocql.NewNativeType(4, gocql.TypeCounter, ""), "id": gocql.NewNativeType(4, gocql.TypeInt, "")}
	_, err := s.Plan([]db.Change{{Kind: db.Update, ObjRef: db.ObjRef{Name: "t", Kind: db.KindTable}, Key: []db.KeyVal{{Col: "id", Val: int64(1)}}, Set: []db.KeyVal{{Col: "n", Val: "+99999999999999999999"}}}})
	if err == nil {
		t.Error("overflowing counter delta accepted")
	}
	// CM-01: a USE behind comments is still a USE
	for _, in := range []string{"-- c\nUSE ks", "/* c */ use ks;", "// c\n  USE ks"} {
		if !useRe.MatchString(in) {
			t.Errorf("%q not recognised as USE", in)
		}
	}
	// CC-01: typed nils from a UDT map are NULL, not a panic
	udt := gocql.UDTTypeInfo{Elements: []gocql.UDTField{{Name: "v", Type: gocql.NewNativeType(4, gocql.TypeVarint, "")}, {Name: "d", Type: gocql.NewNativeType(4, gocql.TypeDecimal, "")}}}
	got := jsonValue(udt, map[string]any{"v": (*big.Int)(nil), "d": (*inf.Dec)(nil)}).(map[string]any)
	if got["v"] != nil || got["d"] != nil {
		t.Errorf("udt typed nils: %v", got)
	}
}
