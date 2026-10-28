package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
	"webtuidb/internal/diagram"
)

// M13.1 ER diagram: add foreign key, delete table, switch schema, copy column name, table colours.

// diagramPalette are the colours `c` offers: name -> ANSI 256 code. "default" (the FK-cluster colour) clears the override.
var diagramPalette = []struct{ Name, Code string }{
	{"default", ""}, {"blue", "39"}, {"orange", "208"}, {"green", "41"}, {"pink", "170"},
	{"yellow", "220"}, {"red", "203"}, {"cyan", "51"}, {"purple", "135"}, {"grey", "245"},
}

// colourCode is the one vocabulary State.DiagramColors holds: an ANSI 256 code. Older files hold a palette name
// (the diagram's `c`) or a hex swatch (the sidebar's "Set colour…"); both are migrated on read.
func colourCode(v string) string {
	if v == "" {
		return ""
	}
	if c := paletteCode(v); c != "" {
		return c
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 255 {
		return v
	}
	if len(v) == 7 && v[0] == '#' {
		for _, sw := range statusColours { // the sidebar's own swatches map by name: nearest-RGB would turn green grey
			if sw.hex == v {
				if c := paletteCode(strings.Replace(sw.name, "teal", "cyan", 1)); c != "" {
					return c
				}
			}
		}
		rgb, err := strconv.ParseUint(v[1:], 16, 32)
		if err != nil {
			return ""
		}
		r, g, b := int(rgb>>16&255), int(rgb>>8&255), int(rgb&255)
		best, bd := "", 1<<30
		for _, p := range diagramPalette {
			if p.Code == "" {
				continue
			}
			pr, pg, pb := xtermRGB(p.Code)
			if d := (r-pr)*(r-pr) + (g-pg)*(g-pg) + (b-pb)*(b-pb); d < bd {
				best, bd = p.Code, d
			}
		}
		return best
	}
	return ""
}

// xtermRGB is the colour of an ANSI 256 code from the 6x6x6 cube or the grey ramp (the palette uses only those).
func xtermRGB(code string) (r, g, b int) {
	n, _ := strconv.Atoi(code)
	lv := []int{0, 95, 135, 175, 215, 255}
	switch {
	case n >= 232:
		v := 8 + 10*(n-232)
		return v, v, v
	case n >= 16:
		n -= 16
		return lv[n/36], lv[n/6%6], lv[n%6]
	}
	return 128, 128, 128
}

// colourName is the palette name of a code, for display.
func colourName(code string) string {
	for _, p := range diagramPalette {
		if p.Code == code && code != "" {
			return p.Name
		}
	}
	return code
}

func paletteCode(name string) string {
	for _, p := range diagramPalette {
		if p.Name == name {
			return p.Code
		}
	}
	return ""
}

var fkActions = []string{"NO ACTION", "RESTRICT", "CASCADE", "SET NULL", "SET DEFAULT"}

// diagramFor loads the diagram of schema ("" = the sidebar's) and opens it.
func (w *Workspace) diagramFor(schema, focus string) tea.Cmd {
	sess := w.sess
	return w.liveCmd(func() func() tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		tbls, err := diagram.Load(ctx, sess, schema)
		var schemas []string
		if err == nil {
			schemas, err = diagram.Schemas(ctx, sess)
		}
		return func() tea.Cmd {
			switch {
			case err != nil:
				return w.a.fail(err)
			case len(tbls) == 0:
				return w.a.say("no tables to draw")
			}
			w.showDiagram(tbls, schema, focus, schemas)
			return nil
		}
	})
}

// loadDef reads one table as the structure the planner works on.
func loadDef(ctx context.Context, sess db.Session, ref db.ObjRef) (db.TableDef, error) {
	cols, err := sess.Columns(ctx, ref)
	if err != nil {
		return db.TableDef{}, err
	}
	d := db.TableDef{Schema: ref.Schema, Name: ref.Name, Columns: cols}
	for _, c := range cols {
		if c.PK {
			d.PrimaryKey = append(d.PrimaryKey, c.Name)
		}
	}
	if sess.Caps().Indexes {
		if d.Indexes, err = sess.Indexes(ctx, ref); err != nil {
			return d, err
		}
	}
	if sess.Caps().ForeignKeys {
		if d.ForeignKeys, err = sess.ForeignKeys(ctx, ref); err != nil {
			return d, err
		}
	}
	return d, nil
}

