package config

import (
	"os"
	"path/filepath"
)

// ConfigDir is ~/.config/tuidb (or $XDG_CONFIG_HOME/tuidb).
func ConfigDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "tuidb")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "tuidb")
}

// DataDir is ~/.local/share/tuidb (or $XDG_DATA_HOME/tuidb): history, saved queries, state.
func DataDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "tuidb")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "share", "tuidb")
}
