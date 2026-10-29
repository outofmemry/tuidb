// harness_test.go: test helpers shared by several test files (fields, fake tools, chat server, menu labels, lock harness).

package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/ai"
	"webtuidb/internal/config"
)

func aiMsg(role, content string) ai.Message { return ai.Message{Role: role, Content: content} }

// menuLabels are the labels of the picker on top of the overlay stack.
func (h *harness) menuLabels() []string {
	h.t.Helper()
	p, ok := h.a.overlays[len(h.a.overlays)-1].(*pickerDlg)
	if !ok {
		h.t.Fatalf("top overlay is %T, not a menu", h.a.overlays[len(h.a.overlays)-1])
	}
	var out []string
	for _, it := range p.items {
		out = append(out, it.Label)
	}
	return out
}

func sameLabels(a, b []string) bool { return strings.Join(a, "|") == strings.Join(b, "|") }

func lockHarness(t *testing.T) *harness {
	hash, err := config.HashPasscode("1234")
	if err != nil {
		t.Fatal(err)
	}
	return newHarness(t, func(_ *config.Connection, c *config.Config) { c.Security.Passcode, c.Security.LockAfterMins = hash, 5 })
}

// setField fills a text field of the dialog on top, like typing it.
func setField(t *testing.T, h *harness, key, val string) {
	t.Helper()
	f, ok := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	if !ok {
		t.Fatalf("no form on top:\n%s", h.screen())
	}
	for _, fl := range f.fields {
		if fl.Key == key {
			fl.edit = newLine(val)
			return
		}
	}
	t.Fatalf("no field %q", key)
}

func queryResult(h *harness, sql string) {
	h.prefix("e")
	h.typeText(sql)
	h.prefix("alt+enter")
	h.prefix("alt+j") // focus the results
}

func writeTo(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}

// topView renders the dialog on top of the stack by itself.
func topView(h *harness) string {
	return stripANSI(h.a.overlays[len(h.a.overlays)-1].View(h.a.th, 120, 36))
}

func fakeTool(t *testing.T, name string) {
	dir := t.TempDir()
	writeTo(t, filepath.Join(dir, name), "#!/bin/sh\necho dumping >&2\nprev=; for a in \"$@\"; do [ \"$prev\" = -f ] && : > \"$a\"; prev=$a; done\n", 0o755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func twoTabs(h *harness) *Workspace {
	h.connect()
	h.openCustomers()
	h.prefix("e")
	return h.a.ws()
}

func chatServer(t *testing.T, reply string, body *string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*body = string(b)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": reply}}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func tabNames(h *harness) []string {
	var out []string
	for _, t := range h.a.ws().tabs {
		out = append(out, h.a.ws().tabTitle(t))
	}
	return out
}

func (h *harness) fakeFormChoice(key, val string) {
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	for _, fl := range f.fields {
		if fl.Key == key {
			fl.Value = val
		}
	}
}

func (h *harness) run(action string) { h.t.Helper(); h.pump(h.a.run(action)) }

func setChoice(t *testing.T, h *harness, key, val string) {
	t.Helper()
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	for _, fl := range f.fields {
		if fl.Key == key {
			fl.Value = val
			return
		}
	}
	t.Fatalf("no field %q", key)
}

// at finds the screen cell of the first occurrence of s.
func (h *harness) at(s string) (x, y int) {
	h.t.Helper()
	for y, l := range strings.Split(h.screen(), "\n") {
		if i := strings.Index(l, s); i >= 0 {
			return len([]rune(l[:i])), y
		}
	}
	h.t.Fatalf("%q is not on screen:\n%s", s, h.screen())
	return
}

// pickLabel chooses the item of the top picker whose label contains label.
func (h *harness) pickLabel(label string) {
	h.t.Helper()
	p, ok := h.a.overlays[len(h.a.overlays)-1].(*pickerDlg)
	if !ok {
		h.t.Fatalf("no picker on top:\n%s", h.screen())
	}
	for i, it := range p.shown() {
		if strings.Contains(it.Label, label) {
			p.sel = i
			h.press("enter")
			return
		}
	}
	h.t.Fatalf("no item %q in %+v", label, p.shown())
}

func (h *harness) queryTab() *queryTab {
	h.t.Helper()
	q, ok := h.a.ws().tab().(*queryTab)
	if !ok {
		h.t.Fatalf("the active tab is not an editor:\n%s", h.screen())
	}
	return q
}

func indexOfRow(rows []sbRow, group string) int {
	for i, r := range rows {
		if r.group == group {
			return i
		}
	}
	return 0
}

func boardsDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

func connNames(h *harness) []string {
	var out []string
	for _, r := range h.a.welcome.rows() {
		if r.header == "" {
			out = append(out, r.conn.Name)
		}
	}
	return out
}

func runSQL(h *harness, sql string) {
	h.t.Helper()
	h.queryTab().ed.SetText(sql)
	h.prefix("enter")
}
