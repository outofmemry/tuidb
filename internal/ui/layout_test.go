package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
)

func TestM15TogglePanels(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	w.showConsole, w.showDetail = true, false
	h.want("Console")
	h.a.run("toggle_panels")
	if w.showSB || w.showConsole || w.showDetail || !w.zen {
		t.Fatalf("zen did not hide: %+v", w)
	}
	h.wantNot("Console")
	if lines := strings.Split(h.screen(), "\n"); len(lines) != h.a.h {
		t.Errorf("zen body must fill the screen: %d lines of %d", len(lines), h.a.h)
	}
	h.a.run("toggle_panels")
	if !w.showSB || !w.showConsole || w.showDetail || w.zen {
		t.Fatalf("zen did not restore: sb=%v console=%v detail=%v", w.showSB, w.showConsole, w.showDetail)
	}
	h.want("Console")
}

func TestM15Hints(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.General.Hints = []string{"commit", "preview"} })
	h.connect()
	h.openCustomers()
	h.a.toast = ""
	s := h.screen()
	status := s[strings.LastIndex(s, "\n")+1:]
	if !strings.Contains(status, "commit") || !strings.Contains(status, "preview") {
		t.Errorf("status line lacks the configured hints: %q", status)
	}
	if strings.Contains(status, "help") || strings.Contains(status, "filter") {
		t.Errorf("status line shows more than the configured hints: %q", status)
	}
}

func TestM15ProductionCue(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.Tag = "production" })
	h.connect()
	w := h.a.ws()
	if w.cue() == "" {
		t.Fatal("production workspaces get a cue colour")
	}
	if got := w.frame(h.a.th, "t", "", "x", 12, 3, true); !strings.Contains(got, "\x1b[") {
		t.Error("the frame border is not coloured")
	}
	w.conn.Tag = "development"
	if w.cue() != "" {
		t.Error("development has no cue")
	}
}

// T-P2-06: the table frame hint follows a [keys] rebinding instead of a hard-coded "C-d s".
func TestM18FrameHintFollowsKeys(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.KeyOverrides = map[string][]string{"commit": {"F9"}} })
	h.connect()
	h.openCustomers()
	h.want("C-d F9")
	h.wantNot("C-d s ")
}

// On a production workspace every border takes the cue colour, but only the focused frame has it in full
// and carries key hints; the others take the faded cue, so focus stays visible.
func TestFocusVisibleOnCuedWorkspace(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.Tag = "production" })
	h.connect()
	w := h.a.ws()
	th := h.a.th
	hint := th.chips("↵", "open")
	on := w.frame(th, "t", hint, "x", 30, 3, true)
	off := w.frame(th, "t", hint, "x", 30, 3, false)
	edge := func(s string) string { return strings.SplitN(s, "╭", 2)[0] } // the escape before the corner
	if edge(on) == edge(off) {
		t.Fatalf("focused and unfocused frames draw the same border: %q", edge(on))
	}
	if !strings.Contains(stripANSI(on), "open") || strings.Contains(stripANSI(off), "open") {
		t.Errorf("only the focused frame carries its hints:\non  %q\noff %q", stripANSI(on), stripANSI(off))
	}
	plainOff := frame(th, "t", hint, "x", 30, 3, false)
	if strings.Contains(stripANSI(plainOff), "open") {
		t.Error("an unfocused plain frame carries hints")
	}
}

func TestKeyGlyph(t *testing.T) {
	for in, want := range map[string]string{
		"Enter": "↵", "↵": "↵", "C-d Enter": "C-d ↵", "C-d M-Enter": "C-d M-↵", "Alt+Enter": "M-↵",
		"Ctrl+t": "C-t", "Shift+Tab": "S-Tab", "C-d s": "C-d s", "Esc": "Esc", "Tab/↑↓": "Tab/↑↓", "+": "+",
	} {
		if got := keyGlyph(in); got != want {
			t.Errorf("keyGlyph(%q) = %q, want %q", in, got, want)
		}
	}
}

// The status line names the focused pane, adds the editor's cursor position, and does not repeat the
// keys the focused frame's border already shows.
func TestStatusLineNamesFocusAndSkipsShownHints(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	status := func() string {
		h.a.toast = ""
		s := h.screen()
		return s[strings.LastIndex(s, "\n")+1:]
	}
	if st := status(); !strings.HasPrefix(st, " OBJECTS ") {
		t.Errorf("sidebar focus is not named: %q", st)
	}
	h.openCustomers()
	st := status()
	if !strings.HasPrefix(st, " DATA ") {
		t.Errorf("grid focus is not named: %q", st)
	}
	if !strings.Contains(h.screen(), "C-d s  commit") {
		t.Fatal("the grid frame lost its commit hint")
	}
	if strings.Contains(st, "commit") || !strings.Contains(st, "help") {
		t.Errorf("status line repeats the frame's hint or lost help: %q", st)
	}
	h.prefix("e")
	h.typeText("select 1")
	if st := status(); !strings.HasPrefix(st, " EDITOR  Ln 1, Col 9") {
		t.Errorf("editor status lacks the cursor position: %q", st)
	}
}

// The sidebar's tab strip never ends in "…": idle tabs shrink to their digit, then to plain digits.
func TestSidebarTitleFits(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	sb := h.a.ws().sb
	for _, maxW := range []int{40, 29, 18} {
		got := stripANSI(sb.title(h.a.th, maxW, true))
		if width(got) > maxW || strings.Contains(got, "…") || !strings.Contains(got, "Objects") {
			t.Errorf("maxW %d: %q (%d cells)", maxW, got, width(got))
		}
	}
	if got := stripANSI(sb.title(h.a.th, 40, true)); !strings.Contains(got, "History") {
		t.Errorf("a wide sidebar shows every label: %q", got)
	}
	h.a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.wantNot("3 Hi…")
	h.wantNot("2 Que…")
}
