package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

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

func TestM6NewDatabaseSendsEncodingAndCollation(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.run("open_database")
	h.pickLabel("New database")
	setField(t, h, "name", "fresh")
	setField(t, h, "encoding", "UTF8")
	setField(t, h, "collation", "C")
	h.press("enter")
	last := h.fake.adminOpts[len(h.fake.adminOpts)-1]
	if last["encoding"] != "UTF8" || last["collation"] != "C" {
		t.Fatalf("opts %v", last)
	}
}

func TestM9QueryRenameAndInsert(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	h.run("new_query_tab")
	q, err := h.a.d.Queries.Save("", "old", nil, "select 9")
	if err != nil {
		t.Fatal(err)
	}
	h.pump(w.sb.renameQuery(q))
	h.typeText("2")
	h.press("enter")
	list, _ := h.a.d.Queries.List()
	if len(list) != 1 || list[0].Name != "old2" || list[0].SQL != "select 9" {
		t.Fatalf("after rename: %+v", list)
	}
	h.pump(w.insertIntoEditor("select 7"))
	if !strings.Contains(h.queryTab().ed.Text(), "select 7") {
		t.Fatal("not inserted")
	}
}
