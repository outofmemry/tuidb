package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/keymap"
)

// M15.30 settings editor: the `settings` action opens a form over config.Schema (grouped by section, validated by
// config.ApplyValues when saved) with a second tab that edits key bindings (config.WriteKeys, after a dry run of
// the keymap that shows a conflict). "Open in $EDITOR" (e) keeps the old hand-off.

type settingsDlg struct {
	a       *App
	fields  []config.Field
	pending map[string]string // "section.key" -> new value, written by s
	keys    bool              // the key-bindings tab
	sel     int
	top     int
	q       lineEdit
	find    bool
	msg     string // the last save / validation message
	msgErr  bool
}

func (a *App) settingsDialog() tea.Cmd { // replaces the $EDITOR hand-off of m7.go (App.settings, kept for "e")
	a.push(&settingsDlg{a: a, fields: config.Schema(a.d.Cfg), pending: map[string]string{}})
	return nil
}

// rowKind of one list line.
type setRow struct {
	header string
	f      *config.Field
	b      *keymap.Binding
}

func (s *settingsDlg) rows() []setRow {
	q := strings.ToLower(strings.TrimSpace(s.q.Value()))
	var out []setRow
	if s.keys {
		bs := s.a.d.Keys.All()
		sort.SliceStable(bs, func(i, j int) bool {
			if bs[i].Context != bs[j].Context {
				return bs[i].Context < bs[j].Context
			}
			return bs[i].Action < bs[j].Action
		})
		last := ""
		for i := range bs {
			b := bs[i]
			if q != "" && !strings.Contains(strings.ToLower(b.Action+" "+b.Desc+" "+b.Key), q) {
				continue
			}
			if b.Context != last {
				out = append(out, setRow{header: strings.ToUpper(b.Context[:1]) + b.Context[1:]})
				last = b.Context
			}
			out = append(out, setRow{b: &b})
		}
		return out
	}
	last := ""
	for i := range s.fields {
		f := &s.fields[i]
		if q != "" && !strings.Contains(strings.ToLower(f.Key+" "+f.Doc), q) {
			continue
		}
		if f.Section != last {
			out = append(out, setRow{header: "[" + f.Section + "]"})
			last = f.Section
		}
		out = append(out, setRow{f: f})
	}
	return out
}

func (s *settingsDlg) value(f *config.Field) string {
	if v, ok := s.pending[f.Key]; ok {
		return v
	}
	return f.Value
}

func (s *settingsDlg) snap(d int) {
	rows := s.rows()
	s.sel = clamp(s.sel, 0, max(0, len(rows)-1))
	for i := s.sel; i >= 0 && i < len(rows); i += d {
		if rows[i].header == "" {
			s.sel = i
			return
		}
	}
	for i := s.sel; i >= 0 && i < len(rows); i -= d { // nothing that way: the other way
		if rows[i].header == "" {
			s.sel = i
			return
		}
	}
}

func (s *settingsDlg) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if p, ok := msg.(tea.PasteMsg); ok && s.find {
		s.q.Handle(p)
		s.sel = 0
		s.snap(1)
		return s, nil
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return s, nil
	}
	ks := k.Keystroke()
	if s.find {
		switch ks {
		case "esc":
			s.find = false
			s.q.Set("")
		case "enter":
			s.find = false
		case "down":
			s.sel++
			s.snap(1)
		case "up":
			s.sel--
			s.snap(-1)
		default:
			if s.q.Handle(k) {
				s.sel = 0
				s.snap(1)
			}
		}
		return s, nil
	}
	switch ks {
	case "esc", "q":
		if len(s.pending) > 0 {
			s.a.push(&confirmDlg{title: "Discard " + plural(len(s.pending), "unsaved change") + "?", yes: "Discard", danger: true, onYes: func() tea.Cmd {
				s.pending = map[string]string{}
				return func() tea.Msg { return tea.KeyPressMsg{Code: tea.KeyEscape} } // Esc again, now with nothing pending
			}})
			return s, nil
		}
		return nil, nil
	case "down", "j", "ctrl+n":
		s.sel++
		s.snap(1)
	case "up", "k", "ctrl+p":
		s.sel--
		s.snap(-1)
	case "pgdown":
		s.sel += 10
		s.snap(-1)
	case "pgup":
		s.sel -= 10
		s.snap(1)
	case "tab":
		s.keys, s.sel, s.top = !s.keys, 0, 0
		s.snap(1)
	case "/":
		s.find = true
	case "s", "ctrl+s":
		return s, s.guardComments(s.save)
	case "r": // revert the field under the cursor
		if r := s.rows(); s.sel < len(r) && r[s.sel].f != nil {
			delete(s.pending, r[s.sel].f.Key)
		}
	case "e":
		return s, s.a.settingsEditor()
	case "enter", "space":
		rows := s.rows()
		if s.sel >= len(rows) {
			return s, nil
		}
		if r := rows[s.sel]; r.f != nil {
			return s, s.edit(r.f)
		} else if r.b != nil {
			return s, s.editKey(r.b.Action)
		}
	}
	return s, nil
}

