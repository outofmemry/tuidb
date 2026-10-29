// quicklook.go: Quick look at a value: hex, encodings, image and binary info, save as.

package ui

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

var encodings = []string{"Hex dump", "Base64", "UTF-8", "Latin-1 (ISO-8859-1)", "UTF-16"}

// decodeAs shows raw in the named encoding.
func decodeAs(raw []byte, enc string) string {
	switch enc {
	case "Hex dump":
		return hexDump(raw)
	case "Base64":
		return base64Lines(raw)
	case "WKT":
		if w, ok := db.WKT(raw); ok {
			return w
		}
		return hexDump(raw)
	case "Latin-1 (ISO-8859-1)":
		r := make([]rune, len(raw))
		for i, b := range raw {
			r[i] = rune(b)
		}
		return string(r)
	case "UTF-16":
		be := len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF // a BOM picks the byte order, little endian otherwise
		if len(raw) >= 2 && (be || raw[0] == 0xFF && raw[1] == 0xFE) {
			raw = raw[2:]
		}
		u := make([]uint16, len(raw)/2)
		for i := range u {
			if be {
				u[i] = uint16(raw[2*i])<<8 | uint16(raw[2*i+1])
			} else {
				u[i] = uint16(raw[2*i+1])<<8 | uint16(raw[2*i])
			}
		}
		return string(utf16.Decode(u))
	}
	return strings.ToValidUTF8(string(raw), "�")
}

// parseHex reads "de ad be ef" (spaces, newlines and an optional offset-free dump are fine): two digits per byte.
func parseHex(s string) ([]byte, error) {
	s = strings.Join(strings.Fields(s), "")
	b, err := hex.DecodeString(s)
	if err != nil { // odd length or a non-hex digit: one plain message (T-P3-17), not encoding/hex's
		return nil, errors.New("Hex needs pairs of 0-9 a-f.")
	}
	return b, nil
}

// newQuickLookValue opens quick look on a cell. A bytes cell starts as a hex dump; v switches the
// encoding it is decoded with, and e edits the bytes as hex when onSave is set.
func (a *App) newQuickLookValue(title string, v any, onSave func([]byte) tea.Cmd) *quickLookDlg {
	raw, isBytes := v.([]byte)
	text := plain(v)
	enc := "UTF-8"
	if isBytes {
		text, enc = hexDump(raw), "Hex dump"
	} else if s, ok := v.(string); ok {
		raw = []byte(s)
	}
	if isBytes && a.d.Cfg.Table.DecodeSpatial {
		if w, ok := db.WKT(raw); ok {
			text, enc = w, "WKT" // a geometry shows as WKT; v switches back to the bytes
		}
	}
	q := newQuickLook(title, text)
	q.a, q.rawBytes, q.isBytes, q.onSave, q.enc = a, raw, isBytes, onSave, enc
	if isBytes {
		q.info = binaryInfo(raw, a.d.Cfg.Table.ImagePreview)
		if strings.Contains(q.info, "image/") {
			q.note = "terminal shows the image size only; the web UI previews the picture"
		}
	}
	return q
}

func (q *quickLookDlg) pickEncoding() tea.Cmd {
	var items []pickItem
	encs := encodings
	if _, ok := db.WKT(q.rawBytes); ok {
		encs = append([]string{"WKT"}, encs...)
	}
	for _, e := range encs {
		d := ""
		if e == q.enc {
			d = "current"
		}
		items = append(items, pickItem{Label: e, Detail: d, Value: e})
	}
	q.a.push(&pickerDlg{title: "View using encoding", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		q.enc = it.Value.(string)
		q.text = decodeAs(q.rawBytes, q.enc)
		q.tree, q.raw, q.lines, q.top = nil, true, nil, 0
		return nil
	}})
	return nil
}

func (q *quickLookDlg) editHex() tea.Cmd {
	var sb strings.Builder
	for i, b := range q.rawBytes {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%02x", b)
	}
	f := newForm("Edit bytes (two hex digits per byte)", []formField{{Key: "hex", Label: "Hex", Kind: fText, Value: sb.String()}}, func(m map[string]string) tea.Cmd {
		b, _ := parseHex(m["hex"])
		q.rawBytes = b
		if q.enc == "Hex dump" {
			q.text, q.lines = hexDump(b), nil
		}
		return q.onSave(b)
	})
	f.validate = func(m map[string]string) error { _, err := parseHex(m["hex"]); return err }
	q.a.push(f)
	return nil
}

