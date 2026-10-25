package web

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/mcp"
)

// raw sends a request and returns the response unparsed (for files and event streams).
func (e *env) raw(method, path string, body io.Reader, hdr map[string]string) *http.Response {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, body)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (e *env) post(path string, v any) *http.Response {
	b, _ := json.Marshal(v)
	return e.raw("POST", path, bytes.NewReader(b), nil)
}

type event struct {
	Name string
	Data map[string]any
}

// events reads a whole SSE body.
func events(t *testing.T, r io.Reader) []event {
	t.Helper()
	var out []event
	var cur event
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		switch l := sc.Text(); {
		case strings.HasPrefix(l, "event: "):
			cur.Name = l[7:]
		case strings.HasPrefix(l, "data: "):
			cur.Data = nil
			json.Unmarshal([]byte(l[6:]), &cur.Data)
			out = append(out, cur)
		}
	}
	return out
}

func names(es []event) string {
	var n []string
	for _, e := range es {
		n = append(n, e.Name)
	}
	return strings.Join(n, ",")
}

func TestExportStreamsAndJob(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) { c.Export.Filename = "{table}" })
	id := e.open()
	resp := e.post("/api/sessions/"+id+"/export", map[string]any{"mode": "table", "ref": map[string]string{"Name": "t"}, "format": "csv"})
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Disposition"), "t.csv") || !strings.HasPrefix(string(b), "id,name") {
		t.Fatalf("export: %d %q %v", resp.StatusCode, b, resp.Header)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/export", map[string]any{"mode": "table", "ref": map[string]string{"Name": "t"}, "format": "zip"}, "tok"); code != 400 {
		t.Fatalf("an error before the first byte is a JSON 400: %d", code)
	}

	jid := e.ok("POST", "/api/sessions/"+id+"/export", map[string]any{"mode": "table", "ref": map[string]string{"Name": "t"}, "format": "json", "job": true})["job"].(string)
	es := events(t, e.raw("GET", "/api/sessions/"+id+"/jobs/"+jid+"/events", nil, nil).Body)
	last := es[len(es)-1]
	if last.Name != "done" || last.Data["error"] != "" || last.Data["download"] != true {
		t.Fatalf("export job events: %s %v", names(es), last)
	}
	f := e.raw("GET", "/api/sessions/"+id+"/jobs/"+jid+"/file", nil, nil)
	fb, _ := io.ReadAll(f.Body)
	if !strings.Contains(string(fb), `"ann"`) || !strings.Contains(f.Header.Get("Content-Disposition"), "t.json") {
		t.Fatalf("job file: %q %v", fb, f.Header)
	}
	if code, _ := e.call("GET", "/api/sessions/"+id+"/jobs/"+jid+"/file", nil, "tok"); code != 404 {
		t.Fatalf("the file is handed out once: %d", code)
	}
}

func multipartBody(parts ...[2]string) (io.Reader, string) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for _, p := range parts {
		var fw io.Writer
		if p[0] == "options" {
			fw, _ = w.CreateFormField("options")
		} else {
			fw, _ = w.CreateFormFile(p[0], "x.csv")
		}
		io.WriteString(fw, p[1])
	}
	w.Close()
	return &b, w.FormDataContentType()
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

func TestSearchJobEvents(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	j := e.ok("POST", "/api/sessions/"+id+"/search", map[string]any{"Op": "Contains", "Value": "a"})["job"].(string)
	es := events(t, e.raw("GET", "/api/sessions/"+id+"/jobs/"+j+"/events", nil, nil).Body)
	if es[0].Name != "progress" || es[len(es)-1].Name != "done" {
		t.Fatalf("search events: %s", names(es))
	}
	if code, _ := e.call("GET", "/api/sessions/"+id+"/jobs/nope/events", nil, "tok"); code != 404 {
		t.Fatalf("unknown job: %d", code)
	}
}

func TestExecStream(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	es := events(t, e.post("/api/sessions/"+id+"/exec/stream", map[string]any{"text": "select 1; select 2"}).Body)
	if names(es) != "columns,rows,done,columns,rows,done,end" || es[1].Data["cells"] == nil {
		t.Fatalf("stream: %s", names(es))
	}
	// the gate answers as JSON before the stream starts
	g := newEnv(t, "alert1")
	r := g.post("/api/sessions/"+g.open()+"/exec/stream", map[string]any{"text": "delete from t"})
	if r.StatusCode != 409 || !strings.Contains(r.Header.Get("Content-Type"), "json") {
		t.Fatalf("gate: %d", r.StatusCode)
	}
	es = events(t, e.post("/api/sessions/"+id+"/exec/stream", map[string]any{"text": "boom"}).Body)
	if names(es) != "done,end" || es[0].Data["error"] != "boom" {
		t.Fatalf("error statement: %v", es)
	}
}

