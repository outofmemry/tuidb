// Package crashlog appends a panic, its stack and the last console lines to a local file. Nothing is sent anywhere.
package crashlog

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"webtuidb/internal/config"
)

// Path is ~/.local/share/webtuidb/crash.log.
func Path() string { return filepath.Join(config.DataDir(), "crash.log") }

// Recover is deferred at the top of main. On a panic it appends the panic, the stack and the last 20 lines
// of console() (may be nil) to Path(), tells the user where, and re-panics so the exit status stays non-zero.
func Recover(console func() []string) {
	r := recover()
	if r == nil {
		return
	}
	Record(r, debug.Stack(), console)
	panic(r)
}

// Catch is deferred directly (defer crashlog.Catch(console, onPanic)) at the top of a goroutine such as a web
// handler or a job: it recovers a panic, writes the crash log like Recover and calls onPanic with the value instead
// of re-panicking, so one request or job fails and the process keeps serving. http.ErrAbortHandler is re-panicked,
// as net/http expects.
func Catch(console func() []string, onPanic func(r any)) {
	r := recover()
	if r == nil {
		return
	}
	if r == http.ErrAbortHandler {
		panic(r)
	}
	Record(r, debug.Stack(), console)
	if onPanic != nil {
		onPanic(r)
	}
}

// Record appends a panic to the crash log and tells the user where.
func Record(r any, stack []byte, console func() []string) {
	var lines []string
	if console != nil {
		func() { // a broken console source must not hide the real panic
			defer func() { _ = recover() }()
			lines = console()
		}()
	}
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	if err := write(Path(), r, stack, lines); err != nil {
		fmt.Fprintln(os.Stderr, "webtuidb: could not write the crash log:", err)
	} else {
		fmt.Fprintln(os.Stderr, "webtuidb: crashed; details were written to", Path(), "(nothing was sent anywhere)")
	}
}

// maxLogBytes is when the log rolls over to path+".1".
const maxLogBytes = 2 << 20

func write(path string, r any, stack []byte, lines []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogBytes {
		_ = os.Rename(path, path+".1") // keep one generation; a panic on a polled endpoint must not fill the disk
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "=== %s panic: %v\n%s\n--- console (last %d lines)\n", time.Now().Format(time.RFC3339), r, stack, len(lines))
	for _, l := range lines {
		fmt.Fprintln(f, l)
	}
	fmt.Fprintln(f)
	return nil
}
