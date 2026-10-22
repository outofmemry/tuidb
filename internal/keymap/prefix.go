package keymap

import (
	"fmt"
	"sort"
	"strings"
)

// Map is a resolved set of bindings plus the prefix key.
type Map struct {
	Prefix   string
	bindings []Binding
}

// Build merges user overrides onto the defaults. prefix and overrides use the
// TOML spellings ("C-d"); an override replaces every default key of that action.
// Override values: a key string, or a list of them; a "-n " prefix means no prefix needed.
func Build(prefix string, overrides map[string][]string) (*Map, error) {
	p, err := Parse(prefix)
	if err != nil {
		return nil, fmt.Errorf("prefix: %w", err)
	}
	m := &Map{Prefix: p}
	known := map[string][]string{} // action -> contexts
	for _, b := range DefaultBindings() {
		known[b.Action] = append(known[b.Action], b.Context)
	}
	for action, specs := range overrides {
		if _, ok := known[action]; !ok {
			return nil, fmt.Errorf("[keys] unknown action %q", action)
		}
		if len(specs) == 0 {
			return nil, fmt.Errorf("[keys] %s: an empty list would remove the action; give it a key", action)
		}
	}
	// Deterministic order so errors are stable.
	names := make([]string, 0, len(overrides))
	for a := range overrides {
		names = append(names, a)
	}
	sort.Strings(names)
	var user []Binding
	for _, action := range names {
		for _, ctx := range known[action] {
			for _, spec := range overrides[action] {
				noPrefix := false
				if rest, ok := strings.CutPrefix(spec, "-n "); ok {
					spec, noPrefix = rest, true
				}
				k, err := Parse(spec)
				if err != nil {
					return nil, fmt.Errorf("[keys] %s: %w", action, err)
				}
				user = append(user, Binding{Action: action, Key: k, Context: ctx, NoPrefix: noPrefix, Desc: descOf(action)})
			}
		}
	}
	// A key the user binds wins over defaults: a global user binding frees that key in every
	// context, a context binding frees it in that context. Otherwise a default such as grid "x"
	// would silently shadow commit="x".
	taken := func(d Binding) bool {
		for _, u := range user {
			if u.Key == d.Key && u.NoPrefix == d.NoPrefix && (u.Context == Global || u.Context == d.Context) {
				return true
			}
		}
		return false
	}
	for _, b := range DefaultBindings() {
		if _, over := overrides[b.Action]; !over && !taken(b) {
			m.bindings = append(m.bindings, b)
		}
	}
	m.bindings = append(m.bindings, user...)
	if err := m.validate(); err != nil {
		return nil, err
	}
	return m, nil
}

func descOf(action string) string {
	for _, d := range defaults {
		if d.action == action {
			return d.desc
		}
	}
	return action
}

func (m *Map) validate() error {
	if m.Prefix == "esc" {
		return fmt.Errorf("prefix: esc cancels the prefix and cannot be the prefix")
	}
	seen := map[string]string{} // ctx|noprefix|key -> action
	for _, b := range m.bindings {
		if b.Key == "esc" {
			return fmt.Errorf("[keys] %s: esc cancels the prefix and cannot be bound", b.Action)
		}
		if b.Key == m.Prefix {
			return fmt.Errorf("[keys] %s: %s is the prefix key and cannot be bound", b.Action, Display(b.Key))
		}
		id := fmt.Sprintf("%s|%t|%s", b.Context, b.NoPrefix, b.Key)
		if prev, dup := seen[id]; dup && prev != b.Action {
			return fmt.Errorf("[keys] %s and %s are both bound to %s in %s", prev, b.Action, Display(b.Key), b.Context)
		}
		seen[id] = b.Action
	}
	return nil
}

// All returns every resolved binding (defaults with user overrides applied).
func (m *Map) All() []Binding { return append([]Binding(nil), m.bindings...) }

// Lookup resolves a key typed after the prefix: focused context first, then global.
func (m *Map) Lookup(ctx, key string) (string, bool) {
	for _, c := range []string{ctx, Global} {
		for _, b := range m.bindings {
			if !b.NoPrefix && b.Context == c && b.Key == key {
				return b.Action, true
			}
		}
		if ctx == Global {
			break
		}
	}
	return "", false
}

// Bindings lists what is valid after the prefix in ctx (focused context shadows global), for which-key.
func (m *Map) Bindings(ctx string) []Binding {
	taken := map[string]bool{}
	var out []Binding
	for _, c := range []string{ctx, Global} {
		for _, b := range m.bindings {
			if b.NoPrefix || b.Context != c || taken[b.Key] {
				continue
			}
			taken[b.Key] = true
			out = append(out, b)
		}
		if ctx == Global {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Result of feeding one key press to the state machine.
type Result struct {
	Action      string // non-empty: run this action
	Pending     bool   // prefix is active, show which-key
	Passthrough bool   // deliver the key to the focused widget unchanged
	Unknown     bool   // a key after the prefix matched nothing
}

// Machine is the tmux-style prefix state machine.
type Machine struct {
	Map     *Map
	pending bool
}

func (s *Machine) Pending() bool { return s.pending }

// Feed handles one key press (canonical string) while ctx is focused.
func (s *Machine) Feed(ctx, key string) Result {
	if s.pending {
		s.pending = false
		switch {
		case key == "esc":
			return Result{}
		case key == s.Map.Prefix: // prefix twice sends the raw key, like tmux
			return Result{Passthrough: true}
		}
		if a, ok := s.Map.Lookup(ctx, key); ok {
			return Result{Action: a}
		}
		return Result{Unknown: true}
	}
	if key == s.Map.Prefix {
		s.pending = true
		return Result{Pending: true}
	}
	for _, c := range []string{ctx, Global} {
		for _, b := range s.Map.bindings {
			if b.NoPrefix && b.Context == c && b.Key == key {
				return Result{Action: b.Action}
			}
		}
	}
	return Result{Passthrough: true}
}
