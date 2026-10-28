package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
)

// ago is the "last connected" cell: "3d ago", "" when the connection was never opened.
func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.Format("2006-01")
}

// pickPasswordCmd fills the form's Password cmd field with a secret-store command; <name> becomes the connection name.
func (w *welcome) pickPasswordCmd(f *formDlg, name string) tea.Cmd {
	name = orDefault(strings.TrimSpace(name), "connection")
	var items []pickItem
	for _, p := range config.PasswordCmdPresets() {
		items = append(items, pickItem{Label: p.Name, Detail: p.Note, Value: p})
	}
	w.a.push(&pickerDlg{title: "Password command", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		p := it.Value.(config.Preset)
		for _, fl := range f.fields {
			if fl.Key == "password_cmd" {
				fl.edit.Set(strings.ReplaceAll(p.Command, "<name>", name))
			}
		}
		return w.a.say("%s", p.Note)
	}})
	return nil
}
