package db

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseChart(t *testing.T) {
	for in, want := range map[string]ChartSpec{
		"-- barchart x: a, y: b\nselect 1":   {"bar", "a", "b", []string{"b"}},
		"select 1\n  --LINECHART  y:b ,x:a ": {"line", "a", "b", []string{"b"}},
		"-- piechart x: name":                {"pie", "name", "", nil},
		"-- barchart x: a, y: b, c ,d":       {"bar", "a", "b", []string{"b", "c", "d"}},
	} {
		if got, ok := ParseChart(in); !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %v %v", in, got, ok)
		}
	}
	if _, ok := ParseChart("select 1 -- chart x: a"); ok {
		t.Error("no chart comment")
	}
}

func TestChart(t *testing.T) {
	res := &Result{
		Columns: []ResultColumn{{Name: "id"}, {Name: "Name"}, {Name: "n"}},
		Rows:    [][]any{{int64(1), "a", "3"}, {int64(2), "b", "x"}, {int64(3), "c", float64(9)}, {int64(4), "d", nil}, {int64(5), "e", []byte("5")}},
	}
	got, err := Chart(res, ChartSpec{})
	if err != nil || !reflect.DeepEqual(got.Labels, []string{"a", "b", "c", "d", "e"}) || !reflect.DeepEqual(got.Values, []float64{1, 2, 3, 4, 5}) {
		t.Errorf("inferred: %v %v", got, err)
	}
	got, err = Chart(res, ChartSpec{Kind: "pie", X: "name", Y: "N"})
	if err != nil || !reflect.DeepEqual(got.Labels, []string{"c", "e", "a"}) || !reflect.DeepEqual(got.Values, []float64{9, 5, 3}) {
		t.Errorf("pie: %v %v", got, err)
	}
	if _, err = Chart(res, ChartSpec{X: "nope"}); err == nil {
		t.Error("unknown column")
	}
	got, err = Chart(res, ChartSpec{Kind: "bar", X: "Name", Ys: []string{"n", "id"}})
	if err != nil || len(got.Series) != 2 || got.Series[1].Name != "id" || !reflect.DeepEqual(got.Series[1].Values, []float64{1, 3, 5}) || !reflect.DeepEqual(got.Values, got.Series[0].Values) {
		t.Errorf("multi: %+v %v", got, err)
	}
}

func TestChartDecimalsAndYOverride(t *testing.T) {
	res := &Result{Columns: []ResultColumn{{Name: "l"}, {Name: "a"}, {Name: "b"}}, Rows: [][]any{{"x", json.Number("1.5"), int32(7)}, {"y", json.Number("2"), 8}}}
	s, err := Chart(res, ChartSpec{X: "l"})
	if err != nil || !reflect.DeepEqual(s.Values, []float64{1.5, 2}) {
		t.Errorf("json.Number: %v %+v", err, s)
	}
	// ParseChart left Ys = [a]; the board then saved Y = b: the explicit Y wins.
	s, err = Chart(res, ChartSpec{X: "l", Y: "b", Ys: []string{"a"}})
	if err != nil || !reflect.DeepEqual(s.Values, []float64{7, 8}) {
		t.Errorf("Y override: %v %+v", err, s)
	}
}

func TestChartRejectsNaN(t *testing.T) {
	res := &Result{Columns: []ResultColumn{{Name: "l"}, {Name: "v"}}, Rows: [][]any{{"a", "NaN"}, {"b", "inf"}, {"c", 2.0}}}
	s, err := Chart(res, ChartSpec{Kind: "bar", X: "l", Y: "v"})
	if err != nil || len(s.Values) != 1 || s.Labels[0] != "c" {
		t.Errorf("%v %+v", err, s)
	}
}
