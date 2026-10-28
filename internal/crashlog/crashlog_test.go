package crashlog

import (
	"os"
	"strings"
	"testing"
)

func TestRecoverWritesStackAndConsole(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	func() {
		defer func() {
			if recover() == nil {
				t.Error("Recover must re-panic")
			}
		}()
		defer Recover(func() []string { return []string{"select 1", "ok"} })
		panic("boom")
	}()
	b, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"panic: boom", "crashlog_test.go", "select 1", "ok"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("crash log lacks %q:\n%s", want, b)
		}
	}
	if st, _ := os.Stat(Path()); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode())
	}
}

func TestRecoverNoPanicWritesNothing(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	func() { defer Recover(nil) }()
	if _, err := os.Stat(Path()); err == nil {
		t.Fatal("no panic, no file")
	}
}

// Catch recovers, logs and calls back; it must be deferred directly for recover() to work.
func TestCatch(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	var got any
	func() {
		defer Catch(func() []string { return []string{"line"} }, func(r any) { got = r })
		panic("boom")
	}()
	if got != "boom" {
		t.Fatalf("onPanic got %v", got)
	}
	if b, err := os.ReadFile(Path()); err != nil || !strings.Contains(string(b), "boom") || !strings.Contains(string(b), "line") {
		t.Fatalf("crash log: %q %v", b, err)
	}
}

// P2-16: a log past the cap rolls over instead of growing without bound.
func TestLogRollsOver(t *testing.T) {
	p := t.TempDir() + "/crash.log"
	os.WriteFile(p, []byte(strings.Repeat("x", maxLogBytes+1)), 0o600)
	if err := write(p, "boom", []byte("stack"), nil); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Size() > 4096 {
		t.Fatalf("log not rolled: %d bytes", st.Size())
	}
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Fatal("previous log not kept")
	}
}
