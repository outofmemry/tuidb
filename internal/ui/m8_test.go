package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/config"
)

func connNames(h *harness) []string {
	var out []string
	for _, r := range h.a.welcome.rows() {
		if r.header == "" {
			out = append(out, r.conn.Name)
		}
	}
	return out
}

func TestM8WelcomeSortBy(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) {
		c.Name, c.Driver, c.Tag = "A", config.MySQL, "production"
	})
	h.a.d.Conns = append(h.a.d.Conns,
		config.Connection{Name: "B", Driver: config.Postgres, Host: "h", Tag: "local"},
		config.Connection{Name: "C", Driver: config.ClickHouse, Host: "h"})
	if got := strings.Join(connNames(h), ""); got != "ABC" {
		t.Fatalf("by name: %s", got)
	}
	h.press("s")
	h.pickLabel("driver")
	if got := strings.Join(connNames(h), ""); got != "CAB" { // clickhouse, mysql, postgres
		t.Fatalf("by driver: %s", got)
	}
	if h.a.d.State.ConnectionSort() != "driver" {
		t.Fatal("the choice is remembered in state.json")
	}
	h.press("s")
	h.pickLabel("tag")
	if got := strings.Join(connNames(h), ""); got != "BAC" { // local, production, untagged
		t.Fatalf("by tag: %s", got)
	}
}

func TestM8ConnectionDetailsColour(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SSLMode = "verify-full" })
	h.connect()
	h.run("connection_details")
	h.want("TLS:       on (verify-full)")
	h.want("Fake 1.0")
	h.press("o")
	h.pickLabel("blue")
	if h.a.ws().conn.Color != "#61afef" || h.a.d.Conns[0].Color != "#61afef" {
		t.Fatalf("colour %q %q", h.a.ws().conn.Color, h.a.d.Conns[0].Color)
	}
	if saved, _ := config.LoadConnections(h.a.d.ConnPath); saved[0].Color != "#61afef" {
		t.Fatalf("saved %+v", saved[0])
	}
}

func TestM8MultiSelectDeleteAndExport(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.run("mark") // customers, then the cursor steps to orders
	h.run("mark")
	if got := h.a.ws().sb.markedRefs(); len(got) != 2 {
		t.Fatalf("marked %v", got)
	}
	h.want("✓")
	h.run("delete_item")
	h.want("Delete 2 tables?")
	setField(t, h, "confirm", "2")
	h.press("enter")
	if len(h.fake.applied) != 1 || len(h.fake.applied[0]) != 2 || h.fake.applied[0][0].Text != "-- drop customers" || h.fake.applied[0][1].Text != "-- drop orders" {
		t.Fatalf("applied %+v", h.fake.applied)
	}
	if len(h.a.ws().sb.marks) != 0 {
		t.Fatal("marks clear after the action")
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

func TestM8AutoPrettyJSON(t *testing.T) {
	old := autoPrettyJSON
	defer func() { autoPrettyJSON = old }()
	raw := `{"a":1,"b":[1,2],"c":"x,y:z"}`
	if got, ok := spacedJSON(raw); !ok || got != `{"a": 1, "b": [1, 2], "c": "x,y:z"}` {
		t.Fatalf("spaced %q", got)
	}
	if _, ok := spacedJSON("plain"); ok {
		t.Fatal("not JSON")
	}
	autoPrettyJSON = false
	if gridCellText(raw, 60) != raw {
		t.Fatal("off: unchanged")
	}
	autoPrettyJSON = true
	if got := gridCellText(raw, 60); got != `{"a": 1, "b": [1, 2], "c": "x,y:z"}` {
		t.Fatalf("wide enough: %q", got)
	}
	if got := gridCellText(raw, 10); got != raw {
		t.Fatalf("too narrow stays compact: %q", got)
	}
	if got := detailText(raw); !strings.Contains(got, "\n  \"a\": 1") {
		t.Fatalf("row detail is indented: %q", got)
	}
}

func TestM8LongRunKeepsOneSummaryTab(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	q.ed.SetText(strings.Repeat("select 1;\n", 101) + "boom;")
	h.run("run_all")
	if len(q.res) != 1 || q.res[0].title != "Summary" {
		t.Fatalf("results %d", len(q.res))
	}
	msg := q.res[0].msg
	if !strings.Contains(msg, "Statements run: 102 of 102") || !strings.Contains(msg, "Failed: 1") || !strings.Contains(msg, "Rows returned: 202") || !strings.Contains(msg, "First failure: statement 102") {
		t.Fatalf("summary %q", msg)
	}
	h.want("1 of 102 failed")
}

func TestM8LabelAndTabNameTheResultTabs(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	q.ed.SetText("-- label: Mine\nselect 1;\n-- tab: Yours\nselect 2;\n-- name: Ours\nselect 3;")
	h.run("run_all")
	var titles []string
	for _, r := range q.res {
		titles = append(titles, r.title)
	}
	if strings.Join(titles, ",") != "Mine,Yours,Ours" {
		t.Fatalf("titles %v", titles)
	}
}

func TestM8VariablesFormatPicker(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.Editor.QueryParams = true })
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	q.ed.SetText("select :a, @b")
	h.run("run_all")
	h.want("Variables")
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	if v := f.values(); v[formatKey] != "colon" || v["a"] != "" {
		t.Fatalf("starts on the configured format: %v", v)
	}
	setChoice(t, h, formatKey, "at") // @b is a variable in the at format
	h.press("enter")
	setField(t, h, "b", "5")
	h.press("enter")
	if len(h.fake.execs) != 1 || h.fake.execs[0] != "select :a, 5" {
		t.Fatalf("execs %v", h.fake.execs)
	}
	// the variables action offers whichever format finds variables
	q.ed.SetText("select ${x}")
	h.run("variables")
	if v := h.a.overlays[len(h.a.overlays)-1].(*formDlg).values(); v[formatKey] != "brace" {
		t.Fatalf("format %v", v)
	}
}

func TestM8DiagramFocusesTheTable(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.press("m")
	h.pickLabel("Diagram")
	h.want("ER diagram")
	h.want("filter: customers")
}

func TestM8StructureRowFormEditsEveryAttribute(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix(")") // structure
	h.want("column_name")
	h.press("space")
	h.want("Column id")
	setField(t, h, "name", "ident")
	setField(t, h, "type", "bigint")
	setChoice(t, h, "null", "true")
	setField(t, h, "default", "7")
	h.press("enter")
	g := h.a.ws().tab().(*tableTab).structGrid
	r := g.Rows[0]
	if cellStr(r.Cur[scName]) != "ident" || cellStr(r.Cur[scType]) != "bigint" || cellStr(r.Cur[scNull]) != "YES" || cellStr(r.Cur[scDefault]) != "7" {
		t.Fatalf("row %v", r.Cur)
	}
	if len(r.Dirty) != 4 {
		t.Fatalf("four attributes changed: %v", r.Dirty)
	}
}
