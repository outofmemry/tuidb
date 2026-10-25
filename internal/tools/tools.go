// Package tools holds the pure data tools behind the Tools menu: schema diff, data diff, bulk update and
// fake data. They work on db types only; the UIs run the statements they return through Apply.
package tools

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"webtuidb/internal/db"
)

func tableKey(t db.TableDef) string { return t.Schema + "\x00" + t.Name }

// SchemaDiff returns the statements that turn from into to: tables only in to are created through
// plan(nil, t), tables in both go through plan(old, new), tables only in from are dropped. Statements
// come out creates, then alters, then drops. Creates follow foreign-key dependencies (a referenced table
// first), drops run the other way (a referencing table first); ties and cycles fall back to name order.
// quote spells the identifiers of the DROP TABLE text (default: ANSI double quotes; pass
// db.Dialect.Quote for MySQL or ClickHouse).
func SchemaDiff(from, to []db.TableDef, plan func(old, new *db.TableDef) ([]db.Statement, error), quote ...func(string) string) ([]db.Statement, error) {
	keep, drops, err := SchemaDiffParts(from, to, plan, quote...)
	return append(keep, drops...), err
}

// SchemaDiffParts is SchemaDiff with the DROP TABLE statements kept apart, so a caller can make them opt-in.
func SchemaDiffParts(from, to []db.TableDef, plan func(old, new *db.TableDef) ([]db.Statement, error), quote ...func(string) string) (keep, drops []db.Statement, err error) {
	q := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	if len(quote) > 0 {
		q = quote[0]
	}
	old := map[string]*db.TableDef{}
	for i := range from {
		old[tableKey(from[i])] = &from[i]
	}
	var creates, alters []db.Statement
	seen := map[string]bool{}
	var added []db.TableDef
	for _, t := range to {
		seen[tableKey(t)] = true
		if old[tableKey(t)] == nil {
			added = append(added, t)
		}
	}
	sorted := append([]db.TableDef(nil), to...)
	sort.Slice(sorted, func(i, j int) bool { return tableKey(sorted[i]) < tableKey(sorted[j]) })
	createOrder := map[string]int{}
	for i, t := range depOrder(added) {
		createOrder[tableKey(t)] = i
	}
	var newTables []*db.TableDef
	for i := range sorted {
		t := &sorted[i]
		o := old[tableKey(*t)]
		if o == nil {
			newTables = append(newTables, t)
			continue
		}
		sts, err := plan(o, t)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", t.Name, err)
		}
		alters = append(alters, sts...)
	}
	sort.SliceStable(newTables, func(i, j int) bool {
		return createOrder[tableKey(*newTables[i])] < createOrder[tableKey(*newTables[j])]
	})
	for _, t := range newTables {
		sts, err := plan(nil, t)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", t.Name, err)
		}
		creates = append(creates, sts...)
	}
	var gone []db.TableDef
	for _, t := range from {
		if !seen[tableKey(t)] {
			gone = append(gone, t)
		}
	}
	order := depOrder(gone)
	for i := len(order) - 1; i >= 0; i-- { // children before the tables they reference
		t := order[i]
		name := q(t.Name)
		if t.Schema != "" {
			name = q(t.Schema) + "." + name
		}
		drops = append(drops, db.Statement{Text: "DROP TABLE " + name})
	}
	return append(creates, alters...), drops, nil
}

// depOrder sorts tables so that each comes after the tables it references (within the set). Tables
// that do not depend on each other, and members of a cycle, stay in name order.
func depOrder(ts []db.TableDef) []db.TableDef {
	sorted := append([]db.TableDef(nil), ts...)
	sort.Slice(sorted, func(i, j int) bool { return tableKey(sorted[i]) < tableKey(sorted[j]) })
	byKey := map[string]int{}
	for i, t := range sorted {
		byKey[tableKey(t)] = i
	}
	deps := make([][]int, len(sorted))
	for i, t := range sorted {
		for _, fk := range t.ForeignKeys {
			schema := fk.RefSchema
			if schema == "" {
				schema = t.Schema
			}
			if j, ok := byKey[schema+"\x00"+fk.RefTable]; ok && j != i {
				deps[i] = append(deps[i], j)
			}
		}
	}
	var out []db.TableDef
	state := make([]int, len(sorted)) // 0 new, 1 on the stack (a cycle: skip the edge), 2 done
	var visit func(int)
	visit = func(i int) {
		if state[i] != 0 {
			return
		}
		state[i] = 1
		for _, d := range deps[i] {
			visit(d)
		}
		state[i] = 2
		out = append(out, sorted[i])
	}
	for i := range sorted {
		visit(i)
	}
	return out
}

