package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
)

// The welcome screen never renders a line wider than the terminal, at any size.
func TestWelcomeFitsTerminal(t *testing.T) {
	h := newHarness(t, nil)
	h.a.d.Conns = append(h.a.d.Conns, config.Connection{Name: "clickhouse.cloudstg.svc.cluster.local", Driver: config.ClickHouse, Host: "127.0.0.1", Port: 9000, Tag: "staging"},
		config.Connection{Name: "localhost/cloudstg", Driver: config.Mongo, Host: "localhost", Port: 27017, Tag: "staging"})
	for _, wh := range [][2]int{{192, 48}, {120, 36}, {80, 24}, {60, 20}} {
		h.a.Update(tea.WindowSizeMsg{Width: wh[0], Height: wh[1]})
		lines := strings.Split(h.screen(), "\n")
		if len(lines) != wh[1] {
			t.Fatalf("%v: %d lines", wh, len(lines))
		}
		for i, l := range lines {
			if width(l) != wh[0] {
				t.Errorf("%v line %d is %d wide: %q", wh, i, width(l), l)
			}
		}
		if wh[0] >= 132 && !strings.Contains(h.screen(), "│ClickHouse │") {
			t.Errorf("%v: ClickHouse tile should show just the name", wh)
		}
	}
}
