// Package diagram builds the ER diagram: tables, FK clusters and a text rendering.
package diagram

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"

	"webtuidb/internal/db"
)

type Table struct {
	Ref     db.ObjRef
	Columns []db.Column
	FKs     []db.ForeignKey
}

func key(schema, name string) string { return schema + "." + name }

// Load fetches every table in schema with its columns and foreign keys.
func Load(ctx context.Context, sess db.Session, schema string) ([]Table, error) {
	objs, err := sess.Objects(ctx, schema)
	if err != nil {
		return nil, err
	}
	var out []Table
	for _, o := range objs {
		if o.Kind != db.KindTable {
			continue
		}
		t := Table{Ref: o.ObjRef}
		if t.Columns, err = sess.Columns(ctx, o.ObjRef); err != nil {
			return nil, err
		}
		if sess.Caps().ForeignKeys {
			if t.FKs, err = sess.ForeignKeys(ctx, o.ObjRef); err != nil {
				return nil, err
			}
		}
		out = append(out, t)
	}
	return out, nil
}

// Cluster numbers tables by FK connectivity (union-find); ids are 0.. in table order.
func Cluster(tables []Table) map[string]int {
	parent := map[string]string{}
	var find func(string) string
	find = func(a string) string {
		if p, ok := parent[a]; ok && p != a {
			parent[a] = find(p)
			return parent[a]
		}
		parent[a] = a
		return a
	}
	for _, t := range tables {
		k := key(t.Ref.Schema, t.Ref.Name)
		find(k)
		for _, fk := range t.FKs {
			parent[find(k)] = find(key(fk.RefSchema, fk.RefTable))
		}
	}
	ids, out := map[string]int{}, map[string]int{}
	for _, t := range tables {
		k := key(t.Ref.Schema, t.Ref.Name)
		r := find(k)
		if _, ok := ids[r]; !ok {
			ids[r] = len(ids)
		}
		out[k] = ids[r]
	}
	return out
}

func box(t Table) []string {
	fkCols := map[string]bool{}
	var refs []string
	for _, fk := range t.FKs {
		for _, c := range fk.Columns {
			fkCols[c] = true
		}
		refs = append(refs, "-> "+fk.RefTable+" ("+strings.Join(fk.Columns, ",")+")")
	}
	body := []string{t.Ref.Name}
	for _, c := range t.Columns {
		m := "  "
		if c.PK {
			m = "pk"
		} else if fkCols[c.Name] || c.FK != nil {
			m = "fk"
		}
		body = append(body, m+" "+c.Name+" "+c.Type)
	}
	body = append(body, refs...)
	for i := range body {
		body[i] = strings.Map(func(r rune) rune { // ESC/OSC/newline in a name must not reach the terminal
			if unicode.IsControl(r) {
				return '?'
			}
			return r
		}, body[i])
	}
	w := 0
	for _, l := range body {
		w = max(w, runewidth.StringWidth(l))
	}
	pad := func(l string) string { return "| " + l + strings.Repeat(" ", w-runewidth.StringWidth(l)) + " |" }
	bar := "+" + strings.Repeat("-", w+2) + "+"
	out := []string{bar, pad(body[0]), bar}
	for _, l := range body[1:] {
		out = append(out, pad(l))
	}
	return append(out, bar)
}

// Render lays the tables out as text boxes, left to right, wrapping at width columns.
// sort is "name" or "columns" (count desc); filter keeps matching tables plus direct FK neighbours.
func Render(tables []Table, width int, sortBy, filter string) string {
	keep := tables
	if f := strings.ToLower(filter); f != "" {
		hit := map[string]bool{}
		for _, t := range tables {
			if strings.Contains(strings.ToLower(t.Ref.Name), f) {
				hit[key(t.Ref.Schema, t.Ref.Name)] = true
			}
		}
		near := map[string]bool{}
		for k := range hit {
			near[k] = true
		}
		for _, t := range tables {
			k := key(t.Ref.Schema, t.Ref.Name)
			for _, fk := range t.FKs {
				r := key(fk.RefSchema, fk.RefTable)
				if hit[k] {
					near[r] = true
				}
				if hit[r] {
					near[k] = true
				}
			}
		}
		keep = nil
		for _, t := range tables {
			if near[key(t.Ref.Schema, t.Ref.Name)] {
				keep = append(keep, t)
			}
		}
	}
	keep = append([]Table(nil), keep...)
	sort.SliceStable(keep, func(i, j int) bool {
		if sortBy == "columns" && len(keep[i].Columns) != len(keep[j].Columns) {
			return len(keep[i].Columns) > len(keep[j].Columns)
		}
		return keep[i].Ref.Name < keep[j].Ref.Name
	})
	var out []string
	var row [][]string
	used := 0
	flush := func() {
		h := 0
		for _, b := range row {
			h = max(h, len(b))
		}
		for i := 0; i < h; i++ {
			var l strings.Builder
			for _, b := range row {
				if i < len(b) {
					l.WriteString(b[i])
				} else {
					l.WriteString(strings.Repeat(" ", runewidth.StringWidth(b[0])))
				}
				l.WriteString("  ")
			}
			out = append(out, strings.TrimRight(l.String(), " "))
		}
		out = append(out, "")
		row, used = nil, 0
	}
	for _, t := range keep {
		b := box(t)
		w := runewidth.StringWidth(b[0]) + 2
		if len(row) > 0 && used+w > width {
			flush()
		}
		row, used = append(row, b), used+w // shortcut: a box wider than width overflows; the UI scrolls sideways
	}
	if len(row) > 0 {
		flush()
	}
	return strings.Join(out, "\n")
}
