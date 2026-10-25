package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/board"
	"webtuidb/internal/db"
)

// Metrics boards: the list (new, rename, duplicate, delete) and the board view with button and import panels.

func (w *Workspace) metricsAction() tea.Cmd {
	p := &pickerDlg{title: "Metrics boards", hint: "Enter: open   n: new   e: rename   d: duplicate   x: delete   Esc: close",
		empty: "no boards in " + board.Dir() + ": press n to make one"}
	rebuild := func() error {
		bs, err := board.List()
		if err != nil {
			return err
		}
		p.items = nil
		for _, b := range bs {
			detail := fmt.Sprintf("%d panel(s), %d variable(s)", len(b.Panels), len(b.Vars))
			if b.Err != nil {
				detail = "error: " + b.Err.Error()
			}
			p.items = append(p.items, pickItem{Label: b.Name, Detail: detail, Value: b})
		}
		p.sel = clamp(p.sel, 0, max(0, len(p.items)-1))
		return nil
	}
	if err := rebuild(); err != nil {
		return w.a.fail(err)
	}
	pick := func(f func(b board.Board) tea.Cmd) func(pickItem, bool) tea.Cmd {
		return func(it pickItem, ok bool) tea.Cmd {
			if !ok {
				return nil
			}
			return f(it.Value.(board.Board))
		}
	}
	after := func(cmd tea.Cmd) tea.Cmd { rebuild(); return cmd }
	del := pick(func(b board.Board) tea.Cmd { return w.deleteBoard(b, func() { rebuild() }) })
	p.keys = map[string]func(pickItem, bool) tea.Cmd{
		"n":      func(pickItem, bool) tea.Cmd { return w.newBoard(func() { rebuild() }) },
		"e":      pick(func(b board.Board) tea.Cmd { return w.renameBoard(b, func() { rebuild() }) }),
		"d":      pick(func(b board.Board) tea.Cmd { return after(w.duplicateBoard(b)) }),
		"x":      del,
		"delete": del,
	}
	p.onPick = func(it pickItem, _ bool) tea.Cmd {
		b := it.Value.(board.Board)
		if b.Err != nil {
			return w.a.fail(b.Err)
		}
		return w.openBoard(b)
	}
	w.a.push(p)
	return nil
}

// newBoard asks for a name and a first panel.
func (w *Workspace) newBoard(done func()) tea.Cmd {
	f := newForm("New metrics board", []formField{
		{Key: "name", Label: "Board name", Kind: fText},
		{Key: "title", Label: "First panel title", Kind: fText},
		{Key: "kind", Label: "Panel kind", Kind: fChoice, Options: []string{"number", "table", "bar", "line", "input", "dropdown", "exporter"}, Value: "table"},
		{Key: "var", Label: "Variable", Kind: fText, Hint: "name of the $var it sets", Show: func(v map[string]string) bool { return v["kind"] == "input" || v["kind"] == "dropdown" }},
		{Key: "options", Label: "Options", Kind: fText, Hint: "a, b, c  (empty: first column of the query)", Show: func(v map[string]string) bool { return v["kind"] == "dropdown" }},
		{Key: "default", Label: "Default", Kind: fText, Show: func(v map[string]string) bool { return v["kind"] == "input" || v["kind"] == "dropdown" }},
		{Key: "format", Label: "Export format", Kind: fChoice, Options: []string{"csv", "json", "sql", "excel"}, Value: "csv", Show: func(v map[string]string) bool { return v["kind"] == "exporter" }},
		{Key: "query", Label: "Read-only query", Kind: fText, Show: func(v map[string]string) bool { return v["kind"] != "input" }},
		{Key: "chart", Label: "Chart columns", Kind: fText, Hint: "x: a, y: b, c", Show: func(v map[string]string) bool { return v["kind"] == "bar" || v["kind"] == "line" }},
	}, func(v map[string]string) tea.Cmd {
		p := board.Panel{Title: strings.TrimSpace(v["title"]), Kind: v["kind"], Query: strings.TrimSpace(v["query"]), Chart: strings.TrimSpace(v["chart"])}
		switch v["kind"] {
		case "input", "dropdown":
			p.Var, p.Default, p.Options = strings.TrimSpace(v["var"]), v["default"], splitList(v["options"])
		case "exporter":
			p.Format = v["format"]
		}
		nb := board.Board{Name: strings.TrimSpace(v["name"]), Panels: []board.Panel{p}}
		err := nb.Save()
		if err != nil {
			return w.a.fail(err)
		}
		done()
		return w.a.say("created board %s", strings.TrimSpace(v["name"]))
	})
	f.validate = func(v map[string]string) error {
		if strings.TrimSpace(v["name"]) == "" || strings.TrimSpace(v["title"]) == "" || strings.TrimSpace(v["query"]) == "" {
			return fmt.Errorf("name, panel title and query are required")
		}
		return nil
	}
	w.a.push(f)
	return nil
}

