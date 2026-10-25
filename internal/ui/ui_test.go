package ui

import (
	"testing"

	"webtuidb/internal/config"

	tea "charm.land/bubbletea/v2"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "ctrl+w":
		return tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func TestLineEdit(t *testing.T) {
	l := newLine("")
	for _, c := range "héllo wörld" {
		l.Handle(key(string(c)))
	}
	if l.Value() != "héllo wörld" {
		t.Fatal(l.Value())
	}
	l.Handle(key("ctrl+w"))
	if l.Value() != "héllo " {
		t.Errorf("ctrl+w: %q", l.Value())
	}
	l.Handle(key("left"))
	l.Handle(key("backspace"))
	if l.Value() != "héll " {
		t.Errorf("%q", l.Value())
	}
	l.Handle(key("home"))
	l.Handle(tea.PasteMsg{Content: "a\nb"})
	if l.Value()[:3] != "a b" {
		t.Errorf("paste newline must become space: %q", l.Value())
	}
	l.Handle(key("ctrl+u"))
	if l.Pos() != 0 {
		t.Errorf("pos %d", l.pos)
	}
	if l.Handle(tea.KeyPressMsg{Code: tea.KeyEnter}) {
		t.Error("enter must be left to the caller")
	}
}

func (l *lineEdit) Pos() int { return l.pos }

func TestFuzzyAndMatch(t *testing.T) {
	if _, ok := fuzzy("cst", "customers"); !ok {
		t.Error("subsequence")
	}
	if _, ok := fuzzy("tsc", "customers"); ok {
		t.Error("order matters")
	}
	a, _ := fuzzy("ord", "orders")
	b, _ := fuzzy("ord", "audit_records_data")
	if a <= b {
		t.Errorf("prefix match should beat scattered: %d vs %d", a, b)
	}
	for _, c := range []struct {
		p, n string
		ok   bool
	}{{"^ord", "orders", true}, {"^ord", "my_orders", false}, {"_items$", "order_items", true}, {"^orders$", "orders", true}, {"^orders$", "orders2", false}} {
		if _, ok := matchName(c.p, c.n, true); ok != c.ok {
			t.Errorf("%s vs %s = %v", c.p, c.n, ok)
		}
	}
	if _, ok := matchName("rdr", "orders", false); ok {
		t.Error("contains mode is not fuzzy")
	}
}

func TestFit(t *testing.T) {
	if fit("héllo", 3) != "hé…" || fit("ab", 4) != "ab  " || fit("x", 0) != "" {
		t.Errorf("%q %q", fit("héllo", 3), fit("ab", 4))
	}
	if oneLine("a\nb\tc") != "a⏎b→c" {
		t.Error(oneLine("a\nb\tc"))
	}
}

func configTheme() config.Theme { return config.Theme{Name: "dark"} }
