package ui

import (
	"strings"
	"testing"
)

func TestM15OpenAnythingExtras(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	var opened string
	old := openURL
	openURL = func(u string) error { opened = u; return nil }
	defer func() { openURL = old }()
	h.a.ws().openAnything()
	h.typeText("Open docs")
	h.press("enter")
	if !strings.HasPrefix(opened, "https://github.com/") {
		t.Errorf("docs url %q", opened)
	}
	// Tab completes the keyword with the selected name
	h.a.ws().openAnything()
	h.typeText("cust")
	h.press("tab")
	p := h.a.overlays[len(h.a.overlays)-1].(*pickerDlg)
	if p.q.Value() != "customers" {
		t.Errorf("tab completion: %q", p.q.Value())
	}
}
