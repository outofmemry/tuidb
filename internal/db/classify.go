package db

import (
	"regexp"
	"strings"
)

// tok is one lexical token outside comments and string literals. Strings and quoted identifiers
// collapse to a single placeholder token so their contents can never look like keywords.
type tok struct {
	s    string
	word bool
}

// scan tokenizes text following each dialect's lexical rules, because a classifier that disagrees
// with the server about where a string or comment ends can be talked into hiding a write:
//   - postgres: ” only inside '...' (backslashes only in E'...'), nested /* */, $tag$...$tag$, # is an operator
//   - mysql: backslash escapes, # and "-- " comments, and /*! ... */ is executed code, not a comment
//   - clickhouse: backslash escapes (also inside backticks), # and -- comments
//
// Whatever the tokenizer cannot decide errs towards "code", never towards "comment".
func scan(dialect, s string) []tok {
	var out []tok
	n := len(s)
	pg, my, js := dialect == "postgres", dialect == "mysql", dialect == "mongo"
	execComment := 0 // inside /*! ... */: the text is code, only the delimiters are skipped
	for i := 0; i < n; {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case js && c == '/' && i+1 < n && s[i+1] == '/': // JavaScript: // comments, and -- is the decrement operator
			for i < n && s[i] != '\n' {
				i++
			}
		case !js && c == '-' && i+1 < n && s[i+1] == '-' && (!my || i+2 >= n || s[i+2] == ' ' || s[i+2] == '\t' || s[i+2] == '\n' || s[i+2] == '\r'):
			for i < n && s[i] != '\n' {
				i++
			}
		case c == '#' && !pg && !js:
			for i < n && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && s[i+1] == '*':
			if my && i+2 < n && (s[i+2] == '!' || (s[i+2] == 'M' && i+3 < n && s[i+3] == '!')) {
				execComment++
				i += 3
				for i < n && s[i] >= '0' && s[i] <= '9' { // version number after /*!
					i++
				}
				continue
			}
			depth := 1
			i += 2
			for i < n && depth > 0 {
				switch {
				case pg && s[i] == '/' && i+1 < n && s[i+1] == '*':
					depth++
					i += 2
				case s[i] == '*' && i+1 < n && s[i+1] == '/':
					depth--
					i += 2
				default:
					i++
				}
			}
		case c == '*' && execComment > 0 && i+1 < n && s[i+1] == '/':
			execComment--
			i += 2
		case c == '\'' || c == '"' || c == '`':
			backslash := !pg
			if pg && c == '\'' && i > 0 && (s[i-1] == 'E' || s[i-1] == 'e') && (i < 2 || !isWord(s[i-2])) {
				backslash = true // E'...' strings
			}
			if dialect == "clickhouse" {
				backslash = true
			}
			i = skipQuoted(s, i, backslash)
			out = append(out, tok{s: string(c)})
		case c == '$' && pg:
			if tag := dollarTag(s, i); tag != "" {
				if j := strings.Index(s[i+len(tag):], tag); j >= 0 {
					i += len(tag) + j + len(tag)
				} else {
					i = n
				}
				out = append(out, tok{s: "$$"})
				continue
			}
			out = append(out, tok{s: "$"})
			i++
		case isWord(c) || c > 127:
			j := i
			for j < n && (isWord(s[j]) || s[j] > 127 || pg && s[j] == '$') { // PostgreSQL identifiers may contain $ after the first char
				j++
			}
			out = append(out, tok{s: s[i:j], word: true})
			i = j
		default:
			out = append(out, tok{s: string(c)})
			i++
		}
	}
	return out
}

func upper(ts []tok) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = strings.ToUpper(t.s)
	}
	return out
}

var writeWords = map[string]bool{"INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true, "CREATE": true, "DROP": true,
	"ALTER": true, "TRUNCATE": true, "REPLACE": true, "UPSERT": true, "INTO": true, "GRANT": true, "REVOKE": true, "COPY": true,
	"EXECUTE": true, "CALL": true, "DO": true} // standalone VACUUM/SET/... are already non-SELECT first tokens

