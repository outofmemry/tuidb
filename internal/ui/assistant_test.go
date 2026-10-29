package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/ai"
	"webtuidb/internal/config"
	"webtuidb/internal/store"
)

// P2-66: a failed save in the error path is reported.
func TestTUI1SendFailedReportsSaveError(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	ws := h.a.ws()
	blocker := filepath.Join(t.TempDir(), "file")
	writeTo(t, blocker, "x", 0o644)
	ws.a.d.Chats = &store.Chats{Dir: filepath.Join(blocker, "chats")} // unwritable: a file is in the way
	ws.panes.chat = nil
	tab := &assistantTab{ws: ws}
	ws.panes.chat = append(ws.panes.chat, ai.Message{Role: "user", Content: "earlier"}, ai.Message{Role: "assistant", Content: "ok"}, ai.Message{Role: "user", Content: "hi"}) // something is left to save
	h.pump(tab.sendFailed("hi", errors.New("boom"), false))
	h.want("saving the chat")
	h.want("boom")
}

// P2-68: a pane narrower than the Esc Stop label keeps a rule of exactly the pane width.
func TestTUI1StopRuleNeverWiderThanPane(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	tab := &assistantTab{ws: h.a.ws(), running: true}
	for _, w := range []int{4, 8, 11, 12, 40} {
		for _, l := range strings.Split(tab.View(h.a.th, w, 6), "\n") {
			if got := width(l); got > w {
				t.Fatalf("W=%d: line %q is %d wide", w, l, got)
			}
		}
	}
}

func TestM3AssistantRunsASQLBlockInAQueryTab(t *testing.T) {
	var req string
	srv := chatServer(t, "Try:\n```sql\nselect * from customers\n```\nDone.", &req)
	h := newHarness(t, func(_ *config.Connection, c *config.Config) {
		c.AI = config.AI{Provider: "openai", Model: "m", BaseURL: srv.URL, APIKey: "k"}
	})
	h.connect()
	h.prefix("A")
	h.typeText("list @customers")
	h.press("enter")
	h.want("select * from customers")
	h.want("copy or run the 1 SQL block")
	if !strings.Contains(req, "CREATE TABLE customers") {
		t.Fatalf("the @mention did not put the DDL in the system prompt: %s", req)
	}
	h.press("ctrl+o")
	h.want("SQL from the assistant")
	h.press("enter")
	if q, ok := h.a.ws().tab().(*queryTab); !ok || q.ed.Text() != "select * from customers" {
		t.Fatalf("no query tab with the block:\n%s", h.screen())
	}
	if len(h.fake.execs) != 1 || h.fake.execs[0] != "select * from customers" {
		t.Fatalf("execs %v", h.fake.execs)
	}
	h.prefix("A") // the same tab again, history intact
	h.want("list @customers")
}

func TestM3AssistantNeedsAKeyAndAsksBeforeWrites(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("A")
	h.typeText("hi")
	h.press("enter")
	h.want("needs an API key")
	h.a.ws().runSQL("delete from customers")
	h.want("changes data")
	if len(h.fake.execs) != 0 {
		t.Fatal("nothing may run before the question is answered")
	}
}

func TestCleanReplyStripsControlSequences(t *testing.T) {
	in := "ok\x1b]52;c;ZXZpbA==\x07 \x1b[2Jfine\r\nline\x00two\ttab"
	if got := cleanReply(in); got != "ok fine\nlinetwo\ttab" {
		t.Fatalf("%q", got)
	}
}

func TestAssistantFailureAfterQuestionDeleted(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	at := &assistantTab{ws: w, id: w.newID(), running: true}
	w.panes.chat = nil // the messages dialog deleted the only message
	at.sendFailed("hi", errors.New("boom"), false)
	if len(w.panes.chat) != 0 || at.running || at.in.Value() != "hi" {
		t.Fatalf("chat %v running %v input %q", w.panes.chat, at.running, at.in.Value())
	}
}

func TestM7ProviderPickerPerChat(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) {
		c.AI.Provider, c.AI.Model = "anthropic", "claude-x"
		c.AI.Providers = map[string]config.AIProvider{"local": {Provider: "openai", Model: "llama", BaseURL: "http://localhost:11434/v1"}}
	})
	h.connect()
	h.run("assistant")
	at := h.a.ws().tab().(*assistantTab)
	if at.aiCfg().Model != "claude-x" {
		t.Fatalf("default model %q", at.aiCfg().Model)
	}
	h.press("ctrl+p")
	p := h.a.overlays[len(h.a.overlays)-1].(*pickerDlg)
	if !strings.Contains(p.items[0].Label, "default") || p.items[1].Label != "local" {
		t.Fatalf("default comes first: %+v", p.items)
	}
	h.pickLabel("local")
	if c := at.aiCfg(); c.Model != "llama" || c.Provider != "openai" || at.Title() != "AI assistant · local" {
		t.Fatalf("cfg %+v title %q", c, at.Title())
	}
}
