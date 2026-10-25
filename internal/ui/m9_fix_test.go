package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/db"
)

func TestReplaceInFoldedLengthAndSkips(t *testing.T) {
	// Kelvin sign lowercases to a shorter byte sequence; prefix/suffix must not slice out of range.
	k := "K" + "x"
	for _, op := range []string{FindPrefix, FindSuffix} {
		s := FindSpec{Text: "kx", Op: op}
		if op == FindSuffix {
			k = "a" + "K" + "x"
			s.Text = "kx"
		}
		got, ok := s.replaceIn(k, "Z")
		if want := map[string]string{FindPrefix: "Z", FindSuffix: "aZ"}[op]; !ok || got != want {
			t.Errorf("%s: %q %v, want %q", op, got, ok, want)
		}
	}
	if _, ok := (FindSpec{Text: "kxy", Op: FindPrefix}).replaceIn("Kx", "Z"); ok {
		t.Error("longer text must not match")
	}

	g := &Grid{Editable: true, Cols: []gridCol{{Name: "id", Type: "int", PK: true}, {Name: "n", Type: "text"}}}
	g.SetData(g.Cols, [][]any{{"1a", "1b"}})
	g.Editable = true
	n, sk := g.ReplaceSkip(FindSpec{Text: "1", Op: FindContains}, "2", true)
	if n != 1 || sk != 1 || g.Rows[0].Cur[0] != "1a" || g.Rows[0].Cur[1] != "2b" {
		t.Errorf("any column: n=%d skipped=%d %v", n, sk, g.Rows[0].Cur)
	}
	if n, _ := g.ReplaceSkip(FindSpec{Text: "1", Col: "id", Op: FindContains}, "9", true); n != 1 {
		t.Errorf("a chosen column is edited: %d", n)
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

func TestFindSuffixMultibyte(t *testing.T) {
	for _, c := range []struct{ cell, suf string }{{"xé", "é"}, {"a日本", "日本"}} {
		if n, ok := (FindSpec{Text: c.suf, Op: FindSuffix, Case: true}).endLen(c.cell, false); !ok || n != len(c.suf) {
			t.Errorf("%q suffix %q: %d %v", c.cell, c.suf, n, ok)
		}
	}
}

func TestTypeChoicesDoublePrecisionIsOneEntry(t *testing.T) {
	for _, s := range typeChoices(db.Capabilities{}) {
		if strings.Contains(s, `\`) {
			t.Errorf("stray backslash in %q", s)
		}
	}
}
