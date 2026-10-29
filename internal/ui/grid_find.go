// grid_find.go: Find in the grid and the search form, find again.

package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

func (w *Workspace) handleOverviewMsg(msg tea.Msg) (tea.Cmd, bool) {
	if m, ok := msg.(overviewMsg); ok {
		if m.err != nil {
			return w.a.fail(m.err), true
		}
		w.a.push(&textViewDlg{title: "Overview: " + m.ref.Name, lines: m.text})
		return nil, true
	}
	return nil, false
}

const anyColumn = "(any column)"

// findForm asks for the search (text, column, operator, match case) and, when the grid is editable, a
// replacement. Enter finds the next match; Alt+r replaces the match under the cursor and moves on, Alt+a
// replaces every match. spec keeps the last search so n / N repeat it.
func (a *App) findForm(title string, g *Grid, spec *FindSpec, canReplace bool) {
	cols := []string{anyColumn}
	for _, c := range g.Cols {
		cols = append(cols, c.Name)
	}
	col := anyColumn
	if spec.Col != "" {
		col = spec.Col
	}
	op := orDefault(spec.Op, FindContains)
	fields := []formField{
		{Key: "q", Label: "Text", Kind: fText, Value: spec.Text},
		{Key: "col", Label: "Column", Kind: fChoice, Options: cols, Value: col},
		{Key: "op", Label: "Match", Kind: fChoice, Options: FindOps, Value: op},
		{Key: "case", Label: "Match case", Kind: fBool, Value: fmt.Sprint(spec.Case)},
	}
	if g.Editable && canReplace {
		fields = append(fields, formField{Key: "with", Label: "Replace with", Kind: fText})
	}
	read := func(v map[string]string) FindSpec {
		s := FindSpec{Text: v["q"], Op: v["op"], Case: v["case"] == "true"}
		if v["col"] != anyColumn {
			s.Col = v["col"]
		}
		return s
	}
	f := newForm(title, fields, func(v map[string]string) tea.Cmd {
		*spec = read(v)
		if spec.Text == "" {
			return nil
		}
		if !g.FindSpec(*spec, 1) {
			return a.say("%q is not in the loaded rows", spec.Text)
		}
		return nil
	})
	f.hotHint = "Alt+c: match case"
	f.hotkeys = map[string]func(map[string]string) tea.Cmd{"alt+c": func(map[string]string) tea.Cmd {
		for _, fl := range f.fields {
			if fl.Key == "case" {
				fl.Value = fmt.Sprint(fl.Value != "true")
				f.title = caseTitle(title, fl.Value == "true")
			}
		}
		return nil
	}}
	f.title = caseTitle(title, spec.Case)
	if g.Editable && canReplace {
		f.hotHint += " · Alt+r: replace · Alt+a: replace all"
		for k, v := range map[string]func(map[string]string) tea.Cmd{
			"alt+r": func(v map[string]string) tea.Cmd {
				*spec = read(v)
				n, sk := g.ReplaceSkip(*spec, v["with"], false)
				g.FindSpec(*spec, 1)
				return a.say("replaced %d cell(s) (pending, not committed)%s", n, skipNote(sk))
			},
			"alt+a": func(v map[string]string) tea.Cmd {
				*spec = read(v)
				n, sk := g.ReplaceSkip(*spec, v["with"], true)
				return a.say("replaced %d cell(s) (pending, not committed)%s", n, skipNote(sk))
			},
		} {
			f.hotkeys[k] = v
		}
	}
	a.push(f)
}

func skipNote(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("; skipped %d key / non-text cell(s), pick the column to edit those", n)
}

func caseTitle(title string, on bool) string {
	return title + " · match case " + map[bool]string{true: "on", false: "off"}[on]
}

// findAgain is n / N: repeat spec in direction dir.
func (a *App) findAgain(g *Grid, spec FindSpec, dir int) tea.Cmd {
	if spec.Text == "" {
		return a.say("press / to search the loaded rows")
	}
	if !g.FindSpec(spec, dir) {
		return a.say("%q is not in the loaded rows", spec.Text)
	}
	return nil
}
