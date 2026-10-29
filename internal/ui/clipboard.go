// clipboard.go: Clipboard: setting it, the copied toast, and the copy-as formats.

package ui

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/clip"
)

// clipMsg reports how setClip copied: via "osc52" cannot be confirmed, err means nothing could copy (T-P2-12).
type clipMsg struct {
	via string
	err error
}

// setClip copies text with OSC 52 through internal/clip over SSH (the user's machine), else with the system
// clipboard tool, else with Bubble Tea's OSC 52; the clipMsg it ends with corrects the caller's "copied" toast.
func setClip(text string) tea.Cmd {
	return func() tea.Msg {
		if clip.Available() && clip.Write(text) == nil {
			return nil
		}
		if clip.System(text) == nil {
			return nil
		}
		if !clip.TerminalMayCopy() {
			return clipMsg{err: errors.New("Could not copy (no clipboard available)")}
		}
		return tea.BatchMsg{tea.SetClipboard(text), func() tea.Msg { return clipMsg{via: "osc52"} }}
	}
}

// clipDone turns a failed copy into ✗ and marks a copy that only went to the terminal as such.
func (a *App) clipDone(m clipMsg) tea.Cmd {
	if m.err != nil {
		return a.fail(m.err)
	}
	if via := " (via terminal, OSC 52)"; !a.toastErr && strings.Contains(a.toast, "copied") && !strings.HasSuffix(a.toast, via) {
		a.toast += via
	}
	return nil
}

func (a *App) copyText(text, what string) tea.Cmd {
	return tea.Batch(setClip(text), a.say("copied %s", what))
}

// pushCopyAs asks for one of the formats, then runs do with it.
func (a *App) pushCopyAs(title string, formats []CopyFormat, do func(CopyFormat) tea.Cmd) {
	var items []pickItem
	for _, f := range formats {
		items = append(items, pickItem{Label: string(f), Value: f})
	}
	a.push(&pickerDlg{title: title, items: items, onPick: func(it pickItem, _ bool) tea.Cmd { return do(it.Value.(CopyFormat)) }})
}

// structFormats are the copy-as formats that make sense for column / index definitions.
var structFormats = []CopyFormat{FmtCSVHeader, FmtJSON, FmtMarkdown, FmtPlain, FmtHTML}

// structCopyAs copies the column (or index) definitions as CSV / JSON / Markdown...; selected rows, else all.
func (t *tableTab) structCopyAs() tea.Cmd {
	g := t.editGrid()
	if g == nil || len(g.Rows) == 0 {
		return t.ws.a.say("the structure is still loading")
	}
	what := map[bool]string{true: "index definitions", false: "column definitions"}[t.view == viewIndex]
	t.ws.a.pushCopyAs("Copy "+what+" as", structFormats, func(f CopyFormat) tea.Cmd {
		r0, r1 := 0, len(g.Rows)-1
		if g.selecting {
			r0, _, r1, _ = g.selection()
		}
		return t.copy(g.FormatRect(r0, 0, r1, len(g.Cols)-1, f, t.ref.Name), what)
	})
	return nil
}