// edit changes one setting: a bool flips, an enum picks, anything else asks in a small form.
func (s *settingsDlg) edit(f *config.Field) tea.Cmd {
	set := func(v string) {
		if v == f.Value {
			delete(s.pending, f.Key)
		} else {
			s.pending[f.Key] = v
		}
		s.msg = ""
	}
	switch f.Type {
	case "bool":
		set(strconv.FormatBool(s.value(f) != "true"))
	case "enum":
		var items []pickItem
		for _, e := range f.Enum {
			mark := ""
			if e == s.value(f) {
				mark = "current"
			}
			items = append(items, pickItem{Label: e, Detail: mark, Value: e})
		}
		s.a.push(&pickerDlg{title: f.Key, items: items, filter: true, onPick: func(it pickItem, _ bool) tea.Cmd {
			set(it.Value.(string))
			return nil
		}})
	default:
		form := newForm(f.Key, []formField{
			{Key: "v", Label: "Value", Kind: fText, Value: s.value(f), Hint: map[string]string{"list": "comma-separated", "int": "a whole number"}[f.Type]},
			{Kind: fLabel, Value: f.Doc},
		}, func(v map[string]string) tea.Cmd { set(v["v"]); return nil })
		form.validate = func(v map[string]string) error {
			if f.Type == "int" {
				if _, err := strconv.ParseInt(strings.TrimSpace(v["v"]), 10, 64); err != nil {
					return fmt.Errorf("%s: %q is not a whole number", f.Key, v["v"])
				}
			}
			return nil
		}
		s.a.push(form)
	}
	return nil
}

// guardComments runs do, after a warning when the save would delete the user's comments (P2-77).
func (s *settingsDlg) guardComments(do func() tea.Cmd) tea.Cmd {
	if !config.HasComments(s.a.cfgPath()) {
		return do()
	}
	s.a.push(&confirmDlg{title: "Saving drops the comments in config.toml", yes: "Save anyway", danger: true,
		lines: []string{"The form rewrites the file and does not keep comments.", "Press n, then e to edit the file by hand in $EDITOR instead: that keeps them."},
		onYes: do})
	return nil
}

// save writes the pending values; the engine's validation message (ApplyValues) is shown here when it refuses.
func (s *settingsDlg) save() tea.Cmd {
	if len(s.pending) == 0 {
		s.msg, s.msgErr = "nothing to save", false
		return nil
	}
	if err := config.ApplyValues(s.a.cfgPath(), s.pending); err != nil {
		s.msg, s.msgErr = err.Error(), true
		return nil
	}
	n := len(s.pending)
	s.pending = map[string]string{}
	note, err := s.a.reloadConfig()
	if err != nil {
		s.msg, s.msgErr = "saved, but reloading failed: "+err.Error(), true
		return nil
	}
	s.fields = config.Schema(s.a.d.Cfg)
	s.msg, s.msgErr = fmt.Sprintf("saved %s. %s", plural(n, "setting"), note), false
	return nil
}

// editKey rebinds one action: the keymap is built with the change first (a dry run), so a conflict shows in the
// form; then config.WriteKeys stores it and the running keymap is replaced.
func (s *settingsDlg) editKey(action string) tea.Cmd {
	cur := ""
	for _, b := range s.a.d.Keys.All() {
		if b.Action == action {
			cur = b.Key
			break
		}
	}
	overrides := func(spec string) map[string][]string {
		out := map[string][]string{}
		for a, v := range s.a.d.Cfg.KeyOverrides {
			out[a] = v
		}
		if spec == "" {
			delete(out, action)
		} else {
			out[action] = []string{spec}
		}
		return out
	}
	form := newForm("Key for "+action, []formField{
		{Key: "key", Label: "Key", Kind: fText, Value: cur, Hint: "e.g. alt+x or ctrl+g; empty restores the default"},
		{Kind: fLabel, Value: "Pressed after the prefix (" + keymap.Display(s.a.d.Keys.Prefix) + ") unless the binding is a no-prefix one."},
	}, func(v map[string]string) tea.Cmd {
		spec := strings.TrimSpace(v["key"])
		return s.guardComments(func() tea.Cmd {
			if err := config.WriteKeys(s.a.cfgPath(), overrides(spec), pluginBindings()...); err != nil {
				return s.a.fail(err)
			}
			note, err := s.a.reloadConfig()
			if err != nil {
				s.msg, s.msgErr = err.Error(), true
				return nil
			}
			s.msg, s.msgErr = action+" rebound. "+note, false
			return nil
		})
	})
	form.validate = func(v map[string]string) error { // the dry run
		c := s.a.d.Cfg
		c.KeyOverrides = overrides(strings.TrimSpace(v["key"]))
		nm, _, err := c.KeymapChecked(pluginBindings())
		if err != nil {
			return err
		}
		return keymap.TakenFrom(s.a.d.Keys, nm, action)
	}
	s.a.push(form)
	return nil
}

