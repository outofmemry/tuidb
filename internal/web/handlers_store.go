package web

import (
	"net/http"
	"path"
	"strings"

	"webtuidb/internal/store"
)

func (s *Server) history(r *http.Request) (any, error) {
	q := r.URL.Query()
	if s.o.Hist == nil || q.Get("conn") == "" {
		return nil, bad("conn is required")
	}
	l, err := s.o.Hist.List(q.Get("conn"), q.Get("q"), atoi(q.Get("limit"), 200))
	if l == nil {
		l = []store.HistoryEntry{}
	}
	return l, err
}

// deleteHistory removes one entry (?id=) or the whole connection's history when id is empty.
func (s *Server) deleteHistory(r *http.Request) (any, error) {
	q := r.URL.Query()
	if s.o.Hist == nil || q.Get("conn") == "" {
		return nil, bad("conn is required")
	}
	if id := q.Get("id"); id != "" {
		return nil, s.o.Hist.Delete(q.Get("conn"), id)
	}
	return nil, s.o.Hist.Clear(q.Get("conn"))
}

func (s *Server) listQueries(r *http.Request) (any, error) {
	l, err := s.o.Queries.List()
	if l == nil {
		l = []store.Query{}
	}
	return l, err
}

func (s *Server) saveQuery(r *http.Request) (any, error) {
	var req struct {
		Folder, Name, SQL string
		Keywords          []string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, bad("name is required")
	}
	return s.o.Queries.Save(req.Folder, req.Name, req.Keywords, req.SQL)
}

func (s *Server) updateQuery(r *http.Request) (any, error) {
	var req struct{ SQL string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	p, err := queryPath(r)
	if err != nil {
		return nil, err
	}
	return nil, s.o.Queries.Update(p, req.SQL)
}

func (s *Server) deleteQuery(r *http.Request) (any, error) {
	p, err := queryPath(r)
	if err != nil {
		return nil, err
	}
	return nil, s.o.Queries.Delete(p)
}

// queryPath rejects paths that clean to the store root ("", ".", "a/.."), which Delete would remove wholesale.
func queryPath(r *http.Request) (string, error) {
	p := r.PathValue("path")
	if c := path.Clean(p); c == "." || c == ".." || strings.HasPrefix(c, "../") || strings.HasPrefix(c, "/") {
		return "", bad("invalid query path %q", p)
	}
	return p, nil
}

func tableKey(r *http.Request) (string, error) {
	q := r.URL.Query()
	if q.Get("conn") == "" || q.Get("table") == "" {
		return "", bad("conn and table are required")
	}
	return store.TableKey(q.Get("conn"), q.Get("db"), q.Get("table")), nil
}

func (s *Server) getTableState(r *http.Request) (any, error) {
	k, err := tableKey(r)
	if err != nil {
		return nil, err
	}
	return s.o.State.Table(k), nil
}

func (s *Server) putTableState(r *http.Request) (any, error) {
	k, err := tableKey(r)
	if err != nil {
		return nil, err
	}
	var ts store.TableState
	if err := decode(r, &ts); err != nil {
		return nil, err
	}
	return nil, s.o.State.SetTable(k, ts)
}

func (s *Server) shortcuts(r *http.Request) (any, error) {
	c := r.URL.Query().Get("conn")
	pinned, recent := s.o.State.Pinned(c), s.o.State.Recent(c)
	if pinned == nil {
		pinned = []string{}
	}
	if recent == nil {
		recent = []string{}
	}
	return map[string]any{"pinned": pinned, "recent": recent}, nil
}

func (s *Server) pin(r *http.Request) (any, error) {
	var req struct{ Conn, Item string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	p, err := s.o.State.TogglePin(req.Conn, req.Item)
	return map[string]any{"pinned": p}, err
}

func (s *Server) touch(r *http.Request) (any, error) {
	var req struct{ Conn, Item string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	return nil, s.o.State.Touch(req.Conn, req.Item)
}
