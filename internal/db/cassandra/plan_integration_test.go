package cassandra

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"tuidb/internal/db"
)

const allTypesRow = `INSERT INTO all_types (id, a, t, ti, si, i, bi, vi, f, d, dec, b, u, tu, ts, dt, tm, dur, ip, bl, l, s, m, fl, tup, addr, v) VALUES
 (1, 'asc', 'tëxt; ''q''', 1, 2, 3, 4, 123456789012345678901234567890, 0.1, 2.5, 3.25, true, 123e4567-e89b-12d3-a456-426614174000,
  50554d6e-29bb-11e5-b345-feff819cdc9f, '2024-05-06 07:08:09.123+0000', '2024-05-06', '07:08:09.5', 1mo2d3h, '10.0.0.1', 0xcafe,
  [1, 2], {'x', 'y'}, {'k': 1}, [3, 4], (7, 'seven'), {street: 's', city: 'c', zip: 1}, [1.0, 2.0, 3.0])`

func allTypes(t *testing.T, s *session, id string) *db.Result {
	t.Helper()
	r, err := s.Rows(context.Background(), db.RowQuery{ObjRef: ref("all_types"), Filters: fs(f("id", "=", id))})
	if err != nil || len(r.Rows) != 1 {
		t.Fatalf("all_types %s: %v %v", id, r, err)
	}
	return r
}

// col is the first row's value of a column (SELECT * lists regular columns alphabetically, so never by index).
func col(r *db.Result, name string) any {
	for i, c := range r.Columns {
		if c.Name == name {
			return r.Rows[0][i]
		}
	}
	return "no column " + name
}

func TestIntegrationNormalize(t *testing.T) {
	s := open(t)
	exec(t, s, allTypesRow)
	exec(t, s, "INSERT INTO all_types (id) VALUES (2)")
	r := allTypes(t, s, "1")
	got := map[string]any{}
	types := map[string]string{}
	for i, c := range r.Columns {
		got[c.Name] = r.Rows[0][i]
		types[c.Name] = c.Type
	}
	ts := time.Date(2024, 5, 6, 7, 8, 9, 123000000, time.UTC)
	want := map[string]any{
		"id": int64(1), "a": "asc", "t": "tëxt; 'q'", "ti": int64(1), "si": int64(2), "i": int64(3), "bi": int64(4),
		"vi": "123456789012345678901234567890", "f": 0.1, "d": 2.5, "dec": "3.25", "b": true,
		"u": "123e4567-e89b-12d3-a456-426614174000", "tu": "50554d6e-29bb-11e5-b345-feff819cdc9f", "ts": ts, "dt": "2024-05-06",
		"tm": "07:08:09.5", "dur": "1mo2d10800000000000ns", "ip": "10.0.0.1",
		"l": "[1,2]", "s": `["x","y"]`, "m": `{"k":1}`, "fl": "[3,4]", "tup": `[7,"seven"]`, "addr": `{"city":"c","street":"s","zip":1}`, "v": "[1,2,3]",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: got %#v want %#v", k, got[k], w)
		}
	}
	if b, ok := got["bl"].([]byte); !ok || string(b) != "\xca\xfe" {
		t.Errorf("blob %#v", got["bl"])
	}
	for col, ty := range map[string]string{"id": "int", "t": "text", "vi": "varint", "tu": "timeuuid", "l": "list<int>", "s": "set<text>", "m": "map<text, int>",
		"tup": "tuple<int, text>", "addr": "address", "v": "vector<float, 3>", "dur": "duration", "dt": "date", "tm": "time"} {
		if types[col] != ty {
			t.Errorf("type of %s: %q want %q", col, types[col], ty)
		}
	}
	// a row of NULLs: every non-key cell is nil (empty collections and absent values alike)
	r = allTypes(t, s, "2")
	for i, c := range r.Columns[1:] {
		if r.Rows[0][i+1] != nil {
			t.Errorf("%s of an empty row: %#v", c.Name, r.Rows[0][i+1])
		}
	}
	// an empty string is not NULL
	exec(t, s, "INSERT INTO all_types (id, t) VALUES (3, '')")
	if r := allTypes(t, s, "3"); col(r, "t") != "" {
		t.Errorf("empty string: %#v", col(r, "t"))
	}
	// a big varint stays a string, a small one is a number
	exec(t, s, "INSERT INTO all_types (id, vi) VALUES (4, 42)")
	if r := allTypes(t, s, "4"); col(r, "vi") != int64(42) {
		t.Errorf("small varint: %#v", col(r, "vi"))
	}
	exec(t, s, "INSERT INTO all_types (id, f, d) VALUES (5, NaN, Infinity)")
	if r := allTypes(t, s, "5"); !math.IsNaN(col(r, "f").(float64)) || !math.IsInf(col(r, "d").(float64), 1) {
		t.Errorf("NaN/Inf: %#v", r.Rows[0])
	}
}

