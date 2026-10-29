package web

import (
	"testing"
)

func TestTableStateMerges(t *testing.T) {
	e := newEnv(t, "")
	q := "/api/state/table?conn=c1&table=t"
	if code, _ := e.call("PUT", q, nil, "tok"); code != 400 {
		t.Fatalf("empty body: %d", code)
	}
	e.ok("PUT", q, map[string]any{"widths": map[string]int{"a": 9}, "hidden": []string{"x"}})
	e.ok("PUT", q, map[string]any{"show_filters": true})
	got := e.ok("GET", q, nil)
	if got["show_filters"] != true || got["widths"] == nil || got["hidden"] == nil {
		t.Fatalf("partial PUT lost fields: %v", got)
	}
}

// P2-34: an explicit empty sort is remembered as sort_none, so [table] default_sort does not come back.
func TestTableStateRemembersNoSort(t *testing.T) {
	e := newEnv(t, "")
	q := "/api/state/table?conn=c1&table=t2"
	e.ok("PUT", q, map[string]any{"sort": []map[string]any{{"Column": "a", "Desc": false}}})
	if got := e.ok("GET", q, nil); got["sort_none"] == true || got["sort"] == nil {
		t.Fatalf("a chosen sort: %v", got)
	}
	e.ok("PUT", q, map[string]any{"sort": []any{}})
	if got := e.ok("GET", q, nil); got["sort_none"] != true || got["sort"] != nil {
		t.Fatalf("un-sorting not remembered: %v", got)
	}
}
