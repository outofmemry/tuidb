package db

import (
	"reflect"
	"strings"
	"testing"
)

func splitTexts(dialect, in string) []string {
	var got []string
	for _, s := range Split(dialect, in) {
		got = append(got, s.Text)
	}
	return got
}

func TestSplitCQL(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"select 1; select 2;", []string{"select 1", "select 2"}},
		{"select 'it''s;ok'; select \"a;b\"", []string{"select 'it''s;ok'", `select "a;b"`}},
		{"select $$a;'b$$; select 2", []string{"select $$a;'b$$", "select 2"}},
		// no backslash escapes: the string ends at the second quote, so what follows is code
		{`select '\'; drop table x; --'`, []string{`select '\'`, "drop table x"}},
		{"select 1 -- a;b\n; select 2", []string{"select 1 -- a;b", "select 2"}},
		{"select 1 // a;b\n; select 2", []string{"select 1 // a;b", "select 2"}},
		{"select /* ; */ 1; select 2", []string{"select /* ; */ 1", "select 2"}},
		// block comments do not nest: the first */ closes it
		{"select /* a /* b */ 1; select 2 */", []string{"select /* a /* b */ 1", "select 2 */"}},
		// # is not a comment
		{"select 1 # x; drop table y", []string{"select 1 # x", "drop table y"}},
		{"-- only a comment\n; /* x */", nil},
		// a batch is one statement
		{"BEGIN BATCH INSERT INTO t (a) VALUES (1); INSERT INTO t (a) VALUES (2); APPLY BATCH; select 3",
			[]string{"BEGIN BATCH INSERT INTO t (a) VALUES (1); INSERT INTO t (a) VALUES (2); APPLY BATCH", "select 3"}},
		{"begin unlogged batch insert into t (a) values ('apply batch;'); apply batch;", []string{"begin unlogged batch insert into t (a) values ('apply batch;'); apply batch"}},
		{"BEGIN COUNTER BATCH UPDATE t SET c = c + 1 WHERE a = 1; APPLY /* x */ BATCH ; select 1", []string{"BEGIN COUNTER BATCH UPDATE t SET c = c + 1 WHERE a = 1; APPLY /* x */ BATCH", "select 1"}},
		{"BEGIN BATCH UPDATE t SET a = 1 WHERE b = 'x' -- APPLY BATCH;\n; APPLY BATCH; BEGIN BATCH DELETE FROM t WHERE a = 1; APPLY BATCH",
			[]string{"BEGIN BATCH UPDATE t SET a = 1 WHERE b = 'x' -- APPLY BATCH;\n; APPLY BATCH", "BEGIN BATCH DELETE FROM t WHERE a = 1; APPLY BATCH"}},
		{"BEGIN BATCH INSERT INTO t (a) VALUES (1); INSERT INTO t (a) VALUES (2);", []string{"BEGIN BATCH INSERT INTO t (a) VALUES (1); INSERT INTO t (a) VALUES (2);"}}, // unterminated: one statement to the end
	} {
		if got := splitTexts("cql", c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q:\n got %q\nwant %q", c.in, got, c.want)
		}
	}
	// the same input for another dialect must not be touched by the batch rule
	if got := splitTexts("postgres", "BEGIN; select 1; COMMIT"); len(got) != 3 {
		t.Errorf("postgres BEGIN: %q", got)
	}
}

