package ui

import (
	"strings"
	"testing"
	"webtuidb/internal/config"
)

func TestSingleTable(t *testing.T) {
	for stmt, want := range map[string]string{
		"select * from customers":                                      "customers",
		"SELECT id, name FROM public.customers WHERE id > 3 LIMIT 10;": "public.customers",
		`select "id", "name" from "my table" order by id`:              "my table",
		"select * from `t` limit 5 offset 2":                           "t",
		"select a.id from customers a":                                 "",
		"select * from a join b on a.id = b.id":                        "",
		"select id as x from customers":                                "",
		"select id + 1 from customers":                                 "",
		"select count(*) from customers":                               "",
		"select distinct name from customers":                          "",
		"select * from customers group by name":                        "",
		"select * from customers where id in (select 1)":               "",
		"with x as (select 1) select * from x":                         "",
		"select * into copy from customers":                            "",
		"select * from a union select * from b":                        "",
		"update customers set name = 'x'":                              "",
		"select * from customers; delete from customers":               "",
	} {
		sch, tbl, ok := singleTable(stmt)
		got := tbl
		if sch != "" {
			got = sch + "." + tbl
		}
		if !ok {
			got = ""
		}
		if got != want {
			t.Errorf("singleTable(%q) = %q, want %q", stmt, got, want)
		}
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

func TestM9EditableTableUsesEngineCheck(t *testing.T) {
	if _, tbl, ok := editableTable("select * from customers c where c.id in (select 1) order by 1", "postgres"); !ok || tbl != "customers" {
		t.Errorf("alias and subquery in WHERE are fine: %v %q", ok, tbl)
	}
	if _, _, ok := editableTable("select * from a join b on a.id = b.id", "postgres"); ok {
		t.Error("a join is not editable")
	}
}
