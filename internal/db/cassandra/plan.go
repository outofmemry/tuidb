package cassandra

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"gopkg.in/inf.v0"

	"tuidb/internal/db"
)

// goValue turns a cell value into what gocql marshals for the column type. The UI hands over original values as the driver
// returned them (int64, time.Time, ...) and edited ones as text. gocql itself reads text for the integer types, uuid, inet,
// varint and date; the rest is converted here. A nil type (unknown column) passes the value through.
func goValue(ti gocql.TypeInfo, v any) (any, error) {
	if ti == nil {
		return v, nil
	}
	s, isStr := v.(string)
	switch ti.Type() {
	case gocql.TypeTinyInt, gocql.TypeSmallInt, gocql.TypeInt, gocql.TypeBigInt, gocql.TypeCounter: // gocql range-checks the int64 itself
		if isStr {
			n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%q is not an integer", s)
			}
			return n, nil
		}
	case gocql.TypeVarint:
		if isStr {
			n, ok := new(big.Int).SetString(strings.TrimSpace(s), 10)
			if !ok {
				return nil, fmt.Errorf("%q is not an integer", s)
			}
			return n, nil
		}
	case gocql.TypeUUID, gocql.TypeTimeUUID:
		if isStr {
			u, err := gocql.ParseUUID(strings.TrimSpace(s))
			if err != nil {
				return nil, fmt.Errorf("%q is not a uuid", s)
			}
			return u, nil
		}
	case gocql.TypeInet:
		if isStr {
			ip := net.ParseIP(strings.TrimSpace(s))
			if ip == nil {
				return nil, fmt.Errorf("%q is not an IP address", s)
			}
			return ip, nil
		}
	case gocql.TypeBoolean:
		if isStr {
			b, err := strconv.ParseBool(strings.TrimSpace(s))
			if err != nil {
				return nil, fmt.Errorf("%q is not a boolean", s)
			}
			return b, nil
		}
	case gocql.TypeFloat:
		if f, ok := v.(float64); ok {
			return float32(f), nil
		}
		if isStr {
			f, err := strconv.ParseFloat(strings.TrimSpace(s), 32)
			if err != nil {
				return nil, fmt.Errorf("%q is not a float", s)
			}
			return float32(f), nil
		}
	case gocql.TypeDouble:
		if isStr {
			f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				return nil, fmt.Errorf("%q is not a double", s)
			}
			return f, nil
		}
	case gocql.TypeDecimal:
		if isStr {
			d, ok := new(inf.Dec).SetString(strings.TrimSpace(s))
			if !ok {
				return nil, fmt.Errorf("%q is not a decimal", s)
			}
			return d, nil
		}
		if n, ok := v.(int64); ok {
			return inf.NewDec(n, 0), nil
		}
	case gocql.TypeTimestamp:
		if isStr {
			return parseTimestamp(strings.TrimSpace(s))
		}
	case gocql.TypeTime:
		if isStr {
			t, err := time.Parse("15:04:05.999999999", strings.TrimSpace(s))
			if err != nil {
				return nil, fmt.Errorf("%q is not a time (want HH:MM:SS[.fff])", s)
			}
			return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute + time.Duration(t.Second())*time.Second + time.Duration(t.Nanosecond()), nil
		}
	case gocql.TypeBlob:
		if isStr {
			b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "0x"), `\x`))
			if err != nil {
				return nil, fmt.Errorf("%q is not hex (want 0x...)", s)
			}
			return b, nil
		}
	}
	return v, nil
}

var timestampLayouts = []string{"2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999-0700", "2006-01-02T15:04:05.999999999-0700", "2006-01-02"}

