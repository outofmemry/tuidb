package db

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Dialect carries the per-engine SQL spelling the shared planner/filter code needs.
type Dialect struct {
	Name     string
	Quote    func(ident string) string
	Param    func(n int) string // 1-based placeholder
	ILike    bool               // supports ILIKE
	Mutate   bool               // ClickHouse: UPDATE/DELETE are ALTER TABLE ... mutations
	NowExpr  string
	CastText func(expr string) string // for Any-column filters
	// LikeCast: LIKE/ILIKE only accept text, so non-text columns are cast first (PostgreSQL, ClickHouse).
	LikeCast bool
	// BackslashEscapes: string literals read a backslash as an escape (MySQL, ClickHouse).
	BackslashEscapes bool
	// Funcs: Contains / Has prefix render as contains(col, ?) / begins_with(col, ?) (PartiQL has no LIKE).
	Funcs bool
}

func quoteWith(q string) func(string) string {
	return func(s string) string { return q + strings.ReplaceAll(s, q, q+q) + q }
}

// chQuote: ClickHouse reads a backslash as an escape inside backticks, so it must be escaped too,
// otherwise a name ending in a backslash would swallow the closing quote.
func chQuote(s string) string {
	return "`" + strings.NewReplacer(`\`, `\\`, "`", "\\`").Replace(s) + "`"
}

var (
	PG         = Dialect{Name: "postgres", Quote: quoteWith(`"`), Param: func(n int) string { return "$" + strconv.Itoa(n) }, ILike: true, NowExpr: "NOW()", LikeCast: true, CastText: func(e string) string { return "CAST(" + e + " AS TEXT)" }}
	MySQL      = Dialect{Name: "mysql", Quote: quoteWith("`"), Param: func(int) string { return "?" }, NowExpr: "NOW()", BackslashEscapes: true, CastText: func(e string) string { return "CAST(" + e + " AS CHAR)" }}
	ClickHouse = Dialect{Name: "clickhouse", Quote: chQuote, Param: func(int) string { return "?" }, Mutate: true, NowExpr: "now()", LikeCast: true, BackslashEscapes: true, CastText: func(e string) string { return "toString(" + e + ")" }}
)

// extraDialects holds dialects registered with RegisterDialect (drivers call it from init()).
var extraDialects = map[string]Dialect{}

func RegisterDialect(d Dialect) { extraDialects[d.Name] = d }

func init() {
	RegisterDialect(Dialect{Name: "sqlite", Quote: quoteWith(`"`), Param: func(int) string { return "?" }, NowExpr: "CURRENT_TIMESTAMP",
		CastText: func(e string) string { return "CAST(" + e + " AS TEXT)" }})
	RegisterDialect(Dialect{Name: "duckdb", Quote: quoteWith(`"`), Param: func(n int) string { return "$" + strconv.Itoa(n) }, ILike: true, NowExpr: "now()",
		LikeCast: true, CastText: func(e string) string { return "CAST(" + e + " AS VARCHAR)" }})
}

func DialectFor(name string) (Dialect, bool) {
	if d, ok := extraDialects[name]; ok {
		return d, true
	}
	switch name {
	case "postgres":
		return PG, true
	case "mysql":
		return MySQL, true
	case "clickhouse":
		return ClickHouse, true
	}
	return Dialect{}, false
}

// QualifiedName quotes schema.name, skipping an empty schema.
func (d Dialect) QualifiedName(ref ObjRef) string {
	if ref.Schema == "" {
		return d.Quote(ref.Name)
	}
	return d.Quote(ref.Schema) + "." + d.Quote(ref.Name)
}

// Literal renders v as a SQL literal for Preview/history display (never used for execution).
func (d Dialect) Literal(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case special:
		switch x {
		case Null:
			return "NULL"
		case Default:
			return "DEFAULT"
		default:
			return "''"
		}
	case Expr:
		return string(x)
	case string:
		if d.BackslashEscapes { // the displayed literal must mean what the server will read
			x = strings.ReplaceAll(x, `\`, `\\`)
		}
		return "'" + strings.ReplaceAll(x, "'", "''") + "'"
	case []byte:
		return fmt.Sprintf("'\\x%x'", x)
	case bool:
		if x {
			return "TRUE"
		}
		return "FALSE"
	case time.Time:
		return "'" + x.Format("2006-01-02 15:04:05.999999") + "'"
	default:
		return fmt.Sprint(x)
	}
}
