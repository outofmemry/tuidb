package mariadb

import (
	"reflect"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func TestRegisteredAsMariaDB(t *testing.T) {
	d, err := db.Lookup(config.MariaDB)
	if err != nil || d.Name() != "mariadb" {
		t.Fatalf("lookup: %v %v", d, err)
	}
	if my, err := db.Lookup(config.MySQL); err != nil || my.Name() != "mysql" { // a distinct driver, not an alias
		t.Fatalf("mysql lookup: %v %v", my, err)
	}
}

func TestCaps(t *testing.T) {
	var c db.Capabilities
	adjust(&c)
	if !c.Sequences {
		t.Error("MariaDB has sequences")
	}
	have := map[string]bool{}
	for _, ty := range c.Types {
		have[ty] = true
	}
	for _, want := range []string{"int", "varchar(255)", "json", "uuid", "inet4", "inet6", "datetime", "longtext", "blob"} {
		if !have[want] {
			t.Errorf("type list lacks %q", want)
		}
	}
}

// MariaDB lexes like MySQL: the editor splits and the safe-mode classifier must agree with the server.
func TestSplitAndClassifyAsMySQL(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{`select 'a\';b'; select 2`, []string{`select 'a\';b'`, "select 2"}},
		{"select `a;b`, \"c;d\"; select 2", []string{"select `a;b`, \"c;d\"", "select 2"}},
		{"select 1 # x;y\n; select 2 -- p;q\n; select 3", []string{"select 1 # x;y", "select 2 -- p;q", "select 3"}},
		{"select 1 /* a; /* b; */ ; select 2", []string{"select 1 /* a; /* b; */", "select 2"}},
		{"select 1--1; drop table t", []string{"select 1--1", "drop table t"}}, // --1 is arithmetic
		{"CREATE SEQUENCE s; SELECT NEXTVAL(s)", []string{"CREATE SEQUENCE s", "SELECT NEXTVAL(s)"}},
		{"DELIMITER $$\nBEGIN NOT ATOMIC SELECT 1; SELECT 2; END$$\nDELIMITER ;\nSELECT 3", []string{"BEGIN NOT ATOMIC SELECT 1; SELECT 2; END", "SELECT 3"}},
	} {
		var got []string
		for _, s := range db.Split("mysql", c.in) {
			got = append(got, s.Text)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Split(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for q, want := range map[string]bool{
		"SELECT * FROM t":                                      true,
		"SHOW CREATE SEQUENCE s":                               true,
		"SHOW CREATE TABLE t":                                  true,
		"EXPLAIN DELETE FROM t":                                true,
		"SELECT 1 FROM t FOR SYSTEM_TIME ALL":                  true,
		"SELECT * FROM t FOR UPDATE":                           true,
		"SELECT NEXTVAL(s)":                                    false,
		"SELECT /* x */ nextval ( s )":                         false,
		"SELECT setval(s, 1)":                                  false,
		"SELECT NEXT\nVALUE  FOR s":                            false,
		"SET STATEMENT max_statement_time=1 FOR DELETE FROM t": false,
		"ANALYZE DELETE FROM t":                                false,
		"INSERT INTO t VALUES (1) RETURNING id":                false,
		"DELETE FROM t RETURNING id":                           false,
		"DELETE HISTORY FROM t":                                false,
		"CREATE OR REPLACE SEQUENCE s":                         false,
		"SELECT 1 INTO OUTFILE '/tmp/x'":                       false,
		"SELECT 1 /*M!100100 INTO @a */":                       false,
		"SELECT 1 /*!100100 INTO @a */":                        false,
		"SELECT 'it\\'s' /* DELETE */ -- DROP":                 true,
	} {
		if got := db.IsReadOnly("mysql", q); got != want {
			t.Errorf("IsReadOnly(%q) = %v, want %v", q, got, want)
		}
	}
}
