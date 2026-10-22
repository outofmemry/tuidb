package ui

import "strings"

type tokKind byte

const (
	tkText tokKind = iota
	tkKeyword
	tkString
	tkComment
	tkNumber
)

var sqlKeywords = map[string]bool{}
var redisCommands = map[string]bool{}
var mongoWords = map[string]bool{}

func init() {
	for _, k := range strings.Fields(`SELECT FROM WHERE AND OR NOT NULL IS IN LIKE ILIKE BETWEEN JOIN LEFT RIGHT INNER OUTER FULL CROSS NATURAL
		ON USING GROUP BY ORDER HAVING LIMIT OFFSET UNION ALL DISTINCT AS INSERT INTO VALUES UPDATE SET DELETE CREATE ALTER DROP TABLE VIEW
		INDEX UNIQUE PRIMARY KEY FOREIGN REFERENCES CONSTRAINT DEFAULT CASE WHEN THEN ELSE END EXISTS ASC DESC WITH RECURSIVE RETURNING
		TRUNCATE BEGIN COMMIT ROLLBACK EXPLAIN ANALYZE SHOW DESCRIBE USE DATABASE SCHEMA ADD COLUMN RENAME TO TRIGGER FUNCTION PROCEDURE
		IF TRUE FALSE CAST OVER PARTITION INTERSECT EXCEPT ANY SOME ESCAPE COLLATE TEMPORARY TEMP MATERIALIZED SEQUENCE CASCADE RESTRICT
		GRANT REVOKE VACUUM REPLACE IGNORE ENGINE FINAL PREWHERE FORMAT SETTINGS ARRAY LATERAL FETCH FIRST NEXT ROWS ONLY FOR SHARE LOCK`) {
		sqlKeywords[k] = true
	}
	for _, k := range strings.Fields(`GET SET DEL MGET MSET INCR DECR INCRBY EXPIRE TTL PTTL PERSIST KEYS SCAN TYPE EXISTS RENAME COPY
		HGET HSET HGETALL HDEL HKEYS HVALS HLEN HMGET HINCRBY LPUSH RPUSH LPOP RPOP LRANGE LLEN LINDEX LSET LREM SADD SREM SMEMBERS SCARD
		SISMEMBER ZADD ZREM ZRANGE ZSCORE ZCARD ZRANK XADD XRANGE XLEN XREAD PUBLISH SUBSCRIBE INFO PING DBSIZE FLUSHDB FLUSHALL SELECT
		CONFIG CLIENT MULTI EXEC DISCARD WATCH EVAL SCRIPT OBJECT MEMORY SETEX SETNX APPEND STRLEN GETSET`) {
		redisCommands[k] = true
	}
	for _, k := range strings.Fields(`db find findOne aggregate countDocuments count distinct insertOne insertMany updateOne updateMany replaceOne
		deleteOne deleteMany createIndex dropIndex drop getIndexes getCollection getCollectionNames sort limit skip project toArray show use
		ObjectId ISODate NumberLong NumberInt NumberDecimal true false null runCommand`) {
		mongoWords[k] = true
	}
}

// lexLines classifies every rune of every line. Block comments and strings may span lines.
func lexLines(dialect string, lines []string) [][]tokKind {
	out := make([][]tokKind, len(lines))
	inBlock := false
	var quote rune // open multi-line string quote
	for li, line := range lines {
		r := []rune(line)
		k := make([]tokKind, len(r))
		i := 0
		firstWord := true
		for i < len(r) {
			c := r[i]
			switch {
			case inBlock:
				k[i] = tkComment
				if c == '*' && i+1 < len(r) && r[i+1] == '/' {
					k[i+1] = tkComment
					i++
					inBlock = false
				}
				i++
			case quote != 0:
				k[i] = tkString
				if c == '\\' && dialect != "postgres" && i+1 < len(r) {
					k[i+1] = tkString
					i += 2
					continue
				}
				if c == quote {
					quote = 0
				}
				i++
			case c == '-' && i+1 < len(r) && r[i+1] == '-', c == '#' && dialect != "postgres" && dialect != "mongo", c == '/' && dialect == "mongo" && i+1 < len(r) && r[i+1] == '/':
				for ; i < len(r); i++ {
					k[i] = tkComment
				}
			case c == '/' && i+1 < len(r) && r[i+1] == '*':
				inBlock = true
				k[i], k[i+1] = tkComment, tkComment
				i += 2
			case c == '\'' || c == '"' && dialect != "postgres" || c == '`' && dialect != "postgres":
				quote = c
				k[i] = tkString
				i++
			case c >= '0' && c <= '9' && (i == 0 || !isIdentRune(r[i-1])):
				for ; i < len(r) && (r[i] >= '0' && r[i] <= '9' || r[i] == '.'); i++ {
					k[i] = tkNumber
				}
			case isIdentRune(c):
				j := i
				for j < len(r) && isIdentRune(r[j]) {
					j++
				}
				w := string(r[i:j])
				kw := false
				switch dialect {
				case "redis":
					kw = firstWord && redisCommands[strings.ToUpper(w)]
				case "mongo":
					kw = mongoWords[w]
				default:
					kw = sqlKeywords[strings.ToUpper(w)]
				}
				if kw {
					for x := i; x < j; x++ {
						k[x] = tkKeyword
					}
				}
				firstWord = false
				i = j
			default:
				i++
			}
		}
		out[li] = k
		if quote != 0 && dialect == "redis" { // redis commands are one line each
			quote = 0
		}
	}
	return out
}

func isIdentRune(c rune) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c > 127
}