// renameBoard saves the board under the new name, then removes the old file.
func (w *Workspace) renameBoard(b board.Board, done func()) tea.Cmd {
	if b.Err != nil {
		return w.a.fail(b.Err)
	}
	w.a.push(newForm("Rename board "+b.Name, []formField{{Key: "name", Label: "New name", Kind: fText, Value: b.Name}}, func(v map[string]string) tea.Cmd {
		name := strings.TrimSpace(v["name"])
		if name == "" || name == b.Name {
			return nil
		}
		nb := b
		nb.Name, nb.Path = name, ""
		if err := nb.Save(); err != nil {
			return w.a.fail(err)
		}
		if err := removeBoardFile(b.Path); err != nil {
			return w.a.fail(err)
		}
		done()
		return w.a.say("renamed to %s", name)
	}))
	return nil
}

func (w *Workspace) deleteBoard(b board.Board, done func()) tea.Cmd {
	f := newForm("Delete board "+b.Name+"?", []formField{
		{Kind: fLabel, Value: "This removes " + b.Path + "."},
		{Key: "confirm", Label: "Type its name", Kind: fText},
	}, func(map[string]string) tea.Cmd {
		if err := removeBoardFile(b.Path); err != nil {
			return w.a.fail(err)
		}
		done()
		return w.a.say("deleted board %s", b.Name)
	})
	f.validate = func(v map[string]string) error {
		if v["confirm"] != b.Name {
			return fmt.Errorf("type %q to confirm", b.Name)
		}
		return nil
	}
	f.submit = "Enter: delete"
	w.a.push(f)
	return nil
}

// removeBoardFile deletes a board file, but only inside the boards folder.
func removeBoardFile(path string) error {
	if rel, err := filepath.Rel(board.Dir(), path); path == "" || err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("%q is not a board file", path)
	}
	return os.Remove(path)
}

// ---- the board view ------------------------------------------------------------------------

type boardView struct {
	w       *Workspace
	b       board.Board
	vals    map[string]string
	res     map[string]board.PanelResult // by panel title
	sel     int                          // the highlighted panel (tab / shift+tab move it)
	running bool
	queued  func() tea.Cmd // a run asked for while one was running: it starts when that one ends
	closed  bool
	at      time.Time
	failure string // why the board could not run at all
}

func (w *Workspace) openBoard(b board.Board) tea.Cmd {
	v := &boardView{w: w, b: b, vals: map[string]string{}, res: map[string]board.PanelResult{}}
	for _, x := range b.Vars {
		v.vals[x.Name] = x.Default
	}
	d := &paneDlg{title: "Board " + b.Name, hint: "Tab: panel   Enter: run it / click   r: run all   e: variables   Esc: close", onClose: func() { v.closed = true }}
	d.lines = v.lines
	step := func(d int) func() tea.Cmd {
		return func() tea.Cmd {
			if n := len(b.Panels); n > 0 {
				v.sel = (v.sel + d + n) % n
			}
			return nil
		}
	}
	d.keys = map[string]func() tea.Cmd{
		"r":         v.run,
		"tab":       step(1),
		"shift+tab": step(-1),
		"enter":     v.activate,
		"e": func() tea.Cmd {
			if len(b.Vars) == 0 {
				return w.a.say("this board has no variables")
			}
			var fs []formField
			for _, x := range b.Vars {
				f := formField{Key: x.Name, Label: orDefault(x.Label, x.Name), Kind: fText, Value: v.vals[x.Name]}
				if len(x.Options) > 0 {
					f.Kind, f.Options = fChoice, x.Options
				}
				fs = append(fs, f)
			}
			w.a.push(newForm("Variables of "+b.Name, fs, func(m map[string]string) tea.Cmd {
				v.vals = m
				return v.run()
			}))
			return nil
		},
	}
	w.a.push(d)
	return tea.Batch(v.run(), v.tick())
}