// parseTimestamp reads the layouts the grid prints and the ones cqlsh takes; a missing zone means UTC.
func parseTimestamp(s string) (time.Time, error) {
	for _, l := range timestampLayouts {
		if t, err := time.ParseInLocation(l, s, time.UTC); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not a timestamp (want YYYY-MM-DD[ HH:MM:SS[.fff]][+0000])", s)
}

// viaJSON says a value is written as fromJson(?): collections, tuples, UDTs and vectors (whose text form is JSON) and
// durations (whose text form is a CQL duration literal, which gocql cannot parse). fromJson is only legal in SET and VALUES.
func viaJSON(ti gocql.TypeInfo) (raw, ok bool) {
	switch ti.(type) {
	case gocql.CollectionType, gocql.TupleTypeInfo, gocql.UDTTypeInfo, gocql.VectorType:
		return true, true
	}
	if ti.Type() == gocql.TypeDuration {
		return false, true
	}
	return false, false
}

// stmt accumulates a statement's parameterized and display forms together.
type stmt struct {
	text, disp strings.Builder
	args       []any
}

func (b *stmt) raw(s string) { b.text.WriteString(s); b.disp.WriteString(s) }

func (b *stmt) arg(placeholder string, v any, shown string) {
	b.args = append(b.args, v)
	b.text.WriteString(placeholder)
	b.disp.WriteString(shown)
}

func (b *stmt) statement() db.Statement {
	return db.Statement{Text: b.text.String(), Args: b.args, Display: b.disp.String()}
}

var counterDelta = regexp.MustCompile(`^\s*([+-]\d+)\s*$`)

// value writes one cell. forKey values sit in WHERE, where fromJson is not allowed and NULL is not a key.
func (b *stmt) value(ti gocql.TypeInfo, col string, v any, forKey bool) error {
	if db.IsNull(v) || v == db.Default { // CQL has no column defaults: DEFAULT is NULL
		if forKey {
			return fmt.Errorf("key column %s is NULL: Cassandra cannot match it", col)
		}
		b.raw("NULL")
		return nil
	}
	if e, ok := v.(db.Expr); ok {
		if strings.EqualFold(string(e), "NOW()") && ti != nil { // the grid's NOW(): CQL's now() is a timeuuid, so convert for the column
			switch ti.Type() {
			case gocql.TypeTimestamp:
				e = "toTimestamp(now())"
			case gocql.TypeDate:
				e = "toDate(now())"
			}
		}
		b.raw(string(e))
		return nil
	}
	if v == db.Empty {
		v = ""
	}
	if ti != nil && !forKey {
		if raw, ok := viaJSON(ti); ok {
			s := fmt.Sprint(v)
			if raw && !json.Valid([]byte(s)) {
				return fmt.Errorf("column %s (%s): %q is not valid JSON", col, typeName(ti), s)
			}
			if !raw {
				jb, _ := json.Marshal(s)
				s = string(jb)
			}
			b.arg("fromJson(?)", s, "fromJson("+quoteLit(s)+")")
			return nil
		}
	}
	g, err := goValue(ti, v)
	if err != nil {
		return fmt.Errorf("column %s: %w", col, err)
	}
	b.arg("?", g, literal(g))
	return nil
}

// literal renders a value as CQL for Preview and history (never executed).
func literal(v any) string {
	switch x := v.(type) {
	case string:
		return quoteLit(x)
	case []byte:
		return "0x" + hex.EncodeToString(x)
	case time.Time:
		return quoteLit(x.UTC().Format("2006-01-02 15:04:05.000+0000"))
	case time.Duration:
		return quoteLit(clock(x))
	case net.IP:
		return quoteLit(x.String())
	case fmt.Stringer: // gocql.UUID, *inf.Dec, *big.Int
		return fmt.Sprint(x)
	}
	return fmt.Sprint(v)
}

func (b *stmt) where(types map[string]gocql.TypeInfo, key []db.KeyVal) error {
	b.raw(" WHERE ")
	for i, k := range key {
		if i > 0 {
			b.raw(" AND ")
		}
		b.raw(q(k.Col) + " = ")
		if err := b.value(types[k.Col], k.Col, k.Val, true); err != nil {
			return err
		}
	}
	return nil
}

// Plan turns the grid's pending edits into CQL. INSERT is an upsert (an existing key is overwritten without a warning),
// NULL cells are left out of INSERT (a NULL write is a tombstone) but written as NULL by UPDATE, which is also a tombstone.
// UPDATE and DELETE need the whole primary key, which is what the grid sends as Key.
func (s *session) Plan(changes []db.Change) ([]db.Statement, error) {
	var out []db.Statement
	for _, c := range changes {
		if c.Kind == db.Update { // Key is the primary key, and Cassandra cannot SET a key column
			for _, kv := range c.Set {
				for _, k := range c.Key {
					if k.Col == kv.Col {
						return nil, fmt.Errorf("primary key column %s of %s cannot be changed in place: insert a new row and delete the old one", kv.Col, c.Name)
					}
				}
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		types, err := s.colTypes(ctx, c.ObjRef)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("read the column types of %s: %w", c.Name, err)
		}
		qn, err := s.qual(c.ObjRef)
		if err != nil {
			return nil, err
		}
		b := &stmt{}
		switch c.Kind {
		case db.Insert:
			var cols []db.KeyVal
			for _, kv := range c.Set {
				if !db.IsNull(kv.Val) && kv.Val != db.Default {
					cols = append(cols, kv)
				}
			}
			if len(cols) == 0 {
				return nil, errors.New("insert needs at least the primary key columns")
			}
			names := make([]string, len(cols))
			for i, kv := range cols {
				names[i] = q(kv.Col)
				if ti := types[kv.Col]; ti != nil && ti.Type() == gocql.TypeCounter {
					return nil, fmt.Errorf("cannot insert into counter column %s: counters are only changed with UPDATE", kv.Col)
				}
			}
			b.raw("INSERT INTO " + qn + " (" + strings.Join(names, ", ") + ") VALUES (")
			for i, kv := range cols {
				if i > 0 {
					b.raw(", ")
				}
				if err := b.value(types[kv.Col], kv.Col, kv.Val, false); err != nil {
					return nil, err
				}
			}
			b.raw(")")
		case db.Update:
			if len(c.Set) == 0 {
				return nil, errors.New("update without changed columns")
			}
			if len(c.Key) == 0 {
				return nil, fmt.Errorf("cannot update %s: Cassandra needs the full primary key", c.Name)
			}
			b.raw("UPDATE " + qn + " SET ")
			for i, kv := range c.Set {
				if i > 0 {
					b.raw(", ")
				}
				b.raw(q(kv.Col) + " = ")
				if ti := types[kv.Col]; ti != nil && ti.Type() == gocql.TypeCounter {
					m := counterDelta.FindStringSubmatch(fmt.Sprint(kv.Val))
					if m == nil {
						return nil, fmt.Errorf("counter column %s can only be incremented: enter +5 or -3 (Cassandra cannot set a counter to a value)", kv.Col)
					}
					n, err := strconv.ParseInt(m[1], 10, 64)
					if err != nil {
						return nil, fmt.Errorf("counter column %s: %s does not fit in 64 bits", kv.Col, strings.TrimSpace(fmt.Sprint(kv.Val)))
					}
					b.raw(q(kv.Col) + " + ")
					b.arg("?", n, strconv.FormatInt(n, 10))
					continue
				}
				if err := b.value(types[kv.Col], kv.Col, kv.Val, false); err != nil {
					return nil, err
				}
			}
			if err := b.where(types, c.Key); err != nil {
				return nil, err
			}
		case db.Delete:
			if len(c.Key) == 0 {
				return nil, fmt.Errorf("cannot delete from %s: Cassandra needs the full primary key", c.Name)
			}
			b.raw("DELETE FROM " + qn)
			if err := b.where(types, c.Key); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unknown change kind %d", c.Kind)
		}
		out = append(out, b.statement())
	}
	return out, nil
}

var (
	dmlRe     = regexp.MustCompile(`(?i)^\s*(insert|update|delete)\b`)
	counterRe = regexp.MustCompile(`= "[^"]+" \+ \?`)
)

// maxBatch keeps a batch under Cassandra's batch_size_fail_threshold (50 KiB by default) for ordinary rows.
const maxBatch = 100

// Apply runs the statements. Cassandra has no transactions: when every statement is an INSERT/UPDATE/DELETE (and none is a
// counter update, which cannot join a logged batch) they go in one LOGGED BATCH, which the cluster applies all-or-nothing
// (eventually, and without isolation). Anything else, or more than maxBatch statements, runs one by one and stops at the
// first error, and the error says how many were already applied.
func (s *session) Apply(ctx context.Context, stmts []db.Statement) error {
	defer s.invalidate() // DDL may have changed column lists
	if batchable(stmts) {
		b := s.sess().Batch(gocql.LoggedBatch).Consistency(s.consistency())
		for _, st := range stmts {
			b.Query(st.Text, st.Args...)
		}
		if err := b.ExecContext(ctx); err != nil {
			var wt *gocql.RequestErrWriteTimeout
			if errors.As(err, &wt) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, gocql.ErrTimeoutNoResponse) {
				return fmt.Errorf("batch of %d statements: %w (timed out: the batch may still be applied, verify the data before retrying)", len(stmts), err)
			}
			return fmt.Errorf("batch of %d statements: %w (a logged batch is all-or-nothing, nothing was applied)", len(stmts), err)
		}
		return nil
	}
	for i, st := range stmts {
		if err := s.query(st.Text, st.Args...).ExecContext(ctx); err != nil {
			return fmt.Errorf("statement %d (%s): %w (%d of %d statements were already applied and stay applied: Cassandra cannot roll back)",
				i+1, st.Shown(), err, i, len(stmts))
		}
	}
	return nil
}

// tableRe captures the table an INSERT/UPDATE/DELETE statement targets.
var tableRe = regexp.MustCompile(`(?i)^\s*(?:insert\s+into|update|delete\s+from)\s+(\S+)`)

// batchable reports whether the statements may share one logged batch. A batch has a single timestamp, so a DELETE that
// shares a table with any other statement (a delete-then-insert of the same key) would swallow it: those run one by one.
// shortcut: two INSERTs of the same key in one save still tie on the timestamp; the grids do not produce them.
func batchable(stmts []db.Statement) bool {
	if len(stmts) < 2 || len(stmts) > maxBatch {
		return false
	}
	del, other := map[string]bool{}, map[string]bool{}
	for _, st := range stmts {
		if !dmlRe.MatchString(st.Text) || counterRe.MatchString(st.Text) {
			return false
		}
		m := tableRe.FindStringSubmatch(st.Text)
		if m == nil {
			continue
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(st.Text)), "delete") {
			del[m[1]] = true
		} else {
			other[m[1]] = true
		}
	}
	for t := range del {
		if other[t] {
			return false
		}
	}
	return true
}
