package web

import (
	"strings"
	"testing"
	"webtuidb/internal/config"
)

func TestDataTools(t *testing.T) {
	e := newEnv(t, "alert1")
	id := e.open()
	stmts := func(p map[string]any) []string {
		var out []string
		for _, x := range p["statements"].([]any) {
			out = append(out, x.(map[string]any)["display"].(string))
		}
		return out
	}

	sd := e.ok("POST", "/api/sessions/"+id+"/tools/schema-diff", map[string]any{"from": "public", "to": "other"})
	if got := strings.Join(stmts(sd), "|"); got != `CREATE t3|DROP TABLE "public"."t2"` {
		t.Fatalf("schema diff: %s", got)
	}
	if len(e.fake.applied) != 0 || sd["gate"].(map[string]any)["confirm"] != true {
		t.Fatalf("a diff only plans, and the plan is gated: %v", sd)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/tools/schema-diff", map[string]any{}, "tok"); code != 400 {
		t.Fatalf("same schema: %d", code)
	}

	dd := e.ok("POST", "/api/sessions/"+id+"/tools/data-diff", map[string]any{"from": map[string]string{"Name": "t1"}, "to": map[string]string{"Name": "d2"}})
	if dd["changes"] != float64(2) || len(stmts(dd)) != 2 {
		t.Fatalf("data diff: %v", dd)
	}
	same := e.ok("POST", "/api/sessions/"+id+"/tools/data-diff", map[string]any{"from": map[string]string{"Name": "d2"}, "to": map[string]string{"Name": "d2"}})
	if same["changes"] != float64(0) {
		t.Fatalf("identical tables: %v", same)
	}

	bu := e.ok("POST", "/api/sessions/"+id+"/tools/bulk-update", map[string]any{
		"ref": map[string]string{"Name": "t1"}, "set": []map[string]any{{"col": "name", "val": "x"}},
		"filters": map[string]any{"Filters": []map[string]any{{"Column": "id", "Op": "=", "Value": "1", "Enabled": true}}},
	})
	if s := stmts(bu); len(s) != 1 || !strings.HasPrefix(s[0], "UPDATE") || !strings.Contains(s[0], "WHERE") {
		t.Fatalf("bulk update: %v", bu)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/tools/bulk-update", map[string]any{"ref": map[string]string{"Name": "t1"}, "set": []map[string]any{{"col": "name", "val": "x"}}}, "tok"); code != 400 {
		t.Fatalf("no filter must be refused: %d", code)
	}

	fr := e.ok("POST", "/api/sessions/"+id+"/tools/fake-rows", map[string]any{"ref": map[string]string{"Name": "t1"}, "n": 3, "seed": 7})
	if len(stmts(fr)) != 3 || fr["seed"] != float64(7) {
		t.Fatalf("fake rows: %v", fr)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/tools/fake-rows", map[string]any{"ref": map[string]string{"Name": "t1"}, "n": 0}, "tok"); code != 400 {
		t.Fatalf("n=0: %d", code)
	}
	if len(e.fake.applied) != 0 {
		t.Fatal("tools must never apply by themselves")
	}
}

func TestDataDiffRefusesTruncatedRead(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) { c.General.RowLimit = 3 })
	id := e.open()
	code, out := e.call("POST", "/api/sessions/"+id+"/tools/data-diff", map[string]any{"from": map[string]string{"Name": "t1"}, "to": map[string]string{"Name": "d2"}}, "tok")
	if code != 400 || !strings.Contains(out["error"].(string), "row limit") {
		t.Errorf("%d %v", code, out)
	}
}