// hasWrite reports a write keyword; REPLACE( is the string function, not the statement.
func hasWrite(ts []tok) bool {
	for i, t := range ts {
		w := strings.ToUpper(t.s)
		if !t.word || !writeWords[w] {
			continue
		}
		if w == "REPLACE" && i+1 < len(ts) && ts[i+1].s == "(" {
			continue
		}
		return true
	}
	return false
}

var redisRead = map[string]bool{}

func init() {
	for _, c := range strings.Fields(`GET MGET GETRANGE STRLEN EXISTS TYPE TTL PTTL EXPIRETIME KEYS SCAN RANDOMKEY DBSIZE
		HGET HMGET HGETALL HKEYS HVALS HLEN HEXISTS HSCAN HSTRLEN LRANGE LLEN LINDEX SMEMBERS SCARD SISMEMBER SMISMEMBER
		SRANDMEMBER SSCAN SINTER SUNION SDIFF ZRANGE ZRANGEBYSCORE ZRANGEBYLEX ZREVRANGE ZREVRANGEBYSCORE ZCARD ZCOUNT
		ZSCORE ZRANK ZREVRANK ZSCAN XLEN XRANGE XREVRANGE XINFO PING INFO TIME ECHO CONFIG OBJECT MEMORY
		BITCOUNT BITPOS GETBIT PFCOUNT GEODIST GEOPOS GEOSEARCH JSON.GET CLIENT LASTSAVE`) {
		redisRead[c] = true
	}
}

// mongoSafe lists the shell methods a read-only statement may call. Anything else called with
// parentheses (runCommand, adminCommand, eval, forEach, deleteMany, ...) makes the statement
// "not read-only": an allow-list fails safe, a deny-list does not.
var mongoSafe = map[string]bool{}
var mongoHelpers = map[string]bool{} // BSON value constructors: the only calls a plain document may contain

func init() {
	for _, m := range strings.Fields(`ObjectId ISODate NumberLong NumberInt NumberDecimal Date RegExp UUID BinData Timestamp MinKey MaxKey Long Int32 Double Decimal128`) {
		mongoHelpers[m], mongoSafe[m] = true, true
	}
	for _, m := range strings.Fields(`find findOne countDocuments count estimatedDocumentCount distinct aggregate getIndexes
		getCollectionNames getCollectionInfos getCollection getName stats sort limit skip project projection toArray pretty
		explain hint collation batchSize min max maxTimeMS size itcount hasNext next tryNext allowDiskUse comment`) {
		mongoSafe[m] = true
	}
}

var (
	mongoCmd      = regexp.MustCompile(`^\s*\{\s*["']?(\w+)`)
	mongoReadCmd  = map[string]bool{"find": true, "aggregate": true, "count": true, "distinct": true, "listCollections": true, "listDatabases": true, "ping": true, "buildInfo": true, "serverStatus": true, "dbStats": true, "collStats": true, "listIndexes": true, "hello": true, "isMaster": true}
	mongoWriteStr = regexp.MustCompile(`(?i)\$(out|merge|function|accumulator|where)\b|\\u|\\x|=>|\bfunction\b|\bwhile\b|\bfor\b|\beval\b`)
	mongoShow     = regexp.MustCompile(`(?i)^\s*(show\s+(collections|dbs|databases|tables|users|roles)|use\s+\w+)\s*;?\s*$`)
)

// callsOnly reports whether every call in ts is to a name in allowed, and there is no member
// access by string (db.coll["deleteMany"](...)).
func callsOnly(ts []tok, allowed map[string]bool) bool {
	for i, t := range ts {
		next := ""
		if i+1 < len(ts) {
			next = ts[i+1].s
		}
		switch {
		case t.word && next == "(" && !allowed[t.s]:
			return false
		case t.s == "[" && i > 0 && (ts[i-1].word || ts[i-1].s == ")" || ts[i-1].s == "]" || ts[i-1].s == "\""):
			return false
		}
	}
	return true
}

