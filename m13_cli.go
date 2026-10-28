package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/x/term"

	"webtuidb/internal/config"
)

const (
	releasesURL   = "https://github.com/outofmemry/webtuidb/releases"
	latestAPIURL  = "https://api.github.com/repos/outofmemry/webtuidb/releases/latest"
	updateTimeout = 5 * time.Second
)

// openURL opens a URL in the default browser; the caller prints the URL when this fails.
func openURL(u string) error {
	name, args := "xdg-open", []string{u}
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", u}
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap the opener so it does not linger as a zombie until exit
	return nil
}

// gitTimeout bounds one git clone/pull so a stalled remote cannot hang the CLI.
var gitTimeout = 2 * time.Minute

// runGit runs git with a timeout and no credential prompt (there is no terminal to answer it).
func runGit(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		err = fmt.Errorf("timed out after %s", gitTimeout)
	}
	return out, err
}

// changelog implements -changelog.
func changelog(w io.Writer) {
	if err := openURL(releasesURL); err != nil {
		fmt.Fprintln(w, "could not open a browser:", err)
	}
	fmt.Fprintln(w, releasesURL)
}

var secretLine = regexp.MustCompile(`(?i)^(\s*(?:[\w.-]*(?:password|passcode|api_key|secret|token)[\w.-]*|value)\s*=\s*)(.+)$`)

var (
	secretKey = regexp.MustCompile(`(?i)(password|passcode|api_key|secret|token|^value$)`)
	urlUser   = regexp.MustCompile(`(://[^:/@\s]*):[^@\s]*@`)
)

func isSecretKey(k string) bool {
	return secretKey.MatchString(k) && !strings.HasSuffix(strings.ToLower(k), "_cmd") // *_cmd holds a command, not a secret
}

// maskValue blanks secret keys at any depth of a decoded TOML value and the password of any URL.
func maskValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			switch e.(type) {
			case map[string]any, []map[string]any:
				x[k] = maskValue(e)
			default:
				if isSecretKey(k) {
					x[k] = "***"
				} else {
					x[k] = maskValue(e)
				}
			}
		}
		return x
	case []map[string]any:
		for i := range x {
			maskValue(x[i])
		}
		return x
	case []any:
		for i := range x {
			x[i] = maskValue(x[i])
		}
		return x
	case string:
		return urlUser.ReplaceAllString(x, "$1:***@")
	}
	return v
}

// maskSecrets hides the value of secret-looking keys; *_cmd keys hold a command, not a secret, and stay.
// It decodes the TOML so inline tables, quoted keys and multi-line strings are covered at any depth, and URL
// passwords go too; the text is then re-encoded (comments are lost). A file that does not decode falls back to the
// line-based pass, which also runs over the re-encoded text as a second net.
func maskSecrets(src string) string {
	var m map[string]any
	if _, err := toml.Decode(src, &m); err == nil {
		var b bytes.Buffer
		if err := toml.NewEncoder(&b).Encode(maskValue(m)); err == nil {
			src = b.String()
		}
	} else {
		src = urlUser.ReplaceAllString(src, "$1:***@")
	}
	return maskLines(src)
}

// maskLines is the line-based pass: it blanks `key = value` lines whose key looks secret.
func maskLines(toml string) string {
	lines := strings.Split(toml, "\n")
	out := lines[:0:0]
	skip := "" // closing delimiter of a masked multi-line string whose body is being dropped
	for _, l := range lines {
		if skip != "" {
			if strings.Contains(l, skip) {
				skip = ""
			}
			continue
		}
		m := secretLine.FindStringSubmatch(l)
		if m == nil {
			out = append(out, l)
			continue
		}
		if key := strings.ToLower(strings.TrimSpace(strings.SplitN(l, "=", 2)[0])); strings.HasSuffix(key, "_cmd") {
			out = append(out, l)
			continue
		}
		out = append(out, m[1]+`"***"`)
		// a secret opening """ or ''' continues on the next lines: drop them too, or the secret leaks
		for _, d := range []string{`"""`, "'''"} {
			if v := strings.TrimSpace(m[2]); strings.HasPrefix(v, d) && !strings.Contains(v[len(d):], d) {
				skip = d
			}
		}
	}
	return strings.Join(out, "\n")
}

// bugReport implements -bugreport: nothing is sent anywhere, the user pastes it into an issue.
func bugReport(w io.Writer, version, cfgPath string) {
	fmt.Fprintf(w, "webtuidb %s\nos/arch: %s/%s\ngo: %s\nTERM: %s\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version(), os.Getenv("TERM"))
	var drivers []string
	cfg, _ := config.Load(cfgPath) // best effort: a broken config is exactly when a bug report is wanted
	if conns, err := config.LoadAllConnections(config.ConnectionsPath(), cfg.Sources); err == nil {
		for _, c := range conns {
			if !slices.Contains(drivers, c.Driver) {
				drivers = append(drivers, c.Driver)
			}
		}
	}
	slices.Sort(drivers)
	fmt.Fprintf(w, "drivers in use: %s\n\n--- %s (secrets masked)\n", strings.Join(drivers, ", "), cfgPath)
	if b, err := os.ReadFile(cfgPath); err != nil {
		fmt.Fprintln(w, "(no config file)")
	} else {
		fmt.Fprintln(w, maskSecrets(string(b)))
	}
}

// versionNums turns "v1.2.3" / "1.2" into [1 2 3]; ok is false for anything else (such as "dev").
func versionNums(v string) (nums []int, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		nums = append(nums, n)
	}
	return nums, len(nums) > 0
}

