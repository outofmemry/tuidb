package ui

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

// Milestone 6: connections, editor, results, sidebar, filters. Milestone 7 and 8 live in m7.go / m8.go.

// runM678 handles the actions that work with or without a workspace. It reports whether it took the action.
func (a *App) runM678(action string) (tea.Cmd, bool) {
	switch action {
	case "open_file":
		return a.openFile(), true
	case "open_recent":
		return a.openRecent(), true
	case "settings":
		return a.settingsDialog(), true
	case "close_other_connections":
		return a.closeOthers(), true
	}
	return nil, false
}

// ---- welcome: groups, sort ---------------------------------------------------------------

// inGroup: c is in group g or one of its sub-groups.
func inGroup(c config.Connection, g string) bool {
	return c.Group == g || strings.HasPrefix(c.Group, g+"/")
}

// groupMenu is g on the welcome screen: pick a group, then rename or delete it.
func (w *welcome) groupMenu() tea.Cmd {
	gs := config.Groups(w.a.d.Conns)
	if len(gs) == 0 {
		return w.a.say("no groups yet: give a connection a Group in its form")
	}
	var items []pickItem
	for _, g := range gs {
		n := 0
		for _, c := range w.a.d.Conns {
			if inGroup(c, g) {
				n++
			}
		}
		items = append(items, pickItem{Label: g, Detail: plural(n, "connection"), Value: g})
	}
	w.a.push(&pickerDlg{title: "Groups", items: items, filter: true, onPick: func(it pickItem, _ bool) tea.Cmd {
		g := it.Value.(string)
		w.a.push(&pickerDlg{title: "Group " + g, items: []pickItem{{Label: "Rename…", Value: "rename"}, {Label: "Delete the group and its connections…", Value: "delete"}},
			onPick: func(it pickItem, _ bool) tea.Cmd {
				if it.Value == "rename" {
					return w.renameGroup(g)
				}
				return w.deleteGroup(g)
			}})
		return nil
	}})
	return nil
}

func (w *welcome) renameGroup(g string) tea.Cmd {
	w.a.push(newForm("Rename group "+g, []formField{{Key: "name", Label: "New path", Kind: fText, Value: g, Hint: "A/B nests"}}, func(v map[string]string) tea.Cmd {
		to := strings.Trim(strings.TrimSpace(v["name"]), "/")
		if to == "" || to == g {
			return nil
		}
		n := 0
		for i, c := range w.a.d.Conns {
			if c.Source == "" && inGroup(c, g) { // connections from a [[source]] folder belong to their own file
				w.a.d.Conns[i].Group = to + strings.TrimPrefix(c.Group, g)
				n++
			}
		}
		if err := w.a.saveConns(); err != nil {
			return w.a.fail(err)
		}
		return w.a.say("moved %s to %s", plural(n, "connection"), to)
	}))
	return nil
}

