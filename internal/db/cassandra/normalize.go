package cassandra

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"gopkg.in/inf.v0"
)

// reader scans result rows. Every column is scanned into a **T so NULL (nil) is told apart from the zero value;
// a tuple column occupies one destination per element, as gocql expects.
type reader struct {
	cols []gocql.ColumnInfo
	dest []any
	ptrs [][]reflect.Value // per column: its destination pointers (several for a tuple)
}

func newReader(cols []gocql.ColumnInfo) *reader {
	r := &reader{cols: cols, ptrs: make([][]reflect.Value, len(cols))}
	for i, c := range cols {
		elems := []gocql.TypeInfo{c.TypeInfo}
		if t, ok := c.TypeInfo.(gocql.TupleTypeInfo); ok {
			elems = t.Elems
		}
		for _, e := range elems {
			p := reflect.New(reflect.PointerTo(reflect.TypeOf(e.Zero())))
			r.ptrs[i] = append(r.ptrs[i], p)
			r.dest = append(r.dest, p.Interface())
		}
	}
	return r
}

// row converts the last scanned row.
func (r *reader) row() []any {
	out := make([]any, len(r.cols))
	for i, c := range r.cols {
		if t, ok := c.TypeInfo.(gocql.TupleTypeInfo); ok {
			vals := make([]any, len(t.Elems))
			have := false
			for j, p := range r.ptrs[i] {
				if !p.Elem().IsNil() {
					vals[j], have = p.Elem().Elem().Interface(), true
				}
			}
			if have {
				out[i] = jsonText(c.TypeInfo, vals)
			}
			continue
		}
		if p := r.ptrs[i][0].Elem(); !p.IsNil() {
			out[i] = normalize(c.TypeInfo, p.Elem().Interface())
		}
	}
	return out
}

// normalize converts a driver value to the shapes documented on db.Result. Scalars become native values,
// collections, tuples, UDTs and vectors become JSON text (the same text fromJson() takes back when editing).
func normalize(ti gocql.TypeInfo, v any) any {
	switch ti.(type) {
	case gocql.CollectionType, gocql.TupleTypeInfo, gocql.UDTTypeInfo, gocql.VectorType:
		return jsonText(ti, v)
	}
	switch x := v.(type) {
	case int:
		return int64(x)
	case int8:
		return int64(x)
	case int16:
		return int64(x)
	case int32:
		return int64(x)
	case float32: // 0.1f must show as 0.1, not 0.10000000149011612
		f, _ := strconv.ParseFloat(strconv.FormatFloat(float64(x), 'g', -1, 32), 64)
		return f
	case gocql.UUID:
		return x.String()
	case net.IP:
		return x.String()
	case *inf.Dec:
		return x.String()
	case *big.Int:
		if x.IsInt64() {
			return x.Int64()
		}
		return x.String()
	case time.Time:
		if ti.Type() == gocql.TypeDate {
			return x.UTC().Format("2006-01-02")
		}
		return x.UTC()
	case time.Duration: // the time type: nanoseconds since midnight
		return clock(x)
	case gocql.Duration:
		return durationText(x)
	}
	return v
}

func clock(d time.Duration) string {
	t := time.Unix(0, 0).UTC().Add(d)
	return t.Format("15:04:05.999999999")
}

// durationText is a CQL duration literal, which round-trips through fromJson().
func durationText(d gocql.Duration) string {
	if d == (gocql.Duration{}) {
		return "0s"
	}
	var b strings.Builder
	for _, p := range []struct {
		n int64
		u string
	}{{int64(d.Months), "mo"}, {int64(d.Days), "d"}, {d.Nanoseconds, "ns"}} {
		if p.n != 0 {
			b.WriteString(strconv.FormatInt(p.n, 10) + p.u)
		}
	}
	return b.String()
}

