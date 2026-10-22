package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"tuidb/internal/keymap"
)

// jnode is an ordered JSON value (encoding/json maps would lose key order, which matters for documents).
type jnode struct {
	key  string
	kind byte // 'o' object, 'a' array, 'v' scalar
	val  string
	kids []*jnode
}

func parseOrdered(dec *json.Decoder) (*jnode, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch d := tok.(type) {
	case json.Delim:
		n := &jnode{kind: 'o'}
		if d == '[' {
			n.kind = 'a'
		}
		for i := 0; dec.More(); i++ {
			key := fmt.Sprint(i)
			if n.kind == 'o' {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ = kt.(string)
			}
			kid, err := parseOrdered(dec)
			if err != nil {
				return nil, err
			}
			kid.key = key
			n.kids = append(n.kids, kid)
		}
		if _, err := dec.Token(); err != nil { // closing delimiter
			return nil, err
		}
		return n, nil
	case json.Number:
		return &jnode{kind: 'v', val: d.String()}, nil
	default:
		b, _ := json.Marshal(d)
		return &jnode{kind: 'v', val: string(b)}, nil
	}
}

func parseDoc(s string) (*jnode, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	n, err := parseOrdered(dec)
	if err == nil {
		if _, err2 := dec.Token(); err2 != io.EOF {
			return nil, fmt.Errorf("trailing data after JSON value")
		}
	}
	return n, err
}

// docLines is the Json view: every document pretty-printed, one array.
func docLines(docs []string) []string {
	var out []string
	for i, d := range docs {
		var b bytes.Buffer
		if json.Indent(&b, []byte(d), "", "  ") != nil {
			b.Reset()
			b.WriteString(d)
		}
		text := b.String()
		if i < len(docs)-1 {
			text += ","
		}
		out = append(out, strings.Split(text, "\n")...)
	}
	return out
}

// JSON token kinds beyond the lexer's, so highlight can tell keys from string values.
const (
	tkKey tokKind = 16 + iota
	tkPunct
	tkBool
	tkNullLit
)

