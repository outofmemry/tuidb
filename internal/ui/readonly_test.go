package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

// read_only is enforced by the workspace gate for every engine, not only the drivers that guard
// themselves (PostgreSQL, MySQL): a grid commit and an editor run both refuse a write with the same message.
func TestReadOnlyConnectionRefusesWritesOnEveryEngine(t *testing.T) {
	for _, c := range []struct{ dialect, write, read string }{
		{"postgres", "delete from t", "select 1"},
		{"clickhouse", "ALTER TABLE t DELETE WHERE 1", "select 1"},
		{"cql", "DELETE FROM t WHERE id = 1", "select * from t"},
		{"partiql", "DELETE FROM t WHERE id = 1", "select * from t"},
		{"redis", "SET k v", "GET k"},
		{"mongo", "db.t.deleteMany({})", "db.t.find({})"},
	} {
		t.Run(c.dialect, func(t *testing.T) {
			h := newHarness(t, func(cn *config.Connection, _ *config.Config) { cn.ReadOnly = true })
			h.fake.dialect = c.dialect
			h.connect()
			// the editor
			h.prefix("e")
			h.typeText(c.write)
			h.prefix("alt+enter")
			h.want("this connection is read-only")
			for _, s := range h.fake.execs {
				if strings.Contains(s, c.write) {
					t.Fatalf("write was sent: %q", s)
				}
			}
			h.press("ctrl+a")
			h.typeText(c.read)
			h.prefix("alt+enter")
			found := false
			for _, s := range h.fake.execs {
				found = found || strings.Contains(s, c.read)
			}
			if !found {
				t.Fatalf("read was not sent: %v", h.fake.execs)
			}
		})
	}
}

func TestReadOnlyConnectionRefusesGridCommit(t *testing.T) {
	h := newHarness(t, func(cn *config.Connection, _ *config.Config) { cn.ReadOnly = true })
	h.connect()
	h.openCustomers()
	h.press("l", "e")
	h.typeText("ie")
	h.press("enter")
	h.prefix("s")
	h.want("this connection is read-only")
	if len(h.fake.applied) != 0 {
		t.Fatalf("a read-only connection sent %+v", h.fake.applied)
	}
}

// A primary key that several rows can share (ClickHouse's sorting key) must not be what an edit
// matches on: the row is matched on every value, the preview says so, and committing asks first.
func TestSharedKeyEditMatchesOnEveryValue(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.sharedKey = true
	h.connect()
	h.openCustomers()
	h.press("l", "e")
	h.typeText("ie")
	h.press("enter")
	h.prefix("P")
	h.want(`WHERE "id" = 1 AND "name" = 'Ann' AND "email" =`)
	h.press("esc")
	h.prefix("s")
	h.want("customers has no unique key")
	if len(h.fake.applied) != 0 {
		t.Fatalf("committed without asking: %+v", h.fake.applied)
	}
	h.press("y")
	if len(h.fake.applied) != 1 {
		t.Fatalf("applied: %+v", h.fake.applied)
	}
}

// MySQL cannot rename a routine: running the edit tab creates the new body under a temporary name,
// drops the old routine, creates the same body under the original name and drops the temporary one.
func TestFinishRoutineSwapRestoresTheName(t *testing.T) {
	ref := db.ObjRef{Name: "f", Kind: db.KindFunction}
	text := editableDDL("mysql", ref, "CREATE DEFINER=`u`@`%` FUNCTION `f`() RETURNS int BEGIN DECLARE x int; RETURN 1; END")
	got := finishRoutineSwap("mysql", db.Split("mysql", text))
	if len(got) != 4 {
		t.Fatalf("want create tmp, drop f, create f, drop tmp: %+v", got)
	}
	for i, want := range []string{"CREATE FUNCTION `f__webtuidb_new`() RETURNS int BEGIN DECLARE x int; RETURN 1; END", "DROP FUNCTION IF EXISTS `f`", "CREATE FUNCTION `f`() RETURNS int BEGIN DECLARE x int; RETURN 1; END", "DROP FUNCTION IF EXISTS `f__webtuidb_new`"} {
		if !strings.HasSuffix(strings.TrimSpace(got[i].Text), want) || (i > 0 && strings.Contains(got[i].Text, "--")) {
			t.Errorf("statement %d = %q, want ...%q", i, got[i].Text, want)
		}
	}
	if again := finishRoutineSwap("mysql", got); len(again) != 4 {
		t.Errorf("running an expanded script must not expand it twice: %d statements", len(again))
	}
	if pg := finishRoutineSwap("postgres", db.Split("mysql", text)); len(pg) != 2 {
		t.Errorf("only MySQL routines are swapped: %d", len(pg))
	}
	if other := db.Split("mysql", "SELECT 1; DROP FUNCTION IF EXISTS `g`"); len(finishRoutineSwap("mysql", other)) != 2 {
		t.Error("unrelated scripts are left alone")
	}
}
