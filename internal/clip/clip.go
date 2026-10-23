// Package clip copies text to the clipboard of the machine the terminal is on, over SSH, with OSC 52.
package clip

import (
	"encoding/base64"
	"errors"
	"io"
	"os"
	"sync"
)

// ErrNotTerminal means OSC 52 is not wanted here: the caller falls back to tea.SetClipboard.
var ErrNotTerminal = errors.New("clip: not an SSH session (set WEBTUIDB_OSC52=1 to force OSC 52)")

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
