package web

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"webtuidb/internal/config"
)

func expandDir(p string) string {
	p = os.ExpandEnv(p)
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(h, p[2:])
		}
	}
	return p
}

// connStamp changes when connections.toml or a file in a [[source]] folder does (name, mtime, size).
func (s *Server) connStamp() string {
	if s.o.ConnPath == "" {
		return ""
	}
	files := []string{s.o.ConnPath}
	for _, src := range s.o.Cfg.Sources {
		g, _ := filepath.Glob(filepath.Join(expandDir(src.Path), "*.toml"))
		files = append(files, g...)
	}
	var b strings.Builder
	for _, f := range files {
		if st, err := os.Stat(f); err == nil {
			fmt.Fprintf(&b, "%s|%d|%d;", f, st.ModTime().UnixNano(), st.Size())
		}
	}
	return b.String()
}

// reloadConns re-reads the connection files when they changed since the last look. connMu must be
// held. A file that no longer parses keeps the list we have and returns the parse error.
func (s *Server) reloadConns() error {
	if s.o.ConnPath == "" {
		return nil
	}
	st := s.connStamp()
	if st == s.stamp {
		return nil
	}
	cs, err := config.LoadAllConnections(s.o.ConnPath, s.o.Cfg.Sources)
	if err != nil {
		return err
	}
	s.stamp, s.o.Conns = st, cs
	return nil
}

// splitSources separates the connections that live in connections.toml (to be saved) from those
// that come from a [[source]] file, which are read-only: they must come back unchanged and all present.
func (s *Server) splitSources(in []config.Connection) (own, fromSource []config.Connection, err error) {
	have := map[string]config.Connection{}
	for _, c := range s.o.Conns {
		if c.Source != "" {
			have[c.Name] = c
		}
	}
	seen := map[string]bool{}
	for _, c := range in {
		old, isSrc := have[c.Name]
		switch {
		case isSrc && !reflect.DeepEqual(c, old):
			return nil, nil, bad("connection %q comes from %s and is read-only: edit that file instead", c.Name, old.Source)
		case isSrc:
			seen[c.Name] = true
			fromSource = append(fromSource, c)
		case c.Source != "":
			return nil, nil, bad("connection %q claims source %s, which does not hold it", c.Name, c.Source)
		default:
			own = append(own, c)
		}
	}
	for n, old := range have {
		if !seen[n] {
			return nil, nil, bad("connection %q comes from %s and is read-only: it cannot be removed here", n, old.Source)
		}
	}
	return own, fromSource, nil
}
