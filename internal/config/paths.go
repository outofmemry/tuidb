package config

import (
	"fmt"
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

// base resolves an XDG base directory, falling back to home-relative parts when the variable is unset.
func base(env string, fallback ...string) string {
	if x := xdg(env); x != "" {
		return x
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(append([]string{h}, fallback...)...)
}

// ConfigDir is ~/.config/webtuidb (or $XDG_CONFIG_HOME/webtuidb). With no usable home it is relative;
// callers must check filepath.IsAbs (main does) rather than trust a CWD-relative file.
func ConfigDir() string {
	return filepath.Join(base("XDG_CONFIG_HOME", ".config"), "webtuidb")
}

// DataDir is ~/.local/share/webtuidb (or $XDG_DATA_HOME/webtuidb): history, saved queries, state.
func DataDir() string {
	return filepath.Join(base("XDG_DATA_HOME", ".local", "share"), "webtuidb")
}

// MigrateLegacy moves the pre-rename tuidb directories to their webtuidb names, so existing
// connections, history, saved queries and UI state survive the rename. It runs once at startup:
// nothing happens when there is no legacy directory or when the new one already exists (the new
// directory wins, and the legacy one is left untouched). A failed move is an error rather than a
// silent fresh start, because starting empty would look like the user's data disappeared.
func MigrateLegacy() error {
	pairs := []struct{ old, new string }{
		{filepath.Join(base("XDG_CONFIG_HOME", ".config"), "tuidb"), ConfigDir()},
		{filepath.Join(base("XDG_DATA_HOME", ".local", "share"), "tuidb"), DataDir()},
	}
	for _, p := range pairs {
		if p.old == p.new {
			continue
		}
		if _, err := os.Stat(p.new); err == nil {
			continue
		}
		if _, err := os.Stat(p.old); err != nil {
			continue
		}
		if err := os.Rename(p.old, p.new); err != nil {
			// Another instance may have moved it between the stats above.
			if _, statErr := os.Stat(p.new); statErr == nil {
				continue
			}
			return fmt.Errorf("move %s to %s: %w", p.old, p.new, err)
		}
	}
	return nil
}
