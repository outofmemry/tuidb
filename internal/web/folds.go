package web

import (
	"net/http"
	"unicode/utf16"
	"webtuidb/internal/db"
)

func foldKey(r *http.Request) (string, error) {
	k := r.URL.Query().Get("key")
	if k == "" {
		return "", bad("key is required")
	}
	return k, nil
}

func (s *Server) getFolds(r *http.Request) (any, error) {
	k, err := foldKey(r)
	if err != nil {
		return nil, err
	}
	f := s.o.State.Folds(k)
	if f == nil {
		f = []int{}
	}
	return f, nil
}

func (s *Server) putFolds(r *http.Request) (any, error) {
	k, err := foldKey(r)
	if err != nil {
		return nil, err
	}
	var f []int
	if err := decode(r, &f); err != nil {
		return nil, err
	}
	return nil, s.o.State.SetFolds(k, f)
}

// split returns each statement's [from,to] as UTF-16 offsets (what CodeMirror uses), cut by the same db.Split as the terminal.
func (s *Server) split(r *http.Request) (any, error) {
	var req struct{ Text, Dialect string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	u := func(b int) int { return len(utf16.Encode([]rune(req.Text[:b]))) }
	out := [][2]int{}
	for _, st := range db.Split(req.Dialect, req.Text) {
		out = append(out, [2]int{u(st.Start), u(st.End)})
	}
	return out, nil
}
