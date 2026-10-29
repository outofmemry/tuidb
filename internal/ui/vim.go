// vim.go: Vim mode of the editor.

package ui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type vimState struct {
	on, insert bool
	vmode      rune   // 0 normal, 'v' char-wise visual, 'V' line-wise visual
	va         pos    // the visual anchor
	pend       string // first key of a two-key command (g) or the operator waiting for its motion (d y c > <)
	count      int    // digits typed so far
	opN        int    // the count typed before the operator
	cmdOn      bool   // the ":" command line is open
	cmd        string
	reg        string // last yank / delete
	regLine    bool
}

func (e *Editor) vimBadge() string {
	switch {
	case !e.vim.on:
		return ""
	case e.vim.cmdOn:
		return " [:]"
	case e.vim.insert:
		return " [I]"
	case e.vim.vmode == 'v':
		return " [V]"
	case e.vim.vmode == 'V':
		return " [VL]"
	}
	return " [N]"
}

// vimClamp keeps the normal-mode cursor on a character, not past the end of the line.
func (e *Editor) vimClamp() {
	if e.vim.vmode == 0 {
		e.anchor = nil
	}
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

// vimSel is the visual selection as a half-open range: 'v' includes the character under the cursor,
// 'V' covers whole lines.
func (e *Editor) vimSel() (pos, pos, bool) {
	a, b := e.vim.va, e.cur
	if b.row < a.row || b.row == a.row && b.col < a.col {
		a, b = b, a
	}
	if e.vim.vmode == 'V' {
		a.col, b.col = 0, runeLen(e.lines[b.row])
	} else {
		b.col = min(b.col+1, runeLen(e.lines[b.row]))
	}
	return a, b, true
}

func (e *Editor) vimVisual(mode rune) {
	if e.vim.vmode == mode {
		e.vimLeaveVisual()
		return
	}
	if e.vim.vmode == 0 {
		e.vim.va = e.cur
	}
	e.vim.vmode = mode
	e.anchor = &e.vim.va
}

func (e *Editor) vimLeaveVisual() {
	e.vim.vmode, e.anchor = 0, nil
	e.vimClamp()
}

// vmove moves the cursor; in visual mode the selection anchor stays put.
func (e *Editor) vmove(row, col int) {
	e.move(row, col, false)
	if e.vim.vmode != 0 {
		e.anchor = &e.vim.va
	}
}

// takeEx hands the owning tab the pending ":" command and message.
func (e *Editor) takeEx() (cmd, note string) {
	cmd, note, e.ex, e.note = e.ex, e.note, "", ""
	return
}

// vimMotion resolves a motion key n times from the cursor. kind: 'x' exclusive, 'i' inclusive, 'l' line-wise.
func (e *Editor) vimMotion(ch string, n int, counted bool) (to pos, kind byte, ok bool) {
	row, col := e.cur.row, e.cur.col
	r := e.runes(row)
	last := max(0, len(r)-1)
	switch ch {
	case "h":
		return pos{row, max(0, col-n)}, 'x', true
	case "l":
		return pos{row, min(last, col+n)}, 'x', true
	case "j":
		return pos{min(len(e.lines)-1, row+n), col}, 'l', true
	case "k":
		return pos{max(0, row-n), col}, 'l', true
	case "0":
		return pos{row, 0}, 'x', true
	case "^":
		c := 0
		for c < last && (r[c] == ' ' || r[c] == '\t') {
			c++
		}
		return pos{row, c}, 'x', true
	case "$":
		return pos{min(len(e.lines)-1, row+n-1), 0}.endOf(e), 'i', true
	case "G":
		t := len(e.lines) - 1
		if counted {
			t = clamp(n-1, 0, t)
		}
		return pos{t, 0}, 'l', true
	case "gg":
		t := 0
		if counted {
			t = clamp(n-1, 0, len(e.lines)-1)
		}
		return pos{t, 0}, 'l', true
	case "w", "b", "e":
		p := e.cur
		for i := 0; i < n; i++ {
			p = e.vimWord(ch, p)
		}
		k := byte('x')
		if ch == "e" {
			k = 'i'
		}
		return p, k, true
	}
	return pos{}, 0, false
}

// endOf is the last character of p's line.
func (p pos) endOf(e *Editor) pos { return pos{p.row, max(0, runeLen(e.lines[p.row])-1)} }

// vimWord moves one word: w to the next word start, b to the previous, e to the end of this/next word.
func (e *Editor) vimWord(ch string, p pos) pos {
	r := e.runes(p.row)
	last := max(0, len(r)-1)
	switch ch {
	case "w":
		c := p.col
		for c < len(r) && isIdentRune(r[c]) {
			c++
		}
		for c < len(r) && !isIdentRune(r[c]) {
			c++
		}
		if c >= len(r) && p.row < len(e.lines)-1 {
			return pos{p.row + 1, 0}
		}
		return pos{p.row, min(c, last)}
	case "b":
		c := p.col
		for c > 0 && !isIdentRune(r[c-1]) {
			c--
		}
		if c == 0 && p.col == 0 && p.row > 0 {
			return pos{p.row - 1, max(0, runeLen(e.lines[p.row-1])-1)}
		}
		return pos{p.row, wordStart(r, c)}
	}
	c := p.col + 1 // e
	for c < len(r) && !isIdentRune(r[c]) {
		c++
	}
	for c+1 < len(r) && isIdentRune(r[c+1]) {
		c++
	}
	if c >= len(r) {
		if p.row < len(e.lines)-1 {
			return pos{p.row + 1, 0}
		}
		return pos{p.row, last}
	}
	return pos{p.row, c}
}

// vimOp applies an operator to [a, b) (or to whole rows a.row..b.row when linewise).
func (e *Editor) vimOp(op string, a, b pos, linewise bool) {
	v := &e.vim
	switch op {
	case "y":
		if linewise {
			v.reg, v.regLine = strings.Join(e.lines[a.row:b.row+1], "\n"), true
		} else {
			v.reg, v.regLine = e.textBetween(a, b), false
		}
		e.cur = a
		return
	case ">", "<":
		e.push("indent")
		for r := a.row; r <= b.row; r++ {
			if op == ">" && e.lines[r] != "" {
				e.lines[r] = e.tab() + e.lines[r]
			} else if op == "<" {
				e.lines[r] = e.outdent(e.lines[r])
			}
		}
		e.cur = pos{a.row, 0}
		e.markChanged()
		return
	}
	e.push("delete") // d and c
	if linewise {
		v.reg, v.regLine = strings.Join(e.lines[a.row:b.row+1], "\n"), true
		ind := e.indentOf(a.row)
		e.lines = append(e.lines[:a.row:a.row], e.lines[b.row+1:]...)
		if op == "c" {
			e.lines = append(e.lines[:a.row], append([]string{ind}, e.lines[a.row:]...)...)
			e.cur = pos{a.row, runeLen(ind)}
		} else {
			if len(e.lines) == 0 {
				e.lines = []string{""}
			}
			e.cur = pos{min(a.row, len(e.lines)-1), 0}
		}
	} else {
		v.reg, v.regLine = e.textBetween(a, b), false
		ra, rb := e.runes(a.row), e.runes(b.row)
		e.lines = append(append(e.lines[:a.row:a.row], string(ra[:a.col])+string(rb[b.col:])), e.lines[b.row+1:]...)
		e.cur = a
	}
	e.markChanged()
	if op == "c" {
		v.insert = true
	}
}

// vimFinish tidies up after an operator: leaves visual mode and keeps the cursor on a character.
func (e *Editor) vimFinish() {
	e.vim.vmode, e.anchor = 0, nil
	if !e.vim.insert {
		e.vimClamp()
	}
	e.scroll()
}

// vimKey handles a key in vim mode; false lets the normal editor see it (arrows, ctrl keys, insert typing).
func (e *Editor) vimKey(m tea.KeyPressMsg) bool {
	v := &e.vim
	k := tea.Key(m)
	if v.cmdOn {
		e.vimCmdKey(m)
		return true
	}
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
			v.pend, v.count, v.opN = "", 0, 0
			if v.vmode != 0 {
				e.vimLeaveVisual()
			}
			return true
		case "enter", "tab", "shift+tab", "backspace", "delete": // editing keys do nothing in normal mode
			e.ac.open = false
			return true
		}
		return false
	}
	ch := k.Text
	// counts: 1-9 start one, 0 continues one (otherwise 0 is the line-start motion)
	if len(ch) == 1 && ch[0] >= '1' && ch[0] <= '9' || ch == "0" && v.count > 0 {
		v.count = min(v.count*10+int(ch[0]-'0'), 100000)
		return true
	}
	counted := v.count > 0
	n := max(1, v.count)
	if v.opN > 0 {
		if n > 10000/v.opN { // count x opN is capped: 99999d99999w would loop ~1e10 times on the UI goroutine
			v.pend, v.count, v.opN = "", 0, 0
			e.note = "count too large (max 10000)"
			return true
		}
		n *= v.opN
		counted = true
	}
	n = min(n, 10000)
	op := ""
	if v.pend != "" && v.pend != "g" {
		op = v.pend
	}
	if v.pend == "g" {
		v.pend, v.count = "", 0
		if ch == "g" {
			if to, _, ok := e.vimMotion("gg", n, counted); ok {
				e.vmove(to.row, e.cur.col)
			}
		}
		return true
	}
	v.pend, v.count, v.opN = "", 0, 0

	if op != "" { // an operator is waiting for its motion
		if ch == op || ch == "d" && op == "d" { // dd yy cc >> <<
			a, b := pos{e.cur.row, 0}, pos{min(len(e.lines)-1, e.cur.row+n-1), 0}
			e.vimOp(op, a, b, true)
			e.vimFinish()
			return true
		}
		to, kind, ok := e.vimMotion(ch, n, counted)
		if !ok {
			return true
		}
		a, b := e.cur, to
		if b.row < a.row || b.row == a.row && b.col < a.col {
			a, b = b, a
		}
		switch kind {
		case 'l':
			e.vimOp(op, a, b, true)
		default:
			if kind == 'i' {
				b.col = min(b.col+1, runeLen(e.lines[b.row]))
			}
			if ch == "w" && b.row > a.row { // dw on the last word of a line stops at the line end
				b = pos{a.row, runeLen(e.lines[a.row])}
			}
			e.vimOp(op, a, b, false)
		}
		e.vimFinish()
		return true
	}

	row, col := e.cur.row, e.cur.col
	r := e.runes(row)
	// motions (extend the selection in visual mode)
	if to, kind, ok := e.vimMotion(ch, n, counted); ok {
		e.vmove(to.row, to.col)
		if kind == 'l' && ch != "G" && ch != "gg" || ch == "$" {
			e.vimClamp()
		}
		if ch == "j" || ch == "k" {
			e.cur.col = min(col, max(0, runeLen(e.lines[e.cur.row])-1))
		}
		return true
	}
	if v.vmode != 0 {
		switch ch {
		case "g":
			v.pend = "g"
		case "v", "V":
			e.vimVisual(rune(ch[0]))
		case "d", "x", "y", "c", ">", "<":
			if ch == "x" {
				ch = "d"
			}
			a, b, _ := e.vimSel()
			e.vimOp(ch, a, b, v.vmode == 'V' || ch == ">" || ch == "<")
			e.vimFinish()
		case "o":
			v.va, e.cur = e.cur, v.va
			e.anchor = &v.va
		case ":":
			e.vimLeaveVisual()
			v.cmdOn, v.cmd = true, ""
		}
		return true
	}
	switch ch {
	case "g", "d", "y", "c", ">", "<":
		v.pend = ch
		if ch != "g" {
			v.opN = n
			if !counted {
				v.opN = 0
			}
		} else {
			v.count = 0
		}
	case "v", "V":
		e.vimVisual(rune(ch[0]))
	case "x":
		if len(r) > 0 {
			e.vimOp("d", e.cur, pos{row, min(len(r), col+n)}, false)
			e.vimFinish()
		}
	case "D", "C":
		e.vimOp(strings.ToLower(ch), e.cur, pos{row, len(r)}, false)
		e.vimFinish()
	case "p":
		e.push("paste")
		for i := 0; i < n && v.reg != ""; i++ {
			if v.regLine {
				at := e.cur.row + 1
				e.lines = append(e.lines[:at], append(strings.Split(v.reg, "\n"), e.lines[at:]...)...)
				e.cur = pos{at, 0}
			} else {
				e.cur.col = min(runeLen(e.lines[e.cur.row]), e.cur.col+1)
				e.Insert(v.reg)
				e.cur.col = max(0, e.cur.col-1)
			}
		}
		e.markChanged()
		e.vimClamp()
		e.scroll()
	case "u":
		for i := 0; i < n; i++ {
			e.Undo()
		}
		e.vimClamp()
	case "i":
		v.insert = true
	case "I":
		v.insert = true
		e.move(row, 0, false)
	case "a":
		v.insert = true
		e.move(row, min(len(r), col+1), false)
	case "A":
		v.insert = true
		e.move(row, len(r), false)
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
		v.cmdOn, v.cmd = true, ""
	} // any other letter is swallowed: typing in normal mode must not insert text
	return true
}

