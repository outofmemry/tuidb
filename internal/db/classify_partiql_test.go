package db

import (
	"reflect"
	"strings"
	"testing"
)

func TestPartiQLSplit(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{`SELECT * FROM "t"; SELECT 2`, []string{`SELECT * FROM "t"`, "SELECT 2"}},
		{`INSERT INTO "t" VALUE {'a': 'x;y', 'b': <<1, ';'>>}; SELECT 1`, []string{`INSERT INTO "t" VALUE {'a': 'x;y', 'b': <<1, ';'>>}`, "SELECT 1"}},
		{`SELECT * FROM "a;b" WHERE x = 'it''s;ok'; DELETE FROM "a;b"`, []string{`SELECT * FROM "a;b" WHERE x = 'it''s;ok'`, `DELETE FROM "a;b"`}},
		{`SELECT 'a\'; SELECT 2`, []string{`SELECT 'a\'`, "SELECT 2"}}, // no backslash escapes in PartiQL strings
		{"SELECT 1 -- ; c\n; SELECT 2", []string{"SELECT 1 -- ; c", "SELECT 2"}},
		{"SELECT /* ; */ 1; SELECT 2", []string{"SELECT /* ; */ 1", "SELECT 2"}},
		{"SELECT 1 # not a comment; SELECT 2", []string{"SELECT 1 # not a comment", "SELECT 2"}},
		{"SELECT $$a;b$$", []string{"SELECT $$a", "b$$"}}, // no dollar quoting
		{"-- only a comment\n/* x; */ ;", nil},
	} {
		var got []string
		for _, s := range Split("partiql", c.in) {
			got = append(got, s.Text)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q:\n got  %q\n want %q", c.in, got, c.want)
		}
	}
}

func TestPartiQLReadOnly(t *testing.T) {
	for stmt, want := range map[string]bool{
		`SELECT * FROM "t"`: true,
		`  select a, b from "t" where c = 'x' and d in [1, 2]`:  true,
		`SELECT * FROM "t" WHERE a = 'DELETE FROM t'`:           true,
		`SELECT * FROM "t" WHERE a = 'it''s' -- DELETE`:         true,
		"-- DELETE FROM t\nSELECT 1":                            true,
		`SELECT * FROM "t";`:                                    true,
		"":                                                      true,
		"-- nothing":                                            true,
		`INSERT INTO "t" VALUE {'id': 1}`:                       false,
		`UPDATE "t" SET a = 1 WHERE id = 1`:                     false,
		`DELETE FROM "t" WHERE id = 1`:                          false,
		`DROP TABLE "t"`:                                        false,
		`TRUNCATE TABLE "t"`:                                    false,
		`CREATE TABLE "t" {"keys": []}`:                         false,
		`ALTER TABLE "t" ADD INDEX {}`:                          false,
		`EXISTS(SELECT * FROM "t" WHERE id = 1)`:                false,
		`SELECT * FROM "t"; DELETE FROM "t" WHERE id = 1`:       false,
		`SELECT * FROM "t" WHERE a = '--'; DELETE FROM "t"`:     false,
		`SELECT 'a\'; DELETE FROM "t" -- '`:                     false, // the quote closes at the backslash: the DELETE runs
		`/* SELECT */ DELETE FROM "t" WHERE id = 1`:             false,
		`/* a /* b */ c */ DELETE FROM "t"`:                     false, // block comments do not nest: " c */ DELETE" is code
		`UPDATE "t" SET a = 1 WHERE id = 1 RETURNING ALL OLD *`: false,
		`SELECT * FROM "t"; -- trailing comment`:                true,
		"SELECT * FROM \"t\";\n;\n":                             true,
	} {
		if got := IsReadOnly("partiql", stmt); got != want {
			t.Errorf("IsReadOnly(%q) = %v, want %v", stmt, got, want)
		}
	}
}

func TestPartiQLFilters(t *testing.T) {
	d, ok := DialectFor("partiql")
	if !ok {
		t.Fatal("partiql dialect not registered")
	}
	fs := FilterSet{Filters: []Filter{
		{Column: "a", Op: "Contains", Value: "x", Enabled: true},
		{Column: "b", Op: "Has prefix", Value: "p", Enabled: true},
		{Column: "c", Op: "Not contains", Value: "z", Enabled: true},
		{Column: "d", Op: "IN", Value: "1, 2", Enabled: true},
	}}
	got, args, err := fs.Where(d, nil, 1)
	want := `contains("a", ?) AND begins_with("b", ?) AND NOT contains("c", ?) AND "d" IN (?, ?)`
	if err != nil || got != want || len(args) != 5 || args[0] != "x" || args[1] != "p" {
		t.Errorf("where %q %v %v", got, args, err)
	}
	for _, op := range []string{"LIKE", "ILIKE", "Has suffix"} {
		if _, _, err := (FilterSet{Filters: []Filter{{Column: "a", Op: op, Value: "x", Enabled: true}}}).Where(d, nil, 1); err == nil {
			t.Errorf("%s accepted for PartiQL", op)
		}
	}
	any, _, err := (FilterSet{Filters: []Filter{{Column: AnyColumn, Op: "Contains", Value: "x", Enabled: true}}}).Where(d, []string{"a", "b"}, 1)
	if err != nil || any != `(contains("a", ?) OR contains("b", ?))` {
		t.Errorf("any column %q %v", any, err)
	}
	if q := d.QualifiedName(ObjRef{Name: `we"ird`}); q != `"we""ird"` {
		t.Errorf("quote %s", q)
	}
	ops := strings.Join(Operators("partiql"), "|")
	if strings.Contains(ops, "LIKE") || strings.Contains(ops, "suffix") || !strings.Contains(ops, "Has prefix") {
		t.Errorf("operators %s", ops)
	}
	if !strings.Contains(strings.Join(Operators("mysql"), "|"), "LIKE") {
		t.Error("other dialects lost LIKE")
	}
}
