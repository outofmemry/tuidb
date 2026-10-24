package db

import "strings"

// CQL is Cassandra's SQL spelling: "double-quoted" identifiers, single-quoted strings without backslash escapes,
// ? placeholders, no ILIKE. NowExpr is what a timestamp column accepts for "now".
var CQL = Dialect{Name: "cql", Quote: quoteWith(`"`), Param: func(int) string { return "?" }, NowExpr: "toTimestamp(now())",
	CastText: func(e string) string { return "CAST(" + e + " AS text)" }}

func init() {
	RegisterDialect(CQL)
	RegisterReadOnly("cql", cqlReadOnly)
}

// cqlReadOnly is an allow-list: only SELECT, DESCRIBE and LIST are read-only. Everything else, including
// BEGIN BATCH (one statement to Split, many writes to the server), USE and the DDL, asks first.
func cqlReadOnly(stmt string) bool {
	ts := scan("cql", stmt)
	if len(ts) == 0 {
		return true
	}
	switch strings.ToUpper(ts[0].s) {
	case "SELECT", "DESCRIBE", "DESC", "LIST":
		return true
	}
	return false
}

// inCQLBatch reports whether seg (a statement cut at a ';') is a BEGIN [UNLOGGED|COUNTER] BATCH whose APPLY BATCH
// has not arrived yet: the ';' ends one statement inside the batch, not the batch.
func inCQLBatch(seg string) bool {
	up := upper(scan("cql", seg))
	n := len(up)
	return n > 0 && up[0] == "BEGIN" && !(n >= 2 && up[n-2] == "APPLY" && up[n-1] == "BATCH")
}
