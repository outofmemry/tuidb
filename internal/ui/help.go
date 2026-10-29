package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"webtuidb/internal/keymap"
)

type helpRow struct {
	header string // section title when set
	action string
	key    string // display form, e.g. "M-Enter"
	desc   string
	ctx    string
	noPref bool // a no-prefix binding: shown without the prefix
}

// chord is the key as the user types it: prefix then key, or the bare key for a no-prefix binding.
func (r helpRow) chord(pre string) string {
	if r.noPref {
		return r.key
	}
	return pre + " " + r.key
}

// helpDlg lists the key bindings for the focused pane plus the global ones.
// "/" starts a fuzzy search over command name, description and key; Enter runs the selected command.
type helpDlg struct {
	a         *App
	pre       string
	rows      []helpRow
	q         lineEdit
	searching bool
	sel, top  int
}

func (a *App) showHelp() {
	ctx := a.context()
	h := &helpDlg{a: a, pre: keymap.Display(a.d.Keys.Prefix)}
	for _, c := range []string{ctx, keymap.Global} {
		title := strings.ToUpper(c[:1]) + c[1:]
		h.rows = append(h.rows, helpRow{header: title})
		// every binding of the context in keymap table order, no-prefix ones included: the web help and `webtuidb -keys`
		// list the same rows in the same order (the menus and the help screen agree), so nothing here re-sorts or filters
		for _, b := range a.d.Keys.All() {
			if b.Context == c {
				h.rows = append(h.rows, helpRow{action: b.Action, key: keymap.Display(b.Key), desc: b.Desc, ctx: b.Context, noPref: b.NoPrefix})
			}
		}
		if ctx == keymap.Global {
			break
		}
	}
	h.snap()
	a.push(h)
}

// shown is the visible list: all rows with section headers, or the search hits ranked best first.
func (h *helpDlg) shown() []helpRow {
	q := strings.TrimSpace(h.q.Value())
	if q == "" {
		return h.rows
	}
	type hit struct {
		r helpRow
		s int
	}
	var hits []hit
	for _, r := range h.rows {
		if r.header != "" {
			continue
		}
		best, ok := 0, false
		// match the description, the action name, or the key itself ("M-Enter", "s")
		for _, field := range []string{r.desc, strings.ReplaceAll(r.action, "_", " "), r.key} {
			if s, m := fuzzy(q, field); m && (!ok || s > best) {
				best, ok = s, true
			}
		}
		if strings.EqualFold(q, r.key) {
			best, ok = best+50, true // typing the exact key finds that binding first
		}
		if ok {
			hits = append(hits, hit{r, best})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].s > hits[j].s })
	out := make([]helpRow, len(hits))
	for i, x := range hits {
		out[i] = x.r
	}
	return out
}

// snap keeps the selection on a command, never on a section header.
func (h *helpDlg) snap() {
	rows := h.shown()
	h.sel = clamp(h.sel, 0, max(0, len(rows)-1))
	for i := h.sel; i < len(rows); i++ {
		if rows[i].header == "" {
			h.sel = i
			return
		}
	}
}

func (h *helpDlg) move(d int) {
	rows := h.shown()
	for i := h.sel + d; i >= 0 && i < len(rows); i += d {
		if rows[i].header == "" {
			h.sel = i
			return
		}
	}
}

func (h *helpDlg) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if p, ok := msg.(tea.PasteMsg); ok && h.searching {
		h.q.Handle(p)
		h.sel = 0
		h.snap()
		return h, nil
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return h, nil
	}
	switch ks := k.Keystroke(); ks {
	case "esc":
		switch {
		case h.searching && h.q.Value() != "":
			h.q.Set("")
			h.snap()
		case h.searching:
			h.searching = false
		default:
			return nil, nil
		}
		return h, nil
	case "enter":
		rows := h.shown()
		if h.sel < len(rows) && rows[h.sel].header == "" {
			action := rows[h.sel].action
			return nil, func() tea.Msg { return runActionMsg{action} }
		}
		return h, nil
	case "down", "ctrl+n":
		h.move(1)
		return h, nil
	case "up", "ctrl+p":
		h.move(-1)
		return h, nil
	case "pgdown":
		for i := 0; i < 10; i++ {
			h.move(1)
		}
		return h, nil
	case "pgup":
		for i := 0; i < 10; i++ {
			h.move(-1)
		}
		return h, nil
	}
	if !h.searching {
		switch keymap.FromMsg(k) {
		case "/":
			h.searching = true
		case "q":
			return nil, nil
		case "j":
			h.move(1)
		case "k":
			h.move(-1)
		}
		return h, nil
	}
	if h.q.Handle(k) {
		h.sel = 0
		h.snap()
	}
	return h, nil
}

