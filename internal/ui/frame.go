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
// A focused frame uses the accent color so the active pane is always obvious; only the focused
// frame carries its key hints, so the screen shows one set of hints at a time.
func frame(th Theme, title, hint, content string, w, h int, focused bool) string {
	edge := th.Border
	ttl := th.Dim.Bold(true)
	if focused {
		edge = th.Accent
		ttl = th.Accent.Bold(true)
	} else {
		hint = ""
	}
	return frameEdge(th, edge, ttl, title, hint, content, w, h)
}

// frameCue is frame with the border in the colour cue ("" = the theme's). The focused frame takes the
// full cue colour; the others take the cue faded toward the theme's border colour, so a production
// workspace stays tinted everywhere and the focused pane still stands out.
func frameCue(th Theme, cue, title, hint, content string, w, h int, focused bool) string {
	if cue == "" {
		return frame(th, title, hint, content, w, h, focused)
	}
	ttl := th.Dim.Bold(true)
	col := cue
	if focused {
		ttl = th.Accent.Bold(true)
	} else {
		hint = ""
		col = blend(cue, th.pal["border"], 0.55)
	}
	return frameEdge(th, lipgloss.NewStyle().Foreground(lipgloss.Color(col)), ttl, title, hint, content, w, h)
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

// chip renders a key as a small badge, used in bars and hints: [C-d s]. The key is written in one
// notation everywhere (keyGlyph), whichever way the caller spelled it.
func (t Theme) chip(key string) string {
	return lipgloss.NewStyle().Background(lipgloss.Color(t.pal["header_bg"])).Foreground(lipgloss.Color(t.pal["accent"])).Bold(true).Render(" " + keyGlyph(key) + " ")
}

// keyGlyph spells a key the way every hint in the terminal UI does: tmux-style modifiers (C-, M-, S-)
// and ↵ for Enter, so "Alt+Enter", "M-Enter" and "M-↵" all read "M-↵". The help screen keeps the
// config spelling ("C-d Enter"), which is what [keys] takes.
func keyGlyph(key string) string {
	parts := strings.Split(key, " ")
	for i, p := range parts {
		for _, m := range [][2]string{{"Ctrl+", "C-"}, {"ctrl+", "C-"}, {"Alt+", "M-"}, {"alt+", "M-"}, {"Shift+", "S-"}, {"shift+", "S-"}} {
			for strings.Contains(p, m[0]) && len(p) > len(m[0]) {
				p = strings.Replace(p, m[0], m[1], 1)
			}
		}
		if strings.HasSuffix(p, "Enter") || strings.HasSuffix(p, "enter") {
			p = p[:len(p)-5] + "↵"
		}
		parts[i] = p
	}
	return strings.Join(parts, " ")
}

// chips renders key/label pairs as badges with labels: [↵] open  [/] find.
func (t Theme) chips(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i] == "" { // an action left without a key (boundKey) gets no hint
			continue
		}
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
