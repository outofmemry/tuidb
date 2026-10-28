package db

import (
	"context"
	"slices"
	"strings"
	"testing"
)

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

type typesFake struct{ Session }

func (typesFake) Caps() Capabilities { return Capabilities{Dialect: "mysql"} }

type typesOwn struct{ typesFake }

func (typesOwn) Caps() Capabilities { return Capabilities{Dialect: "mysql", Types: []string{"x"}} }

func TestDataTypesOf(t *testing.T) {
	if !slices.Contains(DataTypesOf(typesFake{}), "bigint") {
		t.Error("mysql default list")
	}
	if got := DataTypesOf(typesOwn{}); len(got) != 1 || got[0] != "x" {
		t.Errorf("own list: %v", got)
	}
	for _, d := range []string{"postgres", "mysql", "clickhouse"} {
		for _, ty := range DefaultTypes(d) {
			if strings.ContainsAny(ty, `\ `) && ty != "double precision" {
				t.Errorf("%s: odd type %q", d, ty)
			}
		}
	}
	if !slices.Contains(DefaultTypes("postgres"), "double precision") {
		t.Error("double precision must be one entry")
	}
	if DataTypesOf(&mongoLike{}) != nil {
		t.Error("mongo has no types")
	}
}

type mongoLike struct{ Session }

func (*mongoLike) Caps() Capabilities { return Capabilities{Dialect: "mongo"} }

func TestSystemSchemasPerDialect(t *testing.T) {
	for dialect, want := range map[string][]string{
		"mysql":      {"a", "pg_catalog", "system", "b"},
		"clickhouse": {"a", "pg_catalog", "mysql", "sys", "performance_schema", "b"},
		"postgres":   {"a", "mysql", "sys", "performance_schema", "system", "INFORMATION_SCHEMA", "b"},
	} {
		got, err := SearchSchemas(t.Context(), &schemasFake{dialect: dialect}, SearchOptions{AllSchemas: true})
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%s: %v %v, want %v", dialect, err, got, want)
		}
	}
}

func TestSearchAllSchemas(t *testing.T) {
	f := &schemasFake{}
	var got []string
	err := Search(t.Context(), f, SearchOptions{Value: "x", Op: "Contains", AllSchemas: true}, func(h SearchHit) { got = append(got, h.Ref.Schema+"."+h.Ref.Name) })
	if err != nil || !slices.Equal(got, []string{"a.t", "b.t"}) {
		t.Errorf("%v %v", err, got)
	}
}

type schemasFake struct {
	Session
	dialect string
}

func (f *schemasFake) Caps() Capabilities {
	if f.dialect == "" {
		return Capabilities{Dialect: "postgres", Schemas: true}
	}
	return Capabilities{Dialect: f.dialect, Schemas: true}
}
func (*schemasFake) CurrentSchema() string { return "a" }
func (f *schemasFake) Schemas(_ context.Context) ([]string, error) {
	if len(f.dialect) > 0 {
		return []string{"a", "pg_catalog", "mysql", "sys", "performance_schema", "system", "INFORMATION_SCHEMA", "b"}, nil
	}
	return []string{"a", "pg_catalog", "b"}, nil
}
func (*schemasFake) Objects(_ context.Context, s string) ([]Object, error) {
	return []Object{{ObjRef: ObjRef{Schema: s, Name: "t", Kind: KindTable}}}, nil
}
func (*schemasFake) Count(context.Context, RowQuery, bool) (Count, error) { return Count{N: 1}, nil }

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
