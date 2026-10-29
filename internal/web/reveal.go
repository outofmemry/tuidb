package web

import (
	"net/http"
	"os"
	"path/filepath"
	"webtuidb/internal/store"
)

// reveal opens the folder of a file on the machine running webtuidb; a remote browser gets a 403 and offers the download.
func (s *Server) reveal(r *http.Request) (any, error) {
	if !loopback(r.RemoteAddr) {
		return nil, &apiErr{Code: http.StatusForbidden, Msg: "reveal works only on the machine running webtuidb"}
	}
	var in struct{ Path string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(in.Path) {
		return nil, bad("an absolute path is required")
	}
	if _, err := os.Stat(in.Path); err != nil {
		return nil, bad("%v", err)
	}
	if err := store.Reveal(in.Path); err != nil {
		return nil, bad("%v", err)
	}
	return nil, nil
}
