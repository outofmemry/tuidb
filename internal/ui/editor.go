package ui

import (
	"fmt"
	"hash/fnv"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"webtuidb/internal/db"
)

type pos struct{ row, col int }

// Candidate is one autocomplete suggestion.
type Candidate struct {
	Text   string
	Kind   string // keyword | table | column | function | saved
	Insert string // when set, accepting inserts this instead of Text (saved queries expand to their SQL)
}

type EditorOpts struct {
	TabWidth     int
	UseTabs      bool
	CloseBraces  bool
	Uppercase    bool
	CompleteKey  string // enter_or_tab | enter | tab
	Highlight    bool   // tint the statement under the cursor
	Autocomplete bool
	Wrap         bool // soft-wrap long lines instead of scrolling sideways (wrap_lines toggles it)
	AddSpace     bool // accepting a completion appends a space
}

type editSnap struct {
	lines []string
	cur   pos
}

// Editor is the multi-line SQL editor widget.
type Editor struct {
	lines     []string
	cur       pos
	anchor    *pos
	blk       *pos // block mode (block_select): the fixed corner of the rectangle the cursor stretches
	top, left int
	W, H      int
	Dialect   string
	Opts      EditorOpts
	Complete  func(prefix string) []Candidate

	undo, redo []editSnap
	lastKind   string
	ac         struct {
		open  bool
		items []Candidate
		sel   int
	}
	Changed bool   // set on any text change; the owner resets it
	Invis   bool   // show spaces as · and tabs as →
	marks   string // every occurrence of this text is highlighted (select_all_occurrences)
	vim     vimState
	curY    int // visual row of the cursor in the last View, -1 when off screen (anchors the completion popup)
	curFrom int // first rune shown on that row

	// folds is keyed by stmtKey (a hash of the trimmed statement text), not by index, so an edit above a
	// fold keeps it. OnFolds still reports []int statement indexes (the state.json format), mapped back
	// from the hashes with the current statement list.
	folds    map[uint64]bool
	OnFolds  func([]int) // called with the folded indexes whenever they change
	lastFold []int

	ex, note string // vim ":" results for the owning tab: a command it must run (:w :q ...) and a message to show

	// render cache: lexing and statement splitting are redone only when the text changes, and
	// lexing stops at the bottom of the viewport (a frame is drawn after every message, not just keys)
	cache struct {
		dirty bool
		offs  []int // byte offset of every line start, for the current-statement highlight
		text  string
		kinds [][]tokKind // for lines[:len(kinds)]
		stmts []db.Stmt
		split bool
	}
}

func NewEditor(dialect string, opts EditorOpts) *Editor {
	if opts.TabWidth == 0 {
		opts.TabWidth = 4
	}
	return &Editor{lines: []string{""}, Dialect: dialect, Opts: opts}
}

func (e *Editor) Text() string { return strings.Join(e.lines, "\n") }

// markChanged flags the buffer for the owner (Changed) and the render cache (dirty).
func (e *Editor) markChanged() {
	e.Changed, e.cache.dirty = true, true
	if len(e.folds) > 0 { // an edited statement hashes differently and unfolds; the others keep their fold
		keep := map[uint64]bool{}
		for _, s := range e.stmts() {
			if k := stmtKey(s); e.folds[k] {
				keep[k] = true
			}
		}
		e.folds = keep
		if cur := e.Folded(); !slices.Equal(cur, e.lastFold) {
			e.foldsChanged()
		}
	}
}

// stmtKey identifies a statement for folding: FNV-1a of its trimmed text. Identical statements share
// one key, so folding one folds its twins (shortcut: acceptable for SQL scripts, per-occurrence keys if it bites).
func stmtKey(s db.Stmt) uint64 {
	h := fnv.New64a()
	h.Write([]byte(strings.TrimSpace(s.Text)))
	return h.Sum64()
}

// stmts is the statement list for the current text, cached with the render cache.
func (e *Editor) stmts() []db.Stmt {
	c := &e.cache
	if c.dirty || !c.split {
		c.stmts, c.split = db.Split(e.Dialect, e.Text()), true
	}
	return c.stmts
}

