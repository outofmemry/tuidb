// Package web serves the webtuidb engine over a local HTTP+JSON API and the embedded web app.
// It only wraps db.Session, store and config; the terminal UI is untouched.
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/crashlog"
	"webtuidb/internal/mcp"
	"webtuidb/internal/store"
	"webtuidb/internal/ui"
)

//go:embed all:dist
var distFS embed.FS

type Options struct {
	Cfg      config.Config
	Conns    []config.Connection
	ConnPath string
	CfgPath  string // the config.toml in use (-config); "" = the default under ConfigDir
	State    *store.State
	Hist     *store.History
	Queries  *store.Queries
	Listen   string
	Token    string // "" = random
	Open     bool
	Insecure bool           // allow a non-loopback Listen
	Connect  ui.ConnectFunc // nil = ui.DefaultConnect; tests inject a fake
	Version  string         // shown in /api/meta
}

type Server struct {
	o         Options
	connMu    sync.Mutex // guards o.Conns
	queryMu   sync.Mutex // serialises the check-and-write of a saved query update
	mu        sync.Mutex // guards sessions
	sessions  map[string]*session
	mcp       *mcp.Server // nil unless [mcp] enabled
	ap        approvals
	stamp     string // guarded by connMu: mtimes of the connection files at the last (re)load
	logins    logins
	con       consoleLog
	liveMu    sync.Mutex
	live      *config.Config // see cfg()
	pass      passFails
	chatOnce  sync.Once
	chatStore *store.Chats // the default under the data dir, made on first use (chats())
}

func New(o Options) *Server {
	if o.Connect == nil {
		o.Connect = ui.DefaultConnect
	}
	if o.Token == "" {
		o.Token = randID(16)
	}
	s := &Server{o: o, sessions: map[string]*session{}}
	for _, d := range o.Cfg.Queries.SharedDirs { // [queries] shared_dirs: listed beside the main folder, read-only
		if o.Queries != nil {
			o.Queries.AddDir(expandDir(d))
		}
	}
	s.stamp = s.connStamp()
	if o.Cfg.MCP.Enabled {
		s.mcp = mcp.New(o.Cfg, o.Conns)
		s.mcp.Queries, s.mcp.Hist, s.mcp.Boards = o.Queries, o.Hist, true
		s.mcp.Approver = s.ap.ask // shortcut: the MCP server sees the connections saved at startup, restart to pick up new ones
	}
	return s
}

func randID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func loopback(addr string) bool {
	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// Run serves until interrupted. It prints the URL (with the token in the fragment, which
// browsers never send to a server) and closes every open session on exit.
func Run(o Options) error {
	if o.Listen == "" {
		o.Listen = "127.0.0.1:7788"
	}
	if !o.Insecure && !loopback(o.Listen) {
		return fmt.Errorf("refusing to listen on %q: not a loopback address (pass --insecure-listen to override; credentials will cross the network)", o.Listen)
	}
	w := o.Cfg.Web
	if (w.TLSCert == "") != (w.TLSKey == "") {
		return errors.New("[web] tls_cert and tls_key must be set together")
	}
	s := New(o)
	if d := devOrigins(); len(d) > 0 {
		fmt.Fprintf(os.Stderr, "webtuidb: WARNING: WEBTUIDB_DEV_ORIGIN lets %s call the API (token still required); unset it outside development\n", strings.Join(d, ", "))
	}
	ln, err := net.Listen("tcp", o.Listen)
	if err != nil {
		return err
	}
	if !s.tls() && !loopback(o.Listen) {
		fmt.Fprintf(os.Stderr, "webtuidb: WARNING: serving plain HTTP on %s; the token, any login password and your data cross the network unencrypted (set [web] tls_cert and tls_key)\n", o.Listen)
	}
	url := fmt.Sprintf("%s://%s/#token=%s", s.scheme(), ln.Addr(), s.o.Token)
	fmt.Println("webtuidb web UI:", url)
	if _, err := fs.Stat(distFS, "dist/index.html"); err != nil {
		fmt.Fprintln(os.Stderr, "webtuidb: the web app is not built into this binary; run `make web` then `make build` (the API still works)")
	}
	if o.Open {
		openBrowser(url)
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := srv.Shutdown(sh); err != nil {
			fmt.Fprintln(os.Stderr, "webtuidb: shutdown:", err)
		}
	}()
	go s.reapIdle(ctx, idleSessionTTL)
	if s.mcp != nil {
		go s.serveMCP(ctx)
	}
	if s.tls() {
		err = srv.ServeTLS(ln, w.TLSCert, w.TLSKey)
	} else {
		err = srv.Serve(ln)
	}
	if errors.Is(err, http.ErrServerClosed) {
		<-drained // close pools only after in-flight handlers finished (or the drain expired)
	}
	s.closeAll()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "webtuidb: could not open the browser:", err)
		return
	}
	go cmd.Wait()
}

