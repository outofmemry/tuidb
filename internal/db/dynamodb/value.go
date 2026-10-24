package dynamodb

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"tuidb/internal/db"
)

// typeName is the DynamoDB attribute type shown as the column type.
func typeName(av types.AttributeValue) string {
	switch av.(type) {
	case *types.AttributeValueMemberS:
		return "S"
	case *types.AttributeValueMemberN:
		return "N"
	case *types.AttributeValueMemberB:
		return "B"
	case *types.AttributeValueMemberBOOL:
		return "BOOL"
	case *types.AttributeValueMemberNULL:
		return "NULL"
	case *types.AttributeValueMemberL:
		return "L"
	case *types.AttributeValueMemberM:
		return "M"
	case *types.AttributeValueMemberSS:
		return "SS"
	case *types.AttributeValueMemberNS:
		return "NS"
	case *types.AttributeValueMemberBS:
		return "BS"
	}
	return "?"
}

// cell converts an attribute to a grid value. Numbers that are integers become int64; every other
// number stays its exact decimal text, because a float64 would lose DynamoDB's 38 digits. Lists, maps
// and sets become JSON text.
func cell(av types.AttributeValue) any {
	switch v := av.(type) {
	case *types.AttributeValueMemberS:
		return v.Value
	case *types.AttributeValueMemberN:
		if i, err := strconv.ParseInt(v.Value, 10, 64); err == nil {
			return i
		}
		return v.Value
	case *types.AttributeValueMemberB:
		return v.Value
	case *types.AttributeValueMemberBOOL:
		return v.Value
	case *types.AttributeValueMemberNULL:
		return nil
	}
	return jsonText(av, true)
}

// toJSON renders an attribute as JSON that round-trips through fromJSON. Plain JSON covers S, N (as an
// exact json.Number), BOOL, NULL, L and M; binary and sets, which JSON cannot express, use one-key
// wrappers {"$binary": base64}, {"$ss": [...]}, {"$ns": [...]}, {"$bs": [...]}. At the top level of a
// cell the column type already says "set", so sets print as bare arrays (top).
func toJSON(av types.AttributeValue, top bool) any {
	switch v := av.(type) {
	case *types.AttributeValueMemberS:
		return v.Value
	case *types.AttributeValueMemberN:
		return json.Number(v.Value)
	case *types.AttributeValueMemberB:
		return map[string]any{"$binary": base64.StdEncoding.EncodeToString(v.Value)}
	case *types.AttributeValueMemberBOOL:
		return v.Value
	case *types.AttributeValueMemberNULL:
		return nil
	case *types.AttributeValueMemberL:
		out := make([]any, len(v.Value))
		for i, e := range v.Value {
			out[i] = toJSON(e, false)
		}
		return out
	case *types.AttributeValueMemberM:
		out := make(map[string]any, len(v.Value))
		for k, e := range v.Value {
			out[k] = toJSON(e, false)
		}
		return out
	case *types.AttributeValueMemberSS:
		return wrapSet("$ss", top, stringsAny(v.Value))
	case *types.AttributeValueMemberNS:
		out := make([]any, len(v.Value))
		for i, n := range v.Value {
			out[i] = json.Number(n)
		}
		return wrapSet("$ns", top, out)
	case *types.AttributeValueMemberBS:
		out := make([]any, len(v.Value))
		for i, b := range v.Value {
			out[i] = base64.StdEncoding.EncodeToString(b)
		}
		return wrapSet("$bs", top, out)
	}
	return nil
}

func stringsAny(s []string) []any {
	out := make([]any, len(s))
	for i, x := range s {
		out[i] = x
	}
	return out
}

func wrapSet(key string, top bool, items []any) any {
	if top {
		return items
	}
	return map[string]any{key: items}
}

func jsonText(av types.AttributeValue, top bool) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(toJSON(av, top)); err != nil {
		return "null"
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// itemJSON is the Json/Tree view of one item.
func itemJSON(item map[string]types.AttributeValue) string {
	return jsonText(&types.AttributeValueMemberM{Value: item}, false)
}

// parseJSON decodes exactly one JSON value, keeping numbers as text.
func parseJSON(text string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("unexpected text after the JSON value")
	}
	return v, nil
}

