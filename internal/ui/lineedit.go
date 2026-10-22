package ui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// lineEdit is a single-line text field (dialogs, forms, cell editing, filter values, search boxes).
type lineEdit struct {
	r      []rune
	pos    int
	Secret bool // render as bullets (passwords)
}

func newLine(s string) lineEdit { r := []rune(s); return lineEdit{r: r, pos: len(r)} }

func (l *lineEdit) Value() string { return string(l.r) }

func (l *lineEdit) Set(s string) { l.r = []rune(s); l.pos = len(l.r) }

func (l *lineEdit) insert(s string) {
	in := []rune(strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s))
	l.r = append(l.r[:l.pos], append(in, l.r[l.pos:]...)...)
	l.pos += len(in)
}

// Handle applies an editing key and reports whether it was one. Enter/Esc/Tab are left to the caller.
func (l *lineEdit) Handle(msg tea.Msg) bool {
	switch m := msg.(type) {
	case tea.PasteMsg:
		l.insert(m.Content)
		return true
	case tea.KeyPressMsg:
		k := tea.Key(m)
		if k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			l.insert(k.Text)
			return true
		}
		switch m.Keystroke() {
		case "backspace", "ctrl+h":
			if l.pos > 0 {
				l.r = append(l.r[:l.pos-1], l.r[l.pos:]...)
				l.pos--
			}
		case "delete", "ctrl+d":
			if l.pos < len(l.r) {
				l.r = append(l.r[:l.pos], l.r[l.pos+1:]...)
			}
		case "left", "ctrl+b":
			l.pos = max(0, l.pos-1)
		case "right", "ctrl+f":
			l.pos = min(len(l.r), l.pos+1)
		case "home", "ctrl+a":
			l.pos = 0
		case "end", "ctrl+e":
			l.pos = len(l.r)
		case "ctrl+u":
			l.r, l.pos = l.r[l.pos:], 0
		case "ctrl+k":
			l.r = l.r[:l.pos]
		case "ctrl+w", "alt+backspace":
			i := l.pos
			for i > 0 && unicode.IsSpace(l.r[i-1]) {
				i--
			}
			for i > 0 && !unicode.IsSpace(l.r[i-1]) {
				i--
			}
			l.r = append(l.r[:i], l.r[l.pos:]...)
			l.pos = i
		case "alt+left", "alt+b":
			for l.pos > 0 && unicode.IsSpace(l.r[l.pos-1]) {
				l.pos--
			}
			for l.pos > 0 && !unicode.IsSpace(l.r[l.pos-1]) {
				l.pos--
			}
		case "alt+right", "alt+f":
			for l.pos < len(l.r) && unicode.IsSpace(l.r[l.pos]) {
				l.pos++
			}
			for l.pos < len(l.r) && !unicode.IsSpace(l.r[l.pos]) {
				l.pos++
			}
		default:
			return false
		}
		return true
	}
	return false
}

// View renders the text with a reverse-video cursor, scrolled to fit w cells.
func (l *lineEdit) View(w int, th Theme, focused bool) string {
	shown := l.r
	if l.Secret {
		shown = []rune(strings.Repeat("•", len(l.r)))
	}
	if w < 2 {
		return ""
	}
	// window by display cells: wide runes (CJK, emoji) take two cells each
	cur := " "
	if l.pos < len(shown) {
		cur = string(shown[l.pos])
	}
	budget := w - 1 - width(cur)
	start, used := l.pos, 0
	for start > 0 && used+width(string(shown[start-1])) <= budget {
		start--
		used += width(string(shown[start]))
	}
	before := string(shown[start:l.pos])
	after, rest := "", w-1-width(before)-width(cur)
	for i := l.pos + 1; i < len(shown); i++ {
		ch := string(shown[i])
		if width(after)+width(ch) > rest {
			break
		}
		after += ch
	}
	if !focused {
		return fit(before+cur+after, w)
	}
	return fit(before+th.SelCell.Render(cur)+after, w)
}