// Every column of a row, edited to the text the grid shows for it, must write the same value back: this is the path a cell edit takes.
func TestIntegrationEditRoundTrip(t *testing.T) {
	s := open(t)
	exec(t, s, allTypesRow)
	orig := allTypes(t, s, "1")
	var set []db.KeyVal
	for i, c := range orig.Columns {
		if c.Name != "id" {
			set = append(set, kv(c.Name, db.FormatValue(orig.Rows[0][i])))
		}
	}
	exec(t, s, "INSERT INTO all_types (id) VALUES (10)") // the edited row: all NULL to start with
	apply(t, s, db.Change{Kind: db.Update, ObjRef: ref("all_types"), Key: []db.KeyVal{kv("id", int64(10))}, Set: set})
	back := allTypes(t, s, "10")
	for i, c := range orig.Columns[1:] {
		a, b := orig.Rows[0][i+1], back.Rows[0][i+1]
		if ab, ok := a.([]byte); ok {
			a, b = string(ab), string(b.([]byte))
		}
		if a != b {
			t.Errorf("%s: wrote %#v, read back %#v", c.Name, a, b)
		}
	}
	// the same as an INSERT of a new row
	ins := append([]db.KeyVal{kv("id", "11")}, set...)
	apply(t, s, db.Change{Kind: db.Insert, ObjRef: ref("all_types"), Set: ins})
	if got := allTypes(t, s, "11"); col(got, "a") != "asc" || col(got, "l") != `[1,2]` {
		t.Errorf("inserted row %v", got.Rows[0])
	}
	// NULL writes erase the value (a tombstone); NULLs in an INSERT are left out instead
	nulls := make([]db.KeyVal, len(set))
	for i, c := range set {
		nulls[i] = kv(c.Col, db.Null)
	}
	apply(t, s, db.Change{Kind: db.Update, ObjRef: ref("all_types"), Key: []db.KeyVal{kv("id", int64(10))}, Set: nulls})
	for i, v := range allTypes(t, s, "10").Rows[0] {
		if i > 0 && v != nil {
			t.Errorf("column %d not NULL: %#v", i, v)
		}
	}
	st, err := s.Plan([]db.Change{{Kind: db.Insert, ObjRef: ref("all_types"), Set: []db.KeyVal{kv("id", "12"), kv("t", db.Null), kv("i", db.Default)}}})
	if err != nil || st[0].Text != `INSERT INTO "tuidb"."all_types" ("id") VALUES (?)` {
		t.Errorf("insert with NULLs: %v %v", st, err)
	}
	// invalid text fails at Plan time with the column named, before anything is sent
	for col, bad := range map[string]string{"i": "x", "ts": "yesterday", "l": "[1,", "b": "maybe", "dec": "1.2.3", "tm": "noon", "bl": "zz", "u": "nope", "f": "x"} {
		if _, err := s.Plan([]db.Change{{Kind: db.Update, ObjRef: ref("all_types"), Key: []db.KeyVal{kv("id", int64(10))}, Set: []db.KeyVal{kv(col, bad)}}}); err == nil {
			// gocql validates strings for uuid/int lazily, at Apply
			if col != "u" && col != "i" {
				t.Errorf("%s=%q must fail", col, bad)
			}
		}
	}
	exec(t, s, "DELETE FROM all_types WHERE id IN (10, 11)")
}

