package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/config"
)

func TestM6GroupRenameAndDelete(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.Group = "work" })
	h.a.d.Conns = append(h.a.d.Conns, config.Connection{Name: "b", Driver: config.Postgres, Host: "h", Group: "work/sub"}, config.Connection{Name: "c", Driver: config.Postgres, Host: "h", Group: "other"})
	h.press("g")
	h.typeText("work")
	h.press("enter")
	h.pickLabel("Rename")
	setField(t, h, "name", "jobs")
	h.press("enter")
	if g := h.a.d.Conns[1].Group; g != "jobs/sub" || h.a.d.Conns[0].Group != "jobs" {
		t.Fatalf("groups %q %q", h.a.d.Conns[0].Group, g)
	}
	h.press("g")
	h.typeText("jobs")
	h.press("enter")
	h.pickLabel("Delete")
	setField(t, h, "confirm", "wrong")
	h.press("enter")
	h.want("type “jobs”")
	setField(t, h, "confirm", "jobs")
	h.press("enter")
	if len(h.a.d.Conns) != 1 || h.a.d.Conns[0].Name != "c" {
		t.Fatalf("left %+v", h.a.d.Conns)
	}
	if saved, _ := config.LoadConnections(h.a.d.ConnPath); len(saved) != 1 {
		t.Fatalf("saved %d", len(saved))
	}
}

func TestM8WelcomeSortBy(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) {
		c.Name, c.Driver, c.Tag = "A", config.MySQL, "production"
	})
	h.a.d.Conns = append(h.a.d.Conns,
		config.Connection{Name: "B", Driver: config.Postgres, Host: "h", Tag: "local"},
		config.Connection{Name: "C", Driver: config.ClickHouse, Host: "h"})
	if got := strings.Join(connNames(h), ""); got != "ABC" {
		t.Fatalf("by name: %s", got)
	}
	h.press("s")
	h.pickLabel("driver")
	if got := strings.Join(connNames(h), ""); got != "CAB" { // clickhouse, mysql, postgres
		t.Fatalf("by driver: %s", got)
	}
	if h.a.d.State.ConnectionSort() != "driver" {
		t.Fatal("the choice is remembered in state.json")
	}
	h.press("s")
	h.pickLabel("tag")
	if got := strings.Join(connNames(h), ""); got != "BAC" { // local, production, untagged
		t.Fatalf("by tag: %s", got)
	}
}
