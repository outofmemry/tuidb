package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/db"
)

func TestM2ChartToggles(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	queryResult(h, "select * from customers")
	h.prefix("g")
	h.want("bar chart")
	h.want("█")
	h.prefix("g")
	h.wantNot("bar chart")
}

func TestM6ChartWithSeveralYColumns(t *testing.T) {
	res := &db.Result{HasRows: true, Columns: []db.ResultColumn{{Name: "m", Type: "text"}, {Name: "a", Type: "int"}, {Name: "b", Type: "int"}},
		Rows: [][]any{{"jan", int64(3), int64(5)}, {"feb", int64(4), int64(1)}}}
	rv := &resultView{raw: res, stmt: "-- barchart x: m, y: a, b\nselect 1"}
	h := newHarness(t, nil)
	out := stripANSI(strings.Join(chartLines(h.a.th, rv, 80, 20), "\n"))
	for _, want := range []string{"█ a", "▓ b", "jan", "feb", "5"} {
		if !strings.Contains(out, want) {
			t.Fatalf("chart lacks %q:\n%s", want, out)
		}
	}
}
