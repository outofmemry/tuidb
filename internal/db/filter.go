package db

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	AnyColumn = ""        // Filter.Column: search every column
	RawColumn = "\x00raw" // Filter.Column: Value is a raw SQL condition
)

type Filter struct {
	Column  string
	Op      string
	Value   string
	Enabled bool
}

// FilterSet is the TablePlus filter bar: checked filters joined by AND (or OR).
type FilterSet struct {
	Filters []Filter
	Or      bool
}

func (fs FilterSet) Active() []Filter {
	var out []Filter
	for _, f := range fs.Filters {
		if f.Enabled {
			out = append(out, f)
		}
	}
	return out
}

// Operators offered in the filter bar for an engine.
func Operators(dialect string) []string {
	if dialect == "partiql" { // DynamoDB: no LIKE, and no suffix test
		return []string{"=", "<>", "<", ">", "<=", ">=", "IN", "NOT IN", "IS NULL", "IS NOT NULL", "BETWEEN", "NOT BETWEEN", "Contains", "Not contains", "Has prefix"}
	}
	if dialect == "cql" { // CQL has no <>, NOT IN, IS NULL, BETWEEN or substring matching (LIKE needs a SASI index)
		return []string{"=", "<", ">", "<=", ">=", "IN", "LIKE"}
	}
	ops := []string{"=", "<>", "<", ">", "<=", ">=", "IN", "NOT IN", "IS NULL", "IS NOT NULL", "BETWEEN", "NOT BETWEEN", "LIKE"}
	if dialect == "postgres" {
		ops = append(ops, "ILIKE")
	}
	return append(ops, "Contains", "Not contains", "Has prefix", "Has suffix")
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// splitList splits an IN list on commas outside quotes; a doubled quote inside quotes is one literal quote.
func splitList(v string) []string {
	var out []string
	var cur strings.Builder
	var q byte
	quoted := false
	flush := func() {
		s := cur.String()
		if !quoted {
			s = strings.TrimSpace(s)
		}
		out = append(out, s)
		cur.Reset()
		quoted = false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case q != 0 && c == q && i+1 < len(v) && v[i+1] == q:
			cur.WriteByte(q)
			i++
		case q != 0 && c == q:
			q = 0
		case q != 0:
			cur.WriteByte(c)
		case (c == '\'' || c == '"') && strings.TrimSpace(cur.String()) == "":
			q, quoted = c, true
			cur.Reset()
		case c == ',':
			flush()
		case quoted && (c == ' ' || c == '\t'):
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

var andSep = regexp.MustCompile(`(?i) and `) // not ToUpper+Index: upper-casing can change the byte length

func splitBetween(v string) (string, string, error) {
	loc := andSep.FindStringIndex(v)
	if loc == nil {
		return "", "", fmt.Errorf("BETWEEN needs two values: low AND high")
	}
	return strings.TrimSpace(v[:loc[0]]), strings.TrimSpace(v[loc[1]:]), nil
}

// Where renders the active filters as a WHERE body (without the keyword) plus args.
// cols are all column names, used by Any-column filters. startArg is the first placeholder number.
func (fs FilterSet) Where(d Dialect, cols []string, startArg int) (string, []any, error) {
	var parts []string
	var args []any
	next := func(v any) string {
		args = append(args, v)
		return d.Param(startArg + len(args) - 1)
	}
	for _, f := range fs.Active() {
		if f.Column == RawColumn {
			if v := strings.TrimSpace(f.Value); v != "" {
				if d.Name != "cql" { // CQL has no parenthesized relations
					v = "(" + v + ")"
				}
				parts = append(parts, v)
			}
			continue
		}
		cond := func(col string) (string, error) { return oneCondition(d, col, f, next) }
		if isLikeOp(f.Op) && d.LikeCast && f.Column != AnyColumn {
			quoted := d.Quote(f.Column)
			cond = func(string) (string, error) { return oneCondition(d, d.CastText(quoted), f, next) }
		}
		if f.Column == AnyColumn {
			if len(cols) == 0 {
				return "", nil, fmt.Errorf("filter on any column needs the column list")
			}
			var ors []string
			for _, c := range cols {
				col := d.Quote(c)
				if isLikeOp(f.Op) || f.Op == "=" && d.CastText != nil { // LIKE needs text, and = across mixed column types compares as text (MySQL would coerce 'bob' to 0 and match every zero); <, > must compare natively or 10 < 9 as strings
					col = d.CastText(col)
				}
				s, err := cond(col)
				if err != nil {
					return "", nil, err
				}
				ors = append(ors, s)
			}
			parts = append(parts, "("+strings.Join(ors, " OR ")+")")
			continue
		}
		s, err := cond(d.Quote(f.Column))
		if err != nil {
			return "", nil, err
		}
		parts = append(parts, s)
	}
	join := " AND "
	if fs.Or {
		join = " OR "
	}
	return strings.Join(parts, join), args, nil
}

// WhereText renders the active filters as a WHERE body with every value inlined as a literal, for display
// (the filter bar's SQL popover). Nothing runs it.
func WhereText(d Dialect, fs FilterSet, cols []string) (string, error) {
	_, args, err := fs.Where(d, cols, 1)
	if err != nil {
		return "", err
	}
	lit := d
	lit.Param = func(n int) string { return d.Literal(args[n-1]) }
	w, _, err := fs.Where(lit, cols, 1)
	return w, err
}

func isLikeOp(op string) bool {
	switch op {
	case "LIKE", "ILIKE", "Contains", "Not contains", "Has prefix", "Has suffix":
		return true
	}
	return false
}

func oneCondition(d Dialect, col string, f Filter, next func(any) string) (string, error) {
	like := "LIKE"
	if d.ILike {
		like = "ILIKE" // TablePlus: Contains/prefix/suffix are case-insensitive on PostgreSQL
	}
	if d.Funcs {
		switch f.Op {
		case "Contains":
			return "contains(" + col + ", " + next(f.Value) + ")", nil
		case "Not contains":
			return "NOT contains(" + col + ", " + next(f.Value) + ")", nil
		case "Has prefix":
			return "begins_with(" + col + ", " + next(f.Value) + ")", nil
		case "LIKE", "ILIKE", "Has suffix":
			return "", fmt.Errorf("%s is not supported by PartiQL on DynamoDB", f.Op)
		}
	}
	switch f.Op {
	case "=", "<>", "<", ">", "<=", ">=":
		return col + " " + f.Op + " " + next(f.Value), nil
	case "IS NULL", "IS NOT NULL":
		return col + " " + f.Op, nil
	case "IN", "NOT IN":
		items := splitList(f.Value)
		ph := make([]string, len(items))
		for i, it := range items {
			ph[i] = next(it)
		}
		return col + " " + f.Op + " (" + strings.Join(ph, ", ") + ")", nil
	case "BETWEEN", "NOT BETWEEN":
		lo, hi, err := splitBetween(f.Value)
		if err != nil {
			return "", err
		}
		a := next(lo)
		return col + " " + f.Op + " " + a + " AND " + next(hi), nil
	case "LIKE", "ILIKE":
		return col + " " + f.Op + " " + next(f.Value), nil
	case "Contains":
		return col + " " + like + " " + next("%"+likeEscape(f.Value)+"%"), nil
	case "Not contains":
		return col + " NOT " + like + " " + next("%"+likeEscape(f.Value)+"%"), nil
	case "Has prefix":
		return col + " " + like + " " + next(likeEscape(f.Value)+"%"), nil
	case "Has suffix":
		return col + " " + like + " " + next("%"+likeEscape(f.Value)), nil
	}
	return "", fmt.Errorf("unknown filter operator %q", f.Op)
}

// BuildSelect renders the table-browse SELECT for SQL dialects. allCols feeds Any-column filters.
func BuildSelect(d Dialect, q RowQuery, allCols []string) (string, []any, error) {
	sel := "*"
	if len(q.Columns) > 0 {
		qs := make([]string, len(q.Columns))
		for i, c := range q.Columns {
			qs[i] = d.Quote(c)
		}
		sel = strings.Join(qs, ", ")
	}
	var b strings.Builder
	b.WriteString("SELECT " + sel + " FROM " + d.QualifiedName(q.ObjRef))
	where, args, err := q.Filters.Where(d, allCols, 1)
	if err != nil {
		return "", nil, err
	}
	if where != "" {
		b.WriteString(" WHERE " + where)
	}
	sort := q.Sort
	if len(sort) == 0 { // LIMIT/OFFSET over an unordered scan shows an updated row twice or not at all
		for _, k := range q.Key {
			sort = append(sort, Sort{Column: k})
		}
	}
	if len(sort) > 0 {
		o := make([]string, len(sort))
		for i, s := range sort {
			o[i] = d.Quote(s.Column)
			if s.Desc {
				o[i] += " DESC"
			}
		}
		b.WriteString(" ORDER BY " + strings.Join(o, ", "))
	}
	if q.Limit > 0 {
		fmt.Fprintf(&b, " LIMIT %d", q.Limit)
		if q.Offset > 0 {
			fmt.Fprintf(&b, " OFFSET %d", q.Offset)
		}
	}
	return b.String(), args, nil
}

// BuildCount renders SELECT COUNT(*) with the same filters.
func BuildCount(d Dialect, q RowQuery, allCols []string) (string, []any, error) {
	where, args, err := q.Filters.Where(d, allCols, 1)
	if err != nil {
		return "", nil, err
	}
	s := "SELECT COUNT(*) FROM " + d.QualifiedName(q.ObjRef)
	if where != "" {
		s += " WHERE " + where
	}
	return s, args, nil
}

// BulkUpdate renders UPDATE ref SET set... WHERE <filters> (ALTER TABLE ... UPDATE on ClickHouse), with
// Display inlining every value. cols feeds Any-column filters. It refuses an empty filter set: updating
// every row has to be a deliberate statement typed in the editor.
func BulkUpdate(d Dialect, ref ObjRef, fs FilterSet, set []KeyVal, cols []string) (Statement, error) {
	if len(set) == 0 {
		return Statement{}, fmt.Errorf("bulk update without columns to set")
	}
	if len(fs.Active()) == 0 {
		return Statement{}, fmt.Errorf("bulk update needs at least one active filter")
	}
	b := &builder{d: d}
	if d.Mutate {
		b.raw("ALTER TABLE " + d.QualifiedName(ref) + " UPDATE ")
	} else {
		b.raw("UPDATE " + d.QualifiedName(ref) + " SET ")
	}
	for i, kv := range set {
		if i > 0 {
			b.raw(", ")
		}
		b.raw(d.Quote(kv.Col) + " = ")
		b.val(kv.Val)
	}
	start := len(b.args) + 1
	where, args, err := fs.Where(d, cols, start)
	if err != nil {
		return Statement{}, err
	}
	lit := d // second pass: placeholders become literals for Display
	lit.Param = func(n int) string { return d.Literal(args[n-start]) }
	disp, _, _ := fs.Where(lit, cols, start)
	b.text.WriteString(" WHERE " + where)
	b.disp.WriteString(" WHERE " + disp)
	b.args = append(b.args, args...)
	return Statement{Text: b.text.String(), Args: b.args, Display: b.disp.String()}, nil
}
