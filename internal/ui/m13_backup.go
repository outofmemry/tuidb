package ui

import (
	"context"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/backup"
	"webtuidb/internal/db"
	"webtuidb/internal/export"
	"webtuidb/internal/store"
)

// M13.4 backup / restore dialog: extra options, common-option picker, file-name pattern, live output, cancel, reveal.

// backupDefaultFile is the File field's default: the [export] filename pattern for a backup, <db>.sql for a restore.
func (w *Workspace) backupDefaultFile(dbName string, restore bool) string {
	if restore {
		return dbName + ".sql"
	}
	return export.Filename(orDefault(w.a.d.Cfg.Export.Filename, "{table}-{date}"), w.exportVars(dbName)) + ".sql"
}

// toggleExtra adds the flags of the ticked options to text and removes those of the unticked ones.
func toggleExtra(text string, flags []string, on []bool) string {
	toks := strings.Fields(text)
	at := func(ft []string) int {
		for i := 0; i+len(ft) <= len(toks); i++ {
			if strings.Join(toks[i:i+len(ft)], " ") == strings.Join(ft, " ") {
				return i
			}
		}
		return -1
	}
	for i, fl := range flags {
		ft := strings.Fields(fl)
		switch idx := at(ft); {
		case on[i] && idx < 0:
			toks = append(toks, ft...)
		case !on[i] && idx >= 0:
			toks = append(toks[:idx:idx], toks[idx+len(ft):]...)
		}
	}
	return strings.Join(toks, " ")
}

// commonOptions opens the checklist of the tool's usual flags and writes the result into the form's Extra field.
func (w *Workspace) commonOptions(f *formDlg, restore bool) tea.Cmd {
	opts := backup.CommonOptions(w.conn.Driver, restore)
	if len(opts) == 0 {
		return w.a.say("no common options for %s", w.conn.Driver)
	}
	var extra *formField
	for _, fl := range f.fields {
		if fl.Key == "extra" {
			extra = fl
		}
	}
	if extra == nil {
		return nil
	}
	cur := strings.Fields(extra.edit.Value())
	has := func(flag string) bool {
		ft := strings.Fields(flag)
		for i := 0; i+len(ft) <= len(cur); i++ {
			if strings.Join(cur[i:i+len(ft)], " ") == strings.Join(ft, " ") {
				return true
			}
		}
		return false
	}
	var items []checkItem
	var flags []string
	for _, o := range opts {
		items = append(items, checkItem{Label: o.Flag + "  " + o.Desc, Checked: has(o.Flag)})
		flags = append(flags, o.Flag)
	}
	w.a.push(&checklistDlg{title: "Common options", items: items, onOK: func(items []checkItem) tea.Cmd {
		on := make([]bool, len(items))
		for i, it := range items {
			on[i] = it.Checked
		}
		extra.edit = newLine(toggleExtra(extra.edit.Value(), flags, on))
		return nil
	}})
	return nil
}

// backupRunDlg shows the tool's output as it arrives. It stays until the run is over; Esc / Ctrl+c while it runs
// asks to cancel.
type backupRunDlg struct {
	w       *Workspace
	title   string
	lines   []string
	back    int // lines scrolled up from the newest
	running bool
	ok      bool
	reveal  string // the file to show in the file manager when the backup succeeded
}

const backupKeep = 2000

func (d *backupRunDlg) add(s string) {
	d.lines = append(d.lines, cleanReply(s)) // tool output and errors can echo object names carrying ANSI / OSC sequences
	if len(d.lines) > backupKeep {
		d.lines = d.lines[len(d.lines)-backupKeep:]
	}
}

func (d *backupRunDlg) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}
	switch k.Keystroke() {
	case "esc", "ctrl+c", "q":
		if !d.running {
			return nil, nil
		}
		if k.Keystroke() == "q" {
			return d, nil
		}
		d.w.a.push(&confirmDlg{title: "Cancel the " + strings.ToLower(strings.Fields(d.title)[0]) + "?", lines: []string{"The tool is stopped; a partial file may be left behind."}, yes: "Cancel it", danger: true,
			onYes: func() tea.Cmd {
				if d.w.jobCancel != nil {
					d.w.jobCancel()
				}
				return nil
			}})
	case "r":
		if !d.running && d.ok && d.reveal != "" {
			if err := store.Reveal(d.reveal); err != nil {
				return d, d.w.a.fail(err)
			}
		}
	case "up", "k":
		d.back++
	case "down", "j":
		d.back = max(0, d.back-1)
	case "pgup":
		d.back += 10
	case "pgdown", "space":
		d.back = max(0, d.back-10)
	case "home", "g":
		d.back = 1 << 30
	case "end", "G":
		d.back = 0
	}
	return d, nil
}

func (d *backupRunDlg) View(th Theme, maxW, maxH int) string {
	w, vis := min(maxW-4, 110), max(4, maxH-8)
	var all []string
	for _, l := range d.lines {
		all = append(all, wrapText(l, w-4)...)
	}
	d.back = clamp(d.back, 0, max(0, len(all)-vis))
	end := len(all) - d.back
	body := all[max(0, end-vis):end]
	hint := "↑↓ PgUp/PgDn: scroll"
	switch {
	case d.running:
		hint = "Esc / Ctrl+c: cancel   " + hint
	case d.ok && d.reveal != "":
		hint = "r: reveal   Esc: close   " + hint
	default:
		hint = "Esc: close   " + hint
	}
	return box(th, d.title, body, hint, w)
}

// runBackup runs cmd as the workspace job and streams its output into a backupRunDlg.
func (w *Workspace) runBackup(cmd *exec.Cmd, verb, name, out string, restore bool, onFail func() tea.Cmd) tea.Cmd {
	d := &backupRunDlg{w: w, title: verb + " " + name, running: true, lines: []string{cleanReply("$ " + backup.Display(cmd)), ""}}
	if !restore {
		d.reveal = out
	}
	w.a.push(d)
	add := func(s string) func() tea.Cmd {
		return func() tea.Cmd { d.add(s); return nil }
	}
	finish := func(ok bool, line string) func() tea.Cmd {
		return func() tea.Cmd { d.add(line); d.running, d.ok = false, ok; return nil }
	}
	return w.job(strings.ToLower(verb)+" "+name, func(ctx context.Context, _ db.Session, report func(func() tea.Cmd)) error {
		err := backup.Run(ctx, cmd, func(line string) { report(add(line)) })
		switch {
		case ctx.Err() != nil:
			report(finish(false, "-- cancelled"))
		case err != nil:
			report(finish(false, "✗ "+err.Error()))
			if onFail != nil {
				report(onFail)
			}
		default:
			report(finish(true, "✓ done"))
		}
		return err
	}, func() (string, tea.Cmd) { return verb + " finished: " + name, nil })
}
