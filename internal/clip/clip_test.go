package clip

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

type buf struct{ bytes.Buffer }

func (*buf) Close() error { return nil }

func TestWrite(t *testing.T) {
	var b buf
	tty = func() (io.WriteCloser, error) { return &b, nil }
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("WEBTUIDB_OSC52", "")
	if err := Write("hi"); !errors.Is(err, ErrNotTerminal) || b.Len() != 0 {
		t.Fatalf("local session wrote %q, err %v", b.String(), err)
	}
	t.Setenv("SSH_CONNECTION", "1.2.3.4 1 5.6.7.8 22")
	if err := Write("hi"); err != nil || b.String() != "\x1b]52;c;aGk=\x07" {
		t.Fatalf("got %q, err %v", b.String(), err)
	}
}