var numRe = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$`)

func numAttr(s string) (types.AttributeValue, error) {
	s = strings.TrimSpace(s)
	if !numRe.MatchString(s) {
		return nil, fmt.Errorf("%q is not a number", s)
	}
	return &types.AttributeValueMemberN{Value: s}, nil
}

// fromJSON converts a decoded JSON value (see toJSON) to an attribute.
func fromJSON(v any) (types.AttributeValue, error) {
	switch x := v.(type) {
	case nil:
		return &types.AttributeValueMemberNULL{Value: true}, nil
	case bool:
		return &types.AttributeValueMemberBOOL{Value: x}, nil
	case json.Number:
		return numAttr(x.String())
	case string:
		return &types.AttributeValueMemberS{Value: x}, nil
	case []any:
		l := make([]types.AttributeValue, len(x))
		for i, e := range x {
			a, err := fromJSON(e)
			if err != nil {
				return nil, err
			}
			l[i] = a
		}
		return &types.AttributeValueMemberL{Value: l}, nil
	case map[string]any:
		if len(x) == 1 {
			for k, e := range x {
				switch k {
				case "$binary":
					s, ok := e.(string)
					if !ok {
						return nil, errors.New("$binary needs a base64 string")
					}
					b, err := base64.StdEncoding.DecodeString(s)
					if err != nil {
						return nil, fmt.Errorf("$binary: %w", err)
					}
					return &types.AttributeValueMemberB{Value: b}, nil
				case "$ss", "$ns", "$bs":
					arr, ok := e.([]any)
					if !ok {
						return nil, fmt.Errorf("%s needs an array", k)
					}
					return setOf(k[1:], arr)
				}
			}
		}
		m := make(map[string]types.AttributeValue, len(x))
		for k, e := range x {
			a, err := fromJSON(e)
			if err != nil {
				return nil, err
			}
			m[k] = a
		}
		return &types.AttributeValueMemberM{Value: m}, nil
	}
	return nil, fmt.Errorf("unsupported JSON value %v", v)
}

// setOf builds SS, NS or BS ("ss", "ns", "bs") from JSON array elements; DynamoDB sets cannot be empty
// or hold duplicates.
func setOf(kind string, arr []any) (types.AttributeValue, error) {
	if len(arr) == 0 {
		return nil, errors.New("a DynamoDB set cannot be empty")
	}
	seen := map[string]bool{}
	var ss, ns []string
	var bs [][]byte
	for _, e := range arr {
		var key string
		switch kind {
		case "ss":
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("string set element %v is not a string", e)
			}
			key = s
			ss = append(ss, s)
		case "ns":
			n, ok := e.(json.Number)
			if !ok {
				if s, isStr := e.(string); isStr {
					n, ok = json.Number(s), numRe.MatchString(strings.TrimSpace(s))
				}
			}
			if !ok {
				return nil, fmt.Errorf("number set element %v is not a number", e)
			}
			key = n.String()
			ns = append(ns, key)
		default:
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("binary set element %v is not base64 text", e)
			}
			b, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				return nil, fmt.Errorf("binary set element: %w", err)
			}
			key = string(b)
			bs = append(bs, b)
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate set element %q", key)
		}
		seen[key] = true
	}
	switch kind {
	case "ss":
		return &types.AttributeValueMemberSS{Value: ss}, nil
	case "ns":
		return &types.AttributeValueMemberNS{Value: ns}, nil
	}
	return &types.AttributeValueMemberBS{Value: bs}, nil
}

// parseBinary reads a binary cell the way FormatValue prints it (\xHEX) or plain base64.
func parseBinary(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if h, ok := strings.CutPrefix(s, `\x`); ok {
		return hex.DecodeString(h)
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, errors.New(`binary values are written as \xHEX or base64`)
	}
	return b, nil
}

// coerce turns a GUI edit value into the attribute to store, guided by the column's type (typ is the
// attribute type from the key schema or the sampled items). Text typed into a column of unknown or mixed
// type is read as JSON when it parses as a number, boolean, null, array or object, and as a string
// otherwise (the same rule the MongoDB driver uses).
func coerce(v any, typ, col string) (types.AttributeValue, error) {
	bad := func(err error) (types.AttributeValue, error) {
		return nil, fmt.Errorf("attribute %s: cannot convert to %s: %w", col, typ, err)
	}
	switch x := v.(type) {
	case nil:
		return &types.AttributeValueMemberNULL{Value: true}, nil
	case types.AttributeValue:
		return x, nil
	case db.Expr:
		if e := strings.ToUpper(string(x)); e == "NOW()" || e == "CURRENT_TIMESTAMP" {
			if typ == "N" {
				return &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Unix(), 10)}, nil
			}
			return &types.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339)}, nil
		}
		return nil, fmt.Errorf("attribute %s: expression %s is not supported by DynamoDB", col, x)
	case []byte:
		return &types.AttributeValueMemberB{Value: x}, nil
	case int64:
		return &types.AttributeValueMemberN{Value: strconv.FormatInt(x, 10)}, nil
	case uint64:
		return &types.AttributeValueMemberN{Value: strconv.FormatUint(x, 10)}, nil
	case float64:
		return &types.AttributeValueMemberN{Value: strconv.FormatFloat(x, 'g', -1, 64)}, nil
	case bool:
		return &types.AttributeValueMemberBOOL{Value: x}, nil
	case time.Time:
		return &types.AttributeValueMemberS{Value: x.UTC().Format(time.RFC3339Nano)}, nil
	case string:
		return coerceText(x, typ, col, bad)
	}
	switch {
	case db.IsNull(v):
		return &types.AttributeValueMemberNULL{Value: true}, nil
	case v == db.Empty:
		return &types.AttributeValueMemberS{Value: ""}, nil
	}
	return nil, fmt.Errorf("attribute %s: unsupported value %v", col, v)
}

func coerceText(x, typ, col string, bad func(error) (types.AttributeValue, error)) (types.AttributeValue, error) {
	switch typ {
	case "S":
		return &types.AttributeValueMemberS{Value: x}, nil
	case "N":
		a, err := numAttr(x)
		if err != nil {
			return bad(err)
		}
		return a, nil
	case "B":
		b, err := parseBinary(x)
		if err != nil {
			return bad(err)
		}
		return &types.AttributeValueMemberB{Value: b}, nil
	case "BOOL":
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "1", "yes":
			return &types.AttributeValueMemberBOOL{Value: true}, nil
		case "false", "0", "no":
			return &types.AttributeValueMemberBOOL{Value: false}, nil
		}
		return bad(errors.New("want true or false"))
	case "NULL":
		if t := strings.TrimSpace(x); t == "" || strings.EqualFold(t, "null") {
			return &types.AttributeValueMemberNULL{Value: true}, nil
		}
	case "L", "M", "SS", "NS", "BS":
		j, err := parseJSON(x)
		if err != nil {
			return bad(err)
		}
		switch typ {
		case "L", "M":
			a, err := fromJSON(j)
			if err != nil {
				return bad(err)
			}
			if typeName(a) != typ {
				return bad(fmt.Errorf("got %s", typeName(a)))
			}
			return a, nil
		default:
			arr, ok := j.([]any)
			if !ok {
				return bad(errors.New("want a JSON array"))
			}
			a, err := setOf(strings.ToLower(typ), arr)
			if err != nil {
				return bad(err)
			}
			return a, nil
		}
	}
	if j, err := parseJSON(x); err == nil {
		if a, err := fromJSON(j); err == nil {
			return a, nil
		}
	}
	return &types.AttributeValueMemberS{Value: x}, nil
}

// literal renders an attribute as a PartiQL literal for Preview and history. Binary has no PartiQL
// literal, so it prints as a hex marker (the real statement sends it as a bound parameter).
func literal(av types.AttributeValue) string {
	switch v := av.(type) {
	case *types.AttributeValueMemberS:
		return "'" + strings.ReplaceAll(v.Value, "'", "''") + "'"
	case *types.AttributeValueMemberN:
		return v.Value
	case *types.AttributeValueMemberB:
		return fmt.Sprintf("'\\x%x'", v.Value)
	case *types.AttributeValueMemberBOOL:
		return strconv.FormatBool(v.Value)
	case *types.AttributeValueMemberNULL:
		return "NULL"
	case *types.AttributeValueMemberL:
		parts := make([]string, len(v.Value))
		for i, e := range v.Value {
			parts[i] = literal(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *types.AttributeValueMemberM:
		keys := make([]string, 0, len(v.Value))
		for k := range v.Value {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = "'" + strings.ReplaceAll(k, "'", "''") + "': " + literal(v.Value[k])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *types.AttributeValueMemberSS:
		parts := make([]string, len(v.Value))
		for i, e := range v.Value {
			parts[i] = literal(&types.AttributeValueMemberS{Value: e})
		}
		return "<<" + strings.Join(parts, ", ") + ">>"
	case *types.AttributeValueMemberNS:
		return "<<" + strings.Join(v.Value, ", ") + ">>"
	case *types.AttributeValueMemberBS:
		parts := make([]string, len(v.Value))
		for i, e := range v.Value {
			parts[i] = literal(&types.AttributeValueMemberB{Value: e})
		}
		return "<<" + strings.Join(parts, ", ") + ">>"
	}
	return "NULL"
}
