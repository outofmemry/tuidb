package web

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"webtuidb/internal/store"
)

func (s *Server) needState() error {
	if s.o.State == nil {
		return bad("no state store")
	}
	return nil
}

func (s *Server) getGroups(r *http.Request) (any, error) {
	c := r.URL.Query().Get("conn")
	if c == "" || s.needState() != nil {
		return nil, bad("conn is required")
	}
	return map[string]any{"groups": s.o.State.Groups(c)}, nil
}

func (s *Server) putGroups(r *http.Request) (any, error) {
	c := r.URL.Query().Get("conn")
	var in struct{ Groups []store.ItemGroup }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if c == "" || s.needState() != nil {
		return nil, bad("conn is required")
	}
	seen := map[string]bool{}
	for _, g := range in.Groups {
		if strings.TrimSpace(g.Name) == "" || seen[g.Name] {
			return nil, bad("every group needs its own non-empty name")
		}
		seen[g.Name] = true
	}
	return nil, s.o.State.SetGroups(c, in.Groups)
}

func (s *Server) getRecentFiles(r *http.Request) (any, error) {
	if s.needState() != nil {
		return nil, bad("no state store")
	}
	f := s.o.State.RecentFiles()
	if f == nil {
		f = []string{}
	}
	return map[string]any{"files": f, "max": s.o.Cfg.General.RecentFiles}, nil
}

// putRecentFile moves an entry of the shared list to the front. It takes only an absolute path that is already in the
// list (the browser's own file picker gives a name, never a path, and a bare name would be opened by the terminal UI
// relative to its working directory).
func (s *Server) putRecentFile(r *http.Request) (any, error) {
	var in struct{ Name string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if s.needState() != nil || !filepath.IsAbs(in.Name) || !slices.Contains(s.o.State.RecentFiles(), in.Name) {
		return nil, bad("only a path from the recent list can be added")
	}
	return nil, s.o.State.AddRecentFile(in.Name, s.o.Cfg.General.RecentFiles)
}

// openRecentFile reads a recent file by the absolute path the terminal UI stored. Only .sql files that are in the recent
// list can be read, so the endpoint is not a way to read the server's disk.
func (s *Server) openRecentFile(r *http.Request) (any, error) {
	var in struct{ Path string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if s.needState() != nil {
		return nil, bad("no state store")
	}
	listed := false
	for _, f := range s.o.State.RecentFiles() {
		listed = listed || f == in.Path
	}
	switch {
	case !listed:
		return nil, bad("that file is not in the recent list")
	case !filepath.IsAbs(in.Path):
		return nil, bad("a browser cannot reopen %q by name: pick the file again", in.Path)
	case !strings.EqualFold(filepath.Ext(in.Path), ".sql"):
		return nil, bad("only .sql files can be opened")
	}
	st, err := os.Stat(in.Path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 8<<20 {
		return nil, bad("%s cannot be read (missing, not a file, or over 8 MB)", in.Path)
	}
	b, err := os.ReadFile(in.Path)
	if err != nil {
		return nil, bad("%v", err)
	}
	return map[string]any{"name": filepath.Base(in.Path), "text": string(b)}, nil
}

func (s *Server) getConnSort(r *http.Request) (any, error) {
	if s.needState() != nil {
		return nil, bad("no state store")
	}
	return map[string]any{"sort": s.o.State.ConnectionSort(), "default": s.o.Cfg.General.ConnectionSort}, nil
}

func (s *Server) putConnSort(r *http.Request) (any, error) {
	var in struct{ Sort string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.Sort != "" && in.Sort != "name" && in.Sort != "tag" && in.Sort != "driver" && in.Sort != "recent" {
		return nil, bad("sort must be name, tag, driver or recent")
	}
	if s.needState() != nil {
		return nil, bad("no state store")
	}
	return nil, s.o.State.SetConnectionSort(in.Sort)
}

func (s *Server) getBackupOptions(r *http.Request) (any, error) {
	c := r.URL.Query().Get("conn")
	if c == "" || s.needState() != nil {
		return nil, bad("conn is required")
	}
	return map[string]any{"options": s.o.State.BackupOptions(c)}, nil
}

func (s *Server) putBackupOptions(r *http.Request) (any, error) {
	c := r.URL.Query().Get("conn")
	var in struct{ Options map[string]string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if c == "" || s.needState() != nil {
		return nil, bad("conn is required")
	}
	clean := map[string]string{}
	for _, d := range []string{"backup.", "restore."} {
		for _, k := range []string{"file", "gzip", "format", "data", "schema"} {
			if v, ok := in.Options[d+k]; ok && len(v) <= 512 {
				clean[d+k] = v
			}
		}
	}
	if len(clean) == 0 {
		return nil, bad("no known backup option given")
	}
	return nil, s.o.State.SetBackupOptions(c, clean) // merges
}
