package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"webtuidb/internal/crashlog"
	"webtuidb/internal/db"
	"webtuidb/internal/export"
	"webtuidb/internal/importer"
	"webtuidb/internal/sqlfmt"
)

// Milestone 2 data tooling: export, import, search, process list, chart, query parameters, SQL formatting.

// download makes api() answer with a file instead of JSON.
type download struct {
	name  string
	data  []byte
	ctype string // "" = application/octet-stream
}

type exportReq struct {
	Mode      string // "table" (the whole table, filters honoured) | "page" (one loaded page) | "query" (a read-only statement)
	Ref       db.ObjRef
	Filters   db.FilterSet
	Sort      []db.Sort
	Limit     int // page and query: like exec, 0 = the connection's row limit, <0 = none
	Offset    int
	Text      string
	Format    string
	Gzip      bool
	Structure bool
	Drop      bool
	Columns   []string
	Job       bool    // table mode: run in the background, see exportJob
	NullAs    *string // CSV: text written for NULL (default [csv] null_as)
	LineBreak *string // CSV: line break in the file (default [csv] line_break)
	FileName  string  // a [export] filename pattern ({table} {date} ...); "" = the configured one. JSON "filename" (any case)
	Header    *bool   // CSV: header row; nil = [export] header
	NullAsS   *string `json:"null_as"` // same as NullAs, the M9 spelling
}

// export writes the file into the response as it is produced (a whole table is paged through
// export.Table, never held in memory). Page and query exports are bounded by the row limit.
// With Job set a table export runs in the background into a temp file, with progress on the job
// (poll it or GET .../jobs/{jid}/events) and the finished file at GET .../jobs/{jid}/file.
func (s *Server) export(ss *session, r *http.Request) (any, error) {
	var req exportReq
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	switch req.Format {
	case "csv", "json", "sql", "xlsx":
	default:
		return nil, bad("unknown export format %q", req.Format)
	}
	sess := ss.o.Sess
	o := export.Options{
		Format: export.Format(req.Format), CSV: s.o.Cfg.CSV, Columns: req.Columns, Gzip: req.Gzip && req.Format != "xlsx",
		Table: req.Ref.Name, Dialect: sess.Caps().Dialect, Structure: req.Structure && req.Format == "sql", Drop: req.Drop && req.Format == "sql",
	}
	if o.Table == "" {
		o.Table = "result"
	}
	if req.NullAsS != nil {
		req.NullAs = req.NullAsS
	}
	if req.NullAs != nil {
		o.CSV.NullAs = *req.NullAs
	}
	o.NoHeader = !s.o.Cfg.Export.Header
	if req.Header != nil {
		o.NoHeader = !*req.Header
	}
	if req.LineBreak != nil {
		o.CSV.LineBreak = *req.LineBreak
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	if o.Structure {
		if req.Ref.Name == "" {
			return nil, bad("the structure needs a table")
		}
		ddl, err := sess.DDL(ctx, req.Ref)
		if err != nil {
			return nil, bad("%v", err)
		}
		o.DDL = ddl
	}
	var write func(io.Writer) error
	var err error
	switch req.Mode {
	case "table":
		if req.Ref.Name == "" {
			return nil, bad("ref.name is required")
		}
		if req.Job {
			return s.exportJob(ss, req, o)
		}
		write = func(w io.Writer) error { return export.Table(ctx, sess, req.Ref, req.Filters, o, w, nil) }
	case "page":
		if req.Ref.Name == "" {
			return nil, bad("ref.name is required")
		}
		limit := ss.clampLimit(req.Limit)
		var res *db.Result
		if res, err = sess.Rows(ctx, db.RowQuery{ObjRef: req.Ref, Filters: req.Filters, Sort: req.Sort, Limit: limit, Offset: req.Offset}); err == nil {
			write = func(w io.Writer) error { return export.Write(w, res, o) }
		}
	case "query":
		dialect := sess.Caps().Dialect
		st := db.Split(dialect, req.Text)
		// running it again must not change anything: only read-only statements are exported
		if len(st) != 1 || !db.IsReadOnly(dialect, st[0].Text) {
			return nil, bad("only a single read-only statement can be exported")
		}
		limit := ss.clampLimit(req.Limit)
		var res *db.Result
		if res, err = sess.Exec(ctx, st[0].Text, limit); err == nil {
			if !res.HasRows {
				return nil, bad("the statement returned no rows")
			}
			write = func(w io.Writer) error { return export.Write(w, res, o) }
		}
	default:
		return nil, bad("unknown export mode %q", req.Mode)
	}
	if err != nil {
		return nil, bad("%v", err)
	}
	name := req.fileName(ss, o)
	if err := sendFile(rw(r), name, "application/octet-stream", write); err != nil {
		return nil, bad("%v", err)
	}
	return streamed{}, nil
}

// fileName is the name the file is offered as: the one typed, else the [export] filename pattern.
func (req exportReq) fileName(ss *session, o export.Options) string {
	return ss.exportName(strings.TrimSpace(req.FileName), o.Table, export.Ext(o))
}

func (s *Server) exportJob(ss *session, req exportReq, o export.Options) (any, error) {
	if err := ss.jobSlot(); err != nil {
		return nil, err
	}
	if err := ss.reserve(0); err != nil { // an export's size is unknown until it ends: refuse when already full
		return nil, err
	}
	f, err := os.CreateTemp("", "webtuidb-export-*")
	if err != nil {
		return nil, err
	}
	j := &job{kind: "export", title: "Export " + req.Ref.Name, file: f.Name(), fileName: req.fileName(ss, o)}
	id := ss.startJob(j, func(ctx context.Context) error {
		err := export.Table(ctx, ss.o.Sess, req.Ref, req.Filters, o, f, func(n int64) {
			j.mu.Lock()
			j.rows = n
			j.mu.Unlock()
		})
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(f.Name())
			j.mu.Lock()
			j.file = ""
			j.mu.Unlock()
		}
		return err
	})
	return map[string]any{"job": id}, nil
}

