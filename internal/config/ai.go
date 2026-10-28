package config

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// AIFor returns the default assistant settings for name "", else the [ai.providers.<name>] entry
// merged over the defaults (empty fields fall back). Unknown names are an error.
func AIFor(cfg AI, name string) (AI, error) {
	def := cfg
	def.Providers = nil
	if name == "" {
		return def, nil
	}
	p, ok := cfg.Providers[name]
	if !ok {
		return AI{}, fmt.Errorf("unknown ai provider %q", name)
	}
	if p.Provider != "" && !validAIProvider(p.Provider) {
		return AI{}, fmt.Errorf("ai.providers.%s: provider %q is not one of anthropic|openai", name, p.Provider)
	}
	if p.BaseURL != "" && p.Provider == "" { // a custom endpoint must not silently inherit the default provider's wire format
		u, err := url.Parse(p.BaseURL)
		if err != nil || u.Host == "" {
			return AI{}, fmt.Errorf("ai.providers.%s: bad base_url", name)
		}
		switch {
		case strings.Contains(u.Path, "/v1") || !strings.HasSuffix(strings.ToLower(u.Hostname()), "anthropic.com"):
			p.Provider = "openai"
		default:
			return AI{}, fmt.Errorf("ai.providers.%s: base_url is set without provider; add provider = \"anthropic\" or \"openai\"", name)
		}
	}
	for _, f := range []struct {
		dst *string
		src string
	}{
		{&def.Provider, p.Provider}, {&def.Model, p.Model}, {&def.APIKeyCmd, p.APIKeyCmd}, {&def.APIKey, p.APIKey}, {&def.BaseURL, p.BaseURL},
	} {
		if f.src != "" {
			*f.dst = f.src
		}
	}
	if p.APIKey != "" && p.APIKeyCmd == "" {
		def.APIKeyCmd = "" // the provider's own literal key must not be shadowed by the default's command
	}
	if p.APIKeyCmd != "" && p.APIKey == "" {
		def.APIKey = ""
	}
	return def, nil
}

func validAIProvider(p string) bool { return p == "anthropic" || p == "openai" }

// AIProviderNames lists the named providers, sorted.
func AIProviderNames(cfg AI) []string {
	out := make([]string, 0, len(cfg.Providers))
	for n := range cfg.Providers {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
