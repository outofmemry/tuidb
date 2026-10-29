package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"webtuidb/internal/db"
	"webtuidb/internal/keymap"
	"webtuidb/internal/menus"
)

// Parity checks for the terminal side (see also internal/web/parity_test.go and internal/menus/parity_test.go).

func (h *harness) wantMenu(name string, list []menus.Item, env menus.Env) {
	h.t.Helper()
	got, want := h.menuLabels(), menus.Labels(list, env)
	if !sameLabels(got, want) {
		h.t.Errorf("menus.%s in %+v: the terminal menu differs from the shared list:\n got %q\nwant %q", name, env, got, want)
	}
	h.press("esc")
}

// TestParityMenusMatchLists (M14 checks 4 and 5): every context menu the terminal builds is menus.Filter of the shared list.
func TestParityMenusMatchLists(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.procs = nil
	h.connect()
	w := h.a.ws()
	row := func(kind db.Kind) sbRow {
		return sbRow{kind: sbObject, obj: db.Object{ObjRef: db.ObjRef{Schema: "public", Name: "x", Kind: kind}}}
	}
	// sidebar object menu: kinds x capabilities
	for _, kind := range []db.Kind{db.KindTable, db.KindView, db.KindMatView, db.KindFunction, db.KindSequence} {
		for _, on := range []bool{true, false} {
			w.caps.Triggers, w.caps.Schemas = on, on
			r := row(kind)
			w.sb.objectMenu(r)
			h.wantMenu("sidebar_object", menus.SidebarObject, w.sb.objectEnv(r))
		}
	}
	// the + menu with each of its capabilities off
	for _, off := range []string{"", "Views", "MatViews", "Functions", "Schemas"} {
		w.caps.Views, w.caps.MatViews, w.caps.Functions, w.caps.Schemas = true, true, true, true
		switch off {
		case "Views":
			w.caps.Views = false
		case "MatViews":
			w.caps.MatViews = false
		case "Functions":
			w.caps.Functions = false
		case "Schemas":
			w.caps.Schemas = false
		}
		w.sb.newMenu()
		h.wantMenu("sidebar_new", menus.SidebarNew, menus.Env{Caps: capsMap(w.caps)})
	}
	w.sb.databaseMenu("shop")
	h.wantMenu("database", menus.Database, menus.Env{Kind: "database"})
	w.consoleMenu()
	h.wantMenu("console", menus.Console, menus.Env{})

	// welcome (check 5)
	h.a.overlays = nil
	h.a.inWS = false
	h.a.welcome.sel = 0
	h.press("m")
	h.wantMenu("welcome", menus.Welcome, menus.Env{})
	h.a.inWS = true

	// data tab: grid, header, filter bar
	h.openCustomers()
	w.tabMenu(0)
	h.wantMenu("tab", menus.Tab, menus.Env{})
	tt := w.tab().(*tableTab)
	tt.gridMenu()
	h.wantMenu("grid", menus.Grid, menus.Env{Editable: tt.grid.Editable})
	tt.columnMenu()
	h.wantMenu("header", menus.Header, menus.Env{})
	tt.filterBarMenu()
	h.wantMenu("filter_bar", menus.FilterBar, menus.Env{})

	// structure editor: Move up / down follow ReorderColumns
	for _, on := range []bool{true, false} {
		w.caps.ReorderColumns = on
		h.run("show_structure")
		g := tt.editGrid()
		tt.structMenu()
		env := menus.Env{Caps: capsMap(w.caps)}
		if g != nil {
			env.Editable = g.Editable
		}
		h.wantMenu("structure", menus.Structure, env)
		h.run("show_data")
	}

	// query result
	h.run("new_query_tab")
	q := w.tab().(*queryTab)
	h.typeText("select 1")
	h.run("run_current")
	if q.curResult() == nil {
		t.Fatalf("no result:\n%s", h.screen())
	}
	q.resultMenu()
	h.wantMenu("result", menus.Result, menus.Env{Editable: q.editableResult() != nil})

	// process list row menu
	h.fake.procs = []db.ProcessInfo{{ID: "7", User: "ann", DB: "shop", Query: "select 1"}}
	w.caps.ProcessList = true
	h.run("process_list")
	h.press("m")
	h.wantMenu("process_list", menus.ProcessList, menus.Env{})
}

