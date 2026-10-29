package mongo

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"webtuidb/internal/db"
)

// rawJS is text emitted verbatim into generated shell statements.
type rawJS string

func typeName(v any) string {
	switch v.(type) {
	case nil, bson.Null:
		return "null"
	case string:
		return "string"
	case int32:
		return "int32"
	case int64:
		return "int64"
	case float64:
		return "double"
	case bool:
		return "bool"
	case bson.DateTime, time.Time:
		return "date"
	case bson.ObjectID:
		return "objectId"
	case bson.D, bson.M:
		return "object"
	case bson.A:
		return "array"
	case bson.Decimal128:
		return "decimal"
	case bson.Binary:
		return "binData"
	case bson.Timestamp:
		return "timestamp"
	}
	return "other"
}

// extJSON renders any BSON value as compact relaxed extended JSON.
func extJSON(v any) string {
	b, err := bson.MarshalExtJSON(bson.D{{Key: "v", Value: v}}, false, false)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b[len(`{"v":`) : len(b)-1])
}

// cell converts a decoded BSON value to the normalized Result cell.
func cell(v any) any {
	switch x := v.(type) {
	case nil, bson.Null, bson.Undefined:
		return nil
	case string, bool, int64, float64:
		return x
	case int32:
		return int64(x)
	case bson.DateTime:
		return x.Time().UTC() // UTC like the JSON view's $date and parseDate, so an edit round-trips
	case time.Time:
		return x.UTC()
	case bson.ObjectID:
		return x.Hex()
	case bson.Binary:
		return x.Data
	case bson.Decimal128:
		return x.String()
	}
	return extJSON(v)
}

func jsonString(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

// jsLit renders v for a shell statement: readable spacing, extended JSON for BSON-only types.
func jsLit(v any) string {
	switch x := v.(type) {
	case rawJS:
		return string(x)
	case string:
		return jsonString(x)
	case bson.D:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = jsonString(e.Key) + ": " + jsLit(e.Value)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case bson.A:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = jsLit(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case float64: // integral doubles would otherwise come back as ints
		if x == math.Trunc(x) && !math.IsInf(x, 0) && math.Abs(x) < 1e15 {
			return `{"$numberDouble": "` + strconv.FormatFloat(x, 'f', 1, 64) + `"}`
		}
	case int64:
		return `{"$numberLong": "` + strconv.FormatInt(x, 10) + `"}`
	}
	return extJSON(v)
}

var dateLayouts = []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02"}

func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad date %q", s)
}

// coerceTyped converts s to the Go value of a known scalar BSON type; ok=false when typ is not one
// of those (callers decide what to do) and err reports an unparsable value.
func coerceTyped(s, typ string) (v any, ok bool, err error) {
	t := strings.TrimSpace(s)
	switch typ {
	case "string":
		return s, true, nil
	case "int32":
		n, e := strconv.ParseInt(t, 10, 32)
		return int32(n), true, e
	case "int64":
		n, e := strconv.ParseInt(t, 10, 64)
		return n, true, e
	case "double":
		f, e := strconv.ParseFloat(t, 64)
		return f, true, e
	case "bool":
		b, e := strconv.ParseBool(t)
		return b, true, e
	case "date":
		d, e := parseDate(t)
		return bson.NewDateTimeFromTime(d), true, e
	case "objectId":
		o, e := bson.ObjectIDFromHex(t)
		return o, true, e
	case "decimal":
		d, e := bson.ParseDecimal128(t)
		return d, true, e
	case "binData": // the grid shows binary as \x<hex> (db.FormatValue); the subtype is not kept, so it is generic
		b, e := hex.DecodeString(strings.TrimPrefix(t, `\x`))
		return bson.Binary{Data: b}, true, e
	}
	return nil, false, nil
}

// parseJSONValue reads s as one extended JSON value (number, bool, null, object, array...).
func parseJSONValue(s string) (any, bool) {
	var w bson.D
	if err := bson.UnmarshalExtJSON([]byte(`{"v":`+s+`}`), false, &w); err != nil || len(w) != 1 {
		return nil, false
	}
	return w[0].Value, true
}

// coerceSet turns a GUI edit value into the BSON value to store, guided by the field's cached type.
func coerceSet(v any, typ, field string) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case db.Expr:
		if e := strings.ToUpper(string(x)); e == "NOW()" || e == "CURRENT_TIMESTAMP" {
			return rawJS("new Date()"), nil
		}
		return rawJS(x), nil
	case []byte:
		if typ == "binData" {
			return bson.Binary{Data: x}, nil
		}
		return string(x), nil
	case string:
		tv, ok, err := coerceTyped(x, typ)
		if err != nil {
			return nil, fmt.Errorf("field %s: cannot convert %q to %s", field, x, typ)
		}
		if ok {
			return tv, nil
		}
		if jv, ok := parseJSONValue(x); ok {
			return jv, nil
		}
		return x, nil
	}
	switch {
	case db.IsNull(v):
		return nil, nil
	case v == db.Empty:
		return "", nil
	}
	return v, nil
}

