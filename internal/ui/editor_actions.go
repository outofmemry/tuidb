// editor_actions.go: Editor actions that are not plain typing: transform selection, marks, word at cursor, reformat.

package ui

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
	"webtuidb/internal/sqlfmt"
)

func (t *queryTab) reformat(pretty bool) tea.Cmd {
	if t.ws.caps.Editor != "sql" {
		return t.ws.a.say("beautify works on SQL editors only")
	}
	src := t.ed.Selection()
	if src == "" { // nothing selected: the statement under the cursor, else the whole buffer
		text := t.ed.Text()
		if a, b, ok := db.StatementRangeAt(t.ws.caps.Dialect, text, t.ed.Offset()); ok {
			a += len(text[a:]) - len(strings.TrimLeft(text[a:], " \t\r\n"))
			b = a + len(strings.TrimRight(text[a:b], " \t\r\n"))
			if b > a {
				t.ed.SelectBytes(a, b)
				src = text[a:b]
			}
		}
		if src == "" {
			src = text
		}
	}
	c := t.ws.a.d.Cfg.Editor
	out := sqlfmt.Uglify(src, t.ws.caps.Dialect)
	if pretty {
		out = sqlfmt.Beautify(src, sqlfmt.Options{Indent: c.Indent, Width: c.IndentWidth, Uppercase: c.UppercaseKeywords, Dialect: t.ws.caps.Dialect, IndentType: c.IndentType})
	}
	t.ed.Replace(out)
	return t.afterEdit()
}

// extra runs the extra editor actions (marks, word under cursor) and returns the message to show.
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

// transform upper-cases, lower-cases or capitalises the selection (the word under the cursor without
// one) and keeps it selected. It returns the message to show.
func (e *Editor) transform(name string) string {
	if !e.HasSelection() && e.wordAtCursor() == "" {
		return "select some text or put the cursor on a word first"
	}
	a, _, _ := e.selRange()
	s := e.Selection()
	if s == "" { // never Replace("") : that wipes the whole buffer
		return "select some text or put the cursor on a word first"
	}
	switch name {
	case "upper_case":
		s = strings.ToUpper(s)
	case "lower_case":
		s = strings.ToLower(s)
	default: // capitalize: first letter of each word up, the rest down
		up := true
		s = strings.Map(func(r rune) rune {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				up = true
				return r
			}
			if up {
				up = false
				return unicode.ToUpper(r)
			}
			return unicode.ToLower(r)
		}, s)
	}
	e.Replace(s)
	e.anchor = &a
	return ""
}
