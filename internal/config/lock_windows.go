//go:build windows

package config

import (
	"os"

	"golang.org/x/sys/windows"
)

// LockFile takes an exclusive cross-process lock on path+".lock" and returns the unlock func.
// It serializes read-modify-write of files shared by a TUI and a web process.
func LockFile(path string) (func(), error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	h := windows.Handle(f.Fd())
	ol := new(windows.Overlapped) // offset 0, length 1: blocks until the holder unlocks, like flock(LOCK_EX)
	if err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol); err != nil {
		f.Close()
		return nil, err
	}
	return func() { windows.UnlockFileEx(h, 0, 1, 0, ol); f.Close() }, nil
}
