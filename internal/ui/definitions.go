package ui

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

// Definitions (views, functions, procedures, triggers) are edited as SQL in a query tab:
// the text is a runnable replacement, so running it goes through the normal safe-mode gate.

type defMsg struct {
	ref  db.ObjRef
	text string
	err  error
}

func (w *Workspace) editDefinition(ref db.ObjRef) tea.Cmd {
	return w.async(-2, func(ctx context.Context, sess db.Session) tea.Msg {
		ddl, err := sess.DDL(ctx, ref)
		return defMsg{ref: ref, text: ddl, err: err}
	})
}

// editableDDL turns a CREATE statement into one that can be run over the existing object.
// shortcut: MySQL routine bodies containing ';' are split by the editor (no DELIMITER support), upgrade when needed.
func editableDDL(dialect string, ref db.ObjRef, ddl string) string {
	d, _ := db.DialectFor(dialect)
	name := ref.Name
	if d.Quote != nil {
		name = d.QualifiedName(ref)
	}
	ddl = strings.TrimSpace(ddl)
	var drop string
	switch ref.Kind {
	case db.KindView:
		if dialect != "postgres" && !strings.Contains(strings.ToUpper(ddl), "OR REPLACE") {
			ddl = strings.Replace(ddl, "CREATE ", "CREATE OR REPLACE ", 1)
		}
	case db.KindMatView:
		drop = "DROP MATERIALIZED VIEW IF EXISTS " + name + ";\n"
	case db.KindFunction, db.KindProcedure:
		if dialect == "mysql" {
			// Create the new body under a temporary name first; the original is only dropped and
			// renamed once the CREATE has succeeded, so a failed edit never destroys the routine.
			kind := strings.ToUpper(string(ref.Kind))
			tmp := ref.Name + "__webtuidb_new"
			body := strings.Replace(ddl, "`"+ref.Name+"`", "`"+tmp+"`", 1)
			body = regexp.MustCompile(`(?i)^CREATE\s+DEFINER=[^ ]+\s+`).ReplaceAllString(body, "CREATE ")
			return fmt.Sprintf("-- Definition of %s. The routine body may contain ';', so the terminator is switched to $$.\n"+
				"-- Running all creates %s first; the old routine is dropped only after that succeeds.\n"+
				"DELIMITER $$\n%s$$\nDROP %s IF EXISTS %s$$\nDELIMITER ;\n"+
				"-- MySQL cannot rename routines: run this tab again after changing `%s` back to `%s` above, or keep the new name.\n",
				ref.Name, tmp, body, kind, name, tmp, ref.Name)
		}
	}
	head := fmt.Sprintf("-- Definition of %s. Edit it, then run all to apply (it asks first in safe mode).\n", ref.Name)
	return head + drop + ddl + "\n"
}

// triggerSQL is a statement pair that replaces trigger t on table ref.
func triggerSQL(dialect string, ref db.ObjRef, t db.Trigger) string {
	d, ok := db.DialectFor(dialect)
	if !ok {
		return ""
	}
	tbl := d.QualifiedName(ref)
	head := fmt.Sprintf("CREATE TRIGGER %s %s %s ON %s", d.Quote(t.Name), t.Timing, t.Event, tbl)
	if dialect == "postgres" {
		return fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON %s;\n%s\n  %s;\n", d.Quote(t.Name), tbl, head, strings.TrimSuffix(t.Statement, ";"))
	}
	if dialect == "cql" { // a Cassandra trigger is a Java class on the server's classpath, not SQL
		return fmt.Sprintf("-- %s runs the Java class %s on every write to the table.\nDROP TRIGGER IF EXISTS %s ON %s;\nCREATE TRIGGER %s ON %s USING '%s';\n",
			t.Name, t.Statement, d.Quote(t.Name), tbl, d.Quote(t.Name), tbl, strings.ReplaceAll(t.Statement, "'", "''"))
	}
	return fmt.Sprintf("DROP TRIGGER IF EXISTS %s;\n%s FOR EACH ROW\n  %s;\n", d.Quote(t.Name), head, strings.TrimSuffix(t.Statement, ";"))
}

func newTriggerSQL(dialect string, ref db.ObjRef) string {
	d, ok := db.DialectFor(dialect)
	if !ok {
		return ""
	}
	if dialect == "cql" {
		return fmt.Sprintf("-- A trigger is a Java class (ITrigger) that must already be in the server's triggers directory.\nCREATE TRIGGER my_trigger ON %s USING 'com.example.MyTrigger';\n", d.QualifiedName(ref))
	}
	if dialect == "postgres" {
		return fmt.Sprintf("-- Create the trigger function first (RETURNS trigger), then run this.\nCREATE TRIGGER my_trigger\n  AFTER INSERT ON %s\n  FOR EACH ROW EXECUTE FUNCTION my_function();\n", d.QualifiedName(ref))
	}
	return fmt.Sprintf("CREATE TRIGGER my_trigger AFTER INSERT ON %s FOR EACH ROW\n  SET @last_insert = NOW();\n", d.QualifiedName(ref))
}

type triggersMsg struct {
	ref  db.ObjRef
	list []db.Trigger
	err  error
}

// triggerMenu lists a table's triggers; picking one opens it for editing, plus "New trigger…".
func (w *Workspace) triggerMenu(ref db.ObjRef) tea.Cmd {
	if !w.caps.Triggers {
		return w.a.say("%s has no triggers", w.conn.Driver)
	}
	return w.async(-2, func(ctx context.Context, sess db.Session) tea.Msg {
		list, err := sess.Triggers(ctx, ref)
		return triggersMsg{ref: ref, list: list, err: err}
	})
}

