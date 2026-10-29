package ui

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

// ---- sort by several columns, hide / show columns ---------------------------------------

// sortColumns is `sort_columns`: tick the columns, then pick asc / desc for each; the ticked order is the sort priority.
func (t *tableTab) sortColumns() tea.Cmd {
	if len(t.cols) == 0 {
		return t.ws.a.say("the columns are still loading")
	}
	var items []checkItem
	desc := map[string]bool{}
	seen := map[string]bool{}
	have := map[string]bool{}
	for _, c := range t.cols {
		have[c.Name] = true
	}
	for _, s := range t.sort { // the current sort keeps its priority at the top
		if !have[s.Column] { // the column was renamed or dropped since it was saved
			continue
		}
		items = append(items, checkItem{Label: s.Column, Checked: true})
		desc[s.Column], seen[s.Column] = s.Desc, true
	}
	for _, c := range t.cols {
		if !seen[c.Name] {
			items = append(items, checkItem{Label: c.Name})
		}
	}
	t.ws.a.push(&checklistDlg{title: "Sort by (ticked columns, in this order)", items: items, onOK: func(items []checkItem) tea.Cmd {
		var ticked []checkItem
		for _, it := range items {
			if it.Checked {
				ticked = append(ticked, it)
			}
		}
		sort.SliceStable(ticked, func(i, j int) bool { return ticked[i].Seq < ticked[j].Seq }) // the order they were ticked in
		var picked []string
		for _, it := range ticked {
			picked = append(picked, it.Label)
		}
		apply := func(sort []db.Sort) tea.Cmd {
			return t.guard(func() tea.Cmd {
				t.sort, t.offset, t.toTop = sort, 0, true
				t.saveState()
				return t.load()
			})
		}
		if len(picked) == 0 {
			return apply(nil)
		}
		var fields []formField
		for _, c := range picked {
			v := "asc"
			if desc[c] {
				v = "desc"
			}
			fields = append(fields, formField{Key: c, Label: c, Kind: fChoice, Options: []string{"asc", "desc"}, Value: v})
		}
		t.ws.a.push(newForm("Direction", fields, func(v map[string]string) tea.Cmd {
			var out []db.Sort
			for _, c := range picked {
				out = append(out, db.Sort{Column: c, Desc: v[c] == "desc"})
			}
			return apply(out)
		}))
		return nil
	}})
	return nil
}

func (t *tableTab) hideColumn() tea.Cmd {
	g := t.grid
	if g.col >= len(g.Cols) || len(t.cols) == 0 {
		return t.ws.a.say("no column to hide")
	}
	name := g.Cols[g.col].Name
	if len(t.cols)-t.hiddenCount() <= 1 {
		return t.ws.a.say("at least one column must stay visible")
	}
	return t.guard(func() tea.Cmd {
		t.hidden[name] = true
		t.saveState()
		return tea.Batch(t.load(), t.ws.a.say("hid %s (show all columns brings it back)", name))
	})
}

func (t *tableTab) showAllColumns() tea.Cmd {
	if len(t.hidden) == 0 {
		return t.ws.a.say("every column is already shown")
	}
	return t.guard(func() tea.Cmd {
		t.hidden = map[string]bool{}
		t.saveState()
		return t.load()
	})
}

// filterColumns is the column list of the filter bar in table.filter_column_sort order.
func (t *tableTab) filterColumns() []db.Column {
	cols := append([]db.Column(nil), t.cols...)
	if t.ws.a.d.Cfg.Table.FilterColumnSort == "name" {
		sort.SliceStable(cols, func(i, j int) bool { return strings.ToLower(cols[i].Name) < strings.ToLower(cols[j].Name) })
	}
	return cols
}

// ---- Set Value: date picker, file ---------------------------------------------------------

type setValueDate struct{}
type setValueFile struct{}
type setValueExpr struct{}

// maxCellFile caps "Add a file…": the bytes are held in the grid until commit.
const maxCellFile = 16 << 20

func setValueExtras() []pickItem {
	return []pickItem{{Label: "Date picker…", Value: setValueDate{}}, {Label: "Add a file…", Value: setValueFile{}}, {Label: "Advanced…", Value: setValueExpr{}}}
}

