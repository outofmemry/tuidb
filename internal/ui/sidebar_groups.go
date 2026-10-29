// sidebar_groups.go: Sidebar groups: add an item to a group, remove it, edit groups.

package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
	"webtuidb/internal/store"
)

// groupsOf names the custom groups holding the object key.
func (s *sidebar) groupsOf(key string) []string {
	var out []string
	for _, g := range s.w.a.d.State.Groups(s.w.conn.Name) {
		if contains(g.Items, key) {
			out = append(out, g.Name)
		}
	}
	return out
}

// editGroups changes the connection's groups and forgets the ones left empty.
func (s *sidebar) editGroups(f func([]store.ItemGroup) []store.ItemGroup) error {
	var keep []store.ItemGroup
	for _, g := range f(s.w.a.d.State.Groups(s.w.conn.Name)) {
		if len(g.Items) > 0 {
			keep = append(keep, g)
		}
	}
	return s.w.a.d.State.SetGroups(s.w.conn.Name, keep)
}

func (s *sidebar) addToGroup(ref db.ObjRef) tea.Cmd {
	key := objKey(ref)
	add := func(name string) tea.Cmd {
		err := s.editGroups(func(gs []store.ItemGroup) []store.ItemGroup {
			for i := range gs {
				if gs[i].Name == name {
					if !contains(gs[i].Items, key) {
						gs[i].Items = append(gs[i].Items, key)
					}
					return gs
				}
			}
			return append(gs, store.ItemGroup{Name: name, Items: []string{key}})
		})
		if err != nil {
			return s.w.a.fail(err)
		}
		return s.w.a.say("%s is in group %s", ref.Name, name)
	}
	items := []pickItem{{Label: "＋ New group…", Value: ""}}
	for _, g := range s.w.a.d.State.Groups(s.w.conn.Name) {
		items = append(items, pickItem{Label: g.Name, Detail: plural(len(g.Items), "item"), Value: g.Name})
	}
	s.w.a.push(&pickerDlg{title: "Add " + ref.Name + " to a group", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		if name := it.Value.(string); name != "" {
			return add(name)
		}
		s.w.a.push(newForm("New group", []formField{{Key: "name", Label: "Name", Kind: fText}}, func(v map[string]string) tea.Cmd {
			if n := strings.TrimSpace(v["name"]); n != "" {
				return add(n)
			}
			return nil
		}))
		return nil
	}})
	return nil
}

func (s *sidebar) removeFromGroup(ref db.ObjRef, group string) tea.Cmd {
	key := objKey(ref)
	err := s.editGroups(func(gs []store.ItemGroup) []store.ItemGroup {
		for i := range gs {
			if gs[i].Name != group {
				continue
			}
			var keep []string
			for _, it := range gs[i].Items {
				if it != key {
					keep = append(keep, it)
				}
			}
			gs[i].Items = keep
		}
		return gs
	})
	if err != nil {
		return s.w.a.fail(err)
	}
	return s.w.a.say("%s left group %s", ref.Name, group)
}
