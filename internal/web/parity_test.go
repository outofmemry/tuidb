package web

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/keymap"
	"webtuidb/internal/menus"
)

// M14 parity checks that read the web sources. They run from internal/web, so the repo root is two levels up.
var repoRoot = filepath.Join("..", "..")

// readTree concatenates the non-test files under dir with one of the extensions (node_modules and dist skipped).
func readTree(t *testing.T, dir string, exts ...string) string {
	t.Helper()
	var b strings.Builder
	filepath.WalkDir(filepath.Join(repoRoot, dir), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "dist") {
			return filepath.SkipDir
		}
		for _, e := range exts {
			if !d.IsDir() && strings.HasSuffix(p, e) && !strings.HasSuffix(p, "_test.go") {
				if c, err := os.ReadFile(p); err == nil {
					b.Write(c)
					b.WriteByte('\n')
				}
			}
		}
		return nil
	})
	if b.Len() == 0 {
		t.Fatalf("no %v files under %s", exts, dir)
	}
	return b.String()
}

func keymapActions() []string {
	seen := map[string]bool{}
	var out []string
	for _, b := range keymap.DefaultBindings() {
		if !seen[b.Action] {
			seen[b.Action] = true
			out = append(out, b.Action)
		}
	}
	return out
}

// onlyOneUI lists keymap actions a UI genuinely cannot answer, with the reason (also in the action's Desc).
// Empty today: every action is implemented in both UIs.
var onlyTUI = map[string]string{}
var onlyWeb = map[string]string{}

// TestParityActions (M14 check 1): SUPPORTED in Workspace.tsx is exactly the keymap's action set, and every action has a
// handler in the terminal UI and in the web sources.
func TestParityActions(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot, "web", "src", "routes", "Workspace.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	sup := SupportedActions(src)
	if len(sup) == 0 {
		t.Fatal("could not parse SUPPORTED out of web/src/routes/Workspace.tsx")
	}
	inSup := map[string]bool{}
	for _, a := range sup {
		inSup[a] = true
	}
	inKeys := map[string]bool{}
	for _, a := range keymapActions() {
		inKeys[a] = true
		if !inSup[a] && onlyTUI[a] == "" {
			t.Errorf("keymap action %q is missing from SUPPORTED in web/src/routes/Workspace.tsx (the help screen would grey it out)", a)
		}
	}
	for _, a := range sup {
		if !inKeys[a] && !strings.HasPrefix(a, "plugin:") {
			t.Errorf("SUPPORTED lists %q, which is not a keymap action (internal/keymap/action.go)", a)
		}
	}

	tui := readTree(t, "internal/ui", ".go")
	// the SUPPORTED set itself would satisfy a grep for the name, so take it out of the web sources
	web := supportedRe.ReplaceAllString(readTree(t, "web/src", ".ts", ".tsx"), "")
	for _, a := range keymapActions() {
		if strings.HasPrefix(a, "tab_") { // tab_1..tab_9: the terminal builds the name, the web matches /^tab_\d$/
			continue
		}
		if onlyWeb[a] == "" && !strings.Contains(tui, `"`+a+`"`) {
			t.Errorf("keymap action %q has no handler in internal/ui (no quoted %q)", a, a)
		}
		// handled as a quoted name (case 'a', a === 'a') or as an object key in a handler table (a: () => ...)
		if onlyTUI[a] == "" && !strings.Contains(web, "'"+a+"'") && !strings.Contains(web, `"`+a+`"`) &&
			!regexp.MustCompile(`(^|[^\w'"])`+a+`\s*:`).MatchString(web) && !regexp.MustCompile(`[,{]\s*`+a+`\s*[,}]`).MatchString(web) {
			t.Errorf("keymap action %q has no handler in web/src (no quoted %q and no handler-table key %s:)", a, a, a)
		}
	}
	if !strings.Contains(tui, `"tab_`) || !regexp.MustCompile(`tab_|\^tab_`).MatchString(web) {
		t.Errorf("the tab_N family is not handled in both UIs")
	}
}

