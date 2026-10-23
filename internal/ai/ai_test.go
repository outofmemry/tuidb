package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"webtuidb/internal/config"
)

func TestHelpers(t *testing.T) {
	if got := Mentions("join @orders with @app.settings and @orders, mail@x"); !reflect.DeepEqual(got, []string{"orders", "app.settings", "x"}) {
		t.Errorf("mentions %v", got) // "mail@x" is a known false positive
	}
	if got := SQLBlocks("a\n```sql\nSELECT 1;\n```\nb\n```sql\nSELECT 2\n```"); !reflect.DeepEqual(got, []string{"SELECT 1;", "SELECT 2"}) {
		t.Errorf("blocks %v", got)
	}
	if p := SystemPrompt("postgres", "app", map[string]string{"orders": "CREATE TABLE orders(id int)"}); !strings.Contains(p, "CREATE TABLE orders(id int)") || !strings.Contains(p, "postgres") {
		t.Errorf("prompt %q", p)
	}
}

func TestOpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer k" || !strings.Contains(string(b), `"model":"m"`) || !strings.Contains(string(b), "hi") {
			t.Errorf("bad request %s %s %s", r.URL.Path, r.Header.Get("Authorization"), b)
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "yo"}}}})
	}))
	defer srv.Close()
	c := New(config.AI{Provider: "openai", Model: "m", BaseURL: srv.URL + "/v1"}, "k")
	got, err := c.Chat(context.Background(), "sys", []Message{{"user", "hi"}})
	if err != nil || got != "yo" {
		t.Errorf("%q %v", got, err)
	}
}

func TestAnthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v1/messages" || !strings.Contains(string(b), `"model":"claude-x"`) || !strings.Contains(string(b), "sys") {
			t.Errorf("bad request %s %s", r.URL.Path, b)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-x","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()
	c := New(config.AI{Provider: "anthropic", Model: "claude-x", BaseURL: srv.URL}, "k")
	got, err := c.Chat(context.Background(), "sys", []Message{{"user", "hi"}})
	if err != nil || got != "hello" {
		t.Errorf("%q %v", got, err)
	}
}

func TestOpenAIParts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/chat/completions" || strings.Count(string(b), `"role"`) != 2 { // system + one non-empty message
			t.Errorf("bad request %s %s", r.URL.Path, b)
		}
		io.WriteString(w, `{"choices":[{"message":{"content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}}]}`)
	}))
	defer srv.Close()
	c := New(config.AI{Provider: "openai", Model: "m", BaseURL: srv.URL}, "")
	got, err := c.Chat(context.Background(), "sys", []Message{{"user", "hi"}, {"assistant", " "}})
	if err != nil || got != "ab" {
		t.Errorf("%q %v", got, err)
	}
}
