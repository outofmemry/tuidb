package web

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"sync"
	"time"
)

func (s *Server) tls() bool { return s.o.Cfg.Web.TLSCert != "" && s.o.Cfg.Web.TLSKey != "" }

func (s *Server) scheme() string {
	if s.tls() {
		return "https"
	}
	return "http"
}

const (
	cookieName = "webtuidb_session"
	loginTTL   = 12 * time.Hour
)

// logins holds the live login cookies: random values, server-side expiry.
type logins struct {
	mu     sync.Mutex
	m      map[string]time.Time
	fails  []time.Time // recent failed attempts, any client
	locked time.Time   // 401 for every attempt until then
}

const (
	loginMaxFails = 5
	loginWindow   = 10 * time.Minute
	loginLockout  = 60 * time.Second
)

// isLocked reports whether logins are refused right now.
func (l *logins) isLocked() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return time.Now().Before(l.locked)
}

// failed records a wrong password; the 5th within 10 minutes locks logins for 60 s.
func (l *logins) failed() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	keep := l.fails[:0]
	for _, t := range l.fails {
		if now.Sub(t) < loginWindow {
			keep = append(keep, t)
		}
	}
	l.fails = append(keep, now)
	if len(l.fails) >= loginMaxFails {
		l.fails, l.locked = nil, now.Add(loginLockout)
	}
}

func (l *logins) add() string {
	v := randID(32)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = map[string]time.Time{}
	}
	now := time.Now()
	for k, exp := range l.m {
		if now.After(exp) {
			delete(l.m, k)
		}
	}
	l.m[v] = now.Add(loginTTL)
	return v
}

func (l *logins) valid(v string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	exp, ok := l.m[v]
	return ok && time.Now().Before(exp)
}

// login trades the configured [web] password for an HttpOnly cookie that api() accepts in place of the bearer token.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	pw := s.o.Cfg.Web.Password
	if pw == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "login is not enabled ([web] password is not set)"})
		return
	}
	if !s.originOK(r) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	var req struct{ Password string }
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	if s.logins.isLocked() { // shortcut: one global counter, so an attacker can also lock the owner out for 60 s
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "too many failed logins; wait a minute"})
		return
	}
	a, b := sha256.Sum256([]byte(req.Password)), sha256.Sum256([]byte(pw))
	if subtle.ConstantTimeCompare(a[:], b[:]) != 1 {
		s.logins.failed()
		time.Sleep(400 * time.Millisecond)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "wrong password"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: s.logins.add(), Path: "/", MaxAge: int(loginTTL.Seconds()),
		HttpOnly: true, Secure: s.tls(), SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
