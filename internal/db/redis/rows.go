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

	"webtuidb/internal/db"
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
		if start > maxLoad {
			return nil, fmt.Errorf("offset %d is too deep for a %s key (at most %d): its elements have no stable positions, filter instead", start, typ, maxLoad)
		}
		// seen keeps only names (at most start+n) to drop SCAN repeats; passed values are not kept.
		seen := map[string]bool{}
		var cursor uint64
		for len(seen) < start+n {
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
			for i := 0; i+step <= len(items) && len(seen) < start+n; i += step {
				if seen[items[i]] { // SCAN may repeat elements
					continue
				}
				pos := len(seen)
				seen[items[i]] = true
				switch {
				case pos < start:
				case typ == "hash":
					rows = append(rows, []any{cell(items[i]), cell(items[i+1])})
				default:
					rows = append(rows, []any{cell(items[i])})
				}
			}
			if cursor == 0 {
				break
			}
		}
		return rows, nil
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
		from, pos := s.streamFrom(key, start)
		for pos < start { // skip to start in chunks without keeping them
			ms, err := cl.XRangeN(ctx, key, from, "+", int64(min(start-pos, 1000))).Result()
			if err != nil {
				return nil, fmt.Errorf("xrange %q: %w", key, err)
			}
			if len(ms) == 0 {
				return nil, nil
			}
			from, pos = "("+ms[len(ms)-1].ID, pos+len(ms)
		}
		ms, err := cl.XRangeN(ctx, key, from, "+", int64(n)).Result()
		if err != nil {
			return nil, fmt.Errorf("xrange %q: %w", key, err)
		}
		for _, m := range ms {
			b, _ := json.Marshal(m.Values)
			rows = append(rows, []any{m.ID, string(b)})
		}
		if len(ms) > 0 {
			s.markStream(key, streamMark{start, from}, streamMark{start + len(ms), "(" + ms[len(ms)-1].ID})
		}
	default:
		return nil, fmt.Errorf("unsupported redis type %q", typ)
	}
	if typ == "string" || typ == "ReJSON-RL" {
		return window(rows, start, n), nil
	}
	return rows, nil
}

// streamMark says the stream entries from offset on are read by XRANGE starting at from.
type streamMark struct {
	offset int
	from   string
}

// streamFrom returns the closest remembered XRANGE start at or before offset.
// shortcut: marks assume entries before them are not deleted (XDEL/XTRIM shift offsets); a
// reload from offset 0 resets them.
func (s *session) streamFrom(key string, offset int) (string, int) {
	from, pos := "-", 0
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.marks[key] {
		if m.from != "" && m.offset <= offset && m.offset > pos {
			from, pos = m.from, m.offset
		}
	}
	return from, pos
}

func (s *session) markStream(key string, page, next streamMark) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.marks == nil {
		s.marks = map[string][2]streamMark{}
	}
	s.marks[key] = [2]streamMark{page, next}
}

func window(rows [][]any, start, n int) [][]any {
	if start >= len(rows) {
		return nil
	}
	return rows[start:min(len(rows), start+n)]
}

// loadAll reads at most maxLoad elements of key for in-memory filtering/sorting; capped
// reports that the key holds more, so results built from rows are incomplete.
func (s *session) loadAll(ctx context.Context, key, typ string) (rows [][]any, capped bool, err error) {
	if rows, err = s.fetch(ctx, key, typ, 0, 0); err != nil || len(rows) < maxLoad {
		return rows, false, err
	}
	n, err := s.Count(ctx, db.RowQuery{ObjRef: db.ObjRef{Name: key}}, false)
	return rows, n.N > int64(maxLoad), err
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
	capped := false
	if len(q.Filters.Active()) > 0 || len(q.Sort) > 0 {
		if rows, capped, err = s.loadAll(ctx, q.Name, typ); err != nil {
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
	res := &db.Result{Rows: rows, HasRows: true, Truncated: capped}
	for _, r := range rows {
		res.Docs = append(res.Docs, rowDoc(typ, names, r))
	}
	if len(q.Columns) > 0 { // project after the fact: Docs stay whole for identity
		var keep []int
		for _, w := range q.Columns {
			for i, n := range names {
				if n == w {
					keep = append(keep, i)
				}
			}
		}
		if len(keep) > 0 {
			shown := make([]string, len(keep))
			for j, i := range keep {
				shown[j] = names[i]
			}
			for r, row := range rows {
				out := make([]any, len(keep))
				for j, i := range keep {
					out[j] = row[i]
				}
				rows[r] = out
			}
			res.Rows = rows
			names = shown
		}
	}
	for _, n := range names {
		t := colTypes[n]
		if t == "" {
			t = "string"
		}
		res.Columns = append(res.Columns, db.ResultColumn{Name: n, Type: t})
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
		rows, capped, err := s.loadAll(ctx, q.Name, typ)
		if err != nil {
			return db.Count{}, err
		}
		if rows, err = filterRows(rows, names, q.Filters); err != nil {
			return db.Count{}, err
		}
		return db.Count{N: int64(len(rows)), Exact: !capped}, nil
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
