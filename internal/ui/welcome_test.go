package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
)

// The welcome screen never renders a line wider than the terminal, at any size.
func TestWelcomeFitsTerminal(t *testing.T) {
	h := newHarness(t, nil)
	h.a.d.Conns = append(h.a.d.Conns, config.Connection{Name: "clickhouse.cloudstg.svc.cluster.local", Driver: config.ClickHouse, Host: "127.0.0.1", Port: 9000, Tag: "staging"},
		config.Connection{Name: "localhost/cloudstg", Driver: config.Mongo, Host: "localhost", Port: 27017, Tag: "staging"})
	for _, wh := range [][2]int{{192, 48}, {120, 36}, {80, 24}, {60, 20}} {
		h.a.Update(tea.WindowSizeMsg{Width: wh[0], Height: wh[1]})
		lines := strings.Split(h.screen(), "\n")
		if len(lines) != wh[1] {
			t.Fatalf("%v: %d lines", wh, len(lines))
		}
		for i, l := range lines {
			if width(l) != wh[0] {
				t.Errorf("%v line %d is %d wide: %q", wh, i, width(l), l)
			}
		}
		if wh[0] >= 132 && !strings.Contains(h.screen(), "│ClickHouse │") {
			t.Errorf("%v: ClickHouse tile should show just the name", wh)
		}
	}
}

func TestWelcomeEscKeepsTheHighlightedConnection(t *testing.T) {
	h := newHarness(t, nil)
	h.a.d.Conns = []config.Connection{
		{Name: "a1", Driver: config.Postgres, Host: "h"}, {Name: "b1", Driver: config.Postgres, Host: "h"},
		{Name: "a2", Driver: config.Postgres, Host: "h"}, {Name: "b2", Driver: config.Postgres, Host: "h"},
	}
	h.press("/")
	h.typeText("b")
	h.press("down", "esc")
	if r, ok := h.a.welcome.current(); !ok || r.conn.Name != "b2" {
		t.Fatalf("after Esc the cursor is on %+v", r.conn.Name)
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

func TestConnectionFormSavesEncoding(t *testing.T) {
	h := newHarness(t, nil)
	h.a.welcome.form(h.a.d.Conns[0], 0)
	setField(t, h, "encoding", "latin1")
	h.press("enter")
	if got := h.a.d.Conns[0].Encoding; got != "latin1" {
		t.Fatalf("encoding %q\n%s", got, h.screen())
	}
}

func TestM5SourceConnectionsAreReadOnly(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.Source = "/srv/team.toml" })
	h.want("source")
	h.press("e")
	h.wantNot("Edit connection")
	h.press("d")
	if len(h.a.overlays) != 0 {
		t.Fatal("a source connection cannot be deleted")
	}
	h.press("c") // duplicate = local copy
	if saved, _ := config.LoadConnections(h.a.d.ConnPath); len(saved) != 1 || saved[0].Source != "" {
		t.Fatalf("only the copy is saved: %+v", saved)
	}
}

func TestM6SSHURLFillsTheForm(t *testing.T) {
	h := newHarness(t, nil)
	h.press("i")
	setField(t, h, "url", "postgresql+ssh://u:p@db.internal:5432/app?ssh_host=jump.example.com&ssh_user=bob&ssh_key=/k&ssh_port=2222&tLSMode=verify-ca")
	h.press("enter")
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	v := f.values()
	if v["ssh"] != "true" || v["ssh_host"] != "jump.example.com" || v["ssh_user"] != "bob" || v["ssh_key"] != "/k" || v["ssh_port"] != "2222" || v["ssl_mode"] != "verify-ca" || v["host"] != "db.internal" {
		t.Fatalf("form %+v", v)
	}
}

func TestM8MultiSelectDeleteAndExport(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.run("mark") // customers, then the cursor steps to orders
	h.run("mark")
	if got := h.a.ws().sb.markedRefs(); len(got) != 2 {
		t.Fatalf("marked %v", got)
	}
	h.want("✓")
	h.run("delete_item")
	h.want("Delete 2 tables?")
	setField(t, h, "confirm", "2")
	h.press("enter")
	if len(h.fake.applied) != 1 || len(h.fake.applied[0]) != 2 || h.fake.applied[0][0].Text != "-- drop customers" || h.fake.applied[0][1].Text != "-- drop orders" {
		t.Fatalf("applied %+v", h.fake.applied)
	}
	if len(h.a.ws().sb.marks) != 0 {
		t.Fatal("marks clear after the action")
	}
}

// T-P3-05: Esc on a changed connection form asks first; an unchanged one closes at once.
func TestFlowConnectionFormAsksBeforeDiscarding(t *testing.T) {
	h := newHarness(t, nil)
	h.press("n")
	h.press("esc")
	if len(h.a.overlays) != 0 {
		t.Fatal("an unchanged form closes without asking")
	}
	h.press("n")
	h.typeText("zz")
	h.press("esc")
	h.want("Discard changes to this connection?")
	h.press("n") // keep editing
	if f, ok := h.a.overlays[len(h.a.overlays)-1].(*formDlg); !ok || f.values()["name"] != "zz" {
		t.Fatal("cancelling the discard must bring the form back with the edits")
	}
	h.press("esc", "y")
	if len(h.a.overlays) != 0 || len(h.a.d.Conns) != 1 {
		t.Fatalf("discard: overlays %d conns %d", len(h.a.overlays), len(h.a.d.Conns))
	}
}
