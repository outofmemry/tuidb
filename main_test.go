package main

import (
	"encoding/json"
	"flag"
	"strings"
	"testing"
)

func TestMCPSnippet(t *testing.T) {
	for _, c := range []string{"claude-code", "cursor", "vscode"} {
		s, file, err := mcpSnippet(c, `/opt/my dir/webtuidb`)
		var v map[string]map[string]struct {
			Command string
			Args    []string
		}
		if err != nil || file == "" || json.Unmarshal([]byte(s), &v) != nil {
			t.Fatalf("%s: %q %v", c, s, err)
		}
		for _, srv := range v {
			if e := srv["webtuidb"]; e.Command != "/opt/my dir/webtuidb" || len(e.Args) != 1 || e.Args[0] != "--mcp" {
				t.Fatalf("%s: %+v", c, e)
			}
		}
	}
	if s, _, _ := mcpSnippet("codex", "/x"); !strings.Contains(s, "[mcp_servers.webtuidb]") || !strings.Contains(s, `command = "/x"`) {
		t.Fatalf("codex: %q", s)
	}
	if _, _, err := mcpSnippet("emacs", "/x"); err == nil {
		t.Fatal("unknown client accepted")
	}
}

func TestCompletionScript(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.Bool("keys", false, "print [keys] and exit")
	fs.String("config", "", "path to config.toml")
	fs.String("mcp-config", "", "client")
	for _, shell := range []string{"bash", "zsh", "fish"} {
		s, err := completionScript(shell, fs)
		if err != nil || !strings.Contains(s, "keys") || !strings.Contains(s, "mcp-config") || !strings.Contains(s, "claude-code") || !strings.Contains(s, "-list-connections") {
			t.Fatalf("%s: %q %v", shell, s, err)
		}
	}
	if _, err := completionScript("tcsh", fs); err == nil {
		t.Fatal("unknown shell accepted")
	}
}

func TestVersionAndMan(t *testing.T) {
	if versionString() == "" || !strings.HasPrefix(manPage, ".TH WEBTUIDB") {
		t.Fatalf("version %q, man %.20q", versionString(), manPage)
	}
}
