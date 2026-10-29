package ui

import (
	"strings"
	"testing"
)

func TestM4NoticesShowInMessageViewAndConsole(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	queryResult(h, "raise")
	h.want("first notice")
	h.want("second notice")
	h.press("m")
	h.want("m: back to the rows")
	h.press("m")
	var lines []string
	for _, l := range h.a.ws().console.Lines() {
		lines = append(lines, l.SQL)
	}
	if got := strings.Join(lines, "|"); !strings.Contains(got, "-- NOTICE: first notice") || !strings.Contains(got, "-- NOTICE: second notice") {
		t.Fatalf("console %q", got)
	}
	h.run("clear_console")
	if n := len(h.a.ws().console.Lines()); n != 0 {
		t.Fatalf("clear left %d lines", n)
	}
}
