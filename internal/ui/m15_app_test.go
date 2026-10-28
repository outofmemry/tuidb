package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/menus"
	"webtuidb/internal/store"
)

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

// M14.4: the sidebar object menu is menus.SidebarObject filtered by kind and capabilities.
func TestM15SidebarMenuFromMenus(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	for _, triggers := range []bool{true, false} {
		w.caps.Triggers = triggers
		h.press("m")
		got := h.menuLabels()
		want := menus.Labels(menus.SidebarObject, menus.Env{Kind: "table", Caps: capsMap(w.caps)})
		if !sameLabels(got, want) {
			t.Errorf("triggers=%v:\n got %v\nwant %v", triggers, got, want)
		}
		has := strings.Contains(strings.Join(got, "|"), "Triggers…")
		if has != triggers {
			t.Errorf("triggers=%v but the menu has Triggers…: %v", triggers, has)
		}
		for _, l := range []string{"Export…", "Import into this table…", "Mark / unmark", "Set colour…"} {
			if !strings.Contains(strings.Join(got, "|"), l) {
				t.Errorf("menu lacks %q", l)
			}
		}
		h.press("esc")
	}
}

func TestM15QueryHistoryNewDatabaseTabMenus(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	q, err := h.a.d.Queries.Save("", "top", nil, "select 1")
	if err != nil {
		t.Fatal(err)
	}
	w.sb.queryMenu(q)
	if got, want := h.menuLabels(), menus.Labels(menus.SidebarQuery, menus.Env{Kind: "query"}); !sameLabels(got, want) {
		t.Errorf("query menu %v want %v", got, want)
	}
	h.press("esc")
	w.sb.historyMenu(store.HistoryEntry{SQL: "select 2"})
	if got, want := h.menuLabels(), menus.Labels(menus.SidebarHistory, menus.Env{Kind: "history"}); !sameLabels(got, want) {
		t.Errorf("history menu %v want %v", got, want)
	}
	h.press("esc")
	w.sb.newMenu()
	if got, want := h.menuLabels(), menus.Labels(menus.SidebarNew, menus.Env{Caps: capsMap(w.caps)}); !sameLabels(got, want) {
		t.Errorf("+ menu %v want %v", got, want)
	}
	h.press("esc")
	w.sb.databaseMenu("shop")
	if got, want := h.menuLabels(), menus.Labels(menus.Database, menus.Env{Kind: "database"}); !sameLabels(got, want) {
		t.Errorf("database menu %v want %v", got, want)
	}
	h.press("esc")
	h.openCustomers()
	w.tabMenu(0)
	got := h.menuLabels()
	if want := menus.Labels(menus.Tab, menus.Env{}); !sameLabels(got, want) {
		t.Errorf("tab menu %v want %v", got, want)
	}
	// the web-only entry is listed, dimmed, with the reason; choosing it only explains
	p := h.a.overlays[len(h.a.overlays)-1].(*pickerDlg)
	last := p.items[len(p.items)-1]
	if !last.Dim || !strings.Contains(last.Detail, "web only") {
		t.Errorf("web-only entry: %+v", last)
	}
	h.press("esc")
	h.a.overlays = nil
	h.a.ws().focusSB = true
	h.a.welcome.sel = 0
	h.a.inWS = false
	h.press("m")
	if got, want := h.menuLabels(), menus.Labels(menus.Welcome, menus.Env{}); !sameLabels(got, want) {
		t.Errorf("welcome menu %v want %v", got, want)
	}
}

func TestM15CreateTemplates(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	s := h.a.ws().sb
	for _, kind := range []string{"view", "matview", "function"} {
		if txt := s.createTemplate(kind); !strings.Contains(strings.ToUpper(txt), "CREATE") || !strings.Contains(txt, "new_") {
			t.Errorf("%s template: %q", kind, txt)
		}
	}
	s.w.caps.Dialect = "postgres"
	if txt := s.createTemplate("matview"); !strings.Contains(txt, "MATERIALIZED VIEW public.new_matview") {
		t.Errorf("postgres matview: %q", txt)
	}
	s.w.caps.Dialect = "mysql"
	if txt := s.createTemplate("function"); !strings.Contains(txt, "CREATE FUNCTION") {
		t.Errorf("mysql function: %q", txt)
	}
}

