package web

import (
	"testing"
)

func TestM9EditableExecAndStructurePlan(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	p := "/api/sessions/" + id
	res := e.ok("POST", p+"/exec", map[string]any{"text": "select * from t1"})["results"].([]any)[0].(map[string]any)
	tb, _ := res["table"].(map[string]any)
	if res["editable"] != true || res["resultId"] == "" || tb["name"] != "t1" || tb["pk"].([]any)[0] != "id" {
		t.Fatalf("editable exec result: %v", res)
	}
	// the cached result takes /plan edits like a table page
	e.ok("POST", p+"/plan", map[string]any{"resultId": res["resultId"], "updates": []map[string]any{{"row": 0, "set": map[string]any{"name": "bob"}}}})
	res = e.ok("POST", p+"/exec", map[string]any{"text": "select 1"})["results"].([]any)[0].(map[string]any)
	if res["editable"] != false || res["table"] != nil {
		t.Fatalf("a bare select must not be editable: %v", res)
	}
	if types := e.ok("GET", p+"/datatypes", nil)["types"].([]any); len(types) == 0 {
		t.Fatal("datatypes empty")
	}
	plan := e.ok("POST", p+"/structure/plan", map[string]any{"old": nil, "new": map[string]any{"Name": "n", "Columns": []map[string]any{{"Name": "a", "Type": "int"}}}})
	if plan["planId"] == "" {
		t.Fatalf("structure plan: %v", plan)
	}
	if code, _ := e.call("POST", p+"/structure/plan", map[string]any{"new": map[string]any{"Name": "n"}}, "tok"); code != 400 {
		t.Fatalf("no columns: %d", code)
	}
}

func TestM9MongoSyntax(t *testing.T) {
	if checkSyntax("mongo", "command", "db.a.find()") == nil || checkSyntax("mongo", "shell", "{ping:1}") == nil || checkSyntax("mongo", "", "{ping:1}") != nil || checkSyntax("postgres", "shell", "{") != nil {
		t.Fatal("syntax check")
	}
}
