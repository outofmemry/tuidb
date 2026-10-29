package ui

import (
	"strings"
	"testing"
	"webtuidb/internal/config"

	tea "charm.land/bubbletea/v2"
)

func typ(e *Editor, s string) {
	for _, c := range s {
		if c == '\n' {
			e.Handle(tea.KeyPressMsg{Code: tea.KeyEnter})
			continue
		}
		e.Handle(tea.KeyPressMsg{Code: c, Text: string(c)})
	}
}

func press(e *Editor, ks ...tea.KeyPressMsg) {
	for _, k := range ks {
		e.Handle(k)
	}
}

var (
	kLeft  = tea.KeyPressMsg{Code: tea.KeyLeft}
	kRight = tea.KeyPressMsg{Code: tea.KeyRight}
	kUp    = tea.KeyPressMsg{Code: tea.KeyUp}
	kBack  = tea.KeyPressMsg{Code: tea.KeyBackspace}
	kEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	kTab   = tea.KeyPressMsg{Code: tea.KeyTab}
	kSLeft = tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift}
	kSDown = tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}
	kUndo  = tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl}
	kHome  = tea.KeyPressMsg{Code: tea.KeyHome}
)

func newEd() *Editor {
	e := NewEditor("postgres", EditorOpts{TabWidth: 2, CloseBraces: true, Uppercase: true, CompleteKey: "enter_or_tab", Highlight: true, Autocomplete: true})
	e.W, e.H = 60, 8
	return e
}

func TestEditorTypingNewlinesIndent(t *testing.T) {
	e := newEd()
	typ(e, "select 1;\n  from t")
	if e.Text() != "select 1;\n  from t" {
		t.Fatalf("%q", e.Text())
	}
	typ(e, "\nwhere")
	if e.Text() != "select 1;\n  from t\n  where" {
		t.Errorf("auto indent: %q", e.Text())
	}
	press(e, kBack, kBack)
	typ(e, "")
	if !strings.HasSuffix(e.Text(), "  whe") {
		t.Errorf("backspace: %q", e.Text())
	}
	// Backspace at column 0 joins lines.
	e.cur = pos{1, 0}
	press(e, kBack)
	if !strings.HasPrefix(e.Text(), "select 1;  from t") {
		t.Errorf("join: %q", e.Text())
	}
}

func TestEditorBracePairing(t *testing.T) {
	e := newEd()
	typ(e, "f(")
	if e.Text() != "f()" || e.cur.col != 2 {
		t.Fatalf("%q %d", e.Text(), e.cur.col)
	}
	typ(e, "'a'")
	if e.Text() != "f('a')" {
		t.Errorf("quote pair then step over: %q", e.Text())
	}
	typ(e, ")")
	if e.Text() != "f('a')" || e.cur.col != 6 {
		t.Errorf("typing a closer steps over: %q col %d", e.Text(), e.cur.col)
	}
	e2 := newEd()
	typ(e2, "it's")
	if e2.Text() != "it's" {
		t.Errorf("apostrophe in a word is not paired: %q", e2.Text())
	}
	e3 := newEd()
	typ(e3, "(")
	press(e3, kBack)
	if e3.Text() != "" {
		t.Errorf("backspace removes the pair: %q", e3.Text())
	}
}

func TestEditorSelectionAndUndo(t *testing.T) {
	e := newEd()
	typ(e, "hello world")
	press(e, kSLeft, kSLeft, kSLeft, kSLeft, kSLeft)
	if e.Selection() != "world" {
		t.Fatalf("sel %q", e.Selection())
	}
	typ(e, "x")
	if e.Text() != "hello x" {
		t.Errorf("typing replaces selection: %q", e.Text())
	}
	press(e, kUndo)
	if e.Text() != "hello world" {
		t.Errorf("undo: %q", e.Text())
	}
	press(e, kUndo)
	if e.Text() != "" {
		t.Errorf("undo typing run: %q", e.Text())
	}
	e.Redo()
	if e.Text() != "hello world" {
		t.Errorf("redo: %q", e.Text())
	}
	e.SetText("a\nb\nc")
	e.cur, e.anchor = pos{0, 0}, nil
	press(e, kSDown, kSDown)
	if e.Selection() != "a\nb\n" {
		t.Errorf("multi-line sel %q", e.Selection())
	}
	typ(e, "Z")
	if e.Text() != "Zc" {
		t.Errorf("replace multi-line: %q", e.Text())
	}
}

