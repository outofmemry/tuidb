package web

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
	"webtuidb/internal/config"
	"webtuidb/internal/mcp"
)

func TestMcpApprovalRoundTrip(t *testing.T) {
	e := newEnvCfg(t, "alert1", func(c *config.Config) { c.MCP.Enabled = true })
	got := make(chan bool, 1)
	go func() {
		ok, _ := e.s.mcp.Approver(context.Background(), mcp.Approval{Token: "t", Connection: "c1", SQL: "DELETE FROM x"})
		got <- ok
	}()
	var id string
	for i := 0; i < 200 && id == ""; i++ {
		if l := e.s.ap.list(); len(l) == 1 {
			id = l[0].ID
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if id == "" {
		t.Fatal("approval never listed")
	}
	e.ok("POST", "/api/mcp/approvals/"+id, map[string]any{"Approve": true})
	if !<-got {
		t.Fatal("approver should return true")
	}
	if code, _ := e.call("POST", "/api/mcp/approvals/"+id, map[string]any{}, "tok"); code != 400 {
		t.Fatalf("second answer: %d", code)
	}
}

func TestMCPCSVAndClients(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) { c.MCP.Enabled = true })
	e.ok("GET", "/api/mcp/log", nil)
	r := e.raw("GET", "/api/mcp/log?format=csv", nil, nil)
	b, _ := io.ReadAll(r.Body)
	if !strings.HasPrefix(string(b), "time,token,tool") || !strings.Contains(r.Header.Get("Content-Disposition"), "mcp-log.csv") || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/csv") {
		t.Fatalf("csv: %q %v", b, r.Header)
	}
	c := e.ok("GET", "/api/mcp/clients", nil)
	if c["enabled"] != true || len(c["clients"].([]any)) != 0 {
		t.Fatalf("clients: %v", c)
	}
	_ = mcp.Entry{}
}

func TestMCPGetsStores(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) { c.MCP.Enabled = true })
	if e.s.mcp == nil || e.s.mcp.Queries == nil || e.s.mcp.Hist == nil || !e.s.mcp.Boards {
		t.Fatal("the MCP server must get the saved queries, the history and the boards")
	}
}
