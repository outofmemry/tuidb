package web

import (
	"testing"
)

func TestHistoryClearNeedsAll(t *testing.T) {
	e := newEnv(t, "")
	if code, _ := e.call("DELETE", "/api/history?conn=c1", nil, "tok"); code != 400 {
		t.Fatalf("clear without all: %d", code)
	}
}