func TestEditorPasteMultiline(t *testing.T) {
	e := newEd()
	typ(e, "ab")
	press(e, kLeft)
	e.Handle(tea.PasteMsg{Content: "1\r\n2\n3"})
	if e.Text() != "a1\n2\n3b" || e.cur != (pos{2, 1}) {
		t.Errorf("%q %v", e.Text(), e.cur)
	}
}

func TestEditorToggleComment(t *testing.T) {
	e := newEd()
	e.SetText("select 1\n  from t")
	e.cur = pos{0, 0}
	e.anchor = &pos{1, 3}
	e.ToggleComment()
	if e.Text() != "-- select 1\n  -- from t" {
		t.Fatalf("%q", e.Text())
	}
	e.ToggleComment()
	if e.Text() != "select 1\n  from t" {
		t.Errorf("uncomment: %q", e.Text())
	}
	e.Dialect = "redis"
	e.anchor = nil
	e.ToggleComment()
	if !strings.HasPrefix(e.Text(), "# ") {
		t.Errorf("redis comment: %q", e.Text())
	}
	e.SetText("select 1")
	e.anchor, e.cur = &pos{0, 0}, pos{0, 8}
	e.Dialect = "postgres"
	e.ToggleBlockComment()
	if e.Text() != "/* select 1 */" {
		t.Errorf("block: %q", e.Text())
	}
}

func TestEditorCurrentStatement(t *testing.T) {
	e := newEd()
	e.SetText("select 1;\nselect 2;\n\nselect 3")
	e.cur = pos{1, 3}
	if s, _ := e.CurrentStatement(); s.Text != "select 2" {
		t.Errorf("%q", s.Text)
	}
	e.cur = pos{3, 8}
	if s, _ := e.CurrentStatement(); s.Text != "select 3" {
		t.Errorf("%q", s.Text)
	}
	e.cur = pos{2, 0} // blank line between statements picks the previous one
	if s, _ := e.CurrentStatement(); s.Text != "select 2" {
		t.Errorf("%q", s.Text)
	}
}

func TestEditorAutocomplete(t *testing.T) {
	e := newEd()
	all := []Candidate{{Text: "customers", Kind: "table"}, {Text: "orders", Kind: "table"}, {Text: "select", Kind: "keyword"}, {Text: "created_at", Kind: "column"}, {Text: "ro", Kind: "saved", Insert: "SELECT * FROM orders;\n"}}
	e.Complete = func(p string) []Candidate { return RankCandidates(p, all, 10) }
	typ(e, "cu")
	if !e.ac.open || e.ac.items[0].Text != "customers" {
		t.Fatalf("%+v", e.ac)
	}
	press(e, kTab)
	if e.Text() != "customers" || e.ac.open {
		t.Errorf("accept: %q", e.Text())
	}
	press(e, kBack)
	typ(e, "s")
	e.SetText("")
	typ(e, "sel")
	press(e, kEnter)
	if e.Text() != "SELECT" {
		t.Errorf("enter accepts keyword uppercased: %q", e.Text())
	}
	e.SetText("")
	e.Opts.CompleteKey = "tab"
	typ(e, "or")
	press(e, kEnter)
	if e.Text() != "or\n" {
		t.Errorf("tab-only mode: enter is a newline: %q", e.Text())
	}
	e.SetText("")
	e.Opts.CompleteKey = "enter_or_tab"
	typ(e, "ro")
	press(e, kTab)
	if e.Text() != "SELECT * FROM orders;" {
		t.Errorf("saved query keyword expands to its SQL: %q", e.Text())
	}
	e.SetText("")
	typ(e, "zzz")
	if e.ac.open {
		t.Error("no matches, no popup")
	}
}