func TestIntegrationPlanShapes(t *testing.T) {
	s := open(t)
	plan := func(c db.Change) db.Statement {
		t.Helper()
		st, err := s.Plan([]db.Change{c})
		if err != nil {
			t.Fatal(err)
		}
		return st[0]
	}
	st := plan(db.Change{Kind: db.Update, ObjRef: ref("customers"), Key: []db.KeyVal{kv("id", int64(1))}, Set: []db.KeyVal{kv("name", "O'Brien"), kv("notes", db.Null), kv("email", db.Empty), kv("balance", "1.5"), kv("tags", `["a"]`), kv("created_at", db.Expr("toTimestamp(now())"))}})
	if st.Text != `UPDATE "tuidb"."customers" SET "name" = ?, "notes" = NULL, "email" = ?, "balance" = ?, "tags" = fromJson(?), "created_at" = toTimestamp(now()) WHERE "id" = ?` {
		t.Errorf("update text %s", st.Text)
	}
	if !strings.Contains(st.Display, `"name" = 'O''Brien'`) || !strings.Contains(st.Display, `"tags" = fromJson('["a"]')`) || !strings.HasSuffix(st.Display, `WHERE "id" = 1`) {
		t.Errorf("update display %s", st.Display)
	}
	st = plan(db.Change{Kind: db.Update, ObjRef: ref("all_types"), Key: []db.KeyVal{kv("id", int64(1))}, Set: []db.KeyVal{kv("ts", db.Expr("NOW()")), kv("dt", db.Expr("now()")), kv("tu", db.Expr("NOW()"))}})
	if st.Text != `UPDATE "tuidb"."all_types" SET "ts" = toTimestamp(now()), "dt" = toDate(now()), "tu" = NOW() WHERE "id" = ?` {
		t.Errorf("NOW(): %s", st.Text)
	}
	if st := plan(db.Change{Kind: db.Delete, ObjRef: ref("orders"), Key: []db.KeyVal{kv("customer_id", int64(1)), kv("id", int64(2))}}); st.Text != `DELETE FROM "tuidb"."orders" WHERE "customer_id" = ? AND "id" = ?` {
		t.Errorf("delete %s", st.Text)
	}
	for name, c := range map[string]db.Change{
		"update without key":   {Kind: db.Update, ObjRef: ref("customers"), Set: []db.KeyVal{kv("name", "x")}},
		"delete without key":   {Kind: db.Delete, ObjRef: ref("customers")},
		"NULL key":             {Kind: db.Delete, ObjRef: ref("customers"), Key: []db.KeyVal{kv("id", nil)}},
		"empty insert":         {Kind: db.Insert, ObjRef: ref("customers")},
		"insert only NULLs":    {Kind: db.Insert, ObjRef: ref("customers"), Set: []db.KeyVal{kv("name", db.Null)}},
		"update without set":   {Kind: db.Update, ObjRef: ref("customers"), Key: []db.KeyVal{kv("id", int64(1))}},
		"counter insert":       {Kind: db.Insert, ObjRef: ref("hits"), Set: []db.KeyVal{kv("page", "p"), kv("n", "1")}},
		"counter plain value":  {Kind: db.Update, ObjRef: ref("hits"), Key: []db.KeyVal{kv("page", "p")}, Set: []db.KeyVal{kv("n", "7")}},
		"unknown table":        {Kind: db.Delete, ObjRef: ref("nope"), Key: []db.KeyVal{kv("id", int64(1))}},
		"invalid JSON for set": {Kind: db.Update, ObjRef: ref("customers"), Key: []db.KeyVal{kv("id", int64(1))}, Set: []db.KeyVal{kv("tags", "a,b")}},
	} {
		if _, err := s.Plan([]db.Change{c}); err == nil {
			t.Errorf("%s must be an error", name)
		}
	}
}

