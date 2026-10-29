// background.go: Background work: the pump that feeds results back into the Bubble Tea loop, jobs, and result targets.

package ui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

// liveMsg runs f on the UI loop. Background jobs report through it because wsMsg results wait
// while a dialog is open, and these jobs feed dialogs.
type liveMsg struct{ f func() tea.Cmd }

// pump turns a channel of UI-loop callbacks into messages, one at a time, until it is closed.
func pump(ch <-chan func() tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		f, ok := <-ch
		if !ok {
			return nil
		}
		return liveMsg{func() tea.Cmd { return tea.Batch(f(), pump(ch)) }}
	}
}

// liveCmd runs f off the UI loop; the callback it returns runs on it.
func liveCmd(f func() func() tea.Cmd) tea.Cmd {
	return func() tea.Msg { return liveMsg{f()} }
}

// liveCmd is the package liveCmd with the workspace's generation captured now: a result that
// arrives after the workspace closed or switched database/session is dropped, not shown.
func (w *Workspace) liveCmd(f func() func() tea.Cmd) tea.Cmd {
	gen := w.gen
	return liveCmd(func() func() tea.Cmd {
		cb := f()
		return func() tea.Cmd {
			if w.closed || w.gen != gen {
				return nil
			}
			return cb()
		}
	})
}

// job runs work in the background (one at a time per workspace; prefix . cancels it). work reports
// progress as UI-loop callbacks; then runs on success and returns the final message.
func (w *Workspace) job(label string, work func(ctx context.Context, sess db.Session, report func(func() tea.Cmd)) error, then func() (string, tea.Cmd)) tea.Cmd {
	if w.jobCancel != nil {
		return w.a.say("another export, import or search is still running (cancel it first)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.jobCancel = cancel
	je := w.addJob(label, cancel) // the jobs centre (alt+J) lists it until dismissed
	ch, sess, start := make(chan func() tea.Cmd, 16), w.sess, time.Now()
	go func() {
		defer close(ch)
		var err error
		func() { // a panic in work must still end the job, or the workspace stays busy for good
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("internal error: %v", r)
				}
			}()
			err = work(ctx, sess, func(f func() tea.Cmd) { ch <- je.noting(w, f) })
		}()
		stopped := err != nil && ctx.Err() != nil // decided when the work ended, not when the callback runs
		ch <- func() tea.Cmd {
			cancel()
			w.jobCancel = nil
			w.log("-- "+label, time.Since(start), err)
			switch {
			case stopped:
				return tea.Batch(w.a.say("%s: stopped", label), je.finish(w, nil, true))
			case err != nil:
				return tea.Batch(w.a.fail(fmt.Errorf("%s: %s", label, cleanReply(err.Error()))), je.finish(w, err, false)) // tool output may carry escapes (NEW-4)
			}
			msg, cmd := then()
			return tea.Batch(w.a.say("%s", msg), cmd, je.finish(w, nil, false))
		}
	}()
	return tea.Batch(w.a.say("%s…", label), pump(ch))
}

// targetRef is the table the sidebar or the open table tab points at.
func (w *Workspace) targetRef() (db.ObjRef, bool) {
	if w.focusSB {
		if r, ok := w.sb.current(); ok && r.kind == sbObject {
			switch r.obj.Kind {
			case db.KindTable, db.KindView, db.KindMatView:
				return r.obj.ObjRef, true
			}
		}
		return db.ObjRef{}, false
	}
	if t, ok := w.tab().(*tableTab); ok {
		return t.ref, true
	}
	return db.ObjRef{}, false
}

// gridResult is the loaded page of a grid as a Result (deleted rows left out).
func gridResult(g *Grid) *db.Result {
	res := &db.Result{HasRows: true}
	for _, c := range g.Cols {
		res.Columns = append(res.Columns, db.ResultColumn{Name: c.Name, Type: c.Type})
	}
	for _, r := range g.Rows {
		if r.State != rowDeleted {
			res.Rows = append(res.Rows, r.Cur)
		}
	}
	return res
}
