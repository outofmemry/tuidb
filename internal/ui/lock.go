// lock.go: Passcode lock and idle locking.

package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/keymap"
)

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
	pre   bool // the prefix was pressed: the next key may be quit (T-P3-11), nothing else
	armed bool // quit was asked once with uncommitted changes: the next quit goes through
}

// quit is the lock screen's way out: the bound quit key after the prefix. Dialogs cannot open over the lock, so
// uncommitted changes are confirmed by pressing it a second time.
func (l *lockDlg) quit() tea.Cmd {
	for _, w := range l.a.wss {
		if (w.dirty() || w.applying) && !l.armed {
			l.armed = true
			l.err = "uncommitted changes will be lost: press " + boundKey(l.a.d.Keys, keymap.Global, "quit") + " again to quit"
			return nil
		}
	}
	return l.a.quitNow()
}

func (a *App) passcodeSet() bool { return a.d.Cfg.Security.Passcode != "" }

// lock puts the lock screen on top; without a passcode it says how to set one.
func (a *App) lock() tea.Cmd {
	if !a.passcodeSet() {
		return a.fail(errors.New("no passcode is set: run webtuidb -set-passcode and add the line it prints to config.toml"))
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
	if m > 0 && a.passcodeSet() && !a.locked() && !a.term.lastKey.IsZero() && now.Sub(a.term.lastKey) >= time.Duration(m)*time.Minute {
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
		k := keymap.FromMsg(m)
		if l.pre {
			l.pre = false
			if act, ok := l.a.d.Keys.Lookup(keymap.Global, k); ok && act == "quit" {
				return l, l.quit()
			}
			return l, nil
		}
		if k == l.a.d.Keys.Prefix {
			l.pre = true
			return l, nil
		}
		switch m.Keystroke() {
		case "enter":
			if config.CheckPasscode(l.a.d.Cfg.Security.Passcode, l.in.Value()) {
				l.a.term.lastKey = l.now()
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
	foot := "Enter: unlock"
	if q := boundKey(l.a.d.Keys, keymap.Global, "quit"); q != "" {
		foot += "   " + q + ": quit"
	}
	return box(th, "🔒 Locked", body, foot, w)
}

// cover is the full-screen view: nothing of the workspace shows while the lock is up.
func (l *lockDlg) cover(th Theme, W, H int) string {
	return composite(strings.TrimRight(strings.Repeat("\n", H), "\n"), l.View(th, W, H), W, H)
}
