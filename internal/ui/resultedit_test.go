package ui

import "testing"

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
