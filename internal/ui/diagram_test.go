package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/db"
)

func TestM13DiagramAddForeignKeyPlansAndPreviews(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("d")
	h.want("a: add FK")
	h.press("a")
	h.want("Add foreign key")
	setChoice(t, h, "table", "orders")
	setField(t, h, "cols", "id")
	setChoice(t, h, "ref", "customers")
	setField(t, h, "refcols", "id")
	setChoice(t, h, "del", "CASCADE")
	h.press("enter")
	h.want("Preview: 1 statement")
	nd := h.fake.lastNew
	if nd == nil || len(nd.ForeignKeys) != 1 || nd.ForeignKeys[0].RefTable != "customers" || nd.ForeignKeys[0].OnDelete != "CASCADE" {
		t.Fatalf("planned %+v", nd)
	}
	if len(h.fake.applied) != 0 {
		t.Fatal("nothing is sent before the preview is accepted")
	}
	h.press("enter")
	if len(h.fake.applied) != 1 {
		t.Fatalf("applied %+v", h.fake.applied)
	}
}

func TestM13DiagramColourIsRemembered(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("d")
	h.press("c")
	h.pickLabel("customers")
	h.pickLabel("red")
	if got := h.a.d.State.DiagramColors("shop")["public.customers"]; got != "203" {
		t.Fatalf("colours %v", got)
	}
}

// P2-74: one vocabulary (the ANSI code) for the sidebar and the diagram; names and hex migrate on read.
func TestM16DiagramColoursShareOneVocabulary(t *testing.T) {
	for in, want := range map[string]string{"red": "203", "203": "203", "#e06c75": "203", "#61afef": "39", "": "", "nonsense": ""} {
		if got := colourCode(in); got != want {
			t.Errorf("colourCode(%q) = %q, want %q", in, got, want)
		}
	}
	h := newHarness(t, nil)
	h.connect()
	h.a.d.State.SetDiagramColors("shop", map[string]string{"public.a": "red", "public.b": "#98c379", "public.c": "208"})
	got := h.a.ws().diagramColors()
	if got["public.a"] != "203" || got["public.b"] != "41" || got["public.c"] != "208" {
		t.Errorf("migrated %v", got)
	}
	// the sidebar's colour for a table is what the diagram tints with
	s := h.a.ws().sb
	if c := s.itemColour(db.ObjRef{Schema: "public", Name: "a"}); c != "203" {
		t.Errorf("sidebar colour %q", c)
	}
}

func TestM3DiagramRendersAndFilters(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("d")
	if _, ok := h.a.overlays[len(h.a.overlays)-1].(*paneDlg); !ok {
		t.Fatalf("no diagram:\n%s", h.screen())
	}
	if v := topView(h); !strings.Contains(v, "customers") || !strings.Contains(v, "orders") || !strings.Contains(v, "pk id") {
		t.Fatalf("diagram:\n%s", v)
	}
	h.press("/")
	setField(t, h, "f", "ord")
	h.press("enter")
	if v := topView(h); strings.Contains(v, "customers") || !strings.Contains(v, "orders") || !strings.Contains(v, "filter: ord") {
		t.Fatalf("filtered diagram:\n%s", v)
	}
	h.press("n") // hands over to the existing new-table flow
	h.want("New table")
}

func TestM8DiagramFocusesTheTable(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.press("m")
	h.pickLabel("Diagram")
	h.want("ER diagram")
	h.want("filter: customers")
}
