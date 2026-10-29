// files.go: Files: opening a SQL file or a recent file, the SQL viewer, importing a file, saving history.

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
	"webtuidb/internal/store"
)

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
			path := strings.TrimSpace(v["path"])
			// the file is opened and parsed in the job, off the UI loop; a failure or a cancel still
			// refreshes the list and says how far it got
			w, total, written := s.w, 0, 0
			refresh := func() { s.hist, _ = w.a.d.Hist.List(w.conn.Name, "", 500) }
			return w.job("import history", func(ctx context.Context, _ db.Session, report func(func() tea.Cmd)) error {
				defer func() { report(func() tea.Cmd { refresh(); return nil }) }()
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				defer f.Close()
				es, err := store.ImportHistory(f)
				if err != nil {
					return err
				}
				total = len(es)
				for lo := 0; lo < len(es); lo += 500 {
					if err := ctx.Err(); err != nil {
						return fmt.Errorf("stopped after %d of %d entries: %w", written, total, err)
					}
					n, err := w.a.d.Hist.AppendAll(w.conn.Name, es[lo:min(lo+500, len(es))])
					written += n
					if err != nil {
						return fmt.Errorf("imported %d of %d entries, then: %w", written, total, err)
					}
					wr := written
					report(func() tea.Cmd {
						return w.a.say("importing history… %d of %d read, %d new", min(lo+500, total), total, wr)
					})
				}
				return nil
			}, func() (string, tea.Cmd) {
				refresh()
				return "imported " + plural(written, "new entry") + fmt.Sprintf(" (%d in the file)", total), nil
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

// maxSQLFile caps what open_file reads into an editor.
const maxSQLFile = 8 << 20

func (a *App) openFile() tea.Cmd {
	a.push(newForm("Open a SQL file", []formField{{Key: "path", Label: "File", Kind: fText}}, func(v map[string]string) tea.Cmd {
		return a.openSQLFile(strings.TrimSpace(v["path"]))
	}))
	return nil
}

func (a *App) openRecent() tea.Cmd {
	var items []pickItem
	for _, f := range a.d.State.RecentFiles() {
		if !filepath.IsAbs(f) { // the web only knows base names: opening one here would hit the working directory
			continue
		}
		items = append(items, pickItem{Label: filepath.Base(f), Detail: f, Value: f})
	}
	a.push(&pickerDlg{title: "Recent files", filter: true, items: items, empty: "no files opened yet", onPick: func(it pickItem, _ bool) tea.Cmd {
		return a.openSQLFile(it.Value.(string))
	}})
	return nil
}

// openSQLFile reads path: inside a workspace into a new editor tab, on the welcome screen into the read-only viewer.
func (a *App) openSQLFile(path string) tea.Cmd {
	st, err := os.Stat(path)
	switch {
	case err != nil:
		return a.fail(err)
	case st.IsDir():
		return a.say("%s is a folder", path)
	case st.Size() > maxSQLFile:
		return a.say("%s is %d MB: files over %d MB are not opened", path, st.Size()>>20, maxSQLFile>>20)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return a.fail(err)
	}
	a.d.State.AddRecentFile(path, a.d.Cfg.General.RecentFiles)
	if w := a.ws(); w != nil {
		cmd := w.newQueryTab(string(b))
		if t, ok := w.tab().(*queryTab); ok {
			t.name, t.file, t.fileText = filepath.Base(path), path, t.ed.Text()
		}
		return cmd
	}
	a.viewSQL(path, string(b))
	return nil
}

// viewSQL is the connectionless viewer: the file highlighted, read-only, scrollable.
// shortcut: an overlay rather than a tab (the welcome screen has no tabs); connect and open the file there to edit or run it.
func (a *App) viewSQL(path, text string) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	kinds := lexLines("postgres", lines)
	d := &paneDlg{title: "SQL viewer · " + filepath.Base(path) + " (read-only)", hint: "↑↓ PgUp/PgDn: scroll   c: copy   Esc: close"}
	d.lines = func(th Theme, W int) []string {
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = th.Dim.Render(padLeft(fmt.Sprint(i+1), 4)+" ") + highlight(th, l, kinds[i])
		}
		return out
	}
	d.keys = map[string]func() tea.Cmd{"c": func() tea.Cmd { return a.copyText(text, "the file") }}
	a.push(d)
}
