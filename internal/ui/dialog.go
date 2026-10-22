package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Overlay is a modal layer. Update returns (nil, cmd) to close itself.
type Overlay interface {
	Update(msg tea.Msg) (Overlay, tea.Cmd)
	View(th Theme, maxW, maxH int) string
}

// box wraps content in a rounded border with a title.
func box(th Theme, title string, content []string, hint string, w int) string {
	inner := w - 4
	var lines []string
	lines = append(lines, th.Title.Render(fit(title, inner)))
	lines = append(lines, "")
	for _, l := range content {
		lines = append(lines, fit(l, inner))
	}
	if hint != "" {
		lines = append(lines, "", fit(th.hintChips(hint), inner))
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(th.Border.GetForeground()).Padding(0, 1).Render(strings.Join(lines, "\n"))
}

// composite draws box centered over base (both multi-line, base is W x H).
func composite(base, over string, W, H int) string {
	bl := strings.Split(base, "\n")
	for len(bl) < H {
		bl = append(bl, "")
	}
	ol := strings.Split(over, "\n")
	ow := 0
	for _, l := range ol {
		ow = max(ow, width(l))
	}
	x, y := max(0, (W-ow)/2), max(0, (H-len(ol))/2)
	for i, l := range ol {
		if y+i >= len(bl) {
			break
		}
		row := bl[y+i]
		if pad := W - width(row); pad > 0 {
			row += strings.Repeat(" ", pad)
		}
		bl[y+i] = ansi.Truncate(row, x, "") + l + strings.Repeat(" ", max(0, ow-width(l))) + ansi.TruncateLeft(row, x+ow, "")
	}
	return strings.Join(bl[:H], "\n")
}

// ---- confirm ----------------------------------------------------------------------------

type confirmDlg struct {
	title  string
	lines  []string
	yes    string
	danger bool
	onYes  func() tea.Cmd
	scroll int
}

func (d *confirmDlg) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.Keystroke() {
		case "y":
			return nil, d.onYes()
		case "enter": // a reflexive Enter must not be the destructive choice
			if d.danger {
				return nil, nil
			}
			return nil, d.onYes()
		case "n", "esc", "q":
			return nil, nil
		case "down", "j":
			d.scroll++
		case "up", "k":
			d.scroll = max(0, d.scroll-1)
		}
	}
	return d, nil
}

func (d *confirmDlg) View(th Theme, maxW, maxH int) string {
	w := min(maxW-4, 80)
	var body []string
	for _, l := range d.lines {
		body = append(body, wrapText(l, w-4)...)
	}
	vis := max(3, maxH-10)
	d.scroll = clamp(d.scroll, 0, max(0, len(body)-vis))
	more := ""
	if len(body) > vis {
		more = fmt.Sprintf("  (%d-%d of %d lines, ↑/↓ scroll)", d.scroll+1, min(len(body), d.scroll+vis), len(body))
		body = body[d.scroll:min(len(body), d.scroll+vis)]
	}
	label := d.yes
	if label == "" {
		label = "Yes"
	}
	hint := "y/Enter: " + label + "   n/Esc: cancel" + more
	if d.danger {
		hint = "y: " + label + "   Enter/n/Esc: cancel" + more
	}
	t := d.title
	if d.danger {
		t = th.Err.Render("⚠ ") + t
	}
	return box(th, t, body, hint, w)
}

// ---- form -------------------------------------------------------------------------------

type fieldKind int

const (
	fText fieldKind = iota
	fSecret
	fBool
	fChoice
	fLabel // read-only line
)

type formField struct {
	Key     string
	Label   string
	Kind    fieldKind
	Value   string
	Options []string // fChoice
	Hint    string
	Show    func(vals map[string]string) bool // nil = always
	edit    lineEdit
}

type formDlg struct {
	title    string
	fields   []*formField
	idx      int
	err      string
	submit   string
	validate func(vals map[string]string) error
	onSubmit func(vals map[string]string) tea.Cmd
	top      int
	onChange func(key string, vals map[string]string)        // lets the owner react (e.g. driver changes the default port)
	hotkeys  map[string]func(vals map[string]string) tea.Cmd // extra keys while the form is open, e.g. ctrl+t
	hotHint  string
}

