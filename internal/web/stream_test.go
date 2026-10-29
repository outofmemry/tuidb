package web

import (
	"io"
	"strings"
	"testing"
	"webtuidb/internal/config"
)

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

// P2-27: only a job with a temp file advertises a "file"; a backup has none.
func TestJobFileOnlyWhenDownloadable(t *testing.T) {
	bk := &job{kind: "backup", fileName: "a.sql", path: "/x/a.sql", done: true}
	if bk.summary("j")["file"] != "" || bk.snapshot()["file"] != "" {
		t.Fatalf("a backup has no downloadable file: %v %v", bk.summary("j"), bk.snapshot())
	}
	ex := &job{kind: "export", fileName: "a.csv", file: "/tmp/a", done: true}
	if ex.summary("j")["file"] != "a.csv" || ex.snapshot()["file"] != "a.csv" || ex.summary("j")["download"] != true {
		t.Fatalf("export job: %v", ex.summary("j"))
	}
}
