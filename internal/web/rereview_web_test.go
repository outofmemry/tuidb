package web

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestQueryGetAndListEmitETag(t *testing.T) {
	e := newEnv(t, "")
	e.ok("POST", "/api/queries", map[string]any{"folder": "r", "name": "daily", "sql": "select 1"})
	p := "/api/queries/r/daily.sql"
	resp := e.raw("GET", p, nil, nil)
	tag := resp.Header.Get("ETag")
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if tag == "" || body["sql"] != "select 1" || body["name"] != "daily" {
		t.Fatalf("GET: etag=%q body=%v", tag, body)
	}
	lr := e.raw("GET", "/api/queries", nil, nil)
	var l []map[string]any
	json.NewDecoder(lr.Body).Decode(&l)
	if len(l) != 1 || l[0]["etag"] != tag {
		t.Fatalf("list etag %v want %s", l, tag)
	}
	ok := e.raw("PUT", p, strings.NewReader(`{"sql":"select 2"}`), map[string]string{"If-Match": tag})
	if ok.StatusCode != 200 {
		t.Fatalf("PUT with fresh etag: %d", ok.StatusCode)
	}
	stale := e.raw("PUT", p, strings.NewReader(`{"sql":"select 3"}`), map[string]string{"If-Match": tag})
	if stale.StatusCode != 409 {
		t.Fatalf("PUT with stale etag: %d", stale.StatusCode)
	}
}

func TestBoardSaveYClearsComment(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e := newEnv(t, "")
	e.ok("POST", "/api/boards", map[string]any{"Name": "yb", "Query": "-- barchart x: a, y: b, c\nselect 1 as a, 2 as b, 3 as c, 4 as d", "Y": "d"})
	b, err := findBoard("yb")
	if err != nil || len(b.Panels) != 1 {
		t.Fatalf("%v %v", b, err)
	}
	if c := b.Panels[0].Chart; !strings.Contains(c, "d") || strings.Contains(c, "b") || strings.Contains(c, "c,") {
		t.Fatalf("chart spec kept the comment's series: %q", c)
	}
}

func TestJobDiskBudget(t *testing.T) {
	ss := &session{jobs: map[string]*job{}, jobDisk: 100}
	mk := func(id string, n int, age time.Duration) string {
		f, _ := os.CreateTemp(t.TempDir(), "x")
		f.Write(make([]byte, n))
		f.Close()
		ss.jobs[id] = &job{done: true, file: f.Name(), created: time.Now().Add(-age)}
		return f.Name()
	}
	old := mk("old", 60, time.Hour)
	_ = mk("new", 30, time.Minute)
	if u := ss.diskUsed(); u != 90 {
		t.Fatalf("used %d", u)
	}
	if err := ss.reserve(50); err != nil { // evicts the oldest export only
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err == nil || ss.jobs["old"] != nil || ss.jobs["new"] == nil {
		t.Fatal("the oldest export should be evicted first")
	}
	err := ss.reserve(500) // nothing left to evict, still over budget
	if ae, ok := err.(*apiErr); !ok || ae.Code != 507 {
		t.Fatalf("want 507, got %v", err)
	}
	ss.release(50)
	if ss.spooled != 0 {
		t.Fatalf("spooled %d", ss.spooled)
	}
	_ = io.Discard
}
