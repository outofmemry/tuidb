package keymap

import (
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
	} {
		if got := FromMsg(tea.KeyPressMsg(c.k)); got != c.want {
			t.Errorf("%+v -> %q, want %q", c.k, got, c.want)
		}
	}
	// What the keymap parser produces must equal what the terminal reports.
	for spec, k := range map[string]tea.Key{"C-d": {Code: 'd', Mod: tea.ModCtrl}, "M-Enter": {Code: tea.KeyEnter, Mod: tea.ModAlt}, "P": {Code: 'p', ShiftedCode: 'P', Text: "P", Mod: tea.ModShift}} {
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