// newer reports whether tag is a later release than current.
func newer(tag, current string) bool {
	a, ok1 := versionNums(tag)
	b, ok2 := versionNums(current)
	if !ok1 || !ok2 {
		return false
	}
	for len(a) < len(b) {
		a = append(a, 0)
	}
	for len(b) < len(a) {
		b = append(b, 0)
	}
	return slices.Compare(a, b) > 0
}

// checkUpdate implements -check-update (never automatic): one GET, 5 s timeout, a sentence back.
func checkUpdate(client *http.Client, url, current string) (string, error) {
	client.Timeout = updateTimeout
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("check-update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("check-update: %s", resp.Status)
	}
	var rel struct {
		Tag string `json:"tag_name"`
		URL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil || rel.Tag == "" {
		return "", errors.New("check-update: unexpected response")
	}
	switch {
	case newer(rel.Tag, current):
		return fmt.Sprintf("a newer version is available: %s (you have %s)\n%s", rel.Tag, current, releasesURL), nil
	case versionsKnown(rel.Tag, current):
		return fmt.Sprintf("you are up to date (%s, latest %s)", current, rel.Tag), nil
	}
	return fmt.Sprintf("latest release is %s; this build is %q and cannot be compared\n%s", rel.Tag, current, releasesURL), nil
}

func versionsKnown(a, b string) bool {
	_, ok1 := versionNums(a)
	_, ok2 := versionNums(b)
	return ok1 && ok2
}

// setPasscode implements -set-passcode: reads the passcode twice without echo and prints the config lines.
func setPasscode(w io.Writer) error {
	fd := os.Stdin.Fd()
	if !term.IsTerminal(fd) {
		return errors.New("set-passcode needs a terminal to read the passcode from")
	}
	read := func(prompt string) (string, error) {
		fmt.Fprint(os.Stderr, prompt)
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	p1, err := read("New passcode: ")
	if err != nil {
		return err
	}
	p2, err := read("Repeat it: ")
	if err != nil {
		return err
	}
	if p1 != p2 {
		return errors.New("the passcodes differ; nothing was changed")
	}
	h, err := config.HashPasscode(p1)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Put this in %s:\n", filepath.Join(config.ConfigDir(), "config.toml"))
	fmt.Fprintf(w, "[security]\npasscode = %q\n", h)
	return nil
}

// splitRef splits "https://host/x/y.git@v1" into the URL and the ref; an @ inside user@host is not a ref.
func splitRef(arg string) (url, ref string) {
	if i := strings.LastIndex(arg, "@"); i > 0 && !strings.ContainsAny(arg[i+1:], ":/") {
		return arg[:i], arg[i+1:]
	}
	return arg, ""
}

var themeNameRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// themeNameOf is the folder name for a git URL: its last path element without ".git".
func themeNameOf(url string) string {
	n := strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	n = n[strings.LastIndexAny(n, "/:")+1:]
	n = strings.Trim(themeNameRe.ReplaceAllString(n, "-"), "-.")
	return n
}

// themeInstall implements -theme-install: git clone into the themes dir; the repo must hold a theme.toml.
func themeInstall(w io.Writer, arg string) error {
	url, ref := splitRef(arg)
	name := themeNameOf(url)
	if name == "" {
		return fmt.Errorf("cannot derive a theme name from %q", arg)
	}
	dest := filepath.Join(config.ThemesDir(), name)
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("theme %q is already installed (%s); use -theme-update", name, dest)
	}
	if err := os.MkdirAll(config.ThemesDir(), 0o700); err != nil {
		return err
	}
	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, "--", url, dest)
	if out, err := runGit(args...); err != nil {
		os.RemoveAll(dest)
		return fmt.Errorf("git clone failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dest, "theme.toml")); err != nil {
		os.RemoveAll(dest)
		return fmt.Errorf("%s has no theme.toml at its top level; nothing was installed", url)
	}
	fmt.Fprintf(w, "installed theme %q; set [theme] name = %q\n", name, name)
	return nil
}

// themeUpdate implements -theme-update: git pull in every installed theme that is a git checkout.
func themeUpdate(w io.Writer) error {
	ents, _ := os.ReadDir(config.ThemesDir())
	var failed []string
	n := 0
	for _, e := range ents {
		dir := filepath.Join(config.ThemesDir(), e.Name())
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			continue
		}
		n++
		if out, err := runGit("-C", dir, "pull", "--ff-only"); err != nil {
			failed = append(failed, e.Name())
			fmt.Fprintf(w, "%s: %v\n%s", e.Name(), err, out)
		} else {
			fmt.Fprintf(w, "%s: %s", e.Name(), out)
		}
	}
	if n == 0 {
		fmt.Fprintln(w, "no git-installed themes in", config.ThemesDir())
	}
	if len(failed) > 0 {
		return fmt.Errorf("could not update: %s", strings.Join(failed, ", "))
	}
	return nil
}
