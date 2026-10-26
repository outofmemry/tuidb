package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Milestone 8: small conveniences.

// ---- connection details and colour (M8.2) ------------------------------------------------

// statusColours are the swatches of the colour picker; the tag's own colour comes back with "default".
var statusColours = []struct{ name, hex string }{
	{"red", "#e06c75"}, {"orange", "#d19a66"}, {"yellow", "#e5c07b"}, {"green", "#98c379"},
	{"teal", "#56b6c2"}, {"blue", "#61afef"}, {"purple", "#c678dd"}, {"grey", "#8b919b"},
}

func tlsState(c string) string {
	if c == "" || c == "disable" {
		return "off"
	}
	return "on (" + c + ")"
}

func (w *Workspace) connectionDetails() tea.Cmd {
	c := w.conn
	addr := c.Host
	if c.Port != 0 || c.Host != "" {
		addr = fmt.Sprintf("%s:%d", c.Host, c.EffectivePort())
	}
	lines := []string{
		"Name:      " + c.Name,
		"Driver:    " + c.Driver,
		"Server:    " + w.sess.ServerInfo(),
		"Host:      " + orDefault(addr, "-"),
		"Database:  " + w.sess.CurrentDatabase(),
		"User:      " + orDefault(c.User, "-"),
		"TLS:       " + tlsState(c.SSLMode),
		"Safe mode: " + w.level.Label(),
		"Read only: " + strings.ToLower(yesNo(c.ReadOnly)),
		"Colour:    " + orDefault(c.Color, "(the tag's)"),
	}
	if c.SSH != nil && c.SSH.Host != "" {
		lines = append(lines, fmt.Sprintf("SSH:       %s@%s:%d", c.SSH.User, c.SSH.Host, orInt(c.SSH.Port, 22)))
	}
	if c.SocksProxy != "" {
		lines = append(lines, "SOCKS:     "+c.SocksProxy)
	}
	if c.Source != "" {
		lines = append(lines, "Source:    "+c.Source)
	}
	w.a.push(&textViewDlg{title: "Connection details", lines: lines, actions: []viewAction{{Key: "o", Label: "o: colour", Do: w.pickColour}}})
	return nil
}

// pickColour saves the status colour of the connection (empty restores the tag's colour).
func (w *Workspace) pickColour() tea.Cmd {
	items := []pickItem{{Label: "default", Detail: "the tag's colour", Value: ""}}
	for _, s := range statusColours {
		items = append(items, pickItem{Label: s.name, Detail: s.hex, Value: s.hex})
	}
	items = append(items, pickItem{Label: "custom…", Value: "\x00custom"})
	set := func(hex string) tea.Cmd {
		w.conn.Color = hex
		for i := range w.a.d.Conns {
			if w.a.d.Conns[i].Name == w.conn.Name {
				w.a.d.Conns[i].Color = hex
			}
		}
		if w.conn.Source != "" {
			return w.a.say("colour set for this session (%s comes from %s)", w.conn.Name, w.conn.Source)
		}
		if err := w.a.saveConns(); err != nil {
			return w.a.fail(err)
		}
		return w.a.say("colour saved")
	}
	w.a.push(&pickerDlg{title: "Status colour of " + w.conn.Name, items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		hex := it.Value.(string)
		if hex != "\x00custom" {
			return set(hex)
		}
		f := newForm("Custom colour", []formField{{Key: "hex", Label: "Hex", Kind: fText, Value: w.conn.Color, Hint: "#rrggbb"}}, func(v map[string]string) tea.Cmd {
			return set("#" + strings.TrimPrefix(strings.TrimSpace(v["hex"]), "#")) // stored as # + 6 hex, like the web
		})
		f.validate = func(v map[string]string) error {
			h := strings.TrimPrefix(strings.TrimSpace(v["hex"]), "#")
			if len(h) != 6 {
				return fmt.Errorf("a colour is six hex digits, like #61afef")
			}
			for _, r := range h {
				if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
					return fmt.Errorf("a colour is six hex digits, like #61afef")
				}
			}
			return nil
		}
		w.a.push(f)
		return nil
	}})
	return nil
}

// ---- auto pretty JSON (M8.4) ---------------------------------------------------------------

// autoPrettyJSON is [table] auto_pretty_json (set by NewApp).
var autoPrettyJSON bool

// spacedJSON re-spaces compact JSON for a one-line cell: {"a":1,"b":[1,2]} -> {"a": 1, "b": [1, 2]}.
func spacedJSON(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if len(t) < 2 || (t[0] != '{' && t[0] != '[') || !json.Valid([]byte(t)) {
		return "", false
	}
	var sb strings.Builder
	in, esc := false, false
	for i := 0; i < len(t); i++ {
		c := t[i]
		sb.WriteByte(c)
		switch {
		case esc:
			esc = false
		case in && c == '\\':
			esc = true
		case c == '"':
			in = !in
		case !in && (c == ',' || c == ':') && i+1 < len(t) && t[i+1] != ' ':
			sb.WriteByte(' ')
		}
	}
	return sb.String(), true
}

