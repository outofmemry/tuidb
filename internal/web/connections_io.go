package web

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"strings"
	"webtuidb/internal/config"
)

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