func TestM15MoveTab(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.a.run("new_query_tab")
	w := h.a.ws()
	if len(w.tabs) != 2 {
		t.Fatalf("%d tabs", len(w.tabs))
	}
	first := w.tabs[0]
	h.a.run("move_tab_left")
	if w.tabs[1] != first || w.cur != 0 {
		t.Errorf("after move left: cur=%d tabs=%v", w.cur, w.tabs)
	}
	h.a.run("move_tab_right")
	if w.tabs[0] != first || w.cur != 1 {
		t.Errorf("after move right: cur=%d", w.cur)
	}
	h.a.run("move_tab_right") // already last
	if w.cur != 1 || !strings.Contains(h.a.toast, "last") {
		t.Errorf("edge: cur=%d toast=%q", w.cur, h.a.toast)
	}
}

func TestM15TogglePanels(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	w.showConsole, w.showDetail = true, false
	h.want("Console")
	h.a.run("toggle_panels")
	if w.showSB || w.showConsole || w.showDetail || !w.zen {
		t.Fatalf("zen did not hide: %+v", w)
	}
	h.wantNot("Console")
	if lines := strings.Split(h.screen(), "\n"); len(lines) != h.a.h {
		t.Errorf("zen body must fill the screen: %d lines of %d", len(lines), h.a.h)
	}
	h.a.run("toggle_panels")
	if !w.showSB || !w.showConsole || w.showDetail || w.zen {
		t.Fatalf("zen did not restore: sb=%v console=%v detail=%v", w.showSB, w.showConsole, w.showDetail)
	}
	h.want("Console")
}

func TestM15JobsCentre(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	w.jobFile = filepath.Join(t.TempDir(), "out.csv")
	cmd := w.job("export out.csv", func(ctx context.Context, _ db.Session, report func(func() tea.Cmd)) error {
		<-ctx.Done() // runs until stopped
		return ctx.Err()
	}, func() (string, tea.Cmd) { return "done", nil })
	h.a.run("jobs")
	h.want("Background jobs")
	h.want("export out.csv")
	h.want("running")
	h.want("out.csv") // the selected row shows the file
	if len(w.jobs) != 1 || w.jobs[0].kind != "export" || w.jobs[0].file == "" {
		t.Fatalf("jobs: %+v", w.jobs)
	}
	h.press("s") // stop
	h.pump(cmd)  // the job's goroutine reports back
	if !w.jobs[0].done || !w.jobs[0].stopped {
		t.Fatalf("job not stopped: %+v", w.jobs[0])
	}
	h.want("stopped")
	h.press("d") // dismiss
	if len(w.jobs) != 0 {
		t.Errorf("dismiss left %d jobs", len(w.jobs))
	}
	h.want("no jobs yet")
}

func TestM15JobKinds(t *testing.T) {
	for label, kind := range map[string]string{"export a.csv": "export", "import b": "import", "backup x": "backup", "restore x": "restore", "search foo": "search", "schema diff": "diff", "data diff": "diff"} {
		if got := jobKind(label); got != kind {
			t.Errorf("jobKind(%q) = %q, want %q", label, got, kind)
		}
	}
}

func TestM15FinishSignalsOnlyWhenBlurred(t *testing.T) {
	h := newHarness(t, nil)
	if h.a.finishSignal("x") != nil {
		t.Fatal("a focused terminal needs no signal")
	}
	h.a.Update(tea.BlurMsg{})
	cmd := h.a.finishSignal("shop export a.csv: finished")
	if cmd == nil {
		t.Fatal("blurred: expected a signal")
	}
	raw, ok := cmd().(tea.RawMsg)
	if !ok {
		t.Fatalf("not a raw write: %T", cmd())
	}
	s, _ := raw.Msg.(string)
	for _, want := range []string{"\a", "\x1b]9;shop export a.csv: finished\x07", "\x1b]777;notify;webtuidb;shop export a.csv: finished\x07"} {
		if !strings.Contains(s, want) {
			t.Errorf("signal lacks %q: %q", want, s)
		}
	}
	h.a.Update(tea.FocusMsg{})
	if h.a.finishSignal("x") != nil {
		t.Error("focused again: no signal")
	}
}

func lockHarness(t *testing.T) *harness {
	hash, err := config.HashPasscode("1234")
	if err != nil {
		t.Fatal(err)
	}
	return newHarness(t, func(_ *config.Connection, c *config.Config) { c.Security.Passcode, c.Security.LockAfterMins = hash, 5 })
}

