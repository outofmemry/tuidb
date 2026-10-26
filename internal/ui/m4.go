package ui

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
	"webtuidb/internal/export"
	"webtuidb/internal/store"
)

// ---- navigation: tab menu, rename, console (M4 item 2, 6) -------------------------------

// tabTitle is the name in the tab bar; a tab renamed with rename_tab keeps its name.
func (w *Workspace) tabTitle(t tabber) string {
	if n := w.names[t.ID()]; n != "" {
		return n
	}
	return t.Title()
}

func (w *Workspace) m4Action(name string) tea.Cmd {
	switch name {
	case "clear_console":
		w.console.Clear()
		return w.a.say("console cleared")
	case "rename_tab":
		t := w.tab()
		if t == nil {
			return w.a.say("no tab to rename")
		}
		f := newForm("Rename tab", []formField{{Key: "name", Label: "Name (empty restores it)", Kind: fText, Value: w.tabTitle(t)}}, func(v map[string]string) tea.Cmd {
			if w.names == nil {
				w.names = map[int]string{}
			}
			if n := strings.TrimSpace(v["name"]); n != "" {
				w.names[t.ID()] = n
			} else {
				delete(w.names, t.ID())
			}
			return nil
		})
		w.a.push(f)
		return nil
	}
	cur := w.tab()
	if cur == nil {
		return w.a.say("no tabs open")
	}
	grp := w.pane(cur)
	switch name {
	case "close_other_tabs":
		return w.closeMany(func(i int) bool { return w.tabs[i] == cur || w.pane(w.tabs[i]) != grp })
	case "close_tabs_right":
		return w.closeMany(func(i int) bool { return i <= w.cur || w.pane(w.tabs[i]) != grp })
	}
	return w.closeMany(func(int) bool { return false }) // close_all_tabs
}

// closeMany closes every tab keep rejects, asking first when some of them have unsaved changes.
func (w *Workspace) closeMany(keep func(i int) bool) tea.Cmd {
	var drop []tabber
	dirty := 0
	for i, t := range w.tabs {
		if !keep(i) {
			drop = append(drop, t)
			if t.Dirty() {
				dirty++
			}
		}
	}
	if len(drop) == 0 {
		return w.a.say("no tabs to close")
	}
	cur := w.tab()
	do := func() tea.Cmd {
		for _, t := range drop {
			w.closeTab(indexOf(w.tabs, t))
		}
		if i := indexOf(w.tabs, cur); i >= 0 {
			w.cur = i
		}
		return w.a.say("closed %s", plural(len(drop), "tab"))
	}
	if dirty > 0 {
		w.a.push(&confirmDlg{title: "Close " + plural(dirty, "tab") + " with unsaved changes?", lines: []string{"Pending edits in them will be lost."}, yes: "Close", danger: true, onYes: do})
		return nil
	}
	return do()
}

// ---- editor: auto_save_tabs (M4 item 3) -------------------------------------------------

// saveTabs stores the editor tabs for the next connect ([editor] auto_save_tabs = none | first | all).
func (w *Workspace) saveTabs() {
	mode := w.a.d.Cfg.Editor.AutoSaveTabs
	if mode != "first" && mode != "all" {
		return
	}
	var out []store.OpenTab
	for _, t := range w.tabs {
		q, ok := t.(*queryTab)
		if !ok || strings.TrimSpace(q.ed.Text()) == "" {
			continue
		}
		out = append(out, store.OpenTab{Kind: "sql", Name: w.names[q.id], Text: q.ed.Text(), SavedAs: q.savedAs})
		if mode == "first" {
			break
		}
	}
	if err := w.a.d.State.SetOpenTabs(w.conn.Name, out); err != nil {
		w.a.toast, w.a.toastErr = "could not remember the open tabs: "+err.Error(), true
	}
}

