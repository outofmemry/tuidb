package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func TestM5ConnectionsExportImport(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.Group = "work" })
	path := filepath.Join(t.TempDir(), "c.wtdb")
	h.press("x")
	setChoice(t, h, "scope", "work")
	setField(t, h, "path", path)
	setField(t, h, "pass", "s3cret")
	h.press("enter")
	b, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(b), "shop") {
		t.Fatalf("export must be encrypted: %v %q", err, b)
	}
	h.press("I")
	setField(t, h, "path", path)
	setField(t, h, "pass", "s3cret")
	h.press("enter")
	if n := len(h.a.d.Conns); n != 2 || h.a.d.Conns[1].Name != "shop (imported)" || h.a.d.Conns[1].Password != "" {
		t.Fatalf("conns after import: %+v", h.a.d.Conns)
	}
	if saved, _ := config.LoadConnections(h.a.d.ConnPath); len(saved) != 2 {
		t.Fatalf("import must be saved, file has %d", len(saved))
	}
}

func TestM5OpenTargetFiltersTable(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) {
		c.Open = &config.OpenTarget{Table: "customers", Column: "name", Operation: "eq", Value: "Bob"}
	})
	h.connect()
	fs := h.fake.lastQuery.Filters
	if len(fs.Filters) != 1 || fs.Filters[0] != (db.Filter{Column: "name", Op: "=", Value: "Bob", Enabled: true}) {
		t.Fatalf("filters %+v", fs)
	}
	if h.a.ws().tab() == nil || h.a.ws().tab().Title() != "customers" {
		t.Fatal("the table must be open")
	}
}
