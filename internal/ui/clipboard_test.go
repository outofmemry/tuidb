package ui

import (
	"strings"
	"testing"
)

func TestM2CopyRowsAsFromResults(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	queryResult(h, "select * from customers")
	h.prefix("alt+c")
	h.want("Markdown Table")
	h.press("down", "down", "down") // Plain, JSON, HTML, Markdown
	h.press("enter")
	h.want("copied rows")
	g := h.a.ws().tab().(*queryTab).curResult().grid
	if got := g.CopyRows(FmtMarkdown, "result"); !strings.HasPrefix(got, "| id | name | email |") {
		t.Errorf("markdown %q", got)
	}
}

func TestClipboardCSVNeutralizesFormulas(t *testing.T) {
	if got := csvLine([]string{"=1+1", "ok", "@x"}); got != "'=1+1,ok,'@x\n" {
		t.Fatalf("csvLine = %q", got)
	}
}

func TestClipboardKeepsNumbersAndRoundTripsFormulas(t *testing.T) {
	g := &Grid{Cols: []gridCol{{Name: "-id"}, {Name: "n"}, {Name: "t"}}}
	g.SetData(g.Cols, [][]any{{int64(-42), -1.5, `=HYPERLINK("x")`}})
	got := g.FormatRect(0, 0, 0, 2, FmtCSVHeader, "t")
	if want := "'-id,n,t\n-42,-1.5,\"'=HYPERLINK(\"\"x\"\")\""; got != want {
		t.Fatalf("csv = %q, want %q", got, want)
	}
	plainOut := g.FormatRect(0, 0, 0, 2, FmtPlain, "t")
	if plainOut != "-42\t-1.5\t'=HYPERLINK(\"x\")" {
		t.Fatalf("plain = %q", plainOut)
	}
	if v := clipValue("'=1+1"); v != "=1+1" {
		t.Errorf("paste strips one quote: %v", v)
	}
	if v := clipValue("''=1"); v != "''=1" {
		t.Errorf("only before a formula char: %v", v)
	}
	if v := clipValue("'abc"); v != "'abc" {
		t.Errorf("ordinary quote kept: %v", v)
	}
}