func TestLexer(t *testing.T) {
	k := lexLines("postgres", []string{"SELECT 'a;b', 12 -- c", "/* x", "y */ from"})
	at := func(l, c int) tokKind { return k[l][c] }
	if at(0, 0) != tkKeyword || at(0, 7) != tkString || at(0, 14) != tkNumber || at(0, 18) != tkComment {
		t.Errorf("line0 %v", k[0])
	}
	if at(1, 0) != tkComment || at(2, 0) != tkComment || at(2, 5) != tkKeyword {
		t.Errorf("block comment across lines: %v %v", k[1], k[2])
	}
	rk := lexLines("redis", []string{"get foo", "foo get"})
	if rk[0][0] != tkKeyword || rk[1][4] != tkText {
		t.Errorf("redis only first word: %v %v", rk[0], rk[1])
	}
	if lexLines("postgres", []string{"select \"it's\""})[0][8] != tkText {
		t.Error("postgres double quote is an identifier, not a string")
	}
}

func TestEditorViewFits(t *testing.T) {
	th := NewTheme(configTheme())
	e := newEd()
	e.SetText(strings.Repeat("select very_long_column_name, another_long_column from some_table where x = 'y';\n", 20))
	e.W, e.H = 40, 6
	e.cur = pos{10, 70}
	e.anchor = &pos{9, 3}
	for _, focused := range []bool{true, false} {
		lines := strings.Split(e.View(th, focused), "\n")
		if len(lines) != 6 {
			t.Fatalf("height %d", len(lines))
		}
		for i, l := range lines {
			if width(l) != 40 {
				t.Errorf("line %d width %d", i, width(l))
			}
		}
	}
	if e.cur.row < e.top || e.cur.row >= e.top+e.H || e.cur.col < e.left {
		t.Errorf("cursor not scrolled into view: top=%d left=%d cur=%v", e.top, e.left, e.cur)
	}
}

func TestErrorLines(t *testing.T) {
	th := NewTheme(configTheme())
	got := errorLines(th, "ERROR: relation \"nope\" does not exist (SQLSTATE 42P01)\nHINT: check the name\nmore detail", 80)
	for i := range got {
		got[i] = stripANSI(got[i])
	}
	want := []string{`  ✗ ERROR: relation "nope" does not exist`, "      HINT: check the name", "    more detail", "    SQLSTATE 42P01"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q", got)
	}
}

func TestEditorSelectionWinsOverCursorLine(t *testing.T) {
	th := NewTheme(configTheme())
	key := int(tkKeyword) | keyStmt | keyCurLine | keySel
	if bg := newEd().styleFor(th, key).GetBackground(); bg != th.SelRow.GetBackground() {
		t.Errorf("selection background lost: %v", bg)
	}
}

func TestRankCandidates(t *testing.T) {
	all := []Candidate{{Text: "order_items", Kind: "table"}, {Text: "orders", Kind: "table"}, {Text: "audit_records", Kind: "table"}}
	got := RankCandidates("ord", all, 0)
	if len(got) < 2 || got[0].Text != "orders" {
		t.Errorf("%+v", got)
	}
	if len(RankCandidates("zzz", all, 0)) != 0 {
		t.Error("no match")
	}
}

