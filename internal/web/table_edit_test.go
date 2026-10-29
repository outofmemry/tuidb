package web

import (
	"testing"
)

func TestPlanTableEdit(t *testing.T) {
	e, rf := richEnv(t, nil)
	id := e.open()
	ref := map[string]any{"schema": "public", "name": "t", "kind": "table"}
	e.ok("POST", "/api/sessions/"+id+"/plan/table", map[string]any{"ref": ref, "op": "move_column", "column": "name", "to": 0})
	if got := rf.nw.Columns; got[0].Name != "name" || got[1].Name != "id" || rf.old.Columns[0].Name != "id" || got[0].OrigName != "name" || got[1].OrigName != "id" {
		t.Fatalf("columns after move = %v, before = %v", got, rf.old.Columns)
	}
	e.ok("POST", "/api/sessions/"+id+"/plan/table", map[string]any{"ref": ref, "op": "add_fk", "fk": map[string]any{"Columns": []string{"id"}, "RefTable": "u", "RefColumns": []string{"id"}}})
	if len(rf.nw.ForeignKeys) != 1 || rf.nw.ForeignKeys[0].Name != "fk_t_id" || rf.nw.ForeignKeys[0].OnDelete != "NO ACTION" || len(rf.old.ForeignKeys) != 0 {
		t.Fatalf("fk = %+v", rf.nw.ForeignKeys)
	}
	for _, bad := range []map[string]any{
		{"ref": ref, "op": "move_column", "column": "nope", "to": 0},
		{"ref": ref, "op": "move_column", "column": "id", "to": 0},
		{"ref": ref, "op": "add_fk", "fk": map[string]any{"Columns": []string{"zzz"}, "RefTable": "u", "RefColumns": []string{"id"}}},
		{"ref": ref, "op": "add_fk", "fk": map[string]any{"Columns": []string{"id"}, "RefColumns": []string{"id"}}},
		{"ref": ref, "op": "bogus"},
	} {
		if code, _ := e.call("POST", "/api/sessions/"+id+"/plan/table", bad, "tok"); code != 400 {
			t.Errorf("%v: %d, want 400", bad, code)
		}
	}
}
