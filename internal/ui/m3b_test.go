package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/mcp"
)

func TestMCPApprovalSheet(t *testing.T) {
	for _, c := range []struct {
		key  string
		want bool
	}{{"y", true}, {"esc", false}, {"enter", false}} { // a reflexive Enter must not approve
		h := newHarness(t, nil)
		h.a.mcpUI = make(chan func() tea.Cmd)
		got := make(chan bool, 1)
		go func() {
			ok, _ := h.a.approve(context.Background(), mcp.Approval{Token: "cli", Connection: "shop", SQL: "DELETE FROM t"})
			got <- ok
		}()
		h.a.Update(liveMsg{<-h.a.mcpUI})
		h.want("MCP wants to run a statement")
		h.want("DELETE FROM t")
		h.press("y") // inside the 700 ms window nothing answers
		select {
		case <-got:
			t.Fatal("a key right after the sheet opened answered it")
		default:
		}
		time.Sleep(750 * time.Millisecond)
		h.press(c.key)
		if ok := <-got; ok != c.want {
			t.Errorf("%s: approved=%v", c.key, ok)
		}
	}
}

func TestEditorFoldHidesLinesAndPersists(t *testing.T) {
	e := newEd()
	e.SetText("select 1,\n 2,\n 3;\nselect 4")
	var saved []int
	e.OnFolds = func(f []int) { saved = f }
	e.cur = pos{1, 0}
	e.ToggleFold()
	if e.cur.row != 0 || len(saved) != 1 || saved[0] != 0 {
		t.Fatalf("cur=%v saved=%v", e.cur, saved)
	}
	e.W, e.H = 40, 5
	v := stripANSI(e.View(NewTheme(config.Defaults().Theme), true))
	if !strings.Contains(v, "… (2 lines)") || strings.Contains(v, " 3;") {
		t.Errorf("folded view:\n%s", v)
	}
	e.move(1, 0, false) // the hidden lines are skipped
	if e.cur.row != 3 {
		t.Errorf("cursor on %d", e.cur.row)
	}
	e2 := newEd()
	e2.SetText(e.Text())
	e2.SetFolded(saved)
	if len(e2.Folded()) != 1 {
		t.Error("folds not restored")
	}
	e2.Insert("x") // shortcut: an edit unfolds
	if len(e2.Folded()) != 0 {
		t.Error("edit should unfold")
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
