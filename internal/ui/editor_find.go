// editor_find.go: Find and replace inside the query editor.

package ui

import (
	tea "charm.land/bubbletea/v2"
)

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