// gridCellText is cellText, with JSON cells re-spaced when they fit the column (auto_pretty_json).
func gridCellText(v any, w int) string {
	s, ok := v.(string)
	if !autoPrettyJSON || !ok {
		return cellText(v)
	}
	if p, ok := spacedJSON(s); ok && width(p) <= w {
		return p
	}
	return cellText(v)
}

// detailText is cellText for the row-detail pane: JSON cells are indented over several lines (auto_pretty_json).
func detailText(v any) string {
	if s, ok := v.(string); ok && autoPrettyJSON {
		if p := prettyValue(s); p != s {
			return p
		}
	}
	return cellText(v)
}

// ---- one result tab for a long run (M8.5) --------------------------------------------------

// summaryOver: a run of more than this many statements keeps a summary instead of a tab per statement.
const summaryOver = 100

type batchSummary struct {
	total, done, fail int
	rows, affected    int64
	firstErr          string
}

// summaryStep counts one finished statement of a summarised run; the last one produces the "Summary" tab.
func (t *queryTab) summaryStep(m queryDoneMsg) tea.Cmd {
	s := t.sum
	s.done++
	switch {
	case m.err != nil:
		s.fail++
		if s.firstErr == "" {
			s.firstErr = fmt.Sprintf("statement %d: %v", s.done, m.err)
		}
	case m.res != nil:
		s.rows += int64(len(m.res.Rows))
		s.affected += m.res.Affected
	}
	if m.err != nil && t.ws.a.d.Cfg.Editor.ReturnOnError {
		t.queue = nil
	}
	t.status = tookStr(time.Since(t.started))
	if len(t.queue) == 0 { // the last one: now the tab
		lines := []string{fmt.Sprintf("Statements run: %d of %d", s.done, s.total), fmt.Sprintf("Failed: %d", s.fail),
			fmt.Sprintf("Rows returned: %d", s.rows), fmt.Sprintf("Rows affected: %d", s.affected)}
		if s.firstErr != "" {
			lines = append(lines, "First failure: "+s.firstErr)
		}
		t.res = append(t.res, &resultView{title: "Summary", msg: strings.Join(lines, "\n"), took: time.Since(t.started)})
		t.rc = len(t.res) - 1
	}
	return t.next()
}

// ---- structure: a column's attributes as one form (M8.10) --------------------------------------

// structRowForm is space on a structure row: name, type, null, default, primary key and extra in one dialog.
func (t *tableTab) structRowForm(g *Grid) tea.Cmd {
	if t.view != viewStructure || g.cur() == nil {
		return nil
	}
	if !g.Editable || g.cur().State == rowDeleted {
		return t.ws.a.say("%s cannot be edited", t.ref.Name)
	}
	row := g.row
	r := g.Rows[row]
	f := newForm("Column "+cellStr(r.Cur[scName]), []formField{
		{Key: "name", Label: "Name", Kind: fText, Value: cellStr(r.Cur[scName])},
		{Key: "type", Label: "Data type", Kind: fText, Value: cellStr(r.Cur[scType])},
		{Key: "null", Label: "Nullable", Kind: fBool, Value: fmt.Sprint(cellStr(r.Cur[scNull]) == "YES")},
		{Key: "default", Label: "Default", Kind: fText, Value: cellStr(r.Cur[scDefault]), Hint: "empty: none"},
		{Key: "pk", Label: "Primary key", Kind: fBool, Value: fmt.Sprint(cellStr(r.Cur[scPK]) == "YES")},
		{Key: "extra", Label: "Extra", Kind: fText, Value: cellStr(r.Cur[scExtra])},
	}, func(v map[string]string) tea.Cmd {
		if row >= len(g.Rows) {
			return nil
		}
		set := func(col int, val any) {
			if !sameValue(g.Rows[row].Cur[col], val) {
				g.row, g.col = row, col
				g.SetCell(val)
			}
		}
		var def any
		if d := strings.TrimSpace(v["default"]); d != "" {
			def = d
		}
		yn := func(b string) string { return map[string]string{"true": "YES", "false": "NO"}[b] }
		set(scName, strings.TrimSpace(v["name"]))
		set(scType, strings.TrimSpace(v["type"]))
		set(scNull, yn(v["null"]))
		set(scDefault, def)
		set(scPK, yn(v["pk"]))
		set(scExtra, strings.TrimSpace(v["extra"]))
		return nil
	})
	f.validate = func(v map[string]string) error {
		if strings.TrimSpace(v["name"]) == "" || strings.TrimSpace(v["type"]) == "" {
			return fmt.Errorf("a column needs a name and a data type")
		}
		return nil
	}
	f.hotHint = "foreign key: Enter on its cell"
	t.ws.a.push(f)
	return nil
}