// restoreTabs reopens what saveTabs stored.
func (w *Workspace) restoreTabs() tea.Cmd {
	if m := w.a.d.Cfg.Editor.AutoSaveTabs; m != "first" && m != "all" {
		return nil
	}
	var cmds []tea.Cmd
	for _, ot := range w.a.d.State.OpenTabs(w.conn.Name) {
		if ot.Kind != "sql" {
			continue
		}
		cmds = append(cmds, w.newQueryTab(ot.Text))
		if ot.SavedAs != "" { // keep auto-saving into the saved query, unless it is gone or shared (read-only)
			if q, err := w.a.d.Queries.Read(ot.SavedAs); err == nil && !q.Shared {
				if t, ok := w.tab().(*queryTab); ok {
					t.savedAs, t.lastAuto = q.Path, ot.Text
				}
			}
		}
		if ot.Name != "" {
			if w.names == nil {
				w.names = map[int]string{}
			}
			w.names[w.tab().ID()] = ot.Name
		}
	}
	return tea.Batch(cmds...)
}

// ---- results (M4 item 4) ----------------------------------------------------------------

// saveMessage writes the Message view (notices and status lines) to a text file.
func (t *queryTab) saveMessage(rv *resultView) tea.Cmd {
	t.ws.a.push(newForm("Save message as text", []formField{{Key: "path", Label: "File", Kind: fText, Value: "message.txt"}}, func(v map[string]string) tea.Cmd {
		path := strings.TrimSpace(v["path"])
		if path == "" {
			return t.ws.a.fail(fmt.Errorf("file name required"))
		}
		return t.ws.a.ifFree(func() tea.Cmd {
			if err := writeFile(path, rv.msg+"\n"); err != nil {
				return t.ws.a.fail(err)
			}
			return t.ws.a.say("saved %s", path)
		}, path)
	}))
	return nil
}

// numberedPath puts -n before the extension: out.csv.gz, 2 -> out-2.csv.gz.
func numberedPath(path string, n int) string {
	base, ext := path, ""
	for _, e := range []string{".csv.gz", ".json.gz", ".sql.gz"} {
		if strings.HasSuffix(path, e) {
			base, ext = strings.TrimSuffix(path, e), e
		}
	}
	if ext == "" {
		ext = filepath.Ext(path)
		base = strings.TrimSuffix(path, ext)
	}
	return fmt.Sprintf("%s-%d%s", base, n, ext)
}

// runExportAll writes every result tab to its own file, path-1, path-2...
func (w *Workspace) runExportAll(path string, o export.Options, results []*db.Result) tea.Cmd {
	return w.job("export "+plural(len(results), "result"), func(ctx context.Context, _ db.Session, _ func(func() tea.Cmd)) error {
		for i, res := range results {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := writeAtomic(numberedPath(path, i+1), func(f io.Writer) error { return export.Write(f, res, o) }); err != nil {
				return err
			}
		}
		return nil
	}, func() (string, tea.Cmd) {
		return fmt.Sprintf("exported %s next to %s", plural(len(results), "file"), path), nil
	})
}

// ---- sidebar: saved queries and history files (M4 item 6) ---------------------------------

// sortOrSave is S: the order of saved queries, or Save As… on the history tab.
func (s *sidebar) sortOrSave() tea.Cmd {
	switch s.tab {
	case 1:
		var items []pickItem
		for _, by := range []string{"name", "created", "modified"} {
			items = append(items, pickItem{Label: by, Value: by})
		}
		s.w.a.push(&pickerDlg{title: "Sort saved queries by", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
			s.qsort, s.sel, s.top = it.Value.(string), 0, 0
			return nil
		}})
	case 2:
		return s.saveHistory()
	}
	return nil
}