func newForm(title string, fields []formField, onSubmit func(map[string]string) tea.Cmd) *formDlg {
	f := &formDlg{title: title, onSubmit: onSubmit, submit: "Enter: OK"}
	for i := range fields {
		fl := fields[i]
		if fl.Kind == fText || fl.Kind == fSecret {
			fl.edit = newLine(fl.Value)
			fl.edit.Secret = fl.Kind == fSecret
		}
		f.fields = append(f.fields, &fl)
	}
	f.idx = f.firstEditable(0, 1)
	return f
}

func (f *formDlg) values() map[string]string {
	m := map[string]string{}
	for _, fl := range f.fields {
		switch fl.Kind {
		case fText, fSecret:
			m[fl.Key] = fl.edit.Value()
		default:
			m[fl.Key] = fl.Value
		}
	}
	return m
}

func (f *formDlg) visible(i int) bool {
	fl := f.fields[i]
	return fl.Kind != fLabel && (fl.Show == nil || fl.Show(f.values()))
}

func (f *formDlg) firstEditable(from, dir int) int {
	for i := from; i >= 0 && i < len(f.fields); i += dir {
		if f.visible(i) {
			return i
		}
	}
	return from
}

func (f *formDlg) step(dir int) {
	for i := f.idx + dir; i >= 0 && i < len(f.fields); i += dir {
		if f.visible(i) {
			f.idx = i
			return
		}
	}
}

func (f *formDlg) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	fl := f.fields[f.idx]
	switch m := msg.(type) {
	case tea.PasteMsg:
		if fl.Kind == fText || fl.Kind == fSecret {
			fl.edit.Handle(m)
		}
	case tea.KeyPressMsg:
		ks := m.Keystroke()
		if h, ok := f.hotkeys[ks]; ok {
			return f, h(f.values())
		}
		switch ks {
		case "esc":
			return nil, nil
		case "enter":
			vals := f.values()
			if f.validate != nil {
				if err := f.validate(vals); err != nil {
					f.err = err.Error()
					return f, nil
				}
			}
			return nil, f.onSubmit(vals)
		case "tab", "down":
			f.step(1)
		case "shift+tab", "up":
			f.step(-1)
		default:
			switch fl.Kind {
			case fText, fSecret:
				fl.edit.Handle(m)
			case fBool:
				if ks == "space" || ks == "left" || ks == "right" {
					fl.Value = map[string]string{"true": "false"}[fl.Value]
					if fl.Value == "" {
						fl.Value = "true"
					}
				}
			case fChoice:
				n := len(fl.Options)
				cur := 0
				for i, o := range fl.Options {
					if o == fl.Value {
						cur = i
					}
				}
				switch ks {
				case "space", "right":
					fl.Value = fl.Options[(cur+1)%n]
				case "left":
					fl.Value = fl.Options[(cur+n-1)%n]
				}
			}
		}
		f.err = ""
		if f.onChange != nil {
			f.onChange(fl.Key, f.values())
		}
	}
	return f, nil
}

