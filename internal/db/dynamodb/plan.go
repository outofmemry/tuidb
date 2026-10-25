package dynamodb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"webtuidb/internal/db"
)

func quote(ident string) string { return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"` }

func attrLit(name string) string { return "'" + strings.ReplaceAll(name, "'", "''") + "'" }

// pb builds a PartiQL statement: Text with ? parameters (bound as attributes, so every DynamoDB type
// round-trips) and Display with the values inlined.
type pb struct {
	text, disp strings.Builder
	args       []any
}

func (b *pb) raw(s string) { b.text.WriteString(s); b.disp.WriteString(s) }
func (b *pb) param(av types.AttributeValue) {
	b.args = append(b.args, av)
	b.text.WriteString("?")
	b.disp.WriteString(literal(av))
}

func (b *pb) statement() db.Statement {
	return db.Statement{Text: b.text.String(), Args: b.args, Display: b.disp.String()}
}

// planMeta returns the table's cached structure, reading it when this is the first edit.
func (s *session) planMeta(table string) (*tableMeta, error) {
	s.mu.Lock()
	m, cl := s.metas[table], s.cl
	s.mu.Unlock()
	if m != nil {
		return m, nil
	}
	if cl == nil {
		return nil, fmt.Errorf("the structure of %s is not loaded", table)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return s.loadMeta(ctx, table)
}

// Plan writes PartiQL: INSERT INTO t VALUE {...}, UPDATE t SET/REMOVE ... WHERE key, DELETE FROM t WHERE key.
// NULL stores a DynamoDB NULL attribute; DEFAULT removes the attribute (UPDATE) or leaves it out (INSERT).
func (s *session) Plan(changes []db.Change) ([]db.Statement, error) {
	var out []db.Statement
	for _, c := range changes {
		m, err := s.planMeta(c.Name)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		st, err := planOne(m, c)
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

func (b *pb) where(m *tableMeta, key []db.KeyVal, table string) error {
	by := map[string]any{}
	for _, kv := range key {
		by[kv.Col] = kv.Val
	}
	b.raw(" WHERE ")
	for i, k := range m.keys() {
		v, ok := by[k]
		if !ok {
			return fmt.Errorf("%s: the row has no value for key attribute %q, so it cannot be identified", table, k)
		}
		if db.IsNull(v) {
			return fmt.Errorf("%s: key attribute %q is empty, so the item cannot be identified", table, k)
		}
		av, err := coerce(v, m.attrs[k], k)
		if err != nil {
			return err
		}
		if i > 0 {
			b.raw(" AND ")
		}
		b.raw(quote(k) + " = ")
		b.param(av)
	}
	return nil
}

func planOne(m *tableMeta, c db.Change) (db.Statement, error) {
	b := &pb{}
	tbl := quote(c.Name)
	switch c.Kind {
	case db.Insert:
		var cols []string
		vals := map[string]types.AttributeValue{}
		for _, kv := range c.Set {
			if kv.Val == db.Default {
				continue
			}
			av, err := coerce(kv.Val, m.attrs[kv.Col], kv.Col)
			if err != nil {
				return db.Statement{}, err
			}
			cols = append(cols, kv.Col)
			vals[kv.Col] = av
		}
		for _, k := range m.keys() {
			if v, ok := vals[k]; !ok || typeName(v) == "NULL" {
				return db.Statement{}, fmt.Errorf("insert into %s needs a value for key attribute %q", c.Name, k)
			}
		}
		b.raw("INSERT INTO " + tbl + " VALUE {")
		for i, n := range cols {
			if i > 0 {
				b.raw(", ")
			}
			b.raw(attrLit(n) + ": ")
			b.param(vals[n])
		}
		b.raw("}")
	case db.Update:
		if len(c.Set) == 0 {
			return db.Statement{}, errors.New("update without changed attributes")
		}
		var remove []string
		b.raw("UPDATE " + tbl)
		for _, kv := range c.Set {
			if m.isKey(kv.Col) {
				return db.Statement{}, fmt.Errorf("key attribute %q of %s cannot be changed in place: insert a new item and delete the old one", kv.Col, c.Name)
			}
			if kv.Val == db.Default {
				remove = append(remove, kv.Col)
				continue
			}
			av, err := coerce(kv.Val, m.attrs[kv.Col], kv.Col)
			if err != nil {
				return db.Statement{}, err
			}
			b.raw(" SET " + quote(kv.Col) + " = ")
			b.param(av)
		}
		for _, n := range remove {
			b.raw(" REMOVE " + quote(n))
		}
		if err := b.where(m, c.Key, c.Name); err != nil {
			return db.Statement{}, err
		}
	case db.Delete:
		b.raw("DELETE FROM " + tbl)
		if err := b.where(m, c.Key, c.Name); err != nil {
			return db.Statement{}, err
		}
	default:
		return db.Statement{}, fmt.Errorf("unknown change kind %d", c.Kind)
	}
	return b.statement(), nil
}

// ---- table specs and the admin statements ------------------------------------------------

type keySpec struct {
	Name string `json:"name"`
	Type string `json:"type"`          // S | N | B
	Key  string `json:"key,omitempty"` // HASH | RANGE (default: first HASH, second RANGE)
}

type indexSpec struct {
	Name       string    `json:"name"`
	Kind       string    `json:"kind,omitempty"` // GSI (default) | LSI (only when the table is created)
	Keys       []keySpec `json:"keys"`
	Projection string    `json:"projection,omitempty"` // ALL (default) | KEYS_ONLY | INCLUDE
	Include    []string  `json:"include,omitempty"`
	RCU        int64     `json:"rcu,omitempty"`
	WCU        int64     `json:"wcu,omitempty"`
}

type tableSpec struct {
	Keys    []keySpec   `json:"keys"`
	Billing string      `json:"billing,omitempty"` // PAY_PER_REQUEST (default) | PROVISIONED
	RCU     int64       `json:"rcu,omitempty"`
	WCU     int64       `json:"wcu,omitempty"`
	Indexes []indexSpec `json:"indexes,omitempty"`
}

func compactJSON(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSpace(b.String())
}

func prettyJSON(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
	return strings.TrimSpace(b.String())
}

func createText(name string, sp tableSpec) string {
	return "CREATE TABLE " + quote(name) + " " + prettyJSON(sp)
}

func createStatement(name string, sp tableSpec) db.Statement {
	return db.Statement{Text: "CREATE TABLE " + quote(name) + " " + compactJSON(sp), Display: createText(name, sp)}
}

func specOf(d *types.TableDescription) tableSpec {
	typ := map[string]string{}
	for _, a := range d.AttributeDefinitions {
		typ[aws.ToString(a.AttributeName)] = string(a.AttributeType)
	}
	keys := func(ks []types.KeySchemaElement) []keySpec {
		var out []keySpec
		for _, k := range ks {
			n := aws.ToString(k.AttributeName)
			out = append(out, keySpec{Name: n, Type: typ[n], Key: string(k.KeyType)})
		}
		return out
	}
	sp := tableSpec{Keys: keys(d.KeySchema), Billing: string(types.BillingModePayPerRequest)}
	if d.BillingModeSummary == nil || d.BillingModeSummary.BillingMode == types.BillingModeProvisioned {
		sp.Billing = string(types.BillingModeProvisioned)
		if p := d.ProvisionedThroughput; p != nil {
			sp.RCU, sp.WCU = aws.ToInt64(p.ReadCapacityUnits), aws.ToInt64(p.WriteCapacityUnits)
		}
	}
	idx := func(name *string, ks []types.KeySchemaElement, p *types.Projection, kind string, tp *types.ProvisionedThroughputDescription) indexSpec {
		is := indexSpec{Name: aws.ToString(name), Kind: kind, Keys: keys(ks)}
		if p != nil {
			is.Projection, is.Include = string(p.ProjectionType), p.NonKeyAttributes
		}
		if tp != nil && sp.Billing == string(types.BillingModeProvisioned) {
			is.RCU, is.WCU = aws.ToInt64(tp.ReadCapacityUnits), aws.ToInt64(tp.WriteCapacityUnits)
		}
		return is
	}
	for _, g := range d.GlobalSecondaryIndexes {
		sp.Indexes = append(sp.Indexes, idx(g.IndexName, g.KeySchema, g.Projection, "", g.ProvisionedThroughput))
	}
	for _, l := range d.LocalSecondaryIndexes {
		sp.Indexes = append(sp.Indexes, idx(l.IndexName, l.KeySchema, l.Projection, "LSI", nil))
	}
	return sp
}

func keyTypeOK(t string) bool { return t == "S" || t == "N" || t == "B" }

func keySchemaOf(ks []keySpec, what string) ([]types.KeySchemaElement, error) {
	if len(ks) == 0 || len(ks) > 2 {
		return nil, fmt.Errorf("%s needs a partition key and optionally a sort key (1 or 2 key attributes, got %d)", what, len(ks))
	}
	out := make([]types.KeySchemaElement, len(ks))
	for i, k := range ks {
		want := types.KeyTypeHash
		if i == 1 {
			want = types.KeyTypeRange
		}
		if k.Key != "" && !strings.EqualFold(k.Key, string(want)) {
			return nil, fmt.Errorf("%s: key attribute %q must be %s (list the partition key first)", what, k.Name, want)
		}
		if k.Name == "" {
			return nil, fmt.Errorf("%s: a key attribute needs a name", what)
		}
		if !keyTypeOK(strings.ToUpper(k.Type)) {
			return nil, fmt.Errorf("%s: key attribute %q must be of type S, N or B, not %q", what, k.Name, k.Type)
		}
		out[i] = types.KeySchemaElement{AttributeName: aws.String(k.Name), KeyType: want}
	}
	return out, nil
}

func projectionOf(is indexSpec) (*types.Projection, error) {
	switch p := strings.ToUpper(is.Projection); p {
	case "", "ALL":
		return &types.Projection{ProjectionType: types.ProjectionTypeAll}, nil
	case "KEYS_ONLY":
		return &types.Projection{ProjectionType: types.ProjectionTypeKeysOnly}, nil
	case "INCLUDE":
		if len(is.Include) == 0 {
			return nil, fmt.Errorf("index %q: INCLUDE needs the attributes to project", is.Name)
		}
		return &types.Projection{ProjectionType: types.ProjectionTypeInclude, NonKeyAttributes: is.Include}, nil
	}
	return nil, fmt.Errorf("index %q: projection %q is not ALL, KEYS_ONLY or INCLUDE", is.Name, is.Projection)
}

func throughput(r, w int64, what string) (*types.ProvisionedThroughput, error) {
	if r < 1 || w < 1 {
		return nil, fmt.Errorf("%s is PROVISIONED: give rcu and wcu of at least 1", what)
	}
	return &types.ProvisionedThroughput{ReadCapacityUnits: aws.Int64(r), WriteCapacityUnits: aws.Int64(w)}, nil
}

// attrDefs collects the attribute definitions for key specs, refusing two types for one attribute.
func attrDefs(into map[string]string, order *[]string, ks []keySpec) error {
	for _, k := range ks {
		t := strings.ToUpper(k.Type)
		if cur, ok := into[k.Name]; ok && cur != t {
			return fmt.Errorf("attribute %q is used as both %s and %s", k.Name, cur, t)
		}
		if _, ok := into[k.Name]; !ok {
			*order = append(*order, k.Name)
		}
		into[k.Name] = t
	}
	return nil
}

func defsList(m map[string]string, order []string) []types.AttributeDefinition {
	out := make([]types.AttributeDefinition, len(order))
	for i, n := range order {
		out[i] = types.AttributeDefinition{AttributeName: aws.String(n), AttributeType: types.ScalarAttributeType(m[n])}
	}
	return out
}

func createInput(name string, sp tableSpec) (*dynamodb.CreateTableInput, error) {
	ks, err := keySchemaOf(sp.Keys, "a table")
	if err != nil {
		return nil, err
	}
	in := &dynamodb.CreateTableInput{TableName: aws.String(name), KeySchema: ks}
	typ, order := map[string]string{}, []string{}
	if err := attrDefs(typ, &order, sp.Keys); err != nil {
		return nil, err
	}
	switch strings.ToUpper(sp.Billing) {
	case "", "PAY_PER_REQUEST":
		in.BillingMode = types.BillingModePayPerRequest
	case "PROVISIONED":
		in.BillingMode = types.BillingModeProvisioned
		if in.ProvisionedThroughput, err = throughput(sp.RCU, sp.WCU, "the table"); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("billing %q is not PAY_PER_REQUEST or PROVISIONED", sp.Billing)
	}
	seen := map[string]bool{}
	for _, is := range sp.Indexes {
		if seen[is.Name] || is.Name == "" {
			return nil, fmt.Errorf("index names must be unique and not empty (%q)", is.Name)
		}
		seen[is.Name] = true
		iks, err := keySchemaOf(is.Keys, "index "+is.Name)
		if err != nil {
			return nil, err
		}
		if err := attrDefs(typ, &order, is.Keys); err != nil {
			return nil, err
		}
		proj, err := projectionOf(is)
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(is.Kind, "LSI") {
			if len(sp.Keys) != 2 || len(iks) != 2 || is.Keys[0].Name != sp.Keys[0].Name {
				return nil, fmt.Errorf("local index %q needs the table's partition key %q plus a sort key, and the table needs a sort key", is.Name, sp.Keys[0].Name)
			}
			in.LocalSecondaryIndexes = append(in.LocalSecondaryIndexes, types.LocalSecondaryIndex{IndexName: aws.String(is.Name), KeySchema: iks, Projection: proj})
			continue
		}
		g := types.GlobalSecondaryIndex{IndexName: aws.String(is.Name), KeySchema: iks, Projection: proj}
		if in.BillingMode == types.BillingModeProvisioned {
			r, w := is.RCU, is.WCU
			if r == 0 && w == 0 {
				r, w = sp.RCU, sp.WCU
			}
			if g.ProvisionedThroughput, err = throughput(r, w, "index "+is.Name); err != nil {
				return nil, err
			}
		}
		in.GlobalSecondaryIndexes = append(in.GlobalSecondaryIndexes, g)
	}
	in.AttributeDefinitions = defsList(typ, order)
	return in, nil
}

// ---- parsing the admin statements back ----------------------------------------------------

type adminStmt struct {
	op       string // create | like | drop | truncate | add_index | drop_index
	table    string
	other    string // source table (like) or index name (drop_index)
	withData bool
	create   tableSpec
	index    indexSpec
}

type scanner struct{ s string }

func (l *scanner) skip() { l.s = strings.TrimLeft(l.s, " \t\r\n") }

func (l *scanner) word() string {
	l.skip()
	i := 0
	for i < len(l.s) && (l.s[i] == '_' || l.s[i] >= 'A' && l.s[i] <= 'Z' || l.s[i] >= 'a' && l.s[i] <= 'z') {
		i++
	}
	w := l.s[:i]
	l.s = l.s[i:]
	return strings.ToUpper(w)
}

// ident reads "quoted" (with "" for a quote) or a bare name.
func (l *scanner) ident() (string, error) {
	l.skip()
	if strings.HasPrefix(l.s, `"`) {
		var b strings.Builder
		for i := 1; i < len(l.s); i++ {
			if l.s[i] == '"' {
				if i+1 < len(l.s) && l.s[i+1] == '"' {
					b.WriteByte('"')
					i++
					continue
				}
				l.s = l.s[i+1:]
				return b.String(), nil
			}
			b.WriteByte(l.s[i])
		}
		return "", errors.New("unterminated quoted name")
	}
	i := 0
	for i < len(l.s) && (l.s[i] == '_' || l.s[i] == '-' || l.s[i] == '.' || l.s[i] >= '0' && l.s[i] <= '9' || l.s[i] >= 'A' && l.s[i] <= 'Z' || l.s[i] >= 'a' && l.s[i] <= 'z') {
		i++
	}
	if i == 0 {
		return "", errors.New("expected a table or index name")
	}
	n := l.s[:i]
	l.s = l.s[i:]
	return n, nil
}

func (l *scanner) end() error {
	if strings.TrimSpace(l.s) != "" {
		return fmt.Errorf("unexpected text %q", strings.TrimSpace(l.s))
	}
	return nil
}

func (l *scanner) json(into any) error {
	dec := json.NewDecoder(strings.NewReader(l.s))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("spec: %w", err)
	}
	l.s = l.s[dec.InputOffset():]
	return l.end()
}

// stripLead removes leading whitespace and comments and a trailing semicolon.
func stripLead(stmt string) string {
	s := strings.TrimSpace(stmt)
	for {
		switch {
		case strings.HasPrefix(s, "--"):
			i := strings.IndexByte(s, '\n')
			if i < 0 {
				return ""
			}
			s = strings.TrimSpace(s[i:])
		case strings.HasPrefix(s, "/*"):
			i := strings.Index(s, "*/")
			if i < 0 {
				return ""
			}
			s = strings.TrimSpace(s[i+2:])
		default:
			return strings.TrimSpace(strings.TrimSuffix(s, ";"))
		}
	}
}

const adminUsage = `want CREATE TABLE "t" {json} | CREATE TABLE "t" LIKE "src" [WITH DATA] | DROP TABLE "t" | TRUNCATE TABLE "t" | ALTER TABLE "t" ADD INDEX {json} | ALTER TABLE "t" DROP INDEX "i"`

// parseAdmin recognises the table-level statements this driver defines; (nil, nil) means PartiQL.
func parseAdmin(stmt string) (*adminStmt, error) {
	l := &scanner{s: stripLead(stmt)}
	first := l.word()
	fail := func(err error) (*adminStmt, error) { return nil, fmt.Errorf("%w (%s)", err, adminUsage) }
	a := &adminStmt{}
	switch first {
	case "CREATE", "DROP", "TRUNCATE", "ALTER":
	default:
		return nil, nil
	}
	if first == "TRUNCATE" { // TABLE is optional
		if save := *l; save.word() == "TABLE" {
			l.word()
		}
	} else if l.word() != "TABLE" {
		return fail(errors.New("only tables can be created, altered or dropped"))
	}
	var err error
	if a.table, err = l.ident(); err != nil {
		return fail(err)
	}
	switch first {
	case "DROP":
		a.op = "drop"
		return a, wrapAdmin(l.end())
	case "TRUNCATE":
		a.op = "truncate"
		return a, wrapAdmin(l.end())
	case "CREATE":
		if save := *l; save.word() == "LIKE" {
			l.word()
			a.op = "like"
			if a.other, err = l.ident(); err != nil {
				return fail(err)
			}
			if w := l.word(); w == "WITH" {
				if l.word() != "DATA" {
					return fail(errors.New("expected WITH DATA"))
				}
				a.withData = true
			} else if w != "" {
				return fail(errors.New("syntax error"))
			}
			return a, wrapAdmin(l.end())
		}
		a.op = "create"
		return a, wrapAdmin(l.json(&a.create))
	}
	switch w := l.word() + " " + l.word(); w { // ALTER
	case "ADD INDEX":
		a.op = "add_index"
		return a, wrapAdmin(l.json(&a.index))
	case "DROP INDEX":
		a.op = "drop_index"
		if a.other, err = l.ident(); err != nil {
			return fail(err)
		}
		return a, wrapAdmin(l.end())
	}
	return fail(errors.New("syntax error"))
}

func wrapAdmin(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w (%s)", err, adminUsage)
}

// ---- admin operations ------------------------------------------------------------------

func (s *session) tableExists(table string) (bool, error) {
	if s.client() == nil { // planning without a server (unit tests): nothing to check
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := s.client().DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(table)})
	var nf *types.ResourceNotFoundException
	if errors.As(err, &nf) {
		return false, nil
	}
	return err == nil, wrap("describe table "+table, err)
}

func (s *session) PlanAdmin(op db.AdminOp, ref db.ObjRef, opts map[string]string) ([]db.Statement, error) {
	one := func(t string) []db.Statement { return []db.Statement{{Text: t}} }
	q := quote(ref.Name)
	switch op {
	case db.OpDropObject:
		return one("DROP TABLE " + q), nil
	case db.OpTruncate:
		if opts["cascade"] == "true" {
			return nil, errors.New("DynamoDB has no TRUNCATE ... CASCADE")
		}
		return one("TRUNCATE TABLE " + q), nil
	case db.OpClone:
		n := opts["new_name"]
		if n == "" {
			return nil, errors.New("clone needs a new name")
		}
		if exists, err := s.tableExists(n); err != nil {
			return nil, err
		} else if exists {
			return nil, fmt.Errorf("table %q already exists", n)
		}
		t := "CREATE TABLE " + quote(n) + " LIKE " + q
		if opts["with_data"] == "true" {
			t += " WITH DATA"
		}
		return one(t), nil
	case db.OpRenameObject:
		return nil, errors.New("DynamoDB cannot rename a table: clone it with its data (Clone…) and then delete the original")
	case db.OpCreateDatabase, db.OpDropDatabase, db.OpRenameDatabase:
		return nil, errors.New("a region is not something DynamoDB lets you create, drop or rename; open one with the region picker")
	}
	return nil, fmt.Errorf("%s is not supported by DynamoDB", op)
}

// ---- structure editing -------------------------------------------------------------------

func (s *session) PlanTable(old, new *db.TableDef) ([]db.Statement, error) {
	return planTable(old, new)
}

func primaryKey(t *db.TableDef) []string {
	if len(t.PrimaryKey) > 0 {
		return t.PrimaryKey
	}
	var pk []string
	for _, c := range t.Columns {
		if c.PK {
			pk = append(pk, c.Name)
		}
	}
	return pk
}

// parseProjection reads an index's Condition cell: ALL, KEYS_ONLY or INCLUDE a, b (a leading
// "projection:" and parentheses are accepted).
func parseProjection(cond string) (string, []string, error) {
	c := strings.TrimSpace(cond)
	if len(c) >= 11 && strings.EqualFold(c[:11], "projection:") {
		c = strings.TrimSpace(c[11:])
	}
	if c == "" {
		return "ALL", nil, nil
	}
	f := strings.Fields(c)
	switch w := strings.ToUpper(f[0]); w {
	case "ALL", "KEYS_ONLY":
		if len(f) == 1 {
			return w, nil, nil
		}
	case "INCLUDE":
		var attrs []string
		for _, a := range strings.Split(strings.Trim(strings.TrimSpace(c[len(f[0]):]), "()"), ",") {
			if a = strings.TrimSpace(a); a != "" {
				attrs = append(attrs, a)
			}
		}
		if len(attrs) > 0 {
			return w, attrs, nil
		}
	}
	return "", nil, fmt.Errorf("index condition %q is not a projection: use ALL, KEYS_ONLY or INCLUDE attr1, attr2", cond)
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// indexSpecOf turns a structure-editor index into a spec; the key attribute types come from the columns.
func indexSpecOf(ix db.Index, types map[string]string) (indexSpec, error) {
	is := indexSpec{Name: ix.Name}
	switch strings.ToUpper(ix.Algorithm) {
	case "", "GSI":
	case "LSI":
		is.Kind = "LSI"
	default:
		return is, fmt.Errorf("index %q: algorithm %q is not GSI or LSI", ix.Name, ix.Algorithm)
	}
	if ix.Unique {
		return is, fmt.Errorf("index %q: DynamoDB indexes cannot be unique", ix.Name)
	}
	for i, c := range ix.Columns {
		t, ok := types[c]
		if !ok {
			return is, fmt.Errorf("index %q: add attribute %q as a column of type S, N or B first", ix.Name, c)
		}
		if !keyTypeOK(t) {
			return is, fmt.Errorf("index %q: key attribute %q is %s but index keys must be S, N or B (set the column type first)", ix.Name, c, t)
		}
		k := keySpec{Name: c, Type: t, Key: "HASH"}
		if i == 1 {
			k.Key = "RANGE"
		}
		is.Keys = append(is.Keys, k)
	}
	var err error
	is.Projection, is.Include, err = parseProjection(ix.Condition)
	return is, err
}

func planTable(old, new *db.TableDef) ([]db.Statement, error) {
	if new == nil || new.Name == "" {
		return nil, errors.New("table needs a name")
	}
	typ := map[string]string{} // column -> attribute type
	for _, c := range new.Columns {
		typ[c.Name] = strings.ToUpper(strings.TrimSpace(c.Type))
	}
	pk := primaryKey(new)
	if old == nil {
		sp := tableSpec{Billing: "PAY_PER_REQUEST"}
		for i, n := range pk {
			t, ok := typ[n]
			if !ok {
				return nil, fmt.Errorf("primary key column %q does not exist", n)
			}
			k := keySpec{Name: n, Type: t, Key: "HASH"}
			if i == 1 {
				k.Key = "RANGE"
			}
			sp.Keys = append(sp.Keys, k)
		}
		used := map[string]bool{}
		for _, ix := range new.Indexes {
			if ix.Primary {
				continue
			}
			is, err := indexSpecOf(ix, typ)
			if err != nil {
				return nil, err
			}
			for _, c := range ix.Columns {
				used[c] = true
			}
			sp.Indexes = append(sp.Indexes, is)
		}
		for _, c := range new.Columns {
			if !contains(pk, c.Name) && !used[c.Name] {
				return nil, fmt.Errorf("column %q cannot be declared: DynamoDB tables only declare their key attributes (and index keys); other attributes exist once an item has them", c.Name)
			}
		}
		if _, err := createInput(new.Name, sp); err != nil { // validate now so Preview shows the error, not Apply
			return nil, err
		}
		return []db.Statement{createStatement(new.Name, sp)}, nil
	}
	if new.Name != old.Name {
		return nil, errors.New("DynamoDB cannot rename a table: clone it with its data and delete the original")
	}
	if !sameStrings(primaryKey(old), pk) || !equalOrder(primaryKey(old), pk) {
		return nil, errors.New("the primary key of a DynamoDB table cannot be changed: create a new table with the key you want and copy the items")
	}
	oldCols := map[string]db.Column{}
	for _, c := range old.Columns {
		oldCols[c.Name] = c
	}
	kept := map[string]bool{}
	var added []string
	for _, c := range new.Columns {
		switch {
		case c.OrigName == "":
			added = append(added, c.Name)
		case c.OrigName != c.Name:
			return nil, fmt.Errorf("attribute %q cannot be renamed to %q: DynamoDB stores the name in every item (UPDATE each item instead)", c.OrigName, c.Name)
		default:
			kept[c.Name] = true
			if o := oldCols[c.Name]; !strings.EqualFold(strings.TrimSpace(o.Type), strings.TrimSpace(c.Type)) {
				return nil, fmt.Errorf("attribute %q cannot change type from %s to %s: types belong to the items, not the table", c.Name, o.Type, c.Type)
			}
		}
	}
	for _, c := range old.Columns {
		if !kept[c.Name] {
			return nil, fmt.Errorf("attribute %q cannot be dropped from the table: remove it from the items with UPDATE ... REMOVE", c.Name)
		}
	}
	oldIdx, newIdx := map[string]db.Index{}, map[string]db.Index{}
	for _, ix := range old.Indexes {
		if !ix.Primary {
			oldIdx[ix.Name] = ix
		}
	}
	var out []db.Statement
	used := map[string]bool{}
	tbl := quote(old.Name)
	drop := func(name string) error {
		if strings.EqualFold(oldIdx[name].Algorithm, "LSI") {
			return fmt.Errorf("local index %q cannot be dropped: LSIs only exist from table creation", name)
		}
		out = append(out, db.Statement{Text: "ALTER TABLE " + tbl + " DROP INDEX " + quote(name)})
		return nil
	}
	add := func(ix db.Index) error {
		is, err := indexSpecOf(ix, typ)
		if err != nil {
			return err
		}
		if is.Kind == "LSI" {
			return fmt.Errorf("local index %q cannot be added: LSIs can only be created together with the table", ix.Name)
		}
		for _, c := range ix.Columns {
			used[c] = true
		}
		out = append(out, db.Statement{Text: "ALTER TABLE " + tbl + " ADD INDEX " + compactJSON(is), Display: "ALTER TABLE " + tbl + " ADD INDEX " + prettyJSON(is)})
		return nil
	}
	for _, ix := range new.Indexes {
		if ix.Primary {
			continue
		}
		newIdx[ix.Name] = ix
	}
	for _, ix := range old.Indexes { // keep the order the grid shows
		if _, ok := newIdx[ix.Name]; !ok && !ix.Primary {
			if err := drop(ix.Name); err != nil {
				return nil, err
			}
		}
	}
	for _, ix := range new.Indexes {
		if ix.Primary {
			continue
		}
		o, exists := oldIdx[ix.Name]
		if !exists {
			if err := add(ix); err != nil {
				return nil, err
			}
			continue
		}
		op, oinc, _ := parseProjection(o.Condition)
		np, ninc, err := parseProjection(ix.Condition)
		if err != nil {
			return nil, err
		}
		if equalOrder(o.Columns, ix.Columns) && op == np && sameStrings(oinc, ninc) && strings.EqualFold(o.Algorithm, ix.Algorithm) && o.Unique == ix.Unique {
			for _, c := range ix.Columns {
				used[c] = true
			}
			continue
		}
		if err := drop(ix.Name); err != nil { // a changed index is dropped and rebuilt
			return nil, err
		}
		if err := add(ix); err != nil {
			return nil, err
		}
	}
	for _, n := range added {
		if !used[n] {
			return nil, fmt.Errorf("attribute %q cannot be added to the table: it exists once an item has it (only index keys are declared)", n)
		}
	}
	return out, nil
}

func equalOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
