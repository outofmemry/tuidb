package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"tuidb/internal/config"
	"tuidb/internal/db"
)

func TestSSLMode(t *testing.T) {
	for _, m := range []string{"", "prefer", "disable", "require", "verify-ca", "verify-full"} {
		if _, err := sslMode(m); err != nil {
			t.Errorf("%q: %v", m, err)
		}
	}
	if _, err := sslMode("verify-identity"); err == nil {
		t.Error("verify-identity should be rejected")
	}
}

func TestConnString(t *testing.T) {
	c := config.Connection{Host: "h", Port: 5433, User: "u@x", Password: "p/w:d", SSLMode: "verify-full",
		Options: map[string]string{"application_name": "tuidb"}}
	cs, err := connString(c, "my db")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := pgx.ParseConfig(cs)
	if err != nil {
		t.Fatalf("%s: %v", cs, err)
	}
	if cfg.Host != "h" || cfg.Port != 5433 || cfg.User != "u@x" || cfg.Password != "p/w:d" || cfg.Database != "my db" {
		t.Errorf("parsed %+v", cfg.Config)
	}
	if cfg.RuntimeParams["application_name"] != "tuidb" {
		t.Errorf("options lost: %v", cfg.RuntimeParams)
	}
	if cfg.TLSConfig == nil || cfg.TLSConfig.InsecureSkipVerify {
		t.Errorf("verify-full must verify: %+v", cfg.TLSConfig)
	}
	cs, _ = connString(config.Connection{SSLCA: "/ca.pem", SSLCert: "/c.pem", SSLKey: "/k.pem"}, "d")
	for _, p := range []string{"sslrootcert=%2Fca.pem", "sslcert=%2Fc.pem", "sslkey=%2Fk.pem"} {
		if !strings.Contains(cs, p) {
			t.Errorf("%s missing %s", cs, p)
		}
	}
	if _, err := connString(config.Connection{SSLMode: "verify-identity"}, ""); err == nil {
		t.Error("expected ssl error")
	}
	cs, _ = connString(config.Connection{Host: "h", SSLMode: "disable"}, "d")
	if cfg, err = pgx.ParseConfig(cs); err != nil || cfg.TLSConfig != nil {
		t.Errorf("disable: %v %v", err, cfg.TLSConfig)
	}
}

func TestNormalize(t *testing.T) {
	tm := pgtype.NewMap()
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	cases := []struct {
		oid  uint32
		in   string
		want any
	}{
		{pgtype.BoolOID, "t", true},
		{pgtype.BoolOID, "f", false},
		{pgtype.Int4OID, "-7", int64(-7)},
		{pgtype.Int8OID, "9223372036854775807", int64(9223372036854775807)},
		{pgtype.OIDOID, "4294967295", uint64(4294967295)},
		{pgtype.Float8OID, "1.5", 1.5},
		{pgtype.NumericOID, "12.3400", "12.3400"},
		{pgtype.TextOID, "hi", "hi"},
		{pgtype.JSONBOID, `{"a": 1}`, `{"a": 1}`},
		{pgtype.UUIDOID, "6f9619ff-8b86-d011-b42d-00c04fc964ff", "6f9619ff-8b86-d011-b42d-00c04fc964ff"},
		{pgtype.ByteaOID, `\x6869`, []byte("hi")},
		{pgtype.TimestampOID, "2024-01-02 03:04:05", ts},
		{pgtype.TimestamptzOID, "2024-01-02 03:04:05+00", ts},
		{pgtype.DateOID, "2024-01-02", time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)},
		{pgtype.DateOID, "infinity", "infinity"},
	}
	for _, c := range cases {
		got := normalize(tm, c.oid, []byte(c.in))
		if gt, ok := got.(time.Time); ok {
			if !gt.Equal(c.want.(time.Time)) {
				t.Errorf("oid %d %q: %v", c.oid, c.in, got)
			}
			continue
		}
		if g, w := toStr(got), toStr(c.want); g != w {
			t.Errorf("oid %d %q: got %#v want %#v", c.oid, c.in, got, c.want)
		}
	}
	if normalize(tm, pgtype.TextOID, nil) != nil {
		t.Error("NULL must be nil")
	}
}

func toStr(v any) string {
	return strings.TrimSpace(strings.Join([]string{typeName(v), db.FormatValue(v)}, ":"))
}

func typeName(v any) string {
	switch v.(type) {
	case string:
		return "s"
	case []byte:
		return "b"
	case int64:
		return "i"
	case uint64:
		return "u"
	case float64:
		return "f"
	case bool:
		return "t"
	}
	return "?"
}

