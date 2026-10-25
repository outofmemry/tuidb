package sqlfmt

import "strings"

// tabular re-aligns one standard-layout statement: top-level clause keywords go into a column of width W (the
// longest keyword), left- or right-aligned, and everything else shifts right of it.
// It works on rendered lines, so format skips it for a statement with a multi-line string or comment.
func tabular(s string, o *Options) string {
	if s == "" {
		return ""
	}
	unit := "\t"
	if o.Indent != "tabs" {
		unit = strings.Repeat(" ", max(o.Width, 1))
	}
	type line struct {
		kw, rest string
		lvl      int
	}
	var ls []line
	w := 0
	for _, l := range strings.Split(s, "\n") {
		lvl := 0
		for strings.HasPrefix(l, unit) {
			l, lvl = l[len(unit):], lvl+1
		}
		x := line{rest: l, lvl: lvl}
		f := strings.Fields(l)
		if n := clauseLen(lex(l, o.Dialect), 0); n > 0 && lvl == 0 || lvl == 1 && len(f) > 0 && isCont(f[0]) {
			if lvl == 0 {
				f = f[:n]
			} else {
				f = f[:1]
			}
			x.kw = strings.Join(f, " ")
			x.rest = strings.TrimSpace(strings.TrimPrefix(l, x.kw))
			w = max(w, len(x.kw))
		}
		ls = append(ls, x)
	}
	var out []string
	for i := 0; i < len(ls); i++ {
		x := ls[i]
		pad := strings.Repeat(" ", max(w-len(x.kw), 0))
		switch {
		case x.kw == "":
			out = append(out, strings.Repeat(" ", w+1)+strings.Repeat(unit, max(x.lvl-1, 0))+x.rest)
			continue
		case o.IndentType == "tabular_right":
			x.kw = pad + x.kw
		default:
			x.kw += pad
		}
		if x.rest == "" && i+1 < len(ls) && ls[i+1].kw == "" && ls[i+1].lvl == 1 { // SELECT\n  a  ->  SELECT a
			i++
			x.rest = ls[i].rest
		}
		out = append(out, strings.TrimRight(x.kw+" "+x.rest, " "))
	}
	return strings.Join(out, "\n")
}

func isCont(w string) bool {
	switch strings.ToUpper(w) {
	case "AND", "OR", "ON":
		return true
	}
	return false
}
