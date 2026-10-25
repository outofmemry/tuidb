package keymap

import (
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
)

var shiftLetter = regexp.MustCompile(`^(alt\+)?shift\+([a-z])$`)

// Canonical normalizes a Bubble Tea keystroke ("shift+a") to the form Parse produces ("A").
func Canonical(keystroke string) string {
	if m := shiftLetter.FindStringSubmatch(keystroke); m != nil {
		return m[1] + strings.ToUpper(m[2]) // "shift+a" -> "A", "alt+shift+k" -> "alt+K"
	}
	return keystroke
}

// FromMsg is the canonical key string of a key press. Plain text keys use String() (it keeps
// "A", "?", "space"); anything with ctrl/alt uses Keystroke(), which, unlike String(), keeps alt.
func FromMsg(m tea.KeyPressMsg) string {
	k := tea.Key(m)
	if k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) == 0 {
		return m.String()
	}
	return Canonical(m.Keystroke())
}
