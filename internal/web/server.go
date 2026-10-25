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
	"webtuidb/internal/store"
	"webtuidb/internal/ui"
)

//go:embed all:dist
var distFS embed.FS

type Options struct {
	Cfg      config.Config
	Conns    []config.Connection
	ConnPath string
	State    *store.State
	Hist     *store.History
	Queries  *store.Queries
	Listen   string
	Token    string // "" = random
	Open     bool
	Insecure bool           // allow a non-loopback Listen
	Connect  ui.ConnectFunc // nil = ui.DefaultConnect; tests inject a fake
}

type Server struct {
	o        Options
	connMu   sync.Mutex // guards o.Conns
	mu       sync.Mutex // guards sessions
	sessions map[string]*session
}

func New(o Options) *Server {
	if o.Connect == nil {
		o.Connect = ui.DefaultConnect
	}
	if o.Token == "" {
		o.Token = randID(16)
	}
	return &Server{o: o, sessions: map[string]*session{}}
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
	s := New(o)
	ln, err := net.Listen("tcp", o.Listen)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s/#token=%s", ln.Addr(), s.o.Token)
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
	err = srv.Serve(ln)
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

// originOK checks Origin against the configured listen address, not the client-supplied Host
// (DNS rebinding). On a loopback listener any localhost/127.0.0.1/[::1] port is accepted so the
// Vite dev server, which proxies without rewriting Host, keeps working.
func (s *Server) originOK(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	if s.o.Listen == "" || !loopback(s.o.Listen) {
		return o == "http://"+r.Host // non-loopback (insecure) or unconfigured: nothing better to compare with
	}
	u, err := url.Parse(o)
	if err != nil || u.Scheme != "http" {
		return false
	}
	h := u.Hostname()
	return u.Host == s.o.Listen || h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func (s *Server) authorized(r *http.Request) bool {
	if !s.originOK(r) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.o.Token)) == 1
}

type handler func(r *http.Request) (any, error)

func (s *Server) api(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		v, err := h(r)
		if err != nil {
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
	if err := json.Unmarshal(body, v); err != nil {
		return bad("invalid JSON: %v", err)
	}
	return nil
}

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	// global
	m.HandleFunc("GET /api/meta", s.api(s.meta))
	m.HandleFunc("GET /api/connections", s.api(s.getConns))
	m.HandleFunc("PUT /api/connections", s.api(s.putConns))
	m.HandleFunc("POST /api/connections/test", s.api(s.testConn))
	m.HandleFunc("POST /api/connections/parse", s.api(s.parseConnURL))
	m.HandleFunc("POST /api/connections/format", s.api(s.formatConnURL))
	m.HandleFunc("POST /api/sessions", s.api(s.openSession))
	// per session
	sp := "/api/sessions/{id}"
	m.HandleFunc("GET "+sp, s.api(s.withSession(func(ss *session, r *http.Request) (any, error) { return ss.info(), nil })))
	m.HandleFunc("DELETE "+sp, s.api(s.closeSession))
	m.HandleFunc("POST "+sp+"/ping", s.api(s.withSession(s.ping)))
	m.HandleFunc("GET "+sp+"/databases", s.api(s.withSession(s.databases)))
	m.HandleFunc("POST "+sp+"/database", s.api(s.withSession(s.useDatabase)))
	m.HandleFunc("GET "+sp+"/schemas", s.api(s.withSession(s.schemas)))
	m.HandleFunc("GET "+sp+"/objects", s.api(s.withSession(s.objects)))
	m.HandleFunc("GET "+sp+"/object/{what}", s.api(s.withSession(s.object)))
	m.HandleFunc("POST "+sp+"/rows", s.api(s.withSession(s.rows)))
	m.HandleFunc("POST "+sp+"/count", s.api(s.withSession(s.count)))
	m.HandleFunc("POST "+sp+"/exec", s.api(s.withSession(s.exec)))
	m.HandleFunc("DELETE "+sp+"/results/{rid}", s.api(s.withSession(s.dropResult)))
	m.HandleFunc("POST "+sp+"/plan", s.api(s.withSession(s.plan)))
	m.HandleFunc("POST "+sp+"/plan/admin", s.api(s.withSession(s.planAdmin)))
	m.HandleFunc("POST "+sp+"/apply", s.api(s.withSession(s.apply)))
	// store
	m.HandleFunc("GET /api/history", s.api(s.history))
	m.HandleFunc("DELETE /api/history", s.api(s.deleteHistory))
	m.HandleFunc("GET /api/queries", s.api(s.listQueries))
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
