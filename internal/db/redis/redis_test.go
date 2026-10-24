package redis

import (
	"crypto/tls"
	"errors"
	"reflect"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"

	"tuidb/internal/config"
	"tuidb/internal/db"
)

func TestParseQuoteRoundTrip(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{`SET k v`, []string{"SET", "k", "v"}},
		{`  SET   "a b"  'c "d" \n' `, []string{"SET", "a b", `c "d" \n`}},
		{`SET k "q\"uo\\te\n\x41\xff"`, []string{"SET", "k", "q\"uo\\te\nA\xff"}},
		{`GET ""`, []string{"GET", ""}},
		{``, nil},
	}
	for _, c := range cases {
		got, err := parseCommand(c.line)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseCommand(%q) = %q, %v; want %q", c.line, got, err, c.want)
		}
	}
	for _, bad := range []string{`SET "x`, `SET 'x`, `SET "x"y`} {
		if _, err := parseCommand(bad); err == nil {
			t.Errorf("parseCommand(%q) should fail", bad)
		}
	}
	args := []string{"SET", "key with space", "", "O'Neil \"q\" \\ \n\t", "héllo ✓", "bin\x00\xff\x01", "plain:key-1.2"}
	back, err := parseCommand(formatCommand(args...))
	if err != nil || !reflect.DeepEqual(back, args) {
		t.Errorf("round trip = %q, %v", back, err)
	}
	if got := quoteArg("plain:key"); got != "plain:key" {
		t.Errorf("plain key was quoted: %s", got)
	}
}

func TestTLSConfig(t *testing.T) {
	for mode, want := range map[string]bool{"": false, "prefer": false, "disable": false, "require": true, "verify-ca": true, "verify-full": true, "verify-identity": true} {
		cfg, err := tlsConfig(config.Connection{SSLMode: mode}, "h")
		if err != nil || (cfg != nil) != want {
			t.Errorf("mode %q: cfg=%v err=%v", mode, cfg, err)
		}
	}
	if _, err := tlsConfig(config.Connection{SSLMode: "bogus"}, "h"); err == nil {
		t.Error("bogus mode accepted")
	}
	cfg, _ := tlsConfig(config.Connection{SSLMode: "require"}, "h")
	if !cfg.InsecureSkipVerify {
		t.Error("require should not verify")
	}
	cfg, _ = tlsConfig(config.Connection{SSLMode: "verify-full"}, "h")
	if cfg.InsecureSkipVerify || cfg.ServerName != "h" || cfg.MinVersion < tls.VersionTLS12 {
		t.Errorf("verify-full config wrong: %+v", cfg)
	}
	if _, err := tlsConfig(config.Connection{SSLMode: "require", SSLCA: "/nonexistent"}, "h"); err == nil {
		t.Error("missing CA accepted")
	}
}

func TestBuildOptions(t *testing.T) {
	o, err := buildOptions(config.Connection{Host: "h", Database: "3", User: "u", Password: "p"}, nil)
	if err != nil || o.Addr != "h:6379" || o.DB != 3 || o.Username != "u" || o.PoolSize != 8 {
		t.Errorf("opts = %+v, %v", o, err)
	}
	if _, err := buildOptions(config.Connection{Host: "h", Database: "x"}, nil); err == nil {
		t.Error("bad db accepted")
	}
	if _, err := buildOptions(config.Connection{Host: "h", Bootstrap: []string{`SET "x`}}, nil); err == nil {
		t.Error("bad bootstrap accepted")
	}
}

