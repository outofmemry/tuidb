package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/config"
)

func TestM2BeautifyChangesEditor(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select a,b from t where x=1")
	before := h.a.ws().tab().(*queryTab).ed.Text()
	h.prefix("i")
	after := h.a.ws().tab().(*queryTab).ed.Text()
	if after == before || !strings.Contains(after, "\n") {
		t.Fatalf("beautify left %q", after)
	}
	h.prefix("I") // uglify joins it again
	if got := h.a.ws().tab().(*queryTab).ed.Text(); strings.Contains(got, "\n") {
		t.Fatalf("uglify left %q", got)
	}
}

func TestM3EditorOccurrencesAndInvisibles(t *testing.T) {
	e := NewEditor("postgres", EditorOpts{})
	e.SetText("foo bar\tfoo")
	e.W, e.H = 40, 2
	if msg := e.extra("select_all_occurrences"); !strings.Contains(msg, "2 occurrence(s) of \"foo\"") {
		t.Fatalf("msg %q", msg)
	}
	e.marks, e.anchor, e.cur = "", nil, pos{}
	if e.extra("select_next_occurrence"); e.Selection() != "foo" {
		t.Fatalf("first press selects the word, got %q", e.Selection())
	}
	e.extra("select_next_occurrence")
	if e.cur.col != 11 {
		t.Fatalf("second press jumps to the next match: %+v", e.cur)
	}
	e.extra("invisibles")
	th := NewTheme(config.Theme{})
	if v := stripANSI(e.View(th, true)); !strings.Contains(v, "foo·bar→foo") {
		t.Fatalf("view %q", v)
	}
}

func TestM5EditorTransforms(t *testing.T) {
	e := NewEditor("postgres", EditorOpts{})
	e.SetText("select name from t")
	e.anchor, e.cur = &pos{0, 7}, pos{0, 11}
	e.transform("upper_case")
	if e.Text() != "select NAME from t" || e.Selection() != "NAME" {
		t.Fatalf("%q sel %q", e.Text(), e.Selection())
	}
	e.transform("capitalize")
	if e.Text() != "select Name from t" {
		t.Fatalf("%q", e.Text())
	}
	e.transform("lower_case")
	if e.Text() != "select name from t" {
		t.Fatalf("%q", e.Text())
	}
}

func TestM5TransformWordUnderCursor(t *testing.T) {
	e := NewEditor("postgres", EditorOpts{})
	e.SetText("select name from t")
	e.cur, e.anchor = pos{0, 9}, nil
	e.transform("upper_case")
	if e.Text() != "select NAME from t" {
		t.Fatalf("%q", e.Text())
	}
	e.SetText("a  b")
	e.cur, e.anchor = pos{0, 2}, nil // between two spaces: no word
	if msg := e.transform("upper_case"); msg == "" || e.Text() != "a  b" {
		t.Fatalf("%q %q", msg, e.Text())
	}
}

func TestM9ReformatStatementUnderCursor(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.run("new_query_tab")
	q := h.queryTab()
	q.ed.Replace("select 1 from a;\nselect 2 from b;")
	q.ed.SelectBytes(20, 20) // inside the second statement, nothing selected
	h.pump(q.reformat(true))
	got := q.ed.Text()
	if !strings.HasPrefix(got, "select 1 from a;\n") {
		t.Fatalf("first statement touched: %q", got)
	}
	if rest := got[len("select 1 from a;\n"):]; rest == "select 2 from b;" {
		t.Fatalf("second statement not formatted: %q", got)
	}
}