// jsonKinds classifies the runes of one line of JSON: keys, strings, numbers, literals, punctuation.
func jsonKinds(line string) []tokKind {
	r := []rune(line)
	k := make([]tokKind, len(r))
	fill := func(i, j int, kind tokKind) {
		for ; i < j; i++ {
			k[i] = kind
		}
	}
	for i := 0; i < len(r); {
		c, j := r[i], i+1
		switch {
		case c == '"':
			for j < len(r) && r[j] != '"' {
				if r[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j+1, len(r))
			kind, n := tkString, j
			for n < len(r) && r[n] == ' ' {
				n++
			}
			if n < len(r) && r[n] == ':' {
				kind = tkKey
			}
			fill(i, j, kind)
		case c == '-' || c >= '0' && c <= '9':
			for j < len(r) && strings.ContainsRune("0123456789.eE+-", r[j]) {
				j++
			}
			fill(i, j, tkNumber)
		case c >= 'a' && c <= 'z':
			for j < len(r) && r[j] >= 'a' && r[j] <= 'z' {
				j++
			}
			switch string(r[i:j]) {
			case "true", "false":
				fill(i, j, tkBool)
			case "null":
				fill(i, j, tkNullLit)
			}
		case strings.ContainsRune("{}[],:", c):
			k[i] = tkPunct
		}
		i = j
	}
	return k
}

// jsonValueStyle colors one scalar the way the Json view does.
func jsonValueStyle(th Theme, v string) lipgloss.Style {
	switch {
	case strings.HasPrefix(v, `"`):
		return th.String
	case v == "null":
		return th.Null
	case v == "true" || v == "false":
		return th.Keyword
	case v != "" && (v[0] == '-' || v[0] >= '0' && v[0] <= '9'):
		return th.Number
	}
	return th.Cell
}

type treeRow struct {
	id    string
	depth int
	node  *jnode
	guide string // the ancestors' guide lines ("│   " or "    " per level below the roots)
	last  bool   // last child of its parent: drawn with └ instead of ├
	index bool   // an array element: its key is a position, not a name
}

// docTree is the Tree view state.
type docTree struct {
	roots  []*jnode
	open   map[string]bool
	sel    int
	top    int
	cached []treeRow // flattened rows; nil after any fold change
}

func newDocTree(docs []string) *docTree {
	t := &docTree{open: map[string]bool{}}
	for i, d := range docs {
		n, err := parseDoc(d)
		if err != nil {
			n = &jnode{kind: 'v', val: "(invalid JSON: " + err.Error() + ")"}
		}
		n.key = fmt.Sprintf("[%d]", i)
		t.roots = append(t.roots, n)
		t.open[fmt.Sprint(i)] = true
	}
	return t
}

func (t *docTree) rows() []treeRow {
	if t.cached != nil {
		return t.cached
	}
	var out []treeRow
	var walk func(n *jnode, id string, depth int, guide string, last bool)
	walk = func(n *jnode, id string, depth int, guide string, last bool) {
		out = append(out, treeRow{id: id, depth: depth, node: n, guide: guide, last: last})
		if n.kind != 'v' && t.open[id] {
			kg := ""
			if depth > 0 {
				kg = guide + "│   "
				if last {
					kg = guide + "    "
				}
			}
			for i, k := range n.kids {
				at := len(out)
				walk(k, id+"."+fmt.Sprint(i), depth+1, kg, i == len(n.kids)-1)
				out[at].index = n.kind == 'a'
			}
		}
	}
	for i, r := range t.roots {
		walk(r, fmt.Sprint(i), 0, "", false)
	}
	t.cached = out
	return out
}

func (t *docTree) setAll(open bool) {
	var walk func(n *jnode, id string)
	walk = func(n *jnode, id string) {
		if n.kind == 'v' {
			return
		}
		if open {
			t.open[id] = true
		} else {
			delete(t.open, id)
		}
		for i, k := range n.kids {
			walk(k, id+"."+fmt.Sprint(i))
		}
	}
	for i, r := range t.roots {
		walk(r, fmt.Sprint(i))
	}
}

func (t *docTree) input(key string) {
	defer func() { t.cached = nil }() // folds may have changed
	rows := t.rows()
	if len(rows) == 0 {
		return
	}
	t.sel = clamp(t.sel, 0, len(rows)-1)
	cur := rows[t.sel]
	switch key {
	case "down", "j":
		t.sel = min(t.sel+1, len(rows)-1)
	case "up", "k":
		t.sel = max(t.sel-1, 0)
	case "pgdown":
		t.sel = min(t.sel+15, len(rows)-1)
	case "pgup":
		t.sel = max(t.sel-15, 0)
	case "g", "home":
		t.sel = 0
	case "G", "end":
		t.sel = len(rows) - 1
	case "enter", "space":
		if cur.node.kind != 'v' {
			t.open[cur.id] = !t.open[cur.id]
		}
	case "right", "l":
		if cur.node.kind != 'v' {
			t.open[cur.id] = true
		}
	case "left", "h":
		if cur.node.kind != 'v' && t.open[cur.id] {
			delete(t.open, cur.id)
		} else if i := strings.LastIndex(cur.id, "."); i >= 0 { // collapse into the parent
			for j, r := range rows {
				if r.id == cur.id[:i] {
					t.sel = j
				}
			}
		}
	case "E":
		t.setAll(true)
	case "C":
		t.setAll(false)
	}
}

func (t *docTree) view(th Theme, W, H int) string {
	rows := t.rows()
	t.sel = clamp(t.sel, 0, max(0, len(rows)-1))
	if t.sel < t.top {
		t.top = t.sel
	}
	if t.sel >= t.top+H {
		t.top = t.sel - H + 1
	}
	var out []string
	if len(rows) == 0 {
		out = append(out, th.Dim.Render("  no documents"))
	}
	for i := t.top; i < min(len(rows), t.top+H); i++ {
		r := rows[i]
		// paint keeps each piece's color and lays the cursor-row tint underneath it
		paint := func(st lipgloss.Style, s string) string {
			if i == t.sel {
				st = st.Inherit(th.CurRow)
			}
			return st.Render(s)
		}
		line := paint(th.Cell, " ")
		if r.depth > 0 {
			conn := "├─"
			if r.last {
				conn = "└─"
			}
			line += paint(th.Dim, r.guide+conn)
		}
		key := paint(th.Bold, r.node.key)
		if r.index || r.depth == 0 && r.node.kind == 'v' {
			key = paint(th.Dim, r.node.key)
		}
		if r.node.kind == 'v' {
			if r.depth > 0 {
				line += paint(th.Dim, "─")
			}
			line += paint(th.Cell, " ") + key + paint(th.Dim, ": ") + paint(jsonValueStyle(th, r.node.val), oneLine(r.node.val))
		} else {
			arrow, size := "▸", fmt.Sprintf("{%d}", len(r.node.kids))
			if t.open[r.id] {
				arrow = "▾"
			}
			if r.node.kind == 'a' {
				size = fmt.Sprintf("[%d]", len(r.node.kids))
			}
			line += paint(th.Accent, arrow) + paint(th.Cell, " ") + key + paint(th.Dim, "  "+size)
		}
		if n := W - width(line); n > 0 {
			line += paint(th.Cell, strings.Repeat(" ", n))
		}
		out = append(out, fit(line, W))
	}
	for len(out) < H {
		out = append(out, strings.Repeat(" ", W))
	}
	return strings.Join(out, "\n")
}

// ---- hooks used by tableTab -------------------------------------------------------------

func (t *tableTab) jsonView(th Theme, W, H int) string {
	if t.jsonCache.docs == nil || &t.jsonCache.docs[0] != &t.docs[0] || len(t.jsonCache.docs) != len(t.docs) {
		t.jsonCache.docs = t.docs
		t.jsonCache.lines = docLines(t.docs)
		t.jsonCache.kinds = make([][]tokKind, len(t.jsonCache.lines)) // once per result, not per frame
		for i, l := range t.jsonCache.lines {
			t.jsonCache.kinds[i] = jsonKinds(l)
		}
	}
	lines, kinds := t.jsonCache.lines, t.jsonCache.kinds
	if len(lines) == 0 {
		return pad(th.Dim.Render("  no documents"), W, H)
	}
	t.jsonTop = clamp(t.jsonTop, 0, max(0, len(lines)-H))
	// The gutter numbers documents, not lines: each number sits on its document's opening line.
	docStart := func(i int) bool {
		return i == 0 || !strings.HasPrefix(lines[i-1], " ") && strings.HasSuffix(lines[i-1], ",")
	}
	doc := 0
	for i := 0; i < t.jsonTop; i++ {
		if docStart(i) {
			doc++
		}
	}
	gw := max(3, len(fmt.Sprint(len(t.docs)))) + 1
	out := make([]string, H)
	for i := range out {
		li := t.jsonTop + i
		if li < len(lines) {
			num := ""
			if docStart(li) {
				doc++
				num = fmt.Sprint(doc)
			}
			out[i] = th.Dim.Render(padLeft(num, gw-1)+" ") + " " + highlight(th, lines[li], kinds[li])
		}
		out[i] = fit(out[i], W)
	}
	return strings.Join(out, "\n")
}

func (t *tableTab) docInput(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	key := keymap.FromMsg(k)
	if t.view == viewTree {
		if t.tree == nil {
			t.tree = newDocTree(t.docs)
		}
		t.tree.input(key)
		return nil
	}
	switch key {
	case "down", "j":
		t.jsonTop++
	case "up", "k":
		t.jsonTop = max(0, t.jsonTop-1)
	case "pgdown", "space":
		t.jsonTop += 15
	case "pgup":
		t.jsonTop = max(0, t.jsonTop-15)
	case "g", "home":
		t.jsonTop = 0
	case "G", "end":
		t.jsonTop = 1 << 30
	case "c":
		return tea.Batch(tea.SetClipboard(strings.Join(docLines(t.docs), "\n")), t.ws.a.say("copied JSON"))
	}
	return nil
}

// quickLookDlg shows one cell value: a collapsible tree when it is a JSON object or array
// (r switches to the raw text), otherwise the text, scrollable.
type quickLookDlg struct {
	title string
	text  string
	tree  *docTree
	raw   bool
	top   int
	lines []string    // wrapped raw lines, built once per width
	kinds [][]tokKind // their token kinds (JSON coloring)
	wrapW int
}

// quickLookMax caps what quick look formats: a huge cell would be wrapped and pretty-printed per frame.
const quickLookMax = 256 << 10

func newQuickLook(title, text string) *quickLookDlg {
	if len(text) > quickLookMax {
		text = text[:quickLookMax] + fmt.Sprintf("\n… truncated (%d bytes in total; c copies everything)", len(text))
		return &quickLookDlg{title: "Quick look: " + title, text: text, raw: true}
	}
	q := &quickLookDlg{title: "Quick look: " + title, text: text}
	if t := strings.TrimSpace(text); len(t) > 1 && (t[0] == '{' || t[0] == '[') {
		q.text = text
		if _, err := parseDoc(t); err == nil {
			q.tree = newDocTree([]string{t})
		}
	}
	q.raw = q.tree == nil
	return q
}

func (q *quickLookDlg) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return q, nil
	}
	key := keymap.FromMsg(k)
	switch key {
	case "esc", "q":
		return nil, nil
	case "c":
		return q, tea.SetClipboard(q.text)
	case "r":
		if q.tree != nil {
			q.raw = !q.raw
		}
		return q, nil
	}
	if !q.raw {
		q.tree.input(key)
		return q, nil
	}
	switch key {
	case "down", "j":
		q.top++
	case "up", "k":
		q.top = max(0, q.top-1)
	case "pgdown", "space":
		q.top += 10
	case "pgup":
		q.top = max(0, q.top-10)
	case "g", "home":
		q.top = 0
	}
	return q, nil
}

