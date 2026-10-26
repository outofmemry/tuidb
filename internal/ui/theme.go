// Package ui holds the Bubble Tea models: welcome screen, workspaces, tabs, dialogs.
package ui

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/BurntSushi/toml"

	"webtuidb/internal/config"
)

// Theme is built once from a palette; [theme.colors] in config.toml overrides single entries.
type Theme struct {
	Dim, Accent, Err, OK, Warn       lipgloss.Style
	Bold, Title                      lipgloss.Style
	Border                           lipgloss.Style
	HeaderCell, Cell                 lipgloss.Style
	SelRow, SelCell                  lipgloss.Style
	Null                             lipgloss.Style
	Modified, New, Deleted           lipgloss.Style
	Keyword, String, Comment, Number lipgloss.Style
	StatusBar, StatusKey, Prefix     lipgloss.Style
	TabActive, TabIdle               lipgloss.Style
	Popup                            lipgloss.Style
	AltRow                           lipgloss.Style
	CurRow                           lipgloss.Style // the row under the cursor
	Key                              lipgloss.Style // a key name in a hint line
	Hint                             lipgloss.Style // hint label text
	Section                          lipgloss.Style // sidebar / list section headers
	pal                              map[string]string
}

var darkPalette = map[string]string{
	"dim": "#7f8796", "accent": "#7aa2f7", "error": "#f7768e", "ok": "#9ece6a", "warn": "#e0af68",
	"border": "#3b4261", "header_bg": "#292e42", "selection": "#364a82", "sel_cell": "#7aa2f7", "sel_cell_fg": "#1a1b26",
	"modified_row": "#3d3a1f", "new_row": "#1f3d2a", "deleted_row": "#4a2430", "alt_row": "#1f2335",
	"keyword": "#bb9af7", "string": "#9ece6a", "comment": "#565f89", "number": "#ff9e64",
	"status_bg": "#292e42", "status_fg": "#c0caf5", "prefix_bg": "#e0af68", "prefix_fg": "#1a1b26", "popup_bg": "#24283b",
	"tag.local": "#9ece6a", "tag.testing": "#7dcfff", "tag.development": "#7aa2f7", "tag.staging": "#e0af68", "tag.production": "#f7768e",
	"driver.postgres": "#7aa2f7", "driver.mysql": "#e0af68", "driver.clickhouse": "#f7c948", "driver.redis": "#f7768e", "driver.mongodb": "#9ece6a",
	"driver.mariadb":   "#c49a6c",
	"driver.cassandra": "#73daca", "driver.dynamodb": "#ff9e64",
	"driver.redshift": "#7dcfff", "driver.greenplum": "#9ece6a", "driver.vertica": "#bb9af7", "driver.singlestore": "#ff9e64",
}

var lightPalette = map[string]string{
	"dim": "#6c7086", "accent": "#2e5cb8", "error": "#c4264a", "ok": "#3a7d0a", "warn": "#a8680a",
	"border": "#b8bdd1", "header_bg": "#dfe3f0", "selection": "#c8d3f5", "sel_cell": "#2e5cb8", "sel_cell_fg": "#ffffff",
	"modified_row": "#fff3c4", "new_row": "#d8f2e0", "deleted_row": "#f9d6dd", "alt_row": "#f1f3fa",
	"keyword": "#7b3fc4", "string": "#3a7d0a", "comment": "#8a8fa8", "number": "#b25000",
	"status_bg": "#dfe3f0", "status_fg": "#2c3250", "prefix_bg": "#a8680a", "prefix_fg": "#ffffff", "popup_bg": "#eceff8",
	"tag.local": "#3a7d0a", "tag.testing": "#007c91", "tag.development": "#2e5cb8", "tag.staging": "#a8680a", "tag.production": "#c4264a",
	"driver.postgres": "#2e5cb8", "driver.mysql": "#a8680a", "driver.clickhouse": "#8a6d00", "driver.redis": "#c4264a", "driver.mongodb": "#3a7d0a",
	"driver.mariadb":   "#7a5a2e",
	"driver.cassandra": "#1a7a66", "driver.dynamodb": "#b25000",
	"driver.redshift": "#007c91", "driver.greenplum": "#3a7d0a", "driver.vertica": "#7b3fc4", "driver.singlestore": "#b25000",
}

