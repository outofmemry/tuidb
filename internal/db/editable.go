package db

import (
	"strings"
	"unicode"
)

// StatementRangeAt is the byte range [start, end) of the statement of text under offset (in trailing
// whitespace, the previous one), cut by Split; ok is false when text holds no statement.
// (StatementAt, taking already-split statements, predates it and keeps its signature.)
func StatementRangeAt(dialect, text string, offset int) (start, end int, ok bool) {
	s, ok := StatementAt(Split(dialect, text), offset)
	return s.Start, s.End, ok
}

// EditableSelect reports whether stmt is a single SELECT from one table (optionally aliased) whose select list
// is * or plain column references (an alias only if it repeats the column name), with no JOIN, GROUP BY, DISTINCT, UNION, HAVING, window
// function or subquery in the select list or FROM. WHERE, ORDER BY and LIMIT may be anything.
// The caller still has to check the primary key columns are in the select list.
func EditableSelect(stmt, dialect string) (schema, table string, ok bool) {
	toks, good := lexSelect(stmt, dialect)
	if !good || len(toks) == 0 || !strings.EqualFold(toks[0].s, "select") || toks[0].quoted {
		return
	}
	// split into select list, FROM clause and tail at depth 0
	from, depth := -1, 0
	for i, t := range toks {
		if t.quoted {
			continue
		}
		switch {
		case t.s == "(":
			depth++
		case t.s == ")":
			depth--
		case depth == 0:
			switch strings.ToLower(t.s) {
			case "union", "intersect", "except", "group", "having", "window", "into", "distinct", "join", "with", "for", "returning":
				return
			case "from":
				if from < 0 {
					from = i
				}
			}
		}
	}
	if from < 2 || depth != 0 {
		return
	}
	tail := len(toks)
	for i := from + 1; i < len(toks); i++ {
		if t := toks[i]; !t.quoted && (t.s == "(" || isOneOf(t.s, "where", "order", "limit", "offset", "fetch")) {
			if t.s == "(" {
				return // a subquery or function in FROM
			}
			tail = i
			break
		}
	}
	// select list: comma separated [qualifier.]column [[AS] alias] or * or qualifier.*
	items, cur := [][]sqlTok{}, []sqlTok{}
	for _, t := range toks[1:from] {
		if !t.quoted && t.s == "," {
			items, cur = append(items, cur), nil
			continue
		}
		cur = append(cur, t)
	}
	items = append(items, cur)
	for _, it := range items {
		if !plainColumn(it) {
			return
		}
	}
	// FROM: name[.name] [[AS] alias]
	ref := toks[from+1 : tail]
	i := 0
	var parts []string
	for {
		if i >= len(ref) || !ref[i].ident() {
			return
		}
		parts = append(parts, ref[i].s)
		i++
		if i+1 < len(ref) && !ref[i].quoted && ref[i].s == "." {
			i++
			continue
		}
		break
	}
	if len(parts) > 2 {
		return
	}
	if i < len(ref) && !ref[i].quoted && strings.EqualFold(ref[i].s, "as") {
		i++
	}
	if i < len(ref) {
		if !ref[i].ident() {
			return
		}
		i++
	}
	if i != len(ref) {
		return
	}
	if len(parts) == 2 {
		return parts[0], parts[1], true
	}
	return "", parts[0], true
}

func isOneOf(s string, words ...string) bool {
	for _, w := range words {
		if strings.EqualFold(s, w) {
			return true
		}
	}
	return false
}

type sqlTok struct {
	s      string
	quoted bool // a quoted identifier (s is unquoted) or a string literal
	str    bool
}

func (t sqlTok) ident() bool {
	if t.str {
		return false
	}
	if t.quoted {
		return true
	}
	r := []rune(t.s)
	return len(r) > 0 && (unicode.IsLetter(r[0]) || r[0] == '_') && !isOneOf(t.s, "where", "order", "limit", "offset", "fetch", "on", "using", "natural", "cross", "inner", "left", "right", "full", "outer")
}

// plainColumn: * | name | q.name | q.* | name [AS] alias (q may be schema.table, so up to three parts).
func plainColumn(it []sqlTok) bool {
	i := 0
	if len(it) == 1 && !it[0].quoted && it[0].s == "*" {
		return true
	}
	for n := 0; ; n++ {
		if i >= len(it) || n > 2 {
			return false
		}
		if !it[i].quoted && it[i].s == "*" && n > 0 {
			return i == len(it)-1
		}
		if !it[i].ident() {
			return false
		}
		i++
		if i+1 < len(it) && !it[i].quoted && it[i].s == "." {
			i++
			continue
		}
		break
	}
	// an alias is allowed only when it repeats the column's own name: the web edits by result column name,
	// so `name AS id` would write the wrong column.
	col := it[i-1]
	same := func(a sqlTok) bool { return a.ident() && strings.EqualFold(a.s, col.s) && col.s != "*" }
	if i < len(it) && !it[i].quoted && strings.EqualFold(it[i].s, "as") {
		i++
		return i == len(it)-1 && same(it[i])
	}
	return i == len(it) || i == len(it)-1 && same(it[i])
}

// lexSelect splits SQL into words, quoted identifiers, string literals and single punctuation bytes, dropping
// comments and a trailing ';'. ok is false on an unterminated quote or a second statement.
// On MySQL/MariaDB a "double quoted" token is a string (default sql_mode), not an identifier.
func lexSelect(s, dialect string) (toks []sqlTok, ok bool) {
	dqString := dialect == "mysql" || dialect == "mariadb"
	i, n := 0, len(s)
	for i < n {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '-' && i+1 < n && s[i+1] == '-':
			for i < n && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && s[i+1] == '*':
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				return nil, false
			}
			i += j + 4
		case c == '\'' || c == '"' || c == '`':
			j := skipQuoted(s, i, false)
			if j > n || j <= i+1 || s[j-1] != c {
				return nil, false
			}
			body := strings.ReplaceAll(s[i+1:j-1], string(c)+string(c), string(c))
			toks = append(toks, sqlTok{s: body, quoted: true, str: c == '\'' || c == '"' && dqString})
			i = j
		case c == ';':
			if strings.TrimSpace(strings.TrimLeft(s[i:], "; \t\r\n")) != "" {
				return nil, false
			}
			return toks, true
		case isWord(c) || c > 127 || c == '$':
			j := i
			for j < n && (isWord(s[j]) || s[j] > 127 || s[j] == '$') {
				j++
			}
			toks = append(toks, sqlTok{s: s[i:j]})
			i = j
		default:
			toks = append(toks, sqlTok{s: string(c)})
			i++
		}
	}
	return toks, true
}
