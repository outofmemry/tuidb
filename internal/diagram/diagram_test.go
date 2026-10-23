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

type schemaSess struct {
	db.Session
	on bool
}

func (s schemaSess) Caps() db.Capabilities { return db.Capabilities{Schemas: s.on} }
func (s schemaSess) Schemas(context.Context) ([]string, error) {
	return []string{"public", "audit"}, nil
}

func TestSchemasWithFKColumnNames(t *testing.T) {
	if got, err := Schemas(context.Background(), schemaSess{on: true}); err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	if got, _ := Schemas(context.Background(), schemaSess{}); got != nil {
		t.Fatal("no schemas expected")
	}
	def := db.TableDef{Name: "a", ForeignKeys: make([]db.ForeignKey, 1, 1)}
	n := WithFK(def, db.ForeignKey{Name: "x"})
	if len(n.ForeignKeys) != 2 || len(def.ForeignKeys) != 1 || n.ForeignKeys[1].Name != "x" {
		t.Fatal(n, def)
	}
	if got := ColumnNames(tbl("a", "")); len(got) != 2 || got[1] != "r_id" {
		t.Fatal(got)
	}
}

type failSchemas struct{ schemaSess }

func (failSchemas) Schemas(context.Context) ([]string, error) {
	return nil, errors.New("permission denied")
}

func TestSchemasReturnsTheError(t *testing.T) { // P2-57
	if _, err := Schemas(context.Background(), failSchemas{schemaSess{on: true}}); err == nil {
		t.Fatal("the error was swallowed")
	}
}

func TestWithFKSkipsDuplicates(t *testing.T) { // P2-58
	have := db.ForeignKey{Name: "fk_a", Columns: []string{"r_id"}, RefSchema: "public", RefTable: "b", RefColumns: []string{"id"}}
	def := db.TableDef{Name: "a", ForeignKeys: []db.ForeignKey{have}}
	same := have
	same.Name = "other_name" // the same key under a new name
	byName := db.ForeignKey{Name: "fk_a", Columns: []string{"x"}, RefTable: "c", RefColumns: []string{"y"}}
	for _, fk := range []db.ForeignKey{same, byName} {
		if n := WithFK(def, fk); len(n.ForeignKeys) != 1 {
			t.Errorf("%+v was added again", fk)
		}
	}
	other := db.ForeignKey{Name: "fk_b", Columns: []string{"r_id"}, RefSchema: "public", RefTable: "c", RefColumns: []string{"id"}}
	if n := WithFK(def, other); len(n.ForeignKeys) != 2 {
		t.Error("a different FK must be added")
	}
}
