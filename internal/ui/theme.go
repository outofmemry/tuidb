// Package ui holds the Bubble Tea models: welcome screen, workspaces, tabs, dialogs.
package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"tuidb/internal/config"
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
}

var lightPalette = map[string]string{
	"dim": "#6c7086", "accent": "#2e5cb8", "error": "#c4264a", "ok": "#3a7d0a", "warn": "#a8680a",
	"border": "#b8bdd1", "header_bg": "#dfe3f0", "selection": "#c8d3f5", "sel_cell": "#2e5cb8", "sel_cell_fg": "#ffffff",
	"modified_row": "#fff3c4", "new_row": "#d8f2e0", "deleted_row": "#f9d6dd", "alt_row": "#f1f3fa",
	"keyword": "#7b3fc4", "string": "#3a7d0a", "comment": "#8a8fa8", "number": "#b25000",
	"status_bg": "#dfe3f0", "status_fg": "#2c3250", "prefix_bg": "#a8680a", "prefix_fg": "#ffffff", "popup_bg": "#eceff8",
	"tag.local": "#3a7d0a", "tag.testing": "#007c91", "tag.development": "#2e5cb8", "tag.staging": "#a8680a", "tag.production": "#c4264a",
	"driver.postgres": "#2e5cb8", "driver.mysql": "#a8680a", "driver.clickhouse": "#8a6d00", "driver.redis": "#c4264a", "driver.mongodb": "#3a7d0a",
}

func NewTheme(t config.Theme) Theme {
	base := darkPalette
	if t.Name == "light" {
		base = lightPalette
	}
	pal := make(map[string]string, len(base))
	for k, v := range base {
		pal[k] = v
	}
	for k, v := range t.Colors {
		pal[k] = v
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
