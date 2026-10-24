package dynamodb

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"tuidb/internal/db"
)

// maxRows bounds a read with no row limit.
const maxRows = 50000

// expr collects the expression attribute names and values of one request. Every attribute goes through
// a #placeholder, so reserved words and odd characters in names are never a problem.
type expr struct {
	byAttr map[string]string
	names  map[string]string
	vals   map[string]types.AttributeValue
}

func newExpr() *expr {
	return &expr{byAttr: map[string]string{}, names: map[string]string{}, vals: map[string]types.AttributeValue{}}
}

func (e *expr) clone() *expr {
	c := newExpr()
	for k, v := range e.byAttr {
		c.byAttr[k] = v
	}
	for k, v := range e.names {
		c.names[k] = v
	}
	for k, v := range e.vals {
		c.vals[k] = v
	}
	return c
}

func (e *expr) name(a string) string {
	if p, ok := e.byAttr[a]; ok {
		return p
	}
	p := "#n" + strconv.Itoa(len(e.byAttr))
	e.byAttr[a], e.names[p] = p, a
	return p
}

func (e *expr) val(av types.AttributeValue) string {
	p := ":v" + strconv.Itoa(len(e.vals))
	e.vals[p] = av
	return p
}

// readPlan is a Query (the filters pin the partition key) or a Scan.
type readPlan struct {
	query   bool
	keyCond string
	filter  string
	proj    string
	e       *expr
	forward bool
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if len(p) >= 2 && (p[0] == '\'' || p[0] == '"') && p[len(p)-1] == p[0] {
			p = p[1 : len(p)-1]
		}
		out = append(out, p)
	}
	return out
}

func splitBetween(v string) (string, string, error) {
	i := strings.Index(strings.ToUpper(v), " AND ")
	if i < 0 {
		return "", "", fmt.Errorf("BETWEEN needs two values: low AND high")
	}
	return strings.TrimSpace(v[:i]), strings.TrimSpace(v[i+5:]), nil
}

// filterValue types the text of a filter by the column's attribute type. Contains on a set tests one
// element, so the value takes the element type.
func filterValue(typ, text, op string) (types.AttributeValue, error) {
	if op == "Contains" || op == "Not contains" {
		switch typ {
		case "SS":
			typ = "S"
		case "NS":
			typ = "N"
		case "BS":
			typ = "B"
		}
	}
	switch typ {
	case "S", "N", "B", "BOOL":
	default:
		typ = ""
	}
	return coerceText(text, typ, "filter", func(err error) (types.AttributeValue, error) { return nil, err })
}

// colCond renders one filter operator on one attribute.
func colCond(m *tableMeta, e *expr, col string, f db.Filter) (string, error) {
	typ := m.attrs[col]
	n := e.name(col)
	val := func(text string) (string, error) {
		av, err := filterValue(typ, text, f.Op)
		if err != nil {
			return "", fmt.Errorf("filter on %s (%s): %w", col, typ, err)
		}
		return e.val(av), nil
	}
	switch f.Op {
	case "=", "<>", "<", ">", "<=", ">=":
		v, err := val(f.Value)
		return n + " " + f.Op + " " + v, err
	case "IS NULL", "IS NOT NULL":
		t := e.val(&types.AttributeValueMemberS{Value: "NULL"})
		if f.Op == "IS NULL" {
			return "(attribute_not_exists(" + n + ") OR attribute_type(" + n + ", " + t + "))", nil
		}
		return "(attribute_exists(" + n + ") AND NOT attribute_type(" + n + ", " + t + "))", nil
	case "IN", "NOT IN":
		var ph []string
		for _, it := range splitList(f.Value) {
			v, err := val(it)
			if err != nil {
				return "", err
			}
			ph = append(ph, v)
		}
		in := n + " IN (" + strings.Join(ph, ", ") + ")"
		if f.Op == "NOT IN" {
			return "NOT (" + in + ")", nil
		}
		return in, nil
	case "BETWEEN", "NOT BETWEEN":
		lo, hi, err := splitBetween(f.Value)
		if err != nil {
			return "", err
		}
		a, err := val(lo)
		if err != nil {
			return "", err
		}
		b, err := val(hi)
		if err != nil {
			return "", err
		}
		c := n + " BETWEEN " + a + " AND " + b
		if f.Op == "NOT BETWEEN" {
			return "NOT (" + c + ")", nil
		}
		return c, nil
	case "Contains", "Not contains", "Has prefix":
		v, err := val(f.Value)
		if err != nil {
			return "", err
		}
		switch f.Op {
		case "Contains":
			return "contains(" + n + ", " + v + ")", nil
		case "Not contains":
			return "NOT contains(" + n + ", " + v + ")", nil
		}
		return "begins_with(" + n + ", " + v + ")", nil
	case "LIKE", "ILIKE", "Has suffix":
		return "", fmt.Errorf("%s is not supported by DynamoDB: use Contains or Has prefix", f.Op)
	}
	return "", fmt.Errorf("unknown filter operator %q", f.Op)
}

