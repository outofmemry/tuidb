package db

import (
	"bytes"
	"image"
	_ "image/gif"  // register decoders for DecodeConfig
	_ "image/jpeg" //
	_ "image/png"  //
)

// SniffImage reports the MIME type and pixel size of a png, jpeg, gif or webp blob.
func SniffImage(b []byte) (mime string, w, h int, ok bool) {
	if len(b) < 12 {
		return "", 0, 0, false
	}
	if bytes.HasPrefix(b, []byte("RIFF")) && string(b[8:12]) == "WEBP" {
		w, h, ok = webpSize(b[12:])
		return "image/webp", w, h, ok
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return "", 0, 0, false
	}
	return "image/" + format, cfg.Width, cfg.Height, true
}

// IsPDF reports whether b starts with the %PDF- magic.
func IsPDF(b []byte) bool { return bytes.HasPrefix(b, []byte("%PDF-")) }

// webpSize reads the dimensions from the first chunk (VP8, VP8L or VP8X); no decoder dependency needed.
func webpSize(c []byte) (w, h int, ok bool) {
	if len(c) < 8 {
		return 0, 0, false
	}
	d := c[8:]
	switch string(c[:4]) {
	case "VP8 ":
		if len(d) >= 10 && d[3] == 0x9d && d[4] == 0x01 && d[5] == 0x2a {
			return int(d[6]) | int(d[7]&0x3f)<<8, int(d[8]) | int(d[9]&0x3f)<<8, true
		}
	case "VP8L":
		if len(d) >= 5 && d[0] == 0x2f {
			v := uint32(d[1]) | uint32(d[2])<<8 | uint32(d[3])<<16 | uint32(d[4])<<24
			return int(v&0x3fff) + 1, int(v>>14&0x3fff) + 1, true
		}
	case "VP8X":
		if len(d) >= 10 {
			return (int(d[4]) | int(d[5])<<8 | int(d[6])<<16) + 1, (int(d[7]) | int(d[8])<<8 | int(d[9])<<16) + 1, true
		}
	}
	return 0, 0, false
}
