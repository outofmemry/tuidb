package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tuidb/internal/keymap"
)

type helpRow struct {
	header string // section title when set
	action string
	key    string // display form, e.g. "M-Enter"
	desc   string
	ctx    string
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
	seen := map[string]bool{}
	for _, c := range []string{ctx, keymap.Global} {
		title := strings.ToUpper(c[:1]) + c[1:]
		h.rows = append(h.rows, helpRow{header: title})
		bs := a.d.Keys.Bindings(c)
		sort.SliceStable(bs, func(i, j int) bool { return bs[i].Desc < bs[j].Desc })
		for _, b := range bs {
			// Bindings(ctx) also returns the global ones; list each under its own section
			if seen[b.Action] || b.Context != c {
				continue
			}
			seen[b.Action] = true
			h.rows = append(h.rows, helpRow{action: b.Action, key: keymap.Display(b.Key), desc: b.Desc, ctx: b.Context})
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
	rows := h.shown()
	var body []string
	if h.searching || h.q.Value() != "" {
		body = append(body, th.Accent.Render("/ ")+h.q.View(w-8, th, h.searching), "")
	} else {
		body = append(body, th.Dim.Render(fmt.Sprintf("Every action is %s then a key · %s %s sends the raw key · Esc cancels the prefix", h.pre, h.pre, h.pre)), "")
	}
	vis := max(3, maxH-12)
	if h.sel < h.top {
		h.top = h.sel
	}
	if h.sel >= h.top+vis {
		h.top = h.sel - vis + 1
	}
	if len(rows) == 0 {
		body = append(body, th.Dim.Render("no command matches"))
	}
	for i := h.top; i < min(len(rows), h.top+vis); i++ {
		r := rows[i]
		if r.header != "" {
			body = append(body, th.Bold.Render(r.header))
			continue
		}
		key := fit(h.pre+" "+r.key, 14)
		line := "  " + th.Accent.Render(key) + " " + fit(r.desc, w-30) + " " + th.Dim.Render(r.ctx)
		if i == h.sel {
			line = th.SelRow.Render(fit("  "+key+" "+fit(r.desc, w-30)+" "+r.ctx, w-4))
		}
		body = append(body, line)
	}
	hint := "/ search · ↑↓ move · Enter run · Esc close"
	if h.searching {
		hint = "type to search name, description or key · ↑↓ move · Enter run · Esc clear/stop"
	}
	return box(th, "Key bindings", body, hint+"   ·   rebind in ~/.config/tuidb/config.toml [keys]", w)
}

// runActionMsg runs a prefix action chosen from the help list.
type runActionMsg struct{ action string }