func TestRender(t *testing.T) {
	var r db.Result
	render(&r, "OK", 0)
	if r.HasRows || r.Message != "OK" {
		t.Errorf("OK: %+v", r)
	}
	r = db.Result{}
	render(&r, []any{"a", int64(2), []any{"x", "y"}, nil}, 3)
	if !r.HasRows || !r.Truncated || len(r.Rows) != 3 || r.Rows[2][1] != `["x","y"]` || r.Rows[1][1] != int64(2) {
		t.Errorf("array: %+v", r)
	}
	r = db.Result{}
	render(&r, map[any]any{"b": "2", "a": "1"}, 0)
	if r.Rows[0][0] != "a" || r.Rows[1][1] != "2" {
		t.Errorf("map: %+v", r)
	}
	// rows past the limit are never built (P1-56)
	big := make([]any, 10000)
	bigMap := map[any]any{}
	for i := range big {
		big[i] = []any{"nested", i}
		bigMap[i] = "v"
	}
	r = db.Result{}
	render(&r, big, 5)
	if len(r.Rows) != 5 || cap(r.Rows) > 16 || !r.Truncated {
		t.Errorf("big array: len %d cap %d truncated %v", len(r.Rows), cap(r.Rows), r.Truncated)
	}
	r = db.Result{}
	render(&r, bigMap, 5)
	if len(r.Rows) != 5 || cap(r.Rows) > 16 || !r.Truncated || r.Rows[0][0] != "0" {
		t.Errorf("big map: len %d cap %d truncated %v %v", len(r.Rows), cap(r.Rows), r.Truncated, r.Rows[0])
	}
	r = db.Result{}
	render(&r, int64(7), 0)
	if !r.HasRows || r.Rows[0][0] != int64(7) {
		t.Errorf("int: %+v", r)
	}
}

func TestFilterAndSort(t *testing.T) {
	names := []string{"field", "value"}
	rows := func() [][]any {
		return [][]any{{"name", "Ann"}, {"email", "ann@x.io"}, {"age", "31"}, {"n", "5"}}
	}
	f := func(col, op, val string) db.FilterSet {
		return db.FilterSet{Filters: []db.Filter{{Column: col, Op: op, Value: val, Enabled: true}}}
	}
	for _, c := range []struct {
		fs   db.FilterSet
		want int
	}{
		{f("field", "=", "name"), 1},
		{f("field", "Not contains", "a"), 1},
		{f("value", "Has suffix", ".io"), 1},
		{f(db.AnyColumn, "Has prefix", "ann"), 1},
		{f("field", "LIKE", "n%"), 2},
		{f("field", "LIKE", "_ge"), 1},
		{f("field", "LIKE", `100\%`), 0},
		{db.FilterSet{Or: true, Filters: []db.Filter{{Column: "field", Op: "=", Value: "age", Enabled: true}, {Column: "field", Op: "=", Value: "n", Enabled: true}}}, 2},
		{db.FilterSet{Filters: []db.Filter{{Column: "field", Op: "=", Value: "age", Enabled: false}}}, 4},
	} {
		got, err := filterRows(rows(), names, c.fs)
		if err != nil || len(got) != c.want {
			t.Errorf("%+v: got %d rows, err %v; want %d", c.fs, len(got), err, c.want)
		}
	}
	for _, fs := range []db.FilterSet{f("field", "IS NULL", ""), f("nope", "=", "x"), f(db.RawColumn, "", "1=1")} {
		if _, err := filterRows(rows(), names, fs); err == nil {
			t.Errorf("%+v should fail", fs)
		}
	}
	r := [][]any{{"a", "10"}, {"b", "9"}, {"c", "100"}}
	_ = sortRows(r, names, []db.Sort{{Column: "value"}})
	if r[0][1] != "9" || r[2][1] != "100" {
		t.Errorf("numeric sort: %v", r)
	}
}

