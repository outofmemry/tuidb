package web

import (
	"strings"
	"testing"
	"webtuidb/internal/config"
)

func TestFormatParamsChart(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) { c.Editor.QueryParams = true })
	id := e.open()
	f := e.ok("POST", "/api/format", map[string]any{"text": "select a,b from t where x=1", "mode": "beautify"})
	if !strings.Contains(f["text"].(string), "\n") {
		t.Fatalf("beautify: %v", f)
	}
	if u := e.ok("POST", "/api/format", map[string]any{"text": "select a\n  from t", "mode": "uglify"}); strings.Contains(u["text"].(string), "\n") {
		t.Fatalf("uglify: %v", u)
	}
	// parameters: asked for first, then bound
	code, out := e.call("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select :n"}, "tok")
	if code != 422 || out["params"].([]any)[0] != "n" {
		t.Fatalf("want the parameter names: %d %v", code, out)
	}
	e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select :n", "vars": map[string]string{"n": "5"}})
	if got := e.fake.execs[len(e.fake.execs)-1]; got != "select 5" {
		t.Fatalf("bound statement: %q", got)
	}
	c := e.ok("POST", "/api/chart", map[string]any{"Stmt": "-- piechart\nselect", "Columns": []string{"k", "n"}, "Cells": [][]any{{"a", "1"}, {"b", "5"}}})
	if c["kind"] != "pie" || c["labels"].([]any)[0] != "b" {
		t.Fatalf("chart: %v", c)
	}
}

func TestChartSeries(t *testing.T) {
	e := newEnv(t, "")
	s := func(v string) *string { return &v }
	r := e.ok("POST", "/api/chart", map[string]any{"Stmt": "-- barchart x: k, y: a, b", "Columns": []string{"k", "a", "b"},
		"Cells": [][]*string{{s("x"), s("1"), s("2")}, {s("y"), s("3"), s("4")}}})
	series := r["series"].([]any)
	if len(series) != 2 || series[1].(map[string]any)["name"] != "b" {
		t.Fatalf("series = %v", r)
	}
}

func TestParamFormatOverrideAndCurrentStatement(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	text := "select :a; select ${b} from t"
	r := e.ok("POST", "/api/params", map[string]any{"text": text, "dialect": "postgres", "paramFormat": "brace"})
	if p := r["params"].([]any); len(p) != 1 || p[0] != "b" {
		t.Fatalf("brace params = %v", p)
	}
	r = e.ok("POST", "/api/params", map[string]any{"text": text, "dialect": "postgres", "paramFormat": "colon", "current": true, "offset": 3})
	if p := r["params"].([]any); len(p) != 1 || p[0] != "a" {
		t.Fatalf("params of the first statement = %v", p)
	}
	code, out := e.call("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select ${b}", "paramFormat": "brace"}, "tok")
	if code != 422 || out["params"].([]any)[0] != "b" {
		t.Fatalf("exec with an overriding format: %d %v", code, out)
	}
	e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select ${b}", "paramFormat": "brace", "vars": map[string]string{"b": "1"}})
	if last := e.fake.execs[len(e.fake.execs)-1]; last != "select 1" {
		t.Fatalf("ran %q", last)
	}
}