func TestNoticesAndExplain(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	r := e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "do raise"})
	nid := r["results"].([]any)[0].(map[string]any)["noticeId"].(string)
	n := e.ok("GET", "/api/sessions/"+id+"/notices/"+nid, nil)
	ns := n["notices"].([]any)
	if n["message"] != "DO" || len(ns) != 2 || ns[0].(map[string]any)["severity"] != "NOTICE" || ns[1].(map[string]any)["message"] != "careful" {
		t.Fatalf("notices: %v", n)
	}
	if code, _ := e.call("GET", "/api/sessions/"+id+"/notices/zzz", nil, "tok"); code != 400 {
		t.Fatalf("unknown result: %d", code)
	}
	x := e.ok("POST", "/api/sessions/"+id+"/explain", map[string]any{"text": "select * from t"})
	nodes := x["nodes"].([]any)
	if len(nodes) != 1 || !strings.Contains(nodes[0].(map[string]any)["label"].(string), "Seq Scan") || !strings.HasPrefix(x["statement"].(string), "EXPLAIN") {
		t.Fatalf("explain: %v", x)
	}
	if code, _ := e.call("POST", "/api/sessions/"+id+"/explain", map[string]any{"text": "select 1; select 2"}, "tok"); code != 400 {
		t.Fatalf("one statement only: %d", code)
	}
}

func TestConnectionsReloadAndSources(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(src+"/team.toml", []byte("[[connection]]\nname = \"shared\"\ndriver = \"postgres\"\nhost = \"h\"\ndatabase = \"d\"\n"), 0o600)
	e := newEnvCfg(t, "", func(c *config.Config) { c.Sources = []config.Source{{Path: src}} })
	names := func() string {
		var out []string
		resp := e.raw("GET", "/api/connections", nil, nil)
		var cs []config.Connection
		json.NewDecoder(resp.Body).Decode(&cs)
		for _, c := range cs {
			out = append(out, c.Name)
		}
		return strings.Join(out, ",")
	}
	if got := names(); got != "c1" { // no connections.toml yet: the injected list stays
		t.Fatalf("before: %s", got)
	}
	main := e.s.o.ConnPath
	os.WriteFile(main, []byte("[[connection]]\nname = \"c1\"\ndriver = \"postgres\"\nhost = \"h\"\ndatabase = \"app\"\n[[connection]]\nname = \"later\"\ndriver = \"postgres\"\nhost = \"h\"\ndatabase = \"d\"\n"), 0o600)
	if got := names(); got != "c1,later,shared" {
		t.Fatalf("after edit: %s", got)
	}
	os.WriteFile(src+"/team.toml", []byte("[[connection]]\nname = \"shared2\"\ndriver = \"postgres\"\nhost = \"h\"\ndatabase = \"d\"\n"), 0o600)
	if got := names(); got != "c1,later,shared2" {
		t.Fatalf("after source edit: %s", got)
	}
	var cs []config.Connection
	json.NewDecoder(e.raw("GET", "/api/connections", nil, nil).Body).Decode(&cs)
	// a source connection cannot be edited or dropped through the API ...
	edited := append([]config.Connection{}, cs...)
	edited[2].Host = "evil"
	if code, out := e.call("PUT", "/api/connections", edited, "tok"); code != 400 || !strings.Contains(out["error"].(string), "read-only") {
		t.Fatalf("edit: %d %v", code, out)
	}
	if code, _ := e.call("PUT", "/api/connections", cs[:2], "tok"); code != 400 {
		t.Fatalf("remove: %d", code)
	}
	// ... but the rest is saved, and only to the main file
	cs[1].Host = "h2"
	e.ok("PUT", "/api/connections", cs)
	main2, _ := config.LoadConnections(main)
	if len(main2) != 2 || main2[1].Host != "h2" {
		t.Fatalf("main file: %v", main2)
	}
	if got := names(); got != "c1,later,shared2" {
		t.Fatalf("after put: %s", got)
	}
}