// dispatchedIn reports whether src dispatches on action: a `case ... "action"` clause or a comparison with it,
// not just any string literal that happens to spell the same word.
func dispatchedIn(src, action string) bool {
	re := regexp.MustCompile(`(case\s[^\n:]*|[=!]=\s*)"` + regexp.QuoteMeta(action) + `"`)
	return re.MatchString(src)
}

func uiSource(t *testing.T) string {
	t.Helper()
	var src strings.Builder
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			src.Write(b)
		}
	}
	return src.String()
}

// TestParityMenuActionsDispatched: every menu entry the terminal can do names an action the terminal code dispatches on.
func TestParityMenuActionsDispatched(t *testing.T) {
	src := uiSource(t)
	for name, list := range menus.All() {
		for _, it := range list {
			if it.UI == "web" {
				continue
			}
			if !dispatchedIn(src, it.Action) {
				t.Errorf("menus.%s %q: internal/ui never dispatches on %q", name, it.Label, it.Action)
			}
		}
	}
}

// TestParityMatcherFailsWhenRemoved (R-017): the dispatch matcher is not satisfied by a bare string literal, and a
// removed action fails it.
func TestParityMatcherFailsWhenRemoved(t *testing.T) {
	src := uiSource(t)
	if !dispatchedIn(src, "show_structure") {
		t.Fatal("show_structure is dispatched and must match")
	}
	if dispatchedIn(src, "no_such_menu_action_zz") {
		t.Fatal("an action nothing dispatches on must fail")
	}
	removed := strings.ReplaceAll(src, `case "show_structure"`, `case "gone"`)
	removed = strings.ReplaceAll(removed, `== "show_structure"`, `== "gone"`)
	if dispatchedIn(removed, "show_structure") {
		t.Fatal("removing the dispatch of show_structure must fail the matcher")
	}
	if dispatchedIn(`x := "show_structure"`, "show_structure") {
		t.Fatal("a bare string literal is not a dispatch")
	}
}

// capCase is one capability and what the terminal does without it.
type capCase struct {
	cap  string
	gate func(h *harness, w *Workspace) string // returns a failure message, "" when the feature is hidden or refused
}

func hasLabel(labels []string, l string) bool {
	for _, x := range labels {
		if x == l {
			return true
		}
	}
	return false
}