func (f *formDlg) View(th Theme, maxW, maxH int) string {
	w := min(maxW-4, 76)
	lw := 0
	for _, fl := range f.fields {
		lw = max(lw, width(fl.Label))
	}
	var rows []string
	focusRow := 0
	for i, fl := range f.fields {
		if fl.Kind != fLabel && !f.visible(i) {
			continue
		}
		if i == f.idx {
			focusRow = len(rows)
		}
		label := th.Dim.Render(fit(fl.Label, lw))
		if i == f.idx {
			label = th.Accent.Render(fit(fl.Label, lw))
		}
		vw := w - 4 - lw - 2
		var val string
		switch fl.Kind {
		case fText, fSecret:
			val = fl.edit.View(vw, th, i == f.idx)
		case fBool:
			val = "[ ]"
			if fl.Value == "true" {
				val = "[x]"
			}
		case fChoice:
			val = "‹ " + fl.Value + " ›"
		case fLabel:
			val = th.Dim.Render(fl.Value)
		}
		rows = append(rows, label+"  "+val)
	}
	vis := max(4, maxH-9)
	if focusRow < f.top {
		f.top = focusRow
	}
	if focusRow >= f.top+vis {
		f.top = focusRow - vis + 1
	}
	end := min(len(rows), f.top+vis)
	body := rows[f.top:end]
	if f.err != "" {
		body = append(body, "", th.Err.Render(f.err))
	}
	hint := f.submit + "   Esc: cancel   Tab/↑↓: move   Space/←→: toggle"
	if f.hotHint != "" {
		hint += "   " + f.hotHint
	}
	if len(rows) > vis {
		hint += fmt.Sprintf("   (%d/%d)", focusRow+1, len(rows))
	}
	return box(th, f.title, body, hint, w)
}

// ---- picker -----------------------------------------------------------------------------

type pickItem struct {
	Label  string
	Detail string
	Value  any
}

type pickerDlg struct {
	title   string
	items   []pickItem
	q       lineEdit
	filter  bool
	dynamic func(q string) []pickItem // when set, items are recomputed on every keystroke (Open Anything)
	sel     int
	top     int
	onPick  func(it pickItem, ctrl bool) tea.Cmd
	hint    string
	empty   string
}

func (p *pickerDlg) shown() []pickItem {
	if p.dynamic != nil {
		return p.items
	}
	if !p.filter || p.q.Value() == "" {
		return p.items
	}
	type sc struct {
		it pickItem
		s  int
	}
	var hits []sc
	for _, it := range p.items {
		if s, ok := matchName(p.q.Value(), it.Label, true); ok {
			hits = append(hits, sc{it, s})
		}
	}
	for i := 1; i < len(hits); i++ { // insertion sort: stable, lists are short
		for j := i; j > 0 && hits[j].s > hits[j-1].s; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	out := make([]pickItem, len(hits))
	for i, h := range hits {
		out[i] = h.it
	}
	return out
}

func (p *pickerDlg) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	items := p.shown()
	switch m := msg.(type) {
	case tea.PasteMsg:
		p.q.Handle(m)
		p.refresh()
	case tea.KeyPressMsg:
		switch ks := m.Keystroke(); ks {
		case "esc":
			return nil, nil
		case "enter", "ctrl+enter", "alt+enter":
			if len(items) == 0 {
				return p, nil
			}
			return nil, p.onPick(items[clamp(p.sel, 0, len(items)-1)], ks != "enter")
		case "down", "ctrl+n":
			p.sel = min(p.sel+1, len(items)-1)
		case "up", "ctrl+p":
			p.sel = max(p.sel-1, 0)
		case "pgdown":
			p.sel = min(p.sel+8, len(items)-1)
		case "pgup":
			p.sel = max(p.sel-8, 0)
		default:
			if p.filter && p.q.Handle(m) {
				p.refresh()
			}
		}
	}
	return p, nil
}

func (p *pickerDlg) refresh() {
	p.sel, p.top = 0, 0
	if p.dynamic != nil {
		p.items = p.dynamic(p.q.Value())
	}
}

func (p *pickerDlg) View(th Theme, maxW, maxH int) string {
	w := min(maxW-4, 78)
	items := p.shown()
	vis := clamp(maxH-10, 3, 14)
	p.sel = clamp(p.sel, 0, max(0, len(items)-1))
	if p.sel < p.top {
		p.top = p.sel
	}
	if p.sel >= p.top+vis {
		p.top = p.sel - vis + 1
	}
	var body []string
	if p.filter {
		body = append(body, th.Accent.Render("› ")+p.q.View(w-8, th, true), "")
	}
	for i := p.top; i < min(len(items), p.top+vis); i++ {
		it := items[i]
		line := fit(it.Label, w-4)
		if it.Detail != "" {
			dl := width(it.Detail)
			line = fit(it.Label, max(8, w-4-dl-2)) + "  " + th.Dim.Render(it.Detail)
		}
		if i == p.sel {
			line = th.SelRow.Render(fit(ansi.Strip(line), w-4))
		}
		body = append(body, line)
	}
	if len(items) == 0 {
		e := p.empty
		if e == "" {
			e = "no matches"
		}
		body = append(body, th.Dim.Render(e))
	}
	hint := p.hint
	if hint == "" {
		hint = "Enter: select   Esc: cancel   ↑↓: move"
	}
	return box(th, p.title, body, hint, w)
}

