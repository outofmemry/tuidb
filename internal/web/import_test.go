package web

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestImportGatedAndRuns(t *testing.T) {
	e := newEnv(t, "alert1")
	id := e.open()
	body := map[string]any{"source": "csv", "header": true, "content": "id,name\n1,a\n2,b\n", "ref": map[string]string{"Name": "t"}, "mapping": map[string]string{"id": "id", "name": ""}}
	pv := e.ok("POST", "/api/sessions/"+id+"/import/preview", body)
	if len(pv["rows"].([]any)) != 2 || len(pv["columns"].([]any)) != 2 {
		t.Fatalf("preview: %v", pv)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/import", body, "tok"); code != 409 {
		t.Fatalf("an import must pass the safe-mode gate: %d", code)
	}
	body["confirmed"] = true
	j := e.poll(id, e.ok("POST", "/api/sessions/"+id+"/import", body)["job"].(string))
	if j["error"] != "" || j["rows"].(float64) != 2 || len(e.fake.applied) != 1 {
		t.Fatalf("import: %v applied=%d", j, len(e.fake.applied))
	}
}

func TestImportMultipartAndEvents(t *testing.T) {
	e := newEnv(t, "alert1")
	id := e.open()
	opts := `{"source":"csv","header":true,"ref":{"Name":"t"},"confirmed":true}`
	body, ct := multipartBody([2]string{"options", opts}, [2]string{"file", "id,name\n1,a\n2,b\n"})
	jid := func() string {
		var out map[string]any
		json.NewDecoder(e.raw("POST", "/api/sessions/"+id+"/import", body, map[string]string{"Content-Type": ct}).Body).Decode(&out)
		j, _ := out["job"].(string)
		return j
	}()
	if jid == "" {
		t.Fatal("no job from a multipart import")
	}
	es := events(t, e.raw("GET", "/api/sessions/"+id+"/jobs/"+jid+"/events", nil, nil).Body)
	if es[len(es)-1].Name != "done" || es[len(es)-1].Data["error"] != "" || len(e.fake.applied) == 0 {
		t.Fatalf("import events: %s %v", names(es), es)
	}
	// unconfirmed: gated before the file is read
	body, ct = multipartBody([2]string{"options", `{"source":"csv","header":true,"ref":{"Name":"t"}}`}, [2]string{"file", "id\n1\n"})
	if r := e.raw("POST", "/api/sessions/"+id+"/import", body, map[string]string{"Content-Type": ct}); r.StatusCode != 409 {
		t.Fatalf("gate: %d", r.StatusCode)
	}
	// file before options
	body, ct = multipartBody([2]string{"file", "id\n1\n"}, [2]string{"options", opts})
	if r := e.raw("POST", "/api/sessions/"+id+"/import", body, map[string]string{"Content-Type": ct}); r.StatusCode != 400 {
		t.Fatalf("options must come first: %d", r.StatusCode)
	}
	// preview takes multipart too
	body, ct = multipartBody([2]string{"options", `{"source":"csv","header":true}`}, [2]string{"file", "id,name\n1,a\n"})
	var pv map[string]any
	json.NewDecoder(e.raw("POST", "/api/sessions/"+id+"/import/preview", body, map[string]string{"Content-Type": ct}).Body).Decode(&pv)
	if len(pv["rows"].([]any)) != 1 {
		t.Fatalf("preview: %v", pv)
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

// P2-48/P2-59: the import request carries null_as, empty_is_null and keep_formula_quote.
func TestImportNullOptions(t *testing.T) {
	e := newEnv(t, "")
	var req importReq
	if err := json.Unmarshal([]byte(`{"source":"csv","null_as":"\\N","empty_is_null":true,"keep_formula_quote":true}`), &req); err != nil {
		t.Fatal(err)
	}
	o, err := e.s.importOpts(req)
	if err != nil || o.NullAs != `\N` || !o.EmptyIsNull || !o.KeepFormulaQuote {
		t.Fatalf("opts = %+v %v", o, err)
	}
}

func TestImportDropsCommandsAndReportsThem(t *testing.T) {
	e := newEnv(t, "")
	src := "[[connection]]\nname = 'n'\ndriver = 'postgres'\nhost = 'h'\npassword_cmd = 'sh -c id'\nbootstrap = ['select 1']\n"
	out := e.ok("POST", "/api/connections/import", map[string]any{"contentBase64": base64.StdEncoding.EncodeToString([]byte(src))})
	if d := out["dropped"].([]any); len(d) != 2 {
		t.Errorf("%v", out)
	}
	b, _ := os.ReadFile(e.s.o.ConnPath)
	if strings.Contains(string(b), "password_cmd") || strings.Contains(string(b), "bootstrap") {
		t.Errorf("saved:\n%s", b)
	}
	if n := len(e.s.o.Conns); n != 2 {
		t.Errorf("conns: %d", n)
	}
}