// startsOperand: a '-' before a digit is a sign (not a subtraction) after an operator, "(" or ",".
func startsOperand(written string) bool {
	t := strings.TrimRight(written, " ")
	return t == "" || strings.ContainsRune("<>=(,", rune(t[len(t)-1]))
}

var exprWords = map[string]bool{"AND": true, "OR": true, "NOT": true, "IN": true, "BETWEEN": true}

// rawCond turns the filter bar's raw condition into a FilterExpression. DynamoDB expressions cannot hold
// literals or reserved words, so the text is rewritten: 'strings', numbers, true/false and null become
// value placeholders, and every bare word that is not a function (followed by "(") or AND/OR/NOT/IN/BETWEEN
// becomes an attribute-name placeholder (dotted paths and [n] indexes keep their shape).
// Example: age < 40 AND begins_with(name, 'A').
func rawCond(e *expr, text string) (string, error) {
	var b strings.Builder
	val := func(av types.AttributeValue) { b.WriteString(e.val(av)) }
	isWord := func(c byte) bool {
		return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '\'':
			j, sb := i+1, strings.Builder{}
			for ; j < len(text); j++ {
				if text[j] == '\'' {
					if j+1 < len(text) && text[j+1] == '\'' {
						sb.WriteByte('\'')
						j++
						continue
					}
					break
				}
				sb.WriteByte(text[j])
			}
			if j >= len(text) {
				return "", fmt.Errorf("raw filter: unterminated string")
			}
			val(&types.AttributeValueMemberS{Value: sb.String()})
			i = j + 1
		case c >= '0' && c <= '9' || c == '-' && i+1 < len(text) && text[i+1] >= '0' && text[i+1] <= '9' && startsOperand(b.String()):
			j := i + 1
			for j < len(text) && (text[j] >= '0' && text[j] <= '9' || text[j] == '.' || text[j] == 'e' || text[j] == 'E') {
				j++
			}
			av, err := numAttr(text[i:j])
			if err != nil {
				return "", fmt.Errorf("raw filter: %w", err)
			}
			val(av)
			i = j
		case c == '[': // list index
			j := strings.IndexByte(text[i:], ']')
			if j < 0 {
				return "", fmt.Errorf("raw filter: unterminated [")
			}
			b.WriteString(text[i : i+j+1])
			i += j + 1
		case isWord(c):
			j := i
			for j < len(text) && isWord(text[j]) {
				j++
			}
			w := text[i:j]
			rest := strings.TrimLeft(text[j:], " ")
			switch {
			case exprWords[strings.ToUpper(w)]:
				b.WriteString(strings.ToUpper(w))
			case w == "true" || w == "false":
				val(&types.AttributeValueMemberBOOL{Value: w == "true"})
			case w == "null":
				val(&types.AttributeValueMemberNULL{Value: true})
			case strings.HasPrefix(rest, "("):
				b.WriteString(w) // attribute_exists(...), begins_with(...), size(...)
			default:
				b.WriteString(e.name(w))
			}
			i = j
		default:
			b.WriteByte(c)
			i++
		}
	}
	return "(" + b.String() + ")", nil
}

// anyCond is a filter on every column: an OR over the columns the value makes sense for.
func anyCond(m *tableMeta, e *expr, f db.Filter, skipKeys bool) (string, error) {
	var ors []string
	var last error
	for _, c := range m.names {
		t := m.attrs[c]
		if skipKeys && m.isKey(c) {
			continue
		}
		switch f.Op {
		case "Contains", "Not contains":
			if t != "S" && t != "SS" && t != "NS" && t != "BS" && t != "L" && t != "mixed" {
				continue
			}
		case "Has prefix":
			if t != "S" && t != "B" && t != "mixed" {
				continue
			}
		}
		snap := e.clone() // a column the value does not fit must not leave unused placeholders behind
		s, err := colCond(m, e, c, f)
		if err != nil {
			*e = *snap
			last = err
			continue
		}
		ors = append(ors, s)
	}
	if len(ors) == 0 {
		if last != nil {
			return "", last
		}
		return "", fmt.Errorf("no column can be searched with %s", f.Op)
	}
	return "(" + strings.Join(ors, " OR ") + ")", nil
}

// sortKeyOps are the sort-key conditions a KeyConditionExpression accepts.
var sortKeyOps = map[string]bool{"=": true, "<": true, ">": true, "<=": true, ">=": true, "BETWEEN": true, "Has prefix": true}

