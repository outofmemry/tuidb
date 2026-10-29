package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/keymap"
)

func TestM15ConsoleFindCopyErrors(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	w.console.Add("select 1", false, time.Millisecond)
	w.console.Add("select bad", true, time.Millisecond)
	w.console.Add("select 3", false, time.Millisecond)
	w.showConsole = true
	h.a.run("focus_down")
	if !w.focusConsole {
		t.Fatal("focus_down should reach the console")
	}
	h.press("/")
	h.typeText("select 3")
	h.press("enter")
	if ls := w.consoleLines(); w.consoleSel(len(ls)) != 2 {
		t.Errorf("find landed on %d", w.consoleSel(len(ls)))
	}
	h.press("N") // wraps back to the same single match
	h.press("e")
	if ls := w.consoleLines(); len(ls) != 1 || !ls[0].Failed {
		t.Errorf("errors only: %+v", ls)
	}
	h.press("e")
	h.press("y")
	h.press("esc") // clears the find
	h.press("esc") // leaves the pane
	if w.focusConsole {
		t.Error("esc should return the focus")
	}
}

func consoleHarness(t *testing.T, mutate func(*config.Connection, *config.Config)) (*harness, *Workspace) {
	h := newHarness(t, mutate)
	h.connect()
	w := h.a.ws()
	w.console.Add("select 1", false, time.Millisecond)
	w.console.Add("select bad", true, time.Millisecond)
	w.showConsole = true
	h.a.run("focus_down")
	if !w.focusConsole {
		t.Fatal("focus_down should reach the console")
	}
	return h, w
}

// P2-72: the console keys come from the keymap, so [keys] rebinds them and the context is Console.
func TestM16ConsoleKeysAreRebindable(t *testing.T) {
	h, w := consoleHarness(t, func(_ *config.Connection, c *config.Config) {
		c.KeyOverrides = map[string][]string{"console_errors": {"x"}}
	})
	if w.context() != keymap.Console {
		t.Fatalf("context %q", w.context())
	}
	h.press("e") // no longer bound
	if w.con.errorsOnly {
		t.Error("e must not toggle errors-only once console_errors is rebound")
	}
	h.press("x")
	if !w.con.errorsOnly {
		t.Error("the rebound key must toggle errors-only")
	}
}

// P2-78: a console that is not drawn must not keep the keys.
func TestM16ConsoleFocusDropsWhenHidden(t *testing.T) {
	h, w := consoleHarness(t, nil)
	h.a.Update(tea.WindowSizeMsg{Width: 120, Height: 12}) // too short for the console pane
	if w.context() == keymap.Console || w.consoleFocused() {
		t.Fatal("the hidden console still has the context")
	}
	h.press("/")
	if w.con.searching {
		t.Error("a key reached the invisible console")
	}
	if w.focusConsole {
		t.Error("focusConsole should have been cleared")
	}
}