func (w *welcome) deleteGroup(g string) tea.Cmd {
	var gone []string
	for _, c := range w.a.d.Conns {
		if c.Source == "" && inGroup(c, g) {
			gone = append(gone, c.Name)
		}
	}
	// open workspaces of these connections are closed too: say what that discards
	openWarn := func() (dirty, applying []string) {
		for _, ws := range w.a.wss {
			if !slices.Contains(gone, ws.conn.Name) {
				continue
			}
			if ws.applying {
				applying = append(applying, ws.conn.Name)
			} else if ws.dirty() {
				dirty = append(dirty, ws.conn.Name)
			}
		}
		return
	}
	fields := []formField{
		{Kind: fLabel, Value: "This removes " + plural(len(gone), "connection") + " from connections.toml (the databases are untouched)."},
		{Kind: fLabel, Value: strings.Join(gone, ", ")},
	}
	if dirty, applying := openWarn(); len(dirty)+len(applying) > 0 {
		fields = append(fields, formField{Kind: fLabel, Value: "⚠ Open and closed with it, discarding unsaved changes: " + strings.Join(append(dirty, applying...), ", ")})
	}
	fields = append(fields, formField{Key: "confirm", Label: "Type the group name", Kind: fText})
	f := newForm("Delete group "+g+"?", fields, func(map[string]string) tea.Cmd {
		var keep []config.Connection
		for _, c := range w.a.d.Conns {
			if c.Source != "" || !inGroup(c, g) {
				keep = append(keep, c)
			}
		}
		old := w.a.d.Conns
		w.a.d.Conns = keep
		if err := w.a.saveConns(); err != nil { // save first: a failed save leaves memory and file alike
			w.a.d.Conns = old
			return w.a.fail(err)
		}
		var open []*Workspace
		for _, ws := range w.a.wss { // workspaces of the deleted connections go too
			if slices.Contains(gone, ws.conn.Name) {
				ws.close()
			} else {
				open = append(open, ws)
			}
		}
		if len(open) != len(w.a.wss) {
			w.a.wss, w.a.active = open, 0
			w.a.inWS = w.a.inWS && len(open) > 0
		}
		w.sel = 0
		w.move(0)
		return w.a.say("deleted group %s and %s", g, plural(len(gone), "connection"))
	})
	f.validate = func(v map[string]string) error {
		if _, applying := openWarn(); len(applying) > 0 {
			return fmt.Errorf("%s is still sending a change: wait for it to finish", strings.Join(applying, ", "))
		}
		if v["confirm"] != g {
			return fmt.Errorf("type “%s” to confirm", g)
		}
		return nil
	}
	f.submit = "Enter: delete"
	w.a.push(f)
	return nil
}

// sortBy is the list order inside each group: the remembered pick, else [general] connection_sort.
func (w *welcome) sortBy() string {
	return orDefault(w.a.d.State.ConnectionSort(), orDefault(w.a.d.Cfg.General.ConnectionSort, "name"))
}

// sortKey orders connections within a group; ties fall back to the name.
func (w *welcome) sortKey(c config.Connection) string {
	switch w.sortBy() {
	case "driver":
		return c.Driver
	case "recent": // newest first; never connected sorts last
		if t, ok := w.a.d.State.LastConnected()[c.Name]; ok {
			return fmt.Sprintf("%020d", math.MaxInt64-t.Unix())
		}
		return strings.Repeat("9", 20)
	case "tag":
		rank := 99 // untagged last; known tags in the order of the tag list
		for i, t := range w.a.tagNames() {
			if t == c.Tag && c.Tag != "" {
				rank = i
			}
		}
		return fmt.Sprintf("%02d", rank)
	}
	return ""
}

func (w *welcome) pickSort() tea.Cmd {
	var items []pickItem
	for _, by := range []string{"name", "tag", "driver", "recent"} {
		mark := "  "
		if by == w.sortBy() {
			mark = "• "
		}
		items = append(items, pickItem{Label: mark + by, Value: by})
	}
	w.a.push(&pickerDlg{title: "Sort connections by", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		if err := w.a.d.State.SetConnectionSort(it.Value.(string)); err != nil {
			return w.a.fail(err)
		}
		w.sel = 0
		w.move(0)
		return nil
	}})
	return nil
}

// ---- close other connections -------------------------------------------------------------

func (a *App) closeOthers() tea.Cmd {
	cur := a.ws()
	if cur == nil || len(a.wss) < 2 {
		return a.say("no other connection is open")
	}
	dirty, applying := 0, 0
	for _, w := range a.wss {
		if w == cur {
			continue
		}
		if w.applying { // closing would cancel a commit half way
			applying++
		} else if w.dirty() {
			dirty++
		}
	}
	if applying > 0 {
		return a.say("%d connection(s) still sending a change: wait for it to finish before closing the others", applying)
	}
	do := func() tea.Cmd {
		n := len(a.wss) - 1
		for _, w := range a.wss {
			if w != cur {
				w.close()
			}
		}
		a.wss, a.active = []*Workspace{cur}, 0
		return a.say("closed %s", plural(n, "connection"))
	}
	if dirty > 0 {
		a.push(&confirmDlg{title: "Close the other connections?", lines: []string{plural(dirty, "of them") + " have unsaved changes that will be lost."}, yes: "Close", danger: true, onYes: do})
		return nil
	}
	a.push(&confirmDlg{title: "Close the other connections?", lines: []string{"Keeps " + cur.conn.Name + " and closes the " + plural(len(a.wss)-1, "other") + "."}, yes: "Close", onYes: do})
	return nil
}