func TestEditorSelectionStaysValidAfterShorteningEdits(t *testing.T) {
	for name, op := range map[string]func(e *Editor){
		"ctrl+k":  func(e *Editor) { e.Handle(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}) },
		"ctrl+w":  func(e *Editor) { e.Handle(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}) },
		"undo":    func(e *Editor) { e.Undo() },
		"comment": func(e *Editor) { e.ToggleComment(); e.ToggleComment() },
		"outdent": func(e *Editor) { e.Handle(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}) },
	} {
		e := newEd()
		e.SetText("    long line of text here\nshort")
		e.cur, e.anchor = pos{0, 10}, &pos{1, 5}
		op(e)
		if strings.ContainsRune(e.Selection(), 0) {
			t.Errorf("%s: selection contains NUL bytes %q", name, e.Selection())
		}
		if e.anchor != nil && (e.anchor.row >= len(e.lines) || e.anchor.col > runeLen(e.lines[e.anchor.row])) {
			t.Errorf("%s: anchor %v outside the text", name, *e.anchor)
		}
		_ = e.View(NewTheme(configTheme()), true)
	}
}

func TestEditorShiftTabOutdents(t *testing.T) {
	e := newEd()
	e.SetText("    x")
	e.cur = pos{0, 5}
	e.Handle(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if e.Text() != "  x" { // TabWidth is 2 in the test editor
		t.Errorf("shift+tab must outdent, got %q", e.Text())
	}
	if e.HasSelection() {
		t.Error("shift+tab must not start a selection")
	}
}

func TestEditorReviewRound(t *testing.T) {
	// a shift-move that goes nowhere must not leave a stale anchor behind
	e := newEd()
	e.SetText("abc")
	e.cur = pos{0, 3}
	press(e, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}, kBack, kBack)
	if e.Text() != "a" || e.anchor != nil {
		t.Errorf("after shift+down at the end then two backspaces: %q anchor=%v", e.Text(), e.anchor)
	}
	// accepting a completion replaces the selection and splits multi-line text into lines
	e = newEd()
	all := []Candidate{{Text: "ro", Kind: "saved", Insert: "SELECT 1\nFROM t\n"}}
	e.Complete = func(p string) []Candidate { return RankCandidates(p, all, 10) }
	typ(e, "ro")
	press(e, kTab)
	if len(e.lines) != 2 || e.lines[1] != "FROM t" || strings.Contains(e.lines[0], "\n") {
		t.Errorf("lines %q", e.lines)
	}
	for _, l := range strings.Split(e.View(NewTheme(configTheme()), true), "\n") {
		if width(l) != 60 {
			t.Errorf("view line width %d", width(l))
		}
	}
	// a lone CR and invalid UTF-8 are normalized, and Offset counts stored bytes
	e = newEd()
	e.SetText("select 1;\rdrop table x;")
	if len(e.lines) != 2 {
		t.Errorf("lone CR is a newline: %q", e.lines)
	}
	e = newEd()
	e.SetText("select 1;\n\xff\xfe;\ndrop table x;")
	e.cur = pos{1, 1}
	if s, _ := e.CurrentStatement(); strings.Contains(s.Text, "drop") {
		t.Errorf("cursor on the middle line must not run the DROP: %q", s.Text)
	}
	// typing after a cursor move still coalesces into one undo step
	e = newEd()
	typ(e, "ab")
	press(e, kLeft)
	typ(e, "c")
	press(e, kUndo)
	if e.Text() != "" {
		t.Errorf("typing runs coalesce across cursor moves: %q", e.Text())
	}
}

func TestLineEditWideRunes(t *testing.T) {
	l := newLine("日本語日本語")
	th := NewTheme(configTheme())
	for _, w := range []int{6, 8, 10, 13} {
		for p := 0; p <= 6; p++ {
			l.pos = p
			if got := width(l.View(w, th, true)); got != w {
				t.Errorf("w=%d pos=%d: rendered %d cells", w, p, got)
			}
		}
	}
}

