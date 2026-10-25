package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func tabNames(h *harness) []string {
	var out []string
	for _, t := range h.a.ws().tabs {
		out = append(out, h.a.ws().tabTitle(t))
	}
	return out
}

func TestM4TabMenu(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.prefix("e")
	h.prefix("e")
	if n := len(h.a.ws().tabs); n != 3 {
		t.Fatalf("%d tabs, want 3", n)
	}
	h.prefix("alt+n") // rename the active (second editor) tab
	setField(t, h, "name", "scratch")
	h.press("enter")
	h.want("scratch")
	h.press("ctrl+d", "alt+p") // unrelated prefix key must not disturb the name
	h.press("esc")
	if got := tabNames(h); got[2] != "scratch" {
		t.Fatalf("names %v", got)
	}
	h.a.ws().selectTab(1)
	h.run("close_tabs_right")
	if n := len(h.a.ws().tabs); n != 2 {
		t.Fatalf("close_tabs_right left %d tabs", n)
	}
	h.run("close_other_tabs")
	if got := tabNames(h); len(got) != 1 {
		t.Fatalf("close_other_tabs left %v", got)
	}
	h.run("close_all_tabs")
	if n := len(h.a.ws().tabs); n != 0 {
		t.Fatalf("close_all_tabs left %d tabs", n)
	}
}

func TestM4CloseOthersAsksAboutUnsavedEdits(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.a.ws().tab().(*tableTab).grid.SetCell("zed") // a pending edit: the table tab is dirty
	h.prefix("e")
	h.run("close_other_tabs")
	h.want("unsaved changes")
	h.press("esc")
	if n := len(h.a.ws().tabs); n != 2 {
		t.Fatalf("declining must keep both tabs, have %d", n)
	}
}

func TestM4AutoSaveTabsRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want int
	}{{"first", 1}, {"all", 2}, {"none", 0}} {
		h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.Editor.AutoSaveTabs = tc.mode })
		h.connect()
		h.prefix("e")
		h.typeText("select 1")
		h.prefix("e")
		h.typeText("select 2")
		h.run("disconnect")
		h.press("enter")
		if h.a.ws() != nil {
			t.Fatalf("%s: still connected", tc.mode)
		}
		if got := len(h.a.d.State.OpenTabs("shop")); got != tc.want {
			t.Fatalf("%s: stored %d tabs, want %d", tc.mode, got, tc.want)
		}
		h.a.inWS = false
		h.connect()
		var texts []string
		for _, tb := range h.a.ws().tabs {
			if q, ok := tb.(*queryTab); ok {
				texts = append(texts, q.ed.Text())
			}
		}
		if len(texts) != tc.want || tc.want > 0 && texts[0] != "select 1" {
			t.Fatalf("%s: restored %v", tc.mode, texts)
		}
	}
}

func TestM4SortByTwoColumns(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.run("sort_columns")
	h.want("Sort by")
	h.press("down", "space", "down", "space", "enter") // name, email
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	f.fields[1].Value = "desc"
	h.press("enter")
	want := []db.Sort{{Column: "name"}, {Column: "email", Desc: true}}
	if got := h.fake.lastQuery.Sort; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("sort %+v, want %+v", got, want)
	}
}

func TestM4HideAndShowColumns(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("right") // the name column
	h.run("hide_column")
	if got := h.fake.lastQuery.Columns; strings.Join(got, ",") != "id,email" {
		t.Fatalf("columns %v", got)
	}
	h.run("show_all_columns")
	if got := h.fake.lastQuery.Columns; len(got) != 0 {
		t.Fatalf("columns %v after show all", got)
	}
}

func TestM4FilterColumnsSortedByName(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.Table.FilterColumnSort = "name" })
	h.connect()
	h.openCustomers()
	tab := h.a.ws().tab().(*tableTab)
	var names []string
	for _, c := range tab.filterColumns() {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "email,id,name" {
		t.Fatalf("filter columns %v", names)
	}
}