func TestM15PasscodeLock(t *testing.T) {
	h := lockHarness(t)
	h.a.Init() // starts locked
	if !h.a.locked() {
		t.Fatal("a passcode is set: the app starts locked")
	}
	h.press("enter") // would connect without the lock
	if h.a.ws() != nil || len(h.connected) != 0 {
		t.Fatal("keys must not reach the app while locked")
	}
	h.want("Locked")
	h.wantNot("shop") // the connection list is covered
	h.typeText("0000")
	h.press("enter")
	h.want("wrong passcode")
	if !h.a.locked() {
		t.Fatal("wrong passcode unlocked")
	}
	h.typeText("1234")
	h.press("enter")
	if h.a.locked() {
		t.Fatal("right passcode did not unlock")
	}
	h.want("shop")
}

func TestM15PasscodeThreeTriesDelay(t *testing.T) {
	h := lockHarness(t)
	h.a.run("lock")
	l := h.a.overlays[0].(*lockDlg)
	clock := time.Now()
	l.now = func() time.Time { return clock }
	for i := 0; i < 3; i++ {
		h.typeText("9")
		h.press("enter")
	}
	h.want("wait 10 s")
	h.typeText("1234")
	h.press("enter")
	if !h.a.locked() {
		t.Fatal("the right passcode must wait out the delay")
	}
	clock = clock.Add(11 * time.Second)
	h.typeText("1234")
	h.press("enter")
	if h.a.locked() {
		t.Fatal("after the delay the passcode works")
	}
}

func TestM15IdleLock(t *testing.T) {
	h := lockHarness(t)
	h.a.m15.lastKey = time.Now()
	if h.a.idleCheck(time.Now().Add(4*time.Minute)) != nil || h.a.locked() {
		t.Fatal("not idle long enough")
	}
	h.a.idleCheck(time.Now().Add(6 * time.Minute))
	if !h.a.locked() {
		t.Fatal("lock_after_mins passed: expected the lock")
	}
}

func TestM15LockWithoutPasscodeExplains(t *testing.T) {
	h := newHarness(t, nil)
	h.a.run("lock")
	if h.a.locked() || !strings.Contains(h.a.toast, "-set-passcode") {
		t.Errorf("toast %q locked=%v", h.a.toast, h.a.locked())
	}
}

func TestM15MouseSelectsTab(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	h.a.run("new_query_tab")
	w := h.a.ws()
	if w.cur != 1 {
		t.Fatalf("cur %d", w.cur)
	}
	sbW, _, _, _, _ := layoutOf(w.showSB, w.showDetail, w.showConsole, h.a.w, h.a.bodyH())
	x := sbW + 1 + 3 + 2 // inside the first label
	h.a.Update(tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseLeft})
	if w.cur != 0 {
		t.Fatalf("clicking the first tab left cur=%d", w.cur)
	}
	// right click opens the tab menu
	h.a.Update(tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseRight})
	if got, want := h.menuLabels(), menus.Labels(menus.Tab, menus.Env{}); !sameLabels(got, want) {
		t.Errorf("right click menu %v", got)
	}
}

func TestM15MouseSidebarAndGrid(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	rows := w.sb.rows()
	if len(rows) < 3 {
		t.Fatalf("rows %d", len(rows))
	}
	w.focusSB = false
	// a click on the sidebar's third visible row selects it and gives the sidebar the focus
	h.a.Update(tea.MouseClickMsg{X: 4, Y: 1 + 1 + 2 + 2, Button: tea.MouseLeft})
	if !w.focusSB || w.sb.sel != 2 {
		t.Errorf("sidebar click: focus=%v sel=%d", w.focusSB, w.sb.sel)
	}
	w.sb.sel = 1 // customers
	h.press("enter")
	h.want("ann@x.io")
	t0 := w.tab().(*tableTab)
	sbW, _, _, _, _ := layoutOf(w.showSB, w.showDetail, w.showConsole, h.a.w, h.a.bodyH())
	gx := sbW + 1 + 2 // grid column 0
	// row 3 of the grid (header is the first line under the tab bar and the filter bar)
	h.a.Update(tea.MouseClickMsg{X: gx + t0.grid.gutterW() + 12, Y: 1 + 1 + t0.barLines() + 1 + 2, Button: tea.MouseLeft})
	if t0.grid.row != 2 {
		t.Errorf("grid click: row=%d", t0.grid.row)
	}
}

