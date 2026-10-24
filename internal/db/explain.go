package db

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ExplainStatement wraps stmt in the engine's plan-only EXPLAIN, or returns "" for engines without one.
// mode is Capabilities.ExplainMode: a dialect, or "text" for engines that only print a plain-text plan.
func ExplainStatement(mode, stmt string) string {
	switch mode {
	case "text":
		return "EXPLAIN " + stmt
	case "postgres":
		return "EXPLAIN (FORMAT JSON, ANALYZE false) " + stmt
	case "mysql":
		return "EXPLAIN FORMAT=JSON " + stmt
	case "clickhouse":
		return "EXPLAIN " + stmt
	}
	return ""
}

// ExplainNode is one plan step; Depth is its nesting level (0 = root).
type ExplainNode struct {
	Depth      int
	Label      string
	Cost, Rows string
}

// ExplainTree turns the result of an ExplainStatement into an indented tree. PostgreSQL and MySQL return
// a JSON document; anything else is plain text, one node per line, nested by leading blanks (two per level).
func ExplainTree(dialect string, res *Result) ([]ExplainNode, error) {
	if res == nil || len(res.Rows) == 0 {
		return nil, fmt.Errorf("explain: empty plan")
	}
	var parts []string
	for _, r := range res.Rows {
		if len(r) > 0 {
			parts = append(parts, fmt.Sprint(textOf(r[0])))
		}
	}
	text := strings.Join(parts, "\n")
	var out []ExplainNode
	switch dialect {
	case "postgres":
		var doc []map[string]any
		if err := json.Unmarshal([]byte(text), &doc); err != nil || len(doc) == 0 {
			return nil, fmt.Errorf("explain: not a PostgreSQL JSON plan")
		}
		pgWalk(doc[0]["Plan"], 0, &out)
	case "mysql":
		var doc map[string]any
		if err := json.Unmarshal([]byte(text), &doc); err != nil {
			return nil, fmt.Errorf("explain: not a MySQL JSON plan")
		}
		myWalk("plan", doc, -1, &out)
	default:
		for _, l := range strings.Split(text, "\n") {
			if t := strings.TrimLeft(l, " \t"); t != "" {
				out = append(out, ExplainNode{Depth: (len(l) - len(t)) / 2, Label: strings.TrimRight(t, " \t\r")})
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("explain: empty plan")
	}
	return out, nil
}

func textOf(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

func pgWalk(v any, depth int, out *[]ExplainNode) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	label, _ := m["Node Type"].(string)
	if s, ok := m["Index Name"].(string); ok {
		label += " using " + s
	}
	if s, ok := m["Relation Name"].(string); ok {
		label += " on " + s
	}
	n := ExplainNode{Depth: depth, Label: label}
	if f, ok := m["Total Cost"].(float64); ok {
		n.Cost = fmt.Sprintf("%.2f", f)
	}
	if f, ok := m["Plan Rows"].(float64); ok {
		n.Rows = fmt.Sprintf("%.0f", f)
	}
	*out = append(*out, n)
	kids, _ := m["Plans"].([]any)
	for _, k := range kids {
		pgWalk(k, depth+1, out)
	}
}

// myWalk emits a node for every JSON object that is not just cost_info; arrays (nested_loop, ...) hold
// one-key wrappers like {"table": {...}} that are unwrapped.
func myWalk(key string, m map[string]any, depth int, out *[]ExplainNode) {
	if depth >= 0 {
		n := ExplainNode{Depth: depth, Label: key}
		if t, ok := m["table_name"].(string); ok {
			n.Label = "table " + t
			if a, ok := m["access_type"].(string); ok {
				n.Label += " (" + a + ")"
			}
		}
		if ci, ok := m["cost_info"].(map[string]any); ok {
			for _, k := range []string{"query_cost", "prefix_cost", "read_cost"} {
				if s, ok := ci[k].(string); ok {
					n.Cost = s
					break
				}
			}
		}
		for _, k := range []string{"rows_produced_per_join", "rows_examined_per_scan"} {
			if f, ok := m[k].(float64); ok {
				n.Rows = fmt.Sprintf("%.0f", f)
				break
			}
		}
		*out = append(*out, n)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch v := m[k].(type) {
		case map[string]any:
			if k != "cost_info" {
				myWalk(k, v, depth+1, out)
			}
		case []any:
			for _, e := range v {
				if em, ok := e.(map[string]any); ok {
					for _, ek := range sortedKeys(em) {
						if sub, ok := em[ek].(map[string]any); ok {
							myWalk(ek, sub, depth+1, out)
						}
					}
				}
			}
		}
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
