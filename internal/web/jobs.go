package web

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"webtuidb/internal/crashlog"
)

func (j *job) summary(id string) map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	lines := j.lines
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	progress := -1.0
	switch {
	case j.done && j.err == "":
		progress = 1
	case j.total > 0:
		progress = float64(j.checked) / float64(j.total)
	}
	file := ""
	if j.file != "" && j.err == "" { // backups keep only fileName; "file" promises a downloadable temp file
		file = j.fileName
	}
	m := map[string]any{
		"id": id, "kind": j.kind, "title": j.title, "done": j.done, "err": j.err, "progress": progress,
		"file": file, "created": j.created.Format(time.RFC3339), "lines": append([]string{}, lines...),
		"download": j.done && j.err == "" && j.file != "",
	}
	if j.path != "" && j.done && j.err == "" { // a finished backup: the file on the server, for POST /reveal
		m["path"] = j.path
	}
	return m
}

// jobs lists the session's jobs, newest first.
func (s *Server) jobs(ss *session, r *http.Request) (any, error) {
	ss.mu.Lock()
	type kv struct {
		id string
		j  *job
	}
	var l []kv
	for id, j := range ss.jobs {
		l = append(l, kv{id, j})
	}
	ss.mu.Unlock()
	sort.Slice(l, func(a, b int) bool { return l[a].j.created.After(l[b].j.created) })
	out := make([]map[string]any, 0, len(l))
	for _, e := range l {
		out = append(out, e.j.summary(e.id))
	}
	return map[string]any{"jobs": out}, nil
}

// jobCancel stops a running job; it stays listed (failed/cancelled) until dismissed with DELETE.
func (s *Server) jobCancel(ss *session, r *http.Request) (any, error) {
	j, err := s.job(ss, r)
	if err != nil {
		return nil, err
	}
	j.cancel()
	return nil, nil
}

// job is one background import or search; the browser polls it.
type job struct {
	mu             sync.Mutex
	cancel         context.CancelFunc
	done           bool
	err            string
	rows, batches  int64
	total, checked int
	hits           []hitJSON
	lines          []string     // backup / restore output
	paused         *atomic.Bool // search job: set by .../search/{job}/pause
	file, fileName string       // export job: the finished temp file and the name to download it as
	created        time.Time    // when the job started; kept exports are evicted oldest first
	kind, title    string       // import | export | backup | restore | search | diff, and the line the jobs centre shows
	path           string       // backup: the finished file on the server, for /reveal
}

const (
	maxRunningJobs = 4  // per session
	maxKeptExports = 3  // finished, not yet downloaded export files
	maxKeptJobs    = 30 // finished jobs without a file that the jobs centre can still show
)

// defaultJobDisk is the per-session byte budget for import spools and kept exports.
const defaultJobDisk int64 = 1 << 30

func (ss *session) diskBudget() int64 {
	if ss.jobDisk > 0 {
		return ss.jobDisk
	}
	return defaultJobDisk
}

// diskUsed is the bytes held by import spools in flight plus finished, not yet downloaded export files
// (caller holds ss.mu).
func (ss *session) diskUsed() int64 {
	used := ss.spooled
	for _, j := range ss.jobs {
		j.mu.Lock()
		if j.file != "" {
			if fi, err := os.Stat(j.file); err == nil {
				used += fi.Size()
			}
		}
		j.mu.Unlock()
	}
	return used
}

// evictOldest removes the oldest finished export file; false when there is none (caller holds ss.mu).
func (ss *session) evictOldest() bool {
	oldest := ""
	for id, j := range ss.jobs {
		j.mu.Lock()
		ok := j.done && j.file != ""
		t := j.created
		j.mu.Unlock()
		if ok && (oldest == "" || t.Before(ss.jobs[oldest].created)) {
			oldest = id
		}
	}
	if oldest == "" {
		return false
	}
	j := ss.jobs[oldest]
	j.mu.Lock()
	j.removeFile()
	j.mu.Unlock()
	delete(ss.jobs, oldest)
	return true
}

