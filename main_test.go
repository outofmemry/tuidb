package main

import (
	"encoding/json"
	"flag"
	"strings"
	"testing"
	"webtuidb/internal/config"
)

func TestMCPSnippet(t *testing.T) {
	for _, c := range []string{"claude-code", "cursor", "vscode"} {
		s, file, err := mcpSnippet(c, `/opt/my dir/webtuidb`, "--config", "/etc/w.toml")
		var v map[string]map[string]struct {
			Command string
			Args    []string
		}
		if err != nil || file == "" || json.Unmarshal([]byte(s), &v) != nil {
			t.Fatalf("%s: %q %v", c, s, err)
		}
		for _, srv := range v {
			if e := srv["webtuidb"]; e.Command != "/opt/my dir/webtuidb" || strings.Join(e.Args, " ") != "--mcp --config /etc/w.toml" {
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

func TestMCPSnippetDefaultArgs(t *testing.T) {
	if s, _, _ := mcpSnippet("codex", "/x"); !strings.Contains(s, `args = ["--mcp"]`) {
		t.Fatalf("%q", s)
	}
}

func TestCheckModes(t *testing.T) {
	if err := checkModes(map[string]bool{"mcp": true, "frontend": true}, 0); err == nil || !strings.Contains(err.Error(), "--frontend and --mcp") {
		t.Fatalf("%v", err)
	}
	if err := checkModes(map[string]bool{"keys": true, "frontend": true}, 0); err == nil {
		t.Fatal("keys+frontend accepted")
	}
	if err := checkModes(map[string]bool{"mcp": true}, 1); err == nil {
		t.Fatal("--mcp extra accepted")
	}
	if err := checkModes(map[string]bool{"mcp": false}, 1); err != nil {
		t.Fatal(err)
	}
	if err := checkModes(map[string]bool{"version": true}, 0); err != nil {
		t.Fatal(err)
	}
}

func TestOverlayURL(t *testing.T) {
	saved := config.Connection{Name: "Prod", Driver: "postgres", Host: "prod.db", Port: 5432, User: "app", Password: "pw", Database: "main", Tag: "prod", SafeMode: "strict"}
	u, err := config.ParseURL("postgresql://bob:sec@sandbox.db:6000/sbx?name=Prod")
	if err != nil {
		t.Fatal(err)
	}
	m := overlayURL(saved, u)
	if m.Host != "sandbox.db" || m.Port != 6000 || m.User != "bob" || m.Password != "sec" || m.Database != "sbx" || m.Tag != "prod" || m.SafeMode != "strict" || m.Name != "Prod" {
		t.Fatalf("%+v", m)
	}
	if saved.Host != "prod.db" {
		t.Fatal("saved mutated")
	}
}
