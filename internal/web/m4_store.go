package web

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"webtuidb/internal/config"
	"webtuidb/internal/store"
)

// Milestone 4 store endpoints: open tabs, history and query files, connection export / import.

func (s *Server) getTabs(r *http.Request) (any, error) {
	c := r.URL.Query().Get("conn")
	if c == "" {
		return nil, bad("conn is required")
	}
	t := s.o.State.OpenTabs(c)
	if t == nil {
		t = []store.OpenTab{}
	}
	return t, nil
}

func (s *Server) putTabs(r *http.Request) (any, error) {
	c := r.URL.Query().Get("conn")
	if c == "" {
		return nil, bad("conn is required")
	}
	var tabs []store.OpenTab
	if err := decode(r, &tabs); err != nil {
		return nil, err
	}
	return nil, s.o.State.SetOpenTabs(c, tabs)
}

// ---- history: Save As… / Import… -------------------------------------------------------------------

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

// ---- saved queries ----------------------------------------------------------------------------------

// exportQueries downloads every saved query (shared folders excluded) as JSON.
func (s *Server) exportQueries(r *http.Request) (any, error) {
	l, err := s.o.Queries.List()
	if err != nil {
		return nil, err
	}
	out := []store.Query{}
	for _, q := range l {
		if !q.Shared {
			out = append(out, q)
		}
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return &download{name: "queries.json", data: b, ctype: "application/json"}, nil
}

// importQueryFile saves the text of a .sql file as a saved query: {folder, name, sql, keywords}.
func (s *Server) importQueryFile(r *http.Request) (any, error) {
	var req struct {
		Folder, Name, SQL string
		Keywords          []string
		Overwrite         bool
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	req.Name = strings.TrimSuffix(strings.TrimSpace(req.Name), ".sql")
	if req.Name == "" || strings.TrimSpace(req.SQL) == "" {
		return nil, bad("name and sql are required")
	}
	return s.saveNew(req.Folder, req.Name, req.Keywords, req.SQL, req.Overwrite)
}

// ---- connections: export / import ---------------------------------------------------------------------

// exportConns: ?passphrase=&passwords=1&group=. Source connections are not exported (they live in
// their own files). Passwords are only included under a passphrase.
// POST takes the same fields as a JSON body, which keeps the passphrase out of access logs.
// shortcut: GET with ?passphrase= still works for the current client; drop it once the client POSTs.
func (s *Server) exportConns(r *http.Request) (any, error) {
	q := r.URL.Query()
	pass, withPw := q.Get("passphrase"), q.Get("passwords") == "1" || q.Get("passwords") == "true"
	group := q.Get("group")
	if r.Method == http.MethodPost {
		var req struct {
			Passphrase string
			Passwords  bool
			Group      string
		}
		if err := decode(r, &req); err != nil {
			return nil, err
		}
		pass, withPw, group = req.Passphrase, req.Passwords, req.Group
	}
	if withPw && pass == "" {
		return nil, bad("passwords can only be exported under a passphrase")
	}
	s.connMu.Lock()
	var cs []config.Connection
	for _, c := range s.o.Conns {
		if c.Source == "" && (group == "" || c.Group == group || strings.HasPrefix(c.Group, group+"/")) {
			cs = append(cs, c)
		}
	}
	s.connMu.Unlock()
	var b bytes.Buffer
	if err := config.ExportConnections(&b, cs, pass, withPw); err != nil {
		return nil, err
	}
	return &download{name: "connections.wtdb", data: b.Bytes()}, nil
}

// importConns adds the connections of an exported file to connections.toml. The file is
// `contentBase64` (an encrypted file is binary) or `content` (plain TOML text), plus `passphrase`.
// A name that already exists is skipped, never overwritten.
func (s *Server) importConns(r *http.Request) (any, error) {
	var req struct {
		Content, ContentBase64, Passphrase string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	data := []byte(req.Content)
	if req.ContentBase64 != "" {
		var err error
		if data, err = base64.StdEncoding.DecodeString(req.ContentBase64); err != nil {
			return nil, bad("contentBase64: %v", err)
		}
	}
	in, dropped, err := config.ImportConnectionsOpts(bytes.NewReader(data), req.Passphrase, false) // never keep commands from a file: the server would run them
	if err != nil {
		return nil, bad("%v", err)
	}
	s.connMu.Lock()
	defer s.connMu.Unlock()
	s.reloadConns()
	have := map[string]bool{}
	var own []config.Connection
	for _, c := range s.o.Conns {
		have[c.Name] = true
		if c.Source == "" {
			own = append(own, c)
		}
	}
	added, skipped := []string{}, []string{}
	var addedConns []config.Connection
	for _, c := range in {
		c.Source = ""
		if have[c.Name] {
			skipped = append(skipped, c.Name)
			continue
		}
		have[c.Name] = true
		own = append(own, c)
		added = append(added, c.Name)
		addedConns = append(addedConns, c)
	}
	if len(added) > 0 {
		if s.o.ConnPath != "" {
			if err := config.SaveConnections(s.o.ConnPath, own); err != nil {
				return nil, err
			}
			s.stamp = s.connStamp()
		}
		s.o.Conns = append(s.o.Conns, addedConns...)
	}
	if dropped == nil {
		dropped = []string{}
	}
	return map[string]any{"added": added, "skipped": skipped, "dropped": dropped}, nil
}