func TestLoginCookie(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) { c.Web.Password = "hunter2" })
	hit := func(method, path, body string, cookie *http.Cookie) *http.Response {
		req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	if r := hit("GET", "/api/connections", "", nil); r.StatusCode != 401 {
		t.Fatalf("anonymous: %d", r.StatusCode)
	}
	if r := hit("POST", "/api/login", `{"password":"nope"}`, nil); r.StatusCode != 401 || len(r.Cookies()) != 0 {
		t.Fatalf("wrong password: %d", r.StatusCode)
	}
	r := hit("POST", "/api/login", `{"password":"hunter2"}`, nil)
	if r.StatusCode != 200 || len(r.Cookies()) != 1 {
		t.Fatalf("login: %d", r.StatusCode)
	}
	c := r.Cookies()[0]
	if !c.HttpOnly || len(c.Value) != 64 || c.MaxAge != 12*3600 {
		t.Fatalf("cookie: %+v", c)
	}
	if r := hit("GET", "/api/connections", "", c); r.StatusCode != 200 {
		t.Fatalf("cookie must replace the token: %d", r.StatusCode)
	}
	if r := hit("GET", "/api/connections", "", &http.Cookie{Name: cookieName, Value: "guess"}); r.StatusCode != 401 {
		t.Fatalf("forged cookie: %d", r.StatusCode)
	}
	if r := hit("GET", "/", "", nil); r.StatusCode == 401 {
		t.Fatal("the static index must not need a login")
	}
	var meta map[string]any
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/meta", nil)
	req.AddCookie(c)
	resp, _ := http.DefaultClient.Do(req)
	json.NewDecoder(resp.Body).Decode(&meta)
	resp.Body.Close()
	if meta["login"] != true {
		t.Fatalf("meta: %v", meta["login"])
	}
	// no password configured: the endpoint is off
	e2 := newEnv(t, "")
	if code, _ := e2.call("POST", "/api/login", map[string]string{"password": ""}, ""); code != 404 {
		t.Fatalf("login without a password: %d", code)
	}
}

func TestMCPCSVAndClients(t *testing.T) {
	e := newEnvCfg(t, "", func(c *config.Config) { c.MCP.Enabled = true })
	e.ok("GET", "/api/mcp/log", nil)
	r := e.raw("GET", "/api/mcp/log?format=csv", nil, nil)
	b, _ := io.ReadAll(r.Body)
	if !strings.HasPrefix(string(b), "time,token,tool") || !strings.Contains(r.Header.Get("Content-Disposition"), "mcp-log.csv") || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/csv") {
		t.Fatalf("csv: %q %v", b, r.Header)
	}
	c := e.ok("GET", "/api/mcp/clients", nil)
	if c["enabled"] != true || len(c["clients"].([]any)) != 0 {
		t.Fatalf("clients: %v", c)
	}
	_ = mcp.Entry{}
}

func TestConsoleAndTabsAndHistoryFiles(t *testing.T) {
	e := newEnv(t, "")
	id := e.open()
	e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select 7"})
	var con []map[string]any
	json.NewDecoder(e.raw("GET", "/api/console", nil, nil).Body).Decode(&con)
	if len(con) != 1 || con[0]["text"] != "select 7" || con[0]["time"] == "" {
		t.Fatalf("console: %v", con)
	}
	e.ok("DELETE", "/api/console", nil)
	for i := 0; i < consoleCap+5; i++ {
		e.s.con.add("c1", "x")
	}
	if len(e.s.con.lines) != consoleCap {
		t.Fatalf("cap: %d", len(e.s.con.lines))
	}

	if code, _ := e.call("PUT", "/api/state/tabs?conn=c1", []map[string]string{{"Kind": "sql", "Text": "select 1"}, {"Kind": "table", "Name": "public.t"}}, "tok"); code != 200 {
		t.Fatalf("put tabs: %d", code)
	}
	var tabs []map[string]any
	json.NewDecoder(e.raw("GET", "/api/state/tabs?conn=c1", nil, nil).Body).Decode(&tabs)
	if len(tabs) != 2 || tabs[1]["name"] != "public.t" {
		t.Fatalf("tabs: %v", tabs)
	}
	if code, _ := e.call("GET", "/api/state/tabs", nil, "tok"); code != 400 {
		t.Fatalf("conn required: %d", code)
	}

	// history: export csv, clear, import it back
	e.ok("DELETE", "/api/history?conn=c1&all=1", nil)
	e.ok("POST", "/api/sessions/"+id+"/exec", map[string]any{"text": "select 'a,b'"})
	csv, _ := io.ReadAll(e.raw("GET", "/api/history/export?conn=c1&format=csv", nil, nil).Body)
	if !strings.HasPrefix(string(csv), "time,database,sql,duration_ms,source") || !strings.Contains(string(csv), `"select 'a,b'"`) {
		t.Fatalf("history csv: %q", csv)
	}
	js, _ := io.ReadAll(e.raw("GET", "/api/history/export?conn=c1", nil, nil).Body)
	e.ok("DELETE", "/api/history?conn=c1&all=1", nil)
	if out := e.ok("POST", "/api/history/import", map[string]any{"conn": "c1", "format": "csv", "content": string(csv)}); out["imported"] != float64(1) {
		t.Fatalf("import csv: %v", out)
	}
	if out := e.ok("POST", "/api/history/import", map[string]any{"conn": "c1", "content": string(js)}); out["imported"] != float64(1) {
		t.Fatalf("import json: %v", out)
	}
	if l, _ := e.hist.List("c1", "", 0); len(l) != 2 || l[0].SQL != "select 'a,b'" {
		t.Fatalf("history: %v", l)
	}

	// saved queries
	e.ok("POST", "/api/queries/import-file", map[string]any{"name": "report.sql", "sql": "select 1"})
	qs, _ := io.ReadAll(e.raw("GET", "/api/queries/export", nil, nil).Body)
	if !strings.Contains(string(qs), "report") || !strings.Contains(string(qs), "select 1") {
		t.Fatalf("queries: %s", qs)
	}
	if code, _ := e.call("POST", "/api/queries/import-file", map[string]any{"name": "x"}, "tok"); code != 400 {
		t.Fatalf("empty sql: %d", code)
	}
}

