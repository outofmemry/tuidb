// Package sqlfmt is a keyword-based SQL beautifier and minifier.
// shortcut: token-level only (no grammar): it knows clauses, CASE blocks, ON CONFLICT and FOR UPDATE, but
// not the rest of the grammar; upgrade to a real parser if that bites.
package sqlfmt

import (
	"strings"
	"unicode"
)

type Options struct {
	Indent    string // "spaces" | "tabs"
	Width     int    // spaces per level
	Uppercase bool
	Dialect   string // "mysql", "mariadb" and "clickhouse" get # comments and backslash escapes
	// IndentType: "standard" (default), "tabular_left" or "tabular_right": clause keywords share one column,
	// left- or right-aligned to the longest one, and the clause bodies line up after it.
	IndentType string
}

type tok struct {
	k byte // w word, q quoted, c line comment, b block comment, p punct/operator, ; semicolon
	s string
}

const opChars = "<>=!|+-*/%&^~"

func multiOp(s string, my bool) int {
	for _, o := range multiOps {
		if strings.HasPrefix(s, o) && !(my && o[0] != ':' && o[0] != '?') {
			return len(o)
		}
	}
	return 0
}

func isW(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c > 127
}

// dollarLen returns the length of a $tag$ opener at the start of s (0 if none).
func dollarLen(s string) int {
	i := 1
	for i < len(s) && (s[i] == '_' || s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z' || s[i] > 127 || i > 1 && s[i] >= '0' && s[i] <= '9') {
		i++
	}
	if i < len(s) && s[i] == '$' {
		return i + 1
	}
	return 0
}

var multiOps = []string{"#>>", "#>", "#-", "<@", "@>", "?|", "?&", ":="}

func lex(s, dialect string) []tok {
	var out []tok
	my := dialect == "mysql" || dialect == "mariadb" || dialect == "clickhouse"
	for i := 0; i < len(s); {
		c, st := s[i], i
		switch {
		case unicode.IsSpace(rune(c)):
			i++
			continue
		case c == '-' && strings.HasPrefix(s[i:], "--"), c == '#' && my:
			for i < len(s) && s[i] != '\n' {
				i++
			}
			out = append(out, tok{'c', strings.TrimRight(s[st:i], " \t\r")})
			continue
		case c == '/' && strings.HasPrefix(s[i:], "/*"):
			if j := strings.Index(s[i+2:], "*/"); j >= 0 {
				i += j + 4
			} else {
				i = len(s)
			}
			out = append(out, tok{'b', s[st:i]})
			continue
		case c == '\'' || c == '"' || c == '`' || c == '[':
			end := c
			if c == '[' {
				end = ']'
			}
			bs := my && end != ']' && end != '`'
			if c == '\'' { // E'', X'', N'', B'', U&'' and _utf8'' stay one token
				j := st
				for j > 0 && isW(s[j-1]) {
					j--
				}
				pre := strings.ToLower(s[j:st])
				if j == st && st >= 2 && s[st-1] == '&' && (s[st-2] == 'U' || s[st-2] == 'u') && (st < 3 || !isW(s[st-3])) {
					if n := len(out); n >= 2 && out[n-1].s == "&" && out[n-2].k == 'w' {
						out, st = out[:n-2], st-2
					}
				} else if j < st && len(out) > 0 && out[len(out)-1].s == s[j:st] && (len(pre) == 1 && strings.Contains("enxb", pre) || len(pre) > 1 && pre[0] == '_') {
					out, st = out[:len(out)-1], j
					bs = bs || pre == "e"
				}
			}
			for i++; i < len(s); i++ {
				if bs && s[i] == '\\' {
					i++
					continue
				}
				if s[i] == end {
					if end != ']' && i+1 < len(s) && s[i+1] == end { // doubled quote
						i++
						continue
					}
					i++
					break
				}
			}
			i = min(i, len(s)) // a trailing backslash skipped past the end
			out = append(out, tok{'q', s[st:i]})
			continue
		case c == '$' && (i == 0 || !isW(s[i-1])) && dollarLen(s[i:]) > 0 && strings.Contains(s[i+dollarLen(s[i:]):], s[i:i+dollarLen(s[i:])]):
			tag := s[i : i+dollarLen(s[i:])]
			i += len(tag) + strings.Index(s[i+len(tag):], tag) + len(tag)
			out = append(out, tok{'q', s[st:i]})
			continue
		case c == '@' && strings.HasPrefix(s[i:], "@@"):
			for i += 2; i < len(s) && (isW(s[i]) || s[i] == '.'); i++ {
			}
			out = append(out, tok{'w', s[st:i]})
			continue
		case multiOp(s[i:], my) > 0:
			i += multiOp(s[i:], my)
		case c == ';':
			i++
			out = append(out, tok{';', ";"})
			continue
		case strings.HasPrefix(s[i:], "::"):
			i += 2
		case strings.IndexByte(opChars, c) >= 0:
			for i < len(s) && strings.IndexByte(opChars, s[i]) >= 0 && !strings.HasPrefix(s[i:], "--") {
				i++
			}
		case isW(c) || (c == ':' || c == '@' || c == '?') && i+1 < len(s) && isW(s[i+1]) || c == '?':
			i++
			for i < len(s) && isW(s[i]) {
				i++
			}
			out = append(out, tok{'w', s[st:i]})
			continue
		default:
			i++
		}
		out = append(out, tok{'p', s[st:i]})
	}
	return out
}