// pickSetValue applies the chosen Set Value entry to the cursor cell of g.
func (a *App) pickSetValue(g *Grid, v any) tea.Cmd {
	switch v.(type) {
	case setValueDate:
		f := newForm("Date picker", []formField{
			{Key: "date", Label: "Date (YYYY-MM-DD)", Kind: fText, Value: time.Now().Format(time.DateOnly)},
			{Key: "time", Label: "Time (HH:MM:SS, optional)", Kind: fText},
		}, func(m map[string]string) tea.Cmd {
			lit := strings.TrimSpace(m["date"])
			if tm := strings.TrimSpace(m["time"]); tm != "" {
				lit += " " + tm
			}
			g.SetCell(lit)
			return nil
		})
		f.validate = func(m map[string]string) error {
			if _, err := time.Parse(time.DateOnly, strings.TrimSpace(m["date"])); err != nil {
				return fmt.Errorf("date must look like 2026-10-28")
			}
			if tm := strings.TrimSpace(m["time"]); tm != "" {
				if _, err := time.Parse(time.TimeOnly, tm); err != nil {
					return fmt.Errorf("time must look like 13:04:05")
				}
			}
			return nil
		}
		a.push(f)
	case setValueExpr:
		a.push(newForm("Advanced: SQL expression", []formField{{Key: "expr", Label: "Expression", Kind: fText, Hint: "e.g. now() + interval '1 day'"}}, func(m map[string]string) tea.Cmd {
			if e := strings.TrimSpace(m["expr"]); e != "" {
				if err := checkExpr(e); err != nil {
					return a.fail(err)
				}
				g.SetCell(db.Expr(e))
			}
			return nil
		}))
	case setValueFile:
		a.push(newForm("Add a file", []formField{{Key: "path", Label: "File", Kind: fText}}, func(m map[string]string) tea.Cmd {
			path := strings.TrimSpace(m["path"])
			st, err := os.Stat(path)
			switch {
			case err != nil:
				return a.fail(err)
			case st.Size() > maxCellFile:
				return a.fail(fmt.Errorf("%s is %d MB: files over %d MB are not loaded into a cell", path, st.Size()>>20, maxCellFile>>20))
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return a.fail(err)
			}
			g.SetCell(b)
			return a.say("loaded %d bytes from %s", len(b), path)
		}))
	default:
		g.SetCell(v)
	}
	return nil
}

// ---- quick look: hex editing, encodings -----------------------------------------------------

var encodings = []string{"Hex dump", "Base64", "UTF-8", "Latin-1 (ISO-8859-1)", "UTF-16"}

// decodeAs shows raw in the named encoding.
func decodeAs(raw []byte, enc string) string {
	switch enc {
	case "Hex dump":
		return hexDump(raw)
	case "Base64":
		return base64Lines(raw)
	case "WKT":
		if w, ok := db.WKT(raw); ok {
			return w
		}
		return hexDump(raw)
	case "Latin-1 (ISO-8859-1)":
		r := make([]rune, len(raw))
		for i, b := range raw {
			r[i] = rune(b)
		}
		return string(r)
	case "UTF-16":
		be := len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF // a BOM picks the byte order, little endian otherwise
		if len(raw) >= 2 && (be || raw[0] == 0xFF && raw[1] == 0xFE) {
			raw = raw[2:]
		}
		u := make([]uint16, len(raw)/2)
		for i := range u {
			if be {
				u[i] = uint16(raw[2*i])<<8 | uint16(raw[2*i+1])
			} else {
				u[i] = uint16(raw[2*i+1])<<8 | uint16(raw[2*i])
			}
		}
		return string(utf16.Decode(u))
	}
	return strings.ToValidUTF8(string(raw), "�")
}

// parseHex reads "de ad be ef" (spaces, newlines and an optional offset-free dump are fine): two digits per byte.
func parseHex(s string) ([]byte, error) {
	s = strings.Join(strings.Fields(s), "")
	b, err := hex.DecodeString(s)
	if err != nil { // odd length or a non-hex digit: one plain message (T-P3-17), not encoding/hex's
		return nil, errors.New("Hex needs pairs of 0-9 a-f.")
	}
	return b, nil
}

// newQuickLookValue opens quick look on a cell. A bytes cell starts as a hex dump; v switches the
// encoding it is decoded with, and e edits the bytes as hex when onSave is set.
func (a *App) newQuickLookValue(title string, v any, onSave func([]byte) tea.Cmd) *quickLookDlg {
	raw, isBytes := v.([]byte)
	text := plain(v)
	enc := "UTF-8"
	if isBytes {
		text, enc = hexDump(raw), "Hex dump"
	} else if s, ok := v.(string); ok {
		raw = []byte(s)
	}
	if isBytes && a.d.Cfg.Table.DecodeSpatial {
		if w, ok := db.WKT(raw); ok {
			text, enc = w, "WKT" // a geometry shows as WKT; v switches back to the bytes
		}
	}
	q := newQuickLook(title, text)
	q.a, q.rawBytes, q.isBytes, q.onSave, q.enc = a, raw, isBytes, onSave, enc
	if isBytes {
		q.info = binaryInfo(raw, a.d.Cfg.Table.ImagePreview)
		if strings.Contains(q.info, "image/") {
			q.note = "terminal shows the image size only; the web UI previews the picture"
		}
	}
	return q
}

