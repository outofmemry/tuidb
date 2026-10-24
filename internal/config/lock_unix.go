//go:build !windows

package config

import (
	"os"
	"syscall"
)

// LockFile takes an exclusive cross-process lock on path+".lock" and returns the unlock func.
// It serializes read-modify-write of files shared by a TUI and a web process.
func LockFile(path string) (func(), error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
