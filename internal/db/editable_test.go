package db

import "testing"

func TestEditableSelect(t *testing.T) {
	type r struct {
		schema, table string
		ok            bool
	}
	for in, want := range map[string]r{
		`select * from users`: {"", "users", true},
		`SELECT id, name AS name, u.email email FROM public.users u WHERE name = 'a (join)' ORDER BY id LIMIT 5;`: {"public", "users", true},
		"select `id` from `db`.`t` where id in (select 1)":                                                        {"db", "t", true},
		`select u.* from users as u`:                    {"", "users", true},
		`select * from a join b on a.id=b.id`:           {},
		`select * from a, b`:                            {},
		`select distinct id from a`:                     {},
		`select count(*) from a`:                        {},
		`select id from a group by id`:                  {},
		`select id from a union select id from b`:       {},
		`select id from (select 1) x`:                   {},
		`select 1 from a`:                               {},
		`select id from a; select 2`:                    {},
		`with x as (select 1) select * from x`:          {},
		`select id from a where name = 'it''s' limit 1`: {"", "a", true},
		`select name as id, id as name from t`:          {},
		`select name n from t`:                          {},
		`update a set b=1`:                              {},
	} {
		s, tb, ok := EditableSelect(in, "postgres")
		if s != want.schema || tb != want.table || ok != want.ok {
			t.Errorf("%q = %q %q %v, want %+v", in, s, tb, ok, want)
		}
	}
}

func TestStatementRangeAt(t *testing.T) {
	in := "select 1;\nselect 2;"
	if s, e, ok := StatementRangeAt("postgres", in, 12); !ok || in[s:e] != "select 2" {
		t.Errorf("%d %d %v", s, e, ok)
	}
	if _, _, ok := StatementRangeAt("postgres", "  ", 1); ok {
		t.Error("empty text has no statement")
	}
}

func TestEditableSelectDoubleQuoteByDialect(t *testing.T) {
	const q = `select "id" from t`
	if _, tb, ok := EditableSelect(q, "postgres"); !ok || tb != "t" {
		t.Errorf("postgres: \"id\" is an identifier: %v %q", ok, tb)
	}
	for _, d := range []string{"mysql", "mariadb"} {
		if _, _, ok := EditableSelect(q, d); ok {
			t.Errorf("%s: \"id\" is a string literal, so the column is not the PK", d)
		}
	}
}
