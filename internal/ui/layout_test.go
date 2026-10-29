package ui

import (
	"strings"
	"testing"

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
