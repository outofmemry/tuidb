// jobs.go: The jobs centre: background jobs, their status and the jobs dialog.

package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/menus"
)

type jobEntry struct {
	id       int
	kind     string // import | export | backup | restore | search | diff
	title    string
	started  time.Time
	ended    time.Time
	done     bool
	err      error
	stopped  bool
	progress float64 // 0-1, -1 when unknown (the jobs report text, not fractions)
	note     string  // the last status line the job reported
	file     string  // what it wrote ("" = nothing to reveal)
	cancel   context.CancelFunc
}

// jobKind is the kind a job label stands for ("export a.csv" -> export).
func jobKind(label string) string {
	switch f := strings.Fields(label); {
	case len(f) == 0:
		return "export"
	case f[0] == "schema" || f[0] == "data":
		return "diff"
	default:
		return f[0]
	}
}

// addJob registers a job that is starting; it takes the file noted in w.jobFile.
func (w *Workspace) addJob(label string, cancel context.CancelFunc) *jobEntry {
	w.nextJob++
	e := &jobEntry{id: w.nextJob, kind: jobKind(label), title: label, started: time.Now(), progress: -1, cancel: cancel}
	if w.jobFile != "" {
		e.file, _ = filepath.Abs(w.jobFile)
		w.jobFile = ""
	}
	w.jobs = append(w.jobs, e)
	return e
}

// noting wraps a progress callback so the last status text the job shows is kept on its entry.
func (e *jobEntry) noting(w *Workspace, f func() tea.Cmd) func() tea.Cmd {
	return func() tea.Cmd {
		seq := w.a.toastSeq
		cmd := f()
		if w.a.toastSeq != seq {
			e.note = w.a.toast
		}
		return cmd
	}
}

// finish marks the job over and, when the terminal is unfocused, announces it.
func (e *jobEntry) finish(w *Workspace, err error, stopped bool) tea.Cmd {
	e.done, e.err, e.stopped, e.ended, e.cancel = true, err, stopped, time.Now(), nil
	if err == nil && !stopped {
		e.progress = 1
	}
	word := "finished"
	switch {
	case stopped:
		word = "stopped"
	case err != nil:
		word = "failed"
	}
	return w.a.finishSignal(fmt.Sprintf("%s %s: %s", w.conn.Name, e.title, word))
}

// stop cancels a running job.
func (e *jobEntry) stop() bool {
	if e.done || e.cancel == nil {
		return false
	}
	e.cancel()
	return true
}

func (e *jobEntry) status(spin string) string {
	switch {
	case !e.done:
		s := spin + " running " + e.elapsed()
		if e.note != "" {
			s += "  " + cleanReply(e.note)
		}
		return s
	case e.stopped:
		return "■ stopped"
	case e.err != nil:
		return "✗ " + strings.ReplaceAll(cleanReply(e.err.Error()), "\n", " ")
	}
	return "✓ done in " + e.elapsed()
}

func (e *jobEntry) elapsed() string {
	end := e.ended
	if !e.done {
		end = time.Now()
	}
	return end.Sub(e.started).Round(100 * time.Millisecond).String()
}

// finishSignal rings the terminal bell and sends OSC 9 (iTerm2, Windows Terminal...) and OSC 777 (VTE, Konsole...)
// notifications, but only while the terminal is not focused. Terminals that know neither ignore them.
func (a *App) finishSignal(msg string) tea.Cmd {
	if !a.term.blurred {
		return nil
	}
	msg = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, msg)
	return tea.Raw("\a\x1b]9;" + msg + "\x07\x1b]777;notify;webtuidb;" + msg + "\x07")
}

// jobsDlg lists the workspace's jobs. Enter opens the row menu (menus.Jobs); s stops, o opens the folder, d dismisses.
type jobsDlg struct {
	w   *Workspace
	sel int
}

func (w *Workspace) jobsAction() tea.Cmd {
	w.a.push(&jobsDlg{w: w})
	return nil
}

func (d *jobsDlg) cur() *jobEntry {
	if d.sel >= 0 && d.sel < len(d.w.jobs) {
		return d.w.jobs[d.sel]
	}
	return nil
}

// jobAction is one entry of menus.Jobs applied to e.
func (w *Workspace) jobAction(act string, e *jobEntry) tea.Cmd {
	switch act {
	case "cancel":
		if !e.stop() {
			return w.a.say("that job is not running")
		}
		return w.a.say("stopping %s…", e.title)
	case "reveal":
		if e.file == "" {
			return w.a.say("%s wrote no file", e.title)
		}
		return w.reveal(e.file)
	case "close":
		if !e.done {
			return w.a.say("stop the job before dismissing it")
		}
		for i, j := range w.jobs {
			if j == e {
				w.jobs = append(w.jobs[:i], w.jobs[i+1:]...)
				break
			}
		}
	}
	return nil
}

func (d *jobsDlg) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}
	e := d.cur()
	switch k.Keystroke() {
	case "esc", "q", "alt+J":
		return nil, nil
	case "down", "j", "ctrl+n":
		d.sel = min(d.sel+1, len(d.w.jobs)-1)
	case "up", "k", "ctrl+p":
		d.sel = max(d.sel-1, 0)
	case "s", "x":
		if e != nil {
			return d, d.w.jobAction("cancel", e)
		}
	case "o":
		if e != nil {
			return d, d.w.jobAction("reveal", e)
		}
	case "d", "delete":
		if e != nil {
			cmd := d.w.jobAction("close", e)
			d.sel = clamp(d.sel, 0, max(0, len(d.w.jobs)-1))
			return d, cmd
		}
	case "enter", "m":
		if e != nil {
			d.w.a.pushMenu(e.title, menus.Jobs, menus.Env{}, func(it menus.Item) tea.Cmd {
				cmd := d.w.jobAction(it.Action, e)
				d.sel = clamp(d.sel, 0, max(0, len(d.w.jobs)-1))
				return cmd
			})
		}
	}
	return d, nil
}

func (d *jobsDlg) View(th Theme, maxW, maxH int) string {
	w := min(maxW-4, 100)
	cw := w - 4
	var body []string
	for i, e := range d.w.jobs {
		on := rowStyle(th, i == d.sel)
		left := on(th.Accent).Render(fit(e.kind, 8)) + on(th.Bold).Render(fit(e.title, max(10, cw/3)))
		stat := e.status(d.w.a.spinner())
		st := th.Dim
		switch {
		case e.err != nil:
			st = th.Err
		case e.done && !e.stopped:
			st = th.OK
		}
		body = append(body, listRow(th, i == d.sel, left+" ", on(st).Render(fit(stat, max(8, cw-width(left)-2))), cw))
		if e.file != "" && i == d.sel {
			body = append(body, th.Dim.Render("   "+e.file))
		}
	}
	if len(body) == 0 {
		body = []string{th.Dim.Render("  no jobs yet: imports, exports, backups, restores, searches and diffs show up here")}
	}
	return box(th, fmt.Sprintf("Background jobs · %s", d.w.conn.Name), body, "↑↓: move   s: stop   o: open folder   d: dismiss   Enter: menu   Esc: close", w)
}
