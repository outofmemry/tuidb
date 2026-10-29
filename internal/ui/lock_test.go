package ui

import (
	"strings"
	"testing"
	"time"

	"webtuidb/internal/config"
)

func TestM15PasscodeLock(t *testing.T) {
	h := lockHarness(t)
	h.a.Init() // starts locked
	if !h.a.locked() {
		t.Fatal("a passcode is set: the app starts locked")
	}
	h.press("enter") // would connect without the lock
	if h.a.ws() != nil || len(h.connected) != 0 {
		t.Fatal("keys must not reach the app while locked")
	}
	h.want("Locked")
	h.wantNot("shop") // the connection list is covered
	h.typeText("0000")
	h.press("enter")
	h.want("wrong passcode")
	if !h.a.locked() {
		t.Fatal("wrong passcode unlocked")
	}
	h.typeText("1234")
	h.press("enter")
	if h.a.locked() {
		t.Fatal("right passcode did not unlock")
	}
	h.want("shop")
}

func TestM15PasscodeThreeTriesDelay(t *testing.T) {
	h := lockHarness(t)
	h.a.run("lock")
	l := h.a.overlays[0].(*lockDlg)
	clock := time.Now()
	l.now = func() time.Time { return clock }
	for i := 0; i < 3; i++ {
		h.typeText("9")
		h.press("enter")
	}
	h.want("wait 10 s")
	h.typeText("1234")
	h.press("enter")
	if !h.a.locked() {
		t.Fatal("the right passcode must wait out the delay")
	}
	clock = clock.Add(11 * time.Second)
	h.typeText("1234")
	h.press("enter")
	if h.a.locked() {
		t.Fatal("after the delay the passcode works")
	}
}

func TestM15IdleLock(t *testing.T) {
	h := lockHarness(t)
	h.a.term.lastKey = time.Now()
	if h.a.idleCheck(time.Now().Add(4*time.Minute)) != nil || h.a.locked() {
		t.Fatal("not idle long enough")
	}
	h.a.idleCheck(time.Now().Add(6 * time.Minute))
	if !h.a.locked() {
		t.Fatal("lock_after_mins passed: expected the lock")
	}
}

func TestM15LockWithoutPasscodeExplains(t *testing.T) {
	h := newHarness(t, nil)
	h.a.run("lock")
	if h.a.locked() || !strings.Contains(h.a.toast, "-set-passcode") {
		t.Errorf("toast %q locked=%v", h.a.toast, h.a.locked())
	}
}

// P2-75: a secret prompt while the app is locked fails instead of hanging.
func TestM16AskSecretWhileLocked(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.Security.Passcode = "x" })
	h.a.push(&lockDlg{a: h.a, in: lineEdit{Secret: true}, now: time.Now})
	type res struct {
		v   string
		err error
	}
	out := make(chan res, 1)
	go func() { v, err := h.a.askSecret("Password", "shop"); out <- res{v, err} }()
	select {
	case f := <-h.a.mcpUI:
		f()
	case <-time.After(2 * time.Second):
		t.Fatal("the prompt never reached the UI loop")
	}
	select {
	case r := <-out:
		if r.err == nil || r.err.Error() != "locked" {
			t.Errorf("got %q, %v", r.v, r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("askSecret blocked while locked")
	}
	if len(h.a.overlays) != 1 {
		t.Errorf("a dialog was queued behind the lock: %d overlays", len(h.a.overlays))
	}
}

// T-P3-11: Ctrl+C outside the editor says how to quit; the lock screen offers and honours the quit key.
func TestM18CtrlCHintAndLockQuit(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.press("ctrl+c")
	h.want("Quit with C-d q")

	hash, err := config.HashPasscode("1234")
	if err != nil {
		t.Fatal(err)
	}
	h.a.d.Cfg.Security.Passcode = hash
	h.pump(h.a.lock())
	h.want("C-d q  quit") // the box footer draws "key  what" pairs
	h.press("ctrl+d", "q")
	if !h.a.quit {
		t.Fatal("C-d q on the lock screen must quit")
	}
}

func TestM4PasswordAskPrompts(t *testing.T) {
	h := newHarness(t, nil)
	got := make(chan string, 1)
	go func() {
		p, _ := h.a.askSecret("Password", "shop")
		got <- p
	}()
	h.a.Update(liveMsg{<-h.a.mcpUI})
	h.want("Password")
	h.typeText("pw1")
	h.press("enter")
	if p := <-got; p != "pw1" {
		t.Fatalf("got %q", p)
	}
	go func() {
		_, err := h.a.askSecret("Password", "shop")
		got <- err.Error()
	}()
	h.a.Update(liveMsg{<-h.a.mcpUI})
	h.press("esc")
	if e := <-got; e != "cancelled" {
		t.Fatalf("got %q", e)
	}
}