func TestM4HistorySaveAndImport(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	queryResult(h, "select 1")
	if es, _ := h.a.d.Hist.List("shop", "", 0); len(es) == 0 {
		t.Fatal("no history to export")
	}
	out := filepath.Join(t.TempDir(), "h.json")
	h.press("esc", "ctrl+d", "alt+h") // back to the sidebar
	h.a.ws().focusSB = true
	h.a.ws().sb.switchTab(2)
	h.press("S")
	setField(t, h, "path", out)
	h.fakeFormChoice("format", "json")
	h.press("enter")
	if b, err := os.ReadFile(out); err != nil || !strings.Contains(string(b), "select 1") {
		t.Fatalf("export file %q: %v", b, err)
	}
	before, _ := h.a.d.Hist.List("shop", "", 0)
	h.press("i")
	setField(t, h, "path", out)
	h.press("enter")
	after, _ := h.a.d.Hist.List("shop", "", 0)
	if len(after) != len(before) { // the same entries again are skipped, not duplicated
		t.Fatalf("import: %d entries, want %d", len(after), len(before))
	}
}

func (h *harness) fakeFormChoice(key, val string) {
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	for _, fl := range f.fields {
		if fl.Key == key {
			fl.Value = val
		}
	}
}

func TestM4ThemeNamesAndFile(t *testing.T) {
	for _, n := range []string{"Silver Dark", "jade-dark", "polaris_dark", "sunset-dark", "sunset-light"} {
		if namedPalettes[themeName(n)] == nil {
			t.Errorf("no palette for %q", n)
		}
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "mine.toml")
	if err := os.WriteFile(file, []byte("[theme.colors]\naccent = \"#010203\"\nkeyword = \"#aabbcc\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pal := palette(config.Theme{Name: "jade-dark", File: file, Colors: map[string]string{"keyword": "#ffffff"}})
	if pal["accent"] != "#010203" || pal["keyword"] != "#ffffff" || pal["string"] != namedPalettes["jade-dark"]["string"] {
		t.Fatalf("file colors must sit on the named palette, under [theme.colors]: %v", pal)
	}
	if themeError(config.Theme{File: filepath.Join(dir, "missing.toml")}) == "" {
		t.Error("a missing theme file must be reported")
	}
	termDark = false
	defer func() { termDark = true }()
	if palette(config.Theme{Name: "auto"})["accent"] != lightPalette["accent"] {
		t.Error("auto must follow a light terminal")
	}
}

func TestM4NoticesShowInMessageViewAndConsole(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	queryResult(h, "raise")
	h.want("first notice")
	h.want("second notice")
	h.press("m")
	h.want("m: back to the rows")
	h.press("m")
	var lines []string
	for _, l := range h.a.ws().console.Lines() {
		lines = append(lines, l.SQL)
	}
	if got := strings.Join(lines, "|"); !strings.Contains(got, "-- NOTICE: first notice") || !strings.Contains(got, "-- NOTICE: second notice") {
		t.Fatalf("console %q", got)
	}
	h.run("clear_console")
	if n := len(h.a.ws().console.Lines()); n != 0 {
		t.Fatalf("clear left %d lines", n)
	}
}

func TestM4MessageViewSavesText(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	queryResult(h, "raise")
	h.press("m", "s")
	out := filepath.Join(t.TempDir(), "m.txt")
	setField(t, h, "path", out)
	h.press("enter")
	if b, _ := os.ReadFile(out); !strings.Contains(string(b), "second notice") {
		t.Fatalf("saved %q", b)
	}
}

func TestM4ResultSearchMatchCase(t *testing.T) {
	g := &Grid{}
	g.SetData([]gridCol{{Name: "n"}}, [][]any{{"alpha"}, {"ALPHA"}})
	if !g.FindCase("ALPHA", 1, true) || g.row != 1 {
		t.Fatalf("a case-sensitive search must skip row 0, at row %d", g.row)
	}
	g.row = 1
	if g.FindCase("alpha", 1, true) && g.row == 1 {
		t.Fatal("alpha must not match ALPHA when case matters")
	}
	if !g.FindCase("alpha", -1, false) {
		t.Fatal("a case-insensitive search must match")
	}
}

func TestM4SearchPromptTogglesMatchCase(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	queryResult(h, "select * from customers")
	h.press("/")
	h.want("match case off")
	h.press("alt+c")
	h.want("match case on")
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

func TestM4SidebarShowColumnsAndSystemSchemas(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) {
		c.Sidebar.ShowColumns, c.Sidebar.SortColumns = true, true
	})
	h.connect()
	h.press("space")
	h.want("· email")
	scr := h.screen()
	if strings.Index(scr, "· email") > strings.Index(scr, "· id") {
		t.Fatal("sort_columns must list email before id")
	}
	h.press("space")
	h.wantNot("· email")
	s := h.a.ws().sb
	if !s.keepSchema("public") || s.keepSchema("pg_catalog") || s.keepSchema("information_schema") {
		t.Fatal("system schemas are hidden by default")
	}
	h.a.d.Cfg.Sidebar.LoadSystemSchemas = true
	if !s.keepSchema("pg_catalog") {
		t.Fatal("load_system_schemas shows them")
	}
}

func TestM4SortSavedQueries(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.a.d.Queries.Save("", "zeta", nil, "select 1")
	h.a.d.Queries.Save("", "alpha", nil, "select 2")
	s := h.a.ws().sb
	s.switchTab(1)
	s.qsort = "name"
	rows := s.rows()
	if len(rows) != 2 || rows[0].q.Name != "alpha" {
		t.Fatalf("rows %+v", rows)
	}
}

func TestM4PasswordAskPrompts(t *testing.T) {
	h := newHarness(t, nil)
	got := make(chan string, 1)
	go func() {
		p, _ := h.a.askSecret("Password", "shop")
		got <- p
	}()
	h.a.Update(liveMsg{<-h.a.mcpUI})
	h.want("Password")
	h.typeText("pw1")
	h.press("enter")
	if p := <-got; p != "pw1" {
		t.Fatalf("got %q", p)
	}
	go func() {
		_, err := h.a.askSecret("Password", "shop")
		got <- err.Error()
	}()
	h.a.Update(liveMsg{<-h.a.mcpUI})
	h.press("esc")
	if e := <-got; e != "cancelled" {
		t.Fatalf("got %q", e)
	}
}

func TestM4EditorWrapsLongLines(t *testing.T) {
	e := NewEditor("postgres", EditorOpts{Wrap: true})
	e.W, e.H = 20, 6
	text := strings.Repeat("abcdefghij", 5) // 50 runes, 14 per row
	e.SetText(text)
	e.move(0, 50, false)
	var rows []string
	for _, l := range strings.Split(stripANSI(e.View(NewTheme(config.Theme{}), true)), "\n") {
		rows = append(rows, strings.TrimSpace(strings.SplitN(l, "│", 2)[1]))
	}
	if !strings.Contains(strings.Join(rows[:4], ""), text) {
		t.Fatalf("wrapped rows %q", rows)
	}
	if e.curY < 0 {
		t.Fatal("the cursor row must be known")
	}
	e.Opts.Wrap = false
	e.left = 0
	if v := stripANSI(e.View(NewTheme(config.Theme{}), true)); strings.Contains(v, text) {
		t.Fatal("unwrapped, the line is cut at the pane width")
	}
}

func TestM4CleanupVariablesForgetsThem(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.a.ws().vars = map[string]string{"a": "1"}
	h.run("cleanup_variables")
	if h.a.ws().vars != nil {
		t.Fatal("variables must be forgotten")
	}
	h.run("wrap_lines")
	if h.a.ws().tab().(*queryTab).ed.Opts.Wrap {
		t.Fatal("wrap_lines must toggle the default (on) off")
	}
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

func (h *harness) run(action string) { h.t.Helper(); h.pump(h.a.run(action)) }