// session is where the board runs: the workspace's, or the open connection the board names.
func (v *boardView) session() db.Session {
	b := v.b
	if b.Connection == "" || b.Connection == v.w.conn.Name {
		return v.w.sess
	}
	for _, o := range v.w.a.wss {
		if o.conn.Name == b.Connection && !o.closed {
			return o.sess
		}
	}
	v.failure = fmt.Sprintf("connection %q is not open: connect to it first (this board runs there)", b.Connection)
	return nil
}

func (v *boardView) valsCopy() map[string]string {
	vals := map[string]string{}
	for k, x := range v.vals {
		vals[k] = x
	}
	return vals
}

// run executes every panel; the board refreshes itself every refresh_secs.
func (v *boardView) run() tea.Cmd {
	if v.closed {
		return nil
	}
	if v.running {
		v.queued = v.run // rerun with the newest variables once this one is done
		return nil
	}
	v.failure = ""
	sess := v.session()
	if sess == nil {
		return nil
	}
	v.running = true
	b, vals := v.b, v.valsCopy()
	return liveCmd(func() func() tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		res := board.Run(ctx, sess, b, vals, 100)
		return func() tea.Cmd {
			v.running, v.at = false, time.Now()
			v.res = map[string]board.PanelResult{}
			for _, r := range res {
				v.res[r.Panel.Title] = r
			}
			return v.next()
		}
	})
}

// next starts the run that was asked for while the last one was busy.
func (v *boardView) next() tea.Cmd {
	q := v.queued
	v.queued = nil
	if q == nil || v.closed {
		return nil
	}
	return q()
}

// tick re-arms itself every refresh_secs for as long as the board is open, whatever else runs: a
// tick that finds a run in flight skips its work instead of ending the chain.
func (v *boardView) tick() tea.Cmd {
	if v.closed || v.b.RefreshSecs <= 0 || v.w.a.d.NoTimers {
		return nil
	}
	return tea.Tick(time.Duration(v.b.RefreshSecs)*time.Second, func(time.Time) tea.Msg {
		return liveMsg{func() tea.Cmd {
			if v.closed {
				return nil
			}
			if v.running {
				return v.tick()
			}
			return tea.Batch(v.run(), v.tick())
		}}
	})
}

func (v *boardView) panel(title string) (board.Panel, bool) {
	for _, p := range v.b.Panels {
		if p.Title == title {
			return p, true
		}
	}
	return board.Panel{}, false
}

// cascade is seeds plus every panel that refreshes on one of them (refresh_on), transitively, each once.
func (v *boardView) cascade(seeds ...board.Panel) []board.Panel {
	seen := map[string]bool{}
	var out []board.Panel
	for q := seeds; len(q) > 0; q = q[1:] {
		p := q[0]
		if seen[p.Title] {
			continue
		}
		seen[p.Title] = true
		out = append(out, p)
		q = append(q, board.Dependents(v.b, p.Title)...)
	}
	return out
}

// activate is Enter on the highlighted panel: a button runs its target, an import opens the import wizard, any other panel runs again.
func (v *boardView) activate() tea.Cmd {
	if v.sel >= len(v.b.Panels) {
		return nil
	}
	p := v.b.Panels[v.sel]
	switch p.Kind {
	case "import":
		if c := v.b.Connection; c != "" && c != v.w.conn.Name {
			return v.w.a.say("this board runs on %s, but import writes into %s: open the board from %s", c, v.w.conn.Name, c)
		}
		ref := db.ObjRef{Schema: v.w.sess.CurrentSchema(), Name: p.Table, Kind: db.KindTable}
		if s, n, ok := strings.Cut(p.Table, "."); ok {
			ref.Schema, ref.Name = s, n
		}
		if ref.Name == "" {
			return v.w.a.say("the import panel %q names no table", p.Title)
		}
		return v.w.importWizard(ref, true)
	case "button":
		target, ok := v.panel(p.Target)
		if !ok {
			return v.w.a.say("button %q has no target panel %q", p.Title, p.Target)
		}
		return v.runPanels(append(v.cascade(target), v.cascade(board.Dependents(v.b, p.Title)...)...))
	}
	return v.runPanels(v.cascade(p))
}

