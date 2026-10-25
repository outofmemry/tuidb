package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/backup"
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/menus"
)

// Milestone 7: admin tools.

// ---- settings (M7.7) ---------------------------------------------------------------------

type settingsDoneMsg struct{ err error }

// cfgPath is the config file the program was started with ([Deps.CfgPath], else the default location).
func (a *App) cfgPath() string {
	return orDefault(a.d.CfgPath, filepath.Join(config.ConfigDir(), "config.toml"))
}

// editorCommand is $VISUAL or $EDITOR (vi when neither is set) opening path.
func editorCommand(path string) *exec.Cmd {
	ed := strings.Fields(orDefault(os.Getenv("VISUAL"), orDefault(os.Getenv("EDITOR"), "vi")))
	return exec.Command(ed[0], append(ed[1:], path)...)
}

// settings suspends the program and opens config.toml in the user's editor.
func (a *App) settings() tea.Cmd {
	path := a.cfgPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return a.fail(err)
	}
	return tea.ExecProcess(editorCommand(path), func(err error) tea.Msg { return settingsDoneMsg{err} })
}

// ---- process list on its own session (M7.1) ----------------------------------------------

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

// ---- backup / restore (M7.2) -------------------------------------------------------------

// backupTools names the executables a driver's backup and restore use (the keys of Connection.Tools).
func backupTools(driver string) []string {
	switch driver {
	case config.Postgres:
		return []string{"pg_dump", "pg_restore", "psql"}
	case config.MySQL:
		return []string{"mysqldump", "mysql"}
	case config.MariaDB:
		return []string{"mariadb-dump", "mariadb", "mysqldump", "mysql"}
	case config.ClickHouse:
		return []string{"clickhouse-client"}
	case config.Mongo:
		return []string{"mongodump", "mongorestore"}
	case config.Redis:
		return []string{"redis-cli"}
	}
	return nil
}

// toolPaths edits Connection.Tools: where each backup executable lives when it is not on PATH.
func (w *Workspace) toolPaths() tea.Cmd {
	names := backupTools(w.conn.Driver)
	if len(names) == 0 {
		return w.a.say("backup is not supported for %s", w.conn.Driver)
	}
	var fields []formField
	for _, n := range names {
		fields = append(fields, formField{Key: n, Label: n, Kind: fText, Value: w.conn.Tools[n], Hint: "empty: look it up on PATH"})
	}
	f := newForm("Tool paths for "+w.conn.Name, fields, func(v map[string]string) tea.Cmd {
		tools := map[string]string{}
		for _, n := range names {
			if p := strings.TrimSpace(v[n]); p != "" {
				tools[n] = p
			}
		}
		if len(tools) == 0 {
			tools = nil
		}
		w.conn.Tools = tools
		for i := range w.a.d.Conns {
			if w.a.d.Conns[i].Name == w.conn.Name {
				w.a.d.Conns[i].Tools = tools
			}
		}
		if w.conn.Source != "" { // from a [[source]] folder: kept for this session only
			return w.a.say("tool paths set for this session (%s comes from %s)", w.conn.Name, w.conn.Source)
		}
		if err := w.a.saveConns(); err != nil {
			return w.a.fail(err)
		}
		return w.a.say("tool paths saved")
	})
	f.validate = func(v map[string]string) error {
		for _, n := range names {
			if p := strings.TrimSpace(v[n]); p != "" {
				if st, err := os.Stat(p); err != nil || st.IsDir() {
					return fmt.Errorf("%s: no such file", p)
				}
			}
		}
		return nil
	}
	w.a.push(f)
	return nil
}

