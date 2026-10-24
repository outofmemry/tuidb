package mysql

import (
	"context"
	"crypto/tls"
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"tuidb/internal/config"
	"tuidb/internal/db"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		in   any
		typ  string
		want any
	}{
		{nil, "INT", nil},
		{[]byte("42"), "INT", int64(42)},
		{[]byte("-7"), "TINYINT", int64(-7)},
		{[]byte("18446744073709551615"), "UNSIGNED BIGINT", uint64(18446744073709551615)},
		{[]byte("1.50"), "DECIMAL", "1.50"},
		{[]byte("2.5"), "DOUBLE", 2.5},
		{[]byte{0, 0, 0, 5}, "BIT", uint64(5)},
		{[]byte{1, 2}, "BLOB", []byte{1, 2}},
		{[]byte{1, 2}, "VARBINARY", []byte{1, 2}},
		{[]byte(`{"a":1}`), "JSON", `{"a":1}`},
		{[]byte("hi"), "TEXT", "hi"},
		{[]byte("a,b"), "SET", "a,b"},
		{[]byte("12:30:00"), "TIME", "12:30:00"},
		{int32(3), "INT", int64(3)},
		{uint16(3), "UNSIGNED SMALLINT", uint64(3)},
		{float32(1.5), "FLOAT", 1.5},
		{time.Time{}, "DATE", "0000-00-00"},
		{time.Time{}, "DATETIME", "0000-00-00 00:00:00"},
		{time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), "DATETIME", time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)},
		{[]byte("junk"), "INT", "junk"},
	}
	for _, c := range cases {
		if got := normalize(c.in, c.typ); !reflect.DeepEqual(got, c.want) {
			t.Errorf("normalize(%#v, %s) = %#v, want %#v", c.in, c.typ, got, c.want)
		}
	}
}

func TestServerInfo(t *testing.T) {
	if s, m := serverInfo("8.4.3"); s != "MySQL 8.4.3" || m {
		t.Errorf("got %q %v", s, m)
	}
	if s, m := serverInfo("10.11.6-MariaDB-1:10.11.6+maria~ubu2204"); s != "MariaDB 10.11.6" || !m {
		t.Errorf("got %q %v", s, m)
	}
}