type overviewMsg struct {
	ref  db.ObjRef
	text []string
	err  error
}

// itemOverview shows row estimate, size, columns, indexes and foreign keys of the selected table.
func (w *Workspace) itemOverview() tea.Cmd {
	ref, ok := w.targetRef()
	if !ok {
		return w.a.say("select or open a table first")
	}
	detail := ""
	for _, o := range w.sb.objs {
		if o.ObjRef == ref {
			detail = o.Detail
		}
	}
	return w.async(-1, func(ctx context.Context, sess db.Session) tea.Msg {
		m := overviewMsg{ref: ref}
		cols, err := sess.Columns(ctx, ref)
		if err != nil {
			m.err = err
			return m
		}
		idx, _ := sess.Indexes(ctx, ref)
		fks, _ := sess.ForeignKeys(ctx, ref)
		rows := "unknown"
		if n, err := sess.Count(ctx, db.RowQuery{ObjRef: ref}, true); err == nil {
			rows = fmt.Sprint(n.N)
			if !n.Exact {
				rows = "~" + rows
			}
		}
		m.text = append(m.text, "Rows: "+rows, "Size: "+orDefault(detail, "not reported"), "", fmt.Sprintf("Columns (%d)", len(cols)))
		for _, c := range cols {
			flags := ""
			if c.PK {
				flags += " PK"
			}
			if !c.Nullable {
				flags += " NOT NULL"
			}
			m.text = append(m.text, fmt.Sprintf("  %-24s %s%s", c.Name, c.Type, flags))
		}
		m.text = append(m.text, "", fmt.Sprintf("Indexes (%d)", len(idx)))
		for _, ix := range idx {
			u := ""
			if ix.Unique {
				u = " unique"
			}
			m.text = append(m.text, fmt.Sprintf("  %s (%s)%s", ix.Name, strings.Join(ix.Columns, ", "), u))
		}
		m.text = append(m.text, "", fmt.Sprintf("Foreign keys (%d)", len(fks)))
		for _, fk := range fks {
			m.text = append(m.text, fmt.Sprintf("  %s (%s) -> %s (%s)", fk.Name, strings.Join(fk.Columns, ", "), fk.RefTable, strings.Join(fk.RefColumns, ", ")))
		}
		return m
	})
}

// saveAs writes the cell's bytes to a file (quick look on a binary cell).
func (q *quickLookDlg) saveAs() tea.Cmd {
	q.a.push(newForm("Save the cell as a file", []formField{{Key: "path", Label: "File", Kind: fText, Value: "cell.bin"}}, func(v map[string]string) tea.Cmd {
		path := strings.TrimSpace(v["path"])
		if path == "" {
			return q.a.fail(fmt.Errorf("file name required"))
		}
		return q.a.ifFree(func() tea.Cmd {
			err := writeAtomic(path, func(w io.Writer) error { _, err := w.Write(q.rawBytes); return err })
			if err != nil {
				return q.a.fail(err)
			}
			return q.a.say("saved %d bytes to %s", len(q.rawBytes), path)
		}, path)
	}))
	return nil
}

// binaryInfo is the one-line header above a binary value: size, and what the bytes are when they are an image or a PDF.
func binaryInfo(b []byte, sniff bool) string {
	s := fmt.Sprintf("%d bytes", len(b))
	if !sniff {
		return s
	}
	if mime, w, h, ok := db.SniffImage(b); ok {
		return fmt.Sprintf("%s %d×%d, %s", mime, w, h, s)
	}
	if db.IsPDF(b) {
		return "PDF, " + s
	}
	return s
}

// base64Lines is raw as base64 wrapped at 76 columns.
func base64Lines(raw []byte) string {
	s := base64.StdEncoding.EncodeToString(raw)
	var out []string
	for len(s) > 76 {
		out, s = append(out, s[:76]), s[76:]
	}
	return strings.Join(append(out, s), "\n")
}