// ---- open file, recent files, SQL viewer -------------------------------------------------

// maxSQLFile caps what open_file reads into an editor.
const maxSQLFile = 8 << 20

func (a *App) openFile() tea.Cmd {
	a.push(newForm("Open a SQL file", []formField{{Key: "path", Label: "File", Kind: fText}}, func(v map[string]string) tea.Cmd {
		return a.openSQLFile(strings.TrimSpace(v["path"]))
	}))
	return nil
}

func (a *App) openRecent() tea.Cmd {
	var items []pickItem
	for _, f := range a.d.State.RecentFiles() {
		if !filepath.IsAbs(f) { // the web only knows base names: opening one here would hit the working directory
			continue
		}
		items = append(items, pickItem{Label: filepath.Base(f), Detail: f, Value: f})
	}
	a.push(&pickerDlg{title: "Recent files", filter: true, items: items, empty: "no files opened yet", onPick: func(it pickItem, _ bool) tea.Cmd {
		return a.openSQLFile(it.Value.(string))
	}})
	return nil
}

// openSQLFile reads path: inside a workspace into a new editor tab, on the welcome screen into the read-only viewer.
func (a *App) openSQLFile(path string) tea.Cmd {
	st, err := os.Stat(path)
	switch {
	case err != nil:
		return a.fail(err)
	case st.IsDir():
		return a.say("%s is a folder", path)
	case st.Size() > maxSQLFile:
		return a.say("%s is %d MB: files over %d MB are not opened", path, st.Size()>>20, maxSQLFile>>20)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return a.fail(err)
	}
	a.d.State.AddRecentFile(path, a.d.Cfg.General.RecentFiles)
	if w := a.ws(); w != nil {
		cmd := w.newQueryTab(string(b))
		if t, ok := w.tab().(*queryTab); ok {
			t.name, t.file, t.fileText = filepath.Base(path), path, t.ed.Text()
		}
		return cmd
	}
	a.viewSQL(path, string(b))
	return nil
}

// viewSQL is the connectionless viewer: the file highlighted, read-only, scrollable.
// shortcut: an overlay rather than a tab (the welcome screen has no tabs); connect and open the file there to edit or run it.
func (a *App) viewSQL(path, text string) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	kinds := lexLines("postgres", lines)
	d := &paneDlg{title: "SQL viewer · " + filepath.Base(path) + " (read-only)", hint: "↑↓ PgUp/PgDn: scroll   c: copy   Esc: close"}
	d.lines = func(th Theme, W int) []string {
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = th.Dim.Render(padLeft(fmt.Sprint(i+1), 4)+" ") + highlight(th, l, kinds[i])
		}
		return out
	}
	d.keys = map[string]func() tea.Cmd{"c": func() tea.Cmd { return a.copyText(text, "the file") }}
	a.push(d)
}

// ---- editor: find previous / next, block selection ---------------------------------------

// FindPrev selects the closest match of q before the selection (or cursor), wrapping around.
func (e *Editor) FindPrev(q string) bool {
	qr := lowerRunes(q)
	if len(qr) == 0 {
		return false
	}
	start := e.cur
	if a, _, ok := e.selRange(); ok {
		start = a
	}
	n := len(e.lines)
	for i := 0; i <= n; i++ {
		row := ((start.row-i)%n + n) % n
		rs := e.runes(row)
		to := len(rs) - len(qr)
		if i == 0 {
			to = min(to, start.col-1)
		}
		for c := to; c >= 0; c-- {
			if matchAt(rs, c, qr) {
				e.anchor, e.cur = &pos{row, c}, pos{row, c + len(qr)}
				e.scroll()
				return true
			}
		}
	}
	return false
}

