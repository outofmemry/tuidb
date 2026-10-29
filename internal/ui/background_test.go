package ui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

func TestJobSurvivesAPanic(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	h.pump(w.job("boom", func(context.Context, db.Session, func(func() tea.Cmd)) error { panic("x") }, func() (string, tea.Cmd) { return "no", nil }))
	if w.jobCancel != nil || w.busy() {
		t.Fatal("a panicking job left the workspace busy")
	}
}