func TestIntegrationCounterAndStatic(t *testing.T) {
	s := open(t)
	page := tempTable(t, s)
	exec(t, s, "CREATE TABLE "+q(page)+" (page text PRIMARY KEY, n counter)")
	count := func() any {
		r := exec(t, s, "SELECT n FROM "+q(page)+" WHERE page = 'home'")
		return r.Rows[0][0]
	}
	key := []db.KeyVal{kv("page", "home")}
	apply(t, s, db.Change{Kind: db.Update, ObjRef: ref(page), Key: key, Set: []db.KeyVal{kv("n", "+5")}})
	apply(t, s, db.Change{Kind: db.Update, ObjRef: ref(page), Key: key, Set: []db.KeyVal{kv("n", "-2")}})
	if count() != int64(3) {
		t.Errorf("counter %v", count())
	}
	// two counter updates are applied one by one (a logged batch cannot hold them)
	apply(t, s,
		db.Change{Kind: db.Update, ObjRef: ref(page), Key: key, Set: []db.KeyVal{kv("n", "+1")}},
		db.Change{Kind: db.Update, ObjRef: ref(page), Key: key, Set: []db.KeyVal{kv("n", "+1")}})
	if count() != int64(5) {
		t.Errorf("counter after two updates %v", count())
	}
	apply(t, s, db.Change{Kind: db.Delete, ObjRef: ref(page), Key: key})
	if r := exec(t, s, "SELECT n FROM "+q(page)); len(r.Rows) != 0 {
		t.Errorf("counter row not deleted %v", r.Rows)
	}

	// a static column is updated with the partition key alone and shows on every row of the partition
	apply(t, s, db.Change{Kind: db.Update, ObjRef: ref("events"), Key: []db.KeyVal{kv("tenant", "acme"), kv("ts", time.Date(2024, 3, 2, 0, 0, 0, 0, time.UTC))}, Set: []db.KeyVal{kv("tenant_name", "Acme Inc"), kv("kind", "logout2")}})
	r := exec(t, s, "SELECT tenant_name, kind FROM events WHERE tenant = 'acme'")
	eq(t, "static", cells(r), []string{"Acme Inc|login", "Acme Inc|logout2"})
	exec(t, s, "UPDATE events SET tenant_name = 'Acme Corp', kind = 'logout' WHERE tenant = 'acme' AND ts = '2024-03-02 00:00:00+0000'")
}

// Keys of many types: the grid hands back the values Rows returned, and they must match the same row.
func TestIntegrationKeyTypes(t *testing.T) {
	s := open(t)
	tbl := tempTable(t, s)
	exec(t, s, "CREATE TABLE "+q(tbl)+" (u uuid, ts timestamp, d date, bi bigint, dec decimal, ip inet, b boolean, tm time, v text, PRIMARY KEY ((u, ts, d), bi, dec, ip, b, tm))")
	exec(t, s, "INSERT INTO "+q(tbl)+" (u, ts, d, bi, dec, ip, b, tm, v) VALUES (123e4567-e89b-12d3-a456-426614174000, '2024-05-06 07:08:09.123+0000', '2024-05-06', 9000000000, 1.50, '::1', false, '01:02:03.004', 'old')")
	r, err := s.Rows(context.Background(), db.RowQuery{ObjRef: ref(tbl)})
	if err != nil || len(r.Rows) != 1 {
		t.Fatal(r, err)
	}
	key := func() []db.KeyVal {
		var k []db.KeyVal
		for i, c := range r.Columns {
			if c.Name != "v" {
				k = append(k, kv(c.Name, r.Rows[0][i]))
			}
		}
		return k
	}
	apply(t, s, db.Change{Kind: db.Update, ObjRef: ref(tbl), Key: key(), Set: []db.KeyVal{kv("v", "new")}})
	if got := exec(t, s, "SELECT v FROM "+q(tbl)); cells(got)[0] != "new" {
		t.Errorf("update by typed key: %v", cells(got))
	}
	// and with the key as text, as a hand-built change would have it
	apply(t, s, db.Change{Kind: db.Update, ObjRef: ref(tbl), Set: []db.KeyVal{kv("v", "newer")}, Key: []db.KeyVal{
		kv("u", "123e4567-e89b-12d3-a456-426614174000"), kv("ts", "2024-05-06 07:08:09.123"), kv("d", "2024-05-06"), kv("bi", "9000000000"),
		kv("dec", "1.50"), kv("ip", "::1"), kv("b", "false"), kv("tm", "01:02:03.004")}})
	if got := exec(t, s, "SELECT v FROM "+q(tbl)); cells(got)[0] != "newer" {
		t.Errorf("update by text key: %v", cells(got))
	}
	apply(t, s, db.Change{Kind: db.Delete, ObjRef: ref(tbl), Key: key()})
	if got := exec(t, s, "SELECT count(*) FROM "+q(tbl)); got.Rows[0][0] != int64(0) {
		t.Errorf("delete by typed key left %v", got.Rows)
	}
}

