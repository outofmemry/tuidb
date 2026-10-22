package mongo

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"

	"tuidb/internal/db"
)

// colInfo is the sampled structure of a collection, cached for filter and edit coercion.
type colInfo struct {
	names []string
	types map[string]string
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

// likeRegex converts an SQL LIKE pattern to an anchored regex.
func likeRegex(p string) string {
	var b strings.Builder
	b.WriteString("^")
	rs := []rune(p)
	for i := 0; i < len(rs); i++ {
		switch r := rs[i]; {
		case r == '\\' && i+1 < len(rs):
			i++
			b.WriteString(regexp.QuoteMeta(string(rs[i])))
		case r == '%':
			b.WriteString(".*")
		case r == '_':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	return b.String()
}

// loose converts a filter value to the field's type; unconvertible values stay strings.
func loose(s, typ string) any {
	if v, ok, err := coerceTyped(s, typ); ok && err == nil {
		return v
	}
	return s
}

// candidates lists the values a filter string may mean: fields of mixed type match a number or the text.
func candidates(s, typ string) bson.A {
	if typ == "mixed" {
		if n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
			return bson.A{n, s}
		} else if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return bson.A{f, s}
		}
	}
	return bson.A{loose(s, typ)}
}

func condition(col string, f db.Filter, typ string) (bson.D, error) {
	one := func(v any) bson.D { return bson.D{{Key: col, Value: v}} }
	op := func(name string, v any) bson.D { return one(bson.D{{Key: name, Value: v}}) }
	switch f.Op {
	case "=", "<>":
		c := candidates(f.Value, typ)
		switch {
		case len(c) == 1 && f.Op == "=":
			return op("$eq", c[0]), nil
		case len(c) == 1:
			return op("$ne", c[0]), nil
		case f.Op == "=":
			return op("$in", c), nil
		}
		return op("$nin", c), nil
	case "<":
		return op("$lt", candidates(f.Value, typ)[0]), nil
	case ">":
		return op("$gt", candidates(f.Value, typ)[0]), nil
	case "<=":
		return op("$lte", candidates(f.Value, typ)[0]), nil
	case ">=":
		return op("$gte", candidates(f.Value, typ)[0]), nil
	case "IS NULL":
		return one(nil), nil
	case "IS NOT NULL":
		return op("$ne", nil), nil
	case "IN", "NOT IN":
		var items bson.A
		for _, it := range splitList(f.Value) {
			items = append(items, candidates(it, typ)...)
		}
		if f.Op == "IN" {
			return op("$in", items), nil
		}
		return op("$nin", items), nil
	case "BETWEEN", "NOT BETWEEN":
		i := strings.Index(strings.ToUpper(f.Value), " AND ")
		if i < 0 {
			return nil, fmt.Errorf("BETWEEN needs two values: low AND high")
		}
		rng := bson.D{{Key: "$gte", Value: loose(strings.TrimSpace(f.Value[:i]), typ)}, {Key: "$lte", Value: loose(strings.TrimSpace(f.Value[i+5:]), typ)}}
		if f.Op == "BETWEEN" {
			return one(rng), nil
		}
		return op("$not", rng), nil
	case "LIKE":
		return one(bson.Regex{Pattern: likeRegex(f.Value)}), nil
	case "ILIKE":
		return one(bson.Regex{Pattern: likeRegex(f.Value), Options: "i"}), nil
	case "Contains":
		return one(bson.Regex{Pattern: regexp.QuoteMeta(f.Value), Options: "i"}), nil
	case "Not contains":
		return op("$not", bson.Regex{Pattern: regexp.QuoteMeta(f.Value), Options: "i"}), nil
	case "Has prefix":
		return one(bson.Regex{Pattern: "^" + regexp.QuoteMeta(f.Value), Options: "i"}), nil
	case "Has suffix":
		return one(bson.Regex{Pattern: regexp.QuoteMeta(f.Value) + "$", Options: "i"}), nil
	}
	return nil, fmt.Errorf("unknown filter operator %q", f.Op)
}

// buildFilter converts the filter bar to a bson filter. rawDoc parses RawColumn values.
func buildFilter(fs db.FilterSet, ci *colInfo, rawDoc func(string) (bson.D, error)) (bson.D, error) {
	var parts bson.A
	for _, f := range fs.Active() {
		switch f.Column {
		case db.RawColumn:
			if strings.TrimSpace(f.Value) == "" {
				continue
			}
			d, err := rawDoc(f.Value)
			if err != nil {
				return nil, fmt.Errorf("raw filter: %w", err)
			}
			parts = append(parts, d)
		case db.AnyColumn:
			var ors bson.A
			for _, n := range ci.names {
				c, err := condition(n, f, ci.types[n])
				if err != nil {
					return nil, err
				}
				ors = append(ors, c)
			}
			if len(ors) == 0 {
				return nil, fmt.Errorf("filter on any column needs a non-empty collection")
			}
			parts = append(parts, bson.D{{Key: "$or", Value: ors}})
		default:
			c, err := condition(f.Column, f, ci.types[f.Column])
			if err != nil {
				return nil, err
			}
			parts = append(parts, c)
		}
	}
	switch {
	case len(parts) == 0:
		return bson.D{}, nil
	case len(parts) == 1:
		return parts[0].(bson.D), nil
	case fs.Or:
		return bson.D{{Key: "$or", Value: parts}}, nil
	}
	return bson.D{{Key: "$and", Value: parts}}, nil
}
