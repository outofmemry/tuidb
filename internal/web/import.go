package web

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"
	"webtuidb/internal/db"
	"webtuidb/internal/importer"
)

const maxUpload int64 = 512 << 20

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
	o.Encoding, o.OnConflict, o.ColumnTypes = req.Encoding, req.OnConflict, req.ColumnTypes
	o.NullAs, o.EmptyIsNull, o.KeepFormulaQuote = req.NullAs, req.EmptyIsNull, req.KeepFormulaQuote
	for col, typ := range req.ColumnTypes {
		if !validColumnType(typ) {
			return importer.Options{}, bad("invalid type %q for column %q", typ, col)
		}
	}
	switch req.OnConflict {
	case "", "stop", "update", "replace":
	default:
		return importer.Options{}, bad("unknown on_conflict %q (stop, update or replace)", req.OnConflict)
	}
	if rs := []rune(req.Quote); len(rs) == 1 {
		o.Quote = rs[0]
	} else if len(rs) > 1 {
		return importer.Options{}, bad("quote must be one character")
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
	cols, rows, err := importer.PreviewContext(r.Context(), in, o, 100) // IO-7: a closed request stops the read
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

// importGate is the statement kinds an import will run, for the read-only and safe-mode checks.
func importGate(req importReq) []db.Statement {
	stmts := []db.Statement{{Text: "INSERT"}}
	if req.OnConflict == "update" || req.OnConflict == "replace" {
		stmts = append(stmts, db.Statement{Text: "UPDATE"})
	}
	if req.CreateTable {
		stmts = append(stmts, db.Statement{Text: "CREATE TABLE"})
	}
	return stmts
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
		o.Driver = ss.conn.Driver
		if o.Source != "sql" && req.Ref.Name == "" {
			return bad("ref.name is required")
		}
		if err := ss.jobSlot(); err != nil {
			return err
		}
		stmts := importGate(req)
		if err := ss.readOnlyViolation(stmts); err != nil {
			return err
		}
		// an import writes, so it passes the same safe-mode gate as any other write
		return ss.check(ss.gate(stmts), req.Confirmed, req.Password)
	})
	if err != nil {
		cleanup()
		return nil, err
	}
	j := &job{kind: "import", title: "Import into " + req.Ref.Name}
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
