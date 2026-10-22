package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"tuidb/internal/config"
	"tuidb/internal/db"
)

func open(t *testing.T) *session {
	t.Helper()
	if os.Getenv("TUIDB_INTEGRATION") != "1" {
		t.Skip("set TUIDB_INTEGRATION=1")
	}
	url := os.Getenv("TUIDB_TEST_URL_REDIS")
	if url == "" {
		url = "redis://:tuidb@127.0.0.1:56379/0"
	}
	conn, err := config.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	if pw, ok := os.LookupEnv("TUIDB_TEST_PASSWORD_REDIS"); ok {
		conn.Password = pw
	}
	sess, err := driver{}.Open(context.Background(), conn, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	return sess.(*session)
}

// tmpKey returns a unique key that is deleted when the test ends.
func tmpKey(t *testing.T, s *session, name string) string {
	t.Helper()
	b := make([]byte, 4)
	rand.Read(b)
	k := "tuidb_test:" + hex.EncodeToString(b) + ":" + name
	t.Cleanup(func() { s.c().Del(context.Background(), k, k+"2") })
	return k
}

func run(t *testing.T, s *session, cmd string) *db.Result {
	t.Helper()
	r, err := s.Exec(context.Background(), cmd, 0)
	if err != nil {
		t.Fatalf("%s: %v", cmd, err)
	}
	return r
}

func ref(k string) db.ObjRef { return db.ObjRef{Name: k, Kind: db.KindTable} }

func rowsOf(t *testing.T, s *session, q db.RowQuery) [][]any {
	t.Helper()
	r, err := s.Rows(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	return r.Rows
}

func TestOpenMetadata(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil || !strings.HasPrefix(s.ServerInfo(), "Redis 7") {
		t.Errorf("ping %v info %q", err, s.ServerInfo())
	}
	dbs, err := s.Databases(ctx)
	if err != nil || len(dbs) != 16 || dbs[0] != "0" || s.CurrentDatabase() != "0" {
		t.Errorf("databases %v %v current %s", dbs, err, s.CurrentDatabase())
	}
	if sc, _ := s.Schemas(ctx); sc != nil {
		t.Error("schemas should be nil")
	}
	objs, err := s.Objects(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, o := range objs {
		if o.Kind != db.KindTable {
			t.Errorf("kind %v", o.Kind)
		}
		got[o.Name] = o.Detail
	}
	want := map[string]string{"user:1": "hash", "queue:jobs": "list", "tags:post:1": "set", "leaderboard": "zset", "events:stream": "stream", "counter": "string", "json:doc": "string"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("object %s detail %q want %q", k, got[k], v)
		}
	}
	if d := got["session:abc"]; !strings.HasPrefix(d, "string ttl ") || !strings.HasSuffix(d, "s") {
		t.Errorf("session detail %q", d)
	}
	for i := 1; i < len(objs); i++ {
		if objs[i-1].Name > objs[i].Name {
			t.Fatal("objects not sorted")
		}
	}
	for key, cols := range map[string]string{"counter": "value", "user:1": "field,value", "queue:jobs": "index,value", "tags:post:1": "member", "leaderboard": "member,score", "events:stream": "id,fields"} {
		cs, err := s.Columns(ctx, ref(key))
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, c := range cs {
			names = append(names, c.Name)
		}
		if strings.Join(names, ",") != cols || !cs[0].PK || (len(cs) > 1 && cs[1].PK) {
			t.Errorf("%s columns %v", key, cs)
		}
	}
	if _, err := s.Columns(ctx, ref("tuidb_test:missing")); err == nil {
		t.Error("missing key should fail")
	}
	if ix, _ := s.Indexes(ctx, ref("user:1")); ix != nil {
		t.Error("indexes should be nil")
	}
	ddl, err := s.DDL(ctx, ref("user:1"))
	if err != nil || !strings.HasPrefix(ddl, "TYPE hash\nTTL -1\nENCODING ") || !strings.Contains(ddl, "\nMEMORY ") || !strings.HasSuffix(ddl, " bytes") {
		t.Errorf("ddl %q %v", ddl, err)
	}
	ddl, _ = s.DDL(ctx, ref("session:abc"))
	if !strings.Contains(ddl, "\nTTL ") || strings.Contains(ddl, "TTL -1") { // seeded with 3600s, decays
		t.Errorf("ttl ddl %q", ddl)
	}
}

func TestUseDatabase(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if err := s.UseDatabase(ctx, "1"); err != nil || s.CurrentDatabase() != "1" {
		t.Fatal(err)
	}
	objs, err := s.Objects(ctx, "")
	if err != nil || len(objs) != 1 || objs[0].Name != "other:db:key" {
		t.Errorf("db1 objects %v %v", objs, err)
	}
	if r := run(t, s, "GET other:db:key"); r.Rows[0][0] != "in db 1" {
		t.Errorf("exec uses db 1: %v", r.Rows)
	}
	if err := s.UseDatabase(ctx, "0"); err != nil {
		t.Fatal(err)
	}
	if err := s.UseDatabase(ctx, "x"); err == nil {
		t.Error("bad index accepted")
	}
}

func TestRowsAndCount(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	r, err := s.Rows(ctx, db.RowQuery{ObjRef: ref("user:1")})
	if err != nil || len(r.Rows) != 3 || len(r.Docs) != 3 || !strings.Contains(r.Docs[0], `"field"`) {
		t.Fatalf("hash rows %v docs %v err %v", r.Rows, r.Docs, err)
	}
	asc := rowsOf(t, s, db.RowQuery{ObjRef: ref("user:1"), Sort: []db.Sort{{Column: "field"}}})
	if !reflect.DeepEqual(asc, [][]any{{"age", "31"}, {"email", "ann@example.com"}, {"name", "Ann Lee"}}) {
		t.Errorf("sorted hash %v", asc)
	}
	pg := rowsOf(t, s, db.RowQuery{ObjRef: ref("user:1"), Sort: []db.Sort{{Column: "field", Desc: true}}, Limit: 1, Offset: 1})
	if !reflect.DeepEqual(pg, [][]any{{"email", "ann@example.com"}}) {
		t.Errorf("paged hash %v", pg)
	}
	f := db.FilterSet{Filters: []db.Filter{{Column: "value", Op: "Contains", Value: "ann", Enabled: true}}}
	if got := rowsOf(t, s, db.RowQuery{ObjRef: ref("user:1"), Filters: f}); len(got) != 1 || got[0][0] != "email" {
		t.Errorf("filtered hash %v", got)
	}
	if n, err := s.Count(ctx, db.RowQuery{ObjRef: ref("user:1"), Filters: f}, true); err != nil || n != (db.Count{N: 1, Exact: true}) {
		t.Errorf("filtered count %v %v", n, err)
	}
	if got := rowsOf(t, s, db.RowQuery{ObjRef: ref("queue:jobs"), Limit: 2, Offset: 1}); !reflect.DeepEqual(got, [][]any{{int64(1), "j2"}, {int64(2), "j3"}}) {
		t.Errorf("list page %v", got)
	}
	if got := rowsOf(t, s, db.RowQuery{ObjRef: ref("leaderboard"), Sort: []db.Sort{{Column: "score", Desc: true}}, Limit: 2}); !reflect.DeepEqual(got, [][]any{{"bob", 250.0}, {"ann", 100.0}}) {
		t.Errorf("zset desc %v", got)
	}
	if got := rowsOf(t, s, db.RowQuery{ObjRef: ref("leaderboard")}); !reflect.DeepEqual(got, [][]any{{"cy", 75.0}, {"ann", 100.0}, {"bob", 250.0}}) {
		t.Errorf("zset natural %v", got)
	}
	if got := rowsOf(t, s, db.RowQuery{ObjRef: ref("tags:post:1"), Sort: []db.Sort{{Column: "member"}}}); !reflect.DeepEqual(got, [][]any{{"blue"}, {"green"}, {"red"}}) {
		t.Errorf("set %v", got)
	}
	if got := rowsOf(t, s, db.RowQuery{ObjRef: ref("counter")}); !reflect.DeepEqual(got, [][]any{{"42"}}) {
		t.Errorf("string %v", got)
	}
	st := rowsOf(t, s, db.RowQuery{ObjRef: ref("events:stream")})
	if len(st) != 1 || !strings.Contains(st[0][1].(string), `"kind":"click"`) {
		t.Errorf("stream %v", st)
	}
	if r, _ := s.Rows(ctx, db.RowQuery{ObjRef: ref("events:stream")}); !strings.Contains(r.Docs[0], `"fields":{`) {
		t.Errorf("stream doc %v", r.Docs)
	}
	for key, want := range map[string]int64{"user:1": 3, "queue:jobs": 3, "tags:post:1": 3, "leaderboard": 3, "events:stream": 1, "counter": 1} {
		for _, est := range []bool{true, false} {
			if n, err := s.Count(ctx, db.RowQuery{ObjRef: ref(key)}, est); err != nil || n != (db.Count{N: want, Exact: true}) {
				t.Errorf("count %s = %v %v", key, n, err)
			}
		}
	}
	bad := db.FilterSet{Filters: []db.Filter{{Column: "value", Op: "BETWEEN", Value: "1 AND 2", Enabled: true}}}
	if _, err := s.Rows(ctx, db.RowQuery{ObjRef: ref("user:1"), Filters: bad}); err == nil {
		t.Error("unsupported operator accepted")
	}
}

func TestScripts(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	for _, c := range []struct {
		kind db.ScriptKind
		key  string
		want string
	}{
		{db.ScriptDrop, "user:1", "DEL user:1"},
		{db.ScriptTruncate, "user:1", "DEL user:1"},
		{db.ScriptSelectTop, "queue:jobs", "LRANGE queue:jobs 0 99"},
		{db.ScriptSelectTop, "counter", "GET counter"},
		{db.ScriptSelectTop, "leaderboard", "ZRANGE leaderboard 0 99 WITHSCORES"},
		{db.ScriptCreate, "queue:jobs", "RPUSH queue:jobs j1 j2 j3"},
		{db.ScriptCreate, "counter", "SET counter 42"},
		{db.ScriptCreate, "leaderboard", "ZADD leaderboard 75 cy 100 ann 250 bob"},
	} {
		if got, err := s.Script(ctx, c.kind, ref(c.key)); err != nil || got != c.want {
			t.Errorf("%s %s = %q, %v; want %q", c.kind, c.key, got, err, c.want)
		}
	}
	got, err := s.Script(ctx, db.ScriptCreate, ref("session:abc"))
	if err != nil || !strings.HasPrefix(got, "SET session:abc token\nEXPIRE session:abc ") {
		t.Errorf("session script %q %v", got, err)
	}
	got, _ = s.Script(ctx, db.ScriptCreate, ref("json:doc"))
	if cmd, _ := parseCommand(got); len(cmd) != 3 || cmd[2] != `{"a":1,"b":[1,2]}` {
		t.Errorf("json script %q", got)
	}
	got, _ = s.Script(ctx, db.ScriptCreate, ref("events:stream"))
	if !strings.HasPrefix(got, "XADD events:stream ") || !strings.HasSuffix(got, " kind click user 1") {
		t.Errorf("stream script %q", got)
	}
}

func TestExec(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	r := run(t, s, `GET user:1:name`)
	if !r.HasRows || r.Rows[0][0] != "Ann Lee" {
		t.Errorf("get %+v", r)
	}
	r, err := s.Exec(ctx, `LRANGE queue:jobs 0 -1`, 2)
	if err != nil || len(r.Rows) != 2 || !r.Truncated || r.Rows[1][0] != int64(1) || r.Rows[1][1] != "j2" {
		t.Errorf("lrange %+v %v", r, err)
	}
	if r := run(t, s, `GET tuidb_test:nothing`); r.HasRows || r.Message != "(nil)" {
		t.Errorf("nil %+v", r)
	}
	if r := run(t, s, `HGETALL user:1`); len(r.Rows) != 3 || r.Columns[0].Name != "key" {
		t.Errorf("hgetall %+v", r)
	}
	if r := run(t, s, `ZRANGE leaderboard 0 0 WITHSCORES`); len(r.Rows) == 0 {
		t.Errorf("zrange %+v", r)
	}
	if r := run(t, s, `PING`); !r.HasRows || r.Rows[0][0] != "PONG" {
		t.Errorf("ping %+v", r)
	}
	k := tmpKey(t, s, "exec")
	if r := run(t, s, `SET `+k+` "a b\nc"`); r.HasRows || r.Message != "OK" {
		t.Errorf("set %+v", r)
	}
	if r := run(t, s, `GET `+k); r.Rows[0][0] != "a b\nc" {
		t.Errorf("get after set %+v", r)
	}
	if _, err := s.Exec(ctx, `NOSUCHCMD x`, 0); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("error = %v", err)
	}
	if _, err := s.Exec(ctx, `SET "x`, 0); err == nil {
		t.Error("parse error expected")
	}
	if _, err := s.Exec(ctx, `  `, 0); err == nil {
		t.Error("empty expected error")
	}
	if r := run(t, s, `CLIENT LIST`); !r.HasRows {
		t.Errorf("client list %+v", r)
	}
}

func TestExecCancel(t *testing.T) {
	s := open(t)
	k := tmpKey(t, s, "block")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	start := time.Now()
	_, err := s.Exec(ctx, `BLPOP `+k+` 30`, 0)
	if err == nil || time.Since(start) > 1500*time.Millisecond {
		t.Errorf("cancel took %v, err %v", time.Since(start), err)
	}
	// the session still works afterwards
	if err := s.Ping(context.Background()); err != nil {
		t.Error(err)
	}
}

func apply(t *testing.T, s *session, changes ...db.Change) []string {
	t.Helper()
	stmts, err := s.Plan(changes)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(context.Background(), stmts); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, st := range stmts {
		out = append(out, st.Text)
	}
	return out
}

func kv(c string, v any) []db.KeyVal { return []db.KeyVal{{Col: c, Val: v}} }

func TestPlanApply(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	str := tmpKey(t, s, "str")
	run(t, s, "SET "+str+" old EX 500")
	apply(t, s, db.Change{Kind: db.Update, ObjRef: ref(str), Key: kv("value", "old"), Set: kv("value", "new val")})
	if v := s.c().Get(ctx, str).Val(); v != "new val" {
		t.Errorf("string = %q", v)
	}
	if ttl := s.c().TTL(ctx, str).Val(); ttl <= 0 {
		t.Errorf("KEEPTTL lost ttl: %v", ttl)
	}
	if _, err := s.Plan([]db.Change{{Kind: db.Update, ObjRef: ref(str), Set: kv("value", db.Null)}}); err == nil {
		t.Error("NULL accepted")
	}

	h := tmpKey(t, s, "hash")
	run(t, s, "HSET "+h+" a 1 b 2")
	apply(t, s,
		db.Change{Kind: db.Update, ObjRef: ref(h), Key: kv("field", "a"), Set: kv("value", "one")},
		db.Change{Kind: db.Update, ObjRef: ref(h), Key: kv("field", "b"), Set: kv("field", "bb")},
		db.Change{Kind: db.Insert, ObjRef: ref(h), Set: append(kv("field", "c"), db.KeyVal{Col: "value", Val: db.Empty})},
	)
	if got := s.c().HGetAll(ctx, h).Val(); !reflect.DeepEqual(got, map[string]string{"a": "one", "bb": "2", "c": ""}) {
		t.Errorf("hash = %v", got)
	}
	apply(t, s, db.Change{Kind: db.Delete, ObjRef: ref(h), Key: kv("field", "a")})
	if n := s.c().HLen(ctx, h).Val(); n != 2 {
		t.Errorf("hlen = %d", n)
	}

	l := tmpKey(t, s, "list")
	run(t, s, "RPUSH "+l+" x y z")
	apply(t, s,
		db.Change{Kind: db.Update, ObjRef: ref(l), Key: kv("index", int64(0)), Set: kv("value", "X")},
		db.Change{Kind: db.Insert, ObjRef: ref(l), Set: kv("value", "w")},
		db.Change{Kind: db.Delete, ObjRef: ref(l), Key: kv("index", int64(1))},
	)
	if got := s.c().LRange(ctx, l, 0, -1).Val(); !reflect.DeepEqual(got, []string{"X", "z", "w"}) {
		t.Errorf("list = %v", got)
	}

	st := tmpKey(t, s, "set")
	run(t, s, "SADD "+st+" a b")
	apply(t, s,
		db.Change{Kind: db.Update, ObjRef: ref(st), Key: kv("member", "a"), Set: kv("member", "c")},
		db.Change{Kind: db.Delete, ObjRef: ref(st), Key: kv("member", "b")},
	)
	if got := s.c().SMembers(ctx, st).Val(); !reflect.DeepEqual(got, []string{"c"}) {
		t.Errorf("set = %v", got)
	}

	z := tmpKey(t, s, "zset")
	run(t, s, "ZADD "+z+" 1 a 2 b")
	apply(t, s,
		db.Change{Kind: db.Update, ObjRef: ref(z), Key: kv("member", "a"), Set: kv("member", "aa")},
		db.Change{Kind: db.Update, ObjRef: ref(z), Key: kv("member", "b"), Set: kv("score", "9.5")},
		db.Change{Kind: db.Insert, ObjRef: ref(z), Set: append(kv("member", "n"), db.KeyVal{Col: "score", Val: "3"})},
	)
	if got := s.c().ZRangeWithScores(ctx, z, 0, -1).Val(); len(got) != 3 || got[0].Member != "aa" || got[0].Score != 1 || got[2].Member != "b" || got[2].Score != 9.5 {
		t.Errorf("zset = %v", got)
	}

	// a failing statement must not leave partial changes visible beyond the transaction contract
	if err := s.Apply(ctx, []db.Statement{{Text: "NOSUCH x"}}); err == nil {
		t.Error("bad statement accepted")
	}

	if _, err := s.Plan([]db.Change{{Kind: db.Delete, ObjRef: ref("events:stream"), Key: kv("id", "1-0")}}); err == nil {
		t.Error("stream edit accepted")
	}
}

func TestPlanAdminApply(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	k := tmpKey(t, s, "admin")
	run(t, s, "SET "+k+" v")
	exec := func(op db.AdminOp, r string, opts map[string]string) {
		t.Helper()
		stmts, err := s.PlanAdmin(op, ref(r), opts)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Apply(ctx, stmts); err != nil {
			t.Fatal(err)
		}
	}
	exec(db.OpClone, k, map[string]string{"new_name": k + "2", "with_data": "true"})
	if s.c().Get(ctx, k+"2").Val() != "v" {
		t.Error("clone failed")
	}
	exec(db.OpRenameObject, k+"2", map[string]string{"new_name": k + "3"})
	t.Cleanup(func() { s.c().Del(ctx, k+"3") })
	if s.c().Exists(ctx, k+"3").Val() != 1 || s.c().Exists(ctx, k+"2").Val() != 0 {
		t.Error("rename failed")
	}
	exec(db.OpTruncate, k+"3", nil)
	exec(db.OpDropObject, k, nil)
	if s.c().Exists(ctx, k, k+"3").Val() != 0 {
		t.Error("drop failed")
	}
}

func TestProcesses(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	ps, err := s.Processes(ctx)
	if err != nil || len(ps) == 0 || ps[0].ID == "" {
		t.Fatalf("processes %v %v", ps, err)
	}
	// kill a throwaway connection, never ours
	other := open(t)
	id, err := other.c().ClientID(ctx).Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Kill(ctx, strconv.FormatInt(id, 10)); err != nil {
		t.Error(err)
	}
	if err := s.Kill(ctx, "999999999"); err == nil {
		t.Error("killing unknown id should fail")
	}
}

// Found in review: LREM removed the first duplicate instead of the chosen index.
func TestListDeleteByIndexWithDuplicates(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	l := tmpKey(t, s, "dups")
	run(t, s, "RPUSH "+l+" a b a")
	apply(t, s, db.Change{Kind: db.Delete, ObjRef: ref(l), Key: kv("index", int64(2))})
	if got := s.c().LRange(ctx, l, 0, -1).Val(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("deleting index 2 must leave [a b], got %v", got)
	}
}

// Found in review: RENAME overwrote an existing key without a word.
func TestRenameRefusesExistingKey(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	a, b := tmpKey(t, s, "ra"), tmpKey(t, s, "rb")
	run(t, s, "SET "+a+" keep")
	run(t, s, "SET "+b+" other")
	if _, err := s.PlanAdmin(db.OpRenameObject, ref(b), map[string]string{"new_name": a}); err == nil {
		t.Fatal("renaming onto an existing key must be refused")
	}
	if v := s.c().Get(ctx, a).Val(); v != "keep" {
		t.Errorf("existing key changed: %q", v)
	}
	if _, err := s.PlanAdmin(db.OpRenameObject, ref(b), map[string]string{"new_name": a + "2"}); err != nil {
		t.Errorf("renaming to a free name is fine: %v", err)
	}
}

// Found in review: SELECT n was a no-op because every Exec uses a fresh connection.
func TestExecSelectSwitchesDatabase(t *testing.T) {
	s := open(t)
	defer s.UseDatabase(context.Background(), "0")
	run(t, s, "SELECT 1")
	if s.CurrentDatabase() != "1" {
		t.Fatalf("current database %q", s.CurrentDatabase())
	}
	k := tmpKey(t, s, "dbsel")
	run(t, s, "SET "+k+" v")
	if n := s.c().Exists(context.Background(), k).Val(); n != 1 {
		t.Error("the write must land in the selected database")
	}
	s.UseDatabase(context.Background(), "0")
	if n := s.c().Exists(context.Background(), k).Val(); n != 0 {
		t.Error("and not in db 0")
	}
	s.UseDatabase(context.Background(), "1")
	s.c().Del(context.Background(), k)
}

// Filtered loads report the maxLoad cap (P1-58, P1-59) and hash/set paging is bounded (P1-57).
func TestBoundedLoads(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	old := maxLoad
	maxLoad = 10
	t.Cleanup(func() { maxLoad = old })
	big := tmpKey(t, s, "hash")
	small := big + "2"
	for i := 0; i < 15; i++ {
		s.c().HSet(ctx, big, "f"+strconv.Itoa(i), "v")
		if i < 10 {
			s.c().HSet(ctx, small, "f"+strconv.Itoa(i), "v")
		}
	}
	f := db.FilterSet{Filters: []db.Filter{{Column: "field", Op: "Has prefix", Value: "f", Enabled: true}}}
	for key, capped := range map[string]bool{big: true, small: false} {
		r, err := s.Rows(ctx, db.RowQuery{ObjRef: ref(key), Filters: f})
		if err != nil || len(r.Rows) != 10 || r.Truncated != capped {
			t.Errorf("%s filtered rows %d truncated %v err %v", key, len(r.Rows), r.Truncated, err)
		}
		if n, err := s.Count(ctx, db.RowQuery{ObjRef: ref(key), Filters: f}, false); err != nil || n != (db.Count{N: 10, Exact: !capped}) {
			t.Errorf("%s filtered count %v %v", key, n, err)
		}
	}
	seen := map[any]bool{}
	for off := 0; off <= 10; off += 5 {
		for _, r := range rowsOf(t, s, db.RowQuery{ObjRef: ref(big), Offset: off, Limit: 5}) {
			seen[r[0]] = true
		}
	}
	if len(seen) != 15 {
		t.Errorf("pages covered %d fields", len(seen))
	}
	set := tmpKey(t, s, "set")
	s.c().SAdd(ctx, set, "a", "b", "c")
	for _, key := range []string{big, set} {
		if _, err := s.Rows(ctx, db.RowQuery{ObjRef: ref(key), Offset: 11, Limit: 5}); err == nil || !strings.Contains(err.Error(), "too deep") {
			t.Errorf("%s deep offset: %v", key, err)
		}
	}
}

// Stream pages continue from the last-seen ID (P1-62).
func TestStreamPaging(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	key := tmpKey(t, s, "stream")
	var ids []string
	for i := 0; i < 25; i++ {
		ids = append(ids, s.c().XAdd(ctx, &goredis.XAddArgs{Stream: key, Values: []string{"n", strconv.Itoa(i)}}).Val())
	}
	page := func(off, n int) []string {
		var out []string
		for _, r := range rowsOf(t, s, db.RowQuery{ObjRef: ref(key), Offset: off, Limit: n}) {
			out = append(out, r[0].(string))
		}
		return out
	}
	for off := 0; off < 25; off += 10 {
		if got := page(off, 10); !reflect.DeepEqual(got, ids[off:min(off+10, 25)]) {
			t.Errorf("page %d = %v", off, got)
		}
		if off == 10 && s.marks[key][1] != (streamMark{20, "(" + ids[19]}) {
			t.Errorf("marks %v", s.marks[key])
		}
	}
	if got := page(7, 3); !reflect.DeepEqual(got, ids[7:10]) { // backwards jump skips from the head
		t.Errorf("jump = %v", got)
	}
	if got := page(23, 10); !reflect.DeepEqual(got, ids[23:]) {
		t.Errorf("tail = %v", got)
	}
	if got := page(30, 10); got != nil {
		t.Errorf("past end = %v", got)
	}
}

// Apply refuses keys whose type changed since planning and reports partial runtime failures (P1-63).
func TestApplyPreconditions(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	h, other := tmpKey(t, s, "h"), tmpKey(t, s, "o")
	s.c().HSet(ctx, h, "a", "1")
	stmts, err := s.Plan([]db.Change{
		{Kind: db.Insert, ObjRef: ref(other), Set: kv("value", "x")},
		{Kind: db.Insert, ObjRef: ref(h), Set: []db.KeyVal{{Col: "field", Val: "b"}, {Col: "value", Val: "2"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.c().Del(ctx, h)
	s.c().Set(ctx, h, "now a string", 0)
	if err := s.Apply(ctx, stmts); err == nil || !strings.Contains(err.Error(), "nothing was applied") {
		t.Errorf("changed type: %v", err)
	}
	if n := s.c().Exists(ctx, other).Val(); n != 0 {
		t.Error("statement applied despite failed precondition")
	}
	a, b, c := tmpKey(t, s, "a"), tmpKey(t, s, "b"), tmpKey(t, s, "c")
	err = s.Apply(ctx, []db.Statement{{Text: "SET " + a + " 1"}, {Text: "LSET " + b + " 0 x"}, {Text: "SET " + c + " 3"}})
	if err == nil || !strings.Contains(err.Error(), "statement 2 ") || !strings.Contains(err.Error(), "statements 1, 3 were applied") {
		t.Errorf("partial failure: %v", err)
	}
	if s.c().Get(ctx, a).Val() != "1" || s.c().Get(ctx, c).Val() != "3" {
		t.Error("applied statements missing")
	}
}