func TestConnectionsExportImport(t *testing.T) {
	e := newEnv(t, "")
	if code, _ := e.call("GET", "/api/connections/export?passwords=1", nil, "tok"); code != 400 {
		t.Fatalf("passwords need a passphrase: %d", code)
	}
	r := e.raw("GET", "/api/connections/export?passphrase=s3cret&passwords=1", nil, nil)
	b, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || strings.Contains(string(b), "pw") || !strings.HasPrefix(string(b), "WTDB1") {
		t.Fatalf("export: %d %q", r.StatusCode, b)
	}
	if code, _ := e.call("POST", "/api/connections/import", map[string]any{"contentBase64": base64.StdEncoding.EncodeToString(b), "passphrase": "wrong"}, "tok"); code != 400 {
		t.Fatalf("wrong passphrase: %d", code)
	}
	out := e.ok("POST", "/api/connections/import", map[string]any{"contentBase64": base64.StdEncoding.EncodeToString(b), "passphrase": "s3cret"})
	if s := out["skipped"].([]any); len(s) != 1 || s[0] != "c1" || len(out["added"].([]any)) != 0 {
		t.Fatalf("an existing name is skipped: %v", out)
	}
	// a file with a new name is added and saved
	e.s.o.Conns[0].Name = "c9"
	b2 := e.raw("GET", "/api/connections/export?passphrase=s3cret", nil, nil)
	nb, _ := io.ReadAll(b2.Body)
	e.s.o.Conns[0].Name = "c1"
	out = e.ok("POST", "/api/connections/import", map[string]any{"contentBase64": base64.StdEncoding.EncodeToString(nb), "passphrase": "s3cret"})
	if a := out["added"].([]any); len(a) != 1 || a[0] != "c9" {
		t.Fatalf("add: %v", out)
	}
	if saved, _ := config.LoadConnections(e.s.o.ConnPath); len(saved) != 2 {
		t.Fatalf("saved: %v", saved)
	}
}

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

var _ = time.Second

func TestImportQueryFileDoesNotOverwrite(t *testing.T) {
	e := newEnv(t, "")
	e.ok("POST", "/api/queries/import-file", map[string]any{"name": "daily.sql", "sql": "select 1"})
	if code, _ := e.call("POST", "/api/queries/import-file", map[string]any{"name": "daily.sql", "sql": "select 2"}, "tok"); code != 409 {
		t.Fatalf("an existing query must not be replaced silently: %d", code)
	}
	e.ok("POST", "/api/queries/import-file", map[string]any{"name": "daily.sql", "sql": "select 2", "overwrite": true})
}

func TestPutConnsRefusesNewCommands(t *testing.T) {
	e := newEnv(t, "")
	c := map[string]any{"Name": "c1", "Driver": "postgres", "Host": "h", "Database": "app", "PasswordCmd": "sh -c id"}
	if code, _ := e.call("PUT", "/api/connections", []any{c}, "tok"); code != 400 {
		t.Fatalf("a new password_cmd must be refused: %d", code)
	}
	delete(c, "PasswordCmd")
	if code, _ := e.call("PUT", "/api/connections", []any{c}, "tok"); code != 200 && code != 204 {
		t.Fatalf("a plain edit must still work: %d", code)
	}
}
