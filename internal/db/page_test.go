package db

import "testing"

func TestPageStatement(t *testing.T) {
	for _, c := range []struct {
		dialect, in, want string
	}{
		{"postgres", "select * from t where a > 1;", "SELECT * FROM (select * from t where a > 1\n) AS \"webtuidb_page\" LIMIT 10 OFFSET 20"},
		{"postgres", "select a, row_number() over (order by a) from t", "SELECT * FROM (select a, row_number() over (order by a) from t\n) AS \"webtuidb_page\" LIMIT 10 OFFSET 20"},
		{"postgres", "select * from t order by a;", ""},
		{"mysql", "select a from x union select a from y order by 1", ""},
		{"mysql", "with x as (select 1) select * from x -- c", "SELECT * FROM (with x as (select 1) select * from x -- c\n) AS `webtuidb_page` LIMIT 10 OFFSET 20"},
		{"postgres", "select * from t limit 5", ""},
		{"postgres", "select * from t for update", ""},
		{"postgres", "select 1; select 2", ""},
		{"postgres", "delete from t", ""},
		{"postgres", "select * into x from t", ""},
		{"redis", "GET a", ""},
	} {
		got, ok := PageStatement(c.dialect, c.in, 10, 20)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%s %q: %q %v", c.dialect, c.in, got, ok)
		}
	}
}
