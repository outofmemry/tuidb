package ui

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Overlay is a modal layer. Update returns (nil, cmd) to close itself.
type Overlay interface {
	Update(msg tea.Msg) (Overlay, tea.Cmd)
	View(th Theme, maxW, maxH int) string
}

// box wraps content in an accent frame with the title in the top edge and the key hints in the
// bottom edge (or as a last line when they are too long for it), so dialogs read like panes.
func box(th Theme, title string, content []string, hint string, w int) string {
	return boxEdge(th, th.Accent, title, content, hint, w)
}

func boxEdge(th Theme, edge lipgloss.Style, title string, content []string, hint string, w int) string {
	inner := w - 4
	lines := append([]string{""}, content...)
	if hint != "" {
		hint = th.hintChips(hint)
		if width(hint) > inner-2 {
			lines = append(lines, "", fit(hint, inner))
			hint = ""
		}
	}
	lines = append(lines, "")
	return frameEdge(th, edge, edge.Bold(true), title, hint, strings.Join(lines, "\n"), w, len(lines)+2)
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
		wrapped := wrapText(l, w-4)
		if sqlish(l) {
			// postgres rules: only '…' is a literal, so quoted identifiers do not read as strings
			for i, k := range lexLines("postgres", wrapped) {
				wrapped[i] = highlight(th, wrapped[i], k)
			}
		}
		body = append(body, wrapped...)
	}
	vis := max(3, maxH-12)
	d.scroll = clamp(d.scroll, 0, max(0, len(body)-vis))
	hint := ""
	if len(body) > vis {
		hint = fmt.Sprintf("↑↓: scroll   (%d-%d of %d lines)", d.scroll+1, min(len(body), d.scroll+vis), len(body))
		body = body[d.scroll:min(len(body), d.scroll+vis)]
	}
	label := d.yes
	if label == "" {
		label = "Yes"
	}
	// the choices are buttons: the action filled (red when destructive), cancel quiet, keys on both
	yes, no := th.TabActive, th.TabIdle
	yk, nk := "↵/y", "Esc/n"
	if d.danger {
		yes = yes.Background(th.Err.GetForeground())
		yk, nk = "y", "↵/Esc/n"
	}
	btns := yes.Render(" "+label+"  "+yk+" ") + "  " + no.Render(" Cancel  "+nk+" ")
	if len(body) > 0 {
		body = append(body, "")
	}
	body = append(body, padLeft(btns, w-4))
	if d.danger {
		return boxEdge(th, th.Err, "⚠ "+d.title, body, hint, w)
	}
	return box(th, d.title, body, hint, w)
}

// sqlish reports whether a confirm line is a statement rather than prose: it starts with a SQL
// keyword typed in one case ("UPDATE …", "select …"), which prose ("All of its tabs…") does not.
func sqlish(l string) bool {
	f := strings.Fields(l)
	if len(f) == 0 {
		return false
	}
	w := strings.TrimLeft(f[0], "(")
	return sqlKeywords[strings.ToUpper(w)] && (w == strings.ToUpper(w) || w == strings.ToLower(w))
}

// rowStyle puts st on the selection background for the selected row, so the styled pieces of a
// highlighted list row keep their colors without punching holes in the highlight.
func rowStyle(th Theme, sel bool) func(lipgloss.Style) lipgloss.Style {
	return func(st lipgloss.Style) lipgloss.Style {
		if sel {
			return st.Inherit(th.SelRow)
		}
		return st
	}
}

// listRow lays out one list line cw cells wide: a ▸ marker on the selected row, left, and right
// pushed to the right edge. Callers pre-truncate both parts so they fit.
func listRow(th Theme, sel bool, left, right string, cw int) string {
	on := rowStyle(th, sel)
	mark := "  "
	if sel {
		mark = on(th.Accent.Bold(true)).Render("▸ ")
	}
	return mark + left + on(lipgloss.NewStyle()).Render(strings.Repeat(" ", max(0, cw-2-width(left)-width(right)))) + right
}

// moreLine says how many list rows are scrolled out of view, or "" when all fit.
func moreLine(th Theme, above, below int) string {
	var p []string
	if above > 0 {
		p = append(p, fmt.Sprintf("↑ %d above", above))
	}
	if below > 0 {
		p = append(p, fmt.Sprintf("↓ %d more", below))
	}
	if len(p) == 0 {
		return ""
	}
	return th.Dim.Render("  (" + strings.Join(p, " · ") + ")")
}

// fuzzyMark renders s in base with the runes a fuzzy search for q matched (greedily, the way
// fuzzy() matches) in hit.
func fuzzyMark(q, s string, base, hit lipgloss.Style) string {
	p := []rune(strings.ToLower(q))
	var sb strings.Builder
	var run []rune
	on, pi := false, 0
	flush := func() {
		if len(run) > 0 {
			st := base
			if on {
				st = hit
			}
			sb.WriteString(st.Render(string(run)))
			run = run[:0]
		}
	}
	for _, c := range s {
		m := pi < len(p) && unicode.ToLower(c) == p[pi]
		if m {
			pi++
		}
		if m != on {
			flush()
			on = m
		}
		run = append(run, c)
	}
	flush()
	return sb.String()
}

// ---- form -------------------------------------------------------------------------------

type fieldKind int

