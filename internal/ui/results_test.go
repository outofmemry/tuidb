package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func TestM2AppendModeKeepsEarlierResults(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.Editor.ResultMode = "append" })
	h.connect()
	h.prefix("e")
	h.typeText("select 1")
	h.prefix("alt+enter")
	h.prefix("alt+enter")
	if n := len(h.a.ws().tab().(*queryTab).res); n != 2 {
		t.Fatalf("results %d", n)
	}
}

func TestM4MessageViewSavesText(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	queryResult(h, "raise")
	h.press("m", "s")
	out := filepath.Join(t.TempDir(), "m.txt")
	setField(t, h, "path", out)
	h.press("enter")
	if b, _ := os.ReadFile(out); !strings.Contains(string(b), "second notice") {
		t.Fatalf("saved %q", b)
	}
}

func TestQueryRerunWithPinnedResultKeepsIndexInRange(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.a.ws().tab().(*queryTab)
	q.res = []*resultView{{pinned: true}}
	q.override = 100
	q.start([]db.Stmt{{Text: "select 1"}})
	if q.rc != 0 || q.rc >= len(q.res) || q.override != 0 {
		t.Fatalf("rc %d res %d override %d", q.rc, len(q.res), q.override)
	}
	q.resultsView(h.a.th, 40, 10) // used to index t.res[len(t.res)]
}

func TestM8LongRunKeepsOneSummaryTab(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	q.ed.SetText(strings.Repeat("select 1;\n", 101) + "boom;")
	h.run("run_all")
	if len(q.res) != 1 || q.res[0].title != "Summary" {
		t.Fatalf("results %d", len(q.res))
	}
	msg := q.res[0].msg
	if !strings.Contains(msg, "Statements run: 102 of 102") || !strings.Contains(msg, "Failed: 1") || !strings.Contains(msg, "Rows returned: 202") || !strings.Contains(msg, "First failure: statement 102") {
		t.Fatalf("summary %q", msg)
	}
	h.want("1 of 102 failed")
}

func TestM8LabelAndTabNameTheResultTabs(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	q.ed.SetText("-- label: Mine\nselect 1;\n-- tab: Yours\nselect 2;\n-- name: Ours\nselect 3;")
	h.run("run_all")
	var titles []string
	for _, r := range q.res {
		titles = append(titles, r.title)
	}
	if strings.Join(titles, ",") != "Mine,Yours,Ours" {
		t.Fatalf("titles %v", titles)
	}
}
