package web

import (
	"sync"
	"testing"
	"time"
	"webtuidb/internal/config"
)

// R-010: parallel wrong passcodes are serialised, so the 1 s delay adds up instead of overlapping.
func TestPasscodeAttemptsSerialised(t *testing.T) {
	hash, _ := config.HashPasscode("1234")
	e := newEnvCfg(t, "", func(c *config.Config) { c.Security.Passcode = hash })
	for i := 0; i < 3; i++ {
		e.ok("POST", "/api/passcode/check", map[string]any{"passcode": "no"})
	}
	st := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.call("POST", "/api/passcode/check", map[string]any{"passcode": "no"}, "tok")
		}()
	}
	wg.Wait()
	if d := time.Since(st); d < 2800*time.Millisecond {
		t.Fatalf("3 parallel attempts took %v; they must queue (1 s each)", d)
	}
}

// R-010: the per-address table is bounded and expires.
func TestPasscodeTableBounded(t *testing.T) {
	var p passFails
	for i := 0; i < passMaxIPs*2; i++ {
		p.entry(string(rune('a'+i%26)) + string(rune(i)))
	}
	if len(p.n) > passMaxIPs {
		t.Fatalf("%d entries", len(p.n))
	}
	e := p.entry("x")
	e.fails = 5
	e.last.Store(time.Now().Add(-2 * passFailExpiry).UnixNano())
	if e2 := p.entry("y"); e2 == nil || p.n["x"] != nil {
		t.Fatal("an idle address must expire")
	}
}

func TestPasscodeCheck(t *testing.T) {
	h, err := config.HashPasscode("1234")
	if err != nil {
		t.Fatal(err)
	}
	e := newEnvCfg(t, "", func(c *config.Config) { c.Security.Passcode = h; c.Security.LockAfterMins = 5 })
	if out := e.ok("POST", "/api/passcode/check", map[string]any{"passcode": "nope"}); out["ok"] != false {
		t.Fatalf("wrong passcode: %v", out)
	}
	out := e.ok("POST", "/api/passcode/check", map[string]any{"passcode": "1234"})
	if out["ok"] != true || len(out) != 1 {
		t.Fatalf("right passcode: %v", out)
	}
	m := e.ok("GET", "/api/meta", nil)
	sec := m["security"].(map[string]any)
	if sec["passcode"] != true || sec["lockAfterMins"].(float64) != 5 {
		t.Fatalf("security = %v", sec)
	}
	// three failures slow the fourth attempt down
	for i := 0; i < 3; i++ {
		e.ok("POST", "/api/passcode/check", map[string]any{"passcode": "x"})
	}
	start := time.Now()
	e.ok("POST", "/api/passcode/check", map[string]any{"passcode": "x"})
	if time.Since(start) < 900*time.Millisecond {
		t.Fatal("no delay after 3 failures")
	}
}