func (s *settingsDlg) View(th Theme, maxW, maxH int) string {
	w := min(maxW-4, 96)
	cw := w - 4
	vis := clamp(maxH-14, 4, 24)
	rows := s.rows()
	s.sel = clamp(s.sel, 0, max(0, len(rows)-1))
	if s.sel < s.top {
		s.top = s.sel
		if s.top > 0 && rows[s.top-1].header != "" {
			s.top--
		}
	}
	if s.sel >= s.top+vis {
		s.top = s.sel - vis + 1
	}
	tabs := th.chip("Settings")
	if s.keys {
		tabs = th.chip("Key bindings")
	}
	body := []string{tabs + th.Dim.Render("   Tab: "+map[bool]string{false: "key bindings", true: "settings"}[s.keys]+"    "+s.a.cfgPath())}
	if s.find || s.q.Value() != "" {
		body = append(body, th.Accent.Bold(true).Render("/ ")+s.q.View(cw-2, th, s.find))
	} else {
		body = append(body, "")
	}
	end := min(len(rows), s.top+vis)
	for i := s.top; i < end; i++ {
		r := rows[i]
		switch {
		case r.header != "":
			body = append(body, th.Section.Render(r.header))
		case r.f != nil:
			on := rowStyle(th, i == s.sel)
			val := s.value(r.f)
			mark := " "
			if _, ch := s.pending[r.f.Key]; ch {
				mark = th.Warn.Render("*")
			}
			if val == "" {
				val = "(empty)"
			}
			left := mark + on(th.Accent).Render(fit(r.f.Key, 30))
			body = append(body, listRow(th, i == s.sel, left+" ", on(th.Bold).Render(fit(val, max(6, cw-34))), cw))
		case r.b != nil:
			on := rowStyle(th, i == s.sel)
			custom := ""
			if _, ok := s.a.d.Cfg.KeyOverrides[r.b.Action]; ok {
				custom = " ●"
			}
			left := " " + th.chip(fit(keymap.Display(r.b.Key), 12)) + " " + on(th.Bold).Render(fit(r.b.Action, 24))
			body = append(body, listRow(th, i == s.sel, left, on(th.Dim).Render(fit(r.b.Desc+custom, max(6, cw-42))), cw))
		}
	}
	if len(rows) == 0 {
		body = append(body, th.Dim.Render("  nothing matches"))
	}
	doc := ""
	if s.sel < len(rows) && rows[s.sel].f != nil {
		doc = rows[s.sel].f.Doc
	}
	body = append(body, "")
	for _, l := range wrapText(doc, cw)[:min(2, len(wrapText(doc, cw)))] {
		body = append(body, th.Dim.Render(l))
	}
	if s.msg != "" {
		st := th.OK
		if s.msgErr {
			st = th.Err
		}
		for _, l := range wrapText(s.msg, cw) {
			body = append(body, st.Render(l))
		}
	}
	hint := "↵: edit   s: save   r: revert   /: find   Tab: keys   e: $EDITOR   Esc: close"
	if s.keys {
		hint = "↵: rebind   /: find   Tab: settings   e: $EDITOR   Esc: close"
	}
	if s.find {
		hint = "type to filter   ↵: keep   Esc: clear"
	}
	title := "Settings"
	if n := len(s.pending); n > 0 {
		title += fmt.Sprintf(" · %d unsaved", n)
	}
	return box(th, title, body, hint, w)
}

// settingsEditor is the old behaviour: the config file in $VISUAL / $EDITOR.
func (a *App) settingsEditor() tea.Cmd { return a.settings() }

// reloadConfig reads config.toml again and applies what the running app can take over: the theme, the key
// bindings (plugins included) and the options read at use time. It returns the notice for the user.
func (a *App) reloadConfig() (string, error) {
	cfg, err := config.Load(a.cfgPath())
	if err != nil {
		return "", err
	}
	km, conflicts, err := cfg.KeymapChecked(pluginBindings())
	if err != nil {
		return "", err
	}
	a.logPluginConflicts(conflicts)
	a.d.Cfg, a.d.Keys = cfg, km
	a.machine = keymap.Machine{Map: km}
	a.th = NewTheme(cfg.Theme)
	copyCSV, autoPrettyJSON = cfg.CSV, cfg.Table.AutoPrettyJSON
	return "Theme, keys and most options are live; editor options apply to new tabs; restart to apply the MCP and web settings.", nil
}
