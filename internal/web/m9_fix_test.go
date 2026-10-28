package web

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestValidColumnType(t *testing.T) {
	for _, ok := range []string{"int", "varchar(255)", "numeric( 10 , 2 )", "double precision", "text[]", "_int4"} {
		if !validColumnType(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "int); DROP TABLE x;--", "text default 'a'", "1int", "varchar(", "int(a)", "a;b", "int -- c"} {
		if validColumnType(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestImportGateCoversUpdateAndCreate(t *testing.T) {
	if g := importGate(importReq{}); len(g) != 1 || g[0].Text != "INSERT" {
		t.Errorf("plain: %v", g)
	}
	g := importGate(importReq{OnConflict: "replace", CreateTable: true})
	if len(g) != 3 || g[1].Text != "UPDATE" || g[2].Text != "CREATE TABLE" {
		t.Errorf("upsert + create: %v", g)
	}
}

func TestStructurePlanUsesLiveDefinition(t *testing.T) {
	e, rf := richEnv(t, nil)
	p := "/api/sessions/" + e.open()
	// the client claims the table has one column; t1 really has id and name (fake Columns)
	e.ok("POST", p+"/structure/plan", map[string]any{
		"old": map[string]any{"Schema": "public", "Name": "t1", "Columns": []map[string]any{{"Name": "zzz", "Type": "int"}}},
		"new": map[string]any{"Schema": "public", "Name": "t1", "Columns": []map[string]any{{"Name": "id", "Type": "int"}}},
	})
	if rf.old == nil || len(rf.old.Columns) != 2 || rf.old.Columns[1].OrigName != "name" || len(rf.old.PrimaryKey) != 1 {
		t.Fatalf("old def was not loaded server-side: %+v", rf.old)
	}
	if code, _ := e.call("POST", p+"/structure/plan", map[string]any{"new": map[string]any{"Name": "n", "Columns": []map[string]any{{"Name": "a", "Type": "int); DROP TABLE x;--"}}}}, "tok"); code != 400 {
		t.Fatalf("bad type: %d", code)
	}
}

func TestWaitWhilePausedGivesUp(t *testing.T) {
	var p atomic.Bool
	p.Store(true)
	start := time.Now()
	if waitWhilePaused(t.Context(), &p, 120*time.Millisecond) || time.Since(start) < 100*time.Millisecond {
		t.Error("a pause longer than the cap must return false")
	}
	p.Store(false)
	if !waitWhilePaused(t.Context(), &p, time.Second) {
		t.Error("not paused returns true")
	}
}
