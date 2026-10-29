package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

func TestM11TriggerEditorMakesTheSameScript(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	ref := db.ObjRef{Schema: "public", Name: "customers", Kind: db.KindTable}
	tr := db.Trigger{Name: "audit", Timing: "AFTER", Event: "INSERT", Statement: "FOR EACH ROW EXECUTE FUNCTION f()"}
	d := h.a.ws().newTriggerEdit(ref, tr)
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	d.Update(tea.KeyPressMsg{Code: tea.KeyLeft}) // AFTER -> BEFORE
	got := d.trigger()
	if got.Timing != "BEFORE" || got.Name != "audit" || got.Event != "INSERT" {
		t.Fatalf("grid fields: %+v", got)
	}
	want := triggerSQL(h.a.ws().caps.Dialect, ref, got)
	n := len(h.a.ws().tabs)
	d.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if len(h.a.ws().tabs) != n+1 {
		t.Fatalf("ctrl+s should open the script tab (%d tabs)", len(h.a.ws().tabs))
	}
	tab, ok := h.a.ws().tab().(*queryTab)
	if !ok || !strings.Contains(tab.ed.Text(), want) || !strings.Contains(want, "BEFORE") {
		t.Errorf("the script tab does not hold the generated trigger SQL:\n tab: %q\nwant: %q", tab.ed.Text(), want)
	}
}