// previewExec shows the statements in a Preview and sends them through the normal commit path on Enter.
func (w *Workspace) previewExec(stmts []db.Statement, then func(err error) tea.Cmd) tea.Cmd {
	if len(stmts) == 0 {
		return w.a.say("no changes")
	}
	var lines []string
	for _, s := range stmts {
		lines = append(lines, s.Shown()+";", "")
	}
	w.a.push(&textViewDlg{title: fmt.Sprintf("Preview: %d statement(s)", len(stmts)), lines: lines, actions: []viewAction{{Key: "enter", Label: "Enter: apply", Do: func() tea.Cmd {
		return w.execute(stmts, then)
	}}}})
	return nil
}

func tableNames(tbls []diagram.Table) []string {
	out := make([]string, len(tbls))
	for i, t := range tbls {
		out[i] = t.Ref.Name
	}
	return out
}

func findTable(tbls []diagram.Table, name string) (diagram.Table, bool) {
	for _, t := range tbls {
		if t.Ref.Name == name {
			return t, true
		}
	}
	return diagram.Table{}, false
}

// diagramAddFK asks for the key, plans it as a structure change (old def vs old def + FK) and previews it.
func (w *Workspace) diagramAddFK(tbls []diagram.Table, schema string, reopen func() tea.Cmd) tea.Cmd {
	names := tableNames(tbls)
	f := newForm("Add foreign key", []formField{
		{Key: "table", Label: "Table", Kind: fChoice, Options: names, Value: names[0]},
		{Key: "cols", Label: "Columns", Kind: fText, Hint: "comma separated"},
		{Key: "ref", Label: "References table", Kind: fChoice, Options: names, Value: names[0]},
		{Key: "refcols", Label: "Referenced columns", Kind: fText, Hint: "comma separated"},
		{Key: "upd", Label: "ON UPDATE", Kind: fChoice, Options: fkActions, Value: "NO ACTION"},
		{Key: "del", Label: "ON DELETE", Kind: fChoice, Options: fkActions, Value: "NO ACTION"},
		{Key: "name", Label: "Constraint name", Kind: fText, Hint: "empty: generated"},
	}, nil)
	f.validate = func(v map[string]string) error {
		t, _ := findTable(tbls, v["table"])
		r, _ := findTable(tbls, v["ref"])
		have := func(tb diagram.Table, cols []string) error {
			if len(cols) == 0 {
				return fmt.Errorf("name the columns of %s", tb.Ref.Name)
			}
			for _, c := range cols {
				if !contains(diagram.ColumnNames(tb), c) {
					return fmt.Errorf("%s has no column %q", tb.Ref.Name, c)
				}
			}
			return nil
		}
		cols, refc := splitList(v["cols"]), splitList(v["refcols"])
		if err := have(t, cols); err != nil {
			return err
		}
		if err := have(r, refc); err != nil {
			return err
		}
		if len(cols) != len(refc) {
			return fmt.Errorf("the two column lists must be the same length")
		}
		return nil
	}
	f.onSubmit = func(v map[string]string) tea.Cmd {
		t, _ := findTable(tbls, v["table"])
		r, _ := findTable(tbls, v["ref"])
		fk := db.ForeignKey{Name: strings.TrimSpace(v["name"]), Columns: splitList(v["cols"]), RefSchema: r.Ref.Schema, RefTable: r.Ref.Name,
			RefColumns: splitList(v["refcols"]), OnUpdate: v["upd"], OnDelete: v["del"]}
		sess := w.sess
		return w.liveCmd(func() func() tea.Cmd {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			old, err := loadDef(ctx, sess, t.Ref)
			return func() tea.Cmd {
				if err != nil {
					return w.a.fail(err)
				}
				nd := diagram.WithFK(old, fk)
				stmts, err := sess.PlanTable(&old, &nd)
				if err != nil {
					return w.a.fail(err)
				}
				return w.previewExec(stmts, func(err error) tea.Cmd {
					if err != nil {
						return nil
					}
					w.forget(&t.Ref)
					return tea.Batch(w.a.say("foreign key added to %s", t.Ref.Name), reopen())
				})
			}
		})
	}
	w.a.push(f)
	return nil
}

