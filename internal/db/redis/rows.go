package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"tuidb/internal/db"
)

// fetch reads up to n elements of a key starting at element start (n<=0: up to maxLoad).
// Rows follow colNames(typ). hash/set have no stable offsets, so they are scanned from the start.
func (s *session) fetch(ctx context.Context, key, typ string, start, n int) ([][]any, error) {
	cl := s.c()
	if n <= 0 || n > maxLoad {
		n = maxLoad
	}
	var rows [][]any
	switch typ {
	case "string":
		v, err := cl.Get(ctx, key).Result()
		if err != nil {
			return nil, fmt.Errorf("get %q: %w", key, err)
		}
		rows = [][]any{{cell(v)}}
	case "ReJSON-RL":
		v, err := cl.Do(ctx, "JSON.GET", key).Text()
		if err != nil {
			return nil, fmt.Errorf("json.get %q: %w", key, err)
		}
		rows = [][]any{{cell(v)}}
	case "hash", "set":
		seen := map[string]bool{}
		var cursor uint64
		for len(rows) < start+n {
			var items []string
			var err error
			if typ == "hash" {
				items, cursor, err = cl.HScan(ctx, key, cursor, "*", 1000).Result()
			} else {
				items, cursor, err = cl.SScan(ctx, key, cursor, "*", 1000).Result()
			}
			if err != nil {
				return nil, fmt.Errorf("scan %q: %w", key, err)
			}
			step := 1
			if typ == "hash" {
				step = 2
			}
			for i := 0; i+step <= len(items); i += step {
				if seen[items[i]] { // SCAN may repeat elements
					continue
				}
				seen[items[i]] = true
				if typ == "hash" {
					rows = append(rows, []any{cell(items[i]), cell(items[i+1])})
				} else {
					rows = append(rows, []any{cell(items[i])})
				}
			}
			if cursor == 0 {
				break
			}
		}
		return window(rows, start, n), nil
	case "list":
		vs, err := cl.LRange(ctx, key, int64(start), int64(start+n-1)).Result()
		if err != nil {
			return nil, fmt.Errorf("lrange %q: %w", key, err)
		}
		for i, v := range vs {
			rows = append(rows, []any{int64(start + i), cell(v)})
		}
	case "zset":
		zs, err := cl.ZRangeWithScores(ctx, key, int64(start), int64(start+n-1)).Result()
		if err != nil {
			return nil, fmt.Errorf("zrange %q: %w", key, err)
		}
		for _, z := range zs {
			rows = append(rows, []any{cell(str(z.Member)), z.Score})
		}
	case "stream":
		ms, err := cl.XRangeN(ctx, key, "-", "+", int64(start+n)).Result()
		if err != nil {
			return nil, fmt.Errorf("xrange %q: %w", key, err)
		}
		for _, m := range ms {
			b, _ := json.Marshal(m.Values)
			rows = append(rows, []any{m.ID, string(b)})
		}
		return window(rows, start, n), nil
	default:
		return nil, fmt.Errorf("unsupported redis type %q", typ)
	}
	if typ == "string" || typ == "ReJSON-RL" {
		return window(rows, start, n), nil
	}
	return rows, nil
}

func window(rows [][]any, start, n int) [][]any {
	if start >= len(rows) {
		return nil
	}
	return rows[start:min(len(rows), start+n)]
}

func (s *session) Rows(ctx context.Context, q db.RowQuery) (*db.Result, error) {
	typ, err := s.typeOf(ctx, q.Name)
	if err != nil {
		return nil, err
	}
	names, err := colNames(typ)
	if err != nil {
		return nil, err
	}
	offset := max(q.Offset, 0)
	var rows [][]any
	if len(q.Filters.Active()) > 0 || len(q.Sort) > 0 {
		if rows, err = s.fetch(ctx, q.Name, typ, 0, 0); err != nil {
			return nil, err
		}
		if rows, err = filterRows(rows, names, q.Filters); err != nil {
			return nil, err
		}
		if err = sortRows(rows, names, q.Sort); err != nil {
			return nil, err
		}
		if q.Limit > 0 {
			rows = window(rows, offset, q.Limit)
		} else {
			rows = window(rows, offset, maxLoad)
		}
	} else if rows, err = s.fetch(ctx, q.Name, typ, offset, max(q.Limit, 0)); err != nil {
		return nil, err
	}
	res := &db.Result{Rows: rows, HasRows: true}
	for _, n := range names {
		t := colTypes[n]
		if t == "" {
			t = "string"
		}
		res.Columns = append(res.Columns, db.ResultColumn{Name: n, Type: t})
	}
	for _, r := range rows {
		res.Docs = append(res.Docs, rowDoc(typ, names, r))
	}
	return res, nil
}