func buildRead(m *tableMeta, fs db.FilterSet, cols []string, sorts []db.Sort) (*readPlan, error) {
	e := newExpr()
	p := &readPlan{e: e, forward: true}
	act := fs.Active()
	// Query when exactly one "=" pins the partition key and the sort key has at most one usable condition.
	var pk, sk *db.Filter
	query := !fs.Or && len(act) > 0
	for i := range act {
		f := &act[i]
		switch {
		case f.Column == db.RawColumn:
			query = false
		case f.Column == m.hash:
			if pk != nil || f.Op != "=" {
				query = false
			}
			pk = f
		case m.rng != "" && f.Column == m.rng:
			if sk != nil || !sortKeyOps[f.Op] {
				query = false
			}
			sk = f
		}
	}
	query = query && pk != nil
	var parts []string
	if query {
		p.query = true
		c, err := colCond(m, e, pk.Column, *pk)
		if err != nil {
			return nil, err
		}
		p.keyCond = c
		if sk != nil {
			if c, err = colCond(m, e, sk.Column, *sk); err != nil {
				return nil, err
			}
			p.keyCond += " AND " + c
		}
		if len(sorts) > 0 && sorts[0].Column == m.rng && m.rng != "" {
			p.forward = !sorts[0].Desc
		}
	}
	for i := range act {
		f := act[i]
		if p.query && (&act[i] == pk || &act[i] == sk) {
			continue
		}
		var c string
		var err error
		switch f.Column {
		case db.RawColumn:
			if strings.TrimSpace(f.Value) == "" {
				continue
			}
			c, err = rawCond(e, f.Value)
		case db.AnyColumn:
			c, err = anyCond(m, e, f, p.query) // a Query filter may not name key attributes
		default:
			c, err = colCond(m, e, f.Column, f)
		}
		if err != nil {
			return nil, err
		}
		parts = append(parts, c)
	}
	join := " AND "
	if fs.Or {
		join = " OR "
	}
	p.filter = strings.Join(parts, join)
	if len(cols) > 0 {
		ph := make([]string, len(cols))
		for i, c := range cols {
			ph[i] = e.name(c)
		}
		p.proj = strings.Join(ph, ", ")
	}
	return p, nil
}

// build runs buildRead while holding the lock that guards the table's learned attributes.
func (s *session) build(m *tableMeta, fs db.FilterSet, cols []string, sorts []db.Sort) (*readPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return buildRead(m, fs, cols, sorts)
}

type pageOut struct {
	items []map[string]types.AttributeValue
	last  map[string]types.AttributeValue
	count int
}

func nilIfEmptyN(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}

