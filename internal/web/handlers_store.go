package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"webtuidb/internal/db"
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

// deleteHistory removes one entry (?id=), or the whole connection's history when all=1 is passed.
// A missing id alone is a 400: a client sending an undefined id must not wipe the history.
func (s *Server) deleteHistory(r *http.Request) (any, error) {
	q := r.URL.Query()
	if s.o.Hist == nil || q.Get("conn") == "" {
		return nil, bad("conn is required")
	}
	if id := q.Get("id"); id != "" {
		return nil, s.o.Hist.Delete(q.Get("conn"), id)
	}
	if q.Get("all") != "1" {
		return nil, bad("id is required (pass all=1 to clear the whole history)")
	}
	return nil, s.o.Hist.Clear(q.Get("conn"))
}

// queryItem is a list entry: the query plus the ETag a PUT's If-Match expects.
type queryItem struct {
	store.Query
	Etag string `json:"etag"`
}

func (s *Server) listQueries(r *http.Request) (any, error) {
	l, err := s.o.Queries.List()
	out := make([]queryItem, 0, len(l))
	for _, q := range l {
		out = append(out, queryItem{q, queryETag(q)})
	}
	return out, err
}

// getQuery returns one saved query with its ETag header (the value updateQuery checks If-Match against).
func (s *Server) getQuery(r *http.Request) (any, error) {
	p, err := queryPath(r)
	if err != nil {
		return nil, err
	}
	q, err := s.o.Queries.Read(p)
	if err != nil {
		return nil, err
	}
	if w := rw(r); w != nil {
		w.Header().Set("ETag", queryETag(q))
	}
	kw := q.Keywords
	if kw == nil {
		kw = []string{}
	}
	return map[string]any{"name": q.Name, "keywords": kw, "sql": q.SQL, "shared": q.Shared}, nil
}

func (s *Server) saveQuery(r *http.Request) (any, error) {
	var req struct {
		Folder, Name, SQL string
		Keywords          []string
		Overwrite         bool
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, bad("name is required")
	}
	return s.saveNew(req.Folder, req.Name, req.Keywords, req.SQL, req.Overwrite)
}

// saveNew saves a query; an existing file is a 409 {exists:true} unless overwrite is set.
func (s *Server) saveNew(folder, name string, kw []string, sql string, overwrite bool) (any, error) {
	if overwrite {
		return s.o.Queries.Save(folder, name, kw, sql)
	}
	q, err := s.o.Queries.SaveIfAbsent(folder, name, kw, sql)
	if errors.Is(err, store.ErrExists) {
		return nil, &apiErr{Code: http.StatusConflict, Msg: "a saved query with this name already exists", Extra: map[string]any{"exists": true}}
	}
	return q, err
}

func (s *Server) updateQuery(r *http.Request) (any, error) {
	var req struct{ SQL *string } // a pointer: a missing field is not the empty query
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if req.SQL == nil {
		return nil, bad("sql is required")
	}
	p, err := queryPath(r)
	if err != nil {
		return nil, err
	}
	s.queryMu.Lock()
	defer s.queryMu.Unlock()
	if m := r.Header.Get("If-Match"); m != "" { // another tab saved this query since it was loaded
		cur, err := s.o.Queries.Read(p)
		if err != nil {
			return nil, err
		}
		if m != queryETag(cur) {
			return nil, &apiErr{Code: http.StatusConflict, Msg: "this saved query changed elsewhere; reload it before saving"}
		}
	}
	if err := s.o.Queries.Update(p, *req.SQL); err != nil {
		return nil, err
	}
	if w := rw(r); w != nil {
		if cur, err := s.o.Queries.Read(p); err == nil {
			w.Header().Set("ETag", queryETag(cur))
		}
	}
	return nil, nil
}

func queryETag(q store.Query) string {
	return fmt.Sprintf(`"%x"`, sha256.Sum256([]byte(q.Name+"\x00"+strings.Join(q.Keywords, ",")+"\x00"+q.SQL)))
}

func (s *Server) deleteQuery(r *http.Request) (any, error) {
	p, err := queryPath(r)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(p, ".sql") { // "reports/" would otherwise remove the whole folder
		return nil, bad("%q is not a saved query file", p)
	}
	if _, err := s.o.Queries.Read(p); err != nil { // a regular, readable file
		return nil, bad("%q is not a saved query file", p)
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
	var raw map[string]json.RawMessage
	if err := decode(r, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, bad("a table state object is required")
	}
	// merge: fields that are present replace the saved ones, absent ones (the TUI's widths) are kept
	ts := s.o.State.Table(k)
	for key, v := range raw {
		switch key {
		case "widths":
			ts.ColWidths = nil
		case "filters":
			ts.Filters = db.FilterSet{}
		case "hidden":
			ts.Hidden = nil
		case "sort":
			ts.Sort = nil
			ts.SortNone = string(bytes.TrimSpace(v)) == "[]" || string(bytes.TrimSpace(v)) == "null" // P2-34
		case "show_filters":
			ts.ShowBar = false
		default:
			continue
		}
		field, _ := json.Marshal(map[string]json.RawMessage{key: v})
		if err := json.Unmarshal(field, &ts); err != nil {
			return nil, bad("invalid %s: %v", key, err)
		}
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

// renameQuery moves a saved query to a new path. store.Queries has no Rename, so (like the TUI) it saves the
// new file and deletes the old one; an existing target is a 409.
// shortcut: not atomic, a crash between the two steps leaves both files; add Queries.Rename.
func (s *Server) renameQuery(r *http.Request) (any, error) {
	var req struct{ From, To string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if !strings.HasSuffix(req.From, ".sql") || !strings.HasSuffix(req.To, ".sql") {
		return nil, bad("from and to must be saved query files (.sql)")
	}
	s.queryMu.Lock()
	defer s.queryMu.Unlock()
	q, err := s.o.Queries.Read(req.From)
	if err != nil {
		return nil, bad("%q is not a saved query file", req.From)
	}
	if q.Shared {
		return nil, bad("shared queries are read-only")
	}
	to := strings.TrimPrefix(strings.ReplaceAll(req.To, "\\", "/"), "/")
	folder, base := "", to
	if i := strings.LastIndex(to, "/"); i >= 0 {
		folder, base = to[:i], to[i+1:]
	}
	nq, err := s.saveNew(folder, strings.TrimSuffix(base, ".sql"), q.Keywords, q.SQL, false)
	if err != nil {
		return nil, err
	}
	if saved, ok := nq.(store.Query); ok && saved.Path == req.From {
		return nq, nil // same file name after sanitizing: nothing to delete
	}
	if err := s.o.Queries.Delete(req.From); err != nil {
		return nil, err
	}
	return nq, nil
}