// findStep is find_next (dir 1) and find_prev (dir -1) on the last search text.
func (t *queryTab) findStep(dir int) tea.Cmd {
	if t.edFind == "" {
		return t.findInEditor()
	}
	find := t.ed.Find
	if dir < 0 {
		find = t.ed.FindPrev
	}
	if !find(t.edFind) {
		return t.ws.a.say("%q is not in the editor", t.edFind)
	}
	return nil
}

// blockRect is the rectangle of block mode: rows r0..r1, columns c0..c1-1 (ok is false while it is empty).
func (e *Editor) blockRect() (r0, r1, c0, c1 int, ok bool) {
	if e.blk == nil {
		return
	}
	r0, r1 = min(e.blk.row, e.cur.row), max(e.blk.row, e.cur.row)
	c0, c1 = min(e.blk.col, e.cur.col), max(e.blk.col, e.cur.col)
	return r0, r1, c0, c1, c1 > c0
}

func (e *Editor) blockText() string {
	r0, r1, c0, c1, _ := e.blockRect()
	var out []string
	for r := r0; r <= r1; r++ {
		rs := e.runes(r)
		if c0 >= len(rs) {
			out = append(out, "")
			continue
		}
		out = append(out, string(rs[c0:min(c1, len(rs))]))
	}
	return strings.Join(out, "\n")
}

// deleteBlock cuts the rectangle out of every row it covers and leaves block mode.
func (e *Editor) deleteBlock() bool {
	r0, r1, c0, c1, ok := e.blockRect()
	if !ok {
		return false
	}
	for r := r0; r <= r1; r++ {
		if rs := e.runes(r); c0 < len(rs) {
			e.lines[r] = string(rs[:c0]) + string(rs[min(c1, len(rs)):])
		}
	}
	e.cur, e.anchor, e.blk = pos{r0, c0}, nil, nil
	e.cache.dirty = true
	return true
}

// toggleBlock is block_select: the cursor becomes one corner of a rectangle that the arrow keys stretch.
func (e *Editor) toggleBlock() string {
	if e.blk != nil {
		e.blk = nil
		return "block selection off"
	}
	p := e.cur
	e.blk, e.anchor = &p, nil
	return "block selection: arrows stretch it, ctrl+c copies, delete cuts, V or Esc ends it"
}

func (e *Editor) blockBadge() string {
	if e.blk != nil {
		return " [BLOCK]"
	}
	return ""
}

// ---- editor: split down ---------------------------------------------------------------------

// splitDown stacks a second editor under the first in this tab; again, it closes the unfocused one.
func (t *queryTab) splitDown() tea.Cmd {
	if t.other == nil {
		c := t.ws.a.d.Cfg.Editor
		e := NewEditor(t.ws.caps.Dialect, EditorOpts{TabWidth: c.IndentWidth, UseTabs: c.Indent == "tabs", CloseBraces: c.CloseBraces, Uppercase: c.UppercaseKeywords,
			CompleteKey: c.CompleteKey, Highlight: c.HighlightCurrent, Autocomplete: c.Autocomplete, Wrap: c.Wrap, AddSpace: c.AddSpace})
		e.Complete = t.complete
		t.ed, t.other, t.focusBottom = e, t.ed, true
		t.savedAs, t.lastAuto = "", "" // auto-save must not write the new pane over the saved query
		return nil
	}
	drop := func() tea.Cmd {
		t.other, t.focusBottom = nil, false
		return t.ws.a.say("closed the other editor pane")
	}
	if strings.TrimSpace(t.other.Text()) != "" {
		t.ws.a.push(&confirmDlg{title: "Close the other editor pane?", lines: []string{"Its text is dropped."}, yes: "Close", danger: true, onYes: drop})
		return nil
	}
	return drop()
}

// swapPane moves the focus to the other editor of a split tab.
func (t *queryTab) swapPane() {
	t.ed, t.other = t.other, t.ed
	t.savedAs, t.lastAuto = "", ""
	t.focusBottom = !t.focusBottom
}

