// completion.go: Completion in the editor: aliases, table references and Mongo method candidates.

package ui

import (
	"regexp"
	"strings"
)

// openComplete shows the autocomplete popup on demand, even with an empty prefix or autocomplete off.
func (e *Editor) openComplete() bool {
	e.ac.open = false
	if e.Complete == nil {
		return false
	}
	p, _ := e.prefixAtCursor()
	items := e.Complete(p)
	if len(items) == 0 {
		return false
	}
	if len(items) > 50 {
		items = items[:50]
	}
	e.ac.open, e.ac.items, e.ac.sel = true, items, 0
	return true
}

var aliasRe = regexp.MustCompile(`(?i)\b(?:from|join)\s+((?:"[^"]+"|\w+)(?:\.(?:"[^"]+"|\w+))?)(?:\s+(?:as\s+)?(\w+))?`)

var notAlias = map[string]bool{"where": true, "on": true, "using": true, "join": true, "inner": true, "left": true, "right": true, "full": true, "outer": true,
	"cross": true, "natural": true, "group": true, "order": true, "limit": true, "having": true, "union": true, "except": true, "intersect": true, "offset": true,
	"window": true, "set": true, "select": true, "for": true, "returning": true, "lateral": true}

// aliasesIn maps each alias in the FROM / JOIN clauses of sql to its lower-case table name, schema-qualified as
// written (shop.customers), so c. completes the columns of the table the statement names.
// shortcut: comma-separated table lists ("from a x, b y") only resolve their first table.
func aliasesIn(sql string) map[string]string {
	out := map[string]string{}
	for _, m := range aliasRe.FindAllStringSubmatch(sql, -1) {
		if m[2] != "" && !notAlias[strings.ToLower(m[2])] {
			out[strings.ToLower(m[2])] = refKey(m[1])
		}
	}
	return out
}

// refKey is the lower-case, unquoted spelling of a table reference: "Shop"."Orders" -> shop.orders.
func refKey(ref string) string { return strings.ToLower(strings.ReplaceAll(ref, `"`, "")) }

// splitRefKey splits a refKey into schema ("" when unqualified) and table.
func splitRefKey(key string) (string, string) {
	if i := strings.LastIndex(key, "."); i >= 0 {
		return key[:i], key[i+1:]
	}
	return "", key
}

// aliasAtCursor resolves qual through the aliases of the statement under the cursor.
func (t *queryTab) aliasAtCursor(qual string) string {
	text := t.ed.Text()
	if s, ok := t.ed.CurrentStatement(); ok {
		text = s.Text
	}
	if tbl, ok := aliasesIn(text)[qual]; ok {
		return tbl
	}
	return qual
}

var mongoMethods = []string{"find", "findOne", "aggregate", "count", "insertOne", "insertMany", "updateOne", "updateMany", "deleteOne", "deleteMany", "sort", "limit", "skip", "project"}

// mongoMethodCandidates suggests the shell methods after "db.<collection>." (qual is "db.<collection>").
func mongoMethodCandidates(qual string) []Candidate {
	if parts := strings.Split(qual, "."); len(parts) != 2 || !strings.EqualFold(parts[0], "db") || parts[1] == "" {
		return nil
	}
	out := make([]Candidate, len(mongoMethods))
	for i, m := range mongoMethods {
		out[i] = Candidate{Text: m, Kind: "method"}
	}
	return out
}
