package web

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/keymap"
	"webtuidb/internal/menus"
)

// Parity checks that read the web sources. They run from internal/web, so the repo root is two levels up.
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
	for _, c := range keymap.Contexts { // console included (T-P2-07)
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
	for i, x := range keymap.Contexts {
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

func TestSupportedActionsParsesWorkspace(t *testing.T) {
	b, err := os.ReadFile("../../web/src/routes/Workspace.tsx")
	if err != nil {
		t.Skip("web sources not present")
	}
	l := SupportedActions(b)
	has := map[string]bool{}
	for _, a := range l {
		has[a] = true
	}
	for _, a := range []string{"help", "commit", "tab_1", "tab_9", "close_pane"} {
		if !has[a] {
			t.Errorf("%s missing from %d parsed actions", a, len(l))
		}
	}
	if got := SupportedActions([]byte("const SUPPORTED = new Set(['a', 'b_c', ...Array.from({ length: 2 }, (_, i) => `t_${i + 1}`)])")); strings.Join(got, ",") != "t_1,t_2,a,b_c" {
		t.Errorf("got %v", got)
	}
}

var gatedCaps = []string{"Indexes", "Triggers", "Functions", "ProcessList", "Users", "Schemas", "Document", "Views", "MatViews", "Sequences", "ReorderColumns"}

// capsWith returns Capabilities with every gated flag on except (or only) name.
func capsWith(name string, on bool) db.Capabilities {
	c := db.Capabilities{Dialect: "postgres", Editor: "sql", Transactions: true}
	v := reflect.ValueOf(&c).Elem()
	for _, n := range gatedCaps {
		v.FieldByName(n).SetBool(!on)
	}
	v.FieldByName(name).SetBool(on)
	return c
}

// TestParityCapabilityGating (M14 check 7): the session reports each capability the client gates on, the gated routes
// refuse (or answer supported:false) with it off, and the TSX reads every capability.
func TestParityCapabilityGating(t *testing.T) {
	for _, name := range gatedCaps {
		for _, on := range []bool{true, false} {
			e := newEnv(t, "")
			name, on := name, on
			e.fake.capsFn = func() db.Capabilities { return capsWith(name, on) }
			id := e.open()
			info := e.ok("GET", "/api/sessions/"+id, nil)
			caps, _ := info["caps"].(map[string]any)
			if got, _ := caps[name].(bool); got != on {
				t.Errorf("cap %s=%v: GET /api/sessions/{sid} reports caps.%s=%v", name, on, name, caps[name])
			}
			if on {
				continue
			}
			switch name {
			case "ProcessList":
				out := e.ok("GET", "/api/sessions/"+id+"/processes", nil)
				if out["supported"] != false {
					t.Errorf("ProcessList off: GET /processes did not answer supported:false: %v", out)
				}
			case "Users":
				if out := e.ok("GET", "/api/sessions/"+id+"/users", nil); out["supported"] != false {
					t.Errorf("Users off: GET /users did not answer supported:false: %v", out)
				}
				if code, _ := e.call("POST", "/api/sessions/"+id+"/users/plan", map[string]any{"Kind": "create", "User": map[string]any{"Name": "amy"}}, "tok"); code < 400 {
					t.Errorf("Users off: POST /users/plan answered %d", code)
				}
			case "ReorderColumns":
				ref := map[string]any{"Schema": "public", "Name": "t1", "Kind": "table"}
				if code, _ := e.call("POST", "/api/sessions/"+id+"/plan/table", map[string]any{"ref": ref, "op": "move_column", "column": "name", "to": 0}, "tok"); code < 400 {
					t.Errorf("ReorderColumns off: move_column answered %d", code)
				}
			}
		}
	}

	// the client reads every capability: caps.<Name> in the TSX, or cap:<Name> in a shared menu list it filters with
	tsx := readTree(t, "web/src", ".ts", ".tsx")
	menuCaps := map[string]bool{}
	for _, list := range menus.All() {
		for _, it := range list {
			for _, p := range strings.Split(it.When, ";") {
				if strings.HasPrefix(p, "cap:") {
					menuCaps[strings.TrimPrefix(p, "cap:")] = true
				}
			}
		}
	}
	for _, name := range gatedCaps {
		if !regexp.MustCompile(`caps\??\.`+name+`\b`).MatchString(tsx) && !menuCaps[name] {
			t.Errorf("web/src never reads caps.%s and no shared menu has cap:%s: the web does not gate on it", name, name)
		}
	}
}

// TestParityBootstrapFailureReported: the terminal shows "Bootstrap command failed"; the web returns the failures from POST /sessions.
func TestParityBootstrapFailureReported(t *testing.T) {
	e := newEnv(t, "")
	e.s.o.Conns[0].Bootstrap = []string{"select 1", "boom"}
	e.fake.capsFn = nil
	out := e.ok("POST", "/api/sessions", map[string]any{"name": "c1"})
	_ = out["id"]
	if l, _ := out["bootstrapErrors"].([]any); len(l) != 1 || !strings.Contains(l[0].(string), "boom") {
		t.Errorf("bootstrapErrors = %v", out["bootstrapErrors"])
	}
}
