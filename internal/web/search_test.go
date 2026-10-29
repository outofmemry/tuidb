package web

import (
	"sync/atomic"
	"testing"
	"time"
)

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

func TestSearchJobEvents(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	j := e.ok("POST", "/api/sessions/"+id+"/search", map[string]any{"Op": "Contains", "Value": "a"})["job"].(string)
	es := events(t, e.raw("GET", "/api/sessions/"+id+"/jobs/"+j+"/events", nil, nil).Body)
	if es[0].Name != "progress" || es[len(es)-1].Name != "done" {
		t.Fatalf("search events: %s", names(es))
	}
	if code, _ := e.call("GET", "/api/sessions/"+id+"/jobs/nope/events", nil, "tok"); code != 404 {
		t.Fatalf("unknown job: %d", code)
	}
}

func TestWaitWhilePausedGivesUp(t *testing.T) {
	var p atomic.Bool
	p.Store(true)
	start := time.Now()
	if waitWhilePaused(t.Context(), &p, 120*time.Millisecond) || time.Since(start) < 100*time.Millisecond {
		t.Error("a pause longer than the cap must return false")
	}
	p.Store(false)
	if !waitWhilePaused(t.Context(), &p, time.Second) {
		t.Error("not paused returns true")
	}
}

func TestM9SearchAllSchemasPauseAndRename(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	p := "/api/sessions/" + id
	all := e.ok("POST", p+"/search", map[string]any{"value": "x", "op": "Contains", "all_schemas": true})
	if all["total"] != float64(4) {
		t.Fatalf("all_schemas total (public+other, no pg_catalog): %v", all)
	}
	j := e.ok("POST", p+"/search", map[string]any{"value": "x", "op": "Contains"})["job"].(string)
	e.ok("POST", p+"/search/"+j+"/pause", nil)
	e.ok("POST", p+"/search/"+j+"/resume", nil)
	if code, _ := e.call("POST", p+"/search/nope/pause", nil, "tok"); code != 404 {
		t.Fatalf("unknown search: %d", code)
	}
	e.ok("POST", "/api/queries", map[string]any{"name": "old", "sql": "select 1"})
	e.ok("POST", "/api/queries/rename", map[string]any{"from": "old.sql", "to": "new.sql"})
	if code, _ := e.call("POST", "/api/queries/rename", map[string]any{"from": "old.sql", "to": "x.sql"}, "tok"); code != 400 {
		t.Fatalf("renaming a missing query: %d", code)
	}
}
