package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// P2-70: toggling vim off clears visual and ":" mode.
func TestTUI1VimOffClearsModes(t *testing.T) {
	e := NewEditor("postgres", EditorOpts{})
	e.Insert("select 1")
	e.extra("vim_mode")
	e.vim.vmode, e.vim.cmdOn, e.vim.cmd = 'v', true, "w"
	e.extra("vim_mode")
	if e.vim.on || e.vim.vmode != 0 || e.vim.cmdOn || e.vim.cmd != "" || e.HasSelection() {
		t.Fatalf("state %+v", e.vim)
	}
	e.extra("vim_mode")
	if e.vim.cmdOn {
		t.Fatal("re-enabling resumed command mode")
	}
}

func TestM3VimModeDeletesAChar(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("abc")
	h.prefix("v")
	h.want("[N]")
	h.press("0", "x")
	q := h.a.ws().tab().(*queryTab)
	if q.ed.Text() != "bc" {
		t.Fatalf("text %q", q.ed.Text())
	}
	h.typeText("z") // unknown normal-mode letters must not insert
	h.press("i", "q", "esc")
	if q.ed.Text() != "qbc" {
		t.Fatalf("insert mode: %q", q.ed.Text())
	}
	h.press("o")
	h.typeText("n")
	h.press("esc", "g", "g", "d", "d", "p")
	if q.ed.Text() != "n\nqbc" {
		t.Fatalf("dd p: %q", q.ed.Text())
	}
	h.press("u")
	h.prefix("v")
	h.wantNot("[N]")
}

func TestVimNormalModeIgnoresEditingKeys(t *testing.T) {
	e := NewEditor("postgres", EditorOpts{})
	e.SetText("select 1")
	e.vim.on, e.vim.insert = true, false
	e.cur = pos{0, 3}
	for _, c := range []rune{tea.KeyEnter, tea.KeyTab, tea.KeyBackspace, tea.KeyDelete} {
		e.Handle(tea.KeyPressMsg{Code: c})
		if e.Text() != "select 1" {
			t.Fatalf("key %v edited the buffer: %q", c, e.Text())
		}
	}
}