// variant is base with over laid on top.
func variant(base, over map[string]string) map[string]string {
	out := make(map[string]string, len(base))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

// namedPalettes are the built-in themes beside dark and light.
var namedPalettes = map[string]map[string]string{
	"silver-dark": variant(darkPalette, map[string]string{
		"dim": "#8b9099", "accent": "#c0c6d4", "border": "#3d4048", "header_bg": "#2b2e35", "selection": "#3c4150", "sel_cell": "#c0c6d4", "sel_cell_fg": "#1b1d22",
		"alt_row": "#202328", "keyword": "#a9b4d0", "string": "#b5c7a5", "comment": "#5b6070", "number": "#d4b483", "status_bg": "#2b2e35", "status_fg": "#d6d9e0",
		"prefix_bg": "#d4b483", "prefix_fg": "#1b1d22", "popup_bg": "#25282e", "modified_row": "#3a3a28", "new_row": "#26382c", "deleted_row": "#43282e"}),
	"jade-dark": variant(darkPalette, map[string]string{
		"dim": "#6f8f84", "accent": "#3fd9a0", "border": "#2a4a40", "header_bg": "#1d332c", "selection": "#1f4d3f", "sel_cell": "#3fd9a0", "sel_cell_fg": "#0b1b16",
		"alt_row": "#14241f", "keyword": "#7ce3c3", "string": "#b9e07a", "comment": "#46695c", "number": "#ffbe6b", "status_bg": "#1d332c", "status_fg": "#cfeee3",
		"prefix_bg": "#ffbe6b", "prefix_fg": "#0b1b16", "popup_bg": "#172b25", "modified_row": "#3b3a1c", "new_row": "#1c4030", "deleted_row": "#4a2530"}),
	"polaris-dark": variant(darkPalette, map[string]string{
		"dim": "#7d8fa3", "accent": "#7fd1ff", "border": "#2e4057", "header_bg": "#1f2d3f", "selection": "#2a4b6d", "sel_cell": "#7fd1ff", "sel_cell_fg": "#0d1824",
		"alt_row": "#162231", "keyword": "#b4a7ff", "string": "#8fe3c8", "comment": "#53657a", "number": "#ffb48a", "status_bg": "#1f2d3f", "status_fg": "#d3e4f5",
		"prefix_bg": "#ffd479", "prefix_fg": "#0d1824", "popup_bg": "#1a283a"}),
	"sunset-dark": variant(darkPalette, map[string]string{
		"dim": "#9c8a86", "accent": "#ff8c5a", "border": "#4a3436", "header_bg": "#33232a", "selection": "#5a3340", "sel_cell": "#ff8c5a", "sel_cell_fg": "#1f1216",
		"alt_row": "#261a1f", "keyword": "#ff6f91", "string": "#f9c74f", "comment": "#6e5558", "number": "#ffa86b", "status_bg": "#33232a", "status_fg": "#f2dcd6",
		"prefix_bg": "#f9c74f", "prefix_fg": "#1f1216", "popup_bg": "#2c1d23"}),
	"sunset-light": variant(lightPalette, map[string]string{
		"dim": "#8a7570", "accent": "#d9531e", "border": "#e0c9c0", "header_bg": "#f6e3da", "selection": "#fbd3c0", "sel_cell": "#d9531e", "sel_cell_fg": "#ffffff",
		"alt_row": "#fcf1ec", "keyword": "#b3224f", "string": "#5e7d12", "comment": "#a78e86", "number": "#c2620a", "status_bg": "#f6e3da", "status_fg": "#4a2a22",
		"prefix_bg": "#c2620a", "prefix_fg": "#ffffff", "popup_bg": "#faece5"}),
}

// termDark is what theme.name = "auto" resolves to: dark until the terminal reports a light background.
var termDark = true

// themeName normalises "Silver Dark" / "silver_dark" to "silver-dark".
func themeName(n string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(n)), " ", "-"), "_", "-")
}

