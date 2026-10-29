package ui

import (
	"strings"
	"testing"
)

func TestM5AliasAwareComplete(t *testing.T) {
	if got := aliasesIn("select c.id from public.customers c join orders as o on o.cid = c.id where x"); got["c"] != "public.customers" || got["o"] != "orders" || got["where"] != "" {
		t.Fatalf("aliases %v", got)
	}
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.a.ws().tab().(*queryTab)
	q.cols["customers"] = []string{"id", "name"}
	q.ed.SetText("select c.na from customers c")
	q.ed.move(0, 11, false)
	got := q.complete("c.na")
	if len(got) == 0 || got[0].Text != "name" {
		t.Fatalf("complete %+v", got)
	}
}

// T-P1-02: accepting a completion after schema. or alias. keeps the qualifier, and alias. resolves with its schema.
func TestM18CompletionKeepsQualifier(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.a.ws().tab().(*queryTab)
	q.cols["app.customers"] = []string{"id", "city"}
	q.cols["customers"] = []string{"id", "meta"} // the sidebar schema's customers: must not be offered for app.customers c
	q.ed.SetText("select c.ci from app.customers c")
	q.ed.move(0, 12, false)
	if got := q.complete("c.ci"); len(got) == 0 || got[0].Text != "city" {
		t.Fatalf("alias of app.customers: %+v", got)
	}
	q.ed.SetText("select count(*) from public.cus")
	q.ed.move(0, 31, false)
	if !q.ed.openComplete() {
		t.Fatal("no completion for public.cus")
	}
	q.ed.acceptComplete()
	if got := strings.TrimSpace(q.ed.Text()); got != "select count(*) from public.customers" {
		t.Fatalf("schema. dropped: %q", got)
	}
	q.ed.SetText("select c. from app.customers c")
	q.ed.move(0, 9, false)
	if !q.ed.openComplete() {
		t.Fatal("no completion for c.")
	}
	q.ed.acceptComplete()
	if got := q.ed.Text(); !strings.HasPrefix(got, "select c.id") {
		t.Fatalf("alias dropped: %q", got)
	}
}

func TestM6MongoMethodSuggestions(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.dialect = "mongo"
	h.connect()
	h.prefix("e")
	got := h.queryTab().complete("db.users.fi")
	names := map[string]bool{}
	for _, c := range got {
		names[c.Text] = true
	}
	if !names["find"] || !names["findOne"] {
		t.Fatalf("suggestions %+v", got)
	}
	if len(mongoMethodCandidates("db.users")) != len(mongoMethods) || mongoMethodCandidates("users") != nil {
		t.Fatal("methods follow db.<collection>. only")
	}
}
