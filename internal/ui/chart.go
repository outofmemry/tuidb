// chart.go: Text charts drawn from a result (single and multi series).

package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"webtuidb/internal/db"
)

// chartLines draws the result as text bars scaled to the pane width.
// shortcut: a line chart is drawn as bars too (the web draws a real line); the rows are the series either way.
func chartLines(th Theme, rv *resultView, W, H int) []string {
	spec, _ := db.ParseChart(rv.stmt)
	if spec.Kind == "" {
		spec.Kind = "bar"
	}
	s, err := db.Chart(rv.raw, spec)
	if err != nil {
		return []string{"", "  " + th.Err.Render("✗ "+err.Error())}
	}
	if len(s.Series) > 1 {
		return multiChartLines(th, spec.Kind, s, W, H)
	}
	idx := make([]int, len(s.Values))
	total, top := 0.0, 0.0
	for i, v := range s.Values {
		idx[i], total, top = i, total+max(v, 0), max(top, v)
	}
	if spec.Kind == "pie" {
		sort.SliceStable(idx, func(a, b int) bool { return s.Values[idx[a]] > s.Values[idx[b]] })
	}
	lw, vw := 0, 0
	vals := make([]string, len(idx))
	for k, i := range idx {
		vals[k] = strconv.FormatFloat(s.Values[i], 'f', -1, 64)
		if spec.Kind == "pie" && total > 0 {
			vals[k] += fmt.Sprintf(" (%.0f%%)", 100*max(s.Values[i], 0)/total)
		}
		lw, vw = max(lw, width(s.Labels[i])), max(vw, width(vals[k]))
	}
	lw = min(lw, max(8, W/3))
	bw := max(1, W-lw-vw-6)
	lines := []string{th.Dim.Render(fmt.Sprintf(" %s chart · %d points · g: back to the grid · b: add to a metrics board", spec.Kind, len(idx)))}
	for k, i := range idx {
		if len(lines) >= H-1 && k < len(idx)-1 {
			lines = append(lines, th.Dim.Render(fmt.Sprintf("  … %d more", len(idx)-k)))
			break
		}
		n := 0
		if top > 0 && s.Values[i] > 0 {
			n = max(1, int(s.Values[i]/top*float64(bw)+0.5))
		}
		bar := th.Accent.Render(strings.Repeat("█", n)) + strings.Repeat(" ", bw-n)
		lines = append(lines, "  "+fit(s.Labels[i], lw)+" "+bar+" "+padLeft(vals[k], vw))
	}
	return lines
}

var seriesFill = []string{"█", "▓", "▒", "░"}

// multiChartLines draws grouped bars: one row per series under each label, with a legend line first.
func multiChartLines(th Theme, kind string, s db.ChartSeries, W, H int) []string {
	top := 0.0
	for _, ser := range s.Series {
		for _, v := range ser.Values {
			top = max(top, v)
		}
	}
	var legend []string
	for i, ser := range s.Series {
		legend = append(legend, th.Accent.Render(seriesFill[i%len(seriesFill)])+" "+ser.Name)
	}
	lines := []string{th.Dim.Render(fmt.Sprintf(" %s chart · %d points · ", kind, len(s.Labels))) + strings.Join(legend, "  ") + th.Dim.Render("  · g: grid")}
	lw := 0
	for _, l := range s.Labels {
		lw = max(lw, width(l))
	}
	lw = min(lw, max(8, W/3))
	bw := max(1, W-lw-14)
	for k, label := range s.Labels {
		if len(lines)+len(s.Series) > H-1 && k < len(s.Labels)-1 {
			lines = append(lines, th.Dim.Render(fmt.Sprintf("  … %d more", len(s.Labels)-k)))
			break
		}
		for i, ser := range s.Series {
			n := 0
			if top > 0 && ser.Values[k] > 0 {
				n = max(1, int(ser.Values[k]/top*float64(bw)+0.5))
			}
			name := ""
			if i == 0 {
				name = label
			}
			bar := th.Accent.Render(strings.Repeat(seriesFill[i%len(seriesFill)], n)) + strings.Repeat(" ", bw-n)
			lines = append(lines, "  "+fit(name, lw)+" "+bar+" "+fmt.Sprint(ser.Values[k]))
		}
	}
	return lines
}
