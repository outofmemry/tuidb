package web

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestConsoleAndTabsAndHistoryFiles(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select 7"})
	var con []map[string]any
	json.NewDecoder(e.raw("GET", "/api/console", nil, nil).Body).Decode(&con)
	if len(con) != 1 || con[0]["text"] != "select 7" || con[0]["time"] == "" {
		t.Fatalf("console: %v", con)
	}
	e.ok("DELETE", "/api/console", nil)
	for i := 0; i < consoleCap+5; i++ {
		e.s.con.add("c1", "x")
	}
	if len(e.s.con.lines) != consoleCap {
		t.Fatalf("cap: %d", len(e.s.con.lines))
	}

	if code, _ := e.call("PUT", "/api/state/tabs?conn=c1", []map[string]string{{"Kind": "sql", "Text": "select 1"}, {"Kind": "table", "Name": "public.t"}}, "tok"); code != 200 {
		t.Fatalf("put tabs: %d", code)
	}
	var tabs []map[string]any
	json.NewDecoder(e.raw("GET", "/api/state/tabs?conn=c1", nil, nil).Body).Decode(&tabs)
	if len(tabs) != 2 || tabs[1]["name"] != "public.t" {
		t.Fatalf("tabs: %v", tabs)
	}
	if code, _ := e.call("GET", "/api/state/tabs", nil, "tok"); code != 400 {
		t.Fatalf("conn required: %d", code)
	}

	// history: export csv, clear, import it back
	e.ok("DELETE", "/api/history?conn=c1&all=1", nil)
	e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select 'a,b'"})
	csv, _ := io.ReadAll(e.raw("GET", "/api/history/export?conn=c1&format=csv", nil, nil).Body)
	if !strings.HasPrefix(string(csv), "time,database,sql,duration_ms,source") || !strings.Contains(string(csv), `"select 'a,b'"`) {
		t.Fatalf("history csv: %q", csv)
	}
	js, _ := io.ReadAll(e.raw("GET", "/api/history/export?conn=c1", nil, nil).Body)
	e.ok("DELETE", "/api/history?conn=c1&all=1", nil)
	if out := e.ok("POST", "/api/history/import", map[string]any{"conn": "c1", "format": "csv", "content": string(csv)}); out["imported"] != float64(1) {
		t.Fatalf("import csv: %v", out)
	}
	if out := e.ok("POST", "/api/history/import", map[string]any{"conn": "c1", "content": string(js)}); out["imported"] != float64(1) {
		t.Fatalf("import json: %v", out)
	}
	if l, _ := e.hist.List("c1", "", 0); len(l) != 2 || l[0].SQL != "select 'a,b'" {
		t.Fatalf("history: %v", l)
	}

	// saved queries
	e.ok("POST", "/api/queries/import-file", map[string]any{"name": "report.sql", "sql": "select 1"})
	qs, _ := io.ReadAll(e.raw("GET", "/api/queries/export", nil, nil).Body)
	if !strings.Contains(string(qs), "report") || !strings.Contains(string(qs), "select 1") {
		t.Fatalf("queries: %s", qs)
	}
	if code, _ := e.call("POST", "/api/queries/import-file", map[string]any{"name": "x"}, "tok"); code != 400 {
		t.Fatalf("empty sql: %d", code)
	}
}

func TestConsoleReplacesBigStatements(t *testing.T) {
	var c consoleLog
	c.add("c1", strings.Repeat("x", 70<<10))
	if c.lines[0].Text != "<70 KB statement>" {
		t.Fatalf("got %q", c.lines[0].Text)
	}
}