// apiErr is a handler error with an HTTP status and optional extra JSON fields (the gate info).
type apiErr struct {
	Code  int
	Msg   string
	Extra map[string]any
}

func (e *apiErr) Error() string { return e.Msg }

func bad(format string, a ...any) error { return &apiErr{Code: 400, Msg: fmt.Sprintf(format, a...)} }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// devOrigins is the explicit allow-list in WEBTUIDB_DEV_ORIGIN (comma separated origins such as
// http://localhost:5173), for a front end served from another port. Empty = none.
func devOrigins() []string {
	var out []string
	for _, o := range strings.Split(os.Getenv("WEBTUIDB_DEV_ORIGIN"), ",") {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			out = append(out, o)
		}
	}
	return out
}

// effPort is the port of a URL host, defaulting by scheme.
func effPort(scheme, port string) string {
	if port != "" {
		return port
	}
	if scheme == "https" {
		return "443"
	}
	return "80"
}

// originOK checks Origin against the configured listen address, not the client-supplied Host
// (DNS rebinding). On a loopback listener the Origin must be a loopback host on the listen port, or
// equal the Host header (the Vite dev server proxies without rewriting Host); any other origin must
// be listed in WEBTUIDB_DEV_ORIGIN.
func (s *Server) originOK(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	if s.o.Listen == "" || !loopback(s.o.Listen) {
		return o == s.scheme()+"://"+r.Host // non-loopback (insecure) or unconfigured: nothing better to compare with
	}
	for _, d := range devOrigins() {
		if o == d {
			return true
		}
	}
	u, err := url.Parse(o)
	if err != nil || u.Scheme != s.scheme() {
		return false
	}
	if u.Host == s.o.Listen || u.Host == r.Host && loopback(u.Host) { // the Vite proxy keeps Host, so Origin matches it
		return true
	}
	_, lp, _ := net.SplitHostPort(s.o.Listen)
	h := u.Hostname()
	return (h == "localhost" || h == "127.0.0.1" || h == "::1") && effPort(u.Scheme, u.Port()) == lp
}

func (s *Server) authorized(r *http.Request) bool {
	if !s.originOK(r) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	return s.credentialed(r)
}

func (s *Server) credentialed(r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.o.Token)) == 1 {
		return true
	}
	if s.o.Cfg.Web.Password != "" { // the login cookie stands in for the token
		if c, err := r.Cookie(cookieName); err == nil {
			return s.logins.valid(c.Value)
		}
	}
	return false
}

type handler func(r *http.Request) (any, error)

type ctxKey struct{}

// rw is the ResponseWriter of an api() request, for handlers that stream (SSE, file downloads).
func rw(r *http.Request) http.ResponseWriter {
	w, _ := r.Context().Value(ctxKey{}).(http.ResponseWriter)
	return w
}

// streamed is what a handler returns after it wrote the response itself through rw.
type streamed struct{}