func (q *quickLookDlg) View(th Theme, maxW, maxH int) string {
	w := min(maxW-4, 100)
	vis := max(4, maxH-8)
	var body []string
	hint := "c: copy   Esc: close"
	if !q.raw {
		body = strings.Split(q.tree.view(th, w-4, min(vis, len(q.tree.rows()))), "\n") // the box hugs the tree
		hint = "↑↓: move   ↵: fold   E/C: expand/collapse all   r: raw text   " + hint
	} else {
		if q.lines == nil || q.wrapW != w-4 {
			q.lines, q.kinds, q.wrapW = nil, nil, w-4
			for _, l := range strings.Split(prettyValue(q.text), "\n") {
				k := make([]tokKind, runeLen(l)) // plain text stays uncolored
				if q.tree != nil {
					k = jsonKinds(l) // classified before wrapping, so a wrapped string keeps its color
				}
				for _, part := range wrapText(l, w-4) {
					n := min(runeLen(part), len(k))
					q.lines, q.kinds, k = append(q.lines, part), append(q.kinds, k[:n]), k[n:]
				}
			}
		}
		q.top = clamp(q.top, 0, max(0, len(q.lines)-vis))
		for i := q.top; i < min(len(q.lines), q.top+vis); i++ {
			body = append(body, highlight(th, q.lines[i], q.kinds[i]))
		}
		hint = "↑↓: scroll   " + hint
		if q.tree != nil {
			hint = "r: tree   " + hint
		}
	}
	return box(th, q.title, body, hint, w)
}