func (w *Workspace) handleDefinitions(msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case defMsg:
		if m.err != nil {
			return w.a.fail(m.err), true
		}
		t := newQueryTab(w, w.newID())
		t.name = m.ref.Name
		t.ed.SetText(editableDDL(w.caps.Dialect, m.ref, m.text))
		return w.addTab(t), true
	case triggersMsg:
		if m.err != nil {
			return w.a.fail(m.err), true
		}
		items := []pickItem{{Label: "＋ New trigger…", Value: -1}}
		for i, t := range m.list {
			items = append(items, pickItem{Label: t.Name, Detail: t.Timing + " " + t.Event, Value: i})
		}
		w.a.push(&pickerDlg{title: "Triggers on " + m.ref.Name, filter: true, items: items, hint: "Enter: edit name / timing / event / body   Esc: cancel",
			onPick: func(it pickItem, _ bool) tea.Cmd {
				tr := db.Trigger{Name: "my_trigger", Timing: "AFTER", Event: "INSERT", Statement: newTriggerBody(w.caps.Dialect)}
				if i := it.Value.(int); i >= 0 {
					tr = m.list[i]
				}
				w.a.push(w.newTriggerEdit(m.ref, tr))
				return nil
			}})
		return nil, true
	}
	return nil, false
}

// newTriggerBody is the starting body of a new trigger.
func newTriggerBody(dialect string) string {
	switch dialect {
	case "postgres":
		return "FOR EACH ROW EXECUTE FUNCTION my_function()"
	case "cql":
		return "com.example.MyTrigger"
	}
	return "SET @last_insert = NOW()"
}

// triggerEditDlg is the trigger editor: a name / timing / event grid above the body editor. Ctrl+s opens
// the CREATE TRIGGER script (the same one triggerSQL gives) in a query tab, where running it goes through
// the normal safe-mode gate.
type triggerEditDlg struct {
	w      *Workspace
	ref    db.ObjRef
	name   lineEdit
	timing []string
	event  []string
	ti, ei int
	body   *Editor
	focus  int // 0 name, 1 timing, 2 event, 3 body
}

func (w *Workspace) newTriggerEdit(ref db.ObjRef, tr db.Trigger) *triggerEditDlg {
	pick := func(opts []string, cur string) ([]string, int) {
		for i, o := range opts {
			if strings.EqualFold(o, cur) {
				return opts, i
			}
		}
		return append(opts, cur), len(opts) // a value the engine reports that the list lacks (e.g. INSERT OR UPDATE)
	}
	d := &triggerEditDlg{w: w, ref: ref, name: newLine(tr.Name)}
	d.timing, d.ti = pick([]string{"BEFORE", "AFTER", "INSTEAD OF"}, tr.Timing)
	d.event, d.ei = pick([]string{"INSERT", "UPDATE", "DELETE"}, tr.Event)
	d.body = NewEditor(w.caps.Dialect, EditorOpts{TabWidth: 2, CloseBraces: true})
	d.body.SetText(tr.Statement)
	return d
}

// trigger is the dialog's fields as a db.Trigger.
func (d *triggerEditDlg) trigger() db.Trigger {
	return db.Trigger{Name: strings.TrimSpace(d.name.Value()), Timing: d.timing[d.ti], Event: d.event[d.ei], Statement: strings.TrimSpace(d.body.Text())}
}

func (d *triggerEditDlg) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if p, ok := msg.(tea.PasteMsg); ok {
		switch d.focus {
		case 0:
			d.name.Handle(p)
		case 3:
			d.body.Handle(p)
		}
		return d, nil
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}
	switch k.Keystroke() {
	case "esc":
		return nil, nil
	case "tab":
		d.focus = (d.focus + 1) % 4
		return d, nil
	case "shift+tab":
		d.focus = (d.focus + 3) % 4
		return d, nil
	case "ctrl+s":
		tr := d.trigger()
		if tr.Name == "" || tr.Statement == "" {
			return d, d.w.a.say("a trigger needs a name and a body")
		}
		t := newQueryTab(d.w, d.w.newID())
		t.name = tr.Name
		t.ed.SetText(triggerSQL(d.w.caps.Dialect, d.ref, tr))
		return nil, d.w.addTab(t)
	}
	switch d.focus {
	case 0:
		d.name.Handle(k)
	case 1, 2:
		opts, i := d.timing, &d.ti
		if d.focus == 2 {
			opts, i = d.event, &d.ei
		}
		switch k.Keystroke() {
		case "space", "right", "enter":
			*i = (*i + 1) % len(opts)
		case "left":
			*i = (*i + len(opts) - 1) % len(opts)
		}
	default:
		d.body.Handle(k)
	}
	return d, nil
}

func (d *triggerEditDlg) View(th Theme, maxW, maxH int) string {
	w := min(maxW-4, 100)
	row := func(i int, label, val string) string {
		mark := "  "
		if d.focus == i {
			mark = th.Accent.Render("▸ ")
		}
		return mark + th.Dim.Render(fit(label, 8)) + val
	}
	choice := func(i int, opts []string, cur int) string {
		v := opts[cur]
		if d.focus == i {
			return th.Accent.Render("◂ " + v + " ▸")
		}
		return v
	}
	lines := []string{
		row(0, "Name", d.name.View(w-14, th, d.focus == 0)),
		row(1, "Timing", choice(1, d.timing, d.ti)),
		row(2, "Event", choice(2, d.event, d.ei)),
		th.Border.Render(strings.Repeat("─", w-4)),
	}
	d.body.W, d.body.H = w-4, max(4, maxH-14)
	lines = append(lines, strings.Split(d.body.View(th, d.focus == 3), "\n")...)
	return box(th, "Trigger on "+d.ref.Name, lines, "Tab: next field   ←/→: change   C-s: script it into a query tab   Esc: cancel", w)
}
