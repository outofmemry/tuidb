package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
)

// M15.37 passcode lock: with [security] passcode set the app starts locked, locks again after lock_after_mins
// without a key (see App.idleCheck) and on the `lock` action. The lock is an overlay that covers the whole screen;
// every key goes to it, so nothing behind it can be reached. Three wrong tries stop input for 10 seconds.

const (
	lockTries = 3
	lockDelay = 10 * time.Second
)

type lockDlg struct {
	a     *App
	in    lineEdit
	err   string
	tries int
	until time.Time // input is refused until then (after lockTries wrong passcodes)
	now   func() time.Time
}

func (a *App) passcodeSet() bool { return a.d.Cfg.Security.Passcode != "" }

// lock puts the lock screen on top; without a passcode it says how to set one.
func (a *App) lock() tea.Cmd {
	if !a.passcodeSet() {
		return a.say("no passcode is set: run webtuidb -set-passcode and add the line it prints to config.toml")
	}
	if !a.locked() {
		a.push(&lockDlg{a: a, in: lineEdit{Secret: true}, now: time.Now})
	}
	return nil
}

func (a *App) locked() bool {
	for _, o := range a.overlays {
		if _, ok := o.(*lockDlg); ok {
			return true
		}
	}
	return false
}

// idleCheck locks the app when the last key is older than lock_after_mins.
func (a *App) idleCheck(now time.Time) tea.Cmd {
	m := a.d.Cfg.Security.LockAfterMins
	if m > 0 && a.passcodeSet() && !a.locked() && !a.m15.lastKey.IsZero() && now.Sub(a.m15.lastKey) >= time.Duration(m)*time.Minute {
		return a.lock()
	}
	return nil
}

type idleTickMsg struct{}

func (a *App) idleTick() tea.Cmd {
	if a.d.NoTimers || a.d.Cfg.Security.LockAfterMins <= 0 || !a.passcodeSet() {
		return nil
	}
	return tea.Tick(15*time.Second, func(time.Time) tea.Msg { return idleTickMsg{} })
}

func (l *lockDlg) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if wait := l.until.Sub(l.now()); wait > 0 {
		if _, ok := msg.(tea.KeyPressMsg); ok {
			l.err = fmt.Sprintf("too many wrong tries: wait %d s", int(wait.Seconds())+1)
		}
		return l, nil
	}
	switch m := msg.(type) {
	case tea.PasteMsg:
		l.in.Handle(m)
	case tea.KeyPressMsg:
		switch m.Keystroke() {
		case "enter":
			if config.CheckPasscode(l.a.d.Cfg.Security.Passcode, l.in.Value()) {
				l.a.m15.lastKey = l.now()
				return nil, nil
			}
			l.in.Set("")
			l.tries++
			if l.tries >= lockTries {
				l.tries, l.until = 0, l.now().Add(lockDelay)
				l.err = fmt.Sprintf("wrong passcode: wait %d s", int(lockDelay.Seconds()))
			} else {
				l.err = fmt.Sprintf("wrong passcode (%d of %d tries)", l.tries, lockTries)
			}
		case "esc", "ctrl+c":
		default:
			l.in.Handle(m)
		}
	}
	return l, nil
}

func (l *lockDlg) View(th Theme, maxW, maxH int) string {
	w := min(maxW-4, 46)
	body := []string{
		th.Dim.Render("webtuidb is locked."),
		"",
		th.Accent.Bold(true).Render("passcode ") + l.in.View(w-14, th, true),
	}
	if l.err != "" {
		body = append(body, "", th.Err.Render(l.err))
	}
	return box(th, "🔒 Locked", body, "Enter: unlock", w)
}

// cover is the full-screen view: nothing of the workspace shows while the lock is up.
func (l *lockDlg) cover(th Theme, W, H int) string {
	return composite(strings.TrimRight(strings.Repeat("\n", H), "\n"), l.View(th, W, H), W, H)
}
