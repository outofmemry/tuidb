// results.go: The result pane of a query tab: loading more rows, result-only view, saving a result, summaries.

package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

func (t *queryTab) resultOnly(name string) tea.Cmd {
	rv := t.curResult()
	if rv == nil || !t.focusResults {
		return t.ws.a.say("focus the results first")
	}
	tbl := "result"
	if rv.ref != nil {
		tbl = rv.ref.Name
	} else if _, table, ok := editableTable(rv.stmt, t.ws.caps.Dialect); ok { // a single-table SELECT without its key (T-P3-19)
		tbl = table
	}
	switch name {
	case "chart":
		if rv.raw == nil {
			return t.ws.a.say("nothing to chart: this result has no rows")
		}
		rv.chart = !rv.chart
	case "load_more":
		return t.loadMore(rv)
	case "column_menu":
		return t.resultColumnMenu(rv)
	case "copy_rows", "copy_cells", "copy_rows_as", "copy_cells_as", "copy_column":
		g := rv.grid
		if g == nil {
			return t.ws.a.say("nothing to copy: this result has no rows")
		}
		switch name {
		case "copy_column":
			return t.copyOut(g.CopyColumn(), "column")
		case "copy_cells_as":
			t.ws.a.pushCopyAs("Copy selected cells as", CopyFormats, func(f CopyFormat) tea.Cmd { return t.copyOut(g.CopyCells(f, tbl), "cells") })
			return nil
		case "copy_rows":
			return t.copyOut(g.CopyRows(FmtCSV, tbl), "rows")
		case "copy_cells":
			return t.copyOut(g.CopyCells(FmtPlain, tbl), "cells")
		}
		t.ws.a.pushCopyAs("Copy rows as", CopyFormats, func(f CopyFormat) tea.Cmd { return t.copyOut(g.CopyRows(f, tbl), "rows") })
	}
	return nil
}

// loadMore re-runs the statement with a doubled limit and swaps the result in place.
// shortcut: re-reads from row 1 (a server cursor would continue where it stopped); read-only statements only,
// because running an INSERT or UPDATE again to get more rows would repeat its effect.
func (t *queryTab) loadMore(rv *resultView) tea.Cmd {
	cur := max(rv.limit, t.limit)
	switch {
	case !rv.trunc:
		return t.ws.a.say("this result is complete")
	case t.running:
		return t.ws.a.say("a query is already running; cancel it first")
	case !db.IsReadOnly(t.ws.caps.Dialect, rv.stmt):
		return t.ws.a.say("load more only re-runs read-only statements")
	}
	for _, r := range t.res { // the run replaces the grid in place: ask before dropping uncommitted edits
		if r == rv && r.grid != nil && r.grid.HasPending() {
			t.ws.a.push(&confirmDlg{title: "Discard pending result edits?", lines: []string{"Loading more replaces the result; uncommitted edits in it will be lost."}, yes: "Discard", danger: true, onYes: func() tea.Cmd {
				rv.grid.DiscardPending()
				return t.loadMore(rv)
			}})
			return nil
		}
	}
	t.override, t.replace = cur*2, t.rc
	t.running, t.queue, t.batch, t.started = true, []db.Stmt{{Text: rv.stmt}}, t.batch+1, time.Now()
	return t.next()
}

// saveMessage writes the Message view (notices and status lines) to a text file.
func (t *queryTab) saveMessage(rv *resultView) tea.Cmd {
	t.ws.a.push(newForm("Save message as text", []formField{{Key: "path", Label: "File", Kind: fText, Value: "message.txt"}}, func(v map[string]string) tea.Cmd {
		path := strings.TrimSpace(v["path"])
		if path == "" {
			return t.ws.a.fail(fmt.Errorf("file name required"))
		}
		return t.ws.a.ifFree(func() tea.Cmd {
			if err := writeFile(path, rv.msg+"\n"); err != nil {
				return t.ws.a.fail(err)
			}
			return t.ws.a.say("saved %s", path)
		}, path)
	}))
	return nil
}

// sortOrSave is S: the order of saved queries, or Save As… on the history tab.
func (s *sidebar) sortOrSave() tea.Cmd {
	switch s.tab {
	case 1:
		var items []pickItem
		for _, by := range []string{"name", "created", "modified"} {
			items = append(items, pickItem{Label: by, Value: by})
		}
		s.w.a.push(&pickerDlg{title: "Sort saved queries by", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
			s.qsort, s.sel, s.top = it.Value.(string), 0, 0
			return nil
		}})
	case 2:
		return s.saveHistory()
	}
	return nil
}

// summaryOver: a run of more than this many statements keeps a summary instead of a tab per statement.
const summaryOver = 100

type batchSummary struct {
	total, done, fail int
	rows, affected    int64
	firstErr          string
}

// summaryStep counts one finished statement of a summarised run; the last one produces the "Summary" tab.
func (t *queryTab) summaryStep(m queryDoneMsg) tea.Cmd {
	s := t.sum
	s.done++
	switch {
	case m.err != nil:
		s.fail++
		if s.firstErr == "" {
			s.firstErr = fmt.Sprintf("statement %d: %v", s.done, m.err)
		}
	case m.res != nil:
		s.rows += int64(len(m.res.Rows))
		s.affected += m.res.Affected
	}
	if m.err != nil && t.ws.a.d.Cfg.Editor.ReturnOnError {
		t.queue = nil
	}
	t.status = tookStr(time.Since(t.started))
	if len(t.queue) == 0 { // the last one: now the tab
		lines := []string{fmt.Sprintf("Statements run: %d of %d", s.done, s.total), fmt.Sprintf("Failed: %d", s.fail),
			fmt.Sprintf("Rows returned: %d", s.rows), fmt.Sprintf("Rows affected: %d", s.affected)}
		if s.firstErr != "" {
			lines = append(lines, "First failure: "+s.firstErr)
		}
		t.res = append(t.res, &resultView{title: "Summary", msg: strings.Join(lines, "\n"), took: time.Since(t.started)})
		t.rc = len(t.res) - 1
	}
	return t.next()
}