// vimCmdKey edits the ":" command line.
func (e *Editor) vimCmdKey(m tea.KeyPressMsg) {
	v := &e.vim
	switch ks := m.Keystroke(); {
	case ks == "esc":
		v.cmdOn = false
	case ks == "backspace":
		if v.cmd == "" {
			v.cmdOn = false
		} else {
			v.cmd = string([]rune(v.cmd)[:runeLen(v.cmd)-1])
		}
	case ks == "enter":
		v.cmdOn = false
		e.vimEx(v.cmd)
	default:
		if t := tea.Key(m).Text; t != "" && tea.Key(m).Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			v.cmd += t
		}
	}
}

// vimEx runs one ":" command. Editor-only ones (:<n>, :noh) are done here; :w :wq :x :q :q! are left in
// e.ex for the tab, which owns saving and closing.
func (e *Editor) vimEx(cmd string) {
	cmd = strings.TrimSpace(cmd)
	if n, err := strconv.Atoi(cmd); err == nil {
		e.vmove(clamp(n-1, 0, len(e.lines)-1), 0)
		e.vimClamp()
		return
	}
	switch cmd {
	case "":
	case "$":
		e.vmove(len(e.lines)-1, 0)
		e.vimClamp()
	case "noh", "nohlsearch":
		e.marks = ""
		e.note = "highlights cleared"
	case "w", "w!", "wq", "x", "q", "q!":
		e.ex = cmd
	default:
		e.note = "not an editor command: " + cmd
	}
}