// diagramColors are the remembered colours of this connection ("schema.table" -> ANSI code).
func (w *Workspace) diagramColors() map[string]string {
	m := map[string]string{}
	for k, v := range w.a.d.State.DiagramColors(w.conn.Name) {
		if c := colourCode(v); c != "" {
			m[k] = c
		}
	}
	return m
}

func (w *Workspace) saveDiagramColors(m map[string]string) error {
	return w.a.d.State.SetDiagramColors(w.conn.Name, m)
}

// diagramDropTable picks a table, asks for its name and drops it through the usual admin plan.
func (w *Workspace) diagramDropTable(tbls []diagram.Table, reopen func() tea.Cmd) tea.Cmd {
	var items []pickItem
	for _, t := range tbls {
		items = append(items, pickItem{Label: t.Ref.Name, Detail: fmt.Sprintf("%d columns", len(t.Columns)), Value: t.Ref})
	}
	w.a.push(&pickerDlg{title: "Delete which table?", filter: true, items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		ref := it.Value.(db.ObjRef)
		f := newForm("Delete "+ref.Name+"?", []formField{
			{Kind: fLabel, Value: "This cannot be undone. Type the name to confirm."},
			{Key: "confirm", Label: "Type its name", Kind: fText},
		}, func(map[string]string) tea.Cmd {
			stmts, err := w.sess.PlanAdmin(db.OpDropObject, ref, map[string]string{})
			if err != nil {
				return w.a.fail(err)
			}
			return w.execute(stmts, func(err error) tea.Cmd {
				if err != nil {
					return nil
				}
				w.closeTabsFor(ref, true)
				return tea.Batch(w.sb.load(), w.a.say("dropped %s", ref.Name), reopen())
			})
		})
		f.validate = func(v map[string]string) error {
			if v["confirm"] != ref.Name {
				return fmt.Errorf("type %q to confirm", ref.Name)
			}
			return nil
		}
		f.submit = "Enter: drop"
		w.a.push(f)
		return nil
	}})
	return nil
}

// diagramCopyColumn picks a column of any box and copies its name.
// shortcut: a filtered list of table.column instead of a cursor inside the drawn box; upgrade by mapping lines to columns.
func (w *Workspace) diagramCopyColumn(tbls []diagram.Table) tea.Cmd {
	var items []pickItem
	for _, t := range tbls {
		for _, c := range t.Columns {
			items = append(items, pickItem{Label: t.Ref.Name + "." + c.Name, Detail: c.Type, Value: c.Name})
		}
	}
	w.a.push(&pickerDlg{title: "Copy column name", filter: true, items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		return w.a.copyText(it.Value.(string), "column name")
	}})
	return nil
}

// diagramSetColor picks a table, then a colour; it is remembered per connection in state.json.
func (w *Workspace) diagramSetColor(tbls []diagram.Table, schema string, colors map[string]string) tea.Cmd {
	var items []pickItem
	for _, t := range tbls {
		items = append(items, pickItem{Label: t.Ref.Name, Detail: orDefault(colourName(colors[t.Ref.Schema+"."+t.Ref.Name]), "cluster colour"), Value: t.Ref})
	}
	w.a.push(&pickerDlg{title: "Colour which table?", filter: true, items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		ref := it.Value.(db.ObjRef)
		var cs []pickItem
		for _, p := range diagramPalette {
			cs = append(cs, pickItem{Label: p.Name, Value: p.Code})
		}
		w.a.push(&pickerDlg{title: "Colour for " + ref.Name, items: cs, onPick: func(c pickItem, _ bool) tea.Cmd {
			key, code := ref.Schema+"."+ref.Name, c.Value.(string)
			if code == "" { // "default"
				delete(colors, key)
			} else {
				colors[key] = code
			}
			if err := w.saveDiagramColors(colors); err != nil {
				return w.a.fail(err)
			}
			return nil
		}})
		return nil
	}})
	return nil
}

// diagramSchemaPicker switches the diagram to another schema.
func (w *Workspace) diagramSchemaPicker(schemas []string, cur string, open func(schema string) tea.Cmd) tea.Cmd {
	if len(schemas) == 0 {
		return w.a.say("%s has no schemas", w.conn.Driver)
	}
	var items []pickItem
	for _, s := range schemas {
		items = append(items, pickItem{Label: s, Value: s})
	}
	w.a.push(&pickerDlg{title: "Diagram of which schema?", filter: true, items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		return open(it.Value.(string))
	}})
	return nil
}