// themeFileColors reads the [theme.colors] table of an exported theme file.
func themeFileColors(path string) (map[string]string, error) {
	var f struct {
		Theme struct {
			Colors map[string]string `toml:"colors"`
		} `toml:"theme"`
	}
	if _, err := toml.DecodeFile(path, &f); err != nil {
		return nil, fmt.Errorf("theme file %s: %w", path, err)
	}
	return f.Theme.Colors, nil
}

// palette is the named palette, then the colors of theme.file, then the [theme.colors] overrides.
func palette(t config.Theme) map[string]string {
	name := themeName(t.Name)
	if name == "auto" {
		name = map[bool]string{true: "dark", false: "light"}[termDark]
	}
	base, ok := namedPalettes[name]
	switch {
	case ok:
	case name == "light":
		base = lightPalette
	default:
		base = darkPalette
	}
	pal := variant(base, nil)
	if t.File != "" {
		if c, err := themeFileColors(t.File); err == nil { // a bad file is reported once at startup (themeError)
			pal = variant(pal, c)
		}
	}
	return variant(pal, t.Colors)
}

// themeError is the problem with theme.file, "" when there is none.
func themeError(t config.Theme) string {
	if t.File == "" {
		return ""
	}
	if _, err := themeFileColors(t.File); err != nil {
		return err.Error()
	}
	return ""
}

// ThemeTOML is the resolved palette as a [theme.colors] table, ready to paste into config.toml.
func ThemeTOML(t config.Theme) string {
	pal := palette(t)
	keys := make([]string, 0, len(pal))
	for k := range pal {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("[theme.colors]\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%q = %q\n", k, pal[k])
	}
	return b.String()
}

func NewTheme(t config.Theme) Theme {
	pal := palette(t)
	if limitedColors() {
		pal = ansi16(pal)
	}
	fg := func(k string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(pal[k])) }
	bg := func(k string) lipgloss.Style { return lipgloss.NewStyle().Background(lipgloss.Color(pal[k])) }
	return Theme{
		pal: pal,
		Dim: fg("dim"), Accent: fg("accent"), Err: fg("error"), OK: fg("ok"), Warn: fg("warn"),
		Bold: lipgloss.NewStyle().Bold(true), Title: fg("accent").Bold(true),
		Border:     fg("border"),
		HeaderCell: bg("header_bg").Bold(true), Cell: lipgloss.NewStyle(),
		SelRow:   bg("selection"),
		SelCell:  lipgloss.NewStyle().Background(lipgloss.Color(pal["sel_cell"])).Foreground(lipgloss.Color(pal["sel_cell_fg"])).Bold(true),
		Null:     fg("dim").Italic(true),
		Modified: bg("modified_row"), New: bg("new_row"), Deleted: bg("deleted_row").Strikethrough(true), AltRow: bg("alt_row"),
		Keyword: fg("keyword").Bold(true), String: fg("string"), Comment: fg("comment").Italic(true), Number: fg("number"),
		StatusBar: bg("status_bg").Foreground(lipgloss.Color(pal["status_fg"])),
		StatusKey: lipgloss.NewStyle().Background(lipgloss.Color(pal["status_bg"])).Foreground(lipgloss.Color(pal["accent"])).Bold(true),
		Prefix:    lipgloss.NewStyle().Background(lipgloss.Color(pal["prefix_bg"])).Foreground(lipgloss.Color(pal["prefix_fg"])).Bold(true),
		TabActive: lipgloss.NewStyle().Foreground(lipgloss.Color(pal["sel_cell_fg"])).Background(lipgloss.Color(pal["accent"])).Bold(true),
		TabIdle:   bg("header_bg").Foreground(lipgloss.Color(pal["dim"])),
		Popup:     bg("popup_bg"),
		CurRow:    bg("header_bg"),
		Key:       fg("accent").Bold(true),
		Hint:      fg("dim"),
		Section:   fg("dim").Bold(true),
	}
}