func jsonText(ti gocql.TypeInfo, v any) string {
	b, err := json.Marshal(jsonValue(ti, v))
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// jsonValue builds the JSON-ready form of v following its CQL type, so dates, decimals and uuids inside a collection read
// the way the server's own toJson() writes them.
func jsonValue(ti gocql.TypeInfo, v any) any {
	rv := reflect.ValueOf(v)
	switch t := ti.(type) {
	case gocql.CollectionType:
		if rv.Kind() == reflect.Map {
			m := make(map[string]any, rv.Len())
			for it := rv.MapRange(); it.Next(); {
				m[fmt.Sprint(jsonValue(t.Key, it.Key().Interface()))] = jsonValue(t.Elem, it.Value().Interface())
			}
			return m
		}
		return jsonSlice(t.Elem, rv)
	case gocql.VectorType:
		return jsonSlice(t.SubType, rv)
	case gocql.TupleTypeInfo:
		out := make([]any, len(t.Elems))
		if rv.Kind() == reflect.Slice {
			for i := range out {
				if i < rv.Len() && !rv.Index(i).IsNil() {
					out[i] = jsonValue(t.Elems[i], rv.Index(i).Interface())
				}
			}
		}
		return out
	case gocql.UDTTypeInfo:
		m := map[string]any{}
		if mv, ok := v.(map[string]any); ok {
			for _, f := range t.Elements {
				if fv, ok := mv[f.Name]; ok && fv != nil {
					m[f.Name] = jsonValue(f.Type, fv)
				} else {
					m[f.Name] = nil
				}
			}
		}
		return m
	}
	if v == nil {
		return nil
	}
	switch x := normalize(ti, v).(type) {
	case []byte:
		return "0x" + hex.EncodeToString(x)
	case time.Time:
		return x.Format("2006-01-02 15:04:05.000+0000")
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return strconv.FormatFloat(x, 'g', -1, 64)
		}
		return x
	default:
		return x
	}
}

func jsonSlice(elem gocql.TypeInfo, rv reflect.Value) []any {
	out := make([]any, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out = append(out, jsonValue(elem, rv.Index(i).Interface()))
	}
	return out
}

var typeNames = map[gocql.Type]string{
	gocql.TypeAscii: "ascii", gocql.TypeBigInt: "bigint", gocql.TypeBlob: "blob", gocql.TypeBoolean: "boolean", gocql.TypeCounter: "counter",
	gocql.TypeDecimal: "decimal", gocql.TypeDouble: "double", gocql.TypeFloat: "float", gocql.TypeInt: "int", gocql.TypeText: "text",
	gocql.TypeTimestamp: "timestamp", gocql.TypeUUID: "uuid", gocql.TypeVarchar: "text", gocql.TypeVarint: "varint",
	gocql.TypeTimeUUID: "timeuuid", gocql.TypeInet: "inet", gocql.TypeDate: "date", gocql.TypeTime: "time", gocql.TypeSmallInt: "smallint",
	gocql.TypeTinyInt: "tinyint", gocql.TypeDuration: "duration",
}

// typeName spells a result column's CQL type.
func typeName(ti gocql.TypeInfo) string {
	switch t := ti.(type) {
	case gocql.CollectionType:
		switch t.Type() {
		case gocql.TypeMap:
			return "map<" + typeName(t.Key) + ", " + typeName(t.Elem) + ">"
		case gocql.TypeSet:
			return "set<" + typeName(t.Elem) + ">"
		}
		return "list<" + typeName(t.Elem) + ">"
	case gocql.TupleTypeInfo:
		parts := make([]string, len(t.Elems))
		for i, e := range t.Elems {
			parts[i] = typeName(e)
		}
		return "tuple<" + strings.Join(parts, ", ") + ">"
	case gocql.UDTTypeInfo:
		return t.Name
	case gocql.VectorType:
		return fmt.Sprintf("vector<%s, %d>", typeName(t.SubType), t.Dimensions)
	}
	if n, ok := typeNames[ti.Type()]; ok {
		return n
	}
	return "custom"
}
