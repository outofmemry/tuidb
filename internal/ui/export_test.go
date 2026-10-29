package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/export"
)

func TestExportUsesAppliedFiltersOnly(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	tt := h.a.ws().tab().(*tableTab)
	tt.filters = db.FilterSet{Filters: []db.Filter{{Column: "name", Op: "=", Value: "Ann", Enabled: true}}}
	if n := len(tt.applied().Active()); n != 0 {
		t.Fatalf("typed filter leaked into applied(): %d active", n)
	}
	tt.markApplied()
	tt.filters.Filters[0].Value = "Ann typed but not applied"
	if got := tt.applied().Filters[0].Value; got != "Ann" {
		t.Fatalf("applied = %q", got)
	}
}

// P2-62
func TestTUI1ExportNameKeepsOneExtension(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.a.d.Cfg.Export.Filename = "{table}.csv"
	if got := h.a.ws().exportName("users", "csv"); got != "users.csv" {
		t.Fatalf("got %q", got)
	}
	if got := h.a.ws().exportName("users", "json"); got != "users.csv.json" {
		t.Fatalf("got %q", got)
	}
}

// P2-64: export-filtered opens the dialog on an unfiltered table too.
func TestTUI1ExportFilteredWithoutFilters(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.pump(h.a.ws().tab().(*tableTab).exportFiltered())
	h.want("Export customers")
}

func TestM2ExportWritesCSV(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix("E")
	h.want("Export customers")
	path := filepath.Join(t.TempDir(), "out.csv")
	setField(t, h, "path", path)
	h.press("enter")
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "id,name,email") || !strings.Contains(string(b), "Ann,ann@x.io") {
		t.Fatalf("export file %q: %v", b, err)
	}
	h.want("exported 3 rows")
}

func TestM4ExportAllResultTabs(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	queryResult(h, "select * from customers; select 2")
	h.prefix("E")
	h.fakeFormChoice("all", "true")
	dir := t.TempDir()
	setField(t, h, "path", filepath.Join(dir, "out.csv"))
	h.wait = 300 * time.Millisecond
	h.press("enter")
	for _, n := range []string{"out-1.csv", "out-2.csv"} {
		if b, err := os.ReadFile(filepath.Join(dir, n)); err != nil || len(b) == 0 {
			t.Fatalf("%s: %q %v", n, b, err)
		}
	}
	if numberedPath("a/out.csv.gz", 3) != "a/out-3.csv.gz" {
		t.Error("numberedPath must keep a compound extension")
	}
}

func TestM6CSVQuoteModeAndWarning(t *testing.T) {
	old := copyCSV
	defer func() { copyCSV = old }()
	copyCSV = config.CSV{Delimiter: ",", Quote: `"`, LineBreak: "\n", QuoteMode: "always"}
	if got := csvLine([]string{"a", "", "b c"}); got != `"a",,"b c"`+"\n" {
		t.Fatalf("always: %q", got)
	}
	copyCSV.QuoteMode = "needed"
	if got := csvLine([]string{"a", "b,c"}); got != `a,"b,c"`+"\n" {
		t.Fatalf("needed: %q", got)
	}
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.CSV.Delimiter, c.CSV.Decimal = ",", "," })
	h.connect()
	h.openCustomers()
	h.run("export")
	h.want("delimiter and decimal separator")
	if export.CheckCSV(h.a.d.Cfg.CSV) == "" {
		t.Fatal("the config must trigger the warning")
	}
}

func TestM9ExportFileNamePattern(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	ws := h.a.ws()
	if got := ws.expandPath("/tmp/out/{table}-x.csv", "orders"); got != "/tmp/out/orders-x.csv" {
		t.Errorf("expandPath: %q", got)
	}
	if got := ws.expandPath("/tmp/{nope}/a.csv", "orders"); !strings.Contains(got, "/a.csv") {
		t.Errorf("only the file name is expanded: %q", got)
	}
}
