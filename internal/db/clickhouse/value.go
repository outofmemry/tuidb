package clickhouse

import (
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"regexp"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

var decimalScaleRe = regexp.MustCompile(`^(?:Nullable\()?Decimal\w*\(\s*(?:\d+\s*,\s*)?(\d+)\s*\)`)

// normalize maps a scanned ClickHouse value to the types db.Result documents.
// typ is the ClickHouse type name, used to keep Decimal scale.
func normalize(v any, typ string) any {
	rv := reflect.ValueOf(v)
	for rv.IsValid() && (rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface) {
		if rv.IsNil() {
			return nil
		}
		if bi, ok := rv.Interface().(*big.Int); ok {
			return bi.String()
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil
	}
	switch x := rv.Interface().(type) {
	case decimal.Decimal:
		if m := decimalScaleRe.FindStringSubmatch(typ); m != nil {
			scale, _ := strconv.Atoi(m[1])
			return x.StringFixed(int32(scale))
		}
		return x.String()
	case time.Time:
		return x
	case fmt.Stringer: // UUID, IPv4/IPv6
		return x.String()
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint()
	case reflect.Float32: // shortest float32 repr, so 0.1 stays 0.1
		f, _ := strconv.ParseFloat(strconv.FormatFloat(rv.Float(), 'g', -1, 32), 64)
		return f
	case reflect.Float64:
		return rv.Float()
	case reflect.Bool:
		return rv.Bool()
	case reflect.String:
		return rv.String()
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Struct:
		if b, err := json.Marshal(rv.Interface()); err == nil {
			return string(b)
		}
	}
	return fmt.Sprint(rv.Interface())
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