// DataDiff compares two result sets with the same columns and returns the changes that turn from into to:
// inserts, updates (Set holds only the changed columns) and deletes, keyed by the pk columns (every column
// when pk is empty or names an unknown column). The ObjRef is left for the caller.
func DataDiff(from, to *db.Result, pk []string) []db.Change {
	out, _ := dataDiff(from, to, pk)
	return out
}

// DataDiffChecked is DataDiff that fails when two rows of a side share a key (identical rows when keyless),
// because they would collapse into one and their changes would be missed.
func DataDiffChecked(from, to *db.Result, pk []string) ([]db.Change, error) {
	out, dup := dataDiff(from, to, pk)
	if dup {
		return nil, errors.New("duplicate rows share a key, so they cannot be compared; choose a unique key")
	}
	return out, nil
}

func dataDiff(from, to *db.Result, pk []string) (_ []db.Change, dup bool) {
	idx := func(r *db.Result) map[string]int {
		m := map[string]int{}
		for i, c := range r.Columns {
			m[c.Name] = i
		}
		return m
	}
	fi, ti := idx(from), idx(to)
	keyCols := pk
	for _, c := range pk {
		if _, ok := ti[c]; !ok {
			keyCols = nil
		}
		if _, ok := fi[c]; !ok {
			keyCols = nil
		}
	}
	if len(keyCols) == 0 {
		keyCols = nil
		for _, c := range to.Columns {
			keyCols = append(keyCols, c.Name)
		}
	}
	str := func(v any) string {
		if v == nil {
			return "\x00nil"
		}
		if b, ok := v.([]byte); ok {
			return string(b)
		}
		return fmt.Sprint(v)
	}
	keyOf := func(row []any, ix map[string]int) string {
		var parts []string
		for _, c := range keyCols {
			if i, ok := ix[c]; ok && i < len(row) {
				parts = append(parts, str(row[i]))
			}
		}
		return strings.Join(parts, "\x01")
	}
	keyVals := func(row []any, ix map[string]int) []db.KeyVal {
		var kv []db.KeyVal
		for _, c := range keyCols {
			if i, ok := ix[c]; ok && i < len(row) {
				kv = append(kv, db.KeyVal{Col: c, Val: row[i]})
			}
		}
		return kv
	}
	old := map[string][]any{}
	for _, r := range from.Rows {
		k := keyOf(r, fi)
		if _, ok := old[k]; ok {
			dup = true
		}
		old[k] = r
	}
	var out []db.Change
	seen := map[string]bool{}
	for _, r := range to.Rows {
		k := keyOf(r, ti)
		dup = dup || seen[k]
		seen[k] = true
		o, ok := old[k]
		if !ok {
			c := db.Change{Kind: db.Insert}
			for i, col := range to.Columns {
				c.Set = append(c.Set, db.KeyVal{Col: col.Name, Val: r[i]})
			}
			out = append(out, c)
			continue
		}
		var set []db.KeyVal
		for i, col := range to.Columns {
			if j, ok := fi[col.Name]; ok && str(o[j]) != str(r[i]) {
				set = append(set, db.KeyVal{Col: col.Name, Val: r[i]})
			}
		}
		if len(set) > 0 {
			out = append(out, db.Change{Kind: db.Update, Key: keyVals(o, fi), Set: set})
		}
	}
	for _, r := range from.Rows {
		if !seen[keyOf(r, fi)] {
			out = append(out, db.Change{Kind: db.Delete, Key: keyVals(r, fi)})
		}
	}
	return out, dup
}

