package web

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"webtuidb/internal/backup"
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/export"
)

type backupReq struct {
	File                 string
	Database             string
	Gzip                 bool
	Format               string
	DataOnly, SchemaOnly bool
	NewDatabase          string            // restore: create this database first (through the safe-mode gate) and restore into it
	CreateOpts           map[string]string // encoding, collation for NewDatabase
	Run                  bool              // false = only show the command line
	Confirmed            bool
	Password             string
	Extra                []string // free-form tool arguments, appended last (backup.Options.Extra)
	Filename             string   // a name pattern ({connection} {database} {table}=database {tag} {date} {time}) used when File is empty
}

func (s *Server) backup(ss *session, r *http.Request) (any, error) {
	return s.backupRestore(ss, r, false)
}

func (s *Server) restore(ss *session, r *http.Request) (any, error) {
	return s.backupRestore(ss, r, true)
}

// backupOptions lists the flags the backup (or, with ?restore=1, restore) dialog may offer for this driver:
// the common ones with a description, and every extra flag the server accepts. With ?file=<name> (and optional
// &format=custom) a Postgres restore lists only the flags of the tool that file runs (psql or pg_restore).
func (s *Server) backupOptions(ss *session, r *http.Request) (any, error) {
	q := r.URL.Query()
	restore, file, format := q.Get("restore") == "1", q.Get("file"), q.Get("format") // file and format are optional: without them a Postgres restore gets the union
	return map[string]any{"options": nonNil(backup.CommonOptionsForFormat(ss.conn.Driver, restore, file, format)), "extra": nonNil(backup.AllowedExtraFor(ss.conn.Driver, restore, file, format))}, nil
}

// backupFile confines a backup/restore file to <DataDir>/backups: no subdirectories, no "..", no symlinks.
func backupFile(f string) (path string, exists bool, err error) {
	dir := filepath.Join(config.DataDir(), "backups")
	if strings.Contains(f, "..") {
		return "", false, bad("%q: .. is not allowed", f)
	}
	p := filepath.Clean(f)
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	if filepath.Dir(p) != dir || filepath.Base(p) == "." {
		return "", false, bad("backup files live in %s; give a file name there", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", false, err
	}
	if fi, err := os.Lstat(dir); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		return "", false, bad("%s must not be a symlink", dir)
	}
	switch fi, err := os.Lstat(p); {
	case err == nil && !fi.Mode().IsRegular():
		return "", false, bad("%q is not a regular file", p)
	case err == nil:
		exists = true
	}
	return p, exists, nil
}

