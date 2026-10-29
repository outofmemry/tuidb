package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"webtuidb/internal/config"
)

func TestAssistant(t *testing.T) {
	var sys string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 1<<16)
		n, _ := r.Body.Read(b)
		sys = string(b[:n])
		w.Write([]byte(`{"choices":[{"message":{"content":"Try:\n` + "```sql\\nselect 1\\n```" + `"}}]}`))
	}))
	defer llm.Close()
	e := newEnvCfg(t, "", func(c *config.Config) {
		c.AI = config.AI{Provider: "openai", Model: "m", APIKey: "k", BaseURL: llm.URL}
	})
	id := e.open()
	r := e.ok("POST", "/api/sessions/"+id+"/assistant", map[string]any{"messages": []map[string]string{{"role": "user", "content": "count rows in @t1"}}})
	bl := r["blocks"].([]any)
	if len(bl) != 1 || bl[0].(map[string]any)["sql"] != "select 1" || bl[0].(map[string]any)["readOnly"] != true || !strings.Contains(sys, "CREATE TABLE t") {
		t.Fatalf("assistant: %v / %s", r, sys)
	}
	e2 := newEnv(t, "")
	id2 := e2.open()
	if code, _ := e2.call("POST", "/api/sessions/"+id2+"/assistant", map[string]any{"messages": []map[string]string{{"role": "user", "content": "hi"}}}, "tok"); code != 400 {
		t.Fatalf("no key must be a clear 400: %d", code)
	}
}

func TestAssistantModelsAndBoardDuplicate(t *testing.T) {
	prov := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "no", 404)
			return
		}
		io.WriteString(w, `{"data":[{"id":"b"},{"id":"a"}]}`)
	}))
	defer prov.Close()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e, _ := richEnv(t, func(c *config.Config) {
		c.AI = config.AI{Provider: "openai", APIKey: "k", BaseURL: prov.URL, Model: "a"}
	})
	m := e.ok("GET", "/api/assistant/models", nil)
	if l := m["models"].([]any); len(l) != 2 || l[0] != "a" || m["current"] != "a" {
		t.Fatalf("models = %v", m)
	}
	e.ok("POST", "/api/boards", map[string]any{"Name": "b1", "Query": "select 1 as x", "Kind": "bar", "X": "x", "Y": "x"})
	e.ok("POST", "/api/boards/duplicate", map[string]any{"Name": "b1", "NewName": "b2"})
	if code, _ := e.call("POST", "/api/boards/duplicate", map[string]any{"Name": "b1", "NewName": "b2"}, "tok"); code != 400 {
		t.Fatalf("an existing board must not be overwritten: %d", code)
	}
	resp := e.raw("GET", "/api/boards", nil, nil)
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), `"b2"`) {
		t.Fatalf("boards = %s", b)
	}
}

func TestAssistantModelsDropsKeyOnCrossHostRedirect(t *testing.T) {
	var leaked string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("x-api-key")
		io.WriteString(w, `{"data":[{"id":"a"}]}`)
	}))
	defer other.Close()
	prov := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/v1/models", http.StatusFound)
	}))
	defer prov.Close()
	e, _ := richEnv(t, func(c *config.Config) {
		c.AI = config.AI{Provider: "anthropic", APIKey: "secret", BaseURL: prov.URL}
	})
	e.ok("GET", "/api/assistant/models", nil)
	if leaked != "" {
		t.Fatalf("x-api-key leaked to another host: %q", leaked)
	}
}

func TestAssistantProvider(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) {
		c.AI.Providers = map[string]config.AIProvider{"local": {Provider: "openai", BaseURL: "http://127.0.0.1:1/v1"}}
	})
	if m := e.ok("GET", "/api/meta", nil); m["ai"].(map[string]any)["providers"].([]any)[0] != "local" {
		t.Fatalf("meta ai = %v", m["ai"])
	}
	id := e.open()
	msgs := []map[string]any{{"role": "user", "content": "hi"}}
	if code, out := e.call("POST", "/api/sessions/"+id+"/assistant", map[string]any{"messages": msgs, "provider": "nope"}, "tok"); code != 400 || !strings.Contains(out["error"].(string), "unknown ai provider") {
		t.Fatalf("unknown provider: %d %v", code, out)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/assistant", map[string]any{"messages": msgs, "provider": "local"}, "tok"); code != 400 { // reaches the (dead) endpoint, which fails as a 400
		t.Fatalf("known provider: %d", code)
	}
}
