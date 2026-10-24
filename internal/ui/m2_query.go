package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
	"webtuidb/internal/sqlfmt"
)

// ---- query parameters -------------------------------------------------------------------

var paramFormats = []string{"colon", "dollar", "at", "brace", "mustache", "question"}

func (t *queryTab) paramFormat() string { return t.ws.a.d.Cfg.Editor.ParamFormat }

func (t *queryTab) paramNames(stmts []db.Stmt) []string {
	return t.paramNamesAs(stmts, t.paramFormat())
}

func (t *queryTab) paramNamesAs(stmts []db.Stmt, format string) []string {
	var names []string
	seen := map[string]bool{}
	for _, s := range stmts {
		for _, n := range db.Params(s.Text, db.ParamFormat(format), t.ws.caps.Dialect) {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	return names
}

func (t *queryTab) bindAs(stmts []db.Stmt, vals map[string]string, format string) []db.Stmt {
	out := make([]db.Stmt, len(stmts))
	for i, s := range stmts {
		typed := s.Text
		s.Text = db.Bind(s.Text, db.ParamFormat(format), vals, t.ws.caps.Dialect)
		if s.Text != typed {
			if t.unbound == nil {
				t.unbound = map[string]string{}
			}
			t.unbound[s.Text] = typed
		}
		out[i] = s
	}
	return out
}

// typed is the statement as the user wrote it, before variable values were substituted (what history records).
func (t *queryTab) typed(bound string) string {
	if s, ok := t.unbound[bound]; ok {
		return s
	}
	return bound
}

// formatKey names the Variables dialog's format picker among the variable fields.
const formatKey = "\x00format"

// askVars shows one field per parameter, pre-filled with what was typed earlier this session, and a format picker
// that overrides [editor] param_format for this run: picking a format with other variables asks again.
func (t *queryTab) askVars(stmts []db.Stmt, format string, then func(vals map[string]string, format string) tea.Cmd) tea.Cmd {
	names := t.paramNamesAs(stmts, format)
	fields := []formField{{Key: formatKey, Label: "Format", Kind: fChoice, Options: paramFormats, Value: format}}
	for _, n := range names {
		fields = append(fields, formField{Key: n, Label: n, Kind: fText, Value: t.ws.vars[n]})
	}
	f := newForm("Variables", fields, func(v map[string]string) tea.Cmd {
		if t.ws.vars == nil {
			t.ws.vars = map[string]string{}
		}
		for k, x := range v {
			if k != formatKey {
				t.ws.vars[k] = x
			}
		}
		if chosen := v[formatKey]; chosen != format && strings.Join(t.paramNamesAs(stmts, chosen), ",") != strings.Join(names, ",") {
			return t.askVars(stmts, chosen, then) // the other format sees other variables
		}
		return then(v, v[formatKey])
	})
	f.submit = "Enter: run"
	if len(names) > 0 {
		f.idx = 1 // the first variable, not the format picker
	}
	t.ws.a.push(f)
	return nil
}

// editVars is the `variables` action: set the values without running anything.
func (t *queryTab) editVars() tea.Cmd {
	stmts := db.Split(t.ws.caps.Dialect, t.ed.Text())
	format := t.paramFormat()
	for _, f := range append([]string{format}, paramFormats...) { // the configured format first, else whichever finds variables
		if len(t.paramNamesAs(stmts, f)) > 0 {
			format = f
			break
		}
	}
	if len(t.paramNamesAs(stmts, format)) == 0 {
		return t.ws.a.say("no parameters in the editor (format: %s)", t.paramFormat())
	}
	return t.askVars(stmts, format, func(map[string]string, string) tea.Cmd { return t.ws.a.say("variables saved") })
}

// ---- beautify / find --------------------------------------------------------------------

func (t *queryTab) reformat(pretty bool) tea.Cmd {
	if t.ws.caps.Editor != "sql" {
		return t.ws.a.say("beautify works on SQL editors only")
	}
	src := t.ed.Selection()
	if src == "" { // nothing selected: the statement under the cursor, else the whole buffer
		text := t.ed.Text()
		if a, b, ok := db.StatementRangeAt(t.ws.caps.Dialect, text, t.ed.Offset()); ok {
			a += len(text[a:]) - len(strings.TrimLeft(text[a:], " \t\r\n"))
			b = a + len(strings.TrimRight(text[a:b], " \t\r\n"))
			if b > a {
				t.ed.SelectBytes(a, b)
				src = text[a:b]
			}
		}
		if src == "" {
			src = text
		}
	}
	c := t.ws.a.d.Cfg.Editor
	out := sqlfmt.Uglify(src, t.ws.caps.Dialect)
	if pretty {
		out = sqlfmt.Beautify(src, sqlfmt.Options{Indent: c.Indent, Width: c.IndentWidth, Uppercase: c.UppercaseKeywords, Dialect: t.ws.caps.Dialect, IndentType: c.IndentType})
	}
	t.ed.Replace(out)
	return t.afterEdit()
}

func (t *queryTab) findInEditor() tea.Cmd {
	t.ws.a.push(newForm("Find", []formField{{Key: "q", Label: "Text", Kind: fText, Value: t.edFind}}, func(v map[string]string) tea.Cmd {
		t.edFind = v["q"]
		if !t.ed.Find(t.edFind) {
			return t.ws.a.say("%q is not in the editor", t.edFind)
		}
		return nil
	}))
	return nil
}

func (t *queryTab) replaceInEditor() tea.Cmd {
	t.ws.a.push(newForm("Find and replace", []formField{
		{Key: "q", Label: "Find", Kind: fText, Value: t.edFind},
		{Key: "r", Label: "Replace with", Kind: fText},
	}, func(v map[string]string) tea.Cmd {
		t.edFind = v["q"]
		n := t.ed.ReplaceAll(v["q"], v["r"])
		return tea.Batch(t.ws.a.say("replaced %d occurrence(s)", n), t.afterEdit())
	}))
	return nil
}

// ---- results: copy, chart, load more ----------------------------------------------------

func (t *queryTab) resultOnly(name string) tea.Cmd {
	rv := t.curResult()
	if rv == nil || !t.focusResults {
		return t.ws.a.say("focus the results first")
	}
	tbl := "result"
	if rv.ref != nil {
		tbl = rv.ref.Name
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

// chartLines draws the result as text bars scaled to the pane width.
// shortcut: a line chart is drawn as bars too (the web draws a real line); the rows are the series either way.
func chartLines(th Theme, rv *resultView, W, H int) []string {
	spec, _ := db.ParseChart(rv.stmt)
	if spec.Kind == "" {
		spec.Kind = "bar"
	}
	s, err := db.Chart(rv.raw, spec)
	if err != nil {
		return []string{"", "  " + th.Err.Render("✗ "+err.Error())}
	}
	if len(s.Series) > 1 {
		return multiChartLines(th, spec.Kind, s, W, H)
	}
	idx := make([]int, len(s.Values))
	total, top := 0.0, 0.0
	for i, v := range s.Values {
		idx[i], total, top = i, total+max(v, 0), max(top, v)
	}
	if spec.Kind == "pie" {
		sort.SliceStable(idx, func(a, b int) bool { return s.Values[idx[a]] > s.Values[idx[b]] })
	}
	lw, vw := 0, 0
	vals := make([]string, len(idx))
	for k, i := range idx {
		vals[k] = strconv.FormatFloat(s.Values[i], 'f', -1, 64)
		if spec.Kind == "pie" && total > 0 {
			vals[k] += fmt.Sprintf(" (%.0f%%)", 100*max(s.Values[i], 0)/total)
		}
		lw, vw = max(lw, width(s.Labels[i])), max(vw, width(vals[k]))
	}
	lw = min(lw, max(8, W/3))
	bw := max(1, W-lw-vw-6)
	lines := []string{th.Dim.Render(fmt.Sprintf(" %s chart · %d points · g: back to the grid · b: add to a metrics board", spec.Kind, len(idx)))}
	for k, i := range idx {
		if len(lines) >= H-1 && k < len(idx)-1 {
			lines = append(lines, th.Dim.Render(fmt.Sprintf("  … %d more", len(idx)-k)))
			break
		}
		n := 0
		if top > 0 && s.Values[i] > 0 {
			n = max(1, int(s.Values[i]/top*float64(bw)+0.5))
		}
		bar := th.Accent.Render(strings.Repeat("█", n)) + strings.Repeat(" ", bw-n)
		lines = append(lines, "  "+fit(s.Labels[i], lw)+" "+bar+" "+padLeft(vals[k], vw))
	}
	return lines
}