func (e *Editor) SetText(s string) {
	s = strings.ToValidUTF8(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"), "\uFFFD")
	e.lines = strings.Split(s, "\n")
	e.cache.dirty = true
	e.folds, e.lastFold = nil, nil
	e.cur, e.anchor, e.top, e.left = pos{}, nil, 0, 0
	e.undo, e.redo, e.lastKind = nil, nil, ""
	e.blk = nil
	e.ac.open = false
}

func (e *Editor) runes(r int) []rune { return []rune(e.lines[r]) }

// fixAnchor keeps the selection anchor (and cursor) inside the text after an edit that shortened it.
func (e *Editor) fixAnchor() {
	e.cur.row = clamp(e.cur.row, 0, len(e.lines)-1)
	e.cur.col = clamp(e.cur.col, 0, runeLen(e.lines[e.cur.row]))
	if e.blk != nil && e.blk.row >= len(e.lines) {
		e.blk = nil
	}
	if e.anchor == nil {
		return
	}
	if e.anchor.row >= len(e.lines) {
		e.anchor = nil
		return
	}
	e.anchor.col = clamp(e.anchor.col, 0, runeLen(e.lines[e.anchor.row]))
}

// Offset is the byte offset of the cursor in Text(), measured on the stored bytes so it matches
// what Split sees (re-encoding runes would inflate invalid UTF-8).
func (e *Editor) Offset() int {
	n := 0
	for i := 0; i < e.cur.row; i++ {
		n += len(e.lines[i]) + 1
	}
	line, idx := e.lines[e.cur.row], 0
	for i := 0; i < e.cur.col && idx < len(line); i++ {
		_, size := utf8.DecodeRuneInString(line[idx:])
		idx += size
	}
	return n + idx
}

// Selection returns the selected text ("" when none).
func (e *Editor) Selection() string {
	if e.blk != nil {
		if _, _, _, _, ok := e.blockRect(); ok {
			return e.blockText()
		}
		return ""
	}
	a, b, ok := e.selRange()
	if !ok {
		return ""
	}
	return e.textBetween(a, b)
}

func (e *Editor) HasSelection() bool {
	if e.blk != nil {
		_, _, _, _, ok := e.blockRect()
		return ok
	}
	_, _, ok := e.selRange()
	return ok
}

func (e *Editor) selRange() (pos, pos, bool) {
	if e.anchor == nil || *e.anchor == e.cur {
		return pos{}, pos{}, false
	}
	a, b := *e.anchor, e.cur
	if b.row < a.row || b.row == a.row && b.col < a.col {
		a, b = b, a
	}
	return a, b, true
}

func (e *Editor) textBetween(a, b pos) string {
	if a.row == b.row {
		return string(e.runes(a.row)[a.col:b.col])
	}
	parts := []string{string(e.runes(a.row)[a.col:])}
	for r := a.row + 1; r < b.row; r++ {
		parts = append(parts, e.lines[r])
	}
	return strings.Join(append(parts, string(e.runes(b.row)[:b.col])), "\n")
}

// CurrentStatement is the statement under the cursor (what "Run Current" executes).
func (e *Editor) CurrentStatement() (db.Stmt, bool) {
	return db.StatementAt(db.Split(e.Dialect, e.Text()), e.Offset())
}

func (e *Editor) push(kind string) {
	if kind == "type" && e.lastKind == "type" {
		return
	}
	e.undo = append(e.undo, editSnap{lines: append([]string(nil), e.lines...), cur: e.cur})
	depth := 200
	if len(e.lines) > 20000 { // a pasted dump: each snapshot is a line-slice copy, keep few
		depth = 10
	}
	if len(e.undo) > depth {
		e.undo = e.undo[len(e.undo)-depth:]
	}
	e.redo, e.lastKind = nil, kind
}

func (e *Editor) deleteSelection() bool {
	if e.blk != nil {
		return e.deleteBlock()
	}
	a, b, ok := e.selRange()
	if !ok {
		return false
	}
	ra, rb := e.runes(a.row), e.runes(b.row)
	e.lines = append(append(e.lines[:a.row:a.row], string(ra[:a.col])+string(rb[b.col:])), e.lines[b.row+1:]...)
	e.cur, e.anchor = a, nil
	e.cache.dirty = true
	return true
}

// Insert types s at the cursor, replacing any selection.
func (e *Editor) Insert(s string) {
	s = strings.ToValidUTF8(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"), "\uFFFD")
	e.deleteSelection()
	r := e.runes(e.cur.row)
	head, tail := string(r[:e.cur.col]), string(r[e.cur.col:])
	parts := strings.Split(s, "\n")
	if len(parts) == 1 {
		e.lines[e.cur.row] = head + s + tail
		e.cur.col += runeLen(s)
	} else {
		mid := append([]string{head + parts[0]}, parts[1:len(parts)-1]...)
		last := parts[len(parts)-1]
		e.lines = append(append(e.lines[:e.cur.row:e.cur.row], append(mid, last+tail)...), e.lines[e.cur.row+1:]...)
		e.cur = pos{e.cur.row + len(parts) - 1, runeLen(last)}
	}
	e.markChanged()
}

func (e *Editor) indentOf(r int) string {
	l := e.lines[r]
	return l[:len(l)-len(strings.TrimLeft(l, " \t"))]
}

var closers = map[rune]rune{'(': ')', '[': ']', '{': '}', '\'': '\'', '"': '"', '`': '`'}

// rowOf is the line holding byte offset off of Text().
func (e *Editor) rowOf(off int) int {
	for i, l := range e.lines {
		if off <= len(l) {
			return i
		}
		off -= len(l) + 1
	}
	return len(e.lines) - 1
}

// foldInfo returns the hidden lines and, per folded header line, how many lines it hides.
func (e *Editor) foldInfo() (hid map[int]bool, cnt map[int]int) {
	if len(e.folds) == 0 {
		return nil, nil
	}
	hid, cnt = map[int]bool{}, map[int]int{}
	for i, s := range db.Split(e.Dialect, e.Text()) {
		if !e.folds[i] {
			continue
		}
		a, b := e.rowOf(s.Start), e.rowOf(max(s.Start, s.End-1))
		for r := a + 1; r <= b; r++ {
			hid[r] = true
		}
		if b > a {
			cnt[a] = b - a
		}
	}
	return hid, cnt
}

// ToggleFold folds or unfolds the statement under the cursor.
func (e *Editor) ToggleFold() {
	for i, s := range db.Split(e.Dialect, e.Text()) {
		if e.Offset() < s.Start || e.Offset() > s.End {
			continue
		}
		if e.folds == nil {
			e.folds = map[int]bool{}
		}
		if e.folds[i] {
			delete(e.folds, i)
		} else {
			e.folds[i] = true
			e.move(e.rowOf(s.Start), 0, false)
		}
		e.cache.dirty = true
		e.foldsChanged()
		return
	}
}

func (e *Editor) foldsChanged() {
	if e.OnFolds != nil {
		e.OnFolds(e.Folded())
	}
}

// Folded lists the folded statement indexes, ascending.
func (e *Editor) Folded() []int {
	var out []int
	for i := range e.folds {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

// SetFolded restores folds (indexes past the last statement are ignored).
func (e *Editor) SetFolded(idx []int) {
	e.folds = nil
	n := len(db.Split(e.Dialect, e.Text()))
	for _, i := range idx {
		if i >= 0 && i < n {
			if e.folds == nil {
				e.folds = map[int]bool{}
			}
			e.folds[i] = true
		}
	}
}

func (e *Editor) move(row, col int, extend bool) {
	if extend && e.anchor == nil {
		p := e.cur
		e.anchor = &p
	}
	if !extend {
		e.anchor = nil
	}
	next := pos{clamp(row, 0, len(e.lines)-1), 0}
	if hid, _ := e.foldInfo(); hid[next.row] {
		if next.row > e.cur.row { // moving down: land after the folded block
			for hid[next.row] && next.row < len(e.lines)-1 {
				next.row++
			}
			if hid[next.row] { // the block runs to the end: stay on its header
				next.row = e.cur.row
			}
		} else {
			for hid[next.row] {
				next.row--
			}
		}
	}
	next.col = clamp(col, 0, len(e.runes(next.row)))
	if extend && next == e.cur && e.anchor != nil && *e.anchor == e.cur {
		e.anchor = nil // a shift-move that goes nowhere must not leave an empty, stale selection
	}
	if extend || e.anchor != nil {
		e.lastKind = "" // a selection is an undo boundary; a plain cursor move is not
	}
	e.cur = next
	e.scroll()
}

func (e *Editor) scroll() {
	if e.H <= 0 {
		return
	}
	if e.Opts.Wrap {
		// shortcut: scrolls whole logical lines, so a single line taller than the pane keeps its tail off screen; unwrap (wrap_lines) to reach it.
		e.left = 0
		hid, _ := e.foldInfo()
		if e.cur.row < e.top {
			e.top = e.cur.row
		}
		for e.top < e.cur.row {
			n := e.cur.col / e.textW()
			for r := e.top; r < e.cur.row; r++ {
				if !hid[r] {
					n += e.chunks(r)
				}
			}
			if n < e.H {
				break
			}
			e.top++
		}
		return
	}
	if hid, _ := e.foldInfo(); hid != nil {
		var vis []int
		for r := range e.lines {
			if !hid[r] {
				vis = append(vis, r)
			}
		}
		ci := sort.SearchInts(vis, e.cur.row)
		if e.cur.row < e.top {
			e.top = e.cur.row
		} else if ti := ci - e.H + 1; ti > 0 && sort.SearchInts(vis, e.top) < ti {
			e.top = vis[ti]
		}
	} else {
		if e.cur.row < e.top {
			e.top = e.cur.row
		}
		if e.cur.row >= e.top+e.H {
			e.top = e.cur.row - e.H + 1
		}
	}
	tw := e.textW()
	if e.cur.col < e.left {
		e.left = e.cur.col
	}
	if e.cur.col >= e.left+tw-1 {
		e.left = e.cur.col - tw + 2
	}
}

// gutterW is the line-number column plus " │ " before the text.
// chunks is how many screen rows line r takes when wrapped.
func (e *Editor) chunks(r int) int {
	n, tw := runeLen(e.lines[r]), e.textW()
	return max(1, (n+tw-1)/tw)
}

func (e *Editor) gutterW() int { return max(3, len(fmt.Sprint(len(e.lines)))) + 3 }
func (e *Editor) textW() int   { return max(1, e.W-e.gutterW()) }

func wordStart(r []rune, col int) int {
	for col > 0 && isIdentRune(r[col-1]) {
		col--
	}
	return col
}

func (e *Editor) prefixAtCursor() (string, int) {
	r := e.runes(e.cur.row)
	st := wordStart(r, e.cur.col)
	// include a qualifier like "t." so t.<col> completes columns of t
	for st > 0 && r[st-1] == '.' {
		st = wordStart(r, st-1)
	}
	return string(r[st:e.cur.col]), st
}

func (e *Editor) refreshComplete() {
	e.ac.open = false
	if !e.Opts.Autocomplete || e.Complete == nil || e.HasSelection() {
		return
	}
	p, _ := e.prefixAtCursor()
	if p == "" {
		return
	}
	items := e.Complete(p)
	if len(items) == 0 || len(items) == 1 && items[0].Insert == "" && strings.EqualFold(items[0].Text, p) {
		return
	}
	if len(items) > 50 {
		items = items[:50]
	}
	e.ac.open, e.ac.items, e.ac.sel = true, items, 0
}

func (e *Editor) acceptComplete() {
	c := e.ac.items[e.ac.sel]
	_, st := e.prefixAtCursor()
	text := c.Text
	if c.Insert != "" {
		text = strings.TrimRight(c.Insert, "\n")
	}
	if c.Kind == "keyword" {
		if e.Opts.Uppercase {
			text = strings.ToUpper(text)
		} else {
			text = strings.ToLower(text)
		}
	}
	if e.Opts.AddSpace && c.Kind != "saved" && c.Kind != "snippet" {
		text += " "
	}
	e.push("complete")
	e.deleteSelection()
	r := e.runes(e.cur.row)
	e.lines[e.cur.row] = string(r[:st]) + string(r[e.cur.col:])
	e.cur.col = st
	e.Insert(text) // Insert splits multi-line text into lines and places the cursor
	e.ac.open = false
	e.scroll()
}

// ToggleComment comments or uncomments the selected lines (or the cursor line).
func (e *Editor) ToggleComment() {
	prefix := "-- "
	if e.Dialect == "redis" {
		prefix = "# "
	} else if e.Dialect == "mongo" {
		prefix = "// "
	}
	r0, r1 := e.cur.row, e.cur.row
	if a, b, ok := e.selRange(); ok {
		r0, r1 = a.row, b.row
		if b.col == 0 && b.row > a.row {
			r1--
		}
	}
	all := true
	for r := r0; r <= r1; r++ {
		if t := strings.TrimSpace(e.lines[r]); t != "" && !strings.HasPrefix(t, strings.TrimSpace(prefix)) {
			all = false
		}
	}
	e.push("comment")
	for r := r0; r <= r1; r++ {
		ind := e.indentOf(r)
		body := e.lines[r][len(ind):]
		switch {
		case all:
			body = strings.TrimPrefix(strings.TrimPrefix(body, strings.TrimSpace(prefix)), " ")
		case strings.TrimSpace(body) != "":
			body = prefix + body
		}
		e.lines[r] = ind + body
	}
	e.fixAnchor()
	e.markChanged()
}

// ToggleBlockComment wraps the selection in /* */ (or unwraps it).
func (e *Editor) ToggleBlockComment() {
	sel := e.Selection()
	if sel == "" {
		return
	}
	e.push("comment")
	if t := strings.TrimSpace(sel); strings.HasPrefix(t, "/*") && strings.HasSuffix(t, "*/") {
		t = strings.TrimSuffix(strings.TrimPrefix(t, "/*"), "*/")
		e.Insert(strings.TrimSpace(t))
	} else {
		e.Insert("/* " + sel + " */")
	}
}

func (e *Editor) Undo() {
	if len(e.undo) == 0 {
		return
	}
	defer e.fixAnchor()
	s := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	e.redo = append(e.redo, editSnap{lines: append([]string(nil), e.lines...), cur: e.cur})
	e.lines, e.cur, e.anchor, e.lastKind, e.blk = s.lines, s.cur, nil, "", nil
	e.ac.open = false
	e.markChanged()
	e.scroll()
}

func (e *Editor) Redo() {
	if len(e.redo) == 0 {
		return
	}
	s := e.redo[len(e.redo)-1]
	e.redo = e.redo[:len(e.redo)-1]
	e.undo = append(e.undo, editSnap{lines: append([]string(nil), e.lines...), cur: e.cur})
	e.lines, e.cur, e.anchor, e.lastKind, e.blk = s.lines, s.cur, nil, "", nil
	e.ac.open = false
	e.markChanged()
	e.scroll()
}

// Handle processes an editing key or paste. It returns false for keys it does not own.
// Clipboard (ctrl+c/x/v) is left to the caller because it needs the program's clipboard commands.
func (e *Editor) Handle(msg tea.Msg) bool {
	switch m := msg.(type) {
	case tea.PasteMsg:
		e.push("paste")
		e.Insert(m.Content)
		e.ac.open = false
		e.scroll()
		return true
	case tea.KeyPressMsg:
		return e.handleKey(m)
	}
	return false
}

func (e *Editor) handleKey(m tea.KeyPressMsg) bool {
	e.marks = ""
	if e.vim.on && e.vimKey(m) {
		return true
	}
	k := tea.Key(m)
	ks := m.Keystroke()
	if k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
		e.push("type")
		if e.blk == nil && e.HasSelection() && e.Opts.CloseBraces && len(k.Text) == 1 && closers[rune(k.Text[0])] != 0 && k.Text != "'" && k.Text != "\"" && k.Text != "`" {
			sel := e.Selection() // wrap the selection like most editors
			e.Insert(k.Text + sel + string(closers[rune(k.Text[0])]))
		} else if e.Opts.CloseBraces && runeLen(k.Text) == 1 {
			e.typeRune([]rune(k.Text)[0])
		} else {
			e.Insert(k.Text)
		}
		e.scroll()
		e.refreshComplete()
		return true
	}
	if e.ac.open {
		switch ks {
		case "down", "ctrl+n":
			e.ac.sel = (e.ac.sel + 1) % len(e.ac.items)
			return true
		case "up", "ctrl+p":
			e.ac.sel = (e.ac.sel + len(e.ac.items) - 1) % len(e.ac.items)
			return true
		case "esc":
			e.ac.open = false
			return true
		case "tab":
			if e.Opts.CompleteKey != "enter" {
				e.acceptComplete()
				return true
			}
		case "enter":
			if e.Opts.CompleteKey != "tab" {
				e.acceptComplete()
				return true
			}
		}
	}
	extend := false
	if ks == "shift+tab" || ks == "backtab" {
		ks = "outdent" // not a selection-extending key
	} else if strings.HasPrefix(ks, "shift+") {
		extend, ks = true, strings.TrimPrefix(ks, "shift+")
	}
	if e.blk != nil {
		extend = false // the corner is e.blk, not the anchor: every arrow stretches the rectangle
	}
	row, col := e.cur.row, e.cur.col
	switch ks {
	case "left":
		if a, _, ok := e.selRange(); ok && !extend {
			e.move(a.row, a.col, false)
			return true
		}
		if col > 0 {
			e.move(row, col-1, extend)
		} else if row > 0 {
			e.move(row-1, runeLen(e.lines[row-1]), extend)
		}
	case "right":
		if _, b, ok := e.selRange(); ok && !extend {
			e.move(b.row, b.col, false)
			return true
		}
		if col < runeLen(e.lines[row]) {
			e.move(row, col+1, extend)
		} else if row < len(e.lines)-1 {
			e.move(row+1, 0, extend)
		}
	case "up":
		if row == 0 {
			e.move(0, 0, extend)
		} else {
			e.move(row-1, col, extend)
		}
	case "down":
		if row == len(e.lines)-1 {
			e.move(row, runeLen(e.lines[row]), extend)
		} else {
			e.move(row+1, col, extend)
		}
	case "home", "ctrl+a":
		if ks == "ctrl+a" && !extend { // ctrl+a = select all, like cmd+a
			e.anchor = &pos{0, 0}
			e.cur = pos{len(e.lines) - 1, runeLen(e.lines[len(e.lines)-1])}
			e.scroll()
			return true
		}
		ind := runeLen(e.indentOf(row))
		if col == ind || col == 0 && ind == 0 {
			e.move(row, 0, extend)
		} else {
			e.move(row, ind, extend)
		}
	case "end", "ctrl+e":
		e.move(row, runeLen(e.lines[row]), extend)
	case "pgup":
		e.move(row-max(1, e.H-1), col, extend)
	case "pgdown":
		e.move(row+max(1, e.H-1), col, extend)
	case "alt+left", "ctrl+left", "alt+b":
		r := e.runes(row)
		c := col
		for c > 0 && !isIdentRune(r[c-1]) {
			c--
		}
		e.move(row, wordStart(r, c), extend)
	case "alt+right", "ctrl+right", "alt+f":
		r := e.runes(row)
		c := col
		for c < len(r) && !isIdentRune(r[c]) {
			c++
		}
		for c < len(r) && isIdentRune(r[c]) {
			c++
		}
		e.move(row, c, extend)
	case "enter":
		e.push("enter")
		ind := e.indentOf(row)
		e.deleteSelection()
		r := e.runes(e.cur.row)
		if e.cur.col > 0 && e.cur.col < len(r) && closers[r[e.cur.col-1]] == r[e.cur.col] && r[e.cur.col] != '\'' && e.Opts.CloseBraces {
			e.Insert("\n" + ind + e.tab() + "\n" + ind) // {|} -> split with the cursor on the indented middle line
			e.cur = pos{e.cur.row - 1, runeLen(ind + e.tab())}
		} else {
			e.Insert("\n" + ind)
		}
		e.ac.open = false
		e.scroll()
	case "tab":
		e.push("tab")
		if a, b, ok := e.selRange(); ok && a.row != b.row {
			for r := a.row; r <= b.row; r++ {
				e.lines[r] = e.tab() + e.lines[r]
			}
			e.markChanged()
		} else {
			e.Insert(e.tab())
		}
		e.scroll()
	case "outdent":
		e.push("tab")
		r0, r1 := row, row
		if a, b, ok := e.selRange(); ok {
			r0, r1 = a.row, b.row
		}
		for r := r0; r <= r1; r++ {
			e.lines[r] = e.outdent(e.lines[r])
		}
		e.fixAnchor()
		e.markChanged()
	case "backspace", "ctrl+h":
		e.push("delete")
		switch {
		case e.deleteSelection():
		case col > 0:
			r := e.runes(row)
			n := 1
			if e.Opts.CloseBraces && col < len(r) && closers[r[col-1]] == r[col] && closers[r[col-1]] != 0 {
				n = 1
				e.lines[row] = string(r[:col-1]) + string(r[col+1:]) // remove the auto-inserted pair
			} else {
				e.lines[row] = string(r[:col-n]) + string(r[col:])
			}
			e.cur.col -= n
		case row > 0:
			prev := runeLen(e.lines[row-1])
			e.lines[row-1] += e.lines[row]
			e.lines = append(e.lines[:row], e.lines[row+1:]...)
			e.cur = pos{row - 1, prev}
		}
		e.markChanged()
		e.scroll()
		e.refreshComplete()
	case "delete", "ctrl+d":
		e.push("delete")
		switch {
		case e.deleteSelection():
		case col < runeLen(e.lines[row]):
			r := e.runes(row)
			e.lines[row] = string(r[:col]) + string(r[col+1:])
		case row < len(e.lines)-1:
			e.lines[row] += e.lines[row+1]
			e.lines = append(e.lines[:row+1], e.lines[row+2:]...)
		}
		e.markChanged()
	case "ctrl+k": // kill to end of line
		e.push("delete")
		e.lines[row] = string(e.runes(row)[:col])
		e.anchor = nil
		e.markChanged()
	case "ctrl+z":
		e.Undo()
	case "ctrl+y", "ctrl+shift+z":
		e.Redo()
	case "ctrl+w", "alt+backspace":
		e.push("delete")
		r := e.runes(row)
		c := col
		for c > 0 && !isIdentRune(r[c-1]) {
			c--
		}
		c = wordStart(r, c)
		e.lines[row] = string(r[:c]) + string(r[col:])
		e.cur.col = c
		e.anchor = nil
		e.markChanged()
	default:
		return false
	}
	if !strings.HasPrefix(ks, "left") && ks != "backspace" {
		e.ac.open = false
	}
	return true
}

func (e *Editor) tab() string {
	if e.Opts.UseTabs {
		return "\t"
	}
	return strings.Repeat(" ", e.Opts.TabWidth)
}

func (e *Editor) outdent(l string) string {
	if strings.HasPrefix(l, "\t") {
		return l[1:]
	}
	n := 0
	for n < e.Opts.TabWidth && n < len(l) && l[n] == ' ' {
		n++
	}
	return l[n:]
}

// typeRune inserts one typed rune with bracket/quote auto-closing: typing a closer over an
// auto-inserted one just steps past it; an opener inserts its pair.
func (e *Editor) typeRune(c rune) {
	r := e.runes(e.cur.row)
	next := rune(0)
	if e.cur.col < len(r) {
		next = r[e.cur.col]
	}
	closer, isOpener := closers[c]
	isCloser := c == ')' || c == ']' || c == '}' || c == '\'' || c == '"' || c == '`'
	switch {
	case isCloser && next == c:
		e.cur.col++
	case isOpener && (c == '(' || c == '[' || c == '{'):
		e.Insert(string(c) + string(closer))
		e.cur.col--
	case isOpener: // quote: pair only when not inside a word and the next char is not a word char
		prev := rune(0)
		if e.cur.col > 0 {
			prev = r[e.cur.col-1]
		}
		if isIdentRune(prev) || isIdentRune(next) {
			e.Insert(string(c))
		} else {
			e.Insert(string(c) + string(closer))
			e.cur.col--
		}
	default:
		e.Insert(string(c))
	}
	e.markChanged()
}

// FirstWord returns lowercased first word of the buffer (used by tests/tab titles).
func (e *Editor) FirstWord() string {
	f := strings.FieldsFunc(e.Text(), func(r rune) bool { return unicode.IsSpace(r) })
	if len(f) == 0 {
		return ""
	}
	return strings.ToLower(f[0])
}

// Run key bits above the token kind (key & 7).
const (
	keyStmt    = 1 << 3 // inside the statement under the cursor
	keySel     = 1 << 4 // selected
	keyCursor  = 1 << 5 // the cursor cell (used alone)
	keyCurLine = 1 << 6 // on the cursor line
)

// styleFor maps a run key to a style. One background wins: selection, then cursor line, then statement
// (Inherit never overwrites a background that is already set, so they cannot be stacked).
func (e *Editor) styleFor(th Theme, key int) lipgloss.Style {
	if key == keyCursor {
		return th.SelCell
	}
	st := th.Cell
	switch tokKind(key & 7) {
	case tkKeyword:
		st = th.Keyword
	case tkString:
		st = th.String
	case tkComment:
		st = th.Comment
	case tkNumber:
		st = th.Number
	}
	switch {
	case key&keySel != 0:
		st = st.Inherit(th.SelRow)
	case key&keyCurLine != 0:
		st = st.Inherit(th.CurRow)
	case key&keyStmt != 0:
		st = st.Inherit(th.AltRow)
	}
	return st
}

// View renders exactly W x H cells, with the autocomplete popup anchored under the cursor.
func (e *Editor) View(th Theme, focused bool) string {
	if e.W < 8 || e.H < 1 {
		return ""
	}
	e.scroll()
	c := &e.cache
	if c.dirty || c.text == "" && len(e.lines) > 0 && e.lines[0] != "" {
		c.text, c.kinds, c.stmts, c.split, c.offs, c.dirty = e.Text(), nil, nil, false, nil, false
	}
	if need := min(len(e.lines), e.top+e.H); len(c.kinds) < need {
		c.kinds = lexLines(e.Dialect, e.lines[:need]) // block comments carry over from earlier lines
	}
	kinds := c.kinds
	var curStmt *db.Stmt
	var offs []int // byte offset of each line start
	if e.Opts.Highlight {
		if !c.split {
			c.stmts, c.split = db.Split(e.Dialect, c.text), true
		}
		if s, ok := db.StatementAt(c.stmts, e.Offset()); ok {
			curStmt = &s
		}
		if c.offs == nil {
			n := 0
			for _, l := range e.lines {
				c.offs = append(c.offs, n)
				n += len(l) + 1
			}
		}
		offs = c.offs
	}
	a, b, hasSel := e.selRange()
	br0, br1, bc0, bc1, hasBlk := e.blockRect()
	gw, tw := e.gutterW(), e.textW()
	rule := th.Border.Render("│") + " "
	var out []string
	hid, foldN := e.foldInfo()
	var vis []int // visible lines when something is folded
	if hid != nil {
		for r := range e.lines {
			if !hid[r] {
				vis = append(vis, r)
			}
		}
	}
	first := sort.SearchInts(vis, e.top)
	e.curY = -1
	for k := 0; len(out) < e.H; k++ {
		ri := e.top + k
		if hid != nil {
			ri = len(e.lines)
			if first+k < len(vis) {
				ri = vis[first+k]
			}
		}
		if ri >= len(e.lines) {
			out = append(out, strings.Repeat(" ", gw-2)+rule+strings.Repeat(" ", max(0, e.W-gw)))
			continue
		}
		if ri >= len(c.kinds) { // folds jump past the lexed window
			c.kinds = lexLines(e.Dialect, e.lines[:ri+1])
		}
		kinds = c.kinds
		r := e.runes(ri)
		if n := foldN[ri]; n > 0 {
			r = append(r, []rune(fmt.Sprintf(" … (%d lines)", n))...)
		}
		mk := e.markRow(r)
		segs := [][2]int{{e.left, min(len(r), e.left+tw)}}
		if e.Opts.Wrap {
			n := max(1, (len(r)+tw-1)/tw)
			if ri == e.cur.row && len(r) > 0 && e.cur.col == len(r) && len(r)%tw == 0 {
				n++ // the cursor sits just past a full row: it needs a row of its own
			}
			segs = segs[:0]
			for i := 0; i < n; i++ {
				segs = append(segs, [2]int{i * tw, min(len(r), (i+1)*tw)})
			}
		}
		for si, seg := range segs {
			if len(out) >= e.H {
				break
			}
			from, end := seg[0], seg[1]
			var sb strings.Builder
			num := th.Dim.Render(padLeft(fmt.Sprint(ri+1), gw-3) + " ")
			if ri == e.cur.row {
				num = th.Accent.Bold(true).Render(padLeft(fmt.Sprint(ri+1), gw-3) + " ")
			}
			if si > 0 {
				num = strings.Repeat(" ", gw-2)
			}
			sb.WriteString(num + rule)
			curLine := focused && e.Opts.Highlight && ri == e.cur.row
			if ri == e.cur.row && e.cur.col >= from && (e.cur.col < from+tw || !e.Opts.Wrap) {
				e.curY, e.curFrom = len(out), from
			}
			bytePos := 0
			if curStmt != nil && ri < len(offs) {
				bytePos = offs[ri] + len(string(r[:min(from, len(r))]))
			}
			// Characters are grouped into runs that share a style and rendered once per run:
			// a Render call per character made big buffers slow to draw.
			runKey, run := -1, make([]rune, 0, tw)
			flush := func() {
				if len(run) > 0 {
					sb.WriteString(e.styleFor(th, runKey).Render(string(run)))
					run = run[:0]
				}
			}
			for ci := from; ci < end; ci++ {
				key := 0
				if ri < len(kinds) && ci < len(kinds[ri]) {
					key = int(kinds[ri][ci])
				}
				if curStmt != nil && bytePos >= curStmt.Start && bytePos < curStmt.End {
					key |= keyStmt
				}
				if curLine {
					key |= keyCurLine
				}
				if mk != nil && mk[ci] {
					key |= keySel
				}
				if hasBlk && ri >= br0 && ri <= br1 && ci >= bc0 && ci < bc1 {
					key |= keySel
				}
				if hasSel && (ri > a.row || ri == a.row && ci >= a.col) && (ri < b.row || ri == b.row && ci < b.col) {
					key |= keySel
				}
				if focused && ri == e.cur.row && ci == e.cur.col {
					key = keyCursor
				}
				if key != runKey {
					flush()
					runKey = key
				}
				run = append(run, e.shown(r[ci]))
				bytePos += len(string(r[ci]))
			}
			flush()
			if focused && ri == e.cur.row && e.cur.col >= end && e.cur.col-from < tw {
				sb.WriteString(th.SelCell.Render(" "))
			}
			line := ansi.Truncate(sb.String(), e.W, "")
			if pad := e.W - width(line); pad > 0 {
				if curLine { // the cursor-line tint runs to the right edge
					line += th.CurRow.Render(strings.Repeat(" ", pad))
				} else {
					line += strings.Repeat(" ", pad)
				}
			}
			out = append(out, line)
		}
	}
	if e.ac.open && focused {
		e.overlayComplete(th, out, gw)
	}
	return strings.Join(out, "\n")
}

func (e *Editor) overlayComplete(th Theme, out []string, gw int) {
	n := min(len(e.ac.items), 8)
	start := clamp(e.ac.sel-n+1, 0, len(e.ac.items)-n)
	if e.ac.sel < start {
		start = e.ac.sel
	}
	tw, kw := 0, 0
	for _, c := range e.ac.items[start : start+n] {
		tw, kw = max(tw, width(c.Text)), max(kw, width(c.Kind))
	}
	tw = min(tw, 40)
	w := min(2+tw+2+kw+1, e.W-gw) // "› text  kind "
	tw = max(1, w-5-kw)
	_, st := e.prefixAtCursor()
	left, cy := e.left, e.cur.row-e.top
	if e.curY >= 0 {
		left, cy = e.curFrom, e.curY
	}
	x := clamp(gw+st-left-2, gw, max(gw, e.W-w)) // the text column lines up with the word being completed
	y := cy + 1
	if y+n > e.H {
		y = max(0, cy-n)
	}
	for i := 0; i < n && y+i < len(out); i++ {
		c := e.ac.items[start+i]
		bg, txt, mark := th.Popup, th.Popup, "  "
		if start+i == e.ac.sel {
			bg, txt, mark = th.SelRow, th.SelRow.Bold(true), "› "
		}
		cell := th.Accent.Inherit(bg).Bold(true).Render(mark) + txt.Render(fit(c.Text, tw)+"  ") + th.Dim.Inherit(bg).Render(padLeft(c.Kind, kw)+" ")
		line := out[y+i]
		out[y+i] = ansi.Truncate(line, x, "") + ansi.Truncate(cell, w, "") + ansi.TruncateLeft(line, x+w, "")
	}
}

// RankCandidates filters and orders candidates by fuzzy score against prefix (after any "t." qualifier).
func RankCandidates(prefix string, all []Candidate, limit int) []Candidate {
	type scored struct {
		c Candidate
		s int
	}
	var hits []scored
	for _, c := range all {
		if s, ok := fuzzy(prefix, c.Text); ok {
			if strings.HasPrefix(strings.ToLower(c.Text), strings.ToLower(prefix)) {
				s += 20
			}
			hits = append(hits, scored{c, s})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].s > hits[j].s })
	var out []Candidate
	for _, h := range hits {
		out = append(out, h.c)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out
}

// Replace swaps the selection (or, with none, the whole buffer) for s as one undo step.
func (e *Editor) Replace(s string) {
	e.push("replace")
	if !e.deleteSelection() {
		e.lines, e.cur, e.anchor = []string{""}, pos{}, nil
	}
	e.Insert(s)
	e.markChanged()
}

func matchAt(rs []rune, c int, q []rune) bool {
	for i, r := range q {
		if unicode.ToLower(rs[c+i]) != r {
			return false
		}
	}
	return true
}

func lowerRunes(s string) []rune { return []rune(strings.Map(unicode.ToLower, s)) }

// Find selects the next case-insensitive match of q after the cursor, wrapping around; false when there is none.
func (e *Editor) Find(q string) bool {
	qr := lowerRunes(q)
	if len(qr) == 0 {
		return false
	}
	n := len(e.lines)
	for i := 0; i <= n; i++ {
		row := (e.cur.row + i) % n
		rs := e.runes(row)
		from := 0
		if i == 0 {
			from = e.cur.col
		}
		for c := from; c+len(qr) <= len(rs); c++ {
			if matchAt(rs, c, qr) {
				e.anchor, e.cur = &pos{row, c}, pos{row, c + len(qr)}
				e.scroll()
				return true
			}
		}
	}
	return false
}

// ReplaceAll replaces every case-insensitive match of q (one undo step) and returns how many it changed.
// shortcut: r is inserted on one line (newlines become spaces); a multi-line replacement needs a buffer rebuild.
func (e *Editor) ReplaceAll(q, r string) int {
	qr, rr := lowerRunes(q), []rune(strings.ReplaceAll(r, "\n", " "))
	if len(qr) == 0 {
		return 0
	}
	n, out := 0, make([]string, len(e.lines))
	for i := range e.lines {
		rs := e.runes(i)
		var b []rune
		for c := 0; c < len(rs); {
			if c+len(qr) <= len(rs) && matchAt(rs, c, qr) {
				b, c, n = append(b, rr...), c+len(qr), n+1
			} else {
				b, c = append(b, rs[c]), c+1
			}
		}
		out[i] = string(b)
	}
	if n > 0 {
		e.push("replace")
		e.lines, e.anchor = out, nil
		e.fixAnchor()
		e.markChanged()
	}
	return n
}
