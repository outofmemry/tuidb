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
	// next, when set, makes Tab / Shift+Tab save a one-line value and move on to the neighbouring
	// cell's editor (a multi-line value keeps Tab as indentation).
	next func(dir int) tea.Cmd
}

// EditKey runs one key of the inline cell editor: Enter saves, Esc cancels, Tab / Shift+Tab save and
// carry on editing the next / previous cell.
func (g *Grid) EditKey(k tea.KeyPressMsg) {
	switch k.Keystroke() {
	case "enter":
		g.FinishEdit(true)
	case "tab":
		g.TabEdit(1)
	case "shift+tab":
		g.TabEdit(-1)
	case "esc":
		g.FinishEdit(false)
	default:
		g.edit.Handle(k)
	}
}

// TabEdit saves the open editor and opens the one on the next (dir 1) or previous (-1) cell, wrapping
// to the neighbouring row at the ends. It reports whether an editor is open afterwards.
func (g *Grid) TabEdit(dir int) bool {
	g.FinishEdit(true)
	c, r := g.col+dir, g.row
	switch {
	case c >= len(g.Cols):
		c, r = 0, r+1
	case c < 0:
		c, r = len(g.Cols)-1, r-1
	}
	if r < 0 || r >= len(g.Rows) {
		return false
	}
	g.setSelecting(false)
	g.row, g.col = r, c
	g.scroll()
	return g.StartEdit()
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
		case "tab", "shift+tab":
			if d.next == nil || strings.Contains(d.ed.Text(), "\n") {
				break
			}
			if text := d.ed.Text(); text != d.start {
				d.onSave(text)
			}
			dir := 1
			if k.Keystroke() == "shift+tab" {
				dir = -1
			}
			return nil, d.next(dir)
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
