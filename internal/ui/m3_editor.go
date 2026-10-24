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

// ---- vim keys ---------------------------------------------------------------------------

type vimState struct {
	on, insert bool
	pend       string // first key of a two-key command (g, d, y)
	reg        string // last yank / delete
	regLine    bool
}

func (e *Editor) vimBadge() string {
	switch {
	case !e.vim.on:
		return ""
	case e.vim.insert:
		return " [I]"
	}
	return " [N]"
}

// vimClamp keeps the normal-mode cursor on a character, not past the end of the line.
func (e *Editor) vimClamp() {
	e.anchor = nil
	e.cur.col = clamp(e.cur.col, 0, max(0, runeLen(e.lines[e.cur.row])-1))
}

// vimEsc leaves insert mode (the cursor steps back one, like vim).
func (e *Editor) vimEsc() {
	if e.vim.on && e.vim.insert {
		e.vim.insert = false
		e.ac.open = false
		e.cur.col = max(0, e.cur.col-1)
		e.vimClamp()
	}
}

// vimKey handles a key in vim mode; false lets the normal editor see it (arrows, ctrl keys, insert typing).
func (e *Editor) vimKey(m tea.KeyPressMsg) bool {
	v := &e.vim
	k := tea.Key(m)
	if v.insert {
		if m.Keystroke() == "esc" {
			e.vimEsc()
			return true
		}
		return false
	}
	if k.Text == "" || k.Mod&(tea.ModCtrl|tea.ModAlt) != 0 {
		switch m.Keystroke() {
		case "esc":
			v.pend = ""
			return true
		case "enter", "tab", "shift+tab", "backspace", "delete": // editing keys do nothing in normal mode
			e.ac.open = false
			return true
		}
		return false
	}
	row, col := e.cur.row, e.cur.col
	r := e.runes(row)
	last := max(0, len(r)-1)
	ch := k.Text
	if v.pend != "" {
		p := v.pend + ch
		v.pend = ""
		switch p {
		case "gg":
			e.move(0, col, false)
		case "dd":
			e.push("delete")
			v.reg, v.regLine = e.lines[row], true
			if len(e.lines) == 1 {
				e.lines[0] = ""
			} else {
				e.lines = append(e.lines[:row], e.lines[row+1:]...)
			}
			e.cur = pos{min(row, len(e.lines)-1), 0}
			e.markChanged()
			e.vimClamp()
			e.scroll()
		case "yy":
			v.reg, v.regLine = e.lines[row], true
		}
		return true
	}
	switch ch {
	case "g", "d", "y":
		v.pend = ch
	case "h":
		e.move(row, max(0, col-1), false)
	case "l":
		e.move(row, min(last, col+1), false)
	case "j":
		e.move(row+1, col, false)
		e.vimClamp()
	case "k":
		e.move(row-1, col, false)
		e.vimClamp()
	case "0":
		e.move(row, 0, false)
	case "$":
		e.move(row, last, false)
	case "G":
		e.move(len(e.lines)-1, 0, false)
	case "w":
		c := col
		for c < len(r) && isIdentRune(r[c]) {
			c++
		}
		for c < len(r) && !isIdentRune(r[c]) {
			c++
		}
		if c >= len(r) && row < len(e.lines)-1 {
			e.move(row+1, 0, false)
		} else {
			e.move(row, min(c, last), false)
		}
	case "b":
		c := col
		for c > 0 && !isIdentRune(r[c-1]) {
			c--
		}
		if c == 0 && col == 0 && row > 0 {
			e.move(row-1, max(0, runeLen(e.lines[row-1])-1), false)
		} else {
			e.move(row, wordStart(r, c), false)
		}
	case "x":
		if len(r) > 0 {
			e.push("delete")
			v.reg, v.regLine = string(r[col]), false
			e.lines[row] = string(r[:col]) + string(r[col+1:])
			e.markChanged()
			e.vimClamp()
		}
	case "p":
		e.push("paste")
		if v.regLine {
			e.lines = append(e.lines[:row+1], append([]string{v.reg}, e.lines[row+1:]...)...)
			e.cur = pos{row + 1, 0}
		} else if v.reg != "" {
			c := min(len(r), col+1)
			e.lines[row] = string(r[:c]) + v.reg + string(r[c:])
			e.cur.col = c + runeLen(v.reg) - 1
		}
		e.markChanged()
		e.scroll()
	case "u":
		e.Undo()
		e.vimClamp()
	case "i":
		v.insert = true
	case "a":
		v.insert = true
		e.move(row, min(len(r), col+1), false)
	case "o", "O":
		e.push("enter")
		at := row + 1
		if ch == "O" {
			at = row
		}
		ind := e.indentOf(row)
		e.lines = append(e.lines[:at], append([]string{ind}, e.lines[at:]...)...)
		e.cur = pos{at, runeLen(ind)}
		e.markChanged()
		v.insert = true
		e.scroll()
	case ":":
		// shortcut: no ":" commands; save, quit and run are prefix keys
		v.pend = ""
	} // any other letter is swallowed: typing in normal mode must not insert text
	return true
}
