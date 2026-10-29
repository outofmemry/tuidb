package web

import (
	"context"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/ui"
)

// BEGIN on an unpinned tab pins it, the transaction shows as open, and COMMIT releases the pin again.
func TestBeginPinsTheTabUntilCommit(t *testing.T) {
	e, rf := richEnv(t, nil)
	id := e.open()
	run := func(text string) map[string]any {
		return e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": text, "tab": "q1"})
	}
	if r := run("select 1"); r["pinned"] != false || rf.pins != 0 {
		t.Fatalf("a plain select must not pin: %v", r)
	}
	if r := run("begin"); r["pinned"] != true || r["autoPinned"] != true || r["inTx"] != true || rf.pins != 1 {
		t.Fatalf("begin: %v pins=%d", r, rf.pins)
	}
	r := e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "update t set a = 1", "tab": "q1", "pin": "q1"})
	if r["inTx"] != true || len(rf.pinned.execs) != 2 {
		t.Fatalf("the update must run on the pinned connection inside the transaction: %v %v", r, rf.pinned.execs)
	}
	r = e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "commit", "tab": "q1", "pin": "q1"})
	if r["pinned"] != false || r["inTx"] != false {
		t.Fatalf("commit must end the transaction and release the automatic pin: %v", r)
	}
	if r := run("begin; select 1; rollback"); r["pinned"] != false || r["inTx"] != false {
		t.Fatalf("a batch that ends its own transaction keeps nothing pinned: %v", r)
	}
	// a pin the user made stays after COMMIT
	e.ok("POST", "/api/sessions/"+id+"/pin/q2", nil)
	e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "begin", "tab": "q2", "pin": "q2"})
	if r := e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "commit", "tab": "q2", "pin": "q2"}); r["pinned"] != true || r["inTx"] != false {
		t.Fatalf("a manual pin survives COMMIT: %v", r)
	}
}

type schemaFake struct {
	*richFake
	cur string
}

func (f *schemaFake) CurrentSchema() string { return f.cur }
func (f *schemaFake) SetSchema(_ context.Context, name string) error {
	f.cur = name
	return nil
}

func TestUseSchemaSetsTheSessionSchema(t *testing.T) {
	e, rf := richEnv(t, nil)
	sf := &schemaFake{richFake: rf, cur: "public"}
	e.s.o.Connect = func(context.Context, config.Connection) (*ui.Opened, error) { return &ui.Opened{Sess: sf}, nil }
	id := e.open()
	if info := e.ok("POST", "/api/sessions/"+id+"/schema", map[string]any{"name": "other"}); info["schema"] != "other" || sf.cur != "other" {
		t.Fatalf("schema switch: %v", info)
	}
	if info := e.ok("GET", "/api/sessions/"+id, nil); info["schema"] != "other" {
		t.Fatalf("a reload must see the switched schema: %v", info)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/schema", map[string]any{"name": ""}, "tok"); code != 400 {
		t.Fatalf("an empty name: %d", code)
	}
}
