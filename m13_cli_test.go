package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/config"
)

func TestMaskSecrets(t *testing.T) {
	in := "[web]\npassword = \"hunter2\"\nfont_size = 14\n[ai]\napi_key = \"sk-1\"\napi_key_cmd = \"op read x\"\n[[mcp.token]]\nvalue = \"abc\"\n[security]\npasscode = \"$2a$10$xyz\"\n"
	out := maskSecrets(in)
	for _, leak := range []string{"hunter2", "sk-1", "abc", "$2a$"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q leaked:\n%s", leak, out)
		}
	}
	for _, keep := range []string{"font_size = 14", `api_key_cmd = "op read x"`, `password = "***"`} {
		if !strings.Contains(out, keep) {
			t.Errorf("lost %q:\n%s", keep, out)
		}
	}
}

func TestBugReport(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	p := filepath.Join(dir, "c.toml")
	os.WriteFile(p, []byte("[web]\npassword = \"pw\"\n"), 0o600)
	var b strings.Builder
	bugReport(&b, "v9", p)
	if !strings.Contains(b.String(), "webtuidb v9") || strings.Contains(b.String(), "\"pw\"") || !strings.Contains(b.String(), "***") {
		t.Fatal(b.String())
	}
}

func TestCheckUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v1.10.0","html_url":"x"}`))
	}))
	defer srv.Close()
	for current, want := range map[string]string{"v1.9.3": "newer version", "v1.10.0": "up to date", "1.11": "up to date", "dev": "cannot be compared"} {
		got, err := checkUpdate(&http.Client{}, srv.URL, current)
		if err != nil || !strings.Contains(got, want) {
			t.Errorf("%s: %q %v, want %q", current, got, err, want)
		}
	}
	bad := httptest.NewServer(http.NotFoundHandler())
	defer bad.Close()
	if _, err := checkUpdate(&http.Client{}, bad.URL, "v1"); err == nil {
		t.Error("404 must be an error")
	}
}

func TestThemeNameAndRef(t *testing.T) {
	for in, want := range map[string][2]string{
		"https://github.com/a/nord.git@v2": {"https://github.com/a/nord.git", "v2"},
		"git@github.com:a/nord.git":        {"git@github.com:a/nord.git", ""},
		"git@github.com:a/nord.git@main":   {"git@github.com:a/nord.git", "main"},
		"https://github.com/a/nord":        {"https://github.com/a/nord", ""},
	} {
		if u, r := splitRef(in); u != want[0] || r != want[1] {
			t.Errorf("splitRef(%q) = %q, %q", in, u, r)
		}
	}
	if n := themeNameOf("git@github.com:a/Nord Dark.git"); n != "Nord-Dark" {
		t.Errorf("name %q", n)
	}
}

func TestThemeInstallAndUpdate(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	repo := filepath.Join(t.TempDir(), "mytheme")
	os.MkdirAll(repo, 0o755)
	os.WriteFile(filepath.Join(repo, "theme.toml"), []byte("[theme.colors]\n"), 0o644)
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	var b strings.Builder
	if err := themeInstall(&b, repo); err != nil {
		t.Fatal(err)
	}
	if got := config.InstalledThemes(); len(got) != 1 || got[0] != "mytheme" {
		t.Fatalf("installed %v", got)
	}
	if err := themeInstall(&b, repo); err == nil {
		t.Error("second install must fail")
	}
	if err := themeUpdate(&b); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(t.TempDir(), "plain")
	os.MkdirAll(empty, 0o755)
	exec.Command("git", "-C", empty, "init", "-q").Run()
	exec.Command("git", "-C", empty, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-qm", "x").Run()
	if err := themeInstall(&b, empty); err == nil || len(config.InstalledThemes()) != 1 {
		t.Errorf("a repo without theme.toml must not install: %v", err)
	}
}

// R-012: inline tables, quoted keys, multi-line strings and URL passwords are masked too.
func TestMaskSecretsDeep(t *testing.T) {
	in := "[ai]\nproviders = { x = { api_key = \"sk-inline\" }, y = { base_url = \"https://user:pw-url@host/v1\" } }\n" +
		"\"api_key\" = \"sk-quoted\"\n[web]\npassword = \"\"\"\nmulti-line-secret\n\"\"\"\nfont_size = 14\n[[mcp.token]]\nname = \"t\"\nvalue = \"tok-1\"\n"
	out := maskSecrets(in)
	for _, leak := range []string{"sk-inline", "pw-url", "sk-quoted", "multi-line-secret", "tok-1"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q leaked:\n%s", leak, out)
		}
	}
	for _, keep := range []string{"font_size = 14", "user:***@host"} {
		if !strings.Contains(out, keep) {
			t.Errorf("lost %q:\n%s", keep, out)
		}
	}
}

// R-003: the update check and changelog point at the repo the README installs from.
func TestReleaseOwnerMatchesREADME(t *testing.T) {
	b, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`brew install[^\n]*?([\w-]+)/tap/webtuidb`).FindSubmatch(b)
	if m == nil {
		t.Fatal("README has no brew tap line")
	}
	owner := string(m[1])
	for _, u := range []string{releasesURL, latestAPIURL} {
		if !strings.Contains(u, "/"+owner+"/webtuidb/") {
			t.Errorf("%s is not under %s", u, owner)
		}
	}
}

// P2-60: a config that does not decode falls back to the line pass, which must not leak a multi-line secret.
func TestMaskSecretsBrokenMultiline(t *testing.T) {
	in := "[web]\npassword = \"\"\"\nline-one-secret\nline-two-secret\n\"\"\"\nfont_size = 14\nbroken = = =\napi_key = '''\nsk-hidden\n'''\n[x]\n"
	out := maskSecrets(in)
	for _, leak := range []string{"line-one-secret", "line-two-secret", "sk-hidden"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q leaked:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, "font_size = 14") || !strings.Contains(out, `password = "***"`) {
		t.Errorf("over-masked:\n%s", out)
	}
}

// P2-61: a stalled git remote times out instead of hanging the CLI.
func TestRunGitTimesOut(t *testing.T) {
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nsleep 30\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := gitTimeout
	gitTimeout = 200 * time.Millisecond
	defer func() { gitTimeout = old }()
	start := time.Now()
	if _, err := runGit("clone", "x"); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("did not time out")
	}
}
