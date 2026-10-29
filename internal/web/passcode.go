package web

import (
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
	"webtuidb/internal/config"
)

type passFails struct {
	mu sync.Mutex
	n  map[string]*passEntry
}

// passEntry serialises the attempts of one address: its mutex is held across the delay and the bcrypt check,
// so parallel requests queue instead of waiting together.
type passEntry struct {
	mu    sync.Mutex
	fails int          // guarded by mu
	last  atomic.Int64 // unix nanoseconds of the last attempt
}

const (
	passFailExpiry = 15 * time.Minute // a quiet address starts again
	passMaxIPs     = 1000
)

// entry returns the record of ip, dropping the expired ones (and the oldest past passMaxIPs).
func (p *passFails) entry(ip string) *passEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.n == nil {
		p.n = map[string]*passEntry{}
	}
	now := time.Now()
	for k, e := range p.n {
		if now.Sub(time.Unix(0, e.last.Load())) > passFailExpiry && e.mu.TryLock() {
			delete(p.n, k)
			e.mu.Unlock()
		}
	}
	for len(p.n) >= passMaxIPs && p.n[ip] == nil {
		oldest, ot := "", now.UnixNano()
		for k, e := range p.n {
			if l := e.last.Load(); l < ot {
				oldest, ot = k, l
			}
		}
		if oldest == "" {
			break
		}
		delete(p.n, oldest)
	}
	e := p.n[ip]
	if e == nil {
		e = &passEntry{}
		p.n[ip] = e
	}
	e.last.Store(now.UnixNano())
	return e
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// passcodeCheck answers {ok}. It never returns the hash. After 3 wrong answers from one address every
// further attempt from it waits 1 s, which makes guessing slow; a right answer clears the count.
func (s *Server) passcodeCheck(r *http.Request) (any, error) {
	var in struct{ Passcode string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	e := s.pass.entry(clientIP(r))
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fails >= 3 {
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	// config.CheckPasscode compares with bcrypt, which is constant-time in the guess; no passcode set: never matches
	ok := config.CheckPasscode(s.cfg().Security.Passcode, in.Passcode)
	if ok {
		e.fails = 0
	} else {
		e.fails++
	}
	return map[string]any{"ok": ok}, nil
}
