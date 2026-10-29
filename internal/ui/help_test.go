package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/keymap"
)

func TestM12HelpListsBindingsInTableOrder(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.a.showHelp()
	hd := h.a.overlays[len(h.a.overlays)-1].(*helpDlg)
	var got []string
	for _, r := range hd.rows {
		if r.header == "" && r.ctx == keymap.Global {
			got = append(got, r.action)
		}
	}
	var want []string
	for _, b := range h.a.d.Keys.All() { // no-prefix bindings are listed too (M14 check 2)
		if b.Context == keymap.Global {
			want = append(want, b.Action)
		}
	}
	if len(got) == 0 || strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("help order %v, want the keymap table order %v", got, want)
	}
}
