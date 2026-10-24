package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

// setField fills a text field of the dialog on top, like typing it.
func setField(t *testing.T, h *harness, key, val string) {
	t.Helper()
	f, ok := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	if !ok {
		t.Fatalf("no form on top:\n%s", h.screen())
	}
	for _, fl := range f.fields {
		if fl.Key == key {
			fl.edit = newLine(val)
			return
		}
	}
	t.Fatalf("no field %q", key)
}

func queryResult(h *harness, sql string) {
	h.prefix("e")
	h.typeText(sql)
	h.prefix("alt+enter")
	h.prefix("alt+j") // focus the results
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

func TestM2ChartToggles(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	queryResult(h, "select * from customers")
	h.prefix("g")
	h.want("bar chart")
	h.want("█")
	h.prefix("g")
	h.wantNot("bar chart")
}

func TestM2VariablesBindParam(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.Editor.QueryParams = true })
	h.connect()
	h.prefix("e")
	h.typeText("select :id")
	h.prefix("alt+enter")
	h.want("Variables")
	h.typeText("7")
	h.press("enter")
	if len(h.fake.execs) != 1 || !strings.Contains(h.fake.execs[0], "7") || strings.Contains(h.fake.execs[0], ":id") {
		t.Fatalf("execs %v", h.fake.execs)
	}
	h.prefix("alt+enter") // the value is remembered
	h.want("Variables")
	h.want("7")
}

func TestM2SearchOpensFilteredTable(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("/")
	h.typeText("Ann")
	h.press("enter")
	h.want("customers")
	h.press("enter")
	tt, ok := h.a.ws().tab().(*tableTab)
	if !ok || len(tt.filters.Filters) != 1 || tt.filters.Filters[0].Value != "Ann" || !tt.showFilters {
		t.Fatalf("tab %#v", h.a.ws().tab())
	}
	if len(h.fake.lastQuery.Filters.Filters) != 1 {
		t.Errorf("rows were not loaded with the filter: %+v", h.fake.lastQuery)
	}
}

func TestM2ProcessListKills(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.procs = []db.ProcessInfo{{ID: "42", User: "app", DB: "shop", State: "active", Elapsed: time.Second, Query: "select pg_sleep(99)"}}
	h.connect()
	h.prefix(".")
	h.want("Processes")
	h.want("pg_sleep")
	h.press("k")
	h.want("Kill process 42")
	h.press("y")
	if len(h.fake.killed) != 1 || h.fake.killed[0] != "42" {
		t.Fatalf("killed %v\n%s", h.fake.killed, h.screen())
	}
	h.want("no processes")
}

func TestM2BeautifyChangesEditor(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select a,b from t where x=1")
	before := h.a.ws().tab().(*queryTab).ed.Text()
	h.prefix("i")
	after := h.a.ws().tab().(*queryTab).ed.Text()
	if after == before || !strings.Contains(after, "\n") {
		t.Fatalf("beautify left %q", after)
	}
	h.prefix("I") // uglify joins it again
	if got := h.a.ws().tab().(*queryTab).ed.Text(); strings.Contains(got, "\n") {
		t.Fatalf("uglify left %q", got)
	}
}

func TestM2CopyRowsAsFromResults(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	queryResult(h, "select * from customers")
	h.prefix("alt+c")
	h.want("Markdown Table")
	h.press("down", "down", "down") // Plain, JSON, HTML, Markdown
	h.press("enter")
	h.want("copied rows")
	g := h.a.ws().tab().(*queryTab).curResult().grid
	if got := g.CopyRows(FmtMarkdown, "result"); !strings.HasPrefix(got, "| id | name | email |") {
		t.Errorf("markdown %q", got)
	}
}

func TestM2FindReplaceInEditor(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.typeText("select a from a")
	h.prefix("alt+f")
	setField(t, h, "q", "A")
	setField(t, h, "r", "z")
	h.press("enter")
	if got := h.a.ws().tab().(*queryTab).ed.Text(); got != "select z from z" {
		t.Fatalf("got %q", got)
	}
}

func TestM2ThemeTOML(t *testing.T) {
	out := ThemeTOML(config.Theme{Name: "light", Colors: map[string]string{"accent": "#123456"}})
	if !strings.HasPrefix(out, "[theme.colors]\n") || !strings.Contains(out, `"accent" = "#123456"`) || !strings.Contains(out, `"tag.local"`) {
		t.Fatalf("theme toml:\n%s", out)
	}
}

func TestM2ImportCSVRunsInserts(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	path := filepath.Join(t.TempDir(), "in.csv")
	os.WriteFile(path, []byte("id,name\n9,Zed\n"), 0o644)
	h.prefix("I")
	h.want("Import data")
	setField(t, h, "path", path)
	h.press("enter")
	h.want("Column mapping")
	h.press("enter")
	h.want("processed 1 rows")
	if len(h.fake.applied) == 0 {
		t.Fatal("nothing was applied")
	}
}

func TestM2AppendModeKeepsEarlierResults(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.Editor.ResultMode = "append" })
	h.connect()
	h.prefix("e")
	h.typeText("select 1")
	h.prefix("alt+enter")
	h.prefix("alt+enter")
	if n := len(h.a.ws().tab().(*queryTab).res); n != 2 {
		t.Fatalf("results %d", n)
	}
}

func TestM2CommitLogsPreimage(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.General.LogOriginalValues = true })
	h.connect()
	h.openCustomers()
	h.press("down", "right", "e")
	h.typeText("X")
	h.press("enter")
	h.prefix("s")
	es, _ := h.a.d.Hist.List("shop", "", 10)
	if len(es) == 0 || !strings.Contains(es[0].Original, "update") {
		t.Fatalf("history %+v\n%s", es, h.screen())
	}
}