func TestIsReadOnlyCQL(t *testing.T) {
	for stmt, want := range map[string]bool{
		"SELECT * FROM t":                    true,
		"select JSON * from t where a = 1":   true,
		"/* c */ -- d\nSELECT 1":             true,
		"DESCRIBE KEYSPACES":                 true,
		"desc table t":                       true,
		"LIST ROLES":                         true,
		"":                                   true,
		"-- nothing":                         true,
		"INSERT INTO t (a) VALUES (1)":       false,
		"UPDATE t SET a = 1 WHERE b = 2":     false,
		"DELETE FROM t WHERE a = 1":          false,
		"TRUNCATE t":                         false,
		"DROP TABLE t":                       false,
		"CREATE TABLE t (a int PRIMARY KEY)": false,
		"ALTER TABLE t ADD b int":            false,
		"GRANT SELECT ON t TO bob":           false,
		"USE ks":                             false,
		"BEGIN BATCH INSERT INTO t (a) VALUES (1); APPLY BATCH": false,
		"/* SELECT */ DELETE FROM t":                            false,
		"-- SELECT\nDELETE FROM t":                              false,
		"// SELECT\nDELETE FROM t":                              false,
		"$$SELECT$$":                                            false,
		"'SELECT' || x":                                         false,
		"EXPLAIN SELECT 1":                                      false,
		"CONSISTENCY ALL":                                       false,
		"APPLY BATCH":                                           false,
		"(SELECT 1)":                                            false,
	} {
		if got := IsReadOnly("cql", stmt); got != want {
			t.Errorf("IsReadOnly(%q) = %v, want %v", stmt, got, want)
		}
	}
	// the generic classifier would let EXPLAIN ANALYZE and WITH through; CQL's own does not, and the other dialects are unchanged
	if !IsReadOnly("postgres", "EXPLAIN SELECT 1") || IsReadOnly("cql", "EXPLAIN SELECT 1") {
		t.Error("classifier registration is per dialect")
	}
}

// A hidden write is the risk: statements cut with Split must classify the way the server will read them.
func TestCQLSplitThenClassify(t *testing.T) {
	text := `SELECT '\'; DELETE FROM t WHERE a = 1; --'`
	var ro []bool
	for _, s := range Split("cql", text) {
		ro = append(ro, IsReadOnly("cql", s.Text))
	}
	if !reflect.DeepEqual(ro, []bool{true, false}) { // "SELECT '\'" and the DELETE; the comment tail is dropped
		t.Errorf("classification %v of %q", ro, splitTexts("cql", text))
	}
	if st := Split("cql", "BEGIN BATCH DELETE FROM t WHERE a = 1; APPLY BATCH"); len(st) != 1 || IsReadOnly("cql", st[0].Text) {
		t.Error("a batch is one writing statement")
	}
}

func TestCQLDialect(t *testing.T) {
	d, ok := DialectFor("cql")
	if !ok || d.Quote(`a"b`) != `"a""b"` || d.Param(3) != "?" || d.ILike || d.BackslashEscapes || d.LikeCast {
		t.Fatalf("%+v", d)
	}
	if d.QualifiedName(ObjRef{Schema: "ks", Name: "t"}) != `"ks"."t"` {
		t.Error("qualified name")
	}
	if got := d.Literal("it's \\ x"); got != `'it''s \ x'` {
		t.Errorf("literal %s", got)
	}
	ops := Operators("cql")
	if strings.Contains(strings.Join(ops, ","), "ILIKE") || len(ops) != 7 || ops[0] != "=" {
		t.Errorf("operators %v", ops)
	}
	if len(Operators("mysql")) < 12 {
		t.Error("other dialects keep the full operator list")
	}
	// raw filters are not parenthesized in CQL, which has no parenthesized relations
	w, _, err := FilterSet{Filters: []Filter{{Column: RawColumn, Value: "a = 1 AND b = 2", Enabled: true}}}.Where(d, nil, 1)
	if err != nil || w != "a = 1 AND b = 2" {
		t.Errorf("raw where %q %v", w, err)
	}
	if w, _, _ := (FilterSet{Filters: []Filter{{Column: RawColumn, Value: "a = 1", Enabled: true}}}).Where(MySQL, nil, 1); w != "(a = 1)" {
		t.Errorf("mysql raw where %q", w)
	}
	rr := rulesFor("cql")
	if rr.backslash || rr.hash || rr.pg || !rr.dollar || !rr.slashes {
		t.Errorf("rules %+v", rr)
	}
}