func TestEditorUndoInvalidatesRenderCache(t *testing.T) {
	e := newEd()
	e.SetText("select 1\nfrom t\nwhere a = 1")
	_ = e.View(NewTheme(configTheme()), true) // builds the cache for three lines
	e.SetText("select 1")
	e.undo = []editSnap{{lines: []string{"select 1", "from t", "where a = 1"}}}
	_ = e.View(NewTheme(configTheme()), true) // cache now holds one line
	e.Undo()
	_ = e.View(NewTheme(configTheme()), true) // used to panic: offsets cached for one line
}

// vkeys sends each string as one key: single-rune strings are typed text, "enter" and "esc" are special.
func vkeys(e *Editor, ks ...string) {
	for _, k := range ks {
		switch k {
		case "enter":
			e.Handle(tea.KeyPressMsg{Code: tea.KeyEnter})
		case "esc":
			e.Handle(tea.KeyPressMsg{Code: tea.KeyEscape})
		default:
			for _, c := range k {
				e.Handle(tea.KeyPressMsg{Code: c, Text: string(c)})
			}
		}
	}
}

func vimEd(text string) *Editor {
	e := newEd()
	e.SetText(text)
	e.vim.on = true
	return e
}

func TestVimVisualDelete(t *testing.T) {
	e := vimEd("select 1 from t")
	vkeys(e, "v", "l", "l", "d")
	if e.Text() != "ect 1 from t" || e.vim.vmode != 0 {
		t.Fatalf("v + d: %q mode %q", e.Text(), e.vim.vmode)
	}
	e = vimEd("a\nb\nc")
	vkeys(e, "V", "j", "d")
	if e.Text() != "c" {
		t.Fatalf("V + d: %q", e.Text())
	}
	vkeys(e, "p") // the deleted lines are in the register
	if e.Text() != "c\na\nb" {
		t.Fatalf("p: %q", e.Text())
	}
	e = vimEd("one two three")
	vkeys(e, "w", "v", "e", "y", "$", "p")
	if e.Text() != "one two threetwo" {
		t.Fatalf("yank: %q", e.Text())
	}
	e = vimEd("a\nb")
	vkeys(e, "V", ">")
	if !strings.HasPrefix(e.Text(), "  a") { // newEd indents by two spaces
		t.Fatalf("indent: %q", e.Text())
	}
}

func TestVimCounts(t *testing.T) {
	e := vimEd("a\nb\nc\nd")
	vkeys(e, "2", "d", "d")
	if e.Text() != "c\nd" {
		t.Fatalf("2dd: %q", e.Text())
	}
	e = vimEd("abcdef")
	vkeys(e, "3", "x")
	if e.Text() != "def" {
		t.Fatalf("3x: %q", e.Text())
	}
	e = vimEd("a\nb\nc\nd")
	vkeys(e, "3", "j")
	if e.cur.row != 3 {
		t.Fatalf("3j: row %d", e.cur.row)
	}
	e = vimEd("one two three four")
	vkeys(e, "d", "2", "w")
	if e.Text() != "three four" {
		t.Fatalf("d2w: %q", e.Text())
	}
}

func TestVimCommandLine(t *testing.T) {
	e := vimEd("a\nb\nc")
	vkeys(e, ":", "w", "enter")
	if cmd, _ := e.takeEx(); cmd != "w" {
		t.Fatalf(":w should be routed to the tab, got %q", cmd)
	}
	vkeys(e, ":", "3", "enter")
	if e.cur.row != 2 || e.vim.cmdOn {
		t.Fatalf(":3 -> row %d", e.cur.row)
	}
	vkeys(e, ":", "z", "esc")
	if cmd, note := e.takeEx(); cmd != "" || note != "" || e.vim.cmdOn {
		t.Fatal("esc must cancel the command line")
	}
	vkeys(e, ":", "n", "o", "h", "enter")
	if _, note := e.takeEx(); note == "" {
		t.Fatal(":noh should report")
	}
	for _, c := range []string{"q", "q!", "wq"} {
		vkeys(e, ":", c, "enter")
		if got, _ := e.takeEx(); got != c {
			t.Fatalf(":%s routed as %q", c, got)
		}
	}
}

