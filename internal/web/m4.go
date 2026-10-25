package web

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"webtuidb/internal/config"
)

// Milestone 4/5 server infrastructure: streaming responses, SSE, connection reload, TLS and login,
// MCP listener and log, the server-side console.

// ---- streaming responses ---------------------------------------------------------------------

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

// ---- server-sent events ------------------------------------------------------------------------

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
	stmts, limit, sess, err := s.execPrep(ss, r)
	if err != nil {
		return nil, err
	}
	ev, err := newSSE(r)
	if err != nil {
		return nil, err
	}
	if len(stmts) > summarizeOver {
		it := ss.runSummary(r, sess, stmts, limit)
		ev.send("done", map[string]any{"i": 0, "stmt": it.Stmt, "label": it.Label, "message": it.Message, "affected": it.Affected, "durationMs": it.DurationMs})
		ev.send("end", map[string]any{})
		return streamed{}, nil
	}
	for i, st := range stmts {
		it := ss.execOne(r, sess, st, limit)
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
	ev.send("end", map[string]any{})
	return streamed{}, nil
}

// ---- PostgreSQL notices ---------------------------------------------------------------------------

var noticeRe = regexp.MustCompile(`^(DEBUG[1-5]?|LOG|INFO|NOTICE|WARNING|EXCEPTION): (.*)$`)

type notice struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// noticeLines picks the server notices out of a result message: the postgres driver appends
// "SEVERITY: text" lines to the command tag, and a line that is not one continues the previous notice.
func noticeLines(msg string) (tag string, ns []notice) {
	var tagLines []string
	for _, l := range strings.Split(msg, "\n") {
		if m := noticeRe.FindStringSubmatch(l); m != nil {
			ns = append(ns, notice{m[1], m[2]})
		} else if len(ns) > 0 {
			ns[len(ns)-1].Message += "\n" + l
		} else {
			tagLines = append(tagLines, l)
		}
	}
	return strings.TrimSpace(strings.Join(tagLines, "\n")), ns
}

func (s *Server) notices(ss *session, r *http.Request) (any, error) {
	ss.mu.Lock()
	msg, ok := ss.notices[r.PathValue("rid")]
	ss.mu.Unlock()
	if !ok {
		return nil, bad("no notices for that result (none were raised, or it was evicted)")
	}
	tag, ns := noticeLines(msg)
	return map[string]any{"message": tag, "notices": ns}, nil
}

// ---- connections.toml reload ------------------------------------------------------------------------

func expandDir(p string) string {
	p = os.ExpandEnv(p)
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(h, p[2:])
		}
	}
	return p
}

// connStamp changes when connections.toml or a file in a [[source]] folder does (name, mtime, size).
func (s *Server) connStamp() string {
	if s.o.ConnPath == "" {
		return ""
	}
	files := []string{s.o.ConnPath}
	for _, src := range s.o.Cfg.Sources {
		g, _ := filepath.Glob(filepath.Join(expandDir(src.Path), "*.toml"))
		files = append(files, g...)
	}
	var b strings.Builder
	for _, f := range files {
		if st, err := os.Stat(f); err == nil {
			fmt.Fprintf(&b, "%s|%d|%d;", f, st.ModTime().UnixNano(), st.Size())
		}
	}
	return b.String()
}

// reloadConns re-reads the connection files when they changed since the last look. connMu must be
// held. A file that no longer parses keeps the list we have and returns the parse error.
func (s *Server) reloadConns() error {
	if s.o.ConnPath == "" {
		return nil
	}
	st := s.connStamp()
	if st == s.stamp {
		return nil
	}
	cs, err := config.LoadAllConnections(s.o.ConnPath, s.o.Cfg.Sources)
	if err != nil {
		return err
	}
	s.stamp, s.o.Conns = st, cs
	return nil
}

