package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestM4QuickLookHexAndEncoding(t *testing.T) {
	if b, err := parseHex("de AD\nbe ef"); err != nil || string(b) != "\xde\xad\xbe\xef" {
		t.Fatalf("parseHex %x %v", b, err)
	}
	if _, err := parseHex("abc"); err == nil {
		t.Fatal("an odd digit count must be refused")
	}
	if decodeAs([]byte{0xe9}, "Latin-1 (ISO-8859-1)") != "é" || decodeAs([]byte{'h', 0, 'i', 0}, "UTF-16") != "hi" || decodeAs([]byte{0xfe, 0xff, 0, 'h'}, "UTF-16") != "h" {
		t.Fatal("decoding")
	}
	h := newHarness(t, nil)
	var saved []byte
	q := h.a.newQuickLookValue("blob", []byte{1, 2}, func(b []byte) tea.Cmd { saved = b; return nil })
	h.a.push(q)
	h.press("e")
	setField(t, h, "hex", "0a 0b 0c")
	h.press("enter")
	if string(saved) != "\x0a\x0b\x0c" {
		t.Fatalf("saved %x", saved)
	}
	h.press("v")
	h.want("View using encoding")
}

func TestM4ItemOverview(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.run("item_overview")
	h.want("Overview: customers")
	h.want("Columns (3)")
	h.want("customers_pkey")
	h.want("3 rows")
}

func TestM6QuickLookSaveAs(t *testing.T) {
	h := newHarness(t, nil)
	q := h.a.newQuickLookValue("blob", []byte{0xde, 0xad, 0xbe, 0xef}, nil)
	h.a.push(q)
	h.press("s")
	path := filepath.Join(t.TempDir(), "cell.bin")
	setField(t, h, "path", path)
	h.press("enter")
	if b, err := os.ReadFile(path); err != nil || string(b) != "\xde\xad\xbe\xef" {
		t.Fatalf("%v %x", err, b)
	}
}

func TestM9QuickLookMinify(t *testing.T) {
	q := newQuickLook("doc", "{\n  \"a\": 1\n}")
	q.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if !q.raw || q.shown() != `{"a":1}` {
		t.Errorf("m shows the raw text minified: %q", q.shown())
	}
	q.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if !strings.Contains(q.shown(), "\n") {
		t.Error("m again pretty-prints")
	}
}
