package ui

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"webtuidb/internal/db"
)

// fit truncates s to w display cells with an ellipsis, or right-pads it.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	n := ansi.StringWidth(s)
	if n > w {
		return ansi.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", w-n)
}

func padLeft(s string, w int) string {
	if n := ansi.StringWidth(s); n < w {
		return strings.Repeat(" ", w-n) + s
	}
	return s
}

func width(s string) int { return ansi.StringWidth(s) }

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return max(lo, min(v, hi))
}

// oneLine makes a value safe for a single grid cell.
func oneLine(s string) string {
	if !strings.ContainsAny(s, "\r\n\t") {
		return s
	}
	return strings.NewReplacer("\r\n", "⏎", "\n", "⏎", "\r", "⏎", "\t", "→").Replace(s)
}

// cellText is how a value is shown in the grid.
func cellText(v any) string {
	if v == nil {
		return "NULL"
	}
	switch x := v.(type) {
	case string:
		return oneLine(x)
	default:
		if x == db.Null {
			return "NULL"
		}
		if x == db.Default {
			return "DEFAULT"
		}
		if x == db.Empty {
			return ""
		}
		if e, ok := x.(db.Expr); ok {
			return string(e)
		}
		return oneLine(db.FormatValue(v))
	}
}

func isSpecial(v any) bool {
	if v == nil || v == db.Null || v == db.Default || v == db.Empty {
		return true
	}
	_, isExpr := v.(db.Expr)
	return isExpr
}

// fuzzy scores pattern against s (case-insensitive subsequence). Higher is better. Empty pattern matches all.
func fuzzy(pattern, s string) (int, bool) {
	if pattern == "" {
		return 0, true
	}
	p, t := []rune(strings.ToLower(pattern)), []rune(strings.ToLower(s))
	score, pi, last := 0, 0, -2
	for ti := 0; ti < len(t) && pi < len(p); ti++ {
		if t[ti] != p[pi] {
			continue
		}
		score += 1
		if ti == last+1 {
			score += 3 // consecutive
		}
		if ti == 0 || t[ti-1] == '_' || t[ti-1] == '.' || t[ti-1] == ' ' || t[ti-1] == ':' {
			score += 2 // word start
		}
		last = ti
		pi++
	}
	if pi < len(p) {
		return 0, false
	}
	return score - len(t)/8, true
}

// matchName implements the sidebar/Open Anything matching: fuzzy or substring, with ^ and $ anchors.
func matchName(pattern, name string, fuzzyMode bool) (int, bool) {
	if strings.HasPrefix(pattern, "^") || strings.HasSuffix(pattern, "$") {
		l := strings.ToLower(name)
		pp := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(pattern, "^"), "$"))
		switch {
		case strings.HasPrefix(pattern, "^") && strings.HasSuffix(pattern, "$"):
			return 10, l == pp
		case strings.HasPrefix(pattern, "^"):
			return 10, strings.HasPrefix(l, pp)
		default:
			return 10, strings.HasSuffix(l, pp)
		}
	}
	if fuzzyMode {
		return fuzzy(pattern, name)
	}
	return 0, strings.Contains(strings.ToLower(name), strings.ToLower(pattern))
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// wrapText hard-wraps s to width w (display cells) keeping explicit newlines.
func wrapText(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			out = append(out, "")
			continue
		}
		for width(line) > w {
			cut := ansi.Truncate(line, w, "")
			out = append(out, cut)
			line = line[len(cut):]
		}
		out = append(out, line)
	}
	return out
}

func stripANSI(s string) string { return ansi.Strip(s) }

func indentJSON(s string) (string, bool) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		return "", false
	}
	return buf.String(), true
}

func writeFile(path, text string) error {
	return writeAtomic(path, func(w io.Writer) error { _, err := io.WriteString(w, text); return err })
}

// writeAtomic fills a temp file next to path (mode 0600) and renames it over path, so a failed write
// never destroys the previous file.
func writeAtomic(path string, fill func(io.Writer) error) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".webtuidb-*")
	if err != nil {
		return err
	}
	err = fill(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

// ifNoQuery runs run, after an overwrite confirm when a saved query of that name exists in folder.
// shortcut: matches by name via List; use store.SaveIfAbsent once it exists.
func (a *App) ifNoQuery(folder, name string, run func() tea.Cmd) tea.Cmd {
	qs, _ := a.d.Queries.List()
	for _, q := range qs {
		if q.Name == name && q.Folder == folder && !q.Shared {
			a.push(&confirmDlg{title: "Overwrite saved query " + name + "?", yes: "Overwrite", danger: true, onYes: run})
			return nil
		}
	}
	return run()
}

// ifFree runs run, after an overwrite confirm when any of paths already exists.
func (a *App) ifFree(run func() tea.Cmd, paths ...string) tea.Cmd {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			a.push(&confirmDlg{title: "Overwrite " + p + "?", yes: "Overwrite", danger: true, onYes: run})
			return nil
		}
	}
	return run()
}
