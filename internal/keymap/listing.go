package keymap

import (
	"fmt"
	"strings"
)

// Listing is the text `webtuidb -keys` prints: the bindings grouped by context in a fixed context order, each group in
// keymap table order (the order GET /api/meta and both help screens use; the M14 parity test diffs them).
func Listing(prefix string, km *Map) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "prefix: %s   (every action below is: prefix, then the key; \"-n\" bindings need no prefix)\n\n", prefix)
	for _, ctx := range []string{Global, Sidebar, Grid, FilterBar, Structure, Editor, Results} {
		var rows []Binding
		for _, b := range km.All() {
			if b.Context == ctx {
				rows = append(rows, b)
			}
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "[%s]\n", ctx)
		for _, b := range rows {
			k := Display(b.Key)
			if b.NoPrefix {
				k = "-n " + k
			}
			fmt.Fprintf(&sb, "  %-26s %-10s %s\n", b.Action, k, b.Desc)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
