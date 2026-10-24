package db

import (
	"reflect"
	"testing"
)

func TestExplainStatement(t *testing.T) {
	for d, want := range map[string]string{
		"postgres":   "EXPLAIN (FORMAT JSON, ANALYZE false) select 1",
		"mysql":      "EXPLAIN FORMAT=JSON select 1",
		"clickhouse": "EXPLAIN select 1",
		"redis":      "",
		"text":       "EXPLAIN select 1",
		"none":       "",
	} {
		if got := ExplainStatement(d, "select 1"); got != want {
			t.Errorf("%s: %q", d, got)
		}
	}
}

func TestExplainMode(t *testing.T) {
	if m := (Capabilities{Dialect: "postgres"}).ExplainMode(); m != "postgres" {
		t.Error(m)
	}
	if m := (Capabilities{Dialect: "postgres", Explain: "text"}).ExplainMode(); m != "text" {
		t.Error(m)
	}
}

func rows1(s string) *Result { return &Result{Rows: [][]any{{s}}} }

func TestExplainTree(t *testing.T) {
	pg := `[{"Plan":{"Node Type":"Hash Join","Total Cost":12.5,"Plan Rows":10,"Plans":[
		{"Node Type":"Seq Scan","Relation Name":"a","Total Cost":1,"Plan Rows":5},
		{"Node Type":"Index Scan","Index Name":"b_pk","Relation Name":"b","Total Cost":2.25,"Plan Rows":7}]}}]`
	got, err := ExplainTree("postgres", rows1(pg))
	want := []ExplainNode{{0, "Hash Join", "12.50", "10"}, {1, "Seq Scan on a", "1.00", "5"}, {1, "Index Scan using b_pk on b", "2.25", "7"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("pg: %v %+v", err, got)
	}

	my := `{"query_block":{"cost_info":{"query_cost":"3.5"},"nested_loop":[{"table":{"table_name":"a","access_type":"ALL","rows_examined_per_scan":4,"cost_info":{"read_cost":"1.0"}}},{"table":{"table_name":"b","access_type":"ref","rows_examined_per_scan":1}}]}}`
	got, err = ExplainTree("mysql", rows1(my))
	want = []ExplainNode{{0, "query_block", "3.5", ""}, {1, "table a (ALL)", "1.0", "4"}, {1, "table b (ref)", "", "1"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("mysql: %v %+v", err, got)
	}

	got, err = ExplainTree("clickhouse", &Result{Rows: [][]any{{"Expression (Projection)"}, {"  ReadFromMergeTree (t)"}}})
	want = []ExplainNode{{0, "Expression (Projection)", "", ""}, {1, "ReadFromMergeTree (t)", "", ""}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("ch: %v %+v", err, got)
	}
	if _, err := ExplainTree("postgres", rows1("Seq Scan")); err == nil {
		t.Error("text plan on postgres should fail")
	}
}