const (
	fText fieldKind = iota
	fSecret
	fBool
	fChoice
	fLabel   // read-only line
	fHeading // section header: Value in the section style, a blank line before it
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
	return fl.Kind < fLabel && (fl.Show == nil || fl.Show(f.values()))
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
		if fl.Kind < fLabel && !f.visible(i) {
			continue
		}
		if fl.Kind == fHeading {
			if len(rows) > 0 {
				rows = append(rows, "")
			}
			rows = append(rows, th.Section.Render(fl.Value))
			continue
		}
		if i == f.idx {
			focusRow = len(rows)
		}
		mark, label := "  ", th.Dim.Render(fit(fl.Label, lw))
		if i == f.idx {
			mark, label = th.Accent.Render("▸ "), th.Accent.Bold(true).Render(fit(fl.Label, lw))
		}
		vw := w - 4 - lw - 4
		var val string
		switch fl.Kind {
		case fText, fSecret:
			val = fl.edit.View(vw, th, i == f.idx)
		case fBool:
			val = th.Dim.Render("[ ]")
			if fl.Value == "true" {
				val = "[" + th.OK.Bold(true).Render("x") + "]"
			}
		case fChoice:
			val = th.Dim.Render("‹ ") + th.Bold.Render(fl.Value) + th.Dim.Render(" ›")
			if i == f.idx {
				val = th.Accent.Render("‹ ") + th.Accent.Bold(true).Render(fl.Value) + th.Accent.Render(" ›")
			}
		case fLabel:
			val = th.Dim.Render(fl.Value)
			if sqlish(fl.Value) { // the statement a password prompt guards
				v := ansi.Truncate(fl.Value, vw, "…")
				val = highlight(th, v, lexLines("postgres", []string{v})[0])
			}
		}
		rows = append(rows, mark+label+"  "+val)
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
		body = append(body, "", th.Err.Render("✗ "+f.err))
	}
	title, secrets, inputs, toggles := f.title, 0, 0, 0
	for _, fl := range f.fields {
		if fl.Kind < fLabel {
			inputs++
		}
		switch fl.Kind {
		case fSecret:
			secrets++
		case fBool, fChoice:
			toggles++
		}
	}
	// only the keys this form can use: a one-field prompt has no fields to move between
	hint := f.submit + "   Esc: cancel"
	if inputs > 1 {
		hint += "   Tab/↑↓: move"
	}
	if toggles > 0 {
		hint += "   ←→: toggle"
	}
	if f.hotHint != "" {
		hint += "   " + f.hotHint
	}
	if len(rows) > vis {
		hint += fmt.Sprintf("   (%d/%d)", focusRow+1, len(rows))
	}
	if secrets > 0 && secrets == inputs { // a password prompt, not a form that happens to hold one
		title = "🔒 " + title
	}
	return box(th, title, body, hint, w)
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
	hint := p.hint
	if hint == "" {
		hint = "Enter: select   Esc: cancel   ↑↓: move"
	}
	if !p.filter { // a fixed menu is sized to its content; a filtered list keeps a steady width as it narrows
		need := max(width(p.title)+8, width(th.hintChips(hint))+8, 44)
		for _, it := range items {
			need = max(need, width(it.Label)+8+min(width(it.Detail), 40)+2)
		}
		w = min(w, need)
	}
	vis := clamp(maxH-10, 3, 14)
	p.sel = clamp(p.sel, 0, max(0, len(items)-1))
	if p.sel < p.top {
		p.top = p.sel
	}
	if p.sel >= p.top+vis {
		p.top = p.sel - vis + 1
	}
	cw := w - 4
	var body []string
	if p.filter {
		body = append(body, th.Accent.Bold(true).Render("› ")+p.q.View(cw-2, th, true), "")
	}
	q := p.q.Value()
	mark := p.filter && q != "" && !strings.HasPrefix(q, "^") && !strings.HasSuffix(q, "$")
	end := min(len(items), p.top+vis)
	for i := p.top; i < end; i++ {
		it, sel := items[i], i == p.sel
		on := rowStyle(th, sel)
		lw, det := cw-2, ""
		if it.Detail != "" {
			d := ansi.Truncate(it.Detail, max(0, cw-16), "…")
			lw -= width(d) + 2
			det = on(th.Dim).Render(d)
		}
		lbl := ansi.Truncate(ansi.Strip(it.Label), lw, "…")
		left := on(lipgloss.NewStyle()).Render(lbl)
		if mark {
			left = fuzzyMark(q, lbl, on(lipgloss.NewStyle()), on(th.Accent.Bold(true)))
		}
		body = append(body, listRow(th, sel, left, det, cw))
	}
	if m := moreLine(th, p.top, len(items)-end); m != "" {
		body = append(body, m)
	}
	if len(items) == 0 {
		e := p.empty
		if e == "" {
			e = "no matches"
		}
		body = append(body, th.Dim.Render("  "+e))
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
	danger  bool // an error: red frame
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
	if t.danger {
		return boxEdge(th, th.Err, t.title, body, hint, w)
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
	cw := w - 4
	var body []string
	end := min(len(c.items), c.top+vis)
	for i := c.top; i < end; i++ {
		on := rowStyle(th, i == c.sel)
		plain := on(lipgloss.NewStyle())
		chk := on(th.Dim).Render("[ ]")
		if c.items[i].Checked {
			chk = plain.Render("[") + on(th.OK.Bold(true)).Render("x") + plain.Render("]")
		}
		left := chk + plain.Render(" "+ansi.Truncate(c.items[i].Label, cw-6, "…"))
		body = append(body, listRow(th, i == c.sel, left, "", cw))
	}
	if m := moreLine(th, c.top, len(c.items)-end); m != "" {
		body = append(body, m)
	}
	return box(th, c.title, body, "Space: toggle   a: all   Enter: apply   Esc: cancel", w)
}
