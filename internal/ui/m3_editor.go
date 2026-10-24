package ui

import (
	"fmt"
)

// extra runs the milestone-3 editor actions and returns the message to show.
func (e *Editor) extra(name string) string {
	switch name {
	case "invisibles":
		e.Invis = !e.Invis
		return map[bool]string{true: "showing spaces (·) and tabs (→)", false: "invisible characters hidden"}[e.Invis]
	case "vim_mode":
		on, reg, regLine := !e.vim.on, e.vim.reg, e.vim.regLine
		e.vim = vimState{on: on, reg: reg, regLine: regLine} // visual and ":" mode must not survive the toggle
		e.anchor = nil
		if !e.vim.on {
			return "vim keys off"
		}
		e.vimClamp()
		return "vim keys on: NORMAL (motions with counts, v V visual, d y c > < p u i a o O, :w :q :wq :q! :<n> :noh, Esc)"
	}
	had := e.HasSelection()
	q := e.Selection()
	if q == "" {
		q = e.wordAtCursor()
	}
	if q == "" {
		return "put the cursor on a word or select some text first"
	}
	if name == "select_next_occurrence" {
		// shortcut: the editor has one cursor, so "next" jumps the selection to the next match
		// (wrapping, case-insensitive) instead of adding a second cursor.
		if !had {
			return "selected " + q
		}
		if !e.Find(q) {
			return fmt.Sprintf("%q is not in the editor", q)
		}
		return "next occurrence of " + q
	}
	e.marks = q
	n := 0
	for i := range e.lines {
		n += countMarks(e.markRow(e.runes(i)))
	}
	return fmt.Sprintf("%d occurrence(s) of %q highlighted (any key clears them)", n, q)
}

// countMarks counts the runs of true in m.
func countMarks(m []bool) int {
	n := 0
	for i, on := range m {
		if on && (i == 0 || !m[i-1]) {
			n++
		}
	}
	return n
}

// wordAtCursor selects and returns the identifier under the cursor ("" when there is none).
func (e *Editor) wordAtCursor() string {
	r := e.runes(e.cur.row)
	s, t := e.cur.col, e.cur.col
	for s > 0 && s <= len(r) && isIdentRune(r[s-1]) {
		s--
	}
	for t < len(r) && isIdentRune(r[t]) {
		t++
	}
	if s == t {
		return ""
	}
	e.anchor, e.cur = &pos{e.cur.row, s}, pos{e.cur.row, t}
	return string(r[s:t])
}

// markRow flags the runes of one line covered by an occurrence of e.marks (nil when none).
func (e *Editor) markRow(r []rune) []bool {
	q := lowerRunes(e.marks)
	if len(q) == 0 {
		return nil
	}
	var m []bool
	for c := 0; c+len(q) <= len(r); c++ {
		if matchAt(r, c, q) {
			if m == nil {
				m = make([]bool, len(r))
			}
			for i := range q {
				m[c+i] = true
			}
			c += len(q) - 1
		}
	}
	return m
}

// shown is the glyph drawn for r: tabs are one cell, and spaces / tabs get markers when invisibles are on.
func (e *Editor) shown(r rune) rune {
	switch {
	case r == '\t' && e.Invis:
		return '→'
	case r == '\t':
		return ' '
	case r == ' ' && e.Invis:
		return '·'
	}
	return r
}
