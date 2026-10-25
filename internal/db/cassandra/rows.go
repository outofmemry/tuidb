package cassandra

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"

	"webtuidb/internal/db"
)

const filteringNote = "ALLOW FILTERING was added: no key or index covers this filter, so Cassandra reads the rows and filters them server-side (slow on big tables)"

// filterArgs turns the filter bar's text values into driver values, in placeholder order.
func filterArgs(fs db.FilterSet, types map[string]gocql.TypeInfo, args []any) error {
	i := 0
	for _, f := range fs.Active() {
		if f.Column == db.RawColumn {
			continue
		}
		n := 1
		switch f.Op {
		case "IN", "NOT IN":
			n = len(strings.Split(f.Value, ","))
		case "IS NULL", "IS NOT NULL":
			n = 0
		}
		for ; n > 0; n-- {
			v, err := goValue(types[f.Column], args[i])
			if err != nil {
				return fmt.Errorf("filter on %s: %w", f.Column, err)
			}
			args[i] = v
			i++
		}
	}
	return nil
}

// checkFilters rejects what CQL cannot express before the server does with a cryptic message.
func checkFilters(fs db.FilterSet) error {
	act := fs.Active()
	if fs.Or && len(act) > 1 {
		return errors.New("CQL has no OR: use one filter, or an IN filter")
	}
	for _, f := range act {
		if f.Column == db.AnyColumn {
			return errors.New("CQL cannot search every column at once: pick a column for the filter")
		}
	}
	return nil
}

// prepare qualifies the reference and loads column types when there are filters to convert.
func (s *session) prepare(ctx context.Context, rq db.RowQuery) (db.RowQuery, map[string]gocql.TypeInfo, error) {
	ks, err := s.ksOf(rq.ObjRef)
	if err != nil {
		return rq, nil, err
	}
	rq.Schema = ks
	if err := checkFilters(rq.Filters); err != nil {
		return rq, nil, err
	}
	if len(rq.Filters.Active()) == 0 {
		return rq, nil, nil
	}
	types, err := s.colTypes(ctx, rq.ObjRef)
	return rq, types, err
}

// Rows has no OFFSET to use: LIMIT offset+limit rows are read and the first offset are dropped, so a deep page costs as
// much as everything before it. ORDER BY only works on clustering columns inside one partition; the server's error says so.
func (s *session) Rows(ctx context.Context, rq db.RowQuery) (*db.Result, error) {
	skip := rq.Offset
	rq, types, err := s.prepare(ctx, rq)
	if err != nil {
		return nil, err
	}
	if rq.Limit > 0 {
		rq.Limit += skip
	}
	rq.Offset = 0
	text, args, err := db.BuildSelect(db.CQL, rq, nil)
	if err != nil {
		return nil, err
	}
	if err := filterArgs(rq.Filters, types, args); err != nil {
		return nil, err
	}
	start := time.Now()
	filtered := len(rq.Filters.Active()) > 0
	res, err := s.run(ctx, text, args, rq.Limit-skip, skip)
	if err != nil && filtered && needsFiltering(err) {
		if res, err = s.run(ctx, text+" ALLOW FILTERING", args, rq.Limit-skip, skip); err == nil {
			res.Message = filteringNote
		}
	}
	if err != nil {
		return nil, err
	}
	res.Duration = time.Since(start)
	return res, nil
}

func needsFiltering(err error) bool { return strings.Contains(err.Error(), "ALLOW FILTERING") }

// Count estimates from system.size_estimates, which counts partitions (not rows) and refreshes every few minutes;
// an unfiltered estimate of 0 falls through to the exact SELECT COUNT(*), which scans the whole table.
func (s *session) Count(ctx context.Context, rq db.RowQuery, estimate bool) (db.Count, error) {
	rq, types, err := s.prepare(ctx, rq)
	if err != nil {
		return db.Count{}, err
	}
	filtered := len(rq.Filters.Active()) > 0
	if estimate && !filtered {
		rows, err := s.maps(ctx, "SELECT partitions_count FROM system.size_estimates WHERE keyspace_name = ? AND table_name = ?", rq.Schema, rq.Name)
		if err != nil {
			return db.Count{}, fmt.Errorf("count estimate: %w", err)
		}
		var n int64
		for _, r := range rows {
			v, _ := r["partitions_count"].(int64)
			n += v
		}
		if n > 0 {
			return db.Count{N: n}, nil
		}
	}
	text, args, err := db.BuildCount(db.CQL, rq, nil)
	if err != nil {
		return db.Count{}, err
	}
	if err := filterArgs(rq.Filters, types, args); err != nil {
		return db.Count{}, err
	}
	count := func(text string) (n int64, err error) {
		err = s.query(text, args...).ScanContext(ctx, &n)
		return n, err
	}
	n, err := count(text)
	if err != nil && filtered && needsFiltering(err) {
		n, err = count(text + " ALLOW FILTERING")
	}
	if err != nil {
		return db.Count{}, fmt.Errorf("count: %w", err)
	}
	return db.Count{N: n, Exact: true}, nil
}