func (q *quickLookDlg) pickEncoding() tea.Cmd {
	var items []pickItem
	encs := encodings
	if _, ok := db.WKT(q.rawBytes); ok {
		encs = append([]string{"WKT"}, encs...)
	}
	for _, e := range encs {
		d := ""
		if e == q.enc {
			d = "current"
		}
		items = append(items, pickItem{Label: e, Detail: d, Value: e})
	}
	q.a.push(&pickerDlg{title: "View using encoding", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		q.enc = it.Value.(string)
		q.text = decodeAs(q.rawBytes, q.enc)
		q.tree, q.raw, q.lines, q.top = nil, true, nil, 0
		return nil
	}})
	return nil
}

func (q *quickLookDlg) editHex() tea.Cmd {
	var sb strings.Builder
	for i, b := range q.rawBytes {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%02x", b)
	}
	f := newForm("Edit bytes (two hex digits per byte)", []formField{{Key: "hex", Label: "Hex", Kind: fText, Value: sb.String()}}, func(m map[string]string) tea.Cmd {
		b, _ := parseHex(m["hex"])
		q.rawBytes = b
		if q.enc == "Hex dump" {
			q.text, q.lines = hexDump(b), nil
		}
		return q.onSave(b)
	})
	f.validate = func(m map[string]string) error { _, err := parseHex(m["hex"]); return err }
	q.a.push(f)
	return nil
}

// ---- item overview --------------------------------------------------------------------------

type overviewMsg struct {
	ref  db.ObjRef
	text []string
	err  error
}

// itemOverview shows row estimate, size, columns, indexes and foreign keys of the selected table.
func (w *Workspace) itemOverview() tea.Cmd {
	ref, ok := w.targetRef()
	if !ok {
		return w.a.say("select or open a table first")
	}
	detail := ""
	for _, o := range w.sb.objs {
		if o.ObjRef == ref {
			detail = o.Detail
		}
	}
	return w.async(-1, func(ctx context.Context, sess db.Session) tea.Msg {
		m := overviewMsg{ref: ref}
		cols, err := sess.Columns(ctx, ref)
		if err != nil {
			m.err = err
			return m
		}
		idx, _ := sess.Indexes(ctx, ref)
		fks, _ := sess.ForeignKeys(ctx, ref)
		rows := "unknown"
		if n, err := sess.Count(ctx, db.RowQuery{ObjRef: ref}, true); err == nil {
			rows = fmt.Sprint(n.N)
			if !n.Exact {
				rows = "~" + rows
			}
		}
		m.text = append(m.text, "Rows: "+rows, "Size: "+orDefault(detail, "not reported"), "", fmt.Sprintf("Columns (%d)", len(cols)))
		for _, c := range cols {
			flags := ""
			if c.PK {
				flags += " PK"
			}
			if !c.Nullable {
				flags += " NOT NULL"
			}
			m.text = append(m.text, fmt.Sprintf("  %-24s %s%s", c.Name, c.Type, flags))
		}
		m.text = append(m.text, "", fmt.Sprintf("Indexes (%d)", len(idx)))
		for _, ix := range idx {
			u := ""
			if ix.Unique {
				u = " unique"
			}
			m.text = append(m.text, fmt.Sprintf("  %s (%s)%s", ix.Name, strings.Join(ix.Columns, ", "), u))
		}
		m.text = append(m.text, "", fmt.Sprintf("Foreign keys (%d)", len(fks)))
		for _, fk := range fks {
			m.text = append(m.text, fmt.Sprintf("  %s (%s) -> %s (%s)", fk.Name, strings.Join(fk.Columns, ", "), fk.RefTable, strings.Join(fk.RefColumns, ", ")))
		}
		return m
	})
}

func (w *Workspace) handleM4(msg tea.Msg) (tea.Cmd, bool) {
	if m, ok := msg.(overviewMsg); ok {
		if m.err != nil {
			return w.a.fail(m.err), true
		}
		w.a.push(&textViewDlg{title: "Overview: " + m.ref.Name, lines: m.text})
		return nil, true
	}
	return nil, false
}

// ---- find / replace in a grid (M9.1) -------------------------------------------------------

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
