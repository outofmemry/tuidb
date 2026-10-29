package web

import (
	"net/http"
	"strings"
	"testing"
)

func TestBoardExportJobAndDropdownOptions(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "")
	id := e.open()
	panels := []map[string]any{
		{"Title": "n", "Kind": "number", "Query": "select 1 as x"},
		{"Title": "dl", "Kind": "exporter", "Query": "select 1 as x", "Format": "csv"},
		{"Title": "pick", "Kind": "dropdown", "var": "v", "options": []string{"a", "b"}, "default": "a"},
		{"Title": "who", "Kind": "input", "var": "w"},
	}
	e.ok("PUT", "/api/boards/new", map[string]any{"Name": "b1", "Panels": panels})
	if code, _ := e.call("PUT", "/api/boards/b1", map[string]any{"Name": "b1", "Panels": []map[string]any{{"Title": "x", "Kind": "exporter", "Query": "select 1", "Format": "pdf"}}}, "tok"); code != 400 {
		t.Fatalf("bad exporter format: %d", code)
	}
	bs := e.ok("GET", "/api/boards", nil)
	_ = bs
	opts := e.ok("GET", "/api/sessions/"+id+"/boards/b1/options?panel=pick", nil)["options"].([]any)
	if len(opts) != 2 || opts[1] != "b" {
		t.Fatalf("options = %v", opts)
	}
	if code, _ := e.call("GET", "/api/sessions/"+id+"/boards/b1/options?panel=n", nil, "tok"); code != 400 {
		t.Fatalf("options of a number panel: %d", code)
	}
	// the run skips the panels the UIs handle
	if r := e.ok("POST", "/api/sessions/"+id+"/boards/run", map[string]any{"Name": "b1"}); len(r["panels"].([]any)) != 1 {
		t.Fatalf("run = %v", r)
	}
	jid := e.ok("POST", "/api/sessions/"+id+"/boards/b1/export", map[string]any{"panel": "dl", "format": "csv"})["job"].(string)
	waitJob(t, e, id, jid)
	row := e.ok("GET", "/api/sessions/"+id+"/jobs", nil)["jobs"].([]any)[0].(map[string]any)
	if row["kind"] != "export" || !strings.HasSuffix(row["file"].(string), ".csv") || row["err"] != "" {
		t.Fatalf("row = %v", row)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/sessions/"+id+"/jobs/"+jid+"/file", nil)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	if got := strings.ReplaceAll(string(buf[:n]), "\r\n", "\n"); got != "x\n1\n" {
		t.Fatalf("file = %q", got)
	}
}
