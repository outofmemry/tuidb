package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/db"
)

// T-P1-04: the "taking too long" prompt closes when the statement finishes, and its result is shown.
func TestFlowSlowPromptClosesWhenDone(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	_ = q.start([]db.Stmt{{Text: "select 1"}}) // not pumped: the statement "runs" until we deliver its result
	h.pump(q.Handle(queryTimeoutMsg{batch: q.batch}))
	h.want("taking too long")
	w := h.a.ws()
	h.a.Update(wsMsg{ws: w.id, tab: q.ID(), gen: w.gen, msg: queryDoneMsg{batch: q.batch, stmt: db.Stmt{Text: "select 1"}, res: &db.Result{Affected: 1}}})
	if len(h.a.overlays) != 0 || q.running || len(q.res) != 1 || q.res[0].isErr {
		t.Fatalf("prompt must close and the result show: overlays=%d running=%v res=%d", len(h.a.overlays), q.running, len(q.res))
	}
	h.wantNot("taking too long")
	h.wantNot("cancelled")
}

// T-P2-11: a tab opened from a .sql file shows ● when edited and asks to save on close.
func TestFlowFileTabAsksToSave(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	path := filepath.Join(t.TempDir(), "q1.sql")
	if err := os.WriteFile(path, []byte("select 1;"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.pump(h.a.openSQLFile(path))
	q := h.queryTab()
	if strings.Contains(q.Title(), "●") {
		t.Fatal("an unchanged file is not marked")
	}
	h.typeText(" -- edited")
	if !strings.Contains(q.Title(), "●") {
		t.Fatalf("an edited file is marked: %q", q.Title())
	}
	h.prefix("w")
	h.want("Save changes to q1.sql?")
	h.pickLabel("Save")
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "-- edited") || len(h.a.ws().tabs) != 0 {
		t.Fatalf("save and close: file %q tabs %d", b, len(h.a.ws().tabs))
	}
}