func nilIfEmptyV(m map[string]types.AttributeValue) map[string]types.AttributeValue {
	if len(m) == 0 {
		return nil
	}
	return m
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// page runs one Query or Scan request. limit > 0 bounds the items evaluated; count asks for the number only.
func (s *session) page(ctx context.Context, table string, p *readPlan, start map[string]types.AttributeValue, limit int, count bool) (*pageOut, error) {
	cl := s.client()
	var lim *int32
	if limit > 0 {
		lim = aws.Int32(int32(limit))
	}
	sel := types.SelectAllAttributes
	proj := strPtr(p.proj)
	if count {
		sel, proj = types.SelectCount, nil
	} else if proj != nil {
		sel = types.SelectSpecificAttributes
	}
	if p.query {
		out, err := cl.Query(ctx, &dynamodb.QueryInput{TableName: aws.String(table), KeyConditionExpression: aws.String(p.keyCond), FilterExpression: strPtr(p.filter),
			ProjectionExpression: proj, ExpressionAttributeNames: nilIfEmptyN(p.e.names), ExpressionAttributeValues: nilIfEmptyV(p.e.vals),
			ExclusiveStartKey: start, Limit: lim, ScanIndexForward: aws.Bool(p.forward), Select: sel})
		if err != nil {
			return nil, wrap("query "+table, err)
		}
		return &pageOut{items: out.Items, last: out.LastEvaluatedKey, count: int(out.Count)}, nil
	}
	out, err := cl.Scan(ctx, &dynamodb.ScanInput{TableName: aws.String(table), FilterExpression: strPtr(p.filter), ProjectionExpression: proj,
		ExpressionAttributeNames: nilIfEmptyN(p.e.names), ExpressionAttributeValues: nilIfEmptyV(p.e.vals), ExclusiveStartKey: start, Limit: lim, Select: sel})
	if err != nil {
		return nil, wrap("scan "+table, err)
	}
	return &pageOut{items: out.Items, last: out.LastEvaluatedKey, count: int(out.Count)}, nil
}

// fetch walks the pages: it discards the first skip matching items (DynamoDB has no OFFSET) and keeps want.
func (s *session) fetch(ctx context.Context, table string, p *readPlan, skip, want int) (items []map[string]types.AttributeValue, more bool, err error) {
	var start map[string]types.AttributeValue
	consumed := 0
	for {
		need := skip + want - consumed
		limit := min(need, 1000)
		if p.filter != "" {
			limit = min(max(need, 100), 1000) // the limit counts items evaluated, not items kept
		}
		out, err := s.page(ctx, table, p, start, limit, false)
		if err != nil {
			return nil, false, err
		}
		for _, it := range out.items {
			if consumed < skip {
				consumed++
				continue
			}
			if len(items) >= want {
				return items, true, nil
			}
			items = append(items, it)
			consumed++
		}
		if out.last == nil {
			return items, false, nil
		}
		if len(items) >= want {
			return items, true, nil
		}
		start = out.last
	}
}

func (s *session) Rows(ctx context.Context, q db.RowQuery) (*db.Result, error) {
	m, err := s.meta(ctx, q.Name)
	if err != nil {
		return nil, err
	}
	p, err := s.build(m, q.Filters, q.Columns, q.Sort)
	if err != nil {
		return nil, err
	}
	want := q.Limit
	if want <= 0 {
		want = maxRows
	}
	items, more, err := s.fetch(ctx, q.Name, p, max(q.Offset, 0), want)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	m.learn(items)
	names := q.Columns
	if len(names) == 0 {
		names = append([]string(nil), m.names...)
	}
	res := &db.Result{HasRows: true, Truncated: more && q.Limit <= 0}
	typ := make([]string, len(names))
	for i, n := range names {
		typ[i] = m.attrs[n]
		res.Columns = append(res.Columns, db.ResultColumn{Name: n, Type: typ[i]})
	}
	s.mu.Unlock()
	for _, it := range items {
		row := make([]any, len(names))
		for i, n := range names {
			if v, ok := it[n]; ok {
				row[i] = cell(v)
			}
		}
		res.Rows = append(res.Rows, row)
		res.Docs = append(res.Docs, itemJSON(it))
	}
	// Only a Query on the sort key is ordered by the server; anything else orders the fetched page.
	if len(q.Sort) > 0 && !(p.query && m.rng != "" && q.Sort[0].Column == m.rng) {
		sortPage(res, q.Sort, typ)
	}
	return res, nil
}

func (s *session) Count(ctx context.Context, q db.RowQuery, estimate bool) (db.Count, error) {
	if estimate && len(q.Filters.Active()) == 0 {
		d, err := s.describe(ctx, q.Name)
		if err != nil {
			return db.Count{}, err
		}
		// ItemCount is refreshed about every 6 hours: a 0 may just mean "new", so count to be sure.
		if n := aws.ToInt64(d.ItemCount); n > 0 {
			return db.Count{N: n}, nil
		}
	}
	m, err := s.meta(ctx, q.Name)
	if err != nil {
		return db.Count{}, err
	}
	p, err := s.build(m, q.Filters, nil, nil)
	if err != nil {
		return db.Count{}, err
	}
	var n int64
	var start map[string]types.AttributeValue
	for {
		out, err := s.page(ctx, q.Name, p, start, 0, true)
		if err != nil {
			return db.Count{}, err
		}
		n += int64(out.count)
		if out.last == nil {
			return db.Count{N: n, Exact: true}, nil
		}
		start = out.last
	}
}

// ---- client-side ordering of a fetched page --------------------------------------------------

func sortPage(res *db.Result, sorts []db.Sort, typ []string) {
	col := make([]int, len(sorts)) // sort position -> column index, -1 when the column is not in the page
	for j, so := range sorts {
		col[j] = -1
		for i, c := range res.Columns {
			if c.Name == so.Column {
				col[j] = i
			}
		}
	}
	order := make([]int, len(res.Rows))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		for j, so := range sorts {
			if col[j] < 0 {
				continue
			}
			r := cmpCell(res.Rows[order[a]][col[j]], res.Rows[order[b]][col[j]], typ[col[j]] == "N")
			if r != 0 {
				return (r < 0) != so.Desc
			}
		}
		return false
	})
	rows, docs := make([][]any, len(order)), make([]string, len(order))
	for i, o := range order {
		rows[i], docs[i] = res.Rows[o], res.Docs[o]
	}
	res.Rows, res.Docs = rows, docs
}

// cmpCell orders nulls first, numbers numerically (N columns keep exact decimal text) and the rest by text.
func cmpCell(a, b any, numeric bool) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	x, y := db.FormatValue(a), db.FormatValue(b)
	if numeric {
		fx, _, ex := big.ParseFloat(x, 10, 200, big.ToNearestEven)
		fy, _, ey := big.ParseFloat(y, 10, 200, big.ToNearestEven)
		if ex == nil && ey == nil {
			return fx.Cmp(fy)
		}
	}
	return strings.Compare(x, y)
}
