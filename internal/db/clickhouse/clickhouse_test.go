package clickhouse

import (
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"tuidb/internal/config"
	"tuidb/internal/db"
)

func TestNormalize(t *testing.T) {
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	u := uuid.MustParse("61f0c404-5cb3-11e7-907b-a6006ad3dba0")
	var nilStr *string
	s, i8, u32, f32 := "x", int8(-3), uint32(7), float32(0.1)
	for _, c := range []struct {
		in   any
		typ  string
		want any
	}{
		{nilStr, "Nullable(String)", nil},
		{&s, "Nullable(String)", "x"},
		{new(*string), "Nullable(String)", nil},
		{&i8, "Int8", int64(-3)},
		{&u32, "UInt32", uint64(7)},
		{&f32, "Float32", 0.1},
		{decimal.RequireFromString("120.5"), "Decimal(10, 2)", "120.50"},
		{decimal.RequireFromString("5"), "Nullable(Decimal(10, 2))", "5.00"},
		{decimal.RequireFromString("1.25"), "Array(Decimal(10, 2))", "1.25"},
		{ts, "DateTime", ts},
		{u, "UUID", u.String()},
		{net.ParseIP("10.0.0.1"), "IPv4", "10.0.0.1"},
		{[]string{"a", "b"}, "Array(String)", `["a","b"]`},
		{map[string]uint8{"k": 1}, "Map(String, UInt8)", `{"k":1}`},
		{[]any{uint8(1), "x"}, "Tuple(UInt8, String)", `[1,"x"]`},
	} {
		if got := normalize(c.in, c.typ); !reflect.DeepEqual(got, c.want) {
			t.Errorf("normalize(%#v, %s) = %#v, want %#v", c.in, c.typ, got, c.want)
		}
	}
}

func TestTLSConfig(t *testing.T) {
	for _, m := range []string{"", "prefer", "disable"} {
		if c, err := tlsConfig(config.Connection{SSLMode: m}); c != nil || err != nil {
			t.Errorf("%q: %v %v", m, c, err)
		}
	}
	c, err := tlsConfig(config.Connection{SSLMode: "require", Host: "h"})
	if err != nil || !c.InsecureSkipVerify || c.ServerName != "h" {
		t.Errorf("require: %+v %v", c, err)
	}
	c, _ = tlsConfig(config.Connection{SSLMode: "verify-full", Host: "h"})
	if c.InsecureSkipVerify || c.VerifyPeerCertificate != nil {
		t.Errorf("verify-full must verify: %+v", c)
	}
	c, _ = tlsConfig(config.Connection{SSLMode: "verify-ca", Host: "h"})
	if !c.InsecureSkipVerify || c.VerifyPeerCertificate == nil {
		t.Errorf("verify-ca: %+v", c)
	}
	for _, bad := range []config.Connection{{SSLMode: "bogus"}, {SSLMode: "require", SSLCA: "/nonexistent"}, {SSLMode: "require", SSLCert: "a"}} {
		if _, err := tlsConfig(bad); err == nil {
			t.Errorf("%+v should fail", bad)
		}
	}
}

func TestBootstrapSet(t *testing.T) {
	m := setRe.FindStringSubmatch("SET max_threads = 4;")
	if m == nil || m[1] != "max_threads" || settingValue(m[2]) != int64(4) {
		t.Errorf("%v", m)
	}
	if settingValue("'UTC'") != "UTC" || setRe.MatchString("SELECT 1") {
		t.Error("settingValue / setRe")
	}
}

func texts(st []db.Statement) []string {
	var out []string
	for _, s := range st {
		out = append(out, s.Text)
	}
	return out
}

func TestPlanAdmin(t *testing.T) {
	s := &session{}
	tbl := db.ObjRef{Name: "t", Kind: db.KindTable}
	for _, c := range []struct {
		op   db.AdminOp
		ref  db.ObjRef
		opts map[string]string
		want []string
	}{
		{db.OpDropObject, tbl, nil, []string{"DROP TABLE `t`"}},
		{db.OpDropObject, db.ObjRef{Name: "v", Kind: db.KindView}, nil, []string{"DROP VIEW `v`"}},
		{db.OpTruncate, tbl, nil, []string{"TRUNCATE TABLE `t`"}},
		{db.OpClone, tbl, map[string]string{"new_name": "t2"}, []string{"CREATE TABLE `t2` AS `t`"}},
		{db.OpClone, tbl, map[string]string{"new_name": "t2", "with_data": "true"}, []string{"CREATE TABLE `t2` AS `t`", "INSERT INTO `t2` SELECT * FROM `t`"}},
		{db.OpRenameObject, tbl, map[string]string{"new_name": "t3"}, []string{"RENAME TABLE `t` TO `t3`"}},
		{db.OpCreateDatabase, db.ObjRef{Name: "d"}, nil, []string{"CREATE DATABASE `d`"}},
		{db.OpDropDatabase, db.ObjRef{Name: "d"}, nil, []string{"DROP DATABASE `d`"}},
		{db.OpRenameDatabase, db.ObjRef{Name: "d"}, map[string]string{"new_name": "e"}, []string{"RENAME DATABASE `d` TO `e`"}},
	} {
		got, err := s.PlanAdmin(c.op, c.ref, c.opts)
		if err != nil || !reflect.DeepEqual(texts(got), c.want) {
			t.Errorf("%s: %q %v, want %q", c.op, texts(got), err, c.want)
		}
	}
	if _, err := s.PlanAdmin(db.OpClone, tbl, nil); err == nil {
		t.Error("clone without new_name must fail")
	}
}

