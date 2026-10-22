package ui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tuidb/internal/db"
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
			drop = "DROP " + strings.ToUpper(string(ref.Kind)) + " IF EXISTS " + name + ";\n"
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
	return fmt.Sprintf("DROP TRIGGER IF EXISTS %s;\n%s FOR EACH ROW\n  %s;\n", d.Quote(t.Name), head, strings.TrimSuffix(t.Statement, ";"))
}

func newTriggerSQL(dialect string, ref db.ObjRef) string {
	d, ok := db.DialectFor(dialect)
	if !ok {
		return ""
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
		w.a.push(&pickerDlg{title: "Triggers on " + m.ref.Name, filter: true, items: items, hint: "Enter: edit as SQL   Esc: cancel",
			onPick: func(it pickItem, _ bool) tea.Cmd {
				text, name := newTriggerSQL(w.caps.Dialect, m.ref), "new trigger"
				if i := it.Value.(int); i >= 0 {
					text, name = triggerSQL(w.caps.Dialect, m.ref, m.list[i]), m.list[i].Name
				}
				t := newQueryTab(w, w.newID())
				t.name = name
				t.ed.SetText(text)
				return w.addTab(t)
			}})
		return nil, true
	}
	return nil, false
}
