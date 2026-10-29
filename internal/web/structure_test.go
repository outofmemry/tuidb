package web

import (
	"testing"
)

func TestAlterColumnPlansARename(t *testing.T) {
	e, rf := richEnv(t, nil)
	id := e.open()
	ref := map[string]any{"schema": "public", "name": "t", "kind": "table"}
	e.ok("POST", "/api/sessions/"+id+"/plan/table", map[string]any{"ref": ref, "op": "alter_column", "column": "name", "col": map[string]any{"Name": "title", "Type": "varchar(40)", "Nullable": false, "Default": "'x'", "HasDef": true}})
	c := rf.nw.Columns[1]
	if c.Name != "title" || c.OrigName != "name" || c.Type != "varchar(40)" || c.Nullable || !c.HasDef || c.Default != "'x'" || rf.old.Columns[1].Name != "name" {
		t.Fatalf("new column = %+v, old = %+v", c, rf.old.Columns[1])
	}
	for _, bad := range []map[string]any{{"Name": "id", "Type": "int"}, {"Name": "", "Type": "int"}, {"Name": "z", "Type": ""}} {
		if code, _ := e.call("POST", "/api/sessions/"+id+"/plan/table", map[string]any{"ref": ref, "op": "alter_column", "column": "name", "col": bad}, "tok"); code != 400 {
			t.Errorf("%v: %d, want 400", bad, code)
		}
	}
}

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