var kw = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`SELECT FROM WHERE GROUP BY HAVING ORDER LIMIT OFFSET INSERT INTO VALUES UPDATE SET DELETE
		JOIN LEFT RIGHT INNER OUTER FULL CROSS NATURAL ON UNION ALL INTERSECT EXCEPT AND OR NOT IN IS NULL AS BETWEEN LIKE
		ILIKE DISTINCT ASC DESC CASE WHEN THEN ELSE END EXISTS USING WITH RETURNING OVER PARTITION CONFLICT DO NOTHING FOR`) {
		kw[w] = true
	}
}

var phrases = []string{"ON CONFLICT", "DO UPDATE SET", "DO NOTHING", "FOR UPDATE", "FOR NO KEY UPDATE", "FOR SHARE", "FOR KEY SHARE", "GROUP BY", "ORDER BY", "INSERT INTO", "DELETE FROM", "UNION ALL", "UNION", "INTERSECT", "EXCEPT",
	"SELECT", "FROM", "WHERE", "HAVING", "LIMIT", "OFFSET", "VALUES", "UPDATE", "SET", "ON", "RETURNING"}

var joinMods = map[string]bool{"NATURAL": true, "LEFT": true, "RIGHT": true, "FULL": true, "INNER": true, "CROSS": true, "OUTER": true, "JOIN": true}

// clauseLen returns how many tokens at i form a clause keyword (0 if none).
func clauseLen(t []tok, i int) int {
	up := func(j int) string {
		if j < len(t) && t[j].k == 'w' {
			return strings.ToUpper(t[j].s)
		}
		return ""
	}
	if joinMods[up(i)] {
		for j := i; joinMods[up(j)]; j++ {
			if up(j) == "JOIN" {
				return j - i + 1
			}
		}
		return 0
	}
	for _, p := range phrases {
		f := strings.Fields(p)
		ok := true
		for j, w := range f {
			ok = ok && up(i+j) == w
		}
		if ok {
			return len(f)
		}
	}
	return 0
}

func space(p, t tok) bool {
	switch {
	case t.k == ';' || t.k == 'p' && strings.Contains(",.)}", t.s) && len(t.s) == 1 || t.s == "::":
		return false
	case p.k == 'p' && (p.s == "(" || p.s == "." || p.s == "{" || p.s == "::"):
		return false
	case t.s == "(":
		return !(p.k == 'w' && !kw[strings.ToUpper(p.s)] || p.k == 'p' && p.s == "}")
	case t.s == "{":
		return !(p.k == 'w' && strings.HasSuffix(p.s, "$"))
	case t.k == 'q' && t.s[0] == '[':
		return p.k == 'p' && p.s != ")"
	}
	return true
}

func cased(x tok, o *Options) string {
	if o.Uppercase {
		return strings.ToUpper(x.s)
	}
	return x.s
}

type frame struct {
	lvl               int
	inline, lst, kase bool // kase: inside CASE ... END, laid out as a block
}

