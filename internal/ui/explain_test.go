package ui

import (
	"strings"
	"testing"
)

func TestM5ExplainShowsTree(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select * from customers")
	h.run("explain")
	h.want("Explain")
	h.want("Hash Join")
	h.want("Seq Scan")
	if !strings.Contains(h.fake.execs[len(h.fake.execs)-1], "EXPLAIN (FORMAT JSON") {
		t.Fatalf("execs %v", h.fake.execs)
	}
}
