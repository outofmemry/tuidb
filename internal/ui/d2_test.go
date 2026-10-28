package ui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/board"
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/importer"
)

func TestWelcomeEscKeepsTheHighlightedConnection(t *testing.T) {
	h := newHarness(t, nil)
	h.a.d.Conns = []config.Connection{
		{Name: "a1", Driver: config.Postgres, Host: "h"}, {Name: "b1", Driver: config.Postgres, Host: "h"},
		{Name: "a2", Driver: config.Postgres, Host: "h"}, {Name: "b2", Driver: config.Postgres, Host: "h"},
	}
	h.press("/")
	h.typeText("b")
	h.press("down", "esc")
	if r, ok := h.a.welcome.current(); !ok || r.conn.Name != "b2" {
		t.Fatalf("after Esc the cursor is on %+v", r.conn.Name)
	}
}

func TestConnectIsGuardedAndCancellable(t *testing.T) {
	h := newHarness(t, nil)
	c := h.a.d.Conns[0]
	h.a.openConnection(c)
	if len(h.a.pending) != 1 {
		t.Fatal("the attempt is not tracked")
	}
	if cmd := h.a.openConnection(c); cmd == nil && len(h.a.pending) != 1 {
		t.Fatal("a second attempt must be ignored")
	}
	if h.a.cancelConnecting() != 1 {
		t.Fatal("cancel did not see the attempt")
	}
}

func TestJobSurvivesAPanic(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	h.pump(w.job("boom", func(context.Context, db.Session, func(func() tea.Cmd)) error { panic("x") }, func() (string, tea.Cmd) { return "no", nil }))
	if w.jobCancel != nil || w.busy() {
		t.Fatal("a panicking job left the workspace busy")
	}
}

func TestImportMappingRejectsTwoColumnsOntoOne(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.a.ws().mapColumns("f.csv", importer.Options{Table: db.ObjRef{Name: "t"}}, []string{"email", "name"}, nil, []string{"name", "email"})
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	if err := f.validate(map[string]string{"0": "name", "1": "name"}); err == nil || !strings.Contains(err.Error(), "email") {
		t.Fatalf("duplicate target accepted: %v", err)
	}
	if err := f.validate(map[string]string{"0": "email", "1": "(skip)"}); err != nil {
		t.Fatal(err)
	}
}

func TestChecklistRemembersTickOrder(t *testing.T) {
	c := &checklistDlg{items: []checkItem{{Label: "a"}, {Label: "b"}, {Label: "c"}}}
	c.sel = 2
	c.Update(keyMsg("space"))
	c.sel = 0
	c.Update(keyMsg("space"))
	if c.items[2].Seq >= c.items[0].Seq || c.items[2].Seq == 0 {
		t.Fatalf("tick order lost: %+v", c.items)
	}
}

func TestSidebarSelectionFollowsTheItem(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	s := h.a.ws().sb
	rows := s.rows()
	last := -1
	for i, r := range rows {
		if r.kind == sbObject {
			last = i
		}
	}
	s.sel = last
	id := s.selectedID()
	s.objs = append([]db.Object{{ObjRef: db.ObjRef{Schema: "public", Name: "aaa", Kind: db.KindTable}}}, s.objs...)
	s.selectID(id)
	if s.selectedID() != id {
		t.Fatal("the cursor moved to another item")
	}
}

func TestSchemaDiffTextCommentsDropsOut(t *testing.T) {
	keep := []db.Statement{{Text: "CREATE TABLE a (id int)"}}
	drops := []db.Statement{{Text: `DROP TABLE "b"`}}
	off := schemaDiffText(keep, drops, false)
	if !strings.Contains(off, "-- DROP TABLE \"b\";") || strings.Contains(off, "\nDROP TABLE") || !strings.Contains(off, "commented out below") {
		t.Fatalf("drops must be commented out:\n%s", off)
	}
	on := schemaDiffText(keep, drops, true)
	if !strings.Contains(on, "\nDROP TABLE \"b\";") {
		t.Fatalf("drops requested:\n%s", on)
	}
}

func TestConfirmBulkAsksForDeletes(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	ran := 0
	apply := func() tea.Cmd { ran++; return nil }
	w.confirmBulk("t", []db.Statement{{Text: "INSERT INTO t VALUES (1)"}}, apply)
	if ran != 1 {
		t.Fatal("a small insert batch must run at once")
	}
	w.confirmBulk("t", []db.Statement{{Text: "DELETE FROM t WHERE id = 1"}}, apply)
	if ran != 1 {
		t.Fatal("a delete batch must wait for the typed confirmation")
	}
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	if f.validate(map[string]string{"confirm": "2"}) == nil || f.validate(map[string]string{"confirm": "1"}) != nil {
		t.Fatal("the count is the confirmation")
	}
}

func TestBoardQueuesARunAndSurvivesNarrowPanes(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	v := &boardView{w: h.a.ws(), b: board.Board{Name: "b"}, running: true}
	if v.run() != nil || v.queued == nil {
		t.Fatal("a run during a run must be queued, not dropped")
	}
	for _, w := range []int{0, 1, 2, 3} {
		tile(h.a.th, &board.PanelResult{Panel: board.Panel{Title: "t", Kind: "table"}}, w, false)
	}
}

func TestPruneHiddenDropsVanishedColumns(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	tt := h.a.ws().tab().(*tableTab)
	tt.hidden = map[string]bool{"gone": true}
	tt.pruneHidden()
	if len(tt.hidden) != 0 {
		t.Fatal("stale hidden column kept")
	}
}

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
