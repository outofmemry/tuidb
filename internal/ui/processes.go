// processes.go: The process list: loading it, sorting, killing, copying and exporting.

package ui

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/menus"
)

var procColumns = []string{"elapsed", "id", "user", "db", "state"}

// sortProcesses orders by procColumns[by]; elapsed is slowest first.
func sortProcesses(ps []db.ProcessInfo, by int) {
	sort.SliceStable(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]
		switch procColumns[by] {
		case "id":
			return a.ID < b.ID
		case "user":
			return a.User < b.User
		case "db":
			return a.DB < b.DB
		case "state":
			return a.State < b.State
		}
		return a.Elapsed > b.Elapsed
	})
}

// processText renders the rows, header first, joined by sep.
func processText(ps []db.ProcessInfo, sep string) string {
	lines := []string{strings.Join([]string{"id", "user", "db", "state", "elapsed", "query"}, sep)}
	for _, x := range ps {
		lines = append(lines, strings.Join([]string{x.ID, x.User, x.DB, x.State, x.Elapsed.String(), strings.Join(strings.Fields(x.Query), " ")}, sep))
	}
	return strings.Join(lines, "\n")
}

func (w *Workspace) exportProcesses(ps []db.ProcessInfo) tea.Cmd {
	w.a.push(newForm("Export the process list", []formField{{Key: "path", Label: "File (CSV)", Kind: fText, Value: "processes.csv"}}, func(v map[string]string) tea.Cmd {
		path := strings.TrimSpace(v["path"])
		return w.a.ifFree(func() tea.Cmd {
			err := writeAtomic(path, func(f io.Writer) error {
				cw := csv.NewWriter(f)
				cw.Write([]string{"id", "user", "db", "state", "elapsed", "query"})
				for _, x := range ps {
					cw.Write([]string{x.ID, x.User, x.DB, x.State, x.Elapsed.String(), x.Query})
				}
				cw.Flush()
				return cw.Error()
			})
			if err != nil {
				return w.a.fail(err)
			}
			return w.a.say("saved %s to %s", plural(len(ps), "process"), path)
		}, path)
	}))
	return nil
}

// sideConn is the connection the process list opens: the workspace's own, with the password it already resolved.
func (w *Workspace) sideConn() config.Connection {
	c := w.conn
	c.Password, c.PasswordCmd = w.o.Password, ""
	c.Database = w.sess.CurrentDatabase()
	return c
}

func (w *Workspace) closeSide() {
	if w.side != nil {
		w.side.Close()
		w.side = nil
	}
}

