package cassandra

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func testConn(t *testing.T) config.Connection {
	t.Helper()
	if os.Getenv("WEBTUIDB_INTEGRATION") != "1" {
		t.Skip("set WEBTUIDB_INTEGRATION=1 to run against the docker databases")
	}
	url := os.Getenv("WEBTUIDB_TEST_URL_CASSANDRA")
	if url == "" {
		url = "cassandra://webtuidb:webtuidb@127.0.0.1:59042/webtuidb"
	}
	c, err := config.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var seedOnce sync.Once

// seed applies testdata/seed/cassandra.cql, statement by statement through db.Split, before the first test.
func seed(t *testing.T) {
	t.Helper()
	c := testConn(t)
	seedOnce.Do(func() {
		text, err := os.ReadFile("../../../testdata/seed/cassandra.cql")
		if err != nil {
			t.Fatal(err)
		}
		c.Database = ""
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		s, err := drv{}.Open(ctx, c, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		for _, st := range db.Split("cql", string(text)) {
			if _, err := s.Exec(ctx, st.Text, 0); err != nil {
				t.Fatalf("seed %q: %v", st.Text, err)
			}
		}
	})
}

func open(t *testing.T, bootstrap ...string) *session {
	t.Helper()
	seed(t)
	c := testConn(t)
	c.Bootstrap = bootstrap
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := drv{}.Open(ctx, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s.(*session)
}

// tempTable returns a unique table name that is dropped when the test ends.
func tempTable(t *testing.T, s *session) string {
	t.Helper()
	name := fmt.Sprintf("webtuidb_test_%x", rand.Uint32())
	t.Cleanup(func() { s.query("DROP TABLE IF EXISTS " + q(name)).Exec() })
	return name
}

func exec(t *testing.T, s *session, stmt string) *db.Result {
	t.Helper()
	r, err := s.Exec(context.Background(), stmt, 0)
	if err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
	return r
}

func ref(name string) db.ObjRef { return db.ObjRef{Name: name, Kind: db.KindTable} }

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

// cells formats every row as "a|b|c" for compact assertions.
func cells(r *db.Result) []string {
	var out []string
	for _, row := range r.Rows {
		var c []string
		for _, v := range row {
			c = append(c, db.FormatValue(v))
		}
		out = append(out, strings.Join(c, "|"))
	}
	return out
}

func eq(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got %q\nwant %q", what, got, want)
	}
}

func apply(t *testing.T, s *session, changes ...db.Change) {
	t.Helper()
	st, err := s.Plan(changes)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(context.Background(), st); err != nil {
		t.Fatalf("apply %v: %v", st, err)
	}
}

func kv(col string, v any) db.KeyVal { return db.KeyVal{Col: col, Val: v} }

func TestIntegrationOpen(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.ServerInfo(), "Cassandra 5") {
		t.Errorf("server info %q", s.ServerInfo())
	}
	dbs, err := s.Databases(ctx)
	if err != nil || !contains(dbs, "webtuidb") {
		t.Fatalf("databases %v %v", dbs, err)
	}
	if !strings.HasPrefix(dbs[len(dbs)-1], "system") || strings.HasPrefix(dbs[0], "system") {
		t.Errorf("system keyspaces must come last: %v", dbs)
	}
	if s.CurrentDatabase() != "webtuidb" {
		t.Errorf("current db %q", s.CurrentDatabase())
	}
	if sc, _ := s.Schemas(ctx); sc != nil || s.Caps().Schemas {
		t.Error("cassandra has no schemas")
	}
}

func TestIntegrationBadPassword(t *testing.T) {
	seed(t)
	c := testConn(t)
	c.Password = "wrong"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := (drv{}).Open(ctx, c, nil); err == nil {
		t.Fatal("expected auth error")
	}
}

func TestIntegrationUnknownOption(t *testing.T) {
	seed(t)
	c := testConn(t)
	c.Options = map[string]string{"consistancy": "one"}
	if _, err := (drv{}).Open(context.Background(), c, nil); err == nil || !strings.Contains(err.Error(), "unknown cassandra option") {
		t.Fatalf("typo in an option must fail: %v", err)
	}
}

func TestIntegrationOptionsAndBootstrap(t *testing.T) {
	seed(t)
	c := testConn(t)
	c.Database = ""
	c.Options = map[string]string{"keyspace": "webtuidb", "consistency": "one", "timeout": "20s", "protocol_version": "4"}
	c.Bootstrap = []string{"CONSISTENCY LOCAL_ONE", "SELECT * FROM system.local"}
	s, err := (drv{}).Open(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.CurrentDatabase() != "webtuidb" || s.(*session).consistency().String() != "LOCAL_ONE" {
		t.Errorf("keyspace %q consistency %v", s.CurrentDatabase(), s.(*session).consistency())
	}
	c.Bootstrap = []string{"USE system_schema"}
	s2, err := (drv{}).Open(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if s2.CurrentDatabase() != "system_schema" {
		t.Errorf("bootstrap USE: %q", s2.CurrentDatabase())
	}
	c.Bootstrap = []string{"SELECT nonsense FROM nowhere"}
	if _, err := (drv{}).Open(context.Background(), c, nil); err == nil {
		t.Error("a failing bootstrap statement must fail the connection")
	}
}

func TestIntegrationUseAndConsistency(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	r := exec(t, s, "USE system_schema;")
	if r.Message == "" || s.CurrentDatabase() != "system_schema" {
		t.Fatalf("USE: %+v %q", r, s.CurrentDatabase())
	}
	if res := exec(t, s, "select count(*) from keyspaces"); res.Rows[0][0].(int64) < 5 { // unqualified: resolved against the new keyspace
		t.Errorf("rows %v", res.Rows)
	}
	exec(t, s, `USE "webtuidb"`)
	exec(t, s, "USE WEBTUIDB") // bare names fold to lower case
	if s.CurrentDatabase() != "webtuidb" {
		t.Errorf("current %q", s.CurrentDatabase())
	}
	if err := s.UseDatabase(ctx, "no_such_keyspace"); err == nil {
		t.Error("USE of a missing keyspace must fail")
	}
	if s.CurrentDatabase() != "webtuidb" {
		t.Error("a failed USE must keep the old keyspace")
	}
	exec(t, s, "consistency one")
	if s.consistency().String() != "ONE" {
		t.Errorf("consistency %v", s.consistency())
	}
	if _, err := s.Exec(ctx, "CONSISTENCY bogus", 0); err == nil {
		t.Error("bad consistency level")
	}
}
