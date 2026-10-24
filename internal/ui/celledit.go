package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

// cellEditDlg edits one cell in a multi-line popover: JSON documents and long text do not fit an
// inline field. Ctrl+s saves, Esc cancels. JSON opens pretty-printed; saving unchanged text is a no-op.
type cellEditDlg struct {
	title  string
	ed     *Editor
	start  string
	onSave func(text string) tea.Cmd
}

func newCellEdit(column string, v any, onSave func(string) tea.Cmd) *cellEditDlg {
	text := ""
	if !isSpecial(v) {
		text = db.FormatValue(v)
	}
	dialect := "text"
	if t := strings.TrimSpace(text); len(t) > 1 && (t[0] == '{' || t[0] == '[') && json.Valid([]byte(t)) {
		text, dialect = prettyValue(text), "mongo" // the mongo lexer colours JSON keys, strings and numbers
	}
	ed := NewEditor(dialect, EditorOpts{TabWidth: 2, CloseBraces: true, Autocomplete: false})
	ed.SetText(text)
	return &cellEditDlg{title: "Edit " + column, ed: ed, start: text, onSave: onSave}
}

func (d *cellEditDlg) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.Keystroke() {
		case "esc":
			return nil, nil
		case "ctrl+s":
			text := d.ed.Text()
			if text == d.start {
				return nil, nil
			}
			return nil, d.onSave(text)
		}
	}
	d.ed.Handle(msg)
	return d, nil
}

func (d *cellEditDlg) View(th Theme, maxW, maxH int) string {
	w := min(maxW-4, 110)
	d.ed.W, d.ed.H = w-4, max(5, maxH-9)
	return box(th, d.title, strings.Split(d.ed.View(th, true), "\n"), "C-s: save   Esc: cancel   Enter: new line   (e in the grid edits inline)", w)
}

// editCellInPopover opens the popover for the grid's cursor cell.
func editCellInPopover(ws *Workspace, g *Grid) tea.Cmd {
	v, ok := g.CursorValue()
	if !ok || g.col >= len(g.Cols) {
		return nil
	}
	ws.a.push(newCellEdit(g.Cols[g.col].Name, v, func(text string) tea.Cmd {
		g.SetCell(text)
		return nil
	}))
	return nil
}
