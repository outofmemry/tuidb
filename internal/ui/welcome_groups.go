// welcome_groups.go: The welcome screen: connection groups and sorting.

package ui

import (
	"fmt"
	"math"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
)

// inGroup: c is in group g or one of its sub-groups.
func inGroup(c config.Connection, g string) bool {
	return c.Group == g || strings.HasPrefix(c.Group, g+"/")
}

// groupMenu is g on the welcome screen: pick a group, then rename or delete it.
func (w *welcome) groupMenu() tea.Cmd {
	gs := config.Groups(w.a.d.Conns)
	if len(gs) == 0 {
		return w.a.say("no groups yet: give a connection a Group in its form")
	}
	var items []pickItem
	for _, g := range gs {
		n := 0
		for _, c := range w.a.d.Conns {
			if inGroup(c, g) {
				n++
			}
		}
		items = append(items, pickItem{Label: g, Detail: plural(n, "connection"), Value: g})
	}
	w.a.push(&pickerDlg{title: "Groups", items: items, filter: true, onPick: func(it pickItem, _ bool) tea.Cmd {
		g := it.Value.(string)
		w.a.push(&pickerDlg{title: "Group " + g, items: []pickItem{{Label: "Rename…", Value: "rename"}, {Label: "Delete the group and its connections…", Value: "delete"}},
			onPick: func(it pickItem, _ bool) tea.Cmd {
				if it.Value == "rename" {
					return w.renameGroup(g)
				}
				return w.deleteGroup(g)
			}})
		return nil
	}})
	return nil
}

func (w *welcome) renameGroup(g string) tea.Cmd {
	w.a.push(newForm("Rename group "+g, []formField{{Key: "name", Label: "New path", Kind: fText, Value: g, Hint: "A/B nests"}}, func(v map[string]string) tea.Cmd {
		to := strings.Trim(strings.TrimSpace(v["name"]), "/")
		if to == "" || to == g {
			return nil
		}
		n := 0
		for i, c := range w.a.d.Conns {
			if c.Source == "" && inGroup(c, g) { // connections from a [[source]] folder belong to their own file
				w.a.d.Conns[i].Group = to + strings.TrimPrefix(c.Group, g)
				n++
			}
		}
		if err := w.a.saveConns(); err != nil {
			return w.a.fail(err)
		}
		return w.a.say("moved %s to %s", plural(n, "connection"), to)
	}))
	return nil
}

func (w *welcome) deleteGroup(g string) tea.Cmd {
	var gone []string
	for _, c := range w.a.d.Conns {
		if c.Source == "" && inGroup(c, g) {
			gone = append(gone, c.Name)
		}
	}
	// open workspaces of these connections are closed too: say what that discards
	openWarn := func() (dirty, applying []string) {
		for _, ws := range w.a.wss {
			if !slices.Contains(gone, ws.conn.Name) {
				continue
			}
			if ws.applying {
				applying = append(applying, ws.conn.Name)
			} else if ws.dirty() {
				dirty = append(dirty, ws.conn.Name)
			}
		}
		return
	}
	fields := []formField{
		{Kind: fLabel, Value: "This removes " + plural(len(gone), "connection") + " from connections.toml (the databases are untouched)."},
		{Kind: fLabel, Value: strings.Join(gone, ", ")},
	}
	if dirty, applying := openWarn(); len(dirty)+len(applying) > 0 {
		fields = append(fields, formField{Kind: fLabel, Value: "⚠ Open and closed with it, discarding unsaved changes: " + strings.Join(append(dirty, applying...), ", ")})
	}
	fields = append(fields, formField{Key: "confirm", Label: "Type the group name", Kind: fText})
	f := newForm("Delete group "+g+"?", fields, func(map[string]string) tea.Cmd {
		var keep []config.Connection
		for _, c := range w.a.d.Conns {
			if c.Source != "" || !inGroup(c, g) {
				keep = append(keep, c)
			}
		}
		old := w.a.d.Conns
		w.a.d.Conns = keep
		if err := w.a.saveConns(); err != nil { // save first: a failed save leaves memory and file alike
			w.a.d.Conns = old
			return w.a.fail(err)
		}
		var open []*Workspace
		for _, ws := range w.a.wss { // workspaces of the deleted connections go too
			if slices.Contains(gone, ws.conn.Name) {
				ws.close()
			} else {
				open = append(open, ws)
			}
		}
		if len(open) != len(w.a.wss) {
			w.a.wss, w.a.active = open, 0
			w.a.inWS = w.a.inWS && len(open) > 0
		}
		w.sel = 0
		w.move(0)
		return w.a.say("deleted group %s and %s", g, plural(len(gone), "connection"))
	})
	f.validate = func(v map[string]string) error {
		if _, applying := openWarn(); len(applying) > 0 {
			return fmt.Errorf("%s is still sending a change: wait for it to finish", strings.Join(applying, ", "))
		}
		if v["confirm"] != g {
			return fmt.Errorf("type “%s” to confirm", g)
		}
		return nil
	}
	f.submit = "Enter: delete"
	w.a.push(f)
	return nil
}

// sortBy is the list order inside each group: the remembered pick, else [general] connection_sort.
func (w *welcome) sortBy() string {
	return orDefault(w.a.d.State.ConnectionSort(), orDefault(w.a.d.Cfg.General.ConnectionSort, "name"))
}

// sortKey orders connections within a group; ties fall back to the name.
func (w *welcome) sortKey(c config.Connection) string {
	switch w.sortBy() {
	case "driver":
		return c.Driver
	case "recent": // newest first; never connected sorts last
		if t, ok := w.a.d.State.LastConnected()[c.Name]; ok {
			return fmt.Sprintf("%020d", math.MaxInt64-t.Unix())
		}
		return strings.Repeat("9", 20)
	case "tag":
		rank := 99 // untagged last; known tags in the order of the tag list
		for i, t := range w.a.tagNames() {
			if t == c.Tag && c.Tag != "" {
				rank = i
			}
		}
		return fmt.Sprintf("%02d", rank)
	}
	return ""
}

func (w *welcome) pickSort() tea.Cmd {
	var items []pickItem
	for _, by := range []string{"name", "tag", "driver", "recent"} {
		mark := "  "
		if by == w.sortBy() {
			mark = "• "
		}
		items = append(items, pickItem{Label: mark + by, Value: by})
	}
	w.a.push(&pickerDlg{title: "Sort connections by", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		if err := w.a.d.State.SetConnectionSort(it.Value.(string)); err != nil {
			return w.a.fail(err)
		}
		w.sel = 0
		w.move(0)
		return nil
	}})
	return nil
}