func fail(w http.ResponseWriter, err error) {
	var ae *apiErr
	if errors.As(err, &ae) {
		body := map[string]any{"error": ae.Msg}
		for k, x := range ae.Extra {
			body[k] = x
		}
		writeJSON(w, ae.Code, body)
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

// call runs h; a panic in it is written to the crash log and becomes a 500 for that request only.
func (s *Server) call(h handler, r *http.Request) (v any, err error) {
	defer crashlog.Catch(s.con.tail, func(p any) {
		v, err = nil, fmt.Errorf("internal error: %v (see %s)", p, crashlog.Path())
	})
	return h(r)
}

func (s *Server) api(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.originOK(r) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "origin not allowed (set WEBTUIDB_DEV_ORIGIN=http://host:port to allow a dev front end)"})
			return
		}
		if !s.credentialed(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized", "login": s.o.Cfg.Web.Password != ""})
			return
		}
		v, err := s.call(h, r.WithContext(context.WithValue(r.Context(), ctxKey{}, w)))
		if err != nil {
			fail(w, err)
			return
		}
		if _, ok := v.(streamed); ok {
			return
		}
		if d, ok := v.(*download); ok {
			if d.ctype == "" {
				d.ctype = "application/octet-stream"
			}
			w.Header().Set("Content-Type", d.ctype)
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": d.name}))
			w.Header().Set("Cache-Control", "no-store")
			w.Write(d.data)
			return
		}
		if v == nil {
			v = map[string]any{"ok": true}
		}
		writeJSON(w, http.StatusOK, v)
	}
}

