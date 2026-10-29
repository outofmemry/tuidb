package ui

import (
	"context"
	"testing"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/mcp"

	tea "charm.land/bubbletea/v2"
)

func TestM3MCPActivityWithoutServer(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("p") // open_anything
	h.typeText("MCP")
	h.press("enter")
	h.want("MCP server is off")
}

func TestMCPApprovalSheet(t *testing.T) {
	for _, c := range []struct {
		key  string
		want bool
	}{{"y", true}, {"esc", false}, {"enter", false}} { // a reflexive Enter must not approve
		h := newHarness(t, nil)
		h.a.mcpUI = make(chan func() tea.Cmd)
		got := make(chan bool, 1)
		go func() {
			ok, _ := h.a.approve(context.Background(), mcp.Approval{Token: "cli", Connection: "shop", SQL: "DELETE FROM t"})
			got <- ok
		}()
		h.a.Update(liveMsg{<-h.a.mcpUI})
		h.want("MCP wants to run a statement")
		h.want("DELETE FROM t")
		h.press("y") // inside the 700 ms window nothing answers
		select {
		case <-got:
			t.Fatal("a key right after the sheet opened answered it")
		default:
		}
		time.Sleep(750 * time.Millisecond)
		h.press(c.key)
		if ok := <-got; ok != c.want {
			t.Errorf("%s: approved=%v", c.key, ok)
		}
	}
}

func TestM7MCPServerGetsTheStores(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.MCP.Enabled, c.MCP.Listen = true, "127.0.0.1:0" })
	if h.a.mcp == nil || h.a.mcp.Queries != h.a.d.Queries || h.a.mcp.Hist != h.a.d.Hist || !h.a.mcp.Boards {
		t.Fatal("the new MCP tools need the saved queries, the history and the boards")
	}
	h.connect()
	h.run("mcp_log")
	h.want("Tools: list_connections")
}
