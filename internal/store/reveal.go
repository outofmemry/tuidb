package store

import (
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
)

// revealCmd builds the argv that shows path in the file manager on goos.
func revealCmd(goos, path string) []string {
	switch goos {
	case "darwin":
		return []string{"open", "-R", path}
	case "windows":
		return []string{"explorer", "/select," + path}
	default:
		return []string{"xdg-open", filepath.Dir(path)}
	}
}

var revealRun = func(argv []string) error { return exec.Command(argv[0], argv[1:]...).Start() }

// Reveal shows path in the system file manager (on the machine webtuidb runs on).
func Reveal(path string) error {
	if path == "" {
		return errors.New("reveal: empty path")
	}
	return revealRun(revealCmd(runtime.GOOS, path))
}
