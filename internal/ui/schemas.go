// schemas.go: Schema administration from the sidebar: create, administer, drop.

package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

// newSchema is new_schema (and the item menu): CREATE SCHEMA through the safe-mode gate.
func (s *sidebar) newSchema() tea.Cmd {
	if !s.w.caps.Schemas {
		return s.w.a.say("%s has no schemas (a MySQL or ClickHouse schema is a database)", s.w.conn.Driver)
	}
	f := newForm("New schema", []formField{{Key: "name", Label: "Name", Kind: fText}}, func(v map[string]string) tea.Cmd {
		name := strings.TrimSpace(v["name"])
		return s.schemaAdmin(db.OpCreateSchema, name, nil, "created")
	})
	f.validate = func(v map[string]string) error {
		if strings.TrimSpace(v["name"]) == "" {
			return fmt.Errorf("a schema needs a name")
		}
		return nil
	}
	s.w.a.push(f)
	return nil
}

func (s *sidebar) schemaAdmin(op db.AdminOp, name string, opts map[string]string, done string) tea.Cmd {
	stmts, err := s.w.sess.PlanAdmin(op, db.ObjRef{Schema: name, Name: name}, opts)
	if err != nil {
		return s.w.a.fail(err)
	}
	return s.w.execute(stmts, func(err error) tea.Cmd {
		if err != nil {
			return nil
		}
		if op == db.OpDropSchema && name == s.schema { // the open schema is gone: fall back to public, else the first other one
			next := "public"
			if !slices.Contains(s.schemas, next) || next == name {
				for _, sc := range s.schemas {
					if sc != name {
						next = sc
						break
					}
				}
			}
			return tea.Batch(s.w.useSchema(next), s.w.a.say("schema %s %s", name, done))
		}
		s.sel, s.top = 0, 0
		return tea.Batch(s.load(), s.w.a.say("schema %s %s", name, done))
	})
}

// dropSchema picks a schema, then asks for its name before dropping it.
func (s *sidebar) dropSchema() tea.Cmd {
	if !s.w.caps.Schemas {
		return s.w.a.say("%s has no schemas", s.w.conn.Driver)
	}
	var items []pickItem
	for _, sc := range s.schemas {
		items = append(items, pickItem{Label: sc, Value: sc})
	}
	s.w.a.push(&pickerDlg{title: "Drop which schema?", filter: true, items: items, empty: "no schemas", onPick: func(it pickItem, _ bool) tea.Cmd {
		name := it.Value.(string)
		f := newForm("Drop schema "+name+"?", []formField{
			{Key: "cascade", Label: "Cascade (drops what is in it)", Kind: fBool, Value: "false"},
			{Kind: fLabel, Value: "This cannot be undone. Type the name to confirm."},
			{Key: "confirm", Label: "Type its name", Kind: fText},
		}, func(v map[string]string) tea.Cmd {
			return s.schemaAdmin(db.OpDropSchema, name, map[string]string{"cascade": v["cascade"]}, "dropped")
		})
		f.validate = func(v map[string]string) error {
			if v["confirm"] != name {
				return fmt.Errorf("type “%s” to confirm", name)
			}
			return nil
		}
		f.idx = 2
		f.submit = "Enter: drop"
		s.w.a.push(f)
		return nil
	}})
	return nil
}
