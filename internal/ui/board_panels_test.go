package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/board"
	"webtuidb/internal/db"
)

func TestM13ExporterPanelExportsItsRows(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	err := board.Save(board.Board{Name: "ops", Panels: []board.Panel{
		{Title: "Rows", Kind: "exporter", Query: "select * from customers", Format: "json"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, nil)
	h.connect()
	h.run("metrics")
	h.press("enter")
	h.want("export json")
	h.press("enter")
	h.want("Export Rows")
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	if f.values()["format"] != "json" {
		t.Fatalf("format not preselected: %v", f.values())
	}
	out := filepath.Join(t.TempDir(), "rows.json")
	setField(t, h, "path", out)
	h.wait = time.Second
	h.press("enter")
	b, err := os.ReadFile(out)
	if err != nil || !strings.Contains(string(b), "Ann") {
		t.Fatalf("export file %q: %v", b, err)
	}
}

func TestM13InputPanelSetsItsVariable(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	err := board.Save(board.Board{Name: "ops", Panels: []board.Panel{
		{Title: "Who", Kind: "input", Var: "who", Default: "x"},
		{Title: "People", Kind: "table", Query: "select * from customers where name = '$who'"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, nil)
	h.connect()
	h.run("metrics")
	h.press("enter")
	h.want("$who = x")
	h.press("enter")
	setField(t, h, "v", "ann")
	h.press("enter")
	h.want("$who = ann")
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
