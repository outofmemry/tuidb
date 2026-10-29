// backup.go: Backup and restore: tool lookup, the run dialog and the options form.

package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/backup"
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/export"
	"webtuidb/internal/store"
)

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
		return w.a.fail(fmt.Errorf("%s is read-only: backup and restore are refused", w.conn.Name))
	}
	if w.jobCancel != nil {
		return w.a.fail(fmt.Errorf("another export, import, backup or search is still running (cancel it first)"))
	}
	// T-P3-17: say a missing tool before the form is filled in (a pg restore picks psql or pg_restore by the file).
	// The form still opens, because Ctrl+t inside it is where the path gets set.
	missing := ""
	if tool := backup.ToolForConn(w.conn, restore); !(restore && w.conn.Driver == config.Postgres) {
		if tool.Name == "" {
			return w.a.fail(fmt.Errorf("backup is not supported for %s", w.conn.Driver))
		}
		if !tool.Found {
			missing = tool.Name + " not found (on PATH or at its configured path): press Ctrl+t to set its path"
		}
	}
	pg := w.conn.Driver == config.Postgres
	pre := strings.ToLower(verb) + "." // the options last used are remembered per connection and per direction
	mem := w.a.d.State.BackupOptions(w.conn.Name)
	val := func(key, def string) string {
		if v, ok := mem[pre+key]; ok && v != "" {
			return v
		}
		return def
	}
	f := newForm(verb+" "+dbName, []formField{
		{Key: "file", Label: "File", Kind: fText, Value: val("file", w.backupDefaultFile(dbName, restore))},
		{Key: "pattern", Label: "File-name pattern", Kind: fText, Value: val("pattern", ""), Hint: "{connection} {database} {tag} {date} {time}; empty: use File", Show: func(map[string]string) bool { return !restore }},
		{Key: "extra", Label: "Extra options", Kind: fText, Value: val("extra", ""), Hint: "tool arguments; Ctrl+o: common options"},
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
		for _, k := range []string{"file", "pattern", "extra", "gzip", "format", "data", "schema"} {
			cur[pre+k] = v[k]
		}
		if !restore && strings.TrimSpace(v["file"]) == w.backupDefaultFile(dbName, false) {
			cur[pre+"file"] = "" // the default is a dated pattern expanded for today: remember the pattern, not the file
		}
		w.a.d.State.SetBackupOptions(w.conn.Name, cur)
		conn := w.conn
		conn.Password = w.o.Password
		newDB := strings.TrimSpace(v["newdb"])
		o := backup.Options{Conn: conn, Database: dbName, File: strings.TrimSpace(v["file"]), Gzip: v["gzip"] == "true", Format: v["format"],
			DataOnly: v["data"] == "true", SchemaOnly: v["schema"] == "true", NewDatabase: newDB,
			Extra: strings.Fields(v["extra"]), Filename: strings.TrimSpace(v["pattern"]), Tag: conn.Tag}
		if restore {
			o.Filename = ""
		}
		cmd, err := backup.Plan(o, restore)
		if err != nil {
			return w.a.fail(err)
		}
		shown := backup.Display(cmd)
		var onFail func() tea.Cmd
		run := func() tea.Cmd {
			out := backup.OutputPath(o) // the file actually written, after the name pattern is expanded
			w.jobFile = out
			return w.runBackup(cmd, verb, filepath.Base(out), out, restore, onFail)
		}
		yes := func() tea.Cmd { return w.a.ifFree(run, backup.OutputPath(o)) } // a backup never replaces an older dump silently
		if restore {
			// a restore overwrites data: same gate as DROP, with the command line as the statement
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
	f.hotHint = "Ctrl+t: tool paths   Ctrl+o: common options"
	f.err = missing
	f.hotkeys = map[string]func(map[string]string) tea.Cmd{
		"ctrl+t": func(map[string]string) tea.Cmd { return w.toolPaths() },
		"ctrl+o": func(v map[string]string) tea.Cmd { return w.commonOptions(f, restore, v) },
	}
	w.a.push(f)
	return nil
}

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
// The list is built when it opens, from the File and Format values at that moment, so it offers only the flags of
// the tool that file will run (psql or pg_restore, the mysql client).
func (w *Workspace) commonOptions(f *formDlg, restore bool, v map[string]string) tea.Cmd {
	opts := backup.CommonOptionsForFormat(w.conn.Driver, restore, strings.TrimSpace(v["file"]), v["format"])
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