func TestM15HeaderClickSortsAndDragResizes(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	w := h.a.ws()
	t0 := w.tab().(*tableTab)
	sbW, _, _, _, _ := layoutOf(w.showSB, w.showDetail, w.showConsole, h.a.w, h.a.bodyH())
	gx := sbW + 1 + 2
	hy := 1 + 1 + t0.barLines() // top bar, frame edge, filter bar
	col1 := t0.grid.gutterW() + 1 + t0.grid.Cols[0].Width + 1 + 2
	h.a.Update(tea.MouseClickMsg{X: gx + col1, Y: hy, Button: tea.MouseLeft})
	if len(t0.sort) != 1 || t0.sort[0].Column != t0.grid.Cols[1].Name {
		t.Errorf("header click sort: %+v", t0.sort)
	}
	// drag the right edge of column 0
	edge := gx + t0.grid.gutterW() + 1 + t0.grid.Cols[0].Width
	before := t0.grid.Cols[0].Width
	h.a.Update(tea.MouseClickMsg{X: edge, Y: hy, Button: tea.MouseLeft})
	h.a.Update(tea.MouseMotionMsg{X: edge + 3, Y: hy, Button: tea.MouseLeft})
	h.a.Update(tea.MouseReleaseMsg{X: edge + 3, Y: hy, Button: tea.MouseLeft})
	if got := t0.grid.Cols[0].Width; got != before+3 {
		t.Errorf("drag: width %d -> %d", before, got)
	}
	if got := h.a.d.State.Table(t0.stateKey()).ColWidths[t0.grid.Cols[0].Name]; got != before+3 {
		t.Errorf("width not saved: %d", got)
	}
}

func TestM15Hints(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.General.Hints = []string{"commit", "preview"} })
	h.connect()
	h.openCustomers()
	h.a.toast = ""
	s := h.screen()
	status := s[strings.LastIndex(s, "\n")+1:]
	if !strings.Contains(status, "commit") || !strings.Contains(status, "preview") {
		t.Errorf("status line lacks the configured hints: %q", status)
	}
	if strings.Contains(status, "help") || strings.Contains(status, "filter") {
		t.Errorf("status line shows more than the configured hints: %q", status)
	}
}

func TestM15ProductionCue(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.Tag = "production" })
	h.connect()
	w := h.a.ws()
	if w.cue() == "" {
		t.Fatal("production workspaces get a cue colour")
	}
	if got := w.frame(h.a.th, "t", "", "x", 12, 3, true); !strings.Contains(got, "\x1b[") {
		t.Error("the frame border is not coloured")
	}
	w.conn.Tag = "development"
	if w.cue() != "" {
		t.Error("development has no cue")
	}
}

func TestM15LastConnectedAndRecentSort(t *testing.T) {
	h := newHarness(t, nil)
	h.connect() // shop is connected now
	h.a.d.Conns = append(h.a.d.Conns, config.Connection{Name: "alpha", Driver: config.Postgres, Host: "a"})
	if _, ok := h.a.d.State.LastConnected()["shop"]; !ok {
		t.Fatal("connecting must record the time")
	}
	h.a.inWS = false
	h.want("now")
	h.a.d.State.SetConnectionSort("recent")
	var names []string
	for _, r := range h.a.welcome.rows() {
		names = append(names, r.conn.Name)
	}
	if strings.Join(names, ",") != "shop,alpha" {
		t.Errorf("recent order: %v", names)
	}
	h.a.d.State.SetConnectionSort("name")
	names = nil
	for _, r := range h.a.welcome.rows() {
		names = append(names, r.conn.Name)
	}
	if strings.Join(names, ",") != "alpha,shop" {
		t.Errorf("name order: %v", names)
	}
}

func TestM15ConsoleFindCopyErrors(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	w.console.Add("select 1", false, time.Millisecond)
	w.console.Add("select bad", true, time.Millisecond)
	w.console.Add("select 3", false, time.Millisecond)
	w.showConsole = true
	h.a.run("focus_down")
	if !w.focusConsole {
		t.Fatal("focus_down should reach the console")
	}
	h.press("/")
	h.typeText("select 3")
	h.press("enter")
	if ls := w.consoleLines(); w.consoleSel(len(ls)) != 2 {
		t.Errorf("find landed on %d", w.consoleSel(len(ls)))
	}
	h.press("N") // wraps back to the same single match
	h.press("e")
	if ls := w.consoleLines(); len(ls) != 1 || !ls[0].Failed {
		t.Errorf("errors only: %+v", ls)
	}
	h.press("e")
	h.press("y")
	h.press("esc") // clears the find
	h.press("esc") // leaves the pane
	if w.focusConsole {
		t.Error("esc should return the focus")
	}
}