// splitSources separates the connections that live in connections.toml (to be saved) from those
// that come from a [[source]] file, which are read-only: they must come back unchanged and all present.
func (s *Server) splitSources(in []config.Connection) (own, fromSource []config.Connection, err error) {
	have := map[string]config.Connection{}
	for _, c := range s.o.Conns {
		if c.Source != "" {
			have[c.Name] = c
		}
	}
	seen := map[string]bool{}
	for _, c := range in {
		old, isSrc := have[c.Name]
		switch {
		case isSrc && !reflect.DeepEqual(c, old):
			return nil, nil, bad("connection %q comes from %s and is read-only: edit that file instead", c.Name, old.Source)
		case isSrc:
			seen[c.Name] = true
			fromSource = append(fromSource, c)
		case c.Source != "":
			return nil, nil, bad("connection %q claims source %s, which does not hold it", c.Name, c.Source)
		default:
			own = append(own, c)
		}
	}
	for n, old := range have {
		if !seen[n] {
			return nil, nil, bad("connection %q comes from %s and is read-only: it cannot be removed here", n, old.Source)
		}
	}
	return own, fromSource, nil
}

// ---- TLS and login ------------------------------------------------------------------------------------

func (s *Server) tls() bool { return s.o.Cfg.Web.TLSCert != "" && s.o.Cfg.Web.TLSKey != "" }

func (s *Server) scheme() string {
	if s.tls() {
		return "https"
	}
	return "http"
}

const (
	cookieName = "webtuidb_session"
	loginTTL   = 12 * time.Hour
)

// logins holds the live login cookies: random values, server-side expiry.
type logins struct {
	mu     sync.Mutex
	m      map[string]time.Time
	fails  []time.Time // recent failed attempts, any client
	locked time.Time   // 401 for every attempt until then
}

const (
	loginMaxFails = 5
	loginWindow   = 10 * time.Minute
	loginLockout  = 60 * time.Second
)

// isLocked reports whether logins are refused right now.
func (l *logins) isLocked() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return time.Now().Before(l.locked)
}

// failed records a wrong password; the 5th within 10 minutes locks logins for 60 s.
func (l *logins) failed() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	keep := l.fails[:0]
	for _, t := range l.fails {
		if now.Sub(t) < loginWindow {
			keep = append(keep, t)
		}
	}
	l.fails = append(keep, now)
	if len(l.fails) >= loginMaxFails {
		l.fails, l.locked = nil, now.Add(loginLockout)
	}
}

func (l *logins) add() string {
	v := randID(32)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = map[string]time.Time{}
	}
	now := time.Now()
	for k, exp := range l.m {
		if now.After(exp) {
			delete(l.m, k)
		}
	}
	l.m[v] = now.Add(loginTTL)
	return v
}

func (l *logins) valid(v string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	exp, ok := l.m[v]
	return ok && time.Now().Before(exp)
}

// login trades the configured [web] password for an HttpOnly cookie that api() accepts in place of the bearer token.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	pw := s.o.Cfg.Web.Password
	if pw == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "login is not enabled ([web] password is not set)"})
		return
	}
	if !s.originOK(r) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	var req struct{ Password string }
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	if s.logins.isLocked() { // shortcut: one global counter, so an attacker can also lock the owner out for 60 s
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "too many failed logins; wait a minute"})
		return
	}
	a, b := sha256.Sum256([]byte(req.Password)), sha256.Sum256([]byte(pw))
	if subtle.ConstantTimeCompare(a[:], b[:]) != 1 {
		s.logins.failed()
		time.Sleep(400 * time.Millisecond)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "wrong password"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: s.logins.add(), Path: "/", MaxAge: int(loginTTL.Seconds()),
		HttpOnly: true, Secure: s.tls(), SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- MCP ------------------------------------------------------------------------------------------------

// serveMCP runs the MCP HTTP listener on [mcp] listen (default 127.0.0.1:<port>), with TLS when configured.
func (s *Server) serveMCP(ctx context.Context) {
	c := s.o.Cfg.MCP
	addr := c.Listen
	if addr == "" {
		addr = fmt.Sprintf("127.0.0.1:%d", c.Port)
	}
	tls := c.TLSCert != "" && c.TLSKey != ""
	if tls != (c.TLSCert != "" || c.TLSKey != "") {
		fmt.Fprintln(os.Stderr, "webtuidb: mcp: tls_cert and tls_key must be set together")
		return
	}
	if !tls && !loopback(addr) {
		fmt.Fprintf(os.Stderr, "webtuidb: WARNING: MCP is served as plain HTTP on %s; bearer tokens cross the network unencrypted (set [mcp] tls_cert and tls_key)\n", addr)
	}
	srv := &http.Server{Addr: addr, Handler: s.mcp.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(sh)
	}()
	var err error
	if tls {
		err = srv.ListenAndServeTLS(c.TLSCert, c.TLSKey)
	} else {
		err = srv.ListenAndServe()
	}
	if err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "webtuidb: mcp:", err)
	}
}