func TestPlanChanges(t *testing.T) {
	types := map[string]string{"s": "string", "h": "hash", "l": "list", "st": "set", "z": "zset", "x": "stream", "new": "none"}
	reads := map[string]string{"HGET h old": "val", "HEXISTS h new": "0", "HEXISTS h taken": "1", "ZSCORE z a": "7", "ZSCORE z taken": "1", "LINDEX l 1": "second"}
	lk := lookup{
		typ: func(k string) (string, error) { return types[k], nil },
		do: func(a ...string) (string, error) {
			if v, ok := reads[strings.Join(a, " ")]; ok {
				return v, nil
			}
			if a[0] == "ZSCORE" {
				return "", goredis.Nil
			}
			return "", errors.New("nil")
		},
	}
	kv := func(c string, v any) []db.KeyVal { return []db.KeyVal{{Col: c, Val: v}} }
	both := func(a, b []db.KeyVal) []db.KeyVal { return append(append([]db.KeyVal{}, a...), b...) }
	ref := func(n string) db.ObjRef { return db.ObjRef{Name: n, Kind: db.KindTable} }
	ok := []struct {
		c    db.Change
		want []string
	}{
		{db.Change{Kind: db.Update, ObjRef: ref("s"), Key: kv("value", "a"), Set: kv("value", "b c")}, []string{`SET s "b c" KEEPTTL`}},
		{db.Change{Kind: db.Update, ObjRef: ref("s"), Set: kv("value", db.Empty)}, []string{`SET s "" KEEPTTL`}},
		{db.Change{Kind: db.Delete, ObjRef: ref("s")}, []string{`DEL s`}},
		{db.Change{Kind: db.Update, ObjRef: ref("h"), Key: kv("field", "f"), Set: kv("value", "v")}, []string{`HSET h f v`}},
		{db.Change{Kind: db.Update, ObjRef: ref("h"), Key: kv("field", "old"), Set: kv("field", "new")}, []string{`HDEL h old`, `HSET h new val`}},
		{db.Change{Kind: db.Insert, ObjRef: ref("h"), Set: both(kv("field", "f"), kv("value", db.Empty))}, []string{`HSET h f ""`}},
		{db.Change{Kind: db.Delete, ObjRef: ref("h"), Key: kv("field", "f")}, []string{`HDEL h f`}},
		{db.Change{Kind: db.Update, ObjRef: ref("l"), Key: both(kv("index", int64(1)), kv("value", "x")), Set: kv("value", "y")}, []string{`LSET l 1 y`}},
		{db.Change{Kind: db.Insert, ObjRef: ref("l"), Set: kv("value", "y")}, []string{`RPUSH l y`}},
		// list deletes go by index through a unique marker (checked separately below): LREM by value hits duplicates
		{db.Change{Kind: db.Update, ObjRef: ref("st"), Key: kv("member", "a"), Set: kv("member", "b")}, []string{`SREM st a`, `SADD st b`}},
		{db.Change{Kind: db.Insert, ObjRef: ref("st"), Set: kv("member", "a")}, []string{`SADD st a`}},
		{db.Change{Kind: db.Update, ObjRef: ref("z"), Key: kv("member", "a"), Set: kv("score", 5.5)}, []string{`ZADD z 5.5 a`}},
		{db.Change{Kind: db.Update, ObjRef: ref("z"), Key: kv("member", "a"), Set: kv("member", "b")}, []string{`ZREM z a`, `ZADD z 7 b`}},
		{db.Change{Kind: db.Insert, ObjRef: ref("z"), Set: both(kv("member", "m"), kv("score", "3"))}, []string{`ZADD z 3 m`}},
		{db.Change{Kind: db.Delete, ObjRef: ref("z"), Key: kv("member", "a")}, []string{`ZREM z a`}},
		{db.Change{Kind: db.Insert, ObjRef: ref("new"), Set: both(kv("field", "f"), kv("value", "v"))}, []string{`HSET new f v`}},
	}
	for _, c := range ok {
		got, err := planChanges(lk, []db.Change{c.c})
		var texts []string
		for _, s := range got {
			texts = append(texts, s.Text)
			if _, err := parseCommand(s.Text); err != nil {
				t.Errorf("%q does not parse: %v", s.Text, err)
			}
		}
		if err != nil || !reflect.DeepEqual(texts, c.want) {
			t.Errorf("%+v = %q, %v; want %q", c.c, texts, err, c.want)
		}
	}
	for _, c := range []db.Change{{Kind: db.Delete, ObjRef: ref("l"), Key: kv("index", int64(1))}, {Kind: db.Delete, ObjRef: ref("l"), Key: both(kv("index", int64(1)), kv("value", "q"))}} {
		got, err := planChanges(lk, []db.Change{c})
		if err != nil || len(got) != 2 {
			t.Fatalf("list delete: %v %v", got, err)
		}
		set, rem := strings.Fields(got[0].Text), strings.Fields(got[1].Text)
		if set[0] != "LSET" || set[2] != "1" || rem[0] != "LREM" || rem[2] != "1" || set[3] != rem[3] || !strings.HasPrefix(set[3], "__tuidb_deleted_") {
			t.Errorf("list delete must overwrite the index with a marker then remove the marker: %v", got)
		}
	}
	for _, c := range []db.Change{
		{Kind: db.Update, ObjRef: ref("s"), Set: kv("value", db.Null)},
		{Kind: db.Update, ObjRef: ref("h"), Key: kv("field", "f"), Set: kv("value", nil)},
		{Kind: db.Update, ObjRef: ref("x"), Key: kv("id", "1"), Set: kv("fields", "{}")},
		{Kind: db.Update, ObjRef: ref("l"), Key: kv("index", "1"), Set: kv("index", "2")},
		{Kind: db.Update, ObjRef: ref("h"), Key: kv("field", "f")},
		{Kind: db.Update, ObjRef: ref("h"), Key: kv("field", "old"), Set: kv("field", "taken")},
		{Kind: db.Update, ObjRef: ref("z"), Key: kv("member", "a"), Set: kv("member", "taken")},
	} {
		if _, err := planChanges(lk, []db.Change{c}); err == nil {
			t.Errorf("%+v should fail", c)
		}
	}
}

