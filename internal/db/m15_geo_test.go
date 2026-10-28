package db

import (
	"encoding/base64"
	"encoding/binary"
	"math"
	"testing"
)

type order interface {
	binary.ByteOrder
	binary.AppendByteOrder
}

// wkb builds a geometry: parts are uint32 (counts/types) or float64 (ordinates), in order o.
func wkb(o order, parts ...any) []byte {
	var b []byte
	if o == order(binary.LittleEndian) {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	for _, p := range parts {
		switch v := p.(type) {
		case uint32:
			b = o.AppendUint32(b, v)
		case float64:
			b = o.AppendUint64(b, math.Float64bits(v))
		case []byte:
			b = append(b, v...)
		}
	}
	return b
}

func TestWKT(t *testing.T) {
	for _, o := range []order{binary.LittleEndian, binary.BigEndian} {
		pt := wkb(o, uint32(1), 1.5, -2.0)
		ls := wkb(o, uint32(2), uint32(2), 0.0, 0.0, 1.0, 1.0)
		poly := wkb(o, uint32(3), uint32(1), uint32(4), 0.0, 0.0, 4.0, 0.0, 4.0, 4.0, 0.0, 0.0)
		cases := []struct {
			in   []byte
			want string
		}{
			{pt, "POINT(1.5 -2)"},
			{ls, "LINESTRING(0 0,1 1)"},
			{poly, "POLYGON((0 0,4 0,4 4,0 0))"},
			{wkb(o, uint32(4), uint32(2), pt, pt), "MULTIPOINT((1.5 -2),(1.5 -2))"},
			{wkb(o, uint32(6), uint32(1), poly), "MULTIPOLYGON(((0 0,4 0,4 4,0 0)))"},
			{wkb(o, uint32(7), uint32(2), pt, ls), "GEOMETRYCOLLECTION(POINT(1.5 -2),LINESTRING(0 0,1 1))"},
			{wkb(o, uint32(0x20000001), uint32(4326), 3.0, 4.0), "POINT(3 4)"}, // EWKB SRID
			{wkb(o, uint32(0x80000001), 1.0, 2.0, 3.0), "POINT Z(1 2 3)"},      // EWKB Z
			{wkb(o, uint32(1001), 1.0, 2.0, 3.0), "POINT Z(1 2 3)"},            // ISO Z
			{wkb(o, uint32(1), math.NaN(), math.NaN()), "POINT EMPTY"},
			{wkb(o, uint32(2), uint32(0)), "LINESTRING EMPTY"},
		}
		for _, c := range cases {
			if got, ok := WKT(c.in); !ok || got != c.want {
				t.Errorf("%v: got %q %v want %q", o, got, ok, c.want)
			}
		}
	}
	// MySQL: 4-byte SRID in front of the WKB
	my := append([]byte{0xE6, 0x10, 0, 0}, wkb(binary.LittleEndian, uint32(1), 1.0, 2.0)...)
	if got, ok := WKT(my); !ok || got != "POINT(1 2)" {
		t.Errorf("mysql: %q %v", got, ok)
	}
	for _, bad := range [][]byte{nil, {1}, {1, 9, 0, 0, 0}, wkb(binary.LittleEndian, uint32(2), uint32(999999)), wkb(binary.LittleEndian, uint32(1), 1.0), []byte("hello world")} {
		if s, ok := WKT(bad); ok {
			t.Errorf("accepted %v as %q", bad, s)
		}
	}
}

func TestSniffImageAndPDF(t *testing.T) {
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")
	if m, w, h, ok := SniffImage(png); !ok || m != "image/png" || w != 1 || h != 1 {
		t.Errorf("png: %v %d %d %v", m, w, h, ok)
	}
	gif, _ := base64.StdEncoding.DecodeString("R0lGODlhAQABAAAAACw=")
	if m, w, h, ok := SniffImage(gif); !ok || m != "image/gif" || w != 1 || h != 1 {
		t.Errorf("gif: %v %d %d %v", m, w, h, ok)
	}
	// RIFF....WEBPVP8X, 640x480 canvas
	webp := append([]byte("RIFF\x16\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x00\x00\x00\x00"), 0x7f, 0x02, 0x00, 0xdf, 0x01, 0x00)
	if m, w, h, ok := SniffImage(webp); !ok || m != "image/webp" || w != 640 || h != 480 {
		t.Errorf("webp: %v %d %d %v", m, w, h, ok)
	}
	if _, _, _, ok := SniffImage([]byte("not an image at all")); ok {
		t.Error("text accepted")
	}
	if !IsPDF([]byte("%PDF-1.7\n")) || IsPDF([]byte("PDF")) {
		t.Error("pdf")
	}
}

func TestWKTDeepCollection(t *testing.T) {
	le := binary.LittleEndian
	g := wkb(le, uint32(1), 1.0, 2.0)
	for range 12 { // PostGIS allows any nesting
		g = wkb(le, uint32(7), uint32(1), g)
	}
	got, ok := WKT(g)
	if !ok || len(got) < len("GEOMETRYCOLLECTION")*12 {
		t.Errorf("12-deep collection: %v %q", ok, got)
	}
	for range 80 { // still bounded
		g = wkb(le, uint32(7), uint32(1), g)
	}
	if _, ok := WKT(g); ok {
		t.Error("92-deep collection should be refused")
	}
}
