package db

import (
	"encoding/binary"
	"errors"
	"math"
	"strconv"
	"strings"
)

// WKT decodes (E)WKB (PostGIS) or MySQL's internal geometry value (4-byte SRID + WKB) to WKT.
// Point, LineString, Polygon, their Multi* forms and GeometryCollection are supported, in little or big endian,
// with the EWKB SRID flag and Z/M ordinates (ISO or EWKB style). The SRID is dropped. ok is false for anything else.
func WKT(b []byte) (string, bool) {
	if s, ok := wktFrom(b); ok {
		return s, true
	}
	if len(b) > 4 {
		return wktFrom(b[4:]) // MySQL prefixes the SRID
	}
	return "", false
}

func wktFrom(b []byte) (string, bool) {
	d := &wkbReader{b: b}
	var sb strings.Builder
	if d.geom(&sb, 0) != nil || len(d.b) != 0 {
		return "", false
	}
	return sb.String(), true
}

type wkbReader struct{ b []byte }

var errWKB = errors.New("bad wkb")

func (d *wkbReader) u32(o binary.ByteOrder) (uint32, error) {
	if len(d.b) < 4 {
		return 0, errWKB
	}
	v := o.Uint32(d.b)
	d.b = d.b[4:]
	return v, nil
}

func (d *wkbReader) geom(sb *strings.Builder, depth int) error {
	if depth > 64 || len(d.b) < 5 { // each level costs >= 9 bytes, so this only bounds the recursion
		return errWKB
	}
	var o binary.ByteOrder
	switch d.b[0] {
	case 0:
		o = binary.BigEndian
	case 1:
		o = binary.LittleEndian
	default:
		return errWKB
	}
	d.b = d.b[1:]
	t, err := d.u32(o)
	if err != nil {
		return err
	}
	hasZ, hasM := t&0x80000000 != 0, t&0x40000000 != 0
	if t&0x20000000 != 0 { // EWKB SRID
		if _, err := d.u32(o); err != nil {
			return err
		}
	}
	t &= 0x0fffffff
	switch t / 1000 { // ISO codes: 1000 Z, 2000 M, 3000 ZM
	case 1:
		hasZ = true
	case 2:
		hasM = true
	case 3:
		hasZ, hasM = true, true
	}
	t %= 1000
	dim := 2
	tag := ""
	if hasZ {
		dim++
		tag = "Z"
	}
	if hasM {
		dim++
		tag += "M"
	}
	if tag != "" {
		tag = " " + tag
	}
	names := map[uint32]string{1: "POINT", 2: "LINESTRING", 3: "POLYGON", 4: "MULTIPOINT", 5: "MULTILINESTRING", 6: "MULTIPOLYGON", 7: "GEOMETRYCOLLECTION"}
	name, ok := names[t]
	if !ok {
		return errWKB
	}
	sb.WriteString(name + tag)
	switch t {
	case 1:
		p, empty, err := d.point(o, dim)
		if err != nil {
			return err
		}
		if empty {
			sb.WriteString(" EMPTY")
		} else {
			sb.WriteString("(" + p + ")")
		}
	case 2:
		return d.list(sb, o, dim, d.coords)
	case 3:
		return d.list(sb, o, dim, d.ring)
	case 4, 5, 6, 7:
		n, err := d.u32(o)
		if err != nil || int(n) > len(d.b) {
			return errWKB
		}
		if n == 0 {
			sb.WriteString(" EMPTY")
			return nil
		}
		sb.WriteString("(")
		for i := uint32(0); i < n; i++ {
			if i > 0 {
				sb.WriteString(",")
			}
			var part strings.Builder
			if err := d.geom(&part, depth+1); err != nil {
				return err
			}
			s := part.String()
			if t != 7 { // members of Multi* omit their type name: MULTIPOINT((1 2),(3 4))
				if i := strings.IndexByte(s, '('); i >= 0 {
					s = s[i:]
				} else {
					s = "EMPTY"
				}
			}
			sb.WriteString(s)
		}
		sb.WriteString(")")
	}
	return nil
}

func (d *wkbReader) point(o binary.ByteOrder, dim int) (string, bool, error) {
	if len(d.b) < 8*dim {
		return "", false, errWKB
	}
	parts := make([]string, dim)
	empty := false
	for i := range parts {
		f := math.Float64frombits(o.Uint64(d.b))
		d.b = d.b[8:]
		if math.IsNaN(f) {
			empty = true
		}
		parts[i] = strconv.FormatFloat(f, 'g', -1, 64)
	}
	return strings.Join(parts, " "), empty, nil
}

// list writes "(" item,item ")" where the count precedes the items.
func (d *wkbReader) list(sb *strings.Builder, o binary.ByteOrder, dim int, item func(*strings.Builder, binary.ByteOrder, int) error) error {
	n, err := d.u32(o)
	if err != nil || int(n) > len(d.b) {
		return errWKB
	}
	if n == 0 {
		sb.WriteString(" EMPTY")
		return nil
	}
	sb.WriteString("(")
	for i := uint32(0); i < n; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		if err := item(sb, o, dim); err != nil {
			return err
		}
	}
	sb.WriteString(")")
	return nil
}

func (d *wkbReader) coords(sb *strings.Builder, o binary.ByteOrder, dim int) error {
	p, _, err := d.point(o, dim)
	sb.WriteString(p)
	return err
}

// ring is a polygon ring: "(x y,x y,...)".
func (d *wkbReader) ring(sb *strings.Builder, o binary.ByteOrder, dim int) error {
	return d.list(sb, o, dim, d.coords)
}
