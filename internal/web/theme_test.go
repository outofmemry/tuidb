package web

import (
	"os"
	"path/filepath"
	"testing"
	"webtuidb/internal/config"
)

func TestWebThemeResolvesNamesAndFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "t.toml")
	os.WriteFile(f, []byte("[theme.colors]\nwarn = \"#111111\"\n"), 0o600)
	got := webTheme(config.Theme{Name: "sunset-light", File: f, Colors: map[string]string{"ok": "#222222"}})
	c := got["Colors"].(map[string]string)
	if got["Name"] != "light" || c["accent"] != webPalettes["sunset-light"]["accent"] || c["warn"] != "#111111" || c["ok"] != "#222222" {
		t.Fatalf("theme = %v", got)
	}
	if webTheme(config.Theme{Name: "auto"})["Name"] != "auto" {
		t.Fatal("auto must pass through to prefers-color-scheme")
	}
}