func TestColumnDefault(t *testing.T) {
	ns := func(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
	cases := []struct {
		dt    string
		def   sql.NullString
		extra string
		maria bool
		want  string
		has   bool
	}{
		{"varchar", ns("new"), "", false, "'new'", true},
		{"varchar", ns("it's"), "", false, "'it''s'", true},
		{"decimal", ns("0.00"), "", false, "0.00", true},
		{"timestamp", ns("CURRENT_TIMESTAMP"), "DEFAULT_GENERATED", false, "CURRENT_TIMESTAMP", true},
		{"varchar", ns("uuid()"), "DEFAULT_GENERATED", false, "uuid()", true},
		{"int", sql.NullString{}, "", false, "", false},
		{"varchar", ns("'x'"), "", true, "'x'", true},
		{"int", ns("NULL"), "", true, "", false},
	}
	for _, c := range cases {
		got, has := columnDefault(c.dt, c.def, c.extra, c.maria)
		if got != c.want || has != c.has {
			t.Errorf("columnDefault(%s, %v) = %q,%v want %q,%v", c.dt, c.def, got, has, c.want, c.has)
		}
	}
}

func TestDefaultSQL(t *testing.T) {
	for in, want := range map[string]string{
		"'new'": "'new'", "0": "0", "-1.5": "-1.5", "NULL": "NULL", "CURRENT_TIMESTAMP": "CURRENT_TIMESTAMP",
		"CURRENT_TIMESTAMP(3)": "CURRENT_TIMESTAMP(3)", "uuid()": "(uuid())", "'a' || 'b'": "('a' || 'b')",
	} {
		if got := defaultSQL(in); got != want {
			t.Errorf("defaultSQL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTLSConfig(t *testing.T) {
	c := config.Connection{Host: "db.example.com"}
	for _, mode := range []string{"", "disable"} {
		c.SSLMode = mode
		if cfg, _, err := tlsConfig(c); cfg != nil || err != nil {
			t.Errorf("mode %q: %v %v", mode, cfg, err)
		}
	}
	c.SSLMode = "prefer"
	if cfg, fb, err := tlsConfig(c); err != nil || !fb || !cfg.InsecureSkipVerify {
		t.Errorf("prefer: %v %v %v", cfg, fb, err)
	}
	c.SSLMode = "require"
	if cfg, fb, err := tlsConfig(c); err != nil || fb || !cfg.InsecureSkipVerify {
		t.Errorf("require: %v %v %v", cfg, fb, err)
	}
	c.SSLMode = "verify-ca"
	if cfg, _, err := tlsConfig(c); err != nil || !cfg.InsecureSkipVerify || cfg.VerifyConnection == nil {
		t.Errorf("verify-ca: %v %v", cfg, err)
	} else if err := cfg.VerifyConnection(tls.ConnectionState{}); err == nil {
		t.Error("verify-ca accepted a connection without certificates")
	}
	for _, mode := range []string{"verify-full", "verify-identity"} {
		c.SSLMode = mode
		if cfg, _, err := tlsConfig(c); err != nil || cfg.InsecureSkipVerify || cfg.ServerName != "db.example.com" {
			t.Errorf("%s: %v %v", mode, cfg, err)
		}
	}
	c.SSLMode = "bogus"
	if _, _, err := tlsConfig(c); err == nil {
		t.Error("bogus mode accepted")
	}
	c.SSLMode, c.SSLCA = "verify-full", "/nonexistent/ca.pem"
	if _, _, err := tlsConfig(c); err == nil {
		t.Error("missing CA file accepted")
	}
}

func TestUseRe(t *testing.T) {
	for in, want := range map[string]string{"USE foo": "foo", "use `my db`;": "my db", "  Use\nbar ;": "bar"} {
		m := useRe.FindStringSubmatch(in)
		if m == nil || m[1]+m[2] != want {
			t.Errorf("useRe(%q) = %v", in, m)
		}
	}
	if useRe.MatchString("USER()") || useRe.MatchString("SELECT 1") {
		t.Error("useRe too greedy")
	}
}

func TestConvertArg(t *testing.T) {
	if convertArg(db.Null) != nil || convertArg(db.Empty) != "" || convertArg(db.Expr("NOW()")) != "NOW()" || convertArg("x") != "x" {
		t.Error("convertArg")
	}
	if got := convertArg([]byte("b")); !reflect.DeepEqual(got, []byte("b")) {
		t.Errorf("bytes: %v", got)
	}
}

func TestPlanAdmin(t *testing.T) {
	s := &session{}
	ref := db.ObjRef{Name: "t", Kind: db.KindTable}
	text := func(op db.AdminOp, ref db.ObjRef, opts map[string]string) string {
		st, err := s.PlanAdmin(op, ref, opts)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		var parts []string
		for _, x := range st {
			parts = append(parts, x.Text)
		}
		return strings.Join(parts, "; ")
	}
	checks := []struct{ got, want string }{
		{text(db.OpDropObject, ref, nil), "DROP TABLE `t`"},
		{text(db.OpDropObject, db.ObjRef{Name: "v", Kind: db.KindView}, nil), "DROP VIEW `v`"},
		{text(db.OpDropObject, db.ObjRef{Name: "f", Kind: db.KindFunction}, nil), "DROP FUNCTION `f`"},
		{text(db.OpTruncate, ref, nil), "TRUNCATE TABLE `t`"},
		{text(db.OpClone, ref, map[string]string{"new_name": "t2"}), "CREATE TABLE `t2` LIKE `t`"},
		{text(db.OpClone, ref, map[string]string{"new_name": "t2", "with_data": "true"}), "CREATE TABLE `t2` LIKE `t`; INSERT INTO `t2` SELECT * FROM `t`"},
		{text(db.OpRenameObject, ref, map[string]string{"new_name": "t3"}), "RENAME TABLE `t` TO `t3`"},
		{text(db.OpCreateDatabase, db.ObjRef{Name: "d"}, map[string]string{"encoding": "utf8mb4", "collation": "utf8mb4_bin"}), "CREATE DATABASE `d` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"},
		{text(db.OpDropDatabase, db.ObjRef{Name: "d"}, nil), "DROP DATABASE `d`"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("got %q want %q", c.got, c.want)
		}
	}
	for _, bad := range []struct {
		op   db.AdminOp
		opts map[string]string
	}{
		{db.OpTruncate, map[string]string{"cascade": "true"}},
		{db.OpClone, nil},
		{db.OpRenameDatabase, map[string]string{"new_name": "x"}},
		{db.OpCreateDatabase, map[string]string{"encoding": "x; DROP DATABASE y"}},
	} {
		if _, err := s.PlanAdmin(bad.op, ref, bad.opts); err == nil {
			t.Errorf("%s %v: expected error", bad.op, bad.opts)
		}
	}
}

func cols(defs ...string) []db.Column { // "name type" or "name type null"
	var out []db.Column
	for _, d := range defs {
		f := strings.Fields(d)
		c := db.Column{Name: f[0], OrigName: f[0], Type: f[1], Nullable: len(f) > 2}
		out = append(out, c)
	}
	return out
}

func plan(t *testing.T, old, new *db.TableDef) string {
	t.Helper()
	st, err := planTable(old, new)
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, s := range st {
		parts = append(parts, s.Text)
	}
	return strings.Join(parts, ";\n")
}

func TestPlanTableCreate(t *testing.T) {
	c := cols("id int", "name varchar(10) null")
	c[0].Extra = "auto_increment"
	c[1].Default = "'x'"
	got := plan(t, nil, &db.TableDef{Name: "t", Columns: c, PrimaryKey: []string{"id"},
		Indexes:     []db.Index{{Name: "n_idx", Columns: []string{"name"}, Unique: true}},
		ForeignKeys: []db.ForeignKey{{Name: "fk", Columns: []string{"id"}, RefTable: "o", RefColumns: []string{"id"}, OnDelete: "cascade"}}})
	want := "CREATE TABLE `t` (\n  `id` int NOT NULL auto_increment,\n  `name` varchar(10) NULL DEFAULT 'x',\n  PRIMARY KEY (`id`),\n  UNIQUE INDEX `n_idx` (`name`),\n  CONSTRAINT `fk` FOREIGN KEY (`id`) REFERENCES `o` (`id`) ON DELETE CASCADE\n)"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestPlanTableAlter(t *testing.T) {
	old := &db.TableDef{Name: "t", Columns: cols("a int", "b int", "c int"), PrimaryKey: []string{"a"},
		Indexes:     []db.Index{{Name: "ib", Columns: []string{"b"}}, {Name: "PRIMARY", Primary: true, Columns: []string{"a"}}},
		ForeignKeys: []db.ForeignKey{{Name: "fk1", Columns: []string{"c"}, RefTable: "o", RefColumns: []string{"id"}}}}
	clone := func() *db.TableDef {
		n := *old
		n.Columns = append([]db.Column(nil), old.Columns...)
		n.Indexes = append([]db.Index(nil), old.Indexes...)
		n.ForeignKeys = append([]db.ForeignKey(nil), old.ForeignKeys...)
		n.PrimaryKey = append([]string(nil), old.PrimaryKey...)
		return &n
	}
	if got := plan(t, old, clone()); got != "" {
		t.Errorf("no-op plan emitted %q", got)
	}

	n := clone() // drop c + its FK, add d first, rename b -> bb with new type
	n.Columns = []db.Column{{Name: "d", Type: "text", Nullable: true}, old.Columns[0], old.Columns[1]}
	n.Columns[2].Name, n.Columns[2].Type = "bb", "bigint"
	n.ForeignKeys = nil
	n.Indexes = []db.Index{{Name: "ib", Columns: []string{"bb"}}, old.Indexes[1]}
	got := plan(t, old, n)
	want := "ALTER TABLE `t`\n  DROP FOREIGN KEY `fk1`;\n" +
		"ALTER TABLE `t`\n  DROP COLUMN `c`,\n  ADD COLUMN `d` text NULL FIRST,\n  CHANGE COLUMN `b` `bb` bigint NOT NULL"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}

	n = clone() // reorder: c to the front; rename table; change index and fk
	n.Name = "t2"
	n.Columns = []db.Column{old.Columns[2], old.Columns[0], old.Columns[1]}
	n.Indexes = []db.Index{old.Indexes[1], {Name: "ib", Columns: []string{"b", "a"}, Unique: true}}
	n.ForeignKeys = []db.ForeignKey{{Name: "fk1", Columns: []string{"c"}, RefTable: "o", RefColumns: []string{"id"}, OnDelete: "SET NULL"}}
	got = plan(t, old, n)
	for _, w := range []string{"DROP INDEX `ib`", "MODIFY COLUMN `c` int NOT NULL FIRST", "ADD UNIQUE INDEX `ib` (`b`, `a`)", "ADD CONSTRAINT `fk1` FOREIGN KEY (`c`) REFERENCES `o` (`id`) ON DELETE SET NULL", "RENAME TO `t2`", "DROP FOREIGN KEY `fk1`;"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in\n%s", w, got)
		}
	}
	if strings.Count(got, "ALTER TABLE") != 2 || strings.Contains(got, "MODIFY COLUMN `a`") {
		t.Errorf("unexpected plan\n%s", got)
	}

	n = clone() // primary key change keeps being one ALTER with drop+add
	n.PrimaryKey = []string{"a", "b"}
	if got := plan(t, old, n); got != "ALTER TABLE `t`\n  DROP PRIMARY KEY,\n  ADD PRIMARY KEY (`a`, `b`)" {
		t.Errorf("pk: %s", got)
	}

	n = clone() // renaming a primary key column does not touch the key
	n.Columns[0].Name = "id"
	n.PrimaryKey = []string{"id"}
	if got := plan(t, old, n); strings.Contains(got, "PRIMARY KEY") {
		t.Errorf("pk rename: %s", got)
	}

	n = clone()
	n.Columns[1].Type = ""
	if _, err := planTable(old, n); err == nil {
		t.Error("empty type accepted")
	}
}

func TestSearchableSkipsBinary(t *testing.T) {
	for typ, want := range map[string]bool{"varchar(100)": true, "json": true, "int": true, "longblob": false, "varbinary(16)": false, "blob": false, "point": false} {
		if got := searchable(typ); got != want {
			t.Errorf("searchable(%q) = %v, want %v", typ, got, want)
		}
	}
}

func TestColumnExtra(t *testing.T) {
	for _, c := range []struct{ extra, gen, comment, want string }{
		{"auto_increment", "", "it's", "auto_increment COMMENT 'it''s'"},
		{"DEFAULT_GENERATED on update CURRENT_TIMESTAMP", "", "", "on update CURRENT_TIMESTAMP"},
		{"STORED GENERATED", "(length(`a`) + 1)", "", "GENERATED ALWAYS AS ((length(`a`) + 1)) STORED"},
		{"VIRTUAL GENERATED INVISIBLE", `concat(` + "`a`" + `,_utf8mb4\'x\\\'y\')`, "c", `GENERATED ALWAYS AS (concat(` + "`a`" + `,_utf8mb4'x\'y')) VIRTUAL INVISIBLE COMMENT 'c'`},
	} {
		if got := columnExtra(c.extra, c.gen, c.comment, false); got != c.want {
			t.Errorf("%q %q: got %s want %s", c.extra, c.gen, got, c.want)
		}
	}
}

func TestColumnSQLGeneratedAndCollation(t *testing.T) {
	gen := db.Column{Name: "g", Type: "varchar(20) COLLATE utf8mb4_bin", Extra: "GENERATED ALWAYS AS (upper(`a`)) STORED COMMENT 'x'"}
	if got, _ := columnSQL(gen); got != "`g` varchar(20) COLLATE utf8mb4_bin GENERATED ALWAYS AS (upper(`a`)) STORED COMMENT 'x' NOT NULL" {
		t.Errorf("generated: %s", got)
	}
	gen.Nullable = true
	if got, _ := columnSQL(gen); strings.Contains(got, "NULL") {
		t.Errorf("nullable generated: %s", got)
	}
}

func TestIndexSQLFunctionalPart(t *testing.T) {
	got, err := indexSQL(db.Index{Name: "fx", Columns: []string{"a", "(lower(`b`))"}})
	if err != nil || got != "INDEX `fx` (`a`, (lower(`b`)))" {
		t.Errorf("%s %v", got, err)
	}
}

func TestHasIndexExpr(t *testing.T) {
	for info, want := range map[string]bool{"MySQL 8.4.3": true, "MySQL 8.0.13": true, "MySQL 8.0.12": false, "MySQL 5.7.44-log": false, "MySQL 9.1.0": true, "MariaDB 11.4.2": false} {
		if got := hasIndexExpr(info); got != want {
			t.Errorf("%s: %v", info, got)
		}
	}
}

func TestMariaDBSequencesAndVersionedTables(t *testing.T) {
	s := &session{maria: true, info: "MariaDB 11.4.2"}
	seq := db.ObjRef{Name: "s1", Kind: db.KindSequence}
	if got, _ := s.Script(context.Background(), db.ScriptDrop, seq); got != "DROP SEQUENCE `s1`;" {
		t.Errorf("drop script: %q", got)
	}
	if st, err := s.PlanAdmin(db.OpDropObject, seq, nil); err != nil || st[0].Text != "DROP SEQUENCE `s1`" {
		t.Errorf("drop: %v %v", st, err)
	}
	if st, err := s.PlanAdmin(db.OpRenameObject, seq, map[string]string{"new_name": "s2"}); err != nil || st[0].Text != "RENAME TABLE `s1` TO `s2`" {
		t.Errorf("rename: %v %v", st, err)
	}
	if _, err := s.PlanAdmin(db.OpRenameObject, db.ObjRef{Name: "f", Kind: db.KindFunction}, map[string]string{"new_name": "g"}); err == nil || !strings.Contains(err.Error(), "MariaDB") {
		t.Errorf("rename function: %v", err)
	}

	tbl := db.ObjRef{Name: "v", Kind: db.KindTable}
	if st, _ := s.PlanAdmin(db.OpTruncate, tbl, nil); len(st) != 1 || st[0].Text != "TRUNCATE TABLE `v`" {
		t.Errorf("plain truncate: %v", st)
	}
	s.noteVersioned("", "v", true)
	if st, _ := s.PlanAdmin(db.OpTruncate, tbl, nil); len(st) != 2 || st[0].Text != "DELETE FROM `v`" || st[1].Text != "DELETE HISTORY FROM `v`" {
		t.Errorf("versioned truncate: %v", st)
	}
	old := &db.TableDef{Name: "v", Columns: []db.Column{{Name: "a", OrigName: "a", Type: "int", Nullable: true}}}
	next := &db.TableDef{Name: "v", Columns: []db.Column{{Name: "a", OrigName: "a", Type: "bigint", Nullable: true}}}
	st, err := s.PlanTable(old, next)
	if err != nil || len(st) != 1 || !strings.HasPrefix(st[0].Text, "SET STATEMENT system_versioning_alter_history=KEEP FOR ALTER TABLE `v`") {
		t.Errorf("versioned alter: %v %v", st, err)
	}
	if st, _ := s.PlanTable(nil, next); strings.Contains(st[0].Text, "SET STATEMENT") {
		t.Errorf("create must not be wrapped: %v", st)
	}
	old.Name, next.Name = "other", "other"
	if st, _ := s.PlanTable(old, next); strings.Contains(st[0].Text, "SET STATEMENT") {
		t.Errorf("plain table alter must not be wrapped: %v", st)
	}
}

func TestIsDDL(t *testing.T) {
	for text, want := range map[string]bool{"ALTER TABLE `t` DROP FOREIGN KEY `f`": true, "\n  create table x (a int)": true, "rename table a to b": true, "TRUNCATE TABLE t": true,
		"SET STATEMENT system_versioning_alter_history=KEEP FOR ALTER TABLE t ADD COLUMN a INT": true, "set statement a=1 for update t set a=2": false,
		"SET STATEMENT a=1": false, "SET @a = 1": false,
		"INSERT INTO t VALUES (1)": false, "UPDATE t SET a = 1": false, "": false} {
		if got := isDDL(text); got != want {
			t.Errorf("%q: %v", text, got)
		}
	}
}
