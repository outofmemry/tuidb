package config

import "testing"

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
