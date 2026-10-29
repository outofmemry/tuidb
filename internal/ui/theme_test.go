package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/config"
)

// T-P3-16: [theme.colors] entries that do nothing are reported.
func TestM18ThemeColorsWarn(t *testing.T) {
	e := colorsError(map[string]string{"accent": "notacolor", "nope": "#fff", "keyword": "#abc", "number": "208", "tag.mine": "#123456"})
	if !strings.Contains(e, `accent = "notacolor" is not a color`) || !strings.Contains(e, "nope is not a theme color") {
		t.Errorf("got %q", e)
	}
	if strings.Contains(e, "keyword") || strings.Contains(e, "number") || strings.Contains(e, "tag.mine") {
		t.Errorf("valid entries reported: %q", e)
	}
}

func TestM2ThemeTOML(t *testing.T) {
	out := ThemeTOML(config.Theme{Name: "light", Colors: map[string]string{"accent": "#123456"}})
	if !strings.HasPrefix(out, "[theme.colors]\n") || !strings.Contains(out, `"accent" = "#123456"`) || !strings.Contains(out, `"tag.local"`) {
		t.Fatalf("theme toml:\n%s", out)
	}
}

func TestM4ThemeNamesAndFile(t *testing.T) {
	for _, n := range []string{"Silver Dark", "jade-dark", "polaris_dark", "sunset-dark", "sunset-light"} {
		if namedPalettes[themeName(n)] == nil {
			t.Errorf("no palette for %q", n)
		}
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "mine.toml")
	if err := os.WriteFile(file, []byte("[theme.colors]\naccent = \"#010203\"\nkeyword = \"#aabbcc\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pal := palette(config.Theme{Name: "jade-dark", File: file, Colors: map[string]string{"keyword": "#ffffff"}})
	if pal["accent"] != "#010203" || pal["keyword"] != "#ffffff" || pal["string"] != namedPalettes["jade-dark"]["string"] {
		t.Fatalf("file colors must sit on the named palette, under [theme.colors]: %v", pal)
	}
	if themeError(config.Theme{File: filepath.Join(dir, "missing.toml")}) == "" {
		t.Error("a missing theme file must be reported")
	}
	termDark = false
	defer func() { termDark = true }()
	if palette(config.Theme{Name: "auto"})["accent"] != lightPalette["accent"] {
		t.Error("auto must follow a light terminal")
	}
}

func TestM5Ansi16Fallback(t *testing.T) {
	got := ansi16(map[string]string{"a": "#f7768e", "b": "#000000", "c": "red"})
	if got["b"] != "0" || got["c"] != "red" || got["a"] != "9" {
		t.Fatalf("%v", got)
	}
	t.Setenv("COLORTERM", "")
	t.Setenv("TERM", "xterm")
	t.Setenv("WEBTUIDB_COLORS", "")
	if !limitedColors() {
		t.Fatal("plain xterm without COLORTERM is a 16-color terminal")
	}
	t.Setenv("COLORTERM", "truecolor")
	if limitedColors() {
		t.Fatal("truecolor must keep the palette")
	}
}
