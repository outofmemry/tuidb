// json_view.go: Showing JSON and cell text in details and the grid.

package ui

import (
	"encoding/json"
	"strings"
)

// autoPrettyJSON is [table] auto_pretty_json (set by NewApp).
var autoPrettyJSON bool

// spacedJSON re-spaces compact JSON for a one-line cell: {"a":1,"b":[1,2]} -> {"a": 1, "b": [1, 2]}.
func spacedJSON(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if len(t) < 2 || (t[0] != '{' && t[0] != '[') || !json.Valid([]byte(t)) {
		return "", false
	}
	var sb strings.Builder
	in, esc := false, false
	for i := 0; i < len(t); i++ {
		c := t[i]
		sb.WriteByte(c)
		switch {
		case esc:
			esc = false
		case in && c == '\\':
			esc = true
		case c == '"':
			in = !in
		case !in && (c == ',' || c == ':') && i+1 < len(t) && t[i+1] != ' ':
			sb.WriteByte(' ')
		}
	}
	return sb.String(), true
}

// gridCellText is cellText, with JSON cells re-spaced when they fit the column (auto_pretty_json).
func gridCellText(v any, w int) string {
	s, ok := v.(string)
	if !autoPrettyJSON || !ok {
		return cellText(v)
	}
	if p, ok := spacedJSON(s); ok && width(p) <= w {
		return p
	}
	return cellText(v)
}

// detailText is cellText for the row-detail pane: JSON cells are indented over several lines (auto_pretty_json).
func detailText(v any) string {
	if s, ok := v.(string); ok && autoPrettyJSON {
		if p := prettyValue(s); p != s {
			return p
		}
	}
	return cellText(v)
}
