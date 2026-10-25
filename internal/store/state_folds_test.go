package store

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestFolds(t *testing.T) {
	s, err := LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetFolds("q/a.sql", []int{0, 2}); err != nil {
		t.Fatal(err)
	}
	again, _ := LoadState(s.Path)
	if got := again.Folds("q/a.sql"); !reflect.DeepEqual(got, []int{0, 2}) {
		t.Fatalf("got %v", got)
	}
	s.SetFolds("q/a.sql", nil)
	if got := s.Folds("q/a.sql"); len(got) != 0 {
		t.Fatalf("not forgotten: %v", got)
	}
}
