// search.go: Searching a database for a value, and opening the table filtered on a hit.

package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

func (w *Workspace) searchInDatabase() tea.Cmd {
	f := newForm("Search in database", []formField{
		{Key: "value", Label: "Value", Kind: fText},
		{Key: "op", Label: "Operator", Kind: fChoice, Options: []string{"Contains", "Not contains", "=", "Has prefix", "Has suffix"}, Value: "Contains"},
		{Key: "tables", Label: "Tables", Kind: fText, Hint: "empty = all tables, or a, b, c"},
		{Key: "all", Label: "All schemas", Kind: fBool, Show: func(map[string]string) bool { return w.caps.Schemas }},
	}, func(v map[string]string) tea.Cmd {
		if w.jobCancel != nil {
			return w.a.say("another export, import or search is still running (cancel it first)")
		}
		o := db.SearchOptions{Value: v["value"], Op: v["op"], Schema: w.sb.schema, AllSchemas: v["all"] == "true" && w.caps.Schemas}
		for _, n := range strings.Split(v["tables"], ",") {
			if n = strings.TrimSpace(n); n != "" {
				o.Tables = append(o.Tables, n)
			}
		}
		total := len(o.Tables)
		if total == 0 && !o.AllSchemas { // the sidebar lists the current schema's objects
			for _, ob := range w.sb.objs {
				if ob.Kind == db.KindTable {
					total++
				}
			}
		}
		var hits []db.SearchHit
		done := 0
		p := &pickerDlg{empty: "no hits yet", hint: "Enter: open the table with the filter   Esc: stop",
			onClose: func() {
				if w.jobCancel != nil {
					w.jobCancel()
				}
			}}
		title := func(state string) {
			of := fmt.Sprintf("/%d", total)
			if o.AllSchemas && total == 0 {
				of = "" // the table count of every schema is not known up front
			}
			p.title = fmt.Sprintf("Search %q: %d%s tables · %d with hits%s", o.Value, done, of, len(hits), state)
		}
		title("")
		p.onPick = func(it pickItem, _ bool) tea.Cmd {
			if w.jobCancel != nil {
				w.jobCancel()
			}
			h := it.Value.(db.SearchHit)
			return w.openFiltered(h.Ref, h.Filters)
		}
		w.a.push(p)
		return w.job("search "+o.Value, func(ctx context.Context, sess db.Session, report func(func() tea.Cmd)) error {
			return db.Search(ctx, sess, o, func(h db.SearchHit) {
				report(func() tea.Cmd {
					done++
					if h.Count > 0 || h.Err != nil {
						hits = append(hits, h)
						detail := fmt.Sprintf("%d rows", h.Count)
						if h.Err != nil {
							detail = "error: " + h.Err.Error()
						}
						label, at := h.Ref.Name, len(p.items)
						if o.AllSchemas || h.Ref.Schema != "" && h.Ref.Schema != w.sess.CurrentSchema() { // T-P2-09: say which schema opens
							label = h.Ref.Schema + "." + h.Ref.Name
						}
						if o.AllSchemas { // grouped by schema: a hit goes after the last one of its schema or an earlier schema
							at = sort.Search(len(p.items), func(i int) bool { return p.items[i].Value.(db.SearchHit).Ref.Schema > h.Ref.Schema })
							if len(p.items) > 0 && at <= p.sel {
								p.sel++
							}
						}
						p.items = append(p.items, pickItem{})
						copy(p.items[at+1:], p.items[at:])
						p.items[at] = pickItem{Label: label, Detail: detail, Value: h}
					}
					title("")
					return nil
				})
			})
		}, func() (string, tea.Cmd) {
			title(" · done")
			return fmt.Sprintf("search finished: %d table(s) with hits", len(hits)), nil
		})
	})
	w.a.push(f)
	return nil
}

// openFiltered opens a table with filters already applied in its filter bar.
func (w *Workspace) openFiltered(ref db.ObjRef, f db.FilterSet) tea.Cmd {
	w.openObject(ref, false) // its first load is dropped: applyFilters loads with the filters in place
	t, ok := w.tab().(*tableTab)
	if !ok || t.ref != ref {
		return nil
	}
	t.filters, t.showFilters = f, true
	return t.applyFilters()
}

// searchField moves the grid cursor (and so the detail pane) to the best-matching column; [sidebar] matching
// picks fuzzy or contains, like the sidebar search.
func (t *tableTab) searchField() tea.Cmd {
	t.ws.a.push(newForm("Search for field", []formField{{Key: "q", Label: "Field", Kind: fText}}, func(v map[string]string) tea.Cmd {
		q := strings.TrimSpace(v["q"])
		best, score := -1, -1
		for i, c := range t.grid.Cols {
			if s, ok := matchName(q, c.Name, t.ws.a.d.Cfg.Sidebar.Matching != "contains"); ok && s > score {
				best, score = i, s
			}
		}
		if best < 0 {
			return t.ws.a.say("no field matches %q", q)
		}
		t.grid.GoCol(best, false)
		return nil
	}))
	return nil
}