func TestParseTrigger(t *testing.T) {
	tr := parseTrigger("orders_audit", "CREATE TRIGGER orders_audit AFTER INSERT ON public.orders FOR EACH ROW EXECUTE FUNCTION log_order()")
	if tr.Timing != "AFTER" || tr.Event != "INSERT" || tr.Statement != "FOR EACH ROW EXECUTE FUNCTION log_order()" {
		t.Errorf("%+v", tr)
	}
	tr = parseTrigger("x", `CREATE TRIGGER "my trg" INSTEAD OF INSERT OR UPDATE OF a ON "my view" FOR EACH ROW EXECUTE FUNCTION f()`)
	if tr.Timing != "INSTEAD OF" || tr.Event != "INSERT OR UPDATE OF a" {
		t.Errorf("%+v", tr)
	}
}

func TestNoTx(t *testing.T) {
	for in, want := range map[string]bool{"create database x": true, "DROP  DATABASE x": true, "CREATE INDEX CONCURRENTLY i ON t(a)": true,
		"CREATE INDEX i ON t(a)": false, "DROP TABLE t": false} {
		if noTx(in) != want {
			t.Errorf("%q", in)
		}
	}
}

func texts(sts []db.Statement) []string {
	out := make([]string, len(sts))
	for i, s := range sts {
		out[i] = s.Text
	}
	return out
}

func TestPlanAdmin(t *testing.T) {
	s := &session{schema: "public"}
	tbl := db.ObjRef{Schema: "public", Name: "t", Kind: db.KindTable}
	cases := []struct {
		op   db.AdminOp
		ref  db.ObjRef
		opts map[string]string
		want []string
	}{
		{db.OpDropObject, tbl, map[string]string{"cascade": "true"}, []string{`DROP TABLE "public"."t" CASCADE`}},
		{db.OpDropObject, db.ObjRef{Name: "f", Kind: db.KindFunction}, nil, []string{`DROP FUNCTION "public"."f"`}},
		{db.OpDropObject, db.ObjRef{Name: "v", Kind: db.KindMatView}, nil, []string{`DROP MATERIALIZED VIEW "public"."v"`}},
		{db.OpTruncate, tbl, map[string]string{"restart_identity": "true", "cascade": "1"}, []string{`TRUNCATE TABLE "public"."t" RESTART IDENTITY CASCADE`}},
		{db.OpClone, tbl, map[string]string{"new_name": "t2", "with_data": "true"}, []string{`CREATE TABLE "public"."t2" (LIKE "public"."t" INCLUDING ALL)`, `INSERT INTO "public"."t2" SELECT * FROM "public"."t"`}},
		{db.OpClone, tbl, map[string]string{"new_name": "t2"}, []string{`CREATE TABLE "public"."t2" (LIKE "public"."t" INCLUDING ALL)`}},
		{db.OpRenameObject, tbl, map[string]string{"new_name": "t3"}, []string{`ALTER TABLE "public"."t" RENAME TO "t3"`}},
		{db.OpCreateDatabase, db.ObjRef{Name: "d"}, map[string]string{"encoding": "UTF8", "collation": "C"}, []string{`CREATE DATABASE "d" ENCODING 'UTF8' LC_COLLATE 'C' LC_CTYPE 'C' TEMPLATE template0`}},
		{db.OpDropDatabase, db.ObjRef{Name: "d"}, nil, []string{`DROP DATABASE "d"`}},
		{db.OpRenameDatabase, db.ObjRef{Name: "d"}, map[string]string{"new_name": "e"}, []string{`ALTER DATABASE "d" RENAME TO "e"`}},
	}
	for _, c := range cases {
		got, err := s.PlanAdmin(c.op, c.ref, c.opts)
		if err != nil || strings.Join(texts(got), "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: %v %q", c.op, err, texts(got))
		}
	}
	if _, err := s.PlanAdmin(db.OpTruncate, db.ObjRef{Name: "v", Kind: db.KindView}, nil); err == nil {
		t.Error("truncate view must fail")
	}
	if _, err := s.PlanAdmin(db.OpRenameObject, tbl, nil); err == nil {
		t.Error("rename needs new_name")
	}
}

