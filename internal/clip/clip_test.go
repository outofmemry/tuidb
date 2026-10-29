package clip

import (
	"bytes"
	"errors"
	"io"
	"strings"
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

func TestSystemTool(t *testing.T) { // T-P2-12
	onPath := func(have ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, h := range have {
				if h == n {
					return "/usr/bin/" + n, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	x11 := env(map[string]string{"DISPLAY": ":0"})
	for _, c := range []struct {
		goos string
		env  func(string) string
		look func(string) (string, error)
		want string
	}{
		{"darwin", x11, onPath("pbcopy"), "pbcopy"},
		{"windows", x11, onPath("clip"), "clip"},
		{"linux", env(map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}), onPath("wl-copy", "xclip"), "wl-copy"},
		{"linux", x11, onPath("wl-copy", "xsel"), "xsel --clipboard --input"},
		{"linux", x11, onPath("xclip", "xsel"), "xclip -selection clipboard"},
		{"linux", env(nil), onPath("xclip"), ""}, // no display: nothing to copy to
		{"darwin", x11, onPath(), ""},
	} {
		if got := strings.Join(systemTool(c.goos, c.env, c.look), " "); got != c.want {
			t.Errorf("%s: got %q, want %q", c.goos, got, c.want)
		}
	}
	if System("x") != ErrNoTool {
		t.Error("tests must never write the real clipboard")
	}
}
