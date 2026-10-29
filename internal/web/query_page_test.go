package web

import (
	"strings"
	"testing"

	"webtuidb/internal/db"
)

func TestExecPage(t *testing.T) {
	e, rf := richEnv(t, nil)
	id := e.open()
	r := e.ok("POST", "/api/sessions/"+id+"/exec/page", map[string]any{"text": "select 1", "limit": 1, "offset": 5})
	if r["supported"] != true || r["more"] != true || !strings.Contains(rf.execs[len(rf.execs)-1], "OFFSET 5") {
		t.Fatalf("page = %v execs=%v", r, rf.execs)
	}
	if r := e.ok("POST", "/api/sessions/"+id+"/exec/page", map[string]any{"text": "delete from t", "limit": 1}); r["supported"] != false {
		t.Fatalf("a write must fall back: %v", r)
	}
}

// CQL and PartiQL have no derived tables: the page is not sent as a subquery (Cassandra answered
// "no viable alternative at input '('"); the engine's capability turns it off with a reason.
func TestExecPageNotAvailableWithoutSubqueries(t *testing.T) {
	e, rf := richEnv(t, nil)
	e.fake.capsFn = func() db.Capabilities {
		return db.Capabilities{Dialect: "cql", Editor: "sql", NoSubquery: true}
	}
	id := e.open()
	n := len(rf.execs)
	r := e.ok("POST", "/api/sessions/"+id+"/exec/page", map[string]any{"text": "select * from t", "limit": 1, "offset": 5})
	if r["supported"] != false || r["reason"] != "load more is not available for this engine" {
		t.Fatalf("page = %v", r)
	}
	if len(rf.execs) != n {
		t.Fatalf("a subquery was sent: %v", rf.execs[n:])
	}
}
