package sqlfmt

import (
	"strings"
	"testing"
)

var o4 = Options{Indent: "spaces", Width: 2, Uppercase: true}

func TestBeautify(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"select a,b from t where x=1 and y='a;b' or z in (select id from u) order by a desc limit 5",
			"SELECT\n  a,\n  b\nFROM t\nWHERE x = 1\n  AND y = 'a;b'\n  OR z IN (\n  SELECT\n    id\n  FROM u\n)\nORDER BY a DESC\nLIMIT 5"},
		{"select t.*, count(*) from t left outer join u on t.id=u.id and u.x between 1 and 2",
			"SELECT\n  t.*,\n  count(*)\nFROM t\nLEFT OUTER JOIN u\n  ON t.id = u.id\n  AND u.x BETWEEN 1 AND 2"},
		{"insert into t (a,b) values (1,'select from where'); update t set a=1, b=\"from\" where id=:id",
			"INSERT INTO t (a, b)\nVALUES (1, 'select from where');\n\nUPDATE t\nSET\n  a = 1,\n  b = \"from\"\nWHERE id = :id"},
	} {
		if got := Beautify(c.in, o4); got != c.want {
			t.Errorf("%s\n got:\n%s\nwant:\n%s", c.in, got, c.want)
		}
	}
}

func TestUglify(t *testing.T) {
	in := "select  a ,b\n from t -- hi\n where s = 'x  ;  y';\nselect 1"
	if got, want := Uglify(in, ""), "select a, b from t where s = 'x  ;  y';\nselect 1"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	for _, s := range []string{in, "select a from t join u on t.x=u.x where (a=1 or b=2) and c like '%;%' -- c\n;", "select \"from\" , `where`, [and] from t"} {
		if a, b := Uglify(Beautify(s, Options{Indent: "tabs", Width: 4}), ""), Uglify(s, ""); a != b {
			t.Errorf("not idempotent:\n%q\n%q", a, b)
		}
	}
}

func TestLexKeepsTokensWhole(t *testing.T) {
	for _, c := range []struct{ in, dialect, want string }{
		{"select a @> b, j #>> '{x}', @@version, @a := 1", "", "select a @> b, j #>> '{x}', @@version, @a := 1"},
		{"delete from t # old\nwhere id<5", "mysql", "delete from t where id < 5"}, // comment dropped, WHERE survives
		{"select x'ff', e'a\\'b', N'z', U&'d', b'1'", "", "select x'ff', e'a\\'b', N'z', U&'d', b'1'"},
		{"select 'it\\'s', 2", "mysql", "select 'it\\'s', 2"},
		{"select $t$a;  b$t$, 1", "", "select $t$a;  b$t$, 1"},
	} {
		if got := Uglify(c.in, c.dialect); got != c.want {
			t.Errorf("%q: got %q want %q", c.in, got, c.want)
		}
	}
}

func TestBlocks(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"select case when a=1 then 'x' when a=2 then 'y' else 'z' end as c, b from t",
			"SELECT\n  CASE\n    WHEN a = 1 THEN 'x'\n    WHEN a = 2 THEN 'y'\n    ELSE 'z'\n  END AS c,\n  b\nFROM t"},
		{"select sum(case when a then 1 else 0 end) over (partition by x order by y) from t",
			"SELECT\n  sum(CASE WHEN a THEN 1 ELSE 0 END) OVER (PARTITION BY x ORDER BY y)\nFROM t"},
		{"insert into t (a) values (1) on conflict (a) do update set b = excluded.b, c = 2 where t.x > 1",
			"INSERT INTO t (a)\nVALUES (1)\nON CONFLICT (a)\nDO UPDATE SET\n  b = excluded.b,\n  c = 2\nWHERE t.x > 1"},
		{"select * from t where id = 1 for update", "SELECT\n  *\nFROM t\nWHERE id = 1\nFOR UPDATE"},
	} {
		if got := Beautify(c.in, o4); got != c.want {
			t.Errorf("%s\n got:\n%s\nwant:\n%s", c.in, got, c.want)
		}
	}
}

func TestTabular(t *testing.T) {
	in := "select a,b from t left join u on t.id=u.id where x=1 and y=2 order by a"
	for _, c := range []struct{ typ, want string }{
		{"tabular_right", "   SELECT a,\n          b\n     FROM t\nLEFT JOIN u\n       ON t.id = u.id\n    WHERE x = 1\n      AND y = 2\n ORDER BY a"},
		{"tabular_left", "SELECT    a,\n          b\nFROM      t\nLEFT JOIN u\nON        t.id = u.id\nWHERE     x = 1\nAND       y = 2\nORDER BY  a"},
	} {
		if got := Beautify(in, Options{Indent: "spaces", Width: 2, Uppercase: true, IndentType: c.typ}); got != c.want {
			t.Errorf("%s\n got:\n%s\nwant:\n%s", c.typ, got, c.want)
		}
	}
}

func TestEmptyStatementTabularDoesNotPanic(t *testing.T) {
	for _, in := range []string{";", ";;", "; select 1;;"} {
		Beautify(in, Options{Indent: "spaces", Width: 2, IndentType: "tabular_left"})
	}
}

func TestTrailingBackslashDoesNotPanic(t *testing.T) {
	for _, d := range []string{"mysql", "mariadb", "clickhouse"} {
		Beautify("SELECT 'abc\\", Options{Dialect: d})
		Uglify("SELECT 'abc\\", d)
	}
}

func TestTabularKeepsMultilineStrings(t *testing.T) {
	in := "select 'a\n  b' from t where x=1"
	got := Beautify(in, Options{Indent: "spaces", Width: 2, IndentType: "tabular_left"})
	if !strings.Contains(got, "'a\n  b'") {
		t.Errorf("string rewritten: %q", got)
	}
}
