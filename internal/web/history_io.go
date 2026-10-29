package web

import (
	"bytes"
	"net/http"
	"strings"
	"webtuidb/internal/store"
)

// exportHistory: ?conn=&format=csv|json (default json), oldest first.
func (s *Server) exportHistory(r *http.Request) (any, error) {
	q := r.URL.Query()
	if s.o.Hist == nil || q.Get("conn") == "" {
		return nil, bad("conn is required")
	}
	l, err := s.o.Hist.List(q.Get("conn"), q.Get("q"), 0)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(l)-1; i < j; i, j = i+1, j-1 {
		l[i], l[j] = l[j], l[i]
	}
	f := q.Get("format")
	if f == "" {
		f = "json"
	}
	if l == nil {
		l = []store.HistoryEntry{}
	}
	var b bytes.Buffer
	if err := store.ExportHistory(&b, l, f); err != nil { // one column order for both UIs
		return nil, bad("format must be csv or json")
	}
	if f == "csv" {
		return &download{name: "history.csv", data: b.Bytes(), ctype: "text/csv; charset=utf-8"}, nil
	}
	return &download{name: "history.json", data: b.Bytes(), ctype: "application/json"}, nil
}

// importHistory appends entries from a file exported above: {conn, format, content}.
// A re-import adds the entries again; entry ids come from their timestamps.
// The format field is ignored: the content says which it is.
func (s *Server) importHistory(r *http.Request) (any, error) {
	var req struct{ Conn, Format, Content string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if s.o.Hist == nil || req.Conn == "" {
		return nil, bad("conn is required")
	}
	in, err := store.ImportHistory(strings.NewReader(req.Content)) // JSON or CSV, told apart by the first byte
	if err != nil {
		return nil, bad("%v", err)
	}
	n := 0
	for _, e := range in {
		if strings.TrimSpace(e.SQL) == "" {
			continue
		}
		if e.Source == "" {
			e.Source = "editor"
		}
		if err := s.o.Hist.Append(req.Conn, e); err != nil {
			return nil, err
		}
		n++
	}
	return map[string]any{"imported": n}, nil
}
