package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/ai"
	"webtuidb/internal/db"
	"webtuidb/internal/store"
)

// P1-10: a backup over an existing file asks first, and the expanded default name is not remembered.
func TestTUI1BackupAsksBeforeOverwriteAndKeepsPattern(t *testing.T) {
	fakeTool(t, "pg_dump")
	h := newHarness(t, nil)
	h.connect()
	h.prefix("b")
	h.press("enter") // the default file name, submitted as it is
	if got := h.a.d.State.BackupOptions("shop")["backup.file"]; got != "" {
		t.Fatalf("the expanded default %q was remembered", got)
	}
	h.press("esc")
	h.prefix("b")
	existing := filepath.Join(t.TempDir(), "old.sql")
	writeTo(t, existing, "old", 0o644)
	setField(t, h, "file", existing)
	h.press("enter")
	h.press("enter")
	h.want("Overwrite") // the dialog title is truncated to the box
}

// P2-62
func TestTUI1ExportNameKeepsOneExtension(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.a.d.Cfg.Export.Filename = "{table}.csv"
	if got := h.a.ws().exportName("users", "csv"); got != "users.csv" {
		t.Fatalf("got %q", got)
	}
	if got := h.a.ws().exportName("users", "json"); got != "users.csv.json" {
		t.Fatalf("got %q", got)
	}
}

// P2-63: the results find form offers Replace once the result is editable.
func TestTUI1ResultFindOffersReplace(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	q.ed.SetText("select 1")
	h.run("run_current")
	if len(q.res) == 0 || q.res[0].grid == nil {
		t.Fatalf("no result grid:\n%s", h.screen())
	}
	q.res[0].grid.Editable = true // the result maps to one table (makeEditable)
	q.focusResults = true
	h.run("find")
	f, ok := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	if !ok {
		t.Fatalf("no find form:\n%s", h.screen())
	}
	for _, fl := range f.fields {
		if fl.Key == "with" {
			return
		}
	}
	t.Fatal("no Replace field")
}

// P2-66: a failed save in the error path is reported.
func TestTUI1SendFailedReportsSaveError(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	ws := h.a.ws()
	blocker := filepath.Join(t.TempDir(), "file")
	writeTo(t, blocker, "x", 0o644)
	ws.a.d.Chats = &store.Chats{Dir: filepath.Join(blocker, "chats")} // unwritable: a file is in the way
	ws.m3.chat = nil
	tab := &assistantTab{ws: ws}
	ws.m3.chat = append(ws.m3.chat, ai.Message{Role: "user", Content: "earlier"}, ai.Message{Role: "assistant", Content: "ok"}, ai.Message{Role: "user", Content: "hi"}) // something is left to save
	h.pump(tab.sendFailed("hi", errors.New("boom"), false))
	h.want("saving the chat")
	h.want("boom")
}

// P2-68: a pane narrower than the Esc Stop label keeps a rule of exactly the pane width.
func TestTUI1StopRuleNeverWiderThanPane(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	tab := &assistantTab{ws: h.a.ws(), running: true}
	for _, w := range []int{4, 8, 11, 12, 40} {
		for _, l := range strings.Split(tab.View(h.a.th, w, 6), "\n") {
			if got := width(l); got > w {
				t.Fatalf("W=%d: line %q is %d wide", w, l, got)
			}
		}
	}
}

// P2-69 / C1: unreadable limits refuse the form; MAX_USER_CONNECTIONS says 0 is the server default.
func TestTUI1MySQLLimitsUnreadableAndHints(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.dialect = "mysql"
	h.fake.usersOn, h.fake.ulist = true, []db.User{{Name: "ann", Host: "%"}}
	h.connect()
	h.prefix("u")
	h.press("down", "enter")
	h.pickLabel("Resource limits")
	h.want("resource limits could not be read")
	if n := len(h.a.overlays); n > 0 {
		if _, ok := h.a.overlays[n-1].(*formDlg); ok {
			t.Fatal("the form must not open")
		}
	}

	h = newHarness(t, nil)
	h.fake.dialect = "mysql"
	h.fake.usersOn, h.fake.ulist = true, []db.User{{Name: "ann", Host: "%", Limits: map[string]int{}}}
	h.connect()
	h.prefix("u")
	h.press("down", "enter")
	h.pickLabel("Resource limits")
	for _, fl := range h.a.overlays[len(h.a.overlays)-1].(*formDlg).fields {
		want := "0 = unlimited"
		if fl.Key == "MAX_USER_CONNECTIONS" {
			want = "0 = the server default"
		}
		if fl.Hint != want {
			t.Fatalf("%s hint %q", fl.Key, fl.Hint)
		}
	}
}