// MongoPlainDocument reports whether text is a document literal with nothing executable in it:
// no calls except BSON helpers like ObjectId(), no functions, no $where. The MongoDB driver uses
// it before evaluating relaxed (shell-style) filter text.
func MongoPlainDocument(text string) bool {
	return !strings.ContainsAny(text, "`") && !mongoWriteStr.MatchString(text) && callsOnly(scan("mongo", text), mongoHelpers)
}

func mongoReadOnly(stmt string) bool {
	if mongoShow.MatchString(stmt) {
		return true
	}
	// Template literals run ${...} before any tag is called, so nothing with a backtick is read-only.
	if strings.ContainsAny(stmt, "`") {
		return false
	}
	if mongoWriteStr.MatchString(stmt) { // checked on the raw text: strings and keys count too
		return false
	}
	ts := scan("mongo", stmt)
	if m := mongoCmd.FindStringSubmatch(stmt); m != nil {
		// a command document: its first key names the command, and nothing else may execute
		return mongoReadCmd[m[1]] && callsOnly(ts, mongoHelpers)
	}
	if len(ts) == 0 {
		return true
	}
	return ts[0].s == "db" && callsOnly(ts, mongoSafe)
}

// IsReadOnly says whether a statement is SELECT/EXPLAIN/SHOW-like, which Alert/Safe Mode 2 let through.
// It is a keyword heuristic on a dialect-aware token stream: a SELECT that calls a side-effecting
// function still counts as read-only. When in doubt it answers false (the user is asked once more).
// shortcut: no real parser; replace with one if Safe Mode 2 bypasses show up in practice.
func IsReadOnly(dialect, stmt string) bool {
	switch dialect {
	case "redis":
		f := strings.Fields(strings.TrimSpace(stmt))
		if len(f) == 0 {
			return true
		}
		cmd := strings.ToUpper(f[0])
		if cmd == "CONFIG" || cmd == "CLIENT" || cmd == "MEMORY" || cmd == "OBJECT" { // only the GET/LIST/INFO subcommands
			return len(f) > 1 && (strings.EqualFold(f[1], "GET") || strings.EqualFold(f[1], "LIST") || strings.EqualFold(f[1], "INFO") ||
				strings.EqualFold(f[1], "USAGE") || strings.EqualFold(f[1], "ENCODING") || strings.EqualFold(f[1], "FREQ") || strings.EqualFold(f[1], "IDLETIME"))
		}
		return redisRead[cmd]
	case "mongo":
		return mongoReadOnly(stmt)
	}
	ts := scan(dialect, stmt)
	for len(ts) > 0 && ts[0].s == "(" {
		ts = ts[1:]
	}
	if len(ts) == 0 {
		return true
	}
	up := upper(ts)
	switch up[0] {
	case "SELECT", "VALUES", "TABLE":
		return !hasAny(up, "INTO")
	case "SHOW", "DESCRIBE", "DESC", "EXISTS":
		return true
	case "WITH":
		return !hasWrite(ts)
	case "EXPLAIN":
		if hasAny(up[1:], "ANALYZE", "ANALYSE") {
			return !hasWrite(ts[1:]) // EXPLAIN ANALYZE runs the statement; EXECUTE/CALL/DO count as writes
		}
		return true
	}
	return false
}

func hasAny(toks []string, words ...string) bool {
	for _, t := range toks {
		for _, w := range words {
			if t == w {
				return true
			}
		}
	}
	return false
}

// hasCode reports whether text has anything besides whitespace and comments (used by Split).
func hasCode(dialect, text string) bool { return len(scan(dialect, text)) > 0 }
