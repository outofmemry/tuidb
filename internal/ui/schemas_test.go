package ui

import (
	"testing"

	"webtuidb/internal/config"
)

func TestM6SchemaCreateThroughTheGate(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SafeMode = "alert1" })
	h.connect()
	h.run("new_schema")
	setField(t, h, "name", "audit")
	h.press("enter")
	h.want("send to the server?")
	if len(h.fake.applied) != 0 {
		t.Fatal("nothing is sent before confirming")
	}
	h.press("y")
	if len(h.fake.applied) != 1 || h.fake.applied[0][0].Text != "-- create_schema audit" {
		t.Fatalf("applied %+v", h.fake.applied)
	}
}

func TestM6SchemaDropNeedsTheName(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.press("m")
	h.pickLabel("Drop schema")
	h.pickLabel("app")
	setField(t, h, "confirm", "app")
	h.press("enter")
	if len(h.fake.applied) != 1 || h.fake.applied[0][0].Text != "-- drop_schema app" {
		t.Fatalf("applied %+v", h.fake.applied)
	}
}

// T-P2-04 / T-P2-09: the sidebar's schema switch changes the session's current schema.
func TestFlowSchemaSwitchSetsSessionSchema(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.pump(h.a.ws().sb.pickSchema())
	h.pickLabel("app")
	if h.fake.schema != "app" || h.a.ws().sb.schema != "app" {
		t.Fatalf("session schema %q, sidebar %q", h.fake.schema, h.a.ws().sb.schema)
	}
	h.want("shop.app")
}