// Doc extracts a nested value by dotted path from a decoded document.
func lookup(d bson.D, path string) (any, bool) {
	var cur any = d
	for _, p := range strings.Split(path, ".") {
		dd, ok := cur.(bson.D)
		if !ok {
			return nil, false
		}
		found := false
		for _, e := range dd {
			if e.Key == p {
				cur, found = e.Value, true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	return cur, true
}

func toInt(v any) int64 {
	switch x := v.(type) {
	case int32:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	}
	return 0
}

// render builds a Result from documents. known are cached field names that keep the grid stable.
func render(docs []bson.D, known []string, ktypes map[string]string) *db.Result {
	idx := map[string]int{}
	var names []string
	add := func(n string) {
		if _, ok := idx[n]; !ok {
			idx[n] = len(names)
			names = append(names, n)
		}
	}
	for _, n := range known {
		add(n)
	}
	if len(known) == 0 {
		for _, d := range docs {
			if _, ok := lookup(d, "_id"); ok {
				add("_id")
				break
			}
		}
	}
	for _, d := range docs {
		for _, e := range d {
			add(e.Key)
		}
	}
	seen := make([]map[string]bool, len(names))
	res := &db.Result{HasRows: true, Columns: make([]db.ResultColumn, len(names))}
	for i := range seen {
		seen[i] = map[string]bool{}
	}
	for _, d := range docs {
		row := make([]any, len(names))
		for _, e := range d {
			i := idx[e.Key]
			row[i] = cell(e.Value)
			seen[i][typeName(e.Value)] = true
		}
		res.Rows = append(res.Rows, row)
		if b, err := bson.MarshalExtJSON(d, false, false); err == nil {
			res.Docs = append(res.Docs, string(b))
		} else {
			res.Docs = append(res.Docs, "{}")
		}
	}
	for i, n := range names {
		t := ktypes[n]
		if len(seen[i]) > 1 {
			delete(seen[i], "null")
		}
		switch len(seen[i]) {
		case 0:
		case 1:
			for k := range seen[i] {
				t = k
			}
		default:
			t = "mixed"
		}
		res.Columns[i] = db.ResultColumn{Name: n, Type: t}
	}
	return res
}

// project keeps only the wanted columns of res (Docs stay whole); unknown names are ignored.
func project(res *db.Result, want []string) *db.Result {
	if len(want) == 0 {
		return res
	}
	var keep []int
	for _, w := range want {
		for i, c := range res.Columns {
			if c.Name == w {
				keep = append(keep, i)
			}
		}
	}
	if len(keep) == 0 {
		return res
	}
	cols := make([]db.ResultColumn, len(keep))
	for j, i := range keep {
		cols[j] = res.Columns[i]
	}
	for r, row := range res.Rows {
		out := make([]any, len(keep))
		for j, i := range keep {
			out[j] = row[i]
		}
		res.Rows[r] = out
	}
	res.Columns = cols
	return res
}
