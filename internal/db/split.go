package db

import (
	"regexp"
	"strings"
	"unicode"
)

// Stmt is one statement of an editor buffer. Start/End are byte offsets into the buffer.
type Stmt struct {
	Text       string // trimmed, includes leading comments
	Start, End int
	Label      string // from a leading "-- name: X" (or label:/tab:) comment
}

var labelRe = regexp.MustCompile(`(?im)^\s*(?:--|#)\s*(?:name|label|tab)\s*:\s*(.+?)\s*$`)

// Split cuts text into statements for the given dialect name (postgres, mysql, clickhouse, redis, mongo).
// ';' inside quotes, comments and (postgres) dollar quotes does not split. Redis splits on lines.
// shortcut: no DELIMITER support for MySQL stored-program bodies, add if someone pastes mysqldump files.
func Split(dialect, text string) []Stmt {
	if dialect == "redis" {
		return splitLines(text)
	}
	var out []Stmt
	r := rulesFor(dialect)
	pg := r.pg
	start, i, n := 0, 0, len(text)
	term := ";" // MySQL scripts switch it with DELIMITER so routine bodies can contain ';'
	flush := func(end int) {
		seg := text[start:end]
		if hasCode(dialect, seg) {
			lead := len(seg) - len(strings.TrimLeftFunc(seg, unicode.IsSpace))
			t := strings.TrimSpace(seg)
			s := Stmt{Text: t, Start: start + lead, End: start + lead + len(t)}
			if m := labelRe.FindStringSubmatch(t); m != nil {
				s.Label = m[1]
			}
			out = append(out, s)
		}
	}
	for i < n {
		c := text[i]
		switch {
		case dialect == "mongo" && c == '/' && i+1 < n && text[i+1] == '/': // JavaScript line comment; -- is an operator there
			for i < n && text[i] != '\n' {
				i++
			}
		case dialect != "mongo" && c == '-' && i+1 < n && text[i+1] == '-' && (dialect != "mysql" || i+2 >= n || text[i+2] == ' ' || text[i+2] == '\t' || text[i+2] == '\n' || text[i+2] == '\r'),
			c == '#' && (dialect == "mysql" || dialect == "clickhouse"),
			r.slashes && c == '/' && i+1 < n && text[i+1] == '/':
			for i < n && text[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && text[i+1] == '*':
			depth := 1 // PostgreSQL nests block comments; MySQL and ClickHouse do not
			i += 2
			for i < n && depth > 0 {
				switch {
				case pg && text[i] == '/' && i+1 < n && text[i+1] == '*':
					depth++
					i += 2
				case text[i] == '*' && i+1 < n && text[i+1] == '/':
					depth--
					i += 2
				default:
					i++
				}
			}
		case dialect == "mongo" && c == '/' && jsRegexEnd(text, i) > 0: // /re/ may contain // ; or quotes
			i = jsRegexEnd(text, i)
		case c == '\'' || c == '"' || c == '`':
			backslash := r.backslash
			if c == '`' && (dialect == "mysql" || dialect == "mariadb") {
				backslash = false // only doubling escapes inside MySQL backticks
			}
			if pg && c == '\'' && i > 0 && (text[i-1] == 'E' || text[i-1] == 'e') && (i < 2 || !isWord(text[i-2])) {
				backslash = true // E'...' strings treat backslash as an escape
			}
			i = skipQuoted(text, i, backslash)
		case c == '[' && r.bracket: // SQLite [identifier]
			if j := strings.IndexByte(text[i:], ']'); j >= 0 {
				i += j + 1
			} else {
				i = n
			}
		case pg && (isWord(c) || c > 127):
			for i < n && (isWord(text[i]) || text[i] > 127 || text[i] == '$') { // a$$b$$ is one identifier, not a dollar quote
				i++
			}
		case c == '$' && r.dollar:
			if tag := dollarTag(text, i); tag != "" {
				if j := strings.Index(text[i+len(tag):], tag); j >= 0 {
					i += len(tag) + j + len(tag)
					continue
				}
				i = n
				continue
			}
			i++
		case dialect == "mysql" && atLineStart(text, i) && len(text) >= i+10 && strings.EqualFold(text[i:i+9], "DELIMITER") && (text[i+9] == ' ' || text[i+9] == '\t'):
			flush(i)
			j := i + 9
			for j < n && text[j] != '\n' {
				j++
			}
			if t := strings.TrimSpace(text[i+9 : j]); t != "" {
				term = t
			}
			i = j
			start = i
		case strings.HasPrefix(text[i:], term) && !(dialect == "cql" && inCQLBatch(text[start:i])):
			flush(i)
			i += len(term)
			start = i
		default:
			i++
		}
	}
	flush(n)
	return out
}

func splitLines(text string) []Stmt {
	var out []Stmt
	off := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		t := strings.TrimSpace(line)
		if t != "" && !strings.HasPrefix(t, "#") {
			lead := len(line) - len(strings.TrimLeftFunc(line, unicode.IsSpace))
			out = append(out, Stmt{Text: t, Start: off + lead, End: off + lead + len(t)})
		}
		off += len(line)
	}
	return out
}

// atLineStart reports whether only blanks sit between the previous newline (or the start) and i.
func atLineStart(s string, i int) bool {
	for i > 0 && (s[i-1] == ' ' || s[i-1] == '\t') {
		i--
	}
	return i == 0 || s[i-1] == '\n'
}

// jsRegexEnd returns the index after a JavaScript regex literal starting at s[i] (flags are left to the
// caller's word scan), or 0 when the '/' is a division or the literal is not closed on its line.
// shortcut: decides by the previous byte only, so `) / x /` or `} /re/` read as division; fine for shell one-liners.
func jsRegexEnd(s string, i int) int {
	j := i - 1
	for j >= 0 && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
		j--
	}
	if j >= 0 && !strings.ContainsRune("(,:=[{!&|?;+-*%<>~^", rune(s[j])) {
		return 0
	}
	class := false
	for k := i + 1; k < len(s); k++ {
		switch {
		case s[k] == '\n':
			return 0
		case s[k] == '\\':
			k++
		case s[k] == '[':
			class = true
		case s[k] == ']':
			class = false
		case s[k] == '/' && !class:
			return k + 1
		}
	}
	return 0
}

// skipQuoted returns the index after the closing quote. backslash says \x escapes the next byte.
func skipQuoted(s string, i int, backslash bool) int {
	q := s[i]
	i++
	for i < len(s) {
		switch {
		case backslash && s[i] == '\\':
			i += 2
			continue
		case s[i] == q:
			if i+1 < len(s) && s[i+1] == q { // doubled quote
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return i
}

var dollarRe = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_]*)?\$`)

func dollarTag(s string, i int) string {
	// $1 style parameters are not tags: the regexp requires $ident$ or $$.
	if i > 0 && (isWord(s[i-1])) {
		return ""
	}
	return dollarRe.FindString(s[i:])
}

func isWord(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// StatementAt picks the statement under the cursor; in trailing whitespace it picks the previous one.
func StatementAt(stmts []Stmt, offset int) (Stmt, bool) {
	var prev *Stmt
	for i := range stmts {
		s := &stmts[i]
		if offset >= s.Start && offset <= s.End {
			return *s, true
		}
		if s.End < offset {
			prev = s
		}
	}
	if prev != nil {
		return *prev, true
	}
	if len(stmts) > 0 {
		return stmts[0], true
	}
	return Stmt{}, false
}
