package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/ui"
)

func writePlugin(t *testing.T, name, body string) {
	t.Helper()
	dir := filepath.Join(config.ConfigDir(), "plugins")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600)
}

func waitJob(t *testing.T, e *env, id, jid string) map[string]any {
	t.Helper()
	for i := 0; i < 200; i++ {
		j := e.ok("GET", "/api/sessions/"+id+"/jobs/"+jid, nil)
		if j["done"] == true {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return nil
}

// poll waits for a background job to finish.
func (e *env) poll(id, job string) map[string]any {
	e.t.Helper()
	for i := 0; i < 100; i++ {
		j := e.ok("GET", "/api/sessions/"+id+"/jobs/"+job, nil)
		if j["done"] == true {
			return j
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatal("job did not finish")
	return nil
}

func (f *fakeSess) Users(context.Context) ([]db.User, error) {
	return []db.User{{Name: "bob", Attrs: []string{"LOGIN"}}}, nil
}

func (f *fakeSess) PlanUser(c db.UserChange) ([]db.Statement, error) {
	return []db.Statement{{Text: "CREATE ROLE " + c.User.Name + " PASSWORD 'secret'", Display: "CREATE ROLE " + c.User.Name + " PASSWORD '***'"}}, nil
}

func touchBackup(t *testing.T, name string) {
	t.Helper()
	dir := filepath.Join(config.DataDir(), "backups")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, name), []byte("-- dump\n"), 0o600)
}

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

// richFake adds what the wave C endpoints need on top of fakeSess: foreign keys, column order and pinning.
type richFake struct {
	*fakeSess
	old, nw *db.TableDef
	pinned  *fakeSess
	pins    int
}

func (f *richFake) Caps() db.Capabilities {
	c := f.fakeSess.Caps()
	c.ForeignKeys, c.ReorderColumns = true, true
	return c
}

func (f *richFake) ForeignKeys(context.Context, db.ObjRef) ([]db.ForeignKey, error) { return nil, nil }

func (f *richFake) PlanTable(old, nw *db.TableDef) ([]db.Statement, error) {
	f.old, f.nw = old, nw
	return []db.Statement{{Text: "ALTER TABLE t"}}, nil
}

func (f *richFake) Pin(context.Context) (db.Session, error) { f.pins++; return f.pinned, nil }

func richEnv(t *testing.T, mutate func(*config.Config)) (*env, *richFake) {
	e := newEnvCfg(t, "", mutate)
	rf := &richFake{fakeSess: e.fake, pinned: &fakeSess{}}
	e.s.o.Connect = func(_ context.Context, c config.Connection) (*ui.Opened, error) { return &ui.Opened{Sess: rf}, nil }
	return e, rf
}

type adminFake struct{ *fakeSess }

func (f *adminFake) PlanAdmin(_ db.AdminOp, ref db.ObjRef, _ map[string]string) ([]db.Statement, error) {
	return []db.Statement{{Text: "CREATE DATABASE " + ref.Name}}, nil
}
