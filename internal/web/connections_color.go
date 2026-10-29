package web

import (
	"net/http"
	"regexp"
	"webtuidb/internal/config"
)

var colorRe = regexp.MustCompile(`^(#([0-9a-fA-F]{3}|[0-9a-fA-F]{6}))?$`)

func (ss *session) color() string {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.conn.Color
}

// setColor changes the colour of the open session and of the saved connection (not one from a [[source]] file).
func (s *Server) setColor(ss *session, r *http.Request) (any, error) {
	var req struct{ Color string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if !colorRe.MatchString(req.Color) {
		return nil, bad("color must be empty or a hex colour like #3b82f6")
	}
	if len(req.Color) == 4 { // #abc -> #aabbcc: both UIs store 6 digits
		c := req.Color
		req.Color = "#" + c[1:2] + c[1:2] + c[2:3] + c[2:3] + c[3:4] + c[3:4]
	}
	ss.mu.Lock()
	ss.conn.Color = req.Color
	ss.mu.Unlock()
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.reloadConns() != nil { // connections.toml does not parse: keep the file as it is
		return ss.info(), nil
	}
	for i, c := range s.o.Conns {
		if c.Name == ss.conn.Name && c.Source == "" {
			s.o.Conns[i].Color = req.Color
			if s.o.ConnPath != "" {
				own, _, _ := s.splitSources(s.o.Conns)
				if err := config.SaveConnections(s.o.ConnPath, own); err != nil {
					return nil, err
				}
				s.stamp = s.connStamp()
			}
		}
	}
	return ss.info(), nil
}
