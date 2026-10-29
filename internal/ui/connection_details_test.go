package ui

import (
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/safemode"
)

func TestM5CustomTagColour(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, cfg *config.Config) {
		c.Tag = "qa"
		cfg.Tags = []config.Tag{{Name: "qa", Color: "#123456"}}
	})
	if got := h.a.tagColor(h.a.d.Conns[0]); got != "#123456" {
		t.Fatalf("tag colour %q", got)
	}
	if !contains(h.a.tagNames(), "qa") {
		t.Fatal("custom tag missing from the picker")
	}
	h.connect()
	h.want("QA")
}

func TestM5ConnectionDetailsAndStackLabel(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.General.ShowConnectionName = true })
	h.connect()
	h.run("connection_details")
	h.want("Connection details")
	h.want("Fake 1.0")
	if got := h.a.stackLabel(h.a.ws()); got != "shop" {
		t.Fatalf("label %q", got)
	}
}

func TestM6SafeModeLoweringAsksForThePassword(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SafeMode = "safe1" })
	h.connect()
	h.run("safe_mode")
	h.press("enter") // Silent
	h.want("Enter the database password")
	setField(t, h, "pw", "nope")
	h.press("enter")
	h.want("wrong password")
	if h.a.ws().level != safemode.Safe1 {
		t.Fatal("a wrong password must not lower the level")
	}
	setField(t, h, "pw", "pw")
	h.press("enter")
	if h.a.ws().level != safemode.Silent || h.a.d.Conns[0].SafeMode != "silent" {
		t.Fatalf("level %v conn %q", h.a.ws().level, h.a.d.Conns[0].SafeMode)
	}
}

func TestM8ConnectionDetailsColour(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SSLMode = "verify-full" })
	h.connect()
	h.run("connection_details")
	h.want("TLS:       on (verify-full)")
	h.want("Fake 1.0")
	h.press("o")
	h.pickLabel("blue")
	if h.a.ws().conn.Color != "#61afef" || h.a.d.Conns[0].Color != "#61afef" {
		t.Fatalf("colour %q %q", h.a.ws().conn.Color, h.a.d.Conns[0].Color)
	}
	if saved, _ := config.LoadConnections(h.a.d.ConnPath); saved[0].Color != "#61afef" {
		t.Fatalf("saved %+v", saved[0])
	}
}
