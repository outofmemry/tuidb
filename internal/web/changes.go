package web

import (
	"encoding/json"
	"fmt"
	"strings"

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
			// raw SQL is inlined by the planner, so only the expressions the TUI offers are accepted
			switch strings.ToLower(strings.TrimSpace(e)) {
			case "now()", "current_timestamp", "gen_random_uuid()":
				v.V = db.Expr(strings.TrimSpace(e))
				return nil
			}
			return fmt.Errorf("expression %q is not allowed (NOW(), CURRENT_TIMESTAMP, gen_random_uuid())", e)
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
		Row *int              `json:"row"`
		Key map[string]string `json:"key,omitempty"` // optional PK values the client saw; verified against the snapshot
		Set map[string]Val    `json:"set"`
	} `json:"updates"`
	Deletes    []int               `json:"deletes"`
	DeleteKeys []map[string]string `json:"deleteKeys,omitempty"` // optional, parallel to Deletes
	Inserts    []map[string]Val    `json:"inserts"`
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
	if len(req.Updates) > 0 || len(req.Deletes) > 0 { // inserts need no key, so a hidden PK only blocks update/delete
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
		if len(keyCols) == 0 { // a key-less WHERE could hit many rows; the TUI refuses too
			return nil, bad("this table has no primary key; rows cannot be updated or deleted safely")
		}
	}
	key := func(row int, want map[string]string) ([]db.KeyVal, error) {
		if row < 0 || row >= len(c.res.Rows) {
			return nil, bad("row %d out of range", row)
		}
		kv := make([]db.KeyVal, len(keyCols))
		for j, ci := range keyCols {
			kv[j] = db.KeyVal{Col: c.res.Columns[ci].Name, Val: c.res.Rows[row][ci]}
			if want != nil && want[kv[j].Col] != db.FormatValue(kv[j].Val) {
				return nil, bad("row %d no longer matches what was edited (%s changed); reload the table", row, kv[j].Col)
			}
		}
		return kv, nil
	}
	if req.DeleteKeys != nil && len(req.DeleteKeys) != len(req.Deletes) {
		return nil, bad("deleteKeys must match deletes")
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
	for i, row := range req.Deletes {
		if seen[row] {
			continue
		}
		seen[row] = true
		var want map[string]string
		if req.DeleteKeys != nil {
			want = req.DeleteKeys[i]
		}
		k, err := key(row, want)
		if err != nil {
			return nil, err
		}
		out = append(out, db.Change{Kind: db.Delete, ObjRef: c.ref, Key: k})
	}
	for _, u := range req.Updates {
		if u.Row == nil {
			return nil, bad("update without a row")
		}
		if seen[*u.Row] {
			return nil, bad("row %d is both deleted and updated", *u.Row)
		}
		k, err := key(*u.Row, u.Key)
		if err != nil {
			return nil, err
		}
		s, err := set(u.Set)
		if err != nil {
			return nil, err
		}
		if len(s) == 0 {
			return nil, bad("row %d: update without changed columns", *u.Row)
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
