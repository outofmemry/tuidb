package web

import (
	"crypto/subtle"
	"net/http"
	"webtuidb/internal/config"
	"webtuidb/internal/safemode"
)

// setSafeMode changes the session's level and the saved connection's, like the terminal's picker.
// Leaving Safe 1 / Safe 2 asks for the database password (TablePlus rule); with none stored the caller confirms instead.
func (s *Server) setSafeMode(ss *session, r *http.Request) (any, error) {
	var req struct {
		Level, Password string
		Confirmed       bool // with no stored password to verify, the caller confirms instead
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	lvl, err := safemode.Parse(req.Level)
	if err != nil || req.Level == "" {
		return nil, bad("level must be one of silent, alert1, alert2, safe1, safe2")
	}
	if cur := ss.lvl(); (cur == safemode.Safe1 || cur == safemode.Safe2) && lvl != cur {
		if ss.o.Password != "" {
			if subtle.ConstantTimeCompare([]byte(req.Password), []byte(ss.o.Password)) != 1 {
				return nil, &apiErr{Code: http.StatusConflict, Msg: "enter the database password to change safe mode", Extra: map[string]any{"gate": gateInfo{Password: true}}}
			}
		} else if !req.Confirmed { // password_cmd / ask / keyring: nothing to verify, so ask for a confirmation
			return nil, &apiErr{Code: http.StatusConflict, Msg: "confirm changing safe mode from " + cur.Label(), Extra: map[string]any{"gate": gateInfo{Confirm: true}}}
		}
	}
	ss.mu.Lock()
	ss.level = lvl
	ss.mu.Unlock()
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.reloadConns() != nil { // connections.toml does not parse: the level changes for this session, the file is left alone
		return ss.info(), nil
	}
	for i, c := range s.o.Conns {
		if c.Name == ss.conn.Name && c.Source == "" { // source connections live in their own files
			s.o.Conns[i].SafeMode = string(lvl)
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
