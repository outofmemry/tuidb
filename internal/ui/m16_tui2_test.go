package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/keymap"
	"webtuidb/internal/store"
)

func consoleHarness(t *testing.T, mutate func(*config.Connection, *config.Config)) (*harness, *Workspace) {
	h := newHarness(t, mutate)
	h.connect()
	w := h.a.ws()
	w.console.Add("select 1", false, time.Millisecond)
	w.console.Add("select bad", true, time.Millisecond)
	w.showConsole = true
	h.a.run("focus_down")
	if !w.focusConsole {
		t.Fatal("focus_down should reach the console")
	}
	return h, w
}

// P2-72: the console keys come from the keymap, so [keys] rebinds them and the context is Console.
func TestM16ConsoleKeysAreRebindable(t *testing.T) {
	h, w := consoleHarness(t, func(_ *config.Connection, c *config.Config) {
		c.KeyOverrides = map[string][]string{"console_errors": {"x"}}
	})
	if w.context() != keymap.Console {
		t.Fatalf("context %q", w.context())
	}
	h.press("e") // no longer bound
	if w.con.errorsOnly {
		t.Error("e must not toggle errors-only once console_errors is rebound")
	}
	h.press("x")
	if !w.con.errorsOnly {
		t.Error("the rebound key must toggle errors-only")
	}
}

// P2-78: a console that is not drawn must not keep the keys.
func TestM16ConsoleFocusDropsWhenHidden(t *testing.T) {
	h, w := consoleHarness(t, nil)
	h.a.Update(tea.WindowSizeMsg{Width: 120, Height: 12}) // too short for the console pane
	if w.context() == keymap.Console || w.consoleFocused() {
		t.Fatal("the hidden console still has the context")
	}
	h.press("/")
	if w.con.searching {
		t.Error("a key reached the invisible console")
	}
	if w.focusConsole {
		t.Error("focusConsole should have been cleared")
	}
}

// P2-73: deleting a saved chat asks first.
func TestM16ChatDeleteAsksFirst(t *testing.T) {
	h := newHarness(t, nil)
	chats := &store.Chats{Dir: t.TempDir()}
	h.a.d.Chats = chats
	h.connect()
	w := h.a.ws()
	w.m3.chat = append(w.m3.chat, aiMsg("user", "how many customers?"), aiMsg("assistant", "three"))
	h.run("assistant_new")
	h.run("assistant_history")
	h.press("ctrl+x")
	if _, ok := h.a.overlays[len(h.a.overlays)-1].(*confirmDlg); !ok {
		t.Fatalf("no confirmation on top:\n%s", h.screen())
	}
	if l, _ := chats.List("shop"); len(l) != 1 {
		t.Fatal("deleted before the answer")
	}
	h.press("n")
	if l, _ := chats.List("shop"); len(l) != 1 {
		t.Fatal("deleted after no")
	}
	h.press("ctrl+x")
	h.press("y")
	if l, _ := chats.List("shop"); len(l) != 0 {
		t.Fatalf("not deleted after yes: %+v", l)
	}
}

// P2-74: one vocabulary (the ANSI code) for the sidebar and the diagram; names and hex migrate on read.
func TestM16DiagramColoursShareOneVocabulary(t *testing.T) {
	for in, want := range map[string]string{"red": "203", "203": "203", "#e06c75": "203", "#61afef": "39", "": "", "nonsense": ""} {
		if got := colourCode(in); got != want {
			t.Errorf("colourCode(%q) = %q, want %q", in, got, want)
		}
	}
	h := newHarness(t, nil)
	h.connect()
	h.a.d.State.SetDiagramColors("shop", map[string]string{"public.a": "red", "public.b": "#98c379", "public.c": "208"})
	got := h.a.ws().diagramColors()
	if got["public.a"] != "203" || got["public.b"] != "41" || got["public.c"] != "208" {
		t.Errorf("migrated %v", got)
	}
	// the sidebar's colour for a table is what the diagram tints with
	s := h.a.ws().sb
	if c := s.itemColour(db.ObjRef{Schema: "public", Name: "a"}); c != "203" {
		t.Errorf("sidebar colour %q", c)
	}
}

// P2-75: a secret prompt while the app is locked fails instead of hanging.
func TestM16AskSecretWhileLocked(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.Security.Passcode = "x" })
	h.a.push(&lockDlg{a: h.a, in: lineEdit{Secret: true}, now: time.Now})
	type res struct {
		v   string
		err error
	}
	out := make(chan res, 1)
	go func() { v, err := h.a.askSecret("Password", "shop"); out <- res{v, err} }()
	select {
	case f := <-h.a.mcpUI:
		f()
	case <-time.After(2 * time.Second):
		t.Fatal("the prompt never reached the UI loop")
	}
	select {
	case r := <-out:
		if r.err == nil || r.err.Error() != "locked" {
			t.Errorf("got %q, %v", r.v, r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("askSecret blocked while locked")
	}
	if len(h.a.overlays) != 1 {
		t.Errorf("a dialog was queued behind the lock: %d overlays", len(h.a.overlays))
	}
}

// P2-76: a board exporter that hit its row cap says the file is partial.
func TestM16BoardExportLabelsTheLimit(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	rows := make([][]any, boardExportLimit)
	h.a.ws().exportResultForm("panel", &db.Result{Columns: []db.ResultColumn{{Name: "a"}}, Rows: rows, HasRows: true}, "csv")
	h.want("Only the first 100000 rows are exported")
	h.press("esc")
	h.a.ws().exportResultForm("panel", &db.Result{Columns: []db.ResultColumn{{Name: "a"}}, Rows: rows[:3], HasRows: true}, "csv")
	h.wantNot("Only the first")
}

// P2-77: saving from the form warns before it drops the file's comments.
func TestM16SettingsSaveWarnsAboutComments(t *testing.T) {
	h := newHarness(t, nil)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("# my notes\n[general]\nkeep_alive = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.a.d.CfgPath = path
	h.a.run("settings")
	h.press("/")
	h.typeText("general.keep_alive")
	h.press("enter")
	h.press("enter")
	h.press("s")
	h.want("does not keep comments")
	h.press("n")
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "# my notes") {
		t.Fatalf("the comment is gone after no: %s", b)
	}
	h.press("s")
	h.press("y")
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "# my notes") {
		t.Errorf("expected the rewrite after yes: %s", b)
	}
}
