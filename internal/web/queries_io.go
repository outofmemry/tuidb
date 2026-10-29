package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"webtuidb/internal/store"
)

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