func (w *Workspace) processList() tea.Cmd {
	if _, ok := w.sess.(db.ProcessLister); !w.caps.ProcessList || !ok {
		return w.a.say("the process list is not supported by %s", w.conn.Driver)
	}
	p := &pickerDlg{title: "Processes", empty: "loading…", hint: "Enter: show query   Space: mark   k: kill   s: sort   c: copy as   e: export   m: menu   r: refresh   Esc: dismiss"}
	var procs []db.ProcessInfo
	sortBy := 0                 // index into procColumns; the slowest first for Elapsed
	rev := false                // the opposite of the column's natural order
	marked := map[string]bool{} // process ids ticked with Space
	show := func() {
		sortProcesses(procs, sortBy)
		if rev {
			for i, j := 0, len(procs)-1; i < j; i, j = i+1, j-1 {
				procs[i], procs[j] = procs[j], procs[i]
			}
		}
		arrow := map[bool]string{false: "↓", true: "↑"}[rev != (procColumns[sortBy] != "elapsed")]
		p.title = "Processes · sorted by " + procColumns[sortBy] + " " + arrow
		if n := len(marked); n > 0 {
			p.title += fmt.Sprintf(" · %d marked", n)
		}
		p.items = nil
		for _, x := range procs {
			mark := "  "
			if marked[x.ID] {
				mark = "✓ "
			}
			p.items = append(p.items, pickItem{Label: mark + fmt.Sprintf("%-6s %-10s %-10s %-8s %s", x.ID, x.User, x.DB, x.State, x.Elapsed.Round(time.Millisecond)),
				Detail: oneLine(strings.Join(strings.Fields(x.Query), " ")), Value: x})
		}
	}
	// withLister runs f on the process-list session (opened on first use, kept until the workspace closes), so
	// the list works while the workspace's own connection is busy with a query.
	withLister := func(f func(ctx context.Context, pl db.ProcessLister) func() tea.Cmd) tea.Cmd {
		side, connect, c := w.side, w.a.d.Connect, w.sideConn()
		return liveCmd(func() func() tea.Cmd {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			fail := func(err error) func() tea.Cmd {
				return func() tea.Cmd { p.empty = err.Error(); return w.a.fail(err) }
			}
			fresh := false
			if side == nil {
				o, err := connect(ctx, c)
				if err != nil {
					return fail(err)
				}
				side, fresh = o, true
			}
			pl, ok := side.Sess.(db.ProcessLister)
			if !ok {
				return fail(fmt.Errorf("the process list is not supported by %s", w.conn.Driver))
			}
			done := f(ctx, pl)
			return func() tea.Cmd {
				if fresh {
					if w.side == nil && !w.closed {
						w.side = side
					} else {
						side.Close()
					}
				}
				return done()
			}
		})
	}
	var refresh func() tea.Cmd
	refresh = func() tea.Cmd {
		return withLister(func(ctx context.Context, pl db.ProcessLister) func() tea.Cmd {
			ps, err := pl.Processes(ctx)
			return func() tea.Cmd {
				p.empty = "no processes"
				if err != nil {
					p.empty = err.Error()
				}
				procs = ps
				show()
				return nil
			}
		})
	}
	// kill ends the marked processes, or the one under the cursor when none is marked
	kill := func(it pickItem, ok bool) tea.Cmd {
		var victims []db.ProcessInfo
		for _, x := range procs {
			if marked[x.ID] {
				victims = append(victims, x)
			}
		}
		if len(victims) == 0 {
			if !ok {
				return nil
			}
			victims = []db.ProcessInfo{it.Value.(db.ProcessInfo)}
		}
		var stmts []db.Statement
		var lines []string
		for _, x := range victims {
			stmts = append(stmts, db.Statement{Text: "KILL " + x.ID})
			lines = append(lines, x.ID+"  "+x.User+"@"+x.DB+"  "+oneLine(x.Query))
		}
		do := func() tea.Cmd {
			return w.gate(stmts, func() tea.Cmd {
				return withLister(func(ctx context.Context, pl db.ProcessLister) func() tea.Cmd {
					var failed []string
					for _, x := range victims {
						err := pl.Kill(ctx, x.ID)
						w.log("KILL "+x.ID, 0, err)
						if err != nil {
							failed = append(failed, x.ID+": "+err.Error())
						}
					}
					return func() tea.Cmd {
						for _, x := range victims {
							delete(marked, x.ID)
						}
						if len(failed) > 0 {
							return tea.Batch(w.a.fail(fmt.Errorf("%s", strings.Join(failed, "\n"))), refresh())
						}
						return tea.Batch(w.a.say("killed %s", plural(len(victims), "process")), refresh())
					}
				})
			})
		}
		title := "Kill process " + victims[0].ID + "?"
		if len(victims) > 1 {
			title = fmt.Sprintf("Kill %d processes?", len(victims))
		}
		w.a.push(&confirmDlg{title: title, lines: lines, yes: "Kill", danger: true, onYes: do})
		return nil
	}
	// scope is what copy and export act on: the marked rows, else every row
	scope := func() []db.ProcessInfo {
		var m []db.ProcessInfo
		for _, x := range procs {
			if marked[x.ID] {
				m = append(m, x)
			}
		}
		if len(m) == 0 {
			return procs
		}
		return m
	}
	sortMenu := func(pickItem, bool) tea.Cmd {
		var items []pickItem
		for i, c := range procColumns {
			items = append(items, pickItem{Label: c, Value: i})
		}
		w.a.push(&pickerDlg{title: "Sort processes by (again: reverse)", items: items, sel: sortBy, onPick: func(it pickItem, _ bool) tea.Cmd {
			if i := it.Value.(int); i == sortBy {
				rev = !rev
			} else {
				sortBy, rev = i, false
			}
			show()
			return nil
		}})
		return nil
	}
	copyAs := func(pickItem, bool) tea.Cmd { return w.copyProcessesAs(scope()) }
	quickLook := func(it pickItem, ok bool) tea.Cmd {
		if !ok {
			return nil
		}
		x := it.Value.(db.ProcessInfo)
		w.a.push(w.a.newQuickLookValue("Process "+x.ID, x.Query, nil))
		return nil
	}
	p.keys = map[string]func(pickItem, bool) tea.Cmd{
		"r": func(pickItem, bool) tea.Cmd { return refresh() },
		"s": sortMenu,
		"space": func(it pickItem, ok bool) tea.Cmd {
			if !ok {
				return nil
			}
			id := it.Value.(db.ProcessInfo).ID
			if marked[id] {
				delete(marked, id)
			} else {
				marked[id] = true
			}
			sel := p.sel
			show()
			p.sel = min(sel+1, max(0, len(p.items)-1))
			return nil
		},
		"c":      copyAs,
		"e":      func(pickItem, bool) tea.Cmd { return w.exportProcesses(scope()) },
		"k":      kill,
		"delete": kill,
		"m": func(it pickItem, ok bool) tea.Cmd {
			// the labels and their order come from menus.ProcessList, shared with the web UI
			w.a.pushMenu("Process menu", menus.ProcessList, menus.Env{}, func(m menus.Item) tea.Cmd {
				switch m.Action {
				case "kill":
					return kill(it, ok)
				case "quick_look":
					return quickLook(it, ok)
				case "copy_rows_as":
					return copyAs(it, ok)
				case "export":
					return w.exportProcesses(scope())
				}
				return nil
			})
			return nil
		},
	}
	p.onPick = func(it pickItem, _ bool) tea.Cmd { return quickLook(it, true) }
	w.a.push(dismissOnQ{p}) // Esc and q both dismiss the list
	return refresh()
}

