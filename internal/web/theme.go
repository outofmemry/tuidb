package web

import (
	"strings"
	"webtuidb/internal/config"

	"github.com/BurntSushi/toml"
)

// webPalettes colour the web variables (see web/src/utils/theme.ts) for the named built-in themes.
// shortcut: a small copy of the palette names, not the terminal's full tables; the terminal's exact colours are the upgrade.
var webPalettes = map[string]map[string]string{
	"silver-dark":  {"accent": "#b4bcc8", "error": "#e07a7a", "ok": "#9fc59a", "warn": "#d6b46a", "dim": "#8a909a", "border": "#3d424b", "selection": "#2f343c"},
	"jade-dark":    {"accent": "#4fc4a0", "error": "#ec7b84", "ok": "#7fd38a", "warn": "#d9b25a", "dim": "#7d9189", "border": "#2f4a43", "selection": "#26403a"},
	"polaris-dark": {"accent": "#6fa8ff", "error": "#ff7b8f", "ok": "#8fd694", "warn": "#f0c36a", "dim": "#8793ad", "border": "#344061", "selection": "#2a3556"},
	"sunset-dark":  {"accent": "#ff9a5c", "error": "#ff6b81", "ok": "#a8d672", "warn": "#ffc857", "dim": "#a0908a", "border": "#523a35", "selection": "#45302b"},
	"sunset-light": {"accent": "#d9622b", "error": "#c4264a", "ok": "#3a7d0a", "warn": "#a8680a", "dim": "#8a7a72", "border": "#e0cfc5", "selection": "#f6e3d8"},
}

// webTheme resolves config.Theme for the browser: Name is dark | light | auto (what data-theme and
// prefers-color-scheme understand), Colors the named palette, then the theme file, then [theme.colors].
func webTheme(t config.Theme) map[string]any {
	name, colors := t.Name, map[string]string{}
	for k, v := range webPalettes[name] {
		colors[k] = v
	}
	if strings.HasSuffix(name, "-light") || name == "light" {
		name = "light"
	} else if name != "auto" {
		name = "dark"
	}
	if t.File != "" {
		var f struct {
			Colors map[string]string
			Theme  struct{ Colors map[string]string }
		}
		if _, err := toml.DecodeFile(expandDir(t.File), &f); err == nil { // an unreadable file leaves the palette as is
			for _, m := range []map[string]string{f.Colors, f.Theme.Colors} {
				for k, v := range m {
					colors[k] = v
				}
			}
		}
	}
	for k, v := range t.Colors {
		colors[k] = v
	}
	return map[string]any{"Name": name, "Colors": colors}
}
