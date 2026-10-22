// Package keymap parses tmux-style key specs and runs the prefix state machine.
package keymap

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

var namedKeys = map[string]string{
	"enter": "enter", "return": "enter", "tab": "tab", "space": "space", "esc": "esc", "escape": "esc",
	"backspace": "backspace", "bspace": "backspace", "delete": "delete", "del": "delete", "insert": "insert",
	"up": "up", "down": "down", "left": "left", "right": "right", "home": "home", "end": "end",
	"pageup": "pgup", "pgup": "pgup", "pagedown": "pgdown", "pgdown": "pgdown",
}

// Parse turns "C-d", "M-Enter", "S-Tab", "F5", "]", "P" into the canonical
// string Bubble Tea's KeyPressMsg.String() produces ("ctrl+d", "alt+enter", ...).
func Parse(spec string) (string, error) {
	orig := spec
	var ctrl, alt, shift bool
	for len(spec) > 2 && spec[1] == '-' {
		switch spec[0] {
		case 'C', 'c':
			ctrl = true
		case 'M', 'm', 'A', 'a':
			alt = true
		case 'S', 's':
			shift = true
		default:
			return "", fmt.Errorf("bad key %q: unknown modifier %q", orig, spec[:1])
		}
		spec = spec[2:]
	}
	if spec == "" {
		return "", fmt.Errorf("bad key %q: empty", orig)
	}
	var base string
	switch {
	case utf8.RuneCountInString(spec) == 1:
		base = spec
		if shift && !ctrl && !alt && base >= "a" && base <= "z" {
			base, shift = strings.ToUpper(base), false
		}
	case namedKeys[strings.ToLower(spec)] != "":
		base = namedKeys[strings.ToLower(spec)]
	case isFunctionKey(spec):
		base = strings.ToLower(spec)
	default:
		return "", fmt.Errorf("bad key %q: unknown key name %q", orig, spec)
	}
	if ctrl && utf8.RuneCountInString(base) == 1 {
		base = strings.ToLower(base)
	}
	var mods []string
	if ctrl {
		mods = append(mods, "ctrl")
	}
	if alt {
		mods = append(mods, "alt")
	}
	if shift {
		mods = append(mods, "shift")
	}
	return strings.Join(append(mods, base), "+"), nil
}

func isFunctionKey(s string) bool {
	var n int
	if _, err := fmt.Sscanf(strings.ToLower(s), "f%d", &n); err != nil || fmt.Sprintf("f%d", n) != strings.ToLower(s) {
		return false
	}
	return n >= 1 && n <= 24
}

// Display is the inverse of Parse, for the which-key overlay: "ctrl+d" -> "C-d".
func Display(canon string) string {
	parts := strings.Split(canon, "+")
	// A literal "+" key canonicalizes to "+" (or "ctrl++"); Split yields empties.
	if canon == "+" {
		return "+"
	}
	var out string
	for _, p := range parts[:len(parts)-1] {
		switch p {
		case "ctrl":
			out += "C-"
		case "alt":
			out += "M-"
		case "shift":
			out += "S-"
		}
	}
	last := parts[len(parts)-1]
	if last == "" { // "ctrl++"
		last = "+"
	}
	if len(last) > 1 {
		last = strings.ToUpper(last[:1]) + last[1:]
	}
	return out + last
}
