package ui

import (
	"strings"
	"testing"
	"time"

	"webtuidb/internal/store"
)

func TestM9ReformatStatementUnderCursor(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.run("new_query_tab")
	q := h.queryTab()
	q.ed.Replace("select 1 from a;\nselect 2 from b;")
	q.ed.SelectBytes(20, 20) // inside the second statement, nothing selected
	h.pump(q.reformat(true))
	got := q.ed.Text()
	if !strings.HasPrefix(got, "select 1 from a;\n") {
		t.Fatalf("first statement touched: %q", got)
	}
	if rest := got[len("select 1 from a;\n"):]; rest == "select 2 from b;" {
		t.Fatalf("second statement not formatted: %q", got)
	}
}

func TestM9QueryRenameAndInsert(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	h.run("new_query_tab")
	q, err := h.a.d.Queries.Save("", "old", nil, "select 9")
	if err != nil {
		t.Fatal(err)
	}
	h.pump(w.sb.renameQuery(q))
	h.typeText("2")
	h.press("enter")
	list, _ := h.a.d.Queries.List()
	if len(list) != 1 || list[0].Name != "old2" || list[0].SQL != "select 9" {
		t.Fatalf("after rename: %+v", list)
	}
	h.pump(w.insertIntoEditor("select 7"))
	if !strings.Contains(h.queryTab().ed.Text(), "select 7") {
		t.Fatal("not inserted")
	}
}

func TestM9HistoryMenuClear(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	w.a.d.Hist.Append(w.conn.Name, store.HistoryEntry{SQL: "select 5"})
	e := store.HistoryEntry{SQL: "select 5"}
	h.pump(w.sb.historyMenu(e))
	h.pickLabel("Clear all…")
	h.a.overlays[len(h.a.overlays)-1].(*confirmDlg).armed = time.Time{}
	h.press("y")
	if all, _ := w.a.d.Hist.List(w.conn.Name, "", 0); len(all) != 0 {
		t.Fatalf("history left: %+v", all)
	}
}