// P2-70: toggling vim off clears visual and ":" mode.
func TestTUI1VimOffClearsModes(t *testing.T) {
	e := NewEditor("postgres", EditorOpts{})
	e.Insert("select 1")
	e.extra("vim_mode")
	e.vim.vmode, e.vim.cmdOn, e.vim.cmd = 'v', true, "w"
	e.extra("vim_mode")
	if e.vim.on || e.vim.vmode != 0 || e.vim.cmdOn || e.vim.cmd != "" || e.HasSelection() {
		t.Fatalf("state %+v", e.vim)
	}
	e.extra("vim_mode")
	if e.vim.cmdOn {
		t.Fatal("re-enabling resumed command mode")
	}
}

// P2-71: tool output and job errors lose escape sequences.
func TestTUI1BackupLinesAreSanitised(t *testing.T) {
	d := &backupRunDlg{}
	d.add("bad \x1b]52;c;ZXZpbA==\x07name \x1b[31mred\x1b[0m")
	if strings.ContainsRune(d.lines[0], 0x1b) || strings.Contains(d.lines[0], "ZXZpbA") {
		t.Fatalf("line %q", d.lines[0])
	}
	e := &jobEntry{done: true, err: errors.New("x \x1b]0;title\x07y")}
	if s := e.status(""); strings.ContainsRune(s, 0x1b) || strings.Contains(s, "title") {
		t.Fatalf("status %q", s)
	}
}

// P2-48 / P2-59: the import dialog has the NULL-handling fields.
func TestTUI1ImportDialogHasNullFields(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.run("import")
	seen := map[string]string{}
	for _, fl := range h.a.overlays[len(h.a.overlays)-1].(*formDlg).fields {
		seen[fl.Key] = fl.Label
	}
	if seen["nullas"] != "NULL as" || seen["emptynull"] != "Empty is NULL" || seen["keepquote"] != "Keep formula apostrophe" {
		t.Fatalf("fields %v", seen)
	}
	_ = os.Stdout
}

// P2-65: Alt+c changes nothing until the unsaved-changes prompt is answered.
func TestTUI1ClearFiltersWaitsForGuard(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.press("l", "e")
	h.typeText("Q")
	h.press("enter")
	tab := h.a.ws().tab().(*tableTab)
	tab.filters.Filters = []db.Filter{{Enabled: true, Column: "name", Op: "=", Value: "x"}}
	tab.showFilters = true
	h.pump(tab.clearFilters())
	h.want("Discard pending changes?")
	h.press("n")
	if len(tab.filters.Filters) != 1 || !tab.showFilters {
		t.Fatalf("cancelled, but filters=%v bar=%v", tab.filters.Filters, tab.showFilters)
	}
}

// P2-64: export-filtered opens the dialog on an unfiltered table too.
func TestTUI1ExportFilteredWithoutFilters(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.pump(h.a.ws().tab().(*tableTab).exportFiltered())
	h.want("Export customers")
}

// P2-67: the row-detail edit toast counts the rows SetColumn changed and says nothing for an unchanged value.
func TestTUI1FieldEditToast(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	g := h.a.ws().tab().(*tableTab).grid
	g.GoCol(1, false)
	h.pump(editFieldInPopover(h.a.ws(), g))
	h.press("ctrl+a")
	h.typeText("Zed")
	h.press("ctrl+s")
	h.want("staged name in 1 row(s)")
	if strings.Contains(h.screen(), `"name"`) {
		t.Fatal("the toast quotes the column name")
	}
}