// reserve claims n bytes of the session's disk budget for an import spool (n = 0 only checks there is
// room). Kept exports are evicted oldest-first to make room; still over budget is a 507. The caller
// releases the n bytes with release.
func (ss *session) reserve(n int64) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	budget := ss.diskBudget()
	for ss.diskUsed()+n > budget || (n == 0 && ss.diskUsed() >= budget) {
		if !ss.evictOldest() {
			return &apiErr{Code: http.StatusInsufficientStorage, Msg: fmt.Sprintf("this session's temporary files are over their %d MB budget; download or delete finished exports and retry", budget>>20)}
		}
	}
	ss.spooled += n
	return nil
}

func (ss *session) release(n int64) {
	ss.mu.Lock()
	ss.spooled -= n
	ss.mu.Unlock()
}

// jobSlot refuses a new background job while the session already runs maxRunningJobs, and deletes the
// oldest un-downloaded export files so they cannot fill the temp disk.
func (ss *session) jobSlot() error {
	ss.mu.Lock()
	running, kept := 0, 0
	for _, j := range ss.jobs {
		j.mu.Lock()
		switch {
		case !j.done:
			running++
		case j.file != "":
			kept++
		}
		j.mu.Unlock()
	}
	if running >= maxRunningJobs {
		ss.mu.Unlock()
		return &apiErr{Code: http.StatusTooManyRequests, Msg: fmt.Sprintf("%d background jobs are already running on this session; wait or stop one", maxRunningJobs)}
	}
	for ; kept >= maxKeptExports && ss.evictOldest(); kept-- {
	}
	ss.mu.Unlock()
	return nil
}

// startJob registers a job and runs fn on a context that ends with the session or the job's cancel.
func (ss *session) startJob(j *job, fn func(ctx context.Context) error) string {
	id := randID(6)
	ctx, cancel := context.WithCancel(context.Background())
	j.cancel = cancel
	j.created = time.Now()
	ss.mu.Lock()
	// Finished jobs stay for the jobs centre until dismissed; past maxKeptJobs the oldest go. An export file
	// stays until it is downloaded or the session closes.
	var fin []string
	for k, o := range ss.jobs {
		o.mu.Lock()
		if o.done && o.file == "" {
			fin = append(fin, k)
		}
		o.mu.Unlock()
	}
	if len(fin) >= maxKeptJobs {
		sort.Slice(fin, func(a, b int) bool { return ss.jobs[fin[a]].created.Before(ss.jobs[fin[b]].created) })
		for _, k := range fin[:len(fin)-maxKeptJobs+1] {
			delete(ss.jobs, k)
		}
	}
	ss.jobs[id] = j
	ss.mu.Unlock()
	go func() {
		select {
		case <-ss.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	go func() {
		var err error
		func() {
			defer crashlog.Catch(ss.srv.con.tail, func(p any) {
				err = fmt.Errorf("internal error: %v (see %s)", p, crashlog.Path())
			})
			err = fn(ctx)
		}()
		j.mu.Lock()
		j.done = true
		if err != nil {
			j.err = err.Error()
		}
		j.mu.Unlock()
		cancel()
	}()
	return id
}

func (s *Server) jobGet(ss *session, r *http.Request) (any, error) {
	ss.mu.Lock()
	j := ss.jobs[r.PathValue("jid")]
	ss.mu.Unlock()
	if j == nil {
		return nil, &apiErr{Code: http.StatusNotFound, Msg: "no such job"}
	}
	return j.snapshot(), nil
}

func (s *Server) jobStop(ss *session, r *http.Request) (any, error) {
	ss.mu.Lock()
	j := ss.jobs[r.PathValue("jid")]
	ss.mu.Unlock()
	if j == nil {
		return nil, nil
	}
	j.mu.Lock()
	done := j.done
	j.mu.Unlock()
	if !done { // running: stop it (the job stays listed as failed/cancelled)
		j.cancel()
		return nil, nil
	}
	// finished: dismiss it from the jobs centre, and the kept export file with it
	ss.mu.Lock()
	delete(ss.jobs, r.PathValue("jid"))
	ss.mu.Unlock()
	j.mu.Lock()
	j.removeFile()
	j.file = ""
	j.mu.Unlock()
	return nil, nil
}
