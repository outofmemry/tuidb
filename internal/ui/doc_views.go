package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"

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

type treeRow struct {
	id    string
	depth int
	node  *jnode
}

// docTree is the Tree view state.
type docTree struct {
	roots []*jnode
	open  map[string]bool
	sel   int
	top   int
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
	var out []treeRow
	var walk func(n *jnode, id string, depth int)
	walk = func(n *jnode, id string, depth int) {
		out = append(out, treeRow{id: id, depth: depth, node: n})
		if n.kind != 'v' && t.open[id] {
			for i, k := range n.kids {
				walk(k, id+"."+fmt.Sprint(i), depth+1)
			}
		}
	}
	for i, r := range t.roots {
		walk(r, fmt.Sprint(i), 0)
	}
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
		out = append(out, th.Dim.Render("  (no documents)"))
	}
	for i := t.top; i < min(len(rows), t.top+H); i++ {
		r := rows[i]
		ind := strings.Repeat("  ", r.depth)
		var line string
		switch r.node.kind {
		case 'v':
			style := th.Cell
			switch {
			case strings.HasPrefix(r.node.val, `"`):
				style = th.String
			case r.node.val == "null":
				style = th.Null
			default:
				style = th.Number
			}
			line = " " + ind + "  " + th.Accent.Render(r.node.key) + ": " + style.Render(oneLine(r.node.val))
		default:
			arrow, summary := "▸", ""
			if t.open[r.id] {
				arrow = "▾"
			}
			if r.node.kind == 'o' {
				summary = fmt.Sprintf("{%d fields}", len(r.node.kids))
			} else {
				summary = fmt.Sprintf("[%d items]", len(r.node.kids))
			}
			line = " " + ind + arrow + " " + th.Accent.Render(r.node.key) + " " + th.Dim.Render(summary)
		}
		if i == t.sel {
			line = th.SelRow.Render(fit(stripANSI(line), W))
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
	lines := docLines(t.docs)
	if len(lines) == 0 {
		return pad(th.Dim.Render("  (no documents)"), W, H)
	}
	t.jsonTop = clamp(t.jsonTop, 0, max(0, len(lines)-H))
	kinds := lexLines("mongo", lines)
	var out []string
	for i := t.jsonTop; i < min(len(lines), t.jsonTop+H); i++ {
		out = append(out, " "+highlight(th, lines[i], kinds[i]))
	}
	for len(out) < H {
		out = append(out, "")
	}
	for i := range out {
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
}

func newQuickLook(title, text string) *quickLookDlg {
	q := &quickLookDlg{title: "Quick look: " + title, text: text}
	if t := strings.TrimSpace(text); len(t) > 1 && (t[0] == '{' || t[0] == '[') {
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
	hint := "c copy · Esc close"
	if !q.raw {
		body = strings.Split(q.tree.view(th, w-4, vis), "\n")
		hint = "↑↓ move · ↵ fold · E/C expand/collapse all · r raw text · " + hint
	} else {
		var lines []string
		for _, l := range strings.Split(prettyValue(q.text), "\n") {
			lines = append(lines, wrapText(l, w-4)...)
		}
		q.top = clamp(q.top, 0, max(0, len(lines)-vis))
		body = lines[q.top:min(len(lines), q.top+vis)]
		hint = "↑↓ scroll · " + hint
		if q.tree != nil {
			hint = "r tree · " + hint
		}
	}
	return box(th, q.title, body, hint, w)
}
