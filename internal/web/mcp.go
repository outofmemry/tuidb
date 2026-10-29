package web

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"webtuidb/internal/mcp"
)

func (s *Server) mcpLog(r *http.Request) (any, error) {
	if r.URL.Query().Get("format") == "csv" {
		return s.mcpLogCSV()
	}
	if s.mcp == nil {
		return map[string]any{"enabled": false, "entries": []any{}}, nil
	}
	type ent struct {
		Time       string `json:"time"`
		Token      string `json:"token"`
		Tool       string `json:"tool"`
		Connection string `json:"connection"`
		SQL        string `json:"sql"`
		Err        string `json:"error"`
	}
	out := []ent{}
	for _, e := range s.mcp.Log() {
		out = append(out, ent{e.Time.Format("15:04:05"), e.Token, e.Tool, e.Connection, e.SQL, e.Err})
	}
	return map[string]any{"enabled": true, "port": s.o.Cfg.MCP.Port, "listen": s.mcpAddr(), "tls": s.o.Cfg.MCP.TLSCert != "", "entries": out}, nil
}

const approvalTimeout = 5 * time.Minute

type pendingApproval struct {
	ID         string `json:"id"`
	Token      string `json:"token"`
	Connection string `json:"connection"`
	SQL        string `json:"sql"`
	ch         chan bool
}

type approvals struct {
	mu    sync.Mutex
	items []*pendingApproval
}

// ask is mcp.Server.Approver: it parks the statement until the browser answers, ctx ends or 5 minutes pass.
func (a *approvals) ask(ctx context.Context, ap mcp.Approval) (bool, error) {
	p := &pendingApproval{ID: randID(8), Token: ap.Token, Connection: ap.Connection, SQL: ap.SQL, ch: make(chan bool, 1)}
	a.mu.Lock()
	a.items = append(a.items, p)
	a.mu.Unlock()
	defer a.remove(p.ID)
	t := time.NewTimer(approvalTimeout)
	defer t.Stop()
	select {
	case ok := <-p.ch:
		return ok, nil
	case <-ctx.Done():
		return false, ctx.Err()
	case <-t.C:
		return false, errors.New("approval timed out after 5 minutes")
	}
}

func (a *approvals) remove(id string) *pendingApproval {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, p := range a.items {
		if p.ID == id {
			a.items = append(a.items[:i:i], a.items[i+1:]...)
			return p
		}
	}
	return nil
}

func (a *approvals) list() []*pendingApproval {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*pendingApproval{}, a.items...)
}

func (s *Server) answerApproval(r *http.Request) (any, error) {
	var req struct{ Approve bool }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	p := s.ap.remove(r.PathValue("id"))
	if p == nil {
		return nil, bad("no such pending approval (answered or timed out)")
	}
	p.ch <- req.Approve // buffered, so it never blocks
	return nil, nil
}

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