func TestPlanTableCreate(t *testing.T) {
	s := &session{schema: "public"}
	got, err := s.PlanTable(nil, &db.TableDef{Name: "t",
		Columns:     []db.Column{{Name: "id", Type: "integer", Extra: "GENERATED ALWAYS AS IDENTITY"}, {Name: "n", Type: "text", Nullable: true, Default: "'x'", HasDef: true}, {Name: "o", Type: "int", Nullable: true}},
		PrimaryKey:  []string{"id"},
		Indexes:     []db.Index{{Name: "t_n_idx", Columns: []string{"n"}, Unique: true, Algorithm: "btree", Condition: "n IS NOT NULL"}, {Name: "t_pkey", Primary: true, Columns: []string{"id"}}, {Name: "lower_idx", Columns: []string{"lower(n)"}}},
		ForeignKeys: []db.ForeignKey{{Name: "t_o_fk", Columns: []string{"o"}, RefTable: "other", RefColumns: []string{"id"}, OnDelete: "CASCADE"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"CREATE TABLE \"public\".\"t\" (\n  \"id\" integer GENERATED ALWAYS AS IDENTITY NOT NULL,\n  \"n\" text DEFAULT 'x',\n  \"o\" int,\n  PRIMARY KEY (\"id\"),\n  CONSTRAINT \"t_o_fk\" FOREIGN KEY (\"o\") REFERENCES \"other\" (\"id\") ON DELETE CASCADE\n)",
		`CREATE UNIQUE INDEX "t_n_idx" ON "public"."t" USING btree ("n") WHERE n IS NOT NULL`,
		`CREATE INDEX "lower_idx" ON "public"."t" (lower(n))`,
	}
	if g := texts(got); strings.Join(g, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s", strings.Join(g, "\n"))
	}
	if _, err := s.PlanTable(nil, &db.TableDef{Name: "t", Columns: []db.Column{{Name: "a"}}}); err == nil {
		t.Error("column without type must fail")
	}
}

func TestPlanTableAlter(t *testing.T) {
	s := &session{schema: "public"}
	old := &db.TableDef{Schema: "public", Name: "t",
		Columns:     []db.Column{{Name: "id", Type: "integer", PK: true}, {Name: "a", Type: "text", Nullable: true}, {Name: "gone", Type: "int", Nullable: true}, {Name: "o", Type: "int", Nullable: true}},
		PrimaryKey:  []string{"id"},
		Indexes:     []db.Index{{Name: "t_pkey", Primary: true, Columns: []string{"id"}, Unique: true}, {Name: "old_idx", Columns: []string{"a"}}},
		ForeignKeys: []db.ForeignKey{{Name: "old_fk", Columns: []string{"o"}, RefTable: "p", RefColumns: []string{"id"}}}}
	nw := &db.TableDef{Schema: "public", Name: "t2",
		Columns: []db.Column{
			{Name: "id", OrigName: "id", Type: "integer"},
			{Name: "b", OrigName: "a", Type: "varchar(20)", Default: "'z'", HasDef: true},
			{Name: "o", OrigName: "o", Type: "int", Nullable: true},
			{Name: "fresh", Type: "int", Nullable: true}},
		PrimaryKey:  []string{"id", "fresh"},
		Indexes:     []db.Index{{Name: "t_pkey", Primary: true, Columns: []string{"id"}}, {Name: "new_idx", Columns: []string{"b"}}},
		ForeignKeys: []db.ForeignKey{{Name: "new_fk", Columns: []string{"o"}, RefSchema: "public", RefTable: "p", RefColumns: []string{"id"}, OnDelete: "SET NULL"}}}
	got, err := s.PlanTable(old, nw)
	if err != nil {
		t.Fatal(err)
	}
	T := `ALTER TABLE "public"."t" `
	want := []string{
		T + `DROP CONSTRAINT "old_fk"`,
		T + `DROP CONSTRAINT "t_pkey"`,
		`DROP INDEX "public"."old_idx"`,
		T + `DROP COLUMN "gone"`,
		T + `RENAME COLUMN "a" TO "b"`,
		T + `ALTER COLUMN "b" TYPE varchar(20) USING "b"::varchar(20)`,
		T + `ALTER COLUMN "b" SET NOT NULL`,
		T + `ALTER COLUMN "b" SET DEFAULT 'z'`,
		T + `ADD COLUMN "fresh" int`,
		T + `ADD PRIMARY KEY ("id", "fresh")`,
		`CREATE INDEX "new_idx" ON "public"."t" ("b")`,
		T + `ADD CONSTRAINT "new_fk" FOREIGN KEY ("o") REFERENCES "public"."p" ("id") ON DELETE SET NULL`,
		T + `RENAME TO "t2"`,
	}
	// "id" lost nullability info in the fixture (Nullable false both sides), so no NOT NULL change for it.
	if g := texts(got); strings.Join(g, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(g, "\n"), strings.Join(want, "\n"))
	}
	if _, err := s.PlanTable(old, &db.TableDef{Name: "t", Columns: []db.Column{{Name: "x", OrigName: "nope", Type: "int"}}}); err == nil {
		t.Error("unknown OrigName must fail")
	}
	cp := *old
	cp.Columns = nil
	for _, c := range old.Columns {
		c.OrigName = c.Name
		cp.Columns = append(cp.Columns, c)
	}
	same, _ := s.PlanTable(old, &cp)
	if len(same) != 0 {
		t.Errorf("no-op diff produced %q", texts(same))
	}
}

func TestSearchableSkipsBinary(t *testing.T) {
	for typ, want := range map[string]bool{"text": true, "jsonb": true, "integer": true, "bytea": false, "tsvector": false} {
		if got := searchable(typ); got != want {
			t.Errorf("searchable(%q) = %v, want %v", typ, got, want)
		}
	}
}