// LogoStyle colors line i of n of the wordmark: a gradient from the accent to the keyword color.
func (t Theme) LogoStyle(i, n int) lipgloss.Style {
	f := 0.0
	if n > 1 {
		f = float64(i) / float64(n-1)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(blend(t.pal["accent"], t.pal["keyword"], f))).Bold(true)
}

// blend mixes two #rrggbb colors; anything else (a named color override) returns a unchanged.
func blend(a, b string, f float64) string {
	var ar, ag, ab, br, bg, bb int
	if _, err := fmt.Sscanf(a, "#%02x%02x%02x", &ar, &ag, &ab); err != nil {
		return a
	}
	if _, err := fmt.Sscanf(b, "#%02x%02x%02x", &br, &bg, &bb); err != nil {
		return a
	}
	mix := func(x, y int) int { return x + int(float64(y-x)*f) }
	return fmt.Sprintf("#%02x%02x%02x", mix(ar, br), mix(ag, bg), mix(ab, bb))
}

// DriverStyle colors a driver name (postgres blue, mysql orange, ...).
func (t Theme) DriverStyle(driver string) lipgloss.Style {
	if c := t.pal["driver."+driver]; c != "" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(c))
	}
	return t.Dim
}

// Hints renders "key label" pairs as one dim hint line: key in accent, label dimmed.
func (t Theme) Hints(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, t.Key.Render(pairs[i])+" "+t.Hint.Render(pairs[i+1]))
	}
	return strings.Join(parts, t.Hint.Render("  ·  "))
}

// TagStyle colors a connection tag (production = red...). A connection color wins over the tag default.
func (t Theme) TagStyle(tag, color string) lipgloss.Style {
	c := color
	if c == "" {
		c = t.pal["tag."+tag]
	}
	if c == "" {
		c = t.pal["dim"]
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(c)).Foreground(lipgloss.Color("#1a1b26")).Bold(true)
}

// limitedColors: the terminal has no truecolor or 256-color support, so #rrggbb would be guessed badly.
// WEBTUIDB_COLORS=16 forces the 16-color palette, any other value (truecolor, 256) forbids it.
func limitedColors() bool {
	if v := os.Getenv("WEBTUIDB_COLORS"); v != "" {
		return v == "16"
	}
	ct, term := os.Getenv("COLORTERM"), os.Getenv("TERM")
	if ct != "" || term == "" || term == "dumb" {
		return false
	}
	for _, rich := range []string{"256", "direct", "truecolor", "kitty", "ghostty", "wezterm", "alacritty"} {
		if strings.Contains(term, rich) {
			return false
		}
	}
	return true
}

// ansiRGB are the 16 ANSI colors as most terminals draw them.
var ansiRGB = [16][3]int{{0, 0, 0}, {205, 49, 49}, {13, 188, 121}, {229, 229, 16}, {36, 114, 200}, {188, 63, 188}, {17, 168, 205}, {229, 229, 229},
	{102, 102, 102}, {241, 76, 76}, {35, 209, 139}, {245, 245, 67}, {59, 142, 234}, {214, 112, 214}, {41, 184, 219}, {255, 255, 255}}

// ansi16 maps every #rrggbb color of pal to the nearest of the 16 ANSI colors ("0".."15").
func ansi16(pal map[string]string) map[string]string {
	out := make(map[string]string, len(pal))
	for k, v := range pal {
		var r, g, b int
		if _, err := fmt.Sscanf(v, "#%02x%02x%02x", &r, &g, &b); err != nil {
			out[k] = v
			continue
		}
		best, bd := 0, 1<<30
		for i, c := range ansiRGB {
			if d := (r-c[0])*(r-c[0]) + (g-c[1])*(g-c[1]) + (b-c[2])*(b-c[2]); d < bd {
				best, bd = i, d
			}
		}
		out[k] = strconv.Itoa(best)
	}
	return out
}
