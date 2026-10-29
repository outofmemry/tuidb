package ui

import (
	"strings"
	"testing"
	"time"

	"webtuidb/internal/menus"
	"webtuidb/internal/store"
)

// M14.4: the sidebar object menu is menus.SidebarObject filtered by kind and capabilities.
func TestM15SidebarMenuFromMenus(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	for _, triggers := range []bool{true, false} {
		w.caps.Triggers = triggers
		h.press("m")
		got := h.menuLabels()
		want := menus.Labels(menus.SidebarObject, menus.Env{Kind: "table", Caps: capsMap(w.caps)})
		if !sameLabels(got, want) {
			t.Errorf("triggers=%v:\n got %v\nwant %v", triggers, got, want)
		}
		has := strings.Contains(strings.Join(got, "|"), "Triggers…")
		if has != triggers {
			t.Errorf("triggers=%v but the menu has Triggers…: %v", triggers, has)
		}
		for _, l := range []string{"Export…", "Import into this table…", "Mark / unmark", "Set colour…"} {
			if !strings.Contains(strings.Join(got, "|"), l) {
				t.Errorf("menu lacks %q", l)
			}
		}
		h.press("esc")
	}
}

func TestM15QueryHistoryNewDatabaseTabMenus(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	q, err := h.a.d.Queries.Save("", "top", nil, "select 1")
	if err != nil {
		t.Fatal(err)
	}
	w.sb.queryMenu(q)
	if got, want := h.menuLabels(), menus.Labels(menus.SidebarQuery, menus.Env{Kind: "query"}); !sameLabels(got, want) {
		t.Errorf("query menu %v want %v", got, want)
	}
	h.press("esc")
	w.sb.historyMenu(store.HistoryEntry{SQL: "select 2"})
	if got, want := h.menuLabels(), menus.Labels(menus.SidebarHistory, menus.Env{Kind: "history"}); !sameLabels(got, want) {
		t.Errorf("history menu %v want %v", got, want)
	}
	h.press("esc")
	w.sb.newMenu()
	if got, want := h.menuLabels(), menus.Labels(menus.SidebarNew, menus.Env{Caps: capsMap(w.caps)}); !sameLabels(got, want) {
		t.Errorf("+ menu %v want %v", got, want)
	}
	h.press("esc")
	w.sb.databaseMenu("shop")
	if got, want := h.menuLabels(), menus.Labels(menus.Database, menus.Env{Kind: "database"}); !sameLabels(got, want) {
		t.Errorf("database menu %v want %v", got, want)
	}
	h.press("esc")
	h.openCustomers()
	w.tabMenu(0)
	got := h.menuLabels()
	if want := menus.Labels(menus.Tab, menus.Env{}); !sameLabels(got, want) {
		t.Errorf("tab menu %v want %v", got, want)
	}
	// the web-only entry is listed, dimmed, with the reason; choosing it only explains
	p := h.a.overlays[len(h.a.overlays)-1].(*pickerDlg)
	last := p.items[len(p.items)-1]
	if !last.Dim || !strings.Contains(last.Detail, "web only") {
		t.Errorf("web-only entry: %+v", last)
	}
	h.press("esc")
	h.a.overlays = nil
	h.a.ws().focusSB = true
	h.a.welcome.sel = 0
	h.a.inWS = false
	h.press("m")
	if got, want := h.menuLabels(), menus.Labels(menus.Welcome, menus.Env{}); !sameLabels(got, want) {
		t.Errorf("welcome menu %v want %v", got, want)
	}
}

func TestM15CreateTemplates(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	s := h.a.ws().sb
	for _, kind := range []string{"view", "matview", "function"} {
		if txt := s.createTemplate(kind); !strings.Contains(strings.ToUpper(txt), "CREATE") || !strings.Contains(txt, "new_") {
			t.Errorf("%s template: %q", kind, txt)
		}
	}
	s.w.caps.Dialect = "postgres"
	if txt := s.createTemplate("matview"); !strings.Contains(txt, "MATERIALIZED VIEW public.new_matview") {
		t.Errorf("postgres matview: %q", txt)
	}
	s.w.caps.Dialect = "mysql"
	if txt := s.createTemplate("function"); !strings.Contains(txt, "CREATE FUNCTION") {
		t.Errorf("mysql function: %q", txt)
	}
}

func TestM9HistoryMenuClear(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	w.a.d.Hist.Append(w.conn.Name, store.HistoryEntry{SQL: "select 5"})
	e := store.HistoryEntry{SQL: "select 5"}
	h.pump(w.sb.historyMenu(e))
	h.pickLabel("Clear all…")
	h.a.overlays[len(h.a.overlays)-1].(*confirmDlg).armed = time.Time{}
	h.press("y")
	if all, _ := w.a.d.Hist.List(w.conn.Name, "", 0); len(all) != 0 {
		t.Fatalf("history left: %+v", all)
	}
}
