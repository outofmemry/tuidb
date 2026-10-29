package web

import (
	"net/http"
	"webtuidb/internal/store"
)

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
