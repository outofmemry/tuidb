package db

import (
	"regexp"
	"strconv"
	"strings"
)

// ParamFormat is one of "colon" (:name), "dollar" ($name), "at" (@name), "brace" (${name}),
// "mustache" ({{name}}) or "question" (positional ?).
type ParamFormat string

// shortcut: no $tag$ dollar quote awareness, so such a literal can hide or invent a param; borrow Split's lexer rules if that shows up.
// scanParams calls fn for every parameter occurrence outside strings and comments.
func scanParams(sql string, f ParamFormat, dialect string, fn func(start, end int, name string)) {
	n, q := len(sql), 0
	bs := dialect == "mysql" || dialect == "mariadb" || dialect == "clickhouse"
	wc := func(b byte) bool { return isWord(b) || b > 127 }
	word := func(i int) int {
		for i < n && wc(sql[i]) {
			i++
		}
		return i
	}
	fresh := func(i int) bool { return i == 0 || !wc(sql[i-1]) }
	letter := func(i int) bool { return i < n && (sql[i] == '_' || alpha(sql[i])) }
	for i := 0; i < n; {
		c := sql[i]
		switch {
		case c == '-' && strings.HasPrefix(sql[i:], "--"):
			for i < n && sql[i] != '\n' {
				i++
			}
		case c == '/' && strings.HasPrefix(sql[i:], "/*"):
			if j := strings.Index(sql[i+2:], "*/"); j >= 0 {
				i += j + 4
			} else {
				i = n
			}
		case c == '\'' || c == '"' || c == '`':
			for i++; i < n; i++ {
				if bs && c != '`' && sql[i] == '\\' {
					i++
					continue
				}
				if sql[i] == c {
					if i+1 < n && sql[i+1] == c {
						i++
						continue
					}
					break
				}
			}
			i++
		case f == "colon" && c == ':' && i+1 < n && sql[i+1] == ':':
			i += 2
		case f == "colon" && c == ':' && letter(i+1) && fresh(i), f == "dollar" && c == '$' && letter(i+1) && fresh(i) && (i == 0 || sql[i-1] != '$'),
			f == "at" && c == '@' && letter(i+1) && fresh(i) && (i == 0 || sql[i-1] != '@'):
			e := word(i + 1)
			fn(i, e, sql[i+1:e])
			i = e
		case f == "question" && c == '?':
			q++
			fn(i, i+1, strconv.Itoa(q))
			i++
		case f == "brace" && strings.HasPrefix(sql[i:], "${"), f == "mustache" && strings.HasPrefix(sql[i:], "{{"):
			open, end := 2, "}"
			if f == "mustache" {
				end = "}}"
			}
			j := strings.Index(sql[i+open:], end)
			if j < 0 {
				i += open
				break
			}
			if name := strings.TrimSpace(sql[i+open : i+open+j]); name != "" && word(i+open+strings.Index(sql[i+open:], name)) == i+open+strings.Index(sql[i+open:], name)+len(name) {
				fn(i, i+open+j+len(end), name)
			}
			i += open + j + len(end)
		default:
			i++
		}
	}
}

func alpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c > 127 }

// Params returns the distinct parameter names in order of appearance ("1", "2", ... for question).
func Params(sql string, f ParamFormat, dialect string) []string {
	var out []string
	seen := map[string]bool{}
	scanParams(sql, f, dialect, func(_, _ int, name string) {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	})
	return out
}

var numRe = regexp.MustCompile(`^[+-]?\d+(\.\d+)?$`)

// Bind replaces each parameter with a literal built from vals: numbers and NULL stay bare, anything else
// is single-quoted. A parameter with no entry in vals is left as written.
func Bind(sql string, f ParamFormat, vals map[string]string, dialect string) string {
	var b strings.Builder
	last := 0
	scanParams(sql, f, dialect, func(s, e int, name string) {
		v, ok := vals[name]
		if !ok {
			return
		}
		if (!numRe.MatchString(v) || len(v) > 15 || len(v) > 1 && v[0] == '0' && v[1] != '.') && !strings.EqualFold(v, "null") { // long ids and 007 would lose digits as numbers
			if dialect == "mysql" || dialect == "mariadb" || dialect == "clickhouse" {
				v = strings.ReplaceAll(v, `\`, `\\`)
			}
			v = "'" + strings.ReplaceAll(v, "'", "''") + "'"
		}
		if numRe.MatchString(v) && v[0] == '-' {
			v = "(" + v + ")" // "a -$n" must not become the comment "a --5"
		}
		b.WriteString(sql[last:s] + v)
		last = e
	})
	return b.String() + sql[last:]
}