// editorsView draws the editor area: one editor, or two stacked with a rule between.
func (t *queryTab) editorsView(th Theme, W, H int, focused bool) string {
	if t.other == nil || H < 5 {
		t.ed.W, t.ed.H = W, H
		return t.ed.View(th, focused)
	}
	hTop := (H - 1) / 2
	top, bot := t.ed, t.other
	if t.focusBottom {
		top, bot = t.other, t.ed
	}
	top.W, top.H, bot.W, bot.H = W, hTop, W, H-1-hTop
	return top.View(th, focused && !t.focusBottom) + "\n" + th.Border.Render(strings.Repeat("─", W)) + "\n" + bot.View(th, focused && t.focusBottom)
}

// ---- editor: Mongo shell methods ---------------------------------------------------------

var mongoMethods = []string{"find", "findOne", "aggregate", "count", "insertOne", "insertMany", "updateOne", "updateMany", "deleteOne", "deleteMany", "sort", "limit", "skip", "project"}

// mongoMethodCandidates suggests the shell methods after "db.<collection>." (qual is "db.<collection>").
func mongoMethodCandidates(qual string) []Candidate {
	if parts := strings.Split(qual, "."); len(parts) != 2 || !strings.EqualFold(parts[0], "db") || parts[1] == "" {
		return nil
	}
	out := make([]Candidate, len(mongoMethods))
	for i, m := range mongoMethods {
		out[i] = Candidate{Text: m, Kind: "method"}
	}
	return out
}

// ---- results: charts with several Y columns ----------------------------------------------

var seriesFill = []string{"█", "▓", "▒", "░"}

// multiChartLines draws grouped bars: one row per series under each label, with a legend line first.
func multiChartLines(th Theme, kind string, s db.ChartSeries, W, H int) []string {
	top := 0.0
	for _, ser := range s.Series {
		for _, v := range ser.Values {
			top = max(top, v)
		}
	}
	var legend []string
	for i, ser := range s.Series {
		legend = append(legend, th.Accent.Render(seriesFill[i%len(seriesFill)])+" "+ser.Name)
	}
	lines := []string{th.Dim.Render(fmt.Sprintf(" %s chart · %d points · ", kind, len(s.Labels))) + strings.Join(legend, "  ") + th.Dim.Render("  · g: grid")}
	lw := 0
	for _, l := range s.Labels {
		lw = max(lw, width(l))
	}
	lw = min(lw, max(8, W/3))
	bw := max(1, W-lw-14)
	for k, label := range s.Labels {
		if len(lines)+len(s.Series) > H-1 && k < len(s.Labels)-1 {
			lines = append(lines, th.Dim.Render(fmt.Sprintf("  … %d more", len(s.Labels)-k)))
			break
		}
		for i, ser := range s.Series {
			n := 0
			if top > 0 && ser.Values[k] > 0 {
				n = max(1, int(ser.Values[k]/top*float64(bw)+0.5))
			}
			name := ""
			if i == 0 {
				name = label
			}
			bar := th.Accent.Render(strings.Repeat(seriesFill[i%len(seriesFill)], n)) + strings.Repeat(" ", bw-n)
			lines = append(lines, "  "+fit(name, lw)+" "+bar+" "+fmt.Sprint(ser.Values[k]))
		}
	}
	return lines
}

// saveAs writes the cell's bytes to a file (quick look on a binary cell).
func (q *quickLookDlg) saveAs() tea.Cmd {
	q.a.push(newForm("Save the cell as a file", []formField{{Key: "path", Label: "File", Kind: fText, Value: "cell.bin"}}, func(v map[string]string) tea.Cmd {
		path := strings.TrimSpace(v["path"])
		if path == "" {
			return q.a.fail(fmt.Errorf("file name required"))
		}
		return q.a.ifFree(func() tea.Cmd {
			err := writeAtomic(path, func(w io.Writer) error { _, err := w.Write(q.rawBytes); return err })
			if err != nil {
				return q.a.fail(err)
			}
			return q.a.say("saved %d bytes to %s", len(q.rawBytes), path)
		}, path)
	}))
	return nil
}