// TestParityCapabilityGating (M14 check 7): with each capability off the terminal hides or refuses the feature.
func TestParityCapabilityGating(t *testing.T) {
	cases := []capCase{
		{"Indexes", func(h *harness, w *Workspace) string {
			h.openCustomers()
			h.run("show_index")
			if !strings.Contains(h.screen(), "has no Index tab") {
				return "the Index view still shows an index grid:\n" + h.screen()
			}
			return ""
		}},
		{"Triggers", func(h *harness, w *Workspace) string {
			w.sb.objectMenu(sbRow{kind: sbObject, obj: db.Object{ObjRef: db.ObjRef{Name: "customers", Kind: db.KindTable}}})
			l := h.menuLabels()
			h.press("esc")
			if hasLabel(l, "Triggers…") {
				return "sidebar menu still lists Triggers…"
			}
			h.a.toast = ""
			h.pump(w.triggerMenu(db.ObjRef{Name: "customers", Kind: db.KindTable}))
			if !strings.Contains(h.a.toast, "no triggers") {
				return "triggers action did not refuse: " + h.a.toast
			}
			return ""
		}},
		{"Functions", func(h *harness, w *Workspace) string { return newMenuLacks(h, w, "New function / procedure…") }},
		{"Views", func(h *harness, w *Workspace) string { return newMenuLacks(h, w, "New view…") }},
		{"MatViews", func(h *harness, w *Workspace) string { return newMenuLacks(h, w, "New materialized view…") }},
		{"Schemas", func(h *harness, w *Workspace) string { return newMenuLacks(h, w, "New schema…") }},
		{"Sequences", func(h *harness, w *Workspace) string {
			if _, _, seq, _, _ := w.sb.cfg(); seq {
				return "sidebar still shows the Sequences section"
			}
			return ""
		}},
		{"ProcessList", func(h *harness, w *Workspace) string {
			h.a.toast = ""
			h.run("process_list")
			if !strings.Contains(h.a.toast, "not supported") {
				return "process_list did not refuse: " + h.a.toast
			}
			return ""
		}},
		{"Users", func(h *harness, w *Workspace) string {
			h.a.toast = ""
			h.run("users")
			if !strings.Contains(h.a.toast, "not supported") {
				return "users did not refuse: " + h.a.toast
			}
			return ""
		}},
		{"Document", func(h *harness, w *Workspace) string {
			h.openCustomers()
			h.a.toast = ""
			h.run("show_json")
			if !strings.Contains(h.a.toast, "document engines") {
				return "show_json did not refuse: " + h.a.toast
			}
			return ""
		}},
		{"ReorderColumns", func(h *harness, w *Workspace) string {
			h.openCustomers()
			tt := w.tab().(*tableTab)
			h.run("show_structure")
			tt.structMenu()
			l := h.menuLabels()
			h.press("esc")
			if hasLabel(l, "Move up") || hasLabel(l, "Move down") {
				return "structure menu still lists Move up / Move down"
			}
			return ""
		}},
	}
	for _, c := range cases {
		t.Run(c.cap, func(t *testing.T) {
			h := newHarness(t, nil)
			h.connect()
			w := h.a.ws()
			setCap(&w.caps, c.cap, false)
			if msg := c.gate(h, w); msg != "" {
				t.Errorf("with %s off: %s", c.cap, msg)
			}
		})
	}
}

func newMenuLacks(h *harness, w *Workspace, label string) string {
	w.sb.newMenu()
	l := h.menuLabels()
	h.press("esc")
	if hasLabel(l, label) {
		return "the + menu still lists " + label
	}
	return ""
}

func setCap(c *db.Capabilities, name string, v bool) {
	switch name {
	case "Indexes":
		c.Indexes = v
	case "Triggers":
		c.Triggers = v
	case "Functions":
		c.Functions = v
	case "Views":
		c.Views = v
	case "MatViews":
		c.MatViews = v
	case "Schemas":
		c.Schemas = v
	case "Sequences":
		c.Sequences = v
	case "ProcessList":
		c.ProcessList = v
	case "Users":
		c.Users = v
	case "Document":
		c.Document = v
	case "ReorderColumns":
		c.ReorderColumns = v
	}
}

// TestParityHelpListing (M14 check 2): the terminal help lists, per context, exactly the rows `webtuidb -keys` prints
// (action, key, description, in that order), and shown() does not re-sort when no search is active.
func TestParityHelpListing(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.a.showHelp()
	hd := h.a.overlays[len(h.a.overlays)-1].(*helpDlg)
	text := keymap.Listing(keymap.Display(h.a.d.Keys.Prefix), h.a.d.Keys)
	if got := hd.shown(); len(got) != len(hd.rows) {
		t.Fatalf("shown() changed the row count without a query: %d vs %d", len(got), len(hd.rows))
	}
	ctx := ""
	pos := map[string]int{}
	for _, r := range hd.shown() {
		if r.header != "" {
			ctx = strings.ToLower(r.header)
			i := strings.Index(text, "["+ctx+"]\n")
			if i < 0 {
				t.Fatalf("webtuidb -keys has no [%s] section", ctx)
			}
			pos[ctx] = i
			continue
		}
		k := r.key
		if r.noPref {
			k = "-n " + k
		}
		line := "  " + padRight(r.action, 26) + " " + padRight(k, 10) + " " + r.desc + "\n"
		i := strings.Index(text[pos[ctx]:], line)
		if i < 0 {
			t.Errorf("help row %q (%s) is not in `webtuidb -keys` [%s] after the previous row: want line %q", r.action, r.key, ctx, strings.TrimSpace(line))
			continue
		}
		pos[ctx] += i + len(line)
	}
}

func padRight(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}
