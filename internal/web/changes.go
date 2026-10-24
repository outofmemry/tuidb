package web

import (
	"encoding/json"
	"fmt"

	"tuidb/internal/db"
)

// Val is an edited cell: a JSON string, null, {"$":"null"|"default"|"empty"} or {"$expr":"NOW()"}.
type Val struct{ V any }

func (v *Val) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		return fmt.Errorf("empty value")
	}
	switch b[0] {
	case 'n':
		v.V = db.Null
	case '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		v.V = s
	case '{':
		var m map[string]string
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("bad special value: %w", err)
		}
		if e, ok := m["$expr"]; ok {
			v.V = db.Expr(e)
			return nil
		}
		switch m["$"] {
		case "null":
			v.V = db.Null
		case "default":
			v.V = db.Default
		case "empty":
			v.V = db.Empty
		default:
			return fmt.Errorf(`special value must be {"$":"null|default|empty"} or {"$expr":"..."}`)
		}
	default: // numbers and booleans travel as their text, like the TUI's edit buffer
		v.V = string(b)
	}
	return nil
}

type editReq struct {
	ResultID string `json:"resultId"`
	Updates  []struct {
		Row int            `json:"row"`
		Set map[string]Val `json:"set"`
	} `json:"updates"`
	Deletes []int            `json:"deletes"`
	Inserts []map[string]Val `json:"inserts"`
}

// buildChanges is the server-side twin of ui.Grid.Changes: keys come from the cached original
// values, deletes run first and inserts last so a freed key can be reused.
func buildChanges(c *cached, req editReq) ([]db.Change, error) {
	if c.ref.Name == "" {
		return nil, bad("this result is read-only (it is not a plain table view)")
	}
	idx := map[string]int{}
	for i, col := range c.res.Columns {
		idx[col.Name] = i
	}
	var keyCols []int
	for _, col := range c.cols {
		if !col.PK {
			continue
		}
		i, ok := idx[col.Name]
		if !ok {
			return nil, bad("primary key column %q is hidden; show it before editing", col.Name)
		}
		keyCols = append(keyCols, i)
	}
	if len(keyCols) == 0 { // no PK: every original value identifies the row
		for i := range c.res.Columns {
			keyCols = append(keyCols, i)
		}
	}
	key := func(row int) ([]db.KeyVal, error) {
		if row < 0 || row >= len(c.res.Rows) {
			return nil, bad("row %d out of range", row)
		}
		kv := make([]db.KeyVal, len(keyCols))
		for j, ci := range keyCols {
			kv[j] = db.KeyVal{Col: c.res.Columns[ci].Name, Val: c.res.Rows[row][ci]}
		}
		return kv, nil
	}
	set := func(m map[string]Val) ([]db.KeyVal, error) {
		for n := range m {
			if _, ok := idx[n]; !ok {
				return nil, bad("unknown column %q", n)
			}
		}
		var out []db.KeyVal
		for _, col := range c.res.Columns { // result order keeps the SQL deterministic
			if v, ok := m[col.Name]; ok {
				out = append(out, db.KeyVal{Col: col.Name, Val: v.V})
			}
		}
		return out, nil
	}
	var out []db.Change
	seen := map[int]bool{}
	for _, row := range req.Deletes {
		if seen[row] {
			continue
		}
		seen[row] = true
		k, err := key(row)
		if err != nil {
			return nil, err
		}
		out = append(out, db.Change{Kind: db.Delete, ObjRef: c.ref, Key: k})
	}
	for _, u := range req.Updates {
		if seen[u.Row] {
			return nil, bad("row %d is both deleted and updated", u.Row)
		}
		k, err := key(u.Row)
		if err != nil {
			return nil, err
		}
		s, err := set(u.Set)
		if err != nil {
			return nil, err
		}
		if len(s) == 0 {
			return nil, bad("row %d: update without changed columns", u.Row)
		}
		out = append(out, db.Change{Kind: db.Update, ObjRef: c.ref, Key: k, Set: s})
	}
	for _, ins := range req.Inserts {
		s, err := set(ins)
		if err != nil {
			return nil, err
		}
		out = append(out, db.Change{Kind: db.Insert, ObjRef: c.ref, Set: s})
	}
	if len(out) == 0 {
		return nil, bad("no changes to apply")
	}
	return out, nil
}