// ---- text view --------------------------------------------------------------------------

type viewAction struct {
	Key   string
	Label string
	Do    func() tea.Cmd
}

type textViewDlg struct {
	title   string
	lines   []string
	actions []viewAction
	top     int
	wrap    int
}

func (t *textViewDlg) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		ks := k.Keystroke()
		for _, a := range t.actions {
			if ks == a.Key {
				return nil, a.Do()
			}
		}
		switch ks {
		case "esc", "q":
			return nil, nil
		case "down", "j":
			t.top++
		case "up", "k":
			t.top = max(0, t.top-1)
		case "pgdown", "space":
			t.top += 10
		case "pgup":
			t.top = max(0, t.top-10)
		case "home", "g":
			t.top = 0
		case "end", "G":
			t.top = 1 << 30
		}
	}
	return t, nil
}

func (t *textViewDlg) View(th Theme, maxW, maxH int) string {
	w := min(maxW-4, 110)
	var all []string
	for _, l := range t.lines {
		all = append(all, wrapText(l, w-4)...)
	}
	vis := max(4, maxH-8)
	t.top = clamp(t.top, 0, max(0, len(all)-vis))
	body := all[t.top:min(len(all), t.top+vis)]
	hint := "Esc/q: close   ↑↓ PgUp/PgDn: scroll"
	for _, a := range t.actions {
		hint = a.Label + "   " + hint
	}
	if len(all) > vis {
		hint += fmt.Sprintf("   (%d-%d/%d)", t.top+1, t.top+len(body), len(all))
	}
	return box(th, t.title, body, hint, w)
}

// ---- checklist --------------------------------------------------------------------------

type checkItem struct {
	Label   string
	Checked bool
}

// checklistDlg is a multi-select list: Space toggles, a toggles all, Enter applies.
type checklistDlg struct {
	title string
	items []checkItem
	sel   int
	top   int
	onOK  func(items []checkItem) tea.Cmd
}

func (c *checklistDlg) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return c, nil
	}
	switch k.Keystroke() {
	case "esc":
		return nil, nil
	case "enter":
		return nil, c.onOK(c.items)
	case "down", "j":
		c.sel = min(c.sel+1, len(c.items)-1)
	case "up", "k":
		c.sel = max(c.sel-1, 0)
	case "space", "x":
		if len(c.items) > 0 {
			c.items[c.sel].Checked = !c.items[c.sel].Checked
		}
	case "a":
		all := true
		for _, it := range c.items {
			all = all && it.Checked
		}
		for i := range c.items {
			c.items[i].Checked = !all
		}
	}
	return c, nil
}

func (c *checklistDlg) View(th Theme, maxW, maxH int) string {
	w := min(maxW-4, 60)
	vis := clamp(maxH-9, 3, 18)
	if c.sel < c.top {
		c.top = c.sel
	}
	if c.sel >= c.top+vis {
		c.top = c.sel - vis + 1
	}
	var body []string
	for i := c.top; i < min(len(c.items), c.top+vis); i++ {
		mark := "[ ] "
		if c.items[i].Checked {
			mark = "[x] "
		}
		line := fit(mark+c.items[i].Label, w-4)
		if i == c.sel {
			line = th.SelRow.Render(line)
		}
		body = append(body, line)
	}
	return box(th, c.title, body, "Space: toggle   a: all/none   Enter: apply   Esc: cancel", w)
}