func (s *Server) mcpLogCSV() (any, error) {
	var b strings.Builder
	w := csv.NewWriter(&b)
	w.Write([]string{"time", "token", "tool", "connection", "sql", "error"})
	if s.mcp != nil {
		for _, e := range s.mcp.Log() {
			w.Write([]string{e.Time.Format(time.RFC3339), e.Token, e.Tool, e.Connection, e.SQL, e.Err})
		}
	}
	w.Flush()
	return &download{name: "mcp-log.csv", data: []byte(b.String()), ctype: "text/csv; charset=utf-8"}, nil
}

// mcpClients: internal/mcp keeps no list of connections, so this is the distinct token names that
// called a tool in the last 10 minutes, taken from the activity log.
func (s *Server) mcpClients(r *http.Request) (any, error) {
	type client struct {
		Token    string `json:"token"`
		LastSeen string `json:"lastSeen"`
		Calls    int    `json:"calls"`
	}
	seen := map[string]*client{}
	if s.mcp != nil {
		for _, e := range s.mcp.Log() {
			if time.Since(e.Time) > 10*time.Minute {
				continue
			}
			c := seen[e.Token]
			if c == nil {
				c = &client{Token: e.Token}
				seen[e.Token] = c
			}
			c.Calls++
			c.LastSeen = e.Time.Format(time.RFC3339)
		}
	}
	out := []client{}
	for _, c := range seen {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Token < out[j].Token })
	return map[string]any{"enabled": s.mcp != nil, "source": "activity log, last 10 minutes", "clients": out}, nil
}

// ---- console ------------------------------------------------------------------------------------------------

const consoleCap = 1000

type consoleLine struct {
	Time string `json:"time"`
	Conn string `json:"conn"`
	Text string `json:"text"`
}

// consoleLog is the server-side console: every statement the web UI ran, newest last, capped.
type consoleLog struct {
	mu    sync.Mutex
	lines []consoleLine
}

func (c *consoleLog) add(conn, text string) {
	if len(text) > 64<<10 { // same rule as store.Console: a big statement becomes a note
		text = fmt.Sprintf("<%d KB statement>", len(text)>>10)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, consoleLine{time.Now().Format(time.RFC3339), conn, text})
	if len(c.lines) > consoleCap {
		c.lines = c.lines[len(c.lines)-consoleCap:]
	}
}

// tail is the console lines as text (crashlog keeps the last 20), for the crash log.
func (c *consoleLog) tail() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, l := range c.lines {
		out = append(out, l.Conn+": "+l.Text)
	}
	return out
}

func (s *Server) getConsole(r *http.Request) (any, error) {
	conn := r.URL.Query().Get("conn")
	s.con.mu.Lock()
	defer s.con.mu.Unlock()
	out := []consoleLine{}
	for _, l := range s.con.lines {
		if conn == "" || l.Conn == conn {
			out = append(out, l)
		}
	}
	return out, nil
}

func (s *Server) clearConsole(r *http.Request) (any, error) {
	s.con.mu.Lock()
	s.con.lines = nil
	s.con.mu.Unlock()
	return nil, nil
}

func mimeAttachment(name string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": name})
}
