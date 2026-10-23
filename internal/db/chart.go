package db

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Kind "bar" | "line" | "pie". Y is Ys[0] (kept for older callers).
type ChartSpec struct {
	Kind, X, Y string
	Ys         []string
}

var (
	chartRe = regexp.MustCompile(`(?im)^\s*--\s*(bar|line|pie)chart\b(.*)$`)
)

// ParseChart reads a "-- barchart x: a, y: b" comment (barchart, linechart or piechart).
func ParseChart(stmt string) (ChartSpec, bool) {
	m := chartRe.FindStringSubmatch(stmt)
	if m == nil {
		return ChartSpec{}, false
	}
	s := ChartSpec{Kind: strings.ToLower(m[1])}
	key := ""
	for _, tok := range strings.Split(m[2], ",") { // "x: a, y: b, c": a token without "k:" extends the last key
		if k, v, ok := strings.Cut(tok, ":"); ok {
			key, tok = strings.ToLower(strings.TrimSpace(k)), v
		}
		if tok = strings.TrimSpace(tok); tok == "" {
			continue
		}
		switch key {
		case "x":
			s.X = tok
		case "y":
			s.Ys = append(s.Ys, tok)
		}
	}
	if len(s.Ys) > 0 {
		s.Y = s.Ys[0]
	}
	return s, true
}

type NamedSeries struct {
	Name   string
	Values []float64
}

// ChartSeries: Values is the first series, for callers that draw one.
type ChartSeries struct {
	Labels []string
	Values []float64
	Series []NamedSeries
}

func cellNum(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case float32:
		return cellNum(float64(x))
	case json.Number: // DynamoDB keeps non-integer numbers as json.Number
		f, err := x.Float64()
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	case uint64:
		return float64(x), true
	case float64:
		return x, !math.IsNaN(x) && !math.IsInf(x, 0)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	case []byte:
		return cellNum(string(x))
	}
	return 0, false
}

func cellLabel(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(x)
	case time.Time:
		return x.Format(time.RFC3339)
	}
	return fmt.Sprint(v)
}

// Chart extracts the labelled numeric series. Empty spec columns are inferred: Y is the first numeric
// column, X the first other column that is not numeric (else the first other column).
func Chart(res *Result, spec ChartSpec) (ChartSeries, error) {
	col := func(name string) (int, error) {
		for i, c := range res.Columns {
			if strings.EqualFold(c.Name, name) {
				return i, nil
			}
		}
		return -1, fmt.Errorf("chart: no column %q", name)
	}
	numeric := func(i int) bool {
		seen := false
		for _, r := range res.Rows {
			if r[i] == nil {
				continue
			}
			if _, ok := cellNum(r[i]); !ok {
				return false
			}
			seen = true
		}
		return seen
	}
	first := func(skip int, want func(int) bool) int {
		for i := range res.Columns {
			if i != skip && want(i) {
				return i
			}
		}
		return -1
	}
	x := -1
	var ys []int
	var err error
	if spec.X != "" {
		if x, err = col(spec.X); err != nil {
			return ChartSeries{}, err
		}
	}
	names := spec.Ys
	if spec.Y != "" && (len(names) == 0 || !strings.EqualFold(names[0], spec.Y)) { // an explicit Y overrides a stale Ys
		names = []string{spec.Y}
	}
	for _, n := range names {
		y, err := col(n)
		if err != nil {
			return ChartSeries{}, err
		}
		ys = append(ys, y)
	}
	if len(ys) == 0 {
		if y := first(x, numeric); y >= 0 {
			ys = []int{y}
		}
	}
	if len(ys) == 0 {
		return ChartSeries{}, fmt.Errorf("chart: need one label column and one numeric column")
	}
	if x < 0 {
		if x = first(ys[0], func(i int) bool { return !numeric(i) }); x < 0 {
			x = first(ys[0], func(int) bool { return true })
		}
	}
	if x < 0 {
		return ChartSeries{}, fmt.Errorf("chart: need one label column and one numeric column")
	}
	var s ChartSeries
	s.Series = make([]NamedSeries, len(ys))
	for i, y := range ys {
		s.Series[i].Name = res.Columns[y].Name
	}
	for _, r := range res.Rows {
		if _, ok := cellNum(r[ys[0]]); !ok { // rows are kept by the first series; a missing value in another is 0
			continue
		}
		s.Labels = append(s.Labels, cellLabel(r[x]))
		for i, y := range ys {
			v, _ := cellNum(r[y])
			s.Series[i].Values = append(s.Series[i].Values, v)
		}
	}
	if spec.Kind == "pie" {
		idx := make([]int, len(s.Labels))
		for i := range idx {
			idx[i] = i
		}
		v0 := s.Series[0].Values
		sort.SliceStable(idx, func(a, b int) bool { return v0[idx[a]] > v0[idx[b]] })
		l := make([]string, len(idx))
		for i, j := range idx {
			l[i] = s.Labels[j]
		}
		s.Labels = l
		for k := range s.Series {
			v := make([]float64, len(idx))
			for i, j := range idx {
				v[i] = s.Series[k].Values[j]
			}
			s.Series[k].Values = v
		}
	}
	s.Values = s.Series[0].Values
	return s, nil
}