// BulkUpdate renders UPDATE ref SET ... WHERE <filters> for a SQL dialect name (postgres, mysql, ...).
func BulkUpdate(ref db.ObjRef, filters db.FilterSet, set []db.KeyVal, dialect string) (db.Statement, error) {
	d, ok := db.DialectFor(dialect)
	if !ok {
		return db.Statement{}, fmt.Errorf("bulk update: %q is not a SQL dialect", dialect)
	}
	return db.BulkUpdate(d, ref, filters, set, nil)
}

var words = strings.Fields("alpha bravo cedar delta ember fjord grove harbor iris juniper kilo lumen maple nova onyx pearl quartz river sable tundra")

// intMax is the largest value of the integer type x (unsigned halves are not worth the guess).
func intMax(x string) int64 {
	switch {
	case strings.Contains(x, "tinyint"):
		return 127
	case strings.Contains(x, "smallint"):
		return 1<<15 - 1
	case strings.Contains(x, "mediumint"):
		return 1<<23 - 1
	}
	return 1<<31 - 1
}

func pkBase(hi int64, n int, seed int64) int64 {
	if room := hi - int64(n) - 1; room > 1_000_000 {
		return 1_000_000 + int64(uint64(seed)%uint64(room-1_000_000+1))
	}
	return 0
}

// FakeRows returns n Insert changes (ObjRef left to the caller) with values that fit each column's type.
// Primary-key columns that have a default or are identity / auto-increment / serial are skipped so the
// server numbers them. The same seed gives the same values (timestamps are relative to now).
func FakeRows(cols []db.Column, n int, seed int64) []db.Change {
	rng := rand.New(rand.NewPCG(uint64(seed), 0x57ebd))
	now := time.Now().Truncate(time.Second)
	var out []db.Change
	for i := 0; i < n; i++ {
		c := db.Change{Kind: db.Insert}
		for _, col := range cols {
			x := strings.ToLower(col.Type)
			extra := strings.ToLower(col.Extra)
			serial := strings.Contains(x, "serial") || strings.Contains(extra, "auto_increment") || strings.Contains(extra, "identity")
			if col.PK && (col.HasDef || serial) {
				continue
			}
			var v any
			switch {
			case serial:
				v = int64(i + 1)
			case strings.Contains(x, "interval"):
				v = fmt.Sprintf("%d days", rng.IntN(30)+1)
			case strings.Contains(x, "int") && !strings.Contains(x, "point"):
				hi := intMax(x)
				if col.PK { // a seeded offset keeps clear of the 1..N ids a table usually already has; tiny types wrap and the insert fails loudly
					v = (pkBase(hi, n, seed) + int64(i) + 1) % (hi + 1)
				} else {
					v = int64(rng.IntN(int(min(1000, hi))))
				}
			case strings.Contains(x, "bool") || x == "bit":
				v = rng.IntN(2) == 0
			case strings.Contains(x, "float") || strings.Contains(x, "double") || strings.Contains(x, "real") ||
				strings.Contains(x, "numeric") || strings.Contains(x, "decimal") || strings.Contains(x, "money"):
				v = float64(rng.IntN(100000)) / 100
			case strings.Contains(x, "json"):
				v = "{}"
			case strings.Contains(x, "timestamp") || strings.Contains(x, "datetime") || strings.HasPrefix(x, "date") || strings.HasPrefix(x, "time"):
				v = now.Add(-time.Duration(rng.IntN(30*24*3600)) * time.Second)
			case strings.Contains(x, "uuid"):
				v = fmt.Sprintf("%08x-%04x-4%03x-a%03x-%012x", rng.Uint32(), rng.Uint32()&0xffff, rng.Uint32()&0xfff, rng.Uint32()&0xfff, rng.Uint64()&0xffffffffffff)
			default:
				v = fmt.Sprintf("%s %d", words[rng.IntN(len(words))], rng.IntN(1000))
			}
			c.Set = append(c.Set, db.KeyVal{Col: col.Name, Val: v})
		}
		out = append(out, c)
	}
	return out
}
