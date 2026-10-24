package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Logo is the welcome-page wordmark (figlet "standard" face). Five lines, 44 cells wide.
var Logo = []string{
	`              _     _         _     _ _     `,
	`__      _____| |__ | |_ _   _(_) __| | |__  `,
	`\ \ /\ / / _ \ '_ \| __| | | | |/ _` + "`" + ` | '_ \ `,
	` \ V  V /  __/ |_) | |_| |_| | | (_| | |_) |`,
	`  \_/\_/ \___|_.__/ \__|\__,_|_|\__,_|_.__/ `,
}

const logoW = 44

// frame draws content inside a rounded border with the title set into the top edge.
// The result is exactly w cells wide and h lines tall; content is clipped or padded to fit.
// A focused frame uses the accent color so the active pane is always obvious.
func frame(th Theme, title, hint, content string, w, h int, focused bool) string {
	edge := th.Border
	ttl := th.Dim.Bold(true)
	if focused {
		edge = th.Accent
		ttl = th.Accent.Bold(true)
	}
	return frameEdge(th, edge, ttl, title, hint, content, w, h)
}

// frameCue is frame with the border in the colour cue ("" = the theme's); the title still shows the focus.
func frameCue(th Theme, cue, title, hint, content string, w, h int, focused bool) string {
	if cue == "" {
		return frame(th, title, hint, content, w, h, focused)
	}
	ttl := th.Dim.Bold(true)
	if focused {
		ttl = th.Accent.Bold(true)
	}
	return frameEdge(th, lipgloss.NewStyle().Foreground(lipgloss.Color(cue)), ttl, title, hint, content, w, h)
}

// frameEdge is frame with explicit edge and title styles (dialogs use the accent or, for
// destructive ones, the error color).
func frameEdge(th Theme, edge, ttl lipgloss.Style, title, hint, content string, w, h int) string {
	if w < 4 || h < 2 {
		return strings.Join(padLines(nil, w, h), "\n")
	}
	inner := w - 2
	top := "╭─"
	rest := inner - 1
	if title != "" {
		t := " " + title + " "
		if width(t) > rest-1 {
			t = fit(t, rest-1)
		}
		top = edge.Render("╭─") + ttl.Render(t) + edge.Render(strings.Repeat("─", max(0, rest-width(t)))+"╮")
	} else {
		top = edge.Render("╭" + strings.Repeat("─", inner) + "╮")
	}
	bottom := edge.Render("╰" + strings.Repeat("─", inner) + "╯")
	if hint != "" && width(hint) <= inner-2 {
		bottom = edge.Render("╰"+strings.Repeat("─", inner-width(hint)-2)+" ") + hint + edge.Render(" ╯")
	}
	body := padLines(strings.Split(content, "\n"), inner-2, h-2) // one cell of padding each side
	out := make([]string, 0, h)
	out = append(out, top)
	for _, l := range body {
		out = append(out, edge.Render("│")+" "+l+" "+edge.Render("│"))
	}
	out = append(out, bottom)
	return strings.Join(out, "\n")
}

// padLines clips or pads lines to exactly h lines of w cells.
func padLines(lines []string, w, h int) []string {
	out := make([]string, h)
	for i := range out {
		if i < len(lines) {
			out[i] = fit(lines[i], w)
		} else {
			out[i] = strings.Repeat(" ", max(0, w))
		}
	}
	return out
}

// hintChips turns a hint such as "Enter: open   Esc: cancel   ↑↓: move" into chips; text that
// is not key: label pairs is left dimmed, so every dialog's footer reads the same way.
func (t Theme) hintChips(hint string) string {
	var parts []string
	for _, piece := range strings.Split(hint, "   ") {
		piece = strings.TrimSpace(piece)
		if piece == "" {
			continue
		}
		if k, label, ok := strings.Cut(piece, ": "); ok && !strings.Contains(k, " ") || ok && len(k) <= 12 {
			parts = append(parts, t.chip(k)+t.Hint.Render(" "+label))
		} else {
			parts = append(parts, t.Hint.Render(piece))
		}
	}
	return strings.Join(parts, "  ")
}

// chip renders a key as a small badge, used in bars and hints: [C-d s].
func (t Theme) chip(key string) string {
	return lipgloss.NewStyle().Background(lipgloss.Color(t.pal["header_bg"])).Foreground(lipgloss.Color(t.pal["accent"])).Bold(true).Render(" " + key + " ")
}

// chips renders key/label pairs as badges with labels: [↵] open  [/] find.
func (t Theme) chips(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, t.chip(pairs[i])+t.Hint.Render(" "+pairs[i+1]))
	}
	return strings.Join(parts, "  ")
}

// segmented renders a tab-like control: the active item highlighted, others dim, with keys.
func (t Theme) segmented(items []string, keys []string, active int) string {
	var sb strings.Builder
	for i, it := range items {
		label := " " + it + " "
		if i < len(keys) && keys[i] != "" {
			label = " " + it + t.Hint.Render(" "+keys[i]) + " "
		}
		if i == active {
			sb.WriteString(t.TabActive.Render(" " + it + " "))
		} else {
			sb.WriteString(t.TabIdle.Render(label))
		}
		sb.WriteString(" ")
	}
	return strings.TrimRight(sb.String(), " ")
}