func (w *Workspace) backupAction(restore bool) tea.Cmd {
	verb, dbName := map[bool]string{false: "Backup", true: "Restore"}[restore], w.sess.CurrentDatabase()
	if w.conn.ReadOnly { // the dump tools log in on their own, so the driver-level read-only guard does not apply
		return w.a.say("%s is read-only: backup and restore are refused", w.conn.Name)
	}
	if w.jobCancel != nil {
		return w.a.say("another export, import, backup or search is still running (cancel it first)")
	}
	pg := w.conn.Driver == config.Postgres
	pre := strings.ToLower(verb) + "." // the options last used are remembered per connection and per direction
	mem := w.a.d.State.BackupOptions(w.conn.Name)
	val := func(key, def string) string {
		if v, ok := mem[pre+key]; ok {
			return v
		}
		return def
	}
	f := newForm(verb+" "+dbName, []formField{
		{Key: "file", Label: "File", Kind: fText, Value: val("file", dbName+".sql")},
		{Key: "gzip", Label: "Gzip", Kind: fBool, Value: val("gzip", "false")},
		{Key: "format", Label: "Format", Kind: fChoice, Options: []string{"plain", "custom"}, Value: val("format", "plain"), Show: func(map[string]string) bool { return pg }},
		{Key: "data", Label: "Data only", Kind: fBool, Value: val("data", "false"), Show: func(v map[string]string) bool { return !restore && v["schema"] != "true" }},
		{Key: "schema", Label: "Schema only", Kind: fBool, Value: val("schema", "false"), Show: func(v map[string]string) bool { return !restore && v["data"] != "true" }},
		{Key: "newdb", Label: "Restore into a new database", Kind: fText, Hint: "empty: into " + dbName, Show: func(map[string]string) bool { return restore }},
	}, func(v map[string]string) tea.Cmd {
		cur := w.a.d.State.BackupOptions(w.conn.Name) // merge: other keys (and the web's) stay
		if cur == nil {
			cur = map[string]string{}
		}
		for _, k := range []string{"file", "gzip", "format", "data", "schema"} {
			cur[pre+k] = v[k]
		}
		w.a.d.State.SetBackupOptions(w.conn.Name, cur)
		conn := w.conn
		conn.Password = w.o.Password
		newDB := strings.TrimSpace(v["newdb"])
		o := backup.Options{Conn: conn, Database: dbName, File: strings.TrimSpace(v["file"]), Gzip: v["gzip"] == "true", Format: v["format"],
			DataOnly: v["data"] == "true", SchemaOnly: v["schema"] == "true", NewDatabase: newDB}
		cmd, err := backup.Plan(o, restore)
		if err != nil {
			return w.a.fail(err)
		}
		shown := backup.Display(cmd)
		var onFail func() tea.Cmd
		run := func() tea.Cmd { return w.runBackup(cmd, verb, filepath.Base(o.File), onFail) }
		yes := run
		if restore { // a restore overwrites data: same gate as DROP, with the command line as the statement
			yes = func() tea.Cmd { return w.gate([]db.Statement{{Text: shown}}, run) }
		}
		lines := []string{shown}
		if restore && newDB != "" {
			create, err := w.sess.PlanAdmin(db.OpCreateDatabase, db.ObjRef{Name: newDB}, map[string]string{})
			if err != nil {
				return w.a.fail(err)
			}
			restoreOnly := yes
			onFail = func() tea.Cmd { // the new database is empty or half filled: offer to remove it, through the gate
				drop, err := w.sess.PlanAdmin(db.OpDropDatabase, db.ObjRef{Name: newDB}, map[string]string{})
				if err != nil {
					return nil
				}
				w.a.push(&confirmDlg{title: "Drop the new database " + newDB + "?", lines: append([]string{"The restore failed."}, stmtTexts(drop)...), yes: "Drop", danger: true,
					onYes: func() tea.Cmd { return w.execute(drop, func(error) tea.Cmd { return nil }) }})
				return nil
			}
			yes = func() tea.Cmd { // create the database through the gate, then restore into it
				return w.execute(create, func(err error) tea.Cmd {
					if err != nil {
						return nil
					}
					return restoreOnly()
				})
			}
			lines = append([]string{"First creates database " + newDB + ":"}, append(stmtTexts(create), "", shown)...)
		}
		w.a.push(&confirmDlg{title: fmt.Sprintf("%s %s?", verb, dbName), lines: lines, yes: verb, danger: restore, onYes: yes})
		return nil
	})
	f.validate = func(v map[string]string) error {
		p := strings.TrimSpace(v["file"])
		if p == "" {
			return fmt.Errorf("file name required")
		}
		if st, err := os.Stat(p); restore && (err != nil || st.IsDir()) {
			return fmt.Errorf("cannot read that file")
		}
		return nil
	}
	f.submit = "Enter: continue"
	f.hotHint = "Ctrl+t: tool paths"
	f.hotkeys = map[string]func(map[string]string) tea.Cmd{"ctrl+t": func(map[string]string) tea.Cmd { return w.toolPaths() }}
	w.a.push(f)
	return nil
}

func stmtTexts(stmts []db.Statement) []string {
	out := make([]string, len(stmts))
	for i, s := range stmts {
		out[i] = s.Shown()
	}
	return out
}

// ---- users: column-level privileges (M7.3) ---------------------------------------------------

// grantText shows a grant's privileges: "SELECT, INSERT", or "SELECT (a, b), INSERT (a, b)" for a column grant.
func grantText(g db.Grant) string {
	if len(g.Columns) == 0 {
		return strings.Join(g.Privs, ", ")
	}
	out := make([]string, len(g.Privs))
	for i, p := range g.Privs {
		out[i] = p + " (" + strings.Join(g.Columns, ", ") + ")"
	}
	return strings.Join(out, ", ")
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ---- MCP (M7.5) ----------------------------------------------------------------------------

// mcpToolsHelp lists what an MCP client can call, for the top of the activity view.
const mcpToolsHelp = "Tools: list_connections, list_open_connections, open_connection, disconnect, execute_read_query, execute_query, " +
	"list_saved_queries, get_saved_query, list_history, list_boards, run_board"

// ---- assistant: provider per chat (M7.6) ---------------------------------------------------

// aiCfg is the assistant settings for this chat's provider (the default when none was picked).
func (t *assistantTab) aiCfg() config.AI {
	c, err := config.AIFor(t.ws.a.d.Cfg.AI, t.ws.m3.provider)
	if err != nil {
		c, _ = config.AIFor(t.ws.a.d.Cfg.AI, "")
	}
	return c
}

// pickProvider lists the default first, then the [ai.providers.<name>] entries.
func (t *assistantTab) pickProvider() tea.Cmd {
	w := t.ws
	items := []pickItem{{Label: "default", Detail: w.a.d.Cfg.AI.Provider + " " + w.a.d.Cfg.AI.Model, Value: ""}}
	for _, n := range config.AIProviderNames(w.a.d.Cfg.AI) {
		c, _ := config.AIFor(w.a.d.Cfg.AI, n)
		items = append(items, pickItem{Label: n, Detail: c.Provider + " " + c.Model, Value: n})
	}
	for i := range items {
		if items[i].Value.(string) == w.m3.provider {
			items[i].Label = "• " + items[i].Label
		}
	}
	w.a.push(&pickerDlg{title: "Assistant provider (this chat)", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		w.m3.provider, w.m3.model = it.Value.(string), "" // the model of the old provider does not carry over
		return w.a.say("assistant provider: %s", orDefault(w.m3.provider, "default"))
	}})
	return nil
}
