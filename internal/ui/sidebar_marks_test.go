package ui

import (
	"os"
	"path/filepath"
	"testing"

	"webtuidb/internal/config"
)

func TestM6ExportSeveralTablesAcrossSchemas(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.Export.Filename = "{table}" })
	h.connect()
	h.run("export")
	setChoice(t, h, "many", "true")
	h.press("enter")
	h.want("Export which tables?")
	h.press("down", "down", "space") // public.customers is ticked already; add the third entry (app.customers)
	h.press("enter")
	setField(t, h, "folder", dir)
	h.press("enter")
	for _, name := range []string{"public.customers.csv", "app.customers.csv"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			ents, _ := os.ReadDir(dir)
			t.Fatalf("%v (have %v)", err, ents)
		}
	}
}

func TestM8MarkedTablesExportOneFileEach(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.Export.Filename = "{table}" })
	h.connect()
	h.run("mark")
	h.run("mark")
	h.run("export")
	h.want("Export 2 tables")
	setField(t, h, "folder", dir)
	h.press("enter")
	for _, n := range []string{"public.customers.csv", "public.orders.csv"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Fatal(err)
		}
	}
}
