// Package clip copies text to the clipboard: with OSC 52 to the machine the terminal is on (over SSH), or with the
// system clipboard tool (pbcopy, wl-copy, xclip, xsel, clip) on this machine.
package clip

import (
	"encoding/base64"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// ErrNotTerminal means OSC 52 is not wanted here: the caller falls back to tea.SetClipboard.
var ErrNotTerminal = errors.New("clip: not an SSH session (set WEBTUIDB_OSC52=1 to force OSC 52)")

// ErrNoTool means no system clipboard tool is on PATH.
var ErrNoTool = errors.New("clip: no clipboard tool (pbcopy, wl-copy, xclip, xsel, clip)")

// tty is where the escape goes; a variable so tests can swap it.
var tty = func() (io.WriteCloser, error) { return os.OpenFile("/dev/tty", os.O_WRONLY, 0) }

// Available reports whether Write will use OSC 52.
func Available() bool {
	return os.Getenv("WEBTUIDB_OSC52") == "1" || os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CONNECTION") != ""
}

var mu sync.Mutex // serialises writers only; shortcut: it cannot sync with the Bubble Tea renderer, so a sequence may still interleave with a frame

// Write sends text as an OSC 52 "set clipboard" sequence to the controlling terminal.
func Write(text string) error {
	if !Available() {
		return ErrNotTerminal
	}
	mu.Lock()
	defer mu.Unlock()
	w, err := tty()
	if err != nil {
		return err
	}
	defer w.Close()
	// shortcut: some terminals cap OSC 52 near 100 KB and drop longer payloads, not split here.
	_, err = io.WriteString(w, "\x1b]52;c;"+base64.StdEncoding.EncodeToString([]byte(text))+"\x07")
	return err
}

// System copies text with this machine's clipboard tool (T-P2-12). Tests never touch the real clipboard.
// shortcut: Windows clip.exe reads the ANSI code page, so non-ASCII text may arrive garbled there.
func System(text string) error {
	args := systemTool(runtime.GOOS, os.Getenv, exec.LookPath)
	if args == nil || testing.Testing() {
		return ErrNoTool
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// systemTool is the first clipboard command found for goos: pbcopy, clip, else wl-copy (Wayland) / xclip / xsel (X11).
func systemTool(goos string, env func(string) string, look func(string) (string, error)) []string {
	var cands [][]string
	switch goos {
	case "darwin":
		cands = [][]string{{"pbcopy"}}
	case "windows":
		cands = [][]string{{"clip"}}
	default:
		if env("WAYLAND_DISPLAY") != "" {
			cands = append(cands, []string{"wl-copy"})
		}
		if env("DISPLAY") != "" {
			cands = append(cands, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
		}
	}
	for _, c := range cands {
		if _, err := look(c[0]); err == nil {
			return c
		}
	}
	return nil
}

// TerminalMayCopy reports whether OSC 52 sent to this terminal can reach a clipboard. It cannot be confirmed;
// only terminals known to ignore it say no. shortcut: tmux with set-clipboard off is not detected.
func TerminalMayCopy() bool {
	return os.Getenv("TERM") != "dumb" && os.Getenv("TERM_PROGRAM") != "Apple_Terminal"
}
