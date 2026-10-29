package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTUI3SetColumnSkipsUnchanged(t *testing.T) {
	g := &Grid{Cols: []gridCol{{Name: "a"}}, Editable: true}
	g.Rows = []*gridRow{
		{Cur: []any{"x"}, Orig: []any{"x"}, Dirty: map[int]bool{}},
		{Cur: []any{"y"}, Orig: []any{"y"}, Dirty: map[int]bool{}},
	}
	g.anchorRow, g.row, g.selecting = 0, 1, true
	if n := g.SetColumn("x"); n != 1 {
		t.Fatalf("changed %d, want 1", n)
	}
	g.Undo()
	g.Rows[1].Cur[0] = "x"
	if n := g.SetColumn("x"); n != 0 {
		t.Fatalf("changed %d for an unchanged value", n)
	}
	if g.HasPending() {
		t.Fatal("an unchanged value staged something")
	}
}

func TestM4SetValueDateAndFile(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	g := h.a.ws().tab().(*tableTab).grid
	h.a.pickSetValue(g, setValueDate{})
	setField(t, h, "date", "2026-01-02")
	h.press("enter")
	if v, _ := g.CursorValue(); v != "2026-01-02" {
		t.Fatalf("date cell %v", v)
	}
	h.a.pickSetValue(g, setValueDate{})
	setField(t, h, "date", "nope")
	h.press("enter")
	h.want("date must look like")
	h.press("esc")
	file := filepath.Join(t.TempDir(), "f.bin")
	os.WriteFile(file, []byte{1, 2, 3}, 0o600)
	h.a.pickSetValue(g, setValueFile{})
	setField(t, h, "path", file)
	h.press("enter")
	if v, _ := g.CursorValue(); string(v.([]byte)) != "\x01\x02\x03" {
		t.Fatalf("file cell %v", v)
	}
}
