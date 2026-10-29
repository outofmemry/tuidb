package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"webtuidb/internal/ai"
	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func (s *Server) assistant(ss *session, r *http.Request) (any, error) {
	var req struct {
		Messages []ai.Message
		Model    string // overrides [ai] model for this call (the picker)
		Provider string // a [ai.providers.<name>]; "" = the default
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if len(req.Messages) == 0 || req.Messages[len(req.Messages)-1].Role != "user" {
		return nil, bad("the last message must be from the user")
	}
	cfg, err := config.AIFor(s.o.Cfg.AI, req.Provider)
	if err != nil {
		return nil, bad("%v", err)
	}
	key, err := config.ResolveAIKey(cfg)
	if err != nil {
		return nil, bad("%v", err)
	}
	if key == "" && (cfg.Provider != "openai" || cfg.BaseURL == "") { // a local OpenAI-compatible server (Ollama) needs no key
		return nil, bad("no AI key: set [ai] api_key or api_key_cmd in the config")
	}
	sess := ss.o.Sess
	ddl := map[string]string{}
	var names []string
	for _, m := range req.Messages {
		if m.Role == "user" {
			names = append(names, ai.Mentions(m.Content)...)
		}
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	for _, n := range names {
		ref := db.ObjRef{Schema: sess.CurrentSchema(), Name: n, Kind: db.KindTable}
		if i := strings.Index(n, "."); i > 0 {
			ref.Schema, ref.Name = n[:i], n[i+1:]
		}
		if t, err := sess.DDL(ctx, ref); err == nil { // an unknown mention is just prose
			ddl[n] = t
		}
	}
	if req.Model != "" {
		cfg.Model = req.Model
	}
	reply, err := ai.New(cfg, key).Chat(ctx, ai.SystemPrompt(sess.Caps().Dialect, sess.CurrentDatabase(), ddl), req.Messages)
	if err != nil {
		return nil, bad("%v", err)
	}
	dialect := sess.Caps().Dialect
	type block struct {
		SQL      string `json:"sql"`
		ReadOnly bool   `json:"readOnly"`
	}
	blocks := []block{}
	for _, b := range ai.SQLBlocks(reply) {
		blocks = append(blocks, block{b, db.IsReadOnly(dialect, b)})
	}
	return map[string]any{"reply": reply, "blocks": blocks}, nil
}

// assistantModels asks the configured provider which models it offers (OpenAI-compatible GET /models,
// Anthropic GET /v1/models), so the picker lists real names. The model chosen travels with the next /assistant call.
func (s *Server) assistantModels(r *http.Request) (any, error) {
	cfg, err := config.AIFor(s.o.Cfg.AI, r.URL.Query().Get("provider"))
	if err != nil {
		return nil, bad("%v", err)
	}
	key, err := config.ResolveAIKey(cfg)
	if err != nil {
		return nil, bad("%v", err)
	}
	if key == "" && (cfg.Provider != "openai" || cfg.BaseURL == "") {
		return nil, bad("no AI key: set [ai] api_key or api_key_cmd in the config")
	}
	var u string
	hdr := http.Header{}
	if cfg.Provider == "openai" {
		base := cfg.BaseURL
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		u = strings.TrimRight(base, "/") + "/models"
		if key != "" {
			hdr.Set("Authorization", "Bearer "+key)
		}
	} else {
		base := cfg.BaseURL
		if base == "" {
			base = "https://api.anthropic.com"
		}
		u = strings.TrimRight(base, "/") + "/v1/models?limit=100"
		hdr.Set("x-api-key", key)
		hdr.Set("anthropic-version", "2023-06-01")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, bad("%v", err)
	}
	req.Header = hdr
	// the key must not follow a redirect to another host
	client := &http.Client{CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if next.URL.Host != via[0].URL.Host {
			next.Header = http.Header{}
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, bad("%v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, bad("the provider answered %s: %.200s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var out struct{ Data []struct{ ID string } }
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, bad("unexpected answer from the provider: %.200s", raw)
	}
	ids := []string{}
	for _, m := range out.Data {
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	return map[string]any{"provider": cfg.Provider, "current": cfg.Model, "models": ids}, nil
}
