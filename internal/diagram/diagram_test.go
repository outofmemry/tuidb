package diagram

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"

	"webtuidb/internal/db"
)

func tbl(name, ref string) Table {
	t := Table{Ref: db.ObjRef{Schema: "public", Name: name, Kind: db.KindTable}, Columns: []db.Column{{Name: "id", Type: "int", PK: true}, {Name: "r_id", Type: "int"}}}
	if ref != "" {
		t.FKs = []db.ForeignKey{{Columns: []string{"r_id"}, RefSchema: "public", RefTable: ref, RefColumns: []string{"id"}}}
	}
	return t
}

func TestDiagram(t *testing.T) {
	ts := []Table{tbl("a", "b"), tbl("b", ""), tbl("c", "")}
	c := Cluster(ts)
	if c["public.a"] != c["public.b"] || c["public.c"] == c["public.a"] {
		t.Errorf("clusters: %v", c)
	}
	out := Render(ts, 80, "name", "")
	for _, w := range []string{"| a ", "| b ", "-> b (r_id)", "pk id", "fk r_id"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in\n%s", w, out)
		}
	}
	out = Render(ts, 80, "name", "A")
	if !strings.Contains(out, "| b ") || strings.Contains(out, "| c ") {
		t.Errorf("filter:\n%s", out)
	}
}

func TestRenderControlChars(t *testing.T) {
	tb := tbl("a", "")
	tb.Columns[0].Name = "x\x1b]0;pwned\x07\ny"
	if out := Render([]Table{tb}, 80, "name", ""); strings.ContainsAny(out, "\x1b\x07") || strings.Count(out, "pwned") != 1 || !strings.Contains(out, "x?]0;pwned??y") {
		t.Errorf("%q", out)
	}
}

func TestRenderWideRunesAlign(t *testing.T) {
	tb := tbl("订单", "")
	tb.Columns[0].Name = "名前"
	ls := strings.Split(Render([]Table{tb}, 80, "name", ""), "\n")
	for _, l := range ls {
		if l != "" && runewidth.StringWidth(l) != runewidth.StringWidth(ls[0]) {
			t.Errorf("ragged line %q", l)
		}
	}
}
