package web

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// R-008: a panic in a handler is a 500 for that request, not a dead server, and a panicking job ends as failed.
func TestHandlerAndJobPanicsAreContained(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "")
	req, _ := http.NewRequest("GET", "/x", nil)
	if _, err := e.s.call(func(*http.Request) (any, error) { panic("boom") }, req); err == nil || !strings.Contains(err.Error(), "internal error") {
		t.Fatalf("handler panic: %v", err)
	}
	id := e.open()
	ss := e.s.sessions[id]
	j := &job{kind: "x", title: "x"}
	jid := ss.startJob(j, func(context.Context) error { panic("job boom") })
	if r := waitJob(t, e, id, jid); r["error"] == nil || r["error"] == "" || !strings.Contains(r["error"].(string), "internal error") {
		t.Fatalf("job = %v", r)
	}
}

func TestJobsListCancelDismiss(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	ss := e.s.sessions[id]
	j := &job{kind: "import", title: "Import into t"}
	jid := ss.startJob(j, func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	l := e.ok("GET", "/api/sessions/"+id+"/jobs", nil)["jobs"].([]any)
	if len(l) != 1 {
		t.Fatalf("jobs = %v", l)
	}
	row := l[0].(map[string]any)
	if row["id"] != jid || row["kind"] != "import" || row["title"] != "Import into t" || row["done"] != false || row["progress"].(float64) != -1 {
		t.Fatalf("row = %v", row)
	}
	e.ok("POST", "/api/sessions/"+id+"/jobs/"+jid+"/cancel", nil)
	waitJob(t, e, id, jid)
	row = e.ok("GET", "/api/sessions/"+id+"/jobs", nil)["jobs"].([]any)[0].(map[string]any)
	if row["done"] != true || row["err"] == "" {
		t.Fatalf("a cancelled job is listed as failed: %v", row)
	}
	e.ok("DELETE", "/api/sessions/"+id+"/jobs/"+jid, nil) // finished: dismissed
	if l := e.ok("GET", "/api/sessions/"+id+"/jobs", nil)["jobs"].([]any); len(l) != 0 {
		t.Fatalf("dismissed job still listed: %v", l)
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
