package ui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

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
