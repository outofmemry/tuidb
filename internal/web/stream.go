package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// lazyWriter sends headers on the first byte, so an error before any output can still be a JSON
// error, and flushes after every write so the browser gets the file as it is produced.
type lazyWriter struct {
	w       http.ResponseWriter
	head    func()
	started bool
}

func (l *lazyWriter) start() {
	if !l.started {
		l.started = true
		l.head()
	}
}

func (l *lazyWriter) Write(p []byte) (int, error) {
	l.start()
	n, err := l.w.Write(p)
	if f, ok := l.w.(http.Flusher); ok {
		f.Flush()
	}
	return n, err
}

// sendFile streams write's output as an attachment. It returns the error only while nothing has
// been sent; after that the connection is aborted so the client sees a failed download, not a short file.
func sendFile(w http.ResponseWriter, name, ctype string, write func(io.Writer) error) error {
	lw := &lazyWriter{w: w, head: func() {
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Content-Disposition", mimeAttachment(name))
		w.Header().Set("Cache-Control", "no-store")
	}}
	if err := write(lw); err != nil {
		if !lw.started {
			return err
		}
		panic(http.ErrAbortHandler)
	}
	lw.start()
	return nil
}

type sse struct {
	w http.ResponseWriter
	f http.Flusher
}

// newSSE must be called before anything else is written; errors after this point travel as events.
func newSSE(r *http.Request) (*sse, error) {
	w := rw(r)
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming is not supported by this connection")
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	f.Flush()
	return &sse{w, f}, nil
}

func (e *sse) send(event string, v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(e.w, "event: %s\ndata: %s\n\n", event, b)
	e.f.Flush()
}

func (s *Server) job(ss *session, r *http.Request) (*job, error) {
	ss.mu.Lock()
	j := ss.jobs[r.PathValue("jid")]
	ss.mu.Unlock()
	if j == nil {
		return nil, &apiErr{Code: http.StatusNotFound, Msg: "no such job"}
	}
	return j, nil
}

// jobEvents streams a job as `progress` (counters and hits, when they change), `line` (new
// backup / restore output) and a final `done` ({error}). The polling endpoint keeps working.
// shortcut: the job is sampled every 150 ms rather than signalled, upgrade with a cond var if that shows up in a profile.
func (s *Server) jobEvents(ss *session, r *http.Request) (any, error) {
	j, err := s.job(ss, r)
	if err != nil {
		return nil, err
	}
	ev, err := newSSE(r)
	if err != nil {
		return nil, err
	}
	t := time.NewTicker(150 * time.Millisecond)
	defer t.Stop()
	var last string
	sentLines := 0
	for {
		snap := j.snapshot()
		lines := snap["lines"].([]string)
		for ; sentLines < len(lines); sentLines++ {
			ev.send("line", map[string]any{"line": lines[sentLines]})
		}
		delete(snap, "lines")
		done, _ := snap["done"].(bool)
		errMsg, _ := snap["error"].(string)
		delete(snap, "done")
		delete(snap, "error")
		if b, _ := json.Marshal(snap); string(b) != last {
			last = string(b)
			ev.send("progress", snap)
		}
		if done {
			ev.send("done", map[string]any{"error": errMsg, "download": snap["download"]})
			return streamed{}, nil
		}
		select {
		case <-t.C:
		case <-r.Context().Done():
			return streamed{}, nil
		case <-ss.stop:
			return streamed{}, nil
		}
	}
}

// jobFile hands out a finished export job's file once and deletes it.
func (s *Server) jobFile(ss *session, r *http.Request) (any, error) {
	j, err := s.job(ss, r)
	if err != nil {
		return nil, err
	}
	j.mu.Lock()
	file, name, ready := j.file, j.fileName, j.done && j.err == ""
	j.mu.Unlock()
	if file == "" || !ready {
		return nil, &apiErr{Code: http.StatusNotFound, Msg: "this job has no file to download (not finished, failed, or already downloaded)"}
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, bad("%v", err)
	}
	defer func() {
		f.Close()
		os.Remove(file)
		j.mu.Lock()
		j.file = ""
		j.mu.Unlock()
	}()
	if err := sendFile(rw(r), name, "application/octet-stream", func(w io.Writer) error { _, err := io.Copy(w, f); return err }); err != nil {
		return nil, bad("%v", err)
	}
	return streamed{}, nil
}

// execStream is /exec as SSE. Per statement i: `columns` {i, stmt, columns}, `rows` {i, cells}
// in batches of 100, then `done` {i, affected, message, noticeId, durationMs, truncated, hasRows, error};
// `end` closes the stream. The gate, parameters and row limit are exactly /exec's, and fail the
// same way (a JSON error before the stream starts).
// shortcut: db.Session has no row cursor, so a statement's rows are fetched whole and then sent in batches; upgrade when the drivers expose one.
func (s *Server) execStream(ss *session, r *http.Request) (any, error) {
	stmts, limit, sess, run, err := s.execPrep(ss, r)
	if err != nil {
		return nil, err
	}
	ev, err := newSSE(r)
	if err != nil {
		return nil, err
	}
	var ran []string
	if len(stmts) > summarizeOver {
		it := ss.runSummary(r, sess, stmts, limit)
		ev.send("done", map[string]any{"i": 0, "stmt": it.Stmt, "label": it.Label, "message": it.Message, "affected": it.Affected, "durationMs": it.DurationMs})
		for _, st := range stmts {
			ran = append(ran, st.Text)
		}
		ev.send("end", run.done(ss, ran))
		return streamed{}, nil
	}
	defer func() { ev.send("end", run.done(ss, ran)) }() // the pin and transaction state, also after a cancel
	for i, st := range stmts {
		it := ss.execOne(r, sess, st, limit)
		ran = append(ran, st.Text)
		if it.Error == "" {
			ev.send("columns", map[string]any{"i": i, "stmt": it.Stmt, "label": it.Label, "columns": it.Columns})
			for from := 0; from < len(it.Cells); from += 100 {
				ev.send("rows", map[string]any{"i": i, "cells": it.Cells[from:min(from+100, len(it.Cells))]})
			}
		}
		ev.send("done", map[string]any{"i": i, "stmt": it.Stmt, "label": it.Label, "error": it.Error, "affected": it.Affected, "message": it.Message, "noticeId": it.NoticeID,
			"durationMs": it.DurationMs, "truncated": it.Truncated, "hasRows": it.HasRows, "docs": it.Docs,
			"editable": it.Editable, "table": it.Table, "resultId": it.ResultID})
		if it.Error != "" && ss.srv.o.Cfg.Editor.ReturnOnError {
			break
		}
		if r.Context().Err() != nil {
			return streamed{}, nil
		}
	}
	return streamed{}, nil
}