// runPanels runs the given panels one by one (board.RunPanel) and replaces their results.
func (v *boardView) runPanels(ps []board.Panel) tea.Cmd {
	if v.closed {
		return nil
	}
	if v.running {
		v.queued = func() tea.Cmd { return v.runPanels(ps) }
		return nil
	}
	sess := v.session()
	if sess == nil {
		return nil
	}
	v.running = true
	b, vals := v.b, v.valsCopy()
	return liveCmd(func() func() tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		seen := map[string]bool{}
		var out []board.PanelResult
		for _, p := range ps {
			if seen[p.Title] || p.Kind == "button" || p.Kind == "import" {
				continue
			}
			seen[p.Title] = true
			out = append(out, board.RunPanel(ctx, sess, b, p, vals, 100))
		}
		return func() tea.Cmd {
			v.running, v.at = false, time.Now()
			for _, r := range out {
				v.res[r.Panel.Title] = r
			}
			return v.next()
		}
	})
}

const tileH = 8 // content lines per panel

func (v *boardView) lines(th Theme, W int) []string {
	state := "running…"
	if !v.running && !v.at.IsZero() {
		state = "updated " + v.at.Format("15:04:05")
	}
	if v.b.RefreshSecs > 0 {
		state += fmt.Sprintf(" · refresh every %ds", v.b.RefreshSecs)
	}
	head := th.Dim.Render(" " + state)
	for _, x := range v.b.Vars {
		head += "  " + th.Accent.Render(x.Name+"=") + v.vals[x.Name]
	}
	out := []string{head, ""}
	if v.failure != "" {
		return append(out, th.Err.Render("  "+v.failure))
	}
	cols := 1
	if W >= 100 {
		cols = 2
	}
	tw := W / cols
	ps := v.b.Panels
	for i := 0; i < len(ps); i += cols {
		var tiles [][]string
		for j := i; j < min(i+cols, len(ps)); j++ {
			r, ran := v.res[ps[j].Title]
			if !ran {
				r = board.PanelResult{Panel: ps[j]}
			}
			tiles = append(tiles, tile(th, &r, tw-1, j == v.sel))
		}
		for l := 0; l < tileH+2; l++ {
			var row []string
			for _, t := range tiles {
				row = append(row, t[l])
			}
			out = append(out, strings.Join(row, " "))
		}
	}
	if len(ps) == 0 {
		out = append(out, th.Dim.Render("  this board has no panels"))
	}
	return out
}

// tile draws one panel as a framed box of tw cells and tileH content lines; sel highlights the title.
func tile(th Theme, r *board.PanelResult, tw int, sel bool) []string {
	iw := max(1, tw-2) // a very narrow pane must not make the border repeat counts negative
	var body []string
	centered := func(s string) []string {
		return []string{"", "", strings.Repeat(" ", max(0, (iw-width(s))/2)) + th.Accent.Bold(true).Render(s)}
	}
	switch {
	case r.Panel.Kind == "button":
		body = centered("[ ↵ ] run " + r.Panel.Target)
	case r.Panel.Kind == "import":
		body = centered("[ ↵ ] import into " + r.Panel.Table)
	case r.Err != nil:
		body = wrapText(r.Err.Error(), iw)
		for i := range body {
			body[i] = th.Err.Render(body[i])
		}
	case r.Result == nil:
		body = []string{"", th.Dim.Render("  not run yet")}
	case r.Panel.Kind == "number":
		body = centered(orDefault(r.Number, "–"))
	case r.Panel.Kind == "bar" || r.Panel.Kind == "line":
		rv := &resultView{raw: r.Result, stmt: "-- " + r.Panel.Kind + "chart " + r.Panel.Chart}
		body = chartLines(th, rv, iw, tileH+1)[1:] // drop the "g: back to the grid" header meant for the results pane
	default:
		cw := max(4, iw/max(1, len(r.Result.Columns))-1)
		var hd []string
		for _, c := range r.Result.Columns {
			hd = append(hd, fit(c.Name, cw))
		}
		body = []string{th.Dim.Render(strings.Join(hd, " "))}
		for _, row := range r.Result.Rows {
			var cs []string
			for _, c := range row {
				cs = append(cs, fit(oneLine(plain(c)), cw))
			}
			body = append(body, strings.Join(cs, " "))
		}
	}
	mark := " "
	if sel {
		mark = "▸"
	}
	title := fit(mark+r.Panel.Title+" ", max(1, iw-1))
	b := th.Border
	ts := th.Section
	if sel {
		ts = th.Accent.Bold(true)
	}
	out := []string{b.Render("┌─") + ts.Render(title) + b.Render(strings.Repeat("─", max(0, iw-1-width(title)))+"┐")}
	for _, l := range padLines(body, iw, tileH) {
		out = append(out, b.Render("│")+l+b.Render("│"))
	}
	return append(out, b.Render("└"+strings.Repeat("─", iw)+"┘"))
}
