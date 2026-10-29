package keymap

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestParse(t *testing.T) {
	for in, want := range map[string]string{
		"C-d": "ctrl+d", "M-x": "alt+x", "S-Enter": "shift+enter", "C-M-x": "ctrl+alt+x", "F5": "f5",
		"]": "]", "P": "P", "S-p": "P", "Space": "space", "PageUp": "pgup", "C-D": "ctrl+d", "Esc": "esc",
	} {
		if got, err := Parse(in); err != nil || got != want {
			t.Errorf("Parse(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "X-a", "Banana", "C-"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestDisplayRoundTrip(t *testing.T) {
	for _, k := range []string{"C-d", "M-Enter", "F5", "]", "P", "C-M-x"} {
		c, _ := Parse(k)
		if back, _ := Parse(Display(c)); back != c {
			t.Errorf("%q: %q -> %q -> %q", k, c, Display(c), back)
		}
	}
}

func TestDefaultsAreConflictFree(t *testing.T) {
	if _, err := Build("C-d", nil); err != nil {
		t.Fatal(err)
	}
}

func TestOverrideAndConflict(t *testing.T) {
	m, err := Build("C-d", map[string][]string{"commit": {"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := m.Lookup(Grid, "x"); !ok || a != "commit" {
		t.Errorf("x -> %q %v", a, ok)
	}
	if _, ok := m.Lookup(Grid, "s"); ok {
		t.Error("old default key s should be gone")
	}
	// like tmux `bind`: taking a key that has a default binding replaces that default
	rm, err := Build("C-d", map[string][]string{"commit": {"p"}})
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := rm.Lookup(Grid, "p"); a != "commit" {
		t.Errorf("p -> %q", a)
	}
	if _, err := Build("C-d", map[string][]string{"commit": {"z"}, "preview": {"z"}}); err == nil {
		t.Error("two user bindings on the same key in the same context must be rejected")
	}
	if _, err := Build("C-d", map[string][]string{"nope": {"x"}}); err == nil {
		t.Error("unknown action must be rejected")
	}
	if _, err := Build("C-d", map[string][]string{"commit": {"esc"}}); err == nil {
		t.Error("esc must not be bindable")
	}
}

func TestContextShadowsGlobal(t *testing.T) {
	m, _ := Build("C-d", nil)
	if a, _ := m.Lookup(Editor, "."); a != "cancel_query" {
		t.Errorf("editor . = %q", a)
	}
	if a, _ := m.Lookup(Grid, "."); a != "process_list" {
		t.Errorf("grid . = %q", a)
	}
}

func TestMachine(t *testing.T) {
	m, _ := Build("C-d", map[string][]string{"reload": {"-n f5"}})
	s := &Machine{Map: m}
	if r := s.Feed(Grid, "s"); !r.Passthrough {
		t.Error("plain key must pass through")
	}
	if r := s.Feed(Grid, "ctrl+d"); !r.Pending || !s.Pending() {
		t.Error("prefix should start pending")
	}
	if r := s.Feed(Grid, "s"); r.Action != "commit" {
		t.Errorf("got %+v", r)
	}
	s.Feed(Grid, "ctrl+d")
	if r := s.Feed(Grid, "ctrl+d"); !r.Passthrough {
		t.Error("prefix twice should pass the key through")
	}
	s.Feed(Grid, "ctrl+d")
	if r := s.Feed(Grid, "esc"); r.Action != "" || s.Pending() {
		t.Error("esc cancels")
	}
	s.Feed(Grid, "ctrl+d")
	if r := s.Feed(Grid, "~"); !r.Unknown {
		t.Error("unbound key after prefix is Unknown")
	}
	if r := s.Feed(Grid, "f5"); r.Action != "reload" {
		t.Errorf("no-prefix binding: %+v", r)
	}
}

func TestFromMsg(t *testing.T) {
	for _, c := range []struct {
		k    tea.Key
		want string
	}{
		{tea.Key{Code: 'a', Text: "a"}, "a"},
		{tea.Key{Code: 'a', ShiftedCode: 'A', Text: "A", Mod: tea.ModShift}, "A"},
		{tea.Key{Code: 'a', Text: "A"}, "A"},
		{tea.Key{Code: 'd', Mod: tea.ModCtrl}, "ctrl+d"},
		{tea.Key{Code: 'x', Mod: tea.ModAlt}, "alt+x"},
		{tea.Key{Code: 'x', Mod: tea.ModAlt, Text: "x"}, "alt+x"},
		{tea.Key{Code: ' ', Text: " "}, "space"},
		{tea.Key{Code: '?', Text: "?"}, "?"},
		{tea.Key{Code: tea.KeyEnter, Mod: tea.ModAlt}, "alt+enter"},
		{tea.Key{Code: tea.KeyEscape}, "esc"},
		{tea.Key{Code: 'k', Mod: tea.ModAlt | tea.ModShift}, "alt+K"},
		{tea.Key{Code: 'k', ShiftedCode: 'K', Text: "K", Mod: tea.ModAlt | tea.ModShift}, "alt+K"},
	} {
		if got := FromMsg(tea.KeyPressMsg(c.k)); got != c.want {
			t.Errorf("%+v -> %q, want %q", c.k, got, c.want)
		}
	}
	// What the keymap parser produces must equal what the terminal reports.
	for spec, k := range map[string]tea.Key{"C-d": {Code: 'd', Mod: tea.ModCtrl}, "M-Enter": {Code: tea.KeyEnter, Mod: tea.ModAlt}, "M-K": {Code: 'k', Mod: tea.ModAlt | tea.ModShift}, "P": {Code: 'p', ShiftedCode: 'P', Text: "P", Mod: tea.ModShift}} {
		if p, _ := Parse(spec); p != FromMsg(tea.KeyPressMsg(k)) {
			t.Errorf("Parse(%q)=%q but terminal gives %q", spec, p, FromMsg(tea.KeyPressMsg(k)))
		}
	}
}

func TestUserBindingWinsOverContextDefault(t *testing.T) {
	// "x" is quick_filter in the grid by default; binding commit to it must not be shadowed there.
	m, err := Build("C-d", map[string][]string{"commit": {"x"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, ctx := range []string{Grid, Editor, Sidebar, Global} {
		if a, _ := m.Lookup(ctx, "x"); a != "commit" {
			t.Errorf("%s: x -> %q, want commit", ctx, a)
		}
	}
	// A context-scoped user binding only frees the key in its own context.
	m, err = Build("C-d", map[string][]string{"quick_filter": {"y"}})
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := m.Lookup(Grid, "y"); a != "quick_filter" {
		t.Errorf("y -> %q", a)
	}
	if _, ok := m.Lookup(Grid, "x"); ok {
		t.Error("the old default key of an overridden action must be gone")
	}
}

func TestBuildRejectsEscPrefixAndEmptyLists(t *testing.T) {
	if _, err := Build("esc", nil); err == nil {
		t.Error("esc cannot be the prefix")
	}
	if _, err := Build("C-d", map[string][]string{"reload": {"-n esc"}}); err == nil {
		t.Error("esc cannot be a no-prefix binding either")
	}
	if _, err := Build("C-d", map[string][]string{"quit": {}}); err == nil {
		t.Error("an empty list must not delete an action")
	}
	if _, err := Build("C-d", map[string][]string{"reload": {"-n C-d"}}); err == nil {
		t.Error("the prefix key cannot be bound to an action")
	}
	if k, _ := Parse(" "); k != "space" {
		t.Errorf("a space spec must mean the space bar, got %q", k)
	}
}

func TestSessionBindings(t *testing.T) {
	m, err := Build("C-d", nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"n": "new_session", "l": "switch_session"} {
		if got, ok := m.Lookup(Global, k); !ok || got != want {
			t.Errorf("prefix %s = %q, %v; want %q", k, got, ok, want)
		}
	}
}

func TestCloseAllTabsNotBrowserReserved(t *testing.T) {
	for _, b := range defaults {
		if b.action == "close_all_tabs" && b.key == "ctrl+w" {
			t.Fatal("ctrl+w closes the browser window; close_all_tabs must not use it")
		}
	}
}

func TestBindingsKeepTableOrder(t *testing.T) {
	m, err := Build("C-d", nil)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, b := range defaults {
		if b.ctx == Global {
			want = append(want, b.action)
		}
	}
	var got []string
	for _, b := range m.Bindings(Global) {
		got = append(got, b.Action)
	}
	if len(got) == 0 || got[0] != want[0] {
		t.Errorf("Bindings(Global) must start with the first table row %q, got %q", want[0], got)
	}
}

func TestFilterBarAndGridFindDefaults(t *testing.T) {
	m, err := Build("C-d", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ ctx, key, action string }{
		{Grid, "alt+/", "find"}, {Grid, "alt+g", "find_next"}, {Grid, "alt+G", "find_prev"},
	} {
		if a, ok := m.Lookup(c.ctx, c.key); !ok || a != c.action {
			t.Errorf("%s %s = %q, %v; want %s", c.ctx, c.key, a, ok, c.action)
		}
	}
	// the filter-bar keys are no-prefix bindings (both UIs answer the raw key)
	for _, c := range []struct{ key, action string }{{"alt+c", "filter_clear_all"}, {"alt+e", "filter_export"}, {"alt+n", "filter_add_copy"}} {
		s := &Machine{Map: m}
		if r := s.Feed(FilterBar, c.key); r.Action != c.action {
			t.Errorf("raw filterbar %s = %+v; want %s", c.key, r, c.action)
		}
	}
	// the grid find keys must not shadow Global bindings (search_in_database, new_session)
	for key, want := range map[string]string{"/": "search_in_database", "n": "new_session"} {
		if a, _ := m.Lookup(Grid, key); a != want {
			t.Errorf("Grid %s = %q, want the Global %s", key, a, want)
		}
	}
}

func TestCanonicalShiftPunct(t *testing.T) {
	for in, want := range map[string]string{
		"alt+shift+'":      `alt+"`,
		"alt+shift+,":      "alt+<",
		"ctrl+shift+1":     "ctrl+!",
		"ctrl+alt+shift+/": "ctrl+alt+?",
		"alt+shift+k":      "alt+K",
		"alt+shift+x1":     "alt+shift+x1",
	} {
		if got := Canonical(in); got != want {
			t.Errorf("Canonical(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseFoldsShiftPunctLikeCanonical(t *testing.T) {
	for spec, ev := range map[string]string{
		"C-S-1": "ctrl+shift+1", "M-S-1": "alt+shift+1", "M-S-'": "alt+shift+'", "C-M-S-/": "ctrl+alt+shift+/",
	} {
		got, err := Parse(spec)
		if err != nil {
			t.Fatal(err)
		}
		if want := Canonical(ev); got != want {
			t.Errorf("Parse(%q) = %q, want Canonical(%q) = %q", spec, got, ev, want)
		}
	}
	if got, _ := Parse("C-S-1"); got != "ctrl+!" {
		t.Errorf("Parse(C-S-1) = %q", got)
	}
}

func TestBuildExtraPluginBindings(t *testing.T) {
	plug := Binding{Action: "plugin:count", Key: "alt+P", Context: Global, Desc: "plugin: count"}
	m, err := Build("C-d", nil, plug)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range m.All() {
		found = found || (b.Action == "plugin:count" && b.Key == "alt+P")
	}
	if !found {
		t.Fatal("plugin binding missing from All()")
	}
	// a [keys] override applies to it
	m, err = Build("C-d", map[string][]string{"plugin:count": {"M-Q"}}, plug)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range m.All() {
		if b.Action == "plugin:count" && (b.Key != "alt+Q" || b.Desc != "plugin: count") {
			t.Fatalf("override not applied: %+v", b)
		}
	}
	// a conflict with a default errors
	if _, err := Build("C-d", nil, Binding{Action: "plugin:x", Key: "alt+q", Context: Global}); err == nil {
		t.Fatal("expected a conflict with close_pane")
	}
	if _, err := Build("C-d", nil, plug, Binding{Action: "plugin:other", Key: "alt+P", Context: Global}); err == nil {
		t.Fatal("expected a conflict between two plugins")
	}
}

// R-002: a plugin key that clashes is skipped and reported; the others stay.
func TestBuildCheckedSkipsConflicts(t *testing.T) {
	extra := []Binding{
		{Action: "plugin:ok", Key: "alt+Z", Context: Global, Desc: "plugin: ok"},
		{Action: "plugin:clash", Key: "alt+J", Context: Global, Desc: "plugin: clash"}, // alt+J is jobs
		{Action: "plugin:dup", Key: "alt+Z", Context: Global, Desc: "plugin: dup"},     // taken by the first plugin
		{Action: "plugin:bad", Key: "nonsense+++", Context: Global, Desc: "plugin: bad"},
	}
	if _, err := Build("C-b", nil, extra...); err == nil {
		t.Fatal("Build should still fail on a clash")
	}
	m, conflicts, err := BuildChecked("C-b", nil, extra...)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 3 {
		t.Fatalf("conflicts = %q", conflicts)
	}
	have := map[string]bool{}
	for _, b := range m.All() {
		have[b.Action] = true
	}
	if !have["plugin:ok"] || have["plugin:clash"] || have["plugin:dup"] || have["plugin:bad"] || !have["jobs"] {
		t.Errorf("bindings = %v", have)
	}
}

// P1-05: an override of an installed plugin's action must be known to the first Build.
func TestBuildCheckedKnowsExtraActionsInOverrides(t *testing.T) {
	extra := []Binding{{Action: "plugin:x", Key: "alt+P", Context: Global, Desc: "plugin: x"}}
	m, conflicts, err := BuildChecked("C-a", map[string][]string{"plugin:x": {"M-Q"}}, extra...)
	if err != nil || len(conflicts) != 0 || m == nil {
		t.Fatalf("err=%v conflicts=%v", err, conflicts)
	}
	found := false
	for _, b := range m.All() {
		if b.Action == "plugin:x" && b.Key == "alt+Q" {
			found = true
		}
	}
	if !found {
		t.Fatal("override key not bound")
	}
}

func TestTakenFromPerContext(t *testing.T) { // P2-79
	old := &Map{bindings: []Binding{{Action: "copy_cells_as", Key: "alt+C", Context: Grid}, {Action: "copy_cells_as", Key: "alt+C", Context: Results}}}
	same := &Map{bindings: old.All()}
	if err := TakenFrom(old, same, "x"); err != nil {
		t.Errorf("unchanged: %v", err)
	}
	lostOne := &Map{bindings: old.All()[:1]}
	if err := TakenFrom(old, lostOne, "x"); err == nil {
		t.Error("an action losing only its Results binding must be reported")
	}
	if err := TakenFrom(old, lostOne, "copy_cells_as"); err != nil {
		t.Errorf("the edited action may change: %v", err)
	}
	if err := TakenFrom(old, &Map{}, "x"); err == nil {
		t.Error("an action losing every binding must be reported")
	}
}

// T-P2-06: an override that takes another action's key is reported, not silent.
func TestOverrideTakingAKeyWarns(t *testing.T) {
	m, err := Build("C-d", map[string][]string{"commit": {"x"}})
	if err != nil {
		t.Fatal(err)
	}
	want := `[keys] commit = "x" takes the key from quick_filter (grid); quick_filter is now unbound`
	if w := m.Warnings(); len(w) != 1 || w[0] != want {
		t.Fatalf("warnings = %q, want [%q]", w, want)
	}
	if m, _ := Build("C-d", map[string][]string{"commit": {"F9"}}); len(m.Warnings()) != 0 {
		t.Errorf("a free key must not warn: %q", m.Warnings())
	}
}

// T-P2-07: `webtuidb -keys` lists every context GET /api/meta reports (it sends km.All()).
func TestListingCoversEveryContext(t *testing.T) {
	m, _ := Build("C-d", nil)
	seen := map[string]bool{}
	for _, b := range m.All() {
		seen[b.Context] = true
	}
	for _, c := range Contexts {
		delete(seen, c)
	}
	if len(seen) != 0 {
		t.Errorf("contexts missing from Listing: %v", seen)
	}
	if !strings.Contains(Listing("C-d", m), "console_find") {
		t.Error("Listing lacks the console bindings")
	}
}

// P1-05: two installed plugins, both with a [keys] override, must both bind.
func TestBuildCheckedTwoOverriddenPlugins(t *testing.T) {
	extra := []Binding{{Action: "plugin:x", Key: "alt+P", Context: Global, Desc: "plugin: x"}, {Action: "plugin:y", Key: "alt+O", Context: Global, Desc: "plugin: y"}}
	m, conflicts, err := BuildChecked("C-a", map[string][]string{"plugin:x": {"M-Q"}, "plugin:y": {"M-W"}}, extra...)
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("err=%v conflicts=%v", err, conflicts)
	}
	got := map[string]string{}
	for _, b := range m.All() {
		if strings.HasPrefix(b.Action, "plugin:") {
			got[b.Action] = b.Key
		}
	}
	if got["plugin:x"] != "alt+Q" || got["plugin:y"] != "alt+W" {
		t.Fatalf("bound %v", got)
	}
}

// The Json and Tree views exist for every document engine; both help screens render this text.
func TestDocumentViewHelpNamesEveryDocumentEngine(t *testing.T) {
	seen := 0
	for _, b := range defaults {
		if b.action != "show_json" && b.action != "show_tree" {
			continue
		}
		seen++
		for _, eng := range []string{"MongoDB", "Redis", "DynamoDB"} {
			if !strings.Contains(b.desc, eng) {
				t.Errorf("%s: %q does not name %s", b.action, b.desc, eng)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("found %d of show_json / show_tree", seen)
	}
}
