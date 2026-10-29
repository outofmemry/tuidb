package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ThemesDir holds installed themes (`webtuidb -theme-install`): one folder per theme with a theme.toml, or a <name>.toml.
func ThemesDir() string { return filepath.Join(ConfigDir(), "themes") }

// themeFile returns the theme TOML of an installed theme, "" when there is none.
func themeFile(name string) string {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return ""
	}
	for _, p := range []string{filepath.Join(ThemesDir(), name, "theme.toml"), filepath.Join(ThemesDir(), name+".toml")} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// InstalledThemes lists the installed theme names, sorted.
func InstalledThemes() []string {
	ents, _ := os.ReadDir(ThemesDir())
	var out []string
	for _, e := range ents {
		n := strings.TrimSuffix(e.Name(), ".toml")
		if strings.HasPrefix(n, ".") || themeFile(n) == "" || slices.Contains(out, n) {
			continue
		}
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// useInstalledTheme points Theme.File at the installed theme named by Theme.Name (when File is not set), so both
// UIs load its [theme.colors] over their dark palette without knowing about the themes folder.
func (c *Config) useInstalledTheme() {
	if c.Theme.File == "" && !slices.Contains(ThemeNames, c.Theme.Name) {
		c.Theme.File = themeFile(c.Theme.Name)
	}
}
