package db

import (
	"context"
	"errors"
	"testing"
)

type searchFake struct {
	Session // only Objects, CurrentSchema and Count are used
	counts  map[string]int64
	cancel  func()
}

func (f *searchFake) CurrentSchema() string { return "public" }
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

func TestSearchEqualsIsLikeText(t *testing.T) {
	f := &searchFake{counts: map[string]int64{}}
	var got SearchHit
	Search(context.Background(), f, SearchOptions{Value: "50%_", Op: "=", Tables: []string{"a"}}, func(h SearchHit) { got = h })
	if x := got.Filters.Filters[0]; x.Op != "LIKE" || x.Value != `50\%\_` {
		t.Errorf("%+v", x)
	}
}
