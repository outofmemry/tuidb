package db

import (
	"fmt"
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
	ops := []string{"=", "<>", "<", ">", "<=", ">=", "IN", "NOT IN", "IS NULL", "IS NOT NULL", "BETWEEN", "NOT BETWEEN", "LIKE"}
	if dialect == "postgres" {
		ops = append(ops, "ILIKE")
	}
	return append(ops, "Contains", "Not contains", "Has prefix", "Has suffix")
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if len(p) >= 2 && (p[0] == '\'' || p[0] == '"') && p[len(p)-1] == p[0] {
			p = p[1 : len(p)-1]
		}
		out = append(out, p)
	}
	return out
}

func splitBetween(v string) (string, string, error) {
	i := strings.Index(strings.ToUpper(v), " AND ")
	if i < 0 {
		return "", "", fmt.Errorf("BETWEEN needs two values: low AND high")
	}
	return strings.TrimSpace(v[:i]), strings.TrimSpace(v[i+5:]), nil
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
			if strings.TrimSpace(f.Value) != "" {
				parts = append(parts, "("+f.Value+")")
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
				if isLikeOp(f.Op) { // LIKE needs text; =, <, > must compare natively or 10 < 9 as strings
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
	if len(q.Sort) > 0 {
		o := make([]string, len(q.Sort))
		for i, s := range q.Sort {
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
