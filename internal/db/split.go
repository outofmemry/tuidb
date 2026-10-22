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
	start, i, n := 0, 0, len(text)
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
		case c == '-' && i+1 < n && text[i+1] == '-', c == '#' && (dialect == "mysql" || dialect == "clickhouse"):
			for i < n && text[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && text[i+1] == '*':
			depth := 1 // PostgreSQL nests block comments; MySQL and ClickHouse do not
			i += 2
			for i < n && depth > 0 {
				switch {
				case dialect == "postgres" && text[i] == '/' && i+1 < n && text[i+1] == '*':
					depth++
					i += 2
				case text[i] == '*' && i+1 < n && text[i+1] == '/':
					depth--
					i += 2
				default:
					i++
				}
			}
		case c == '\'' || c == '"' || c == '`':
			backslash := dialect != "postgres" || dialect == "clickhouse"
			if dialect == "postgres" && c == '\'' && i > 0 && (text[i-1] == 'E' || text[i-1] == 'e') && (i < 2 || !isWord(text[i-2])) {
				backslash = true // E'...' strings treat backslash as an escape
			}
			i = skipQuoted(text, i, backslash)
		case c == '$' && dialect == "postgres":
			if tag := dollarTag(text, i); tag != "" {
				if j := strings.Index(text[i+len(tag):], tag); j >= 0 {
					i += len(tag) + j + len(tag)
					continue
				}
				i = n
				continue
			}
			i++
		case c == ';':
			flush(i)
			i++
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
