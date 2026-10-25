package config

import (
	"os"
	"path/filepath"
)

// xdg returns an XDG base dir only when it is absolute (the spec says to ignore relative ones).
func xdg(name string) string {
	if x := os.Getenv(name); filepath.IsAbs(x) {
		return x
	}
	return ""
}

// ConfigDir is ~/.config/webtuidb (or $XDG_CONFIG_HOME/webtuidb). With no usable home it is relative;
// callers must check filepath.IsAbs (main does) rather than trust a CWD-relative file.
func ConfigDir() string {
	if x := xdg("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "webtuidb")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "webtuidb")
}

// DataDir is ~/.local/share/webtuidb (or $XDG_DATA_HOME/webtuidb): history, saved queries, state.
func DataDir() string {
	if x := xdg("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "webtuidb")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "share", "webtuidb")
}
