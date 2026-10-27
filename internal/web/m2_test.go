package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/config"
)

func TestExportModes(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	post := func(body map[string]any) (int, string) {
		code, out := e.call("POST", "/api/sessions/"+id+"/export", body, "tok")
		if code == 200 {
			return code, ""
		}
		return code, out["error"].(string)
	}
	if code, msg := post(map[string]any{"mode": "table", "ref": map[string]string{"Name": "t"}, "format": "csv", "columns": []string{"id"}}); code != 200 {
		t.Fatalf("table export: %d %s", code, msg)
	}
	if code, _ := post(map[string]any{"mode": "query", "text": "select 1", "format": "json"}); code != 200 {
		t.Fatalf("query export: %d", code)
	}
	if code, _ := post(map[string]any{"mode": "query", "text": "delete from t", "format": "csv"}); code != 400 {
		t.Fatalf("a write must not be re-run by an export: %d", code)
	}
	if code, _ := post(map[string]any{"mode": "page", "ref": map[string]string{"Name": "t"}, "format": "nope"}); code != 400 {
		t.Fatalf("unknown format: %d", code)
	}
}

// poll waits for a background job to finish.
func (e *env) poll(id, job string) map[string]any {
	e.t.Helper()
	for i := 0; i < 100; i++ {
		j := e.ok("GET", "/api/sessions/"+id+"/jobs/"+job, nil)
		if j["done"] == true {
			return j
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatal("job did not finish")
	return nil
}

func TestImportGatedAndRuns(t *testing.T) {
	e := newEnv(t, "alert1")
	id := e.open()
	body := map[string]any{"source": "csv", "header": true, "content": "id,name\n1,a\n2,b\n", "ref": map[string]string{"Name": "t"}, "mapping": map[string]string{"id": "id", "name": ""}}
	pv := e.ok("POST", "/api/sessions/"+id+"/import/preview", body)
	if len(pv["rows"].([]any)) != 2 || len(pv["columns"].([]any)) != 2 {
		t.Fatalf("preview: %v", pv)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/import", body, "tok"); code != 409 {
		t.Fatalf("an import must pass the safe-mode gate: %d", code)
	}
	body["confirmed"] = true
	j := e.poll(id, e.ok("POST", "/api/sessions/"+id+"/import", body)["job"].(string))
	if j["error"] != "" || j["rows"].(float64) != 2 || len(e.fake.applied) != 1 {
		t.Fatalf("import: %v applied=%d", j, len(e.fake.applied))
	}
}

func TestSearchJob(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	r := e.ok("POST", "/api/sessions/"+id+"/search", map[string]any{"Value": "x", "Op": "Contains"})
	j := e.poll(id, r["job"].(string))
	hits := j["hits"].([]any)
	if r["total"].(float64) != 2 || j["checked"].(float64) != 2 || len(hits) != 1 || hits[0].(map[string]any)["count"].(float64) != 2 {
		t.Fatalf("search: %v %v", r, j)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/search", map[string]any{"Value": "x", "Op": "Like"}, "tok"); code != 400 {
		t.Fatalf("unknown operator: %d", code)
	}
}

func TestProcessesKillGated(t *testing.T) {
	e := newEnv(t, "alert2")
	id := e.open()
	p := e.ok("GET", "/api/sessions/"+id+"/processes", nil)
	if p["supported"] != true || len(p["processes"].([]any)) != 1 {
		t.Fatalf("processes: %v", p)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/processes/kill", map[string]any{"ID": "7"}, "tok"); code != 409 || len(e.fake.killed) != 0 {
		t.Fatalf("an unconfirmed kill must be refused: %d", code)
	}
	e.ok("POST", "/api/sessions/"+id+"/processes/kill", map[string]any{"ID": "7", "Confirmed": true})
	if len(e.fake.killed) != 1 {
		t.Fatal("kill did not run")
	}
}

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

func TestLogOriginalValues(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) { c.General.LogOriginalValues = true })
	id := e.open()
	rows := e.ok("POST", "/api/sessions/"+id+"/rows", map[string]any{"ref": map[string]string{"Name": "t", "Kind": "table"}})
	p := e.ok("POST", "/api/sessions/"+id+"/plan", map[string]any{"resultId": rows["resultId"], "deletes": []int{0}})
	e.ok("POST", "/api/sessions/"+id+"/apply", map[string]any{"planId": p["planId"], "confirmed": true})
	h, _ := e.hist.List("c1", "", 0)
	if len(h) != 1 || !strings.Contains(h[0].Original, "delete") {
		t.Fatalf("history must carry the pre-image: %+v", h)
	}
}

func TestStartJobKeepsUndownloadedExport(t *testing.T) {
	e := newEnv(t, "")
	ss := e.s.sessions[e.open()]
	f := filepath.Join(t.TempDir(), "x")
	os.WriteFile(f, []byte("data"), 0o600)
	a := &job{file: f}
	idA := ss.startJob(a, func(context.Context) error { return nil })
	time.Sleep(50 * time.Millisecond)
	ss.startJob(&job{}, func(context.Context) error { return nil })
	if _, err := os.Stat(f); err != nil || ss.jobs[idA] == nil {
		t.Fatalf("a finished export must survive the next job: %v", err)
	}
}