// render lays out one statement. o == nil means minified.
func render(t []tok, o *Options) string {
	ind := ""
	if o != nil {
		ind = "\t"
		if o.Indent != "tabs" {
			ind = strings.Repeat(" ", max(o.Width, 1))
		}
	}
	var b strings.Builder
	st := []frame{{}}
	pend, cur, between := -1, 0, false
	var prev, prev2 tok
	emit := func(x tok, s string) {
		switch {
		case pend >= 0 && b.Len() > 0:
			b.WriteString("\n" + strings.Repeat(ind, pend))
			cur = pend
		case pend >= 0:
			b.WriteString(strings.Repeat(ind, pend))
			cur = pend
		case b.Len() > 0 && (space(prev, x) || x.s == "(" && strings.EqualFold(prev2.s, "INTO")): // INSERT INTO t (cols)
			b.WriteByte(' ')
		}
		pend = -1
		b.WriteString(s)
		prev2, prev = prev, x
	}
	for i := 0; i < len(t); i++ {
		x, top := t[i], &st[len(st)-1]
		up := strings.ToUpper(x.s)
		s := x.s
		if o != nil && o.Uppercase && x.k == 'w' && kw[up] {
			s = up
		}
		switch {
		case x.k == 'c' && o == nil:
		case x.k == 'c':
			emit(x, s)
			pend = cur
		case o == nil:
			if x.k == 'p' && x.s == "(" {
				st = append(st, frame{})
			}
			emit(x, s)
		case x.k == 'w' && !top.inline && clauseLen(t, i) > 0:
			n := clauseLen(t, i)
			pend = top.lvl
			if up == "ON" && n == 1 { // a join condition; ON CONFLICT is a clause of its own
				pend++
			}
			for j := 0; j < n; j++ {
				emit(t[i+j], cased(t[i+j], o))
			}
			i += n - 1
			last := strings.ToUpper(t[i].s)
			top.lst = up == "SELECT" || last == "SET"
			if up == "SELECT" && i+1 < len(t) && strings.EqualFold(t[i+1].s, "DISTINCT") {
				i++
				emit(t[i], cased(t[i], o))
			}
			if top.lst {
				pend = top.lvl + 1
			}
		case x.k == 'w' && up == "CASE" && !top.inline:
			emit(x, s)
			st = append(st, frame{lvl: cur + 1, kase: true})
		case x.k == 'w' && (up == "WHEN" || up == "ELSE") && top.kase:
			pend = top.lvl
			emit(x, s)
		case x.k == 'w' && up == "END" && top.kase:
			pend = top.lvl - 1
			st = st[:len(st)-1]
			emit(x, s)
		case x.k == 'w' && !top.inline && !top.kase && (up == "AND" || up == "OR"):
			if up == "AND" && between {
				between = false
			} else {
				pend = top.lvl + 1
			}
			emit(x, s)
		case x.k == 'w' && up == "BETWEEN":
			between = true
			emit(x, s)
		case x.s == "," && x.k == 'p' && top.lst && !top.inline:
			emit(x, s)
			pend = top.lvl + 1
		case x.s == "(" && x.k == 'p':
			sub := i+1 < len(t) && strings.EqualFold(t[i+1].s, "SELECT")
			emit(x, s)
			st = append(st, frame{lvl: top.lvl + 1, inline: !sub})
		case x.s == ")" && x.k == 'p':
			if len(st) > 1 {
				if !top.inline {
					pend = top.lvl - 1
				}
				st = st[:len(st)-1]
			}
			emit(x, s)
		default:
			emit(x, s)
		}
	}
	return b.String()
}

func format(sql string, o *Options, dialect string) string {
	var out []string
	var cur []tok
	flush := func(semi bool) {
		s := render(cur, o)
		if s != "" && o != nil && strings.HasPrefix(o.IndentType, "tabular_") && !multiline(cur) {
			s = tabular(s, o)
		}
		if len(cur) == 0 || s == "" {
			cur = nil
			return
		}
		if semi {
			if cur[len(cur)-1].k == 'c' && o != nil {
				s += "\n"
			}
			s += ";"
		}
		out = append(out, s)
		cur = nil
	}
	for _, x := range lex(sql, dialect) {
		if x.k == ';' {
			flush(true)
		} else {
			cur = append(cur, x)
		}
	}
	flush(false)
	if o == nil {
		return strings.Join(out, "\n")
	}
	return strings.Join(out, "\n\n")
}

// multiline: tabular shifts rendered lines, which would change a multi-line string or block comment.
func multiline(t []tok) bool {
	for _, x := range t {
		if (x.k == 'q' || x.k == 'b') && strings.Contains(x.s, "\n") {
			return true
		}
	}
	return false
}

func Beautify(sql string, o Options) string { return format(sql, &o, o.Dialect) }

// Uglify minifies sql; dialect is as in Options.Dialect.
func Uglify(sql, dialect string) string { return format(sql, nil, dialect) }