func TestIntegrationApplyBatch(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	tbl := tempTable(t, s)
	exec(t, s, "CREATE TABLE "+q(tbl)+" (id int PRIMARY KEY, v int)")
	qt := `"tuidb".` + q(tbl)
	ins := func(id, v string) db.Statement {
		return db.Statement{Text: "INSERT INTO " + qt + " (id, v) VALUES (" + id + ", " + v + ")", Display: "insert " + id}
	}
	// all DML: one logged batch, all or nothing: the bad statement stops the good ones from being applied
	err := s.Apply(ctx, []db.Statement{ins("1", "1"), ins("2", "2"), {Text: "INSERT INTO " + qt + " (id, v) VALUES (3, 'not a number')"}})
	if err == nil || !strings.Contains(err.Error(), "nothing was applied") {
		t.Fatalf("bad batch: %v", err)
	}
	if r := exec(t, s, "SELECT count(*) FROM "+qt); r.Rows[0][0] != int64(0) {
		t.Errorf("a failed batch applied %v rows", r.Rows[0][0])
	}
	if err := s.Apply(ctx, []db.Statement{ins("1", "1"), ins("2", "2"), ins("3", "3")}); err != nil {
		t.Fatal(err)
	}
	if r := exec(t, s, "SELECT count(*) FROM "+qt); r.Rows[0][0] != int64(3) {
		t.Errorf("batch applied %v rows", r.Rows[0][0])
	}
	// with DDL in the mix there is no batch: statements run in order and the error says how many stayed applied
	err = s.Apply(ctx, []db.Statement{ins("4", "4"), {Text: "ALTER TABLE " + qt + " ADD extra text"}, {Text: "ALTER TABLE " + qt + " ADD extra text", Display: "add extra again"}, ins("5", "5")})
	if err == nil || !strings.Contains(err.Error(), "2 of 4 statements were already applied") || !strings.Contains(err.Error(), "add extra again") {
		t.Fatalf("partial failure: %v", err)
	}
	if r := exec(t, s, "SELECT count(*) FROM "+qt); r.Rows[0][0] != int64(4) {
		t.Errorf("rows after partial apply %v", r.Rows[0][0])
	}
	// one statement, and more statements than a batch should hold, also run one by one
	var many []db.Statement
	for i := 100; i < 100+maxBatch+5; i++ {
		many = append(many, ins(strconv.Itoa(i), "0"))
	}
	if err := s.Apply(ctx, many); err != nil {
		t.Fatal(err)
	}
	if r := exec(t, s, "SELECT count(*) FROM "+qt); r.Rows[0][0] != int64(4+maxBatch+5) {
		t.Errorf("large apply wrote %v rows", r.Rows[0][0])
	}
	if batchable(many) || batchable(many[:1]) || !batchable(many[:2]) {
		t.Error("batchable thresholds")
	}
}