func TestPlanTableCreate(t *testing.T) {
	s := &session{}
	got, err := s.PlanTable(nil, &db.TableDef{Name: "t", PrimaryKey: []string{"id"}, Columns: []db.Column{
		{Name: "id", Type: "UInt32"}, {Name: "n", Type: "String", Nullable: true, Default: "'x'", HasDef: true}}})
	want := "CREATE TABLE `t` (\n  `id` UInt32,\n  `n` Nullable(String) DEFAULT 'x'\n) ENGINE = MergeTree ORDER BY (`id`)"
	if err != nil || texts(got)[0] != want {
		t.Errorf("%q %v", texts(got), err)
	}
	got, _ = s.PlanTable(nil, &db.TableDef{Name: "t", Columns: []db.Column{{Name: "id", Type: "UInt32"}}})
	if !strings.HasSuffix(texts(got)[0], "ORDER BY tuple()") {
		t.Errorf("%q", texts(got))
	}
}

func TestPlanTableAlter(t *testing.T) {
	s := &session{}
	col := func(n, typ string) db.Column { return db.Column{Name: n, OrigName: n, Type: typ} }
	old := &db.TableDef{Name: "t", PrimaryKey: []string{"id"}, Columns: []db.Column{col("id", "UInt32"), col("a", "String"), col("b", "String"), col("c", "Int32")}}
	nw := &db.TableDef{Name: "t2", PrimaryKey: []string{"id"}, Columns: []db.Column{
		col("id", "UInt32"),
		{Name: "n", Type: "UInt8"},                                // new, after id
		{Name: "a2", OrigName: "a", Type: "String"},               // renamed
		{Name: "c", OrigName: "c", Type: "Int32", Nullable: true}, // moved before nothing (b dropped), type changed
	}}
	got, err := s.PlanTable(old, nw)
	want := []string{
		"ALTER TABLE `t` DROP COLUMN `b`",
		"ALTER TABLE `t` RENAME COLUMN `a` TO `a2`",
		"ALTER TABLE `t` ADD COLUMN `n` UInt8 AFTER `id`",
		"ALTER TABLE `t` MODIFY COLUMN `c` Nullable(Int32)",
		"RENAME TABLE `t` TO `t2`",
	}
	if err != nil || !reflect.DeepEqual(texts(got), want) {
		t.Errorf("got  %q\nwant %q\n%v", texts(got), want, err)
	}

	// pure reorder: c first
	nw2 := &db.TableDef{Name: "t", PrimaryKey: []string{"id"}, Columns: []db.Column{col("c", "Int32"), col("id", "UInt32"), col("a", "String"), col("b", "String")}}
	got, _ = s.PlanTable(old, nw2)
	if w := []string{"ALTER TABLE `t` MODIFY COLUMN `c` Int32 FIRST"}; !reflect.DeepEqual(texts(got), w) {
		t.Errorf("reorder: %q", texts(got))
	}

	nw3 := *old
	nw3.PrimaryKey = []string{"a"}
	if _, err := s.PlanTable(old, &nw3); err == nil || !strings.Contains(err.Error(), "sorting key") {
		t.Errorf("pk change: %v", err)
	}
	nw4 := *old
	nw4.ForeignKeys = []db.ForeignKey{{Name: "f"}}
	if _, err := s.PlanTable(old, &nw4); err == nil {
		t.Error("fk must fail")
	}
}

func TestSearchableSkipsHeavyTypes(t *testing.T) {
	for typ, want := range map[string]bool{
		"String": true, "LowCardinality(String)": true, "Nullable(String)": true, "UInt64": true, "DateTime64(9)": true, "UUID": true,
		"Map(LowCardinality(String), String)": false, "Array(String)": false, "Nested(a String)": false, "JSON": false,
		"Tuple(a String, b UInt8)": false, "Array(Tuple(a String))": false, "AggregateFunction(sum, UInt64)": false,
	} {
		if got := searchable(typ); got != want {
			t.Errorf("searchable(%q) = %v, want %v", typ, got, want)
		}
	}
}

func TestUseRe(t *testing.T) {
	for stmt, want := range map[string]string{"USE system": "system", " use `my db` ; ": "my db", `USE "x"`: "x", "use\ntuidb;": "tuidb", "-- to system\nUSE system": "system", "/* c */ USE x /* d */;-- e": "x", "USE/**/y": "y"} {
		m := useRe.FindStringSubmatch(stmt)
		if m == nil || m[1]+m[2]+m[3] != want {
			t.Errorf("%q: %q", stmt, m)
		}
	}
	for _, stmt := range []string{"SELECT 1", "USE a b", "user_table", "USE"} {
		if useRe.MatchString(stmt) {
			t.Errorf("%q matched", stmt)
		}
	}
}
