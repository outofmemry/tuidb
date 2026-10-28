package config

import (
	"strings"
	"syscall"
	"testing"
)

func TestAIFor(t *testing.T) {
	a := AI{Provider: "anthropic", Model: "m", APIKeyCmd: "pass x", Providers: map[string]AIProvider{
		"local": {Provider: "openai", BaseURL: "http://localhost:11434/v1", APIKey: "k"}}}
	d, err := AIFor(a, "")
	if err != nil || d.Model != "m" || d.Providers != nil {
		t.Fatalf("%v %+v", err, d)
	}
	l, err := AIFor(a, "local")
	if err != nil || l.Provider != "openai" || l.Model != "m" || l.APIKey != "k" || l.APIKeyCmd != "" {
		t.Fatalf("%v %+v", err, l)
	}
	if _, err := AIFor(a, "nope"); err == nil {
		t.Fatal("unknown name must fail")
	}
	if n := AIProviderNames(a); len(n) != 1 || n[0] != "local" {
		t.Fatal(n)
	}
}

func TestAIForBaseURLInfersProvider(t *testing.T) {
	a := AI{Provider: "anthropic", Providers: map[string]AIProvider{
		"ollama": {BaseURL: "http://localhost:11434/v1"},
		"proxy":  {BaseURL: "https://llm.example.com"},
		"amb":    {BaseURL: "https://api.anthropic.com"},
	}}
	for _, n := range []string{"ollama", "proxy"} {
		if p, err := AIFor(a, n); err != nil || p.Provider != "openai" {
			t.Fatalf("%s: %v %q", n, err, p.Provider)
		}
	}
	if _, err := AIFor(a, "amb"); err == nil || !strings.Contains(err.Error(), "amb") {
		t.Fatalf("%v", err)
	}
}

func TestChmodTolerable(t *testing.T) {
	if chmodTolerable("/x/y", 0o600, syscall.EPERM) != true || chmodTolerable("/x/y", 0o644, syscall.EACCES) {
		t.Fatal("tolerable rules")
	}
	if !chmodTolerable("/nonexistent-dir/y", 0o644, syscall.EROFS) {
		t.Fatal("unwritable dir should be tolerated")
	}
	if chmodTolerable(t.TempDir()+"/y", 0o644, syscall.EPERM) {
		t.Fatal("world-readable file in a writable dir must stay fatal")
	}
}