func (s *sidebar) saveHistory() tea.Cmd {
	conn := s.w.conn.Name
	s.w.a.push(newForm("Save history as", []formField{
		{Key: "format", Label: "Format", Kind: fChoice, Options: []string{"csv", "json"}, Value: "csv"},
		{Key: "path", Label: "File", Kind: fText, Value: "history.csv"},
	}, func(v map[string]string) tea.Cmd {
		all, err := s.w.a.d.Hist.List(conn, "", 0)
		if err != nil {
			return s.w.a.fail(err)
		}
		path := strings.TrimSpace(v["path"])
		return s.w.a.ifFree(func() tea.Cmd {
			if err := writeAtomic(path, func(f io.Writer) error { return store.ExportHistory(f, all, v["format"]) }); err != nil {
				return s.w.a.fail(err)
			}
			return s.w.a.say("saved %s to %s", plural(len(all), "entry"), path)
		}, path)
	}))
	return nil
}

// importFile is i: import a .sql file as a saved query, or a history file on the history tab.
func (s *sidebar) importFile() tea.Cmd {
	switch s.tab {
	case 1:
		s.w.a.push(newForm("Import a .sql file as a saved query", []formField{{Key: "path", Label: "File", Kind: fText}}, func(v map[string]string) tea.Cmd {
			q, err := store.ImportSQLFile(strings.TrimSpace(v["path"]))
			if err != nil {
				return s.w.a.fail(err)
			}
			return s.w.a.ifNoQuery("", q.Name, func() tea.Cmd {
				if _, err := s.w.a.d.Queries.Save("", q.Name, q.Keywords, q.SQL); err != nil {
					return s.w.a.fail(err)
				}
				s.queries, _ = s.w.a.d.Queries.List()
				return s.w.a.say("imported %s", q.Name)
			})
		}))
	case 2:
		s.w.a.push(newForm("Import history", []formField{{Key: "path", Label: "File (csv or json)", Kind: fText}}, func(v map[string]string) tea.Cmd {
			f, err := os.Open(strings.TrimSpace(v["path"]))
			if err != nil {
				return s.w.a.fail(err)
			}
			defer f.Close()
			es, err := store.ImportHistory(f)
			if err != nil {
				return s.w.a.fail(err)
			}
			// off the UI loop (each Append locks and rewrites a file); a failure says how far it got
			w, done := s.w, 0
			return w.job("import history", func(ctx context.Context, _ db.Session, report func(func() tea.Cmd)) error {
				for i, e := range es {
					if err := ctx.Err(); err != nil {
						return fmt.Errorf("stopped after %d of %d entries: %w", done, len(es), err)
					}
					if err := w.a.d.Hist.Append(w.conn.Name, e); err != nil {
						return fmt.Errorf("imported %d of %d entries, then: %w", done, len(es), err)
					}
					done = i + 1
					if done%500 == 0 {
						n := done
						report(func() tea.Cmd { return w.a.say("importing history… %d of %d", n, len(es)) })
					}
				}
				return nil
			}, func() (string, tea.Cmd) {
				s.hist, _ = w.a.d.Hist.List(w.conn.Name, "", 500)
				return "imported " + plural(len(es), "entry"), nil
			})
		}))
	}
	return nil
}

// openFile is f on the queries tab: a .sql file straight into an editor tab, not saved anywhere.
func (s *sidebar) openFile() tea.Cmd {
	if s.tab != 1 {
		return nil
	}
	s.w.a.push(newForm("Open a file in a tab", []formField{{Key: "path", Label: "File", Kind: fText}}, func(v map[string]string) tea.Cmd {
		b, err := os.ReadFile(strings.TrimSpace(v["path"]))
		if err != nil {
			return s.w.a.fail(err)
		}
		return s.w.newQueryTab(string(b))
	}))
	return nil
}

// addSharedDirs lists the [queries] shared_dirs beside the main folder.
func (a *App) addSharedDirs() {
	for _, d := range a.d.Cfg.Queries.SharedDirs {
		d = os.ExpandEnv(d)
		if strings.HasPrefix(d, "~/") {
			if h, err := os.UserHomeDir(); err == nil {
				d = filepath.Join(h, d[2:])
			}
		}
		a.d.Queries.AddDir(d)
	}
}
