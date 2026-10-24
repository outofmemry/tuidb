package db

import "strings"

// PartiQL on DynamoDB: only SELECT reads. INSERT/UPDATE/DELETE, EXISTS (transaction-only) and the
// driver's CREATE/ALTER/DROP/TRUNCATE TABLE are writes. A ';' followed by more code means the text is
// not one statement, so it is not trusted either.
func init() {
	RegisterReadOnly("partiql", func(stmt string) bool {
		ts := scan("partiql", stmt)
		if len(ts) == 0 {
			return true
		}
		if !strings.EqualFold(ts[0].s, "SELECT") {
			return false
		}
		for len(ts) > 0 && ts[len(ts)-1].s == ";" {
			ts = ts[:len(ts)-1]
		}
		for _, t := range ts {
			if t.s == ";" {
				return false
			}
		}
		return true
	})
}