// rowDoc is the Json/Tree view of one row. Stream fields are already JSON text.
func rowDoc(typ string, names []string, row []any) string {
	m := make(map[string]any, len(names))
	for i, n := range names {
		v := row[i]
		if b, ok := v.([]byte); ok {
			v = string(b)
		}
		if typ == "stream" && n == "fields" {
			v = json.RawMessage(str(v))
		}
		m[n] = v
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func (s *session) Count(ctx context.Context, q db.RowQuery, _ bool) (db.Count, error) {
	typ, err := s.typeOf(ctx, q.Name)
	if err != nil {
		return db.Count{}, err
	}
	if len(q.Filters.Active()) > 0 {
		names, err := colNames(typ)
		if err != nil {
			return db.Count{}, err
		}
		rows, err := s.fetch(ctx, q.Name, typ, 0, 0)
		if err != nil {
			return db.Count{}, err
		}
		if rows, err = filterRows(rows, names, q.Filters); err != nil {
			return db.Count{}, err
		}
		return db.Count{N: int64(len(rows)), Exact: true}, nil
	}
	cl := s.c()
	var n int64
	switch typ {
	case "hash":
		n, err = cl.HLen(ctx, q.Name).Result()
	case "list":
		n, err = cl.LLen(ctx, q.Name).Result()
	case "set":
		n, err = cl.SCard(ctx, q.Name).Result()
	case "zset":
		n, err = cl.ZCard(ctx, q.Name).Result()
	case "stream":
		n, err = cl.XLen(ctx, q.Name).Result()
	default:
		n = 1
	}
	if err != nil {
		return db.Count{}, fmt.Errorf("count %q: %w", q.Name, err)
	}
	return db.Count{N: n, Exact: true}, nil
}

func text(v any) string {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return db.FormatValue(v)
}

func filterRows(rows [][]any, names []string, fs db.FilterSet) ([][]any, error) {
	active := fs.Active()
	if len(active) == 0 {
		return rows, nil
	}
	idx := make([]int, len(active)) // -1 = any column
	for i, f := range active {
		idx[i] = -1
		if f.Column == db.RawColumn {
			return nil, errors.New("raw filters are not supported for redis")
		}
		if f.Column != db.AnyColumn {
			if idx[i] = indexOf(names, f.Column); idx[i] < 0 {
				return nil, fmt.Errorf("no column %q", f.Column)
			}
		}
		if _, err := match(f.Op, "", f.Value); err != nil {
			return nil, err
		}
	}
	var out [][]any
	for _, r := range rows {
		keep := !fs.Or
		for i, f := range active {
			ok := false
			for c := range r {
				if idx[i] >= 0 && idx[i] != c {
					continue
				}
				if m, _ := match(f.Op, text(r[c]), f.Value); m {
					ok = true
					break
				}
			}
			if fs.Or {
				keep = keep || ok
			} else {
				keep = keep && ok
			}
		}
		if keep {
			out = append(out, r)
		}
	}
	return out, nil
}

func indexOf(ss []string, s string) int {
	for i, x := range ss {
		if x == s {
			return i
		}
	}
	return -1
}

func match(op, cell, val string) (bool, error) {
	switch op {
	case "=":
		return cell == val, nil
	case "<>":
		return cell != val, nil
	case "Contains":
		return strings.Contains(cell, val), nil
	case "Not contains":
		return !strings.Contains(cell, val), nil
	case "Has prefix":
		return strings.HasPrefix(cell, val), nil
	case "Has suffix":
		return strings.HasSuffix(cell, val), nil
	case "LIKE":
		return likeRegexp(val).MatchString(cell), nil
	}
	return false, fmt.Errorf("filter operator %q is not supported for redis", op)
}

func likeRegexp(pat string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("(?s)^")
	rs := []rune(pat)
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
	return regexp.MustCompile(b.String())
}

func sortRows(rows [][]any, names []string, sorts []db.Sort) error {
	idx := make([]int, len(sorts))
	for i, s := range sorts {
		if idx[i] = indexOf(names, s.Column); idx[i] < 0 {
			return fmt.Errorf("no column %q to sort by", s.Column)
		}
	}
	sort.SliceStable(rows, func(a, b int) bool {
		for i, s := range sorts {
			x, y := text(rows[a][idx[i]]), text(rows[b][idx[i]])
			c := strings.Compare(x, y)
			fx, ex := strconv.ParseFloat(x, 64)
			fy, ey := strconv.ParseFloat(y, 64)
			if ex == nil && ey == nil { // numbers sort numerically
				switch {
				case fx < fy:
					c = -1
				case fx > fy:
					c = 1
				default:
					c = 0
				}
			}
			if c != 0 {
				return (c < 0) != s.Desc
			}
		}
		return false
	})
	return nil
}
