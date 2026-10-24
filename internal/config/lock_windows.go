//go:build windows

package config

// LockFile is a no-op on Windows (shortcut: no cross-process lock there, add LockFileEx if needed).
func LockFile(path string) (func(), error) { return func() {}, nil }