// dismissOnQ lets q close a picker like Esc does (Dismiss in the process menu).
type dismissOnQ struct{ *pickerDlg }

func (d dismissOnQ) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok && k.Keystroke() == "q" {
		return d.pickerDlg.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	}
	o, cmd := d.pickerDlg.Update(msg)
	if o == Overlay(d.pickerDlg) {
		return d, cmd
	}
	return o, cmd
}

var procHeader = []string{"id", "user", "db", "state", "elapsed", "query"}

func procFields(x db.ProcessInfo) []string {
	return []string{x.ID, x.User, x.DB, x.State, x.Elapsed.String(), strings.Join(strings.Fields(x.Query), " ")}
}

// processAs renders the rows as CSV (header first), JSON (array of objects) or a Markdown table.
func processAs(ps []db.ProcessInfo, f CopyFormat) string {
	switch f {
	case FmtJSON:
		out := make([]map[string]string, len(ps))
		for i, x := range ps {
			out[i] = map[string]string{}
			for j, v := range procFields(x) {
				out[i][procHeader[j]] = v
			}
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return string(b)
	case FmtMarkdown:
		lines := []string{"| " + strings.Join(procHeader, " | ") + " |", "|" + strings.Repeat(" --- |", len(procHeader))}
		for _, x := range ps {
			cells := procFields(x)
			for i := range cells {
				cells[i] = strings.ReplaceAll(cells[i], "|", `\|`)
			}
			lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
		}
		return strings.Join(lines, "\n")
	}
	var sb strings.Builder
	cw := csv.NewWriter(&sb)
	cw.Write(procHeader)
	for _, x := range ps {
		cw.Write(procFields(x))
	}
	cw.Flush()
	return strings.TrimRight(sb.String(), "\n")
}

func (w *Workspace) copyProcessesAs(ps []db.ProcessInfo) tea.Cmd {
	if len(ps) == 0 {
		return w.a.say("no processes to copy")
	}
	w.a.pushCopyAs("Copy processes as", []CopyFormat{FmtCSVHeader, FmtJSON, FmtMarkdown}, func(f CopyFormat) tea.Cmd {
		return w.a.copyText(processAs(ps, f), plural(len(ps), "process"))
	})
	return nil
}