func TestM15SettingsEditorSavesAndReloads(t *testing.T) {
	h := newHarness(t, nil)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[general]\nkeep_alive = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.a.d.CfgPath = path
	h.a.run("settings")
	h.want("Settings")
	h.press("/")
	h.typeText("general.keep_alive")
	h.press("enter")
	h.press("enter") // flips the bool
	h.want("1 unsaved")
	h.press("s")
	h.want("saved 1 setting")
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "keep_alive = false") {
		t.Errorf("config.toml: %s", b)
	}
	if h.a.d.Cfg.General.KeepAlive {
		t.Error("the running config was not reloaded")
	}
	// a value the engine refuses stays in the dialog with its message
	h.press("/")
	h.press("ctrl+u")
	h.typeText("general.row_limit")
	h.press("enter")
	h.press("enter")
	h.press("ctrl+u")
	h.typeText("-5")
	h.press("enter")
	h.press("s")
	if !strings.Contains(h.screen(), "Settings") {
		t.Error("the dialog must stay open after a refused save")
	}
}

func TestM15SettingsKeyConflictShown(t *testing.T) {
	rebind := func(t *testing.T, cfg, key string) (*harness, string) {
		h := newHarness(t, nil)
		path := filepath.Join(t.TempDir(), "config.toml")
		os.WriteFile(path, []byte(cfg), 0o600)
		h.a.d.CfgPath = path
		loaded, err := config.Load(path) // the running config carries the file's [keys] entries
		if err != nil {
			t.Fatal(err)
		}
		h.a.d.Cfg = loaded
		h.a.run("settings")
		h.press("tab")
		h.want("Key bindings")
		h.press("/")
		h.typeText("rename_tab")
		h.press("enter")
		h.press("enter")
		h.want("Key for rename_tab")
		h.press("ctrl+u")
		h.typeText(key)
		h.press("enter")
		return h, path
	}
	// alt+x is cancel_queries' default: the builder would give it away silently (P2-79), so it is refused.
	h, path := rebind(t, "", "alt+x")
	h.want("takes the key from cancel_queries")
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "rename_tab") {
		t.Errorf("a refused binding was written: %s", b)
	}
	// two [keys] entries on one key is a real conflict too
	h, path = rebind(t, "[keys]\nnew_query_tab = \"alt+z\"\n", "alt+z")
	h.want("alt+z")
	h.want("rename_tab")
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "rename_tab") {
		t.Errorf("a clashing binding was written: %s", b)
	}
	// a free key still works
	h, path = rebind(t, "", "ctrl+alt+j")
	h.want("rename_tab rebound")
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "rename_tab") {
		t.Errorf("[keys] not written: %s", b)
	}
}

func TestM15OpenAnythingExtras(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	var opened string
	old := openURL
	openURL = func(u string) error { opened = u; return nil }
	defer func() { openURL = old }()
	h.a.ws().openAnything()
	h.typeText("Open docs")
	h.press("enter")
	if !strings.HasPrefix(opened, "https://github.com/") {
		t.Errorf("docs url %q", opened)
	}
	// Tab completes the keyword with the selected name
	h.a.ws().openAnything()
	h.typeText("cust")
	h.press("tab")
	p := h.a.overlays[len(h.a.overlays)-1].(*pickerDlg)
	if p.q.Value() != "customers" {
		t.Errorf("tab completion: %q", p.q.Value())
	}
}

func TestM15PasswordCmdPreset(t *testing.T) {
	h := newHarness(t, nil)
	h.press("n") // new connection form
	h.typeText("pg")
	h.press("ctrl+o")
	h.want("Password command")
	h.press("enter")
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	for _, fl := range f.fields {
		if fl.Key == "password_cmd" && !strings.Contains(fl.edit.Value(), "security find-generic-password") {
			t.Errorf("preset not filled: %q", fl.edit.Value())
		}
	}
}