func TestPlanAdmin(t *testing.T) {
	s := &session{db: 2}
	ref := db.ObjRef{Name: "a b"}
	one := func(op db.AdminOp, opts map[string]string, r db.ObjRef) string {
		st, err := s.PlanAdmin(op, r, opts)
		if err != nil {
			return "ERR " + err.Error()
		}
		return st[0].Text
	}
	for got, want := range map[string]string{
		one(db.OpDropObject, nil, ref):                                                `DEL "a b"`,
		one(db.OpTruncate, nil, ref):                                                  `DEL "a b"`,
		one(db.OpRenameObject, map[string]string{"new_name": "n"}, ref):               `RENAME "a b" n`,
		one(db.OpClone, map[string]string{"new_name": "n", "with_data": "true"}, ref): `COPY "a b" n`,
		one(db.OpDropDatabase, nil, db.ObjRef{Name: "2"}):                             `FLUSHDB`,
	} {
		if got != want {
			t.Errorf("got %s want %s", got, want)
		}
	}
	for _, got := range []string{
		one(db.OpClone, map[string]string{"new_name": "n"}, ref),
		one(db.OpDropDatabase, nil, db.ObjRef{Name: "3"}),
		one(db.OpCreateDatabase, nil, ref),
		one(db.OpRenameDatabase, nil, ref),
	} {
		if !strings.HasPrefix(got, "ERR") {
			t.Errorf("expected error, got %s", got)
		}
	}
	if _, err := s.PlanTable(nil, nil); err == nil {
		t.Error("PlanTable should fail")
	}
}

func TestParseClientList(t *testing.T) {
	ps := parseClientList("id=3 addr=127.0.0.1:5000 laddr=x name=tui age=12 idle=0 db=1 cmd=client|list user=default\nid=4 addr=a:1 age=0 db=0 cmd=get user=u\n")
	if len(ps) != 2 || ps[0].ID != "3" || ps[0].Elapsed.Seconds() != 12 || ps[0].State != "127.0.0.1:5000 tui" || ps[0].Query != "client|list" || ps[1].User != "u" {
		t.Errorf("%+v", ps)
	}
}