func (h *helpDlg) View(th Theme, maxW, maxH int) string {
	w := min(maxW-4, 90)
	cw := w - 4
	rows := h.shown()
	q := strings.TrimSpace(h.q.Value())
	slash := th.Accent.Bold(true).Render("/ ")
	var body []string
	if h.searching || h.q.Value() != "" {
		noun := "commands match"
		if len(rows) == 1 {
			noun = "command matches"
		}
		body = append(body, slash+h.q.View(cw-2, th, h.searching),
			th.Dim.Render(fmt.Sprintf("%d %s · Enter runs the selected one", len(rows), noun)))
	} else {
		body = append(body, slash+th.Dim.Render("search commands by name, description or key"),
			th.Dim.Render(fmt.Sprintf("Every action is %s then a key · %s %s sends the raw key · Esc cancels the prefix", h.pre, h.pre, h.pre)))
	}
	body = append(body, "")
	// a fixed key column sized to the longest key, and a context column on the right
	keyW, ctxW := 0, 0
	for _, r := range h.rows {
		keyW, ctxW = max(keyW, width(r.chord(h.pre))), max(ctxW, width(r.ctx))
	}
	keyW = min(keyW, 18)
	// lay every row out, then window by lines so the blank lines between sections scroll too
	var lines []string
	head := map[int]bool{}
	selLine := 0
	for i, r := range rows {
		if r.header != "" {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			head[len(lines)] = true
			lines = append(lines, th.Section.Render(r.header))
			continue
		}
		sel := i == h.sel
		if sel {
			selLine = len(lines)
		}
		on := rowStyle(th, sel)
		plain := on(lipgloss.NewStyle())
		k := ansi.Truncate(r.chord(h.pre), keyW, "…")
		d := ansi.Truncate(r.desc, max(0, cw-2-(keyW+4)-ctxW-2), "…")
		desc := plain.Render(d)
		if _, ok := fuzzy(q, r.desc); ok && q != "" {
			desc = fuzzyMark(q, d, plain, on(th.Accent.Bold(true)))
		}
		left := th.chip(k) + plain.Render(strings.Repeat(" ", keyW-width(k)+2)) + desc
		lines = append(lines, listRow(th, sel, left, on(th.Dim).Render(r.ctx), cw))
	}
	vis := max(3, maxH-13)
	if selLine < h.top {
		h.top = selLine
		if h.top > 0 && head[h.top-1] { // keep a section's title in view with its first command
			h.top--
		}
	}
	if selLine >= h.top+vis {
		h.top = selLine - vis + 1
	}
	h.top = clamp(h.top, 0, max(0, len(lines)-vis))
	body = append(body, lines[h.top:min(len(lines), h.top+vis)]...)
	if len(rows) == 0 {
		body = append(body, th.Dim.Render("  no command matches"))
	}
	body = append(body, "", th.Dim.Render("Rebind any key in ~/.config/webtuidb/config.toml under [keys], or in Settings"),
		th.Dim.Render("Open docs: Open Anything ("+h.pre+" "+keymap.Display(firstKey(h.a.d.Keys, "open_anything"))+")  ·  webtuidb -changelog  ·  webtuidb -bugreport"))
	hint := "/: search   ↑↓: move   Enter: run   Esc: close"
	if h.searching {
		hint = "↑↓: move   Enter: run   Esc: clear/stop"
	}
	return box(th, "Key bindings", body, hint, w)
}

// runActionMsg runs a prefix action chosen from the help list.
type runActionMsg struct{ action string }
