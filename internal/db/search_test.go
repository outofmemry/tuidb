package db

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type searchFake struct {
	Session // only Objects, CurrentSchema and Count are used
	counts  map[string]int64
	cancel  func()
}

func (f *searchFake) Caps() Capabilities                        { return Capabilities{Dialect: "postgres", Schemas: true} }
func (f *searchFake) Schemas(context.Context) ([]string, error) { return []string{"public"}, nil }
func (f *searchFake) CurrentSchema() string                     { return "public" }
func (f *searchFake) Objects(context.Context, string) ([]Object, error) {
	r := func(n string, k Kind) Object { return Object{ObjRef: ObjRef{Schema: "public", Name: n, Kind: k}} }
	return []Object{r("a", KindTable), r("v", KindView), r("b", KindTable), r("c", KindTable)}, nil
}
func (f *searchFake) Count(_ context.Context, q RowQuery, _ bool) (Count, error) {
	if f.cancel != nil {
		f.cancel()
	}
	if q.Name == "b" {
		return Count{}, errors.New("cannot cast")
	}
	return Count{N: f.counts[q.Name]}, nil
}

func TestSearch(t *testing.T) {
	f := &searchFake{counts: map[string]int64{"a": 2, "c": 0}}
	var got []SearchHit
	err := Search(context.Background(), f, SearchOptions{Value: "x", Op: "Contains"}, func(h SearchHit) { got = append(got, h) })
	if err != nil || len(got) != 3 {
		t.Fatalf("err=%v hits=%+v", err, got)
	}
	if got[0].Ref.Name != "a" || got[0].Count != 2 || got[1].Ref.Name != "b" || got[1].Err == nil || got[2].Ref.Name != "c" || got[2].Count != 0 {
		t.Errorf("hits: %+v", got)
	}
	if f := got[0].Filters.Filters[0]; f.Column != AnyColumn || f.Op != "Contains" || f.Value != "x" || !f.Enabled {
		t.Errorf("filter: %+v", f)
	}

	got = nil
	err = Search(context.Background(), f, SearchOptions{Value: "x", Op: "=", Tables: []string{"c"}}, func(h SearchHit) { got = append(got, h) })
	if err != nil || len(got) != 1 || got[0].Ref.Name != "c" {
		t.Errorf("tables filter: %v %+v", err, got)
	}
}

func TestSearchCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &searchFake{counts: map[string]int64{"a": 1}}
	var got []SearchHit
	// cancelling during the first Count stops before reporting it half-done; cancel after it instead
	n := 0
	f.cancel = func() {
		if n++; n == 1 {
			return
		}
		cancel()
	}
	err := Search(ctx, f, SearchOptions{Value: "x", Op: "Contains"}, func(h SearchHit) { got = append(got, h) })
	if !errors.Is(err, context.Canceled) || len(got) != 1 || got[0].Ref.Name != "a" {
		t.Errorf("err=%v hits=%+v", err, got)
	}
}

func TestSearchEqualsStaysEquals(t *testing.T) {
	f := &searchFake{counts: map[string]int64{}}
	var got SearchHit
	Search(context.Background(), f, SearchOptions{Value: "50%_", Op: "=", Tables: []string{"a"}}, func(h SearchHit) { got = h })
	if x := got.Filters.Filters[0]; x.Op != "=" || x.Value != "50%_" {
		t.Errorf("%+v", x)
	}
}

type searchDB struct {
	searchFake
	caps Capabilities
	dbs  []string
	seen []string
}

func (f *searchDB) Caps() Capabilities                          { return f.caps }
func (f *searchDB) Databases(context.Context) ([]string, error) { return f.dbs, nil }
func (f *searchDB) Objects(_ context.Context, schema string) ([]Object, error) {
	f.seen = append(f.seen, schema)
	return []Object{{ObjRef: ObjRef{Schema: schema, Name: "t", Kind: KindTable}}}, nil
}

func TestSearchAllSchemasUsesDatabases(t *testing.T) {
	f := &searchDB{caps: Capabilities{Dialect: "mysql"}, dbs: []string{"a", "mysql", "b"}}
	var n int
	err := Search(context.Background(), f, SearchOptions{Value: "x", Op: "Contains", AllSchemas: true}, func(SearchHit) { n++ })
	if err != nil || n != 2 || len(f.seen) != 2 || f.seen[0] != "a" || f.seen[1] != "b" {
		t.Fatalf("err=%v n=%d seen=%v", err, n, f.seen)
	}
	f = &searchDB{caps: Capabilities{Dialect: "mysql"}}
	if err := Search(context.Background(), f, SearchOptions{Value: "x", Op: "Contains", AllSchemas: true}, func(SearchHit) {}); err == nil {
		t.Error("no databases and no schemas must be refused")
	}
}

func TestSearchValidatesOp(t *testing.T) {
	f := &searchDB{caps: Capabilities{Dialect: "partiql"}, dbs: []string{"a"}}
	if err := Search(context.Background(), f, SearchOptions{Value: "x", Op: "Has suffix"}, func(SearchHit) {}); err == nil {
		t.Error("Has suffix is not a PartiQL operator")
	}
	if len(f.seen) != 0 {
		t.Error("nothing may be searched after a bad op")
	}
}

func TestSearchEqualsRendering(t *testing.T) {
	fs := FilterSet{Filters: []Filter{{Column: AnyColumn, Op: "=", Value: "x", Enabled: true}}}
	w, _, _ := fs.Where(PG, []string{"a"}, 1)
	if w != "(CAST(\"a\" AS TEXT) = $1)" {
		t.Errorf("pg: %s", w)
	}
	w, _, _ = fs.Where(MySQL, []string{"a"}, 1)
	if w != "(CAST(`a` AS CHAR) = ?)" { // a native compare would coerce 'bob' to 0
		t.Errorf("mysql: %s", w)
	}
}

func TestSearchAllSchemasOnlyWhereObjectsHonourIt(t *testing.T) {
	for _, d := range []string{"redis", "mongo"} {
		f := &searchDB{caps: Capabilities{Dialect: d}, dbs: []string{"0", "1", "2"}}
		var n int
		if err := Search(context.Background(), f, SearchOptions{Value: "x", Op: "Contains", AllSchemas: true}, func(SearchHit) { n++ }); err != nil || n != 1 {
			t.Errorf("%s: err=%v n=%d seen=%v", d, err, n, f.seen)
		}
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