func decode(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	// a cross-site form or fetch can only send text/plain, urlencoded or multipart without a preflight
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		return &apiErr{Code: http.StatusUnsupportedMediaType, Msg: "Content-Type must be application/json"}
	}
	if err := json.Unmarshal(body, v); err != nil {
		return bad("invalid JSON: %v", err)
	}
	return nil
}

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	// global
	m.HandleFunc("POST /api/login", s.login)
	m.HandleFunc("GET /api/meta", s.api(s.meta))
	m.HandleFunc("GET /api/connections", s.api(s.getConns))
	m.HandleFunc("PUT /api/connections", s.api(s.putConns))
	m.HandleFunc("POST /api/connections/export", s.api(s.exportConns))
	m.HandleFunc("POST /api/connections/test", s.api(s.testConn))
	m.HandleFunc("POST /api/connections/parse", s.api(s.parseConnURL))
	m.HandleFunc("POST /api/connections/format", s.api(s.formatConnURL))
	m.HandleFunc("POST /api/sessions", s.api(s.openSession))
	// per session
	sp := "/api/sessions/{id}"
	m.HandleFunc("GET "+sp, s.api(s.withSession(func(ss *session, r *http.Request) (any, error) { return ss.info(), nil })))
	m.HandleFunc("DELETE "+sp, s.api(s.closeSession))
	m.HandleFunc("POST "+sp+"/safe-mode", s.api(s.withSession(s.setSafeMode)))
	m.HandleFunc("POST "+sp+"/ping", s.api(s.withSession(s.ping)))
	m.HandleFunc("GET "+sp+"/databases", s.api(s.withSession(s.databases)))
	m.HandleFunc("POST "+sp+"/database", s.api(s.withSession(s.useDatabase)))
	m.HandleFunc("GET "+sp+"/schemas", s.api(s.withSession(s.schemas)))
	m.HandleFunc("GET "+sp+"/objects", s.api(s.withSession(s.objects)))
	m.HandleFunc("GET "+sp+"/object/{what}", s.api(s.withSession(s.object)))
	m.HandleFunc("POST "+sp+"/rows", s.api(s.withSession(s.rows)))
	m.HandleFunc("POST "+sp+"/count", s.api(s.withSession(s.count)))
	m.HandleFunc("POST "+sp+"/exec", s.api(s.withSession(s.exec)))
	m.HandleFunc("POST "+sp+"/exec/stream", s.api(s.withSession(s.execStream)))
	m.HandleFunc("GET "+sp+"/notices/{rid}", s.api(s.withSession(s.notices)))
	m.HandleFunc("POST "+sp+"/exec/page", s.api(s.withSession(s.execPage)))
	m.HandleFunc("POST "+sp+"/pin/{tab}", s.api(s.withSession(s.pinTab)))
	m.HandleFunc("DELETE "+sp+"/pin/{tab}", s.api(s.withSession(s.pinTab)))
	m.HandleFunc("POST "+sp+"/plan/table", s.api(s.withSession(s.planTableEdit)))
	m.HandleFunc("POST "+sp+"/explain", s.api(s.withSession(s.explain)))
	m.HandleFunc("POST "+sp+"/tools/schema-diff", s.api(s.withSession(s.schemaDiff)))
	m.HandleFunc("POST "+sp+"/tools/data-diff", s.api(s.withSession(s.dataDiff)))
	m.HandleFunc("POST "+sp+"/tools/bulk-update", s.api(s.withSession(s.bulkUpdate)))
	m.HandleFunc("POST "+sp+"/tools/fake-rows", s.api(s.withSession(s.fakeRows)))
	m.HandleFunc("DELETE "+sp+"/results/{rid}", s.api(s.withSession(s.dropResult)))
	m.HandleFunc("POST "+sp+"/plan", s.api(s.withSession(s.plan)))
	m.HandleFunc("POST "+sp+"/plan/admin", s.api(s.withSession(s.planAdmin)))
	m.HandleFunc("POST "+sp+"/apply", s.api(s.withSession(s.apply)))
	m.HandleFunc("POST "+sp+"/export", s.api(s.withSession(s.export)))
	m.HandleFunc("POST "+sp+"/import/preview", s.api(s.withSession(s.importPreview)))
	m.HandleFunc("POST "+sp+"/import", s.api(s.withSession(s.importStart)))
	m.HandleFunc("POST "+sp+"/search", s.api(s.withSession(s.searchStart)))
	m.HandleFunc("POST "+sp+"/search/{job}/pause", s.api(s.withSession(s.searchPause)))
	m.HandleFunc("POST "+sp+"/search/{job}/resume", s.api(s.withSession(s.searchResume)))
	m.HandleFunc("GET "+sp+"/datatypes", s.api(s.withSession(s.datatypes)))
	m.HandleFunc("POST "+sp+"/structure/plan", s.api(s.withSession(s.structurePlan)))
	m.HandleFunc("POST /api/queries/rename", s.api(s.renameQuery))
	m.HandleFunc("GET "+sp+"/jobs", s.api(s.withSession(s.jobs)))
	m.HandleFunc("POST "+sp+"/jobs/{jid}/cancel", s.api(s.withSession(s.jobCancel)))
	m.HandleFunc("GET "+sp+"/jobs/{jid}", s.api(s.withSession(s.jobGet)))
	m.HandleFunc("GET "+sp+"/jobs/{jid}/events", s.api(s.withSession(s.jobEvents)))
	m.HandleFunc("GET "+sp+"/jobs/{jid}/file", s.api(s.withSession(s.jobFile)))
	m.HandleFunc("DELETE "+sp+"/jobs/{jid}", s.api(s.withSession(s.jobStop)))
	m.HandleFunc("GET "+sp+"/processes", s.api(s.withSession(s.processes)))
	m.HandleFunc("POST "+sp+"/processes/kill", s.api(s.withSession(s.killProcess)))
	m.HandleFunc("GET "+sp+"/users", s.api(s.withSession(s.users)))
	m.HandleFunc("POST "+sp+"/users/plan", s.api(s.withSession(s.usersPlan)))
	m.HandleFunc("POST "+sp+"/backup", s.api(s.withSession(s.backup)))
	m.HandleFunc("POST "+sp+"/restore", s.api(s.withSession(s.restore)))
	m.HandleFunc("GET "+sp+"/diagram", s.api(s.withSession(s.diagram)))
	m.HandleFunc("POST "+sp+"/boards/run", s.api(s.withSession(s.boardRun)))
	m.HandleFunc("POST "+sp+"/boards/panel", s.api(s.withSession(s.boardPanel)))
	m.HandleFunc("POST "+sp+"/filters/sql", s.api(s.withSession(s.filtersSQL)))
	m.HandleFunc("POST "+sp+"/color", s.api(s.withSession(s.setColor)))
	m.HandleFunc("POST "+sp+"/plugins/run", s.api(s.withSession(s.pluginRun)))
	m.HandleFunc("POST "+sp+"/assistant", s.api(s.withSession(s.assistant)))
	m.HandleFunc("GET /api/boards", s.api(s.boards))
	m.HandleFunc("POST /api/boards", s.api(s.boardSave))
	m.HandleFunc("POST /api/boards/duplicate", s.api(s.boardDuplicate))
	m.HandleFunc("PUT /api/boards/{name}", s.api(s.boardPut))
	m.HandleFunc("DELETE /api/boards/{name}", s.api(s.boardDelete))
	m.HandleFunc("GET /api/settings", s.api(s.settings))
	m.HandleFunc("GET /api/state/groups", s.api(s.getGroups))
	m.HandleFunc("PUT /api/state/groups", s.api(s.putGroups))
	m.HandleFunc("GET /api/state/recent-files", s.api(s.getRecentFiles))
	m.HandleFunc("PUT /api/state/recent-files", s.api(s.putRecentFile))
	m.HandleFunc("POST /api/files/open", s.api(s.openRecentFile))
	m.HandleFunc("GET /api/state/connection-sort", s.api(s.getConnSort))
	m.HandleFunc("PUT /api/state/connection-sort", s.api(s.putConnSort))
	m.HandleFunc("GET /api/state/backup-options", s.api(s.getBackupOptions))
	m.HandleFunc("PUT /api/state/backup-options", s.api(s.putBackupOptions))
	m.HandleFunc("GET /api/assistant/models", s.api(s.assistantModels))
	m.HandleFunc("GET /api/plugins", s.api(s.plugins))
	m.HandleFunc("GET /api/mcp/log", s.api(s.mcpLog))
	m.HandleFunc("GET /api/mcp/clients", s.api(s.mcpClients))
	m.HandleFunc("GET /api/mcp/approvals", s.api(func(*http.Request) (any, error) { return s.ap.list(), nil }))
	m.HandleFunc("POST /api/mcp/approvals/{id}", s.api(s.answerApproval))
	m.HandleFunc("GET /api/state/tabs", s.api(s.getTabs))
	m.HandleFunc("PUT /api/state/tabs", s.api(s.putTabs))
	m.HandleFunc("GET /api/console", s.api(s.getConsole))
	m.HandleFunc("DELETE /api/console", s.api(s.clearConsole))
	m.HandleFunc("GET /api/connections/export", s.api(s.exportConns))
	m.HandleFunc("POST /api/connections/import", s.api(s.importConns))
	m.HandleFunc("GET /api/state/folds", s.api(s.getFolds))
	m.HandleFunc("PUT /api/state/folds", s.api(s.putFolds))
	m.HandleFunc("POST /api/split", s.api(s.split))
	m.HandleFunc("POST /api/format", s.api(s.format))
	m.HandleFunc("POST /api/params", s.api(s.params))
	m.HandleFunc("POST /api/chart", s.api(s.chart))
	// store
	m.HandleFunc("GET /api/history", s.api(s.history))
	m.HandleFunc("DELETE /api/history", s.api(s.deleteHistory))
	m.HandleFunc("GET /api/history/export", s.api(s.exportHistory))
	m.HandleFunc("POST /api/history/import", s.api(s.importHistory))
	m.HandleFunc("GET /api/queries/export", s.api(s.exportQueries))
	m.HandleFunc("POST /api/queries/import-file", s.api(s.importQueryFile))
	m.HandleFunc("GET /api/queries", s.api(s.listQueries))
	m.HandleFunc("GET /api/queries/{path...}", s.api(s.getQuery))
	m.HandleFunc("POST /api/queries", s.api(s.saveQuery))
	m.HandleFunc("PUT /api/queries/{path...}", s.api(s.updateQuery))
	m.HandleFunc("DELETE /api/queries/{path...}", s.api(s.deleteQuery))
	m.HandleFunc("GET /api/state/table", s.api(s.getTableState))
	m.HandleFunc("PUT /api/state/table", s.api(s.putTableState))
	m.HandleFunc("GET /api/state/shortcuts", s.api(s.shortcuts))
	m.HandleFunc("POST /api/state/pin", s.api(s.pin))
	m.HandleFunc("POST /api/state/touch", s.api(s.touch))
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no such endpoint"})
	})
	m.HandleFunc("/", s.static)
	return m
}

// static serves the embedded Vite build; unknown paths fall back to index.html (SPA routing).
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	sub, _ := fs.Sub(distFS, "dist")
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	if f, err := sub.Open(name); err == nil {
		st, _ := f.Stat()
		f.Close()
		if !st.IsDir() {
			http.ServeFileFS(w, r, sub, name)
			return
		}
	}
	if strings.HasPrefix(name, "assets/") || path.Ext(name) != "" { // a stale hashed bundle must 404, not get HTML
		http.NotFound(w, r)
		return
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		http.Error(w, "web app not built: run `make web` and rebuild", http.StatusServiceUnavailable)
		return
	}
	http.ServeFileFS(w, r, sub, "index.html")
}
