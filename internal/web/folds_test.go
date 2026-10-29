package web

import (
	"testing"
)

func TestFoldsAndSplit(t *testing.T) {
	e := newEnv(t, "alert1")
	if code, _ := e.call("PUT", "/api/state/folds?key=c1/tab1", []int{1, 3}, "tok"); code != 200 {
		t.Fatalf("put: %d", code)
	}
	if got := e.s.o.State.Folds("c1/tab1"); len(got) != 2 || got[1] != 3 {
		t.Fatalf("folds: %v", got)
	}
	if code, _ := e.call("GET", "/api/state/folds", nil, "tok"); code != 400 {
		t.Fatalf("key required: %d", code)
	}
}
