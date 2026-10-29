package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/db"
	"webtuidb/internal/diagram"
)

func TestM3ColorHeadersTintsOnlyHeaderRows(t *testing.T) {
	tables := []diagram.Table{{Ref: db.ObjRef{Name: "a"}, Columns: customerCols}}
	text := diagram.Render(tables, 80, "name", "")
	lines := colorHeaders(text, map[string]int{"a": 1})
	var tinted int
	for _, l := range lines {
		if strings.Contains(l, "\x1b[") {
			tinted++
		}
	}
	if tinted != 1 || !strings.Contains(stripANSI(lines[1]), "| a") {
		t.Fatalf("lines %q", lines)
	}
}