func (s *Server) backupRestore(ss *session, r *http.Request, restore bool) (any, error) {
	var req backupReq
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if req.File == "" && req.Filename != "" && !restore {
		dbn := req.Database
		if dbn == "" {
			dbn = ss.o.Sess.CurrentDatabase()
		}
		req.File = export.Filename(req.Filename, map[string]string{"connection": ss.conn.Name, "database": dbn, "table": dbn, "tag": ss.conn.Tag})
		if !strings.Contains(req.File, ".") { // shortcut: a pattern without an extension gets .sql(.gz), whatever the format
			req.File += ".sql"
			if req.Gzip {
				req.File += ".gz"
			}
		}
	}
	if req.File == "" {
		return nil, bad("a file path is required")
	}
	var file string
	var exists bool
	var err error
	if ss.conn.Driver == config.ClickHouse {
		// the name is on the server's "backups" disk, not a local path: no rewriting, no local existence check
		if strings.HasPrefix(req.File, "-") {
			return nil, bad("%q: a file name cannot start with -", req.File)
		}
		file = req.File
	} else if file, exists, err = backupFile(req.File); err != nil {
		return nil, err
	}
	req.File = file
	conn := ss.conn
	if conn.ReadOnly { // the dump tools log in on their own, so the session's read-only guard does not cover them
		return nil, bad("this connection is read only; backup and restore are refused")
	}
	if restore && !exists && ss.conn.Driver != config.ClickHouse {
		return nil, bad("%q does not exist in the backups folder", filepath.Base(file))
	}
	conn.Password = ss.o.Password
	if req.Database == "" {
		req.Database = ss.o.Sess.CurrentDatabase()
	}
	tool := backup.ToolForConn(conn, restore)
	if !restore {
		req.NewDatabase = ""
	}
	cmd, err := backup.Plan(backup.Options{Conn: conn, Database: req.Database, File: req.File, Gzip: req.Gzip, Format: req.Format, DataOnly: req.DataOnly, SchemaOnly: req.SchemaOnly, NewDatabase: req.NewDatabase, Extra: req.Extra}, restore)
	if err != nil {
		if !tool.Found && !req.Run { // let the dialog name the missing executable
			return map[string]any{"display": "", "tool": tool, "levelLabel": ss.lvl().Label()}, nil
		}
		return nil, bad("%v", err)
	}
	display := backup.Display(cmd)
	info := map[string]any{"display": display, "tool": tool}
	gated := []db.Statement{{Text: display}}
	var create []db.Statement
	if req.NewDatabase != "" { // the new database is created first, through the same gate
		if create, err = ss.o.Sess.PlanAdmin(db.OpCreateDatabase, db.ObjRef{Name: req.NewDatabase}, req.CreateOpts); err != nil {
			return nil, bad("%v", err)
		}
		gated = append(create, gated...)
		info["create"] = create
	}
	if !req.Run {
		info["gate"] = ss.gate(gated)
		info["exists"] = exists
		info["levelLabel"] = ss.lvl().Label()
		return info, nil
	}
	if !tool.Found {
		return nil, bad("%s is not on PATH", tool.Name)
	}
	// a restore overwrites data and a backup overwrites a file: both are gated like a destructive statement
	if err := ss.check(ss.gate(gated), req.Confirmed, req.Password); err != nil {
		return nil, err
	}
	if len(create) > 0 {
		// detached like /apply: a closed tab must not cancel a CREATE DATABASE that may already have committed
		cctx, ccancel := context.WithTimeout(context.WithoutCancel(r.Context()), applyTimeout)
		defer ccancel()
		if err := ss.o.Sess.Apply(cctx, create); err != nil {
			return nil, bad("create %s: %v", req.NewDatabase, err)
		}
		ss.record(create[0].Shown(), "gui", 0, "")
	}
	if st := s.o.State; st != nil { // remembered per connection for the next time
		pre := "backup." // same keys as the terminal UI; a restore file name is never remembered
		mem := map[string]string{pre + "gzip": strconv.FormatBool(req.Gzip), pre + "format": req.Format, pre + "data": strconv.FormatBool(req.DataOnly), pre + "schema": strconv.FormatBool(req.SchemaOnly)}
		if restore {
			pre = "restore."
			mem = map[string]string{pre + "gzip": strconv.FormatBool(req.Gzip), pre + "format": req.Format}
		} else {
			mem[pre+"file"] = filepath.Base(req.File)
		}
		st.SetBackupOptions(ss.conn.Name, mem)
	}
	if exists && !restore && !req.Confirmed {
		return nil, &apiErr{Code: http.StatusConflict, Msg: "the backup file already exists; confirm to overwrite it", Extra: map[string]any{"exists": true}}
	}
	if err := ss.jobSlot(); err != nil {
		return nil, err
	}
	j := &job{kind: "backup", title: "Backup " + filepath.Base(req.File), fileName: filepath.Base(req.File), path: req.File}
	if restore {
		j.kind, j.title, j.path = "restore", "Restore "+filepath.Base(req.File), ""
	}
	info["job"] = ss.startJob(j, func(ctx context.Context) error {
		err := backup.Run(ctx, cmd, func(line string) {
			j.mu.Lock()
			defer j.mu.Unlock()
			if len(j.lines) < 5000 { // shortcut: output past 5000 lines is dropped
				j.lines = append(j.lines, line)
			}
		})
		if err == nil {
			ss.clearColCache()
			ss.record(display, "backup", 0, "")
		}
		if err != nil && len(create) > 0 { // shortcut: not dropped automatically (that would need its own gate); say so
			err = fmt.Errorf("%w (database %s was created before the restore and is still there; drop it if you do not want it)", err, req.NewDatabase)
		}
		return err
	})
	return info, nil
}

// checkTools lets the browser change a connection's tool paths only to what config.ToolPathOK accepts: a path override
// runs a program on the server, so it must not name /bin/sh.
func checkTools(c, old config.Connection) error {
	for k, p := range c.Tools {
		if p != "" && old.Tools[k] != p && !config.ToolPathOK(k, p) {
			return bad("%q: tool %q must be an absolute path to a file named like %s", c.Name, k, k)
		}
	}
	return nil
}
