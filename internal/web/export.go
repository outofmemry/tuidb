package web

import (
	"context"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"webtuidb/internal/db"
	"webtuidb/internal/export"
)

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
	NullAsS   *string `json:"null_as"` // same as NullAs, the older snake_case spelling
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

func mimeAttachment(name string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": name})
}

// exportName expands a file name pattern (default [export] filename; {connection} {database} {table} {tag}
// {date} {time}) with export.Filename and adds the extension unless the pattern already ends with it.
func (ss *session) exportName(pattern, table, ext string) string {
	if pattern == "" {
		pattern = ss.srv.o.Cfg.Export.Filename
	}
	if pattern == "" {
		pattern = "{table}-{date}"
	}
	name := export.Filename(pattern, map[string]string{"connection": ss.conn.Name, "database": ss.o.Sess.CurrentDatabase(), "table": table, "tag": ss.conn.Tag})
	if strings.HasSuffix(strings.ToLower(name), "."+strings.ToLower(ext)) {
		return name
	}
	return name + "." + ext
}
