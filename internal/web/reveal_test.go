package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRevealOnlyFromLoopback(t *testing.T) {
	e := newEnv(t, "")
	req := httptest.NewRequest("POST", "/api/reveal", strings.NewReader(`{"path":"/tmp"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.9:5555"
	_, err := e.s.reveal(req)
	ae, ok := err.(*apiErr)
	if !ok || ae.Code != 403 || ae.Msg != "reveal works only on the machine running webtuidb" {
		t.Fatalf("err = %v", err)
	}
}