// vimEx runs the tab-level ":" commands: :w saves the saved query (or asks where to save a new one),
// :q closes the tab (refused when dirty; :q! forces), :wq / :x do both.
func (t *queryTab) vimEx(cmd, note string) tea.Cmd {
	if cmd == "" {
		return t.ws.a.say("%s", note)
	}
	var cmds []tea.Cmd
	if strings.HasPrefix(cmd, "w") || cmd == "x" {
		if t.file != "" { // a tab opened from a .sql file writes back to it
			if err := t.writeBack(); err != nil {
				return t.ws.a.fail(err)
			}
			cmds = append(cmds, t.ws.a.say("saved %s", t.file))
		} else if t.savedAs != "" {
			text := t.ed.Text()
			if err := t.ws.a.d.Queries.Update(t.savedAs, text); err != nil {
				return t.ws.a.fail(err)
			}
			t.lastAuto = text
			cmds = append(cmds, t.ws.a.say("saved %s", t.savedAs))
		} else {
			// shortcut: :wq on a never-saved buffer only opens Save As; the tab stays open, run :q after saving
			return t.saveAs()
		}
	}
	if cmd == "q" || cmd == "q!" || cmd == "wq" || cmd == "x" {
		closeIt := func() tea.Cmd {
			if i := indexOf(t.ws.tabs, tabber(t)); i >= 0 {
				t.ws.closeTab(i)
			}
			return nil
		}
		if cmd == "q" && (t.fileDirty() || t.inTx) { // :q! closes without asking
			t.ws.askClose(t, closeIt)
			return tea.Batch(cmds...)
		}
		if cmd == "q" && t.Dirty() {
			return t.ws.a.say("unsaved changes (:q! discards them, :wq saves)")
		}
		closeIt()
	}
	return tea.Batch(cmds...)
}

// SelectAll selects the whole buffer.
func (e *Editor) SelectAll() {
	e.blk = nil
	last := len(e.lines) - 1
	e.anchor, e.cur = &pos{}, pos{last, runeLen(e.lines[last])}
	e.scroll()
}

// ClickAt puts the cursor under the screen cell (x, y) relative to the editor's top-left corner.
// shortcut: with folds or soft wrap on, it maps through the visible lines only for folds; wrapped rows land on their first chunk.
func (e *Editor) ClickAt(x, y int) {
	e.anchor, e.blk = nil, nil
	hid, _ := e.foldInfo()
	row, seen := e.top, 0
	for ; row < len(e.lines); row++ {
		if hid[row] {
			continue
		}
		h := 1
		if e.Opts.Wrap {
			h = e.chunks(row)
		}
		if y < seen+h {
			break
		}
		seen += h
	}
	row = min(row, len(e.lines)-1)
	col := x - e.gutterW()
	if e.Opts.Wrap {
		col += (y - seen) * e.textW()
	} else {
		col += e.left
	}
	e.cur = pos{row, clamp(col, 0, runeLen(e.lines[row]))}
	if e.vim.on && !e.vim.insert {
		e.vimClamp()
	}
	e.lastKind = ""
	e.scroll()
}