// ---- background jobs (import, search) -------------------------------------------------

type hitJSON struct {
	Ref     db.ObjRef    `json:"ref"`
	Count   int64        `json:"count"`
	Filters db.FilterSet `json:"filters"`
	Error   string       `json:"error,omitempty"`
	Schema  string       `json:"schema"`
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

func (j *job) removeFile() {
	if j.file != "" {
		os.Remove(j.file)
	}
}

// snapFile is the download name, only for a job that has a temp file to serve.
func snapFile(j *job) string {
	if j.file == "" {
		return ""
	}
	return j.fileName
}

func (j *job) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	hits := j.hits
	if hits == nil {
		hits = []hitJSON{}
	}
	return map[string]any{"done": j.done, "error": j.err, "rows": j.rows, "batches": j.batches, "total": j.total, "checked": j.checked, "hits": hits, "lines": append([]string{}, j.lines...), "paused": j.paused != nil && j.paused.Load(), "kind": j.kind, "title": j.title, "file": snapFile(j), "download": j.done && j.err == "" && j.file != ""}
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

const maxUpload int64 = 512 << 20 // a bigger spool fills the temp disk

// ---- import -----------------------------------------------------------------------------

type importReq struct {
	Source           string
	Content          string // JSON body only; a multipart upload sends the file as its own part
	Header           bool
	Delimiter        string
	CreateTable      bool
	Ref              db.ObjRef
	Mapping          map[string]string
	Confirmed        bool
	Password         string
	Encoding         string            // "" | utf-8 | latin1 | utf-16le | ... (IANA names)
	Quote            string            // CSV quote character (one rune)
	OnConflict       string            `json:"on_conflict"`  // "" | stop | update | replace
	ColumnTypes      map[string]string `json:"column_types"` // created columns: column -> type
	NullAs           string            `json:"null_as"`      // cell text that becomes NULL
	EmptyIsNull      bool              `json:"empty_is_null"`
	KeepFormulaQuote bool              `json:"keep_formula_quote"` // keep the apostrophe before = + - @
}

// importInput reads the request: a JSON body (Content inline) or multipart/form-data with an
// `options` part (the same JSON, without Content) followed by the `file` part. The options part
// must come first, because the file is consumed as it arrives. With spool the file goes to a
// temp file (the import runs after the request ends); without it the reader is the part itself.
// pre (may be nil) vets the options before any of the file is read, so a refused import stores nothing.
func importInput(r *http.Request, spool bool, charge func(n int64) (release func(), err error), pre func(importReq) error) (req importReq, in io.Reader, cleanup func(), err error) {
	cleanup = func() {}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "multipart/form-data" {
		if err = decode(r, &req); err != nil {
			return
		}
		if pre != nil {
			if err = pre(req); err != nil {
				return
			}
		}
		return req, strings.NewReader(req.Content), cleanup, nil
	}
	r.Body = http.MaxBytesReader(rw(r), r.Body, maxUpload)
	mr, err := r.MultipartReader()
	if err != nil {
		return req, nil, cleanup, bad("%v", err)
	}
	for {
		p, err := mr.NextPart()
		if err != nil {
			return req, nil, cleanup, bad("the upload needs an `options` part and then a `file` part")
		}
		switch p.FormName() {
		case "options":
			if err := json.NewDecoder(io.LimitReader(p, 1<<20)).Decode(&req); err != nil {
				return req, nil, cleanup, bad("invalid options JSON: %v", err)
			}
		case "file":
			if req.Source == "" {
				return req, nil, cleanup, bad("send the `options` part before the `file` part")
			}
			if pre != nil {
				if err := pre(req); err != nil {
					return req, nil, cleanup, err
				}
			}
			if !spool {
				return req, p, cleanup, nil
			}
			release := func() {}
			if charge != nil {
				n := maxUpload
				if r.ContentLength > 0 && r.ContentLength < n {
					n = r.ContentLength
				}
				if release, err = charge(n); err != nil {
					return req, nil, cleanup, err
				}
			}
			f, err := os.CreateTemp("", "webtuidb-import-*")
			if err != nil {
				release()
				return req, nil, cleanup, err
			}
			cleanup = func() { f.Close(); os.Remove(f.Name()); release() }
			if _, err := io.Copy(f, p); err != nil {
				cleanup()
				return req, nil, func() {}, bad("upload failed: %v", err)
			}
			f.Seek(0, io.SeekStart)
			return req, f, cleanup, nil
		}
	}
}

func (s *Server) importOpts(req importReq) (importer.Options, error) {
	switch req.Source {
	case "csv", "json", "sql":
	default:
		return importer.Options{}, bad("unknown import source %q", req.Source)
	}
	o := importer.Options{Source: importer.Source(req.Source), CSV: s.o.Cfg.CSV, Header: req.Header, Mapping: req.Mapping, CreateTable: req.CreateTable, Table: req.Ref}
	if req.Delimiter != "" {
		o.CSV.Delimiter = req.Delimiter
	}
	return o, nil
}

func (s *Server) importPreview(ss *session, r *http.Request) (any, error) {
	req, in, cleanup, err := importInput(r, false, nil, nil)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	o, err := s.importOpts(req)
	if err != nil {
		return nil, err
	}
	cols, rows, err := importer.Preview(in, o, 100)
	if err != nil {
		return nil, bad("%v", err)
	}
	if cols == nil {
		cols = []string{}
	}
	if rows == nil {
		rows = [][]string{}
	}
	return map[string]any{"columns": cols, "rows": rows}, nil
}

func (s *Server) importStart(ss *session, r *http.Request) (any, error) {
	var o importer.Options
	req, in, cleanup, err := importInput(r, true, func(n int64) (func(), error) {
		if err := ss.reserve(n); err != nil {
			return nil, err
		}
		return func() { ss.release(n) }, nil
	}, func(req importReq) (err error) {
		if o, err = s.importOpts(req); err != nil {
			return err
		}
		if o.Source != "sql" && req.Ref.Name == "" {
			return bad("ref.name is required")
		}
		if err := ss.jobSlot(); err != nil {
			return err
		}
		if err := ss.readOnlyViolation([]db.Statement{{Text: "INSERT"}}); err != nil {
			return err
		}
		// an import writes, so it passes the same safe-mode gate as any other write
		return ss.check(ss.gate([]db.Statement{{Text: "INSERT"}}), req.Confirmed, req.Password)
	})
	if err != nil {
		cleanup()
		return nil, err
	}
	j := &job{}
	start := time.Now()
	id := ss.startJob(j, func(ctx context.Context) error {
		defer cleanup()
		err := importer.Run(ctx, ss.o.Sess, in, o, func(p importer.Progress) {
			j.mu.Lock()
			j.rows, j.batches = p.Rows, p.Batches
			j.mu.Unlock()
		})
		ss.clearColCache() // CREATE TABLE or a dump may have changed the structure
		if err == nil {
			ss.record("import "+req.Source+" into "+req.Ref.Name, "import", time.Since(start), "")
		}
		return err
	})
	return map[string]any{"job": id}, nil
}

// ---- search in database --------------------------------------------------------------------

func (s *Server) searchStart(ss *session, r *http.Request) (any, error) {
	var req db.SearchOptions
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	switch req.Op {
	case "Contains", "Not contains", "=", "Has prefix", "Has suffix":
	default:
		return nil, bad("unknown operator %q", req.Op)
	}
	if req.Value == "" {
		return nil, bad("a value is required")
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	schema := req.Schema
	if schema == "" {
		schema = ss.o.Sess.CurrentSchema()
	}
	objs, err := ss.o.Sess.Objects(ctx, schema)
	if err != nil {
		return nil, bad("%v", err)
	}
	only := map[string]bool{}
	for _, t := range req.Tables {
		only[t] = true
	}
	if err := ss.jobSlot(); err != nil {
		return nil, err
	}
	j := &job{}
	for _, ob := range objs {
		if ob.Kind == db.KindTable && (len(only) == 0 || only[ob.Name]) {
			j.total++
		}
	}
	id := ss.startJob(j, func(ctx context.Context) error {
		return db.Search(ctx, ss.o.Sess, req, func(h db.SearchHit) {
			j.mu.Lock()
			defer j.mu.Unlock()
			j.checked++
			if h.Count > 0 || h.Err != nil {
				x := hitJSON{Ref: h.Ref, Count: h.Count, Filters: h.Filters}
				if h.Err != nil {
					x.Error = h.Err.Error()
				}
				j.hits = append(j.hits, x)
			}
		})
	})
	return map[string]any{"job": id, "total": j.total}, nil
}

// ---- process list --------------------------------------------------------------------------

// processLister is the process list of the session's side connection (opened on first use, closed with the
// session); if a second connection cannot be made the workspace's own is used.
func (ss *session) processLister() (db.ProcessLister, bool) {
	if _, ok := ss.o.Sess.(db.ProcessLister); !ok || !ss.o.Sess.Caps().ProcessList {
		return nil, false
	}
	ss.sideMu.Lock()
	defer ss.sideMu.Unlock()
	if ss.sideO == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if o, err := ss.srv.o.Connect(ctx, ss.conn); err == nil {
			ss.sideO = o
		}
	}
	if ss.sideO != nil {
		if pl, ok := ss.sideO.Sess.(db.ProcessLister); ok {
			return pl, true
		}
	}
	return ss.o.Sess.(db.ProcessLister), true
}

func (s *Server) processes(ss *session, r *http.Request) (any, error) {
	pl, ok := ss.processLister()
	if !ok {
		return map[string]any{"supported": false, "driver": ss.conn.Driver, "processes": []any{}}, nil
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	l, err := pl.Processes(ctx)
	if err != nil {
		return nil, bad("%v", err)
	}
	out := make([]map[string]any, len(l))
	for i, p := range l {
		out[i] = map[string]any{"id": p.ID, "user": p.User, "db": p.DB, "state": p.State, "elapsedMs": p.Elapsed.Milliseconds(), "query": p.Query}
	}
	return map[string]any{"supported": true, "processes": out}, nil
}

func (s *Server) killProcess(ss *session, r *http.Request) (any, error) {
	var req struct {
		ID        string
		Confirmed bool
		Password  string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	pl, ok := ss.processLister()
	if !ok {
		return nil, bad("not supported by %s", ss.conn.Driver)
	}
	// killing is never read-only, so safe mode gates it like a write
	if err := ss.check(ss.gate([]db.Statement{{Text: "KILL " + req.ID}}), req.Confirmed, req.Password); err != nil {
		return nil, err
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	if err := pl.Kill(ctx, req.ID); err != nil {
		return nil, bad("%v", err)
	}
	ss.record("kill process "+req.ID, "gui", 0, "")
	return nil, nil
}

// ---- editor helpers --------------------------------------------------------------------------

func (s *Server) format(r *http.Request) (any, error) {
	var req struct{ Text, Mode, Dialect, IndentType string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	switch req.Mode {
	case "beautify":
		e := s.o.Cfg.Editor
		if req.IndentType == "" {
			req.IndentType = e.IndentType
		}
		return map[string]string{"text": sqlfmt.Beautify(req.Text, sqlfmt.Options{Indent: e.Indent, Width: e.IndentWidth, Uppercase: e.UppercaseKeywords, Dialect: req.Dialect, IndentType: req.IndentType})}, nil
	case "uglify":
		return map[string]string{"text": sqlfmt.Uglify(req.Text, req.Dialect)}, nil
	}
	return nil, bad("mode must be beautify or uglify")
}

// paramNames lists the distinct parameter names across statements, in order of appearance.
func paramNames(stmts []db.Stmt, pf db.ParamFormat, dialect string) []string {
	seen := map[string]bool{}
	names := []string{}
	for _, st := range stmts {
		for _, n := range db.Params(st.Text, pf, dialect) {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	return names
}

func (s *Server) params(r *http.Request) (any, error) {
	var req struct {
		Text, Dialect, ParamFormat string
		Current                    bool // only the statement at Offset (a byte offset), like /exec
		Offset                     int
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	pf := db.ParamFormat(s.o.Cfg.Editor.ParamFormat)
	if req.ParamFormat != "" {
		pf = db.ParamFormat(req.ParamFormat)
	}
	stmts := []db.Stmt{{Text: req.Text}}
	if req.Current {
		st, ok := db.StatementAt(db.Split(req.Dialect, req.Text), req.Offset)
		if !ok {
			return nil, bad("no statement at the cursor")
		}
		stmts = []db.Stmt{st}
	}
	return map[string]any{"params": paramNames(stmts, pf, req.Dialect)}, nil
}

// chart draws a result the browser already holds: cells arrive as the strings the grid shows.
func (s *Server) chart(r *http.Request) (any, error) {
	var req struct {
		Stmt, Kind string
		Columns    []string
		Cells      [][]*string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	res := &db.Result{}
	for _, c := range req.Columns {
		res.Columns = append(res.Columns, db.ResultColumn{Name: c})
	}
	for _, row := range req.Cells {
		if len(row) != len(req.Columns) {
			return nil, bad("every row needs %d cells", len(req.Columns))
		}
		out := make([]any, len(row))
		for i, c := range row {
			if c != nil {
				out[i] = *c
			}
		}
		res.Rows = append(res.Rows, out)
	}
	spec, _ := db.ParseChart(req.Stmt) // no "-- barchart" comment: infer the columns
	if spec.Kind == "" {
		spec.Kind = "bar"
	}
	if req.Kind != "" {
		spec.Kind = req.Kind
	}
	ser, err := db.Chart(res, spec)
	if err != nil {
		return nil, bad("%v", err)
	}
	if ser.Labels == nil {
		ser.Labels, ser.Values = []string{}, []float64{}
	}
	return map[string]any{"kind": spec.Kind, "labels": ser.Labels, "values": ser.Values, "series": jsonSeries(ser.Series)}, nil
}
