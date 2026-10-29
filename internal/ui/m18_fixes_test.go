package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"webtuidb/internal/config"
)

// T-P2-06: the table frame hint follows a [keys] rebinding instead of a hard-coded "C-d s".
func TestM18FrameHintFollowsKeys(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.KeyOverrides = map[string][]string{"commit": {"F9"}} })
	h.connect()
	h.openCustomers()
	h.want("C-d F9")
	h.wantNot("C-d s ")
}

// T-P3-03: dialog prose breaks at spaces, not inside words or numbers.
func TestM18WordWrapKeepsWords(t *testing.T) {
	s := "Rows are identified by all of their values, so every row identical to an edited or deleted one is changed. Error 1045 SQLSTATE 23514"
	words := map[string]bool{}
	for _, w := range strings.Fields(s) {
		words[w] = true
	}
	for _, l := range wordWrap(s, 23) {
		if width(l) > 23 {
			t.Errorf("line too wide: %q", l)
		}
		for _, w := range strings.Fields(l) {
			if !words[w] {
				t.Errorf("word cut: %q in %q", w, l)
			}
		}
	}
}

// T-P3-11: Ctrl+C outside the editor says how to quit; the lock screen offers and honours the quit key.
func TestM18CtrlCHintAndLockQuit(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.press("ctrl+c")
	h.want("Quit with C-d q")

	hash, err := config.HashPasscode("1234")
	if err != nil {
		t.Fatal(err)
	}
	h.a.d.Cfg.Security.Passcode = hash
	h.pump(h.a.lock())
	h.want("C-d q  quit") // the box footer draws "key  what" pairs
	h.press("ctrl+d", "q")
	if !h.a.quit {
		t.Fatal("C-d q on the lock screen must quit")
	}
}

// T-P3-16: [theme.colors] entries that do nothing are reported.
func TestM18ThemeColorsWarn(t *testing.T) {
	e := colorsError(map[string]string{"accent": "notacolor", "nope": "#fff", "keyword": "#abc", "number": "208", "tag.mine": "#123456"})
	if !strings.Contains(e, `accent = "notacolor" is not a color`) || !strings.Contains(e, "nope is not a theme color") {
		t.Errorf("got %q", e)
	}
	if strings.Contains(e, "keyword") || strings.Contains(e, "number") || strings.Contains(e, "tag.mine") {
		t.Errorf("valid entries reported: %q", e)
	}
}

// T-P3-19: JSON copy keeps the column order (and stays valid JSON).
func TestM18JSONCopyKeepsColumnOrder(t *testing.T) {
	g := &Grid{W: 60, H: 8}
	g.SetData([]gridCol{{Name: "name"}, {Name: "city"}}, [][]any{{"Ann", "Paris"}, {"Bob", nil}})
	g.Move(1, 0, true)
	got := g.CopyRows(FmtJSON, "t")
	if strings.Index(got, `"name"`) > strings.Index(got, `"city"`) {
		t.Errorf("keys reordered:\n%s", got)
	}
	var docs []map[string]any
	if err := json.Unmarshal([]byte(got), &docs); err != nil || len(docs) != 2 || docs[1]["city"] != nil {
		t.Errorf("invalid JSON %v:\n%s", err, got)
	}
}
