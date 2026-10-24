package db

import (
	"strings"
	"testing"
	"unicode"
)

// bare drops terminators and the blanks Split trims but scan keeps.
func bare(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ';' || unicode.IsSpace(r) && !strings.ContainsRune(" \t\n\r", r) {
			return -1
		}
		return r
	}, s)
}

var fuzzDialects = []string{"postgres", "mysql", "clickhouse", "mongo", "redis"}

func FuzzSplit(f *testing.F) {
	for _, s := range []string{"select 1; select 2", "DELIMITER $$\nbegin a; end$$\nDELIMITER ;\nselect 1;", "select $a$;$a$; -- c\nselect 'x;'", "/* ; */ a;b"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		for _, d := range fuzzDialects {
			custom := d == "mysql" && strings.Contains(strings.ToUpper(text), "DELIMITER")
			end := 0
			for _, s := range Split(d, text) {
				if s.Start < end || s.End < s.Start || s.End > len(text) || text[s.Start:s.End] != s.Text {
					t.Fatalf("%s: bad statement %+v in %q", d, s, text)
				}
				// Whatever lies between statements must be whitespace, terminators or comments, never code.
				if gap := text[end:s.Start]; !custom && d != "redis" && hasCode(d, bare(gap)) {
					t.Fatalf("%s: lost code %q in %q", d, gap, text)
				}
				end = s.End
			}
			if tail := text[end:]; !custom && d != "redis" && hasCode(d, bare(tail)) {
				t.Fatalf("%s: lost code %q in %q", d, tail, text)
			}
		}
	})
}

func FuzzIsReadOnly(f *testing.F) {
	for _, s := range []string{"select 1", "delete from t", "/*! drop table t */", "with a as (select 1) select * from a", "db.a.find({})"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, stmt string) {
		for _, d := range fuzzDialects {
			IsReadOnly(d, stmt)
		}
	})
}