// TestParityHelpListing (M14 check 2): `webtuidb -keys` and GET /api/meta carry the same bindings in the same order, and the
// web help dialog renders them without re-sorting.
func TestParityHelpListing(t *testing.T) {
	e := newEnv(t, "")
	meta := e.ok("GET", "/api/meta", nil)
	raw, _ := json.Marshal(meta["keymap"])
	var km struct {
		Prefix   string
		Bindings []keymap.Binding
	}
	if err := json.Unmarshal(raw, &km); err != nil {
		t.Fatal(err)
	}
	m, err := config.Defaults().KeymapWith(pluginBindings())
	if err != nil {
		t.Fatal(err)
	}
	want := keymap.Listing(km.Prefix, m)
	// the web help groups by context in order of first appearance, bindings in meta order
	var ctxs []string
	seen := map[string]bool{}
	for _, b := range km.Bindings {
		if !seen[b.Context] {
			seen[b.Context] = true
			ctxs = append(ctxs, b.Context)
		}
	}
	sort.SliceStable(ctxs, func(i, j int) bool { return ctxRank(ctxs[i]) < ctxRank(ctxs[j]) })
	var sb strings.Builder
	for _, c := range ctxs {
		for _, b := range km.Bindings {
			if b.Context == c {
				sb.WriteString(fmt.Sprintf("%s|%s|%s|%v|%s\n", c, b.Action, keymap.Display(b.Key), b.NoPrefix, b.Desc))
			}
		}
	}
	var wl strings.Builder
	for _, c := range ctxs {
		for _, b := range m.All() {
			if b.Context == c {
				wl.WriteString(fmt.Sprintf("%s|%s|%s|%v|%s\n", c, b.Action, keymap.Display(b.Key), b.NoPrefix, b.Desc))
			}
		}
	}
	if sb.String() != wl.String() {
		t.Errorf("GET /api/meta keymap differs from the engine keymap:\n--- meta\n%s--- keymap\n%s", sb.String(), wl.String())
	}
	if want == "" || !strings.Contains(want, "close_pane") {
		t.Errorf("keymap.Listing (webtuidb -keys) lacks close_pane:\n%s", want)
	}
	// every meta binding appears in the -keys text with its action, key and description, in the same relative order
	pos := 0
	for _, c := range []string{keymap.Global, keymap.Sidebar, keymap.Grid, keymap.FilterBar, keymap.Structure, keymap.Editor, keymap.Results} {
		for _, b := range km.Bindings {
			if b.Context != c {
				continue
			}
			k := keymap.Display(b.Key)
			if b.NoPrefix {
				k = "-n " + k
			}
			line := fmt.Sprintf("  %-26s %-10s %s\n", b.Action, k, b.Desc)
			i := strings.Index(want[pos:], line)
			if i < 0 {
				t.Errorf("webtuidb -keys lacks (or reorders) the binding %q %q in [%s]: %q", b.Action, b.Key, c, strings.TrimSpace(line))
				continue
			}
			pos += i + len(line)
		}
	}
	// the web help must not re-sort
	help := readTree(t, "web/src/routes", ".tsx")
	i := strings.Index(help, "function Help(")
	if i < 0 {
		t.Fatal("web/src/routes/Workspace.tsx has no Help component")
	}
	body := help[i:]
	if j := strings.Index(body, "\n}\n"); j > 0 {
		body = body[:j]
	}
	if strings.Contains(body, ".sort(") {
		t.Errorf("the web Help component re-sorts the bindings (.sort( found); it must render /api/meta in order")
	}
}

func ctxRank(c string) int {
	for i, x := range []string{keymap.Global, keymap.Sidebar, keymap.Grid, keymap.FilterBar, keymap.Structure, keymap.Editor, keymap.Results} {
		if x == c {
			return i
		}
	}
	return 99
}

// TestParityMenusFromMeta (M14 checks 4 and 5): the web builds every menu from meta.menus.<name> and keeps no list of its own.
func TestParityMenusFromMeta(t *testing.T) {
	web := readTree(t, "web/src", ".ts", ".tsx")
	// a few lists are reached through helpers (listOf(meta.menus, 'grid')), which is the same reference
	for name := range menus.All() {
		direct := strings.Contains(web, "menus."+name) || strings.Contains(web, "menus?."+name) || strings.Contains(web, "menus?.['"+name+"']")
		viaHelper := strings.Contains(web, "'"+name+"')") && strings.Contains(web, "listOf(")
		if !direct && !viaHelper {
			t.Errorf("menus.%s (internal/menus) is not referenced as meta.menus.%s in web/src: the web hardcodes that menu", name, name)
		}
	}
	conn := readTree(t, "web/src/routes", ".tsx")
	if !strings.Contains(conn, "menus?.welcome") && !strings.Contains(conn, "menus.welcome") {
		t.Errorf("web/src/routes/Connections.tsx does not build its menu from meta.menus.welcome")
	}
	e := newEnv(t, "")
	meta := e.ok("GET", "/api/meta", nil)
	got, _ := meta["menus"].(map[string]any)
	for name := range menus.All() {
		if _, ok := got[name]; !ok {
			t.Errorf("GET /api/meta menus lacks the list %q", name)
		}
	}
}