func TestFoldSurvivesEditAbove(t *testing.T) {
	e := newEd()
	e.SetText("select 1;\nselect\n  2;\nselect 3;")
	var got [][]int
	e.OnFolds = func(f []int) { got = append(got, f) }
	e.cur = pos{1, 0}
	e.ToggleFold()
	if f := e.Folded(); len(f) != 1 || f[0] != 1 {
		t.Fatalf("folded %v", f)
	}
	e.cur = pos{0, 0}
	e.Insert("select 0;\n")
	if f := e.Folded(); len(f) != 1 || f[0] != 2 {
		t.Fatalf("after an edit above the fold is %v, want [2]", f)
	}
	if hid, _ := e.foldInfo(); !hid[3] {
		t.Fatal("the folded body should still be hidden")
	}
	if last := got[len(got)-1]; len(last) != 1 || last[0] != 2 {
		t.Fatalf("OnFolds saw %v", got)
	}
	e.cur = pos{2, 0}
	e.Insert("x") // editing the folded statement itself unfolds it
	if len(e.Folded()) != 0 {
		t.Fatalf("edited statement stays folded: %v", e.Folded())
	}
}

func TestSelectAllAndClick(t *testing.T) {
	e := newEd()
	e.SetText("ab\ncd")
	e.SelectAll()
	if e.Selection() != "ab\ncd" {
		t.Fatalf("%q", e.Selection())
	}
	e.ClickAt(e.gutterW()+1, 1)
	if e.cur != (pos{1, 1}) || e.HasSelection() {
		t.Fatalf("click -> %v", e.cur)
	}
}

func TestVimCommandLineAndVisualRender(t *testing.T) {
	e := vimEd("select 1\nfrom t")
	vkeys(e, "V")
	th := NewTheme(configTheme())
	if !strings.Contains(e.vimBadge(), "VL") {
		t.Fatalf("badge %q", e.vimBadge())
	}
	e.View(th, true)
	vkeys(e, "esc", ":", "w", "q")
	if out := e.View(th, true); !strings.Contains(out, ":wq") {
		t.Fatalf("the command line should show on the bottom row:\n%s", out)
	}
}

// R-021: count x operator count is clamped, so 99999d99999w cannot freeze the UI goroutine.
func TestVimCountProductRefused(t *testing.T) {
	e := vimEd("a b c d e")
	vkeys(e, "9", "9", "9", "9", "9", "d", "9", "9", "9", "9", "9", "w")
	if e.Text() != "a b c d e" {
		t.Fatalf("an oversized count must not run: %q", e.Text())
	}
	if _, note := e.takeEx(); !strings.Contains(note, "count too large") {
		t.Fatalf("no refusal note: %q", note)
	}
}

func TestM4EditorWrapsLongLines(t *testing.T) {
	e := NewEditor("postgres", EditorOpts{Wrap: true})
	e.W, e.H = 20, 6
	text := strings.Repeat("abcdefghij", 5) // 50 runes, 14 per row
	e.SetText(text)
	e.move(0, 50, false)
	var rows []string
	for _, l := range strings.Split(stripANSI(e.View(NewTheme(config.Theme{}), true)), "\n") {
		rows = append(rows, strings.TrimSpace(strings.SplitN(l, "│", 2)[1]))
	}
	if !strings.Contains(strings.Join(rows[:4], ""), text) {
		t.Fatalf("wrapped rows %q", rows)
	}
	if e.curY < 0 {
		t.Fatal("the cursor row must be known")
	}
	e.Opts.Wrap = false
	e.left = 0
	if v := stripANSI(e.View(NewTheme(config.Theme{}), true)); strings.Contains(v, text) {
		t.Fatal("unwrapped, the line is cut at the pane width")
	}
}
