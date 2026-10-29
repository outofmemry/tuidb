// Package backup builds and runs the command-line tools (pg_dump, mysqldump, ...) that back up and
// restore a database. The tools must be on PATH; secrets go through the environment or a URI that
// Display redacts, never a bare argument.
package backup

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/export"
)

type Options struct {
	Conn                 config.Connection // Password already resolved
	Database             string
	File                 string // output (backup) or input (restore) path
	Gzip                 bool   // text formats: pg plain, mysql; mongo uses its own --gzip
	Format               string // pg: "plain" | "custom"; others: ""
	DataOnly, SchemaOnly bool   // where the tool supports it
	// NewDatabase, when set, is the restore target instead of Database. Plan does not create it: the UIs
	// run PlanAdmin create_database first. Ignored for backups.
	NewDatabase string
	// Extra are free-form tool arguments, inserted after Plan's own flags (before any "--" / trailing file).
	// Only AllowedExtra entries are accepted (an entry like "-j 4" is split on spaces).
	Extra []string
	// Filename, when set, replaces the base name of File. With a "{" it is a pattern for export.Filename
	// ({connection} {database} {table}=database {tag} {date} {time}); otherwise it is used as is.
	Filename string
	Tag      string // value of {tag} in a Filename pattern
}

// Option is one common tool flag the UIs offer as a checkbox.
type Option struct{ Flag, Desc string }

// CommonOptions lists the usual extra flags for driver's dump (restore=false) or restore tool, for callers that
// have no file yet. A Postgres restore list is the union of pg_restore's and psql's flags; once the file is known
// use CommonOptionsFor, which lists only what the tool that will really run accepts.
func CommonOptions(driver string, restore bool) []Option {
	return CommonOptionsFor(driver, restore, "")
}

// archiveExt are the file extensions of a pg_dump custom / tar archive, which pg_restore reads (psql cannot).
var archiveExt = []string{".dump", ".tar", ".backup", ".pgdump", ".custom"}

// pgRestoreTool names the program a Postgres restore of filename runs: "pg_restore" for a custom-format
// request, an archive-looking name (.dump, .tar, ...) or a directory, "psql" for any other file (.sql, .sql.gz),
// and "" while there is no file name yet.
func pgRestoreTool(filename, format string) string {
	switch name := strings.TrimSuffix(strings.ToLower(filename), ".gz"); {
	case format == "custom":
		return "pg_restore"
	case filename == "":
		return ""
	default:
		for _, e := range archiveExt {
			if strings.HasSuffix(name, e) {
				return "pg_restore"
			}
		}
		if st, err := os.Stat(filename); err == nil && st.IsDir() {
			return "pg_restore"
		}
		return "psql"
	}
}

// CommonOptionsFor is CommonOptions narrowed to the tool that runs: for a Postgres restore, filename picks psql
// (a plain .sql / .sql.gz file) or pg_restore (.dump, .tar, a directory); a MySQL restore is the mysql client's
// flags, not mysqldump's. An empty filename gives the union for Postgres.
func CommonOptionsFor(driver string, restore bool, filename string) []Option {
	return CommonOptionsForFormat(driver, restore, filename, "")
}

// CommonOptionsForFormat is CommonOptionsFor for a dialog that also has the Postgres Format choice: "custom"
// means pg_restore whatever the file is called.
func CommonOptionsForFormat(driver string, restore bool, filename, format string) []Option {
	switch driver {
	case config.Postgres:
		if restore {
			pgr := []Option{{"--clean", "drop objects before recreating them"}, {"--if-exists", "with --clean, ignore missing objects"},
				{"--no-owner", "skip ownership changes"},
				{"-j 4", "restore with 4 parallel jobs"}}
			switch pgRestoreTool(filename, format) {
			case "psql":
				return []Option{{"--single-transaction", "run the whole file in one transaction"}}
			case "pg_restore":
				return pgr
			}
			return append(pgr, Option{"--single-transaction", "psql (plain .sql) only: one transaction"})
		}
		return []Option{
			{"--no-owner", "skip ownership commands"}, {"--no-privileges", "skip GRANT/REVOKE"}, {"--clean", "add DROP statements"},
			{"--if-exists", "with --clean, use IF EXISTS"}, {"--inserts", "INSERT statements instead of COPY"}}
	case config.MySQL, config.MariaDB:
		if restore { // the mysql client, not mysqldump
			return []Option{{"--force", "continue after SQL errors"}}
		}
		return []Option{{"--no-data", "schema only"}, {"--no-create-info", "data only"}, {"--single-transaction", "consistent snapshot (InnoDB)"},
			{"--routines", "include stored routines"}, {"--triggers", "include triggers"}, {"--events", "include events"},
			{"--skip-add-drop-table", "no DROP TABLE before CREATE"}}
	case config.Mongo:
		if restore {
			return []Option{{"--gzip", "archive is gzip-compressed"}, {"--drop", "drop collections before restoring"}, {"--oplogReplay", "replay the oplog"}}
		}
		return []Option{{"--gzip", "compress the archive"}, {"--oplog", "include the oplog for a point-in-time dump"}}
	}
	return nil
}

// safeExtra are value-less flags accepted beyond CommonOptions (matched exactly, never by prefix), per tool:
// the key is driver, with "/restore" appended for the restore tool.
var safeExtra = map[string][]string{
	config.Postgres:              {"--no-comments", "--no-tablespaces", "--no-acl", "--column-inserts", "--quote-all-identifiers", "--no-privileges"},
	config.Postgres + "/restore": {"--no-comments", "--no-tablespaces", "--no-acl", "--single-transaction", "--exit-on-error", "--no-privileges"},
	config.MySQL:                 {"--no-tablespaces", "--skip-lock-tables", "--skip-triggers", "--compact", "--hex-blob"},
	config.MariaDB:               {"--no-tablespaces", "--skip-lock-tables", "--skip-triggers", "--compact", "--hex-blob"},
	config.MySQL + "/restore":    {"--binary-mode"},
	config.MariaDB + "/restore":  {"--binary-mode"},
	config.Mongo:                 {},
}

// psqlExtra are the allowed flags a plain-SQL Postgres restore (psql) really takes; the rest are dropped.
var psqlExtra = map[string]bool{"--single-transaction": true}

// checkPsqlExtra refuses the (already allow-listed) extras psql does not take, naming the tool.
func checkPsqlExtra(extra []string) error {
	for i := 0; i < len(extra); i++ {
		if psqlExtra[extra[i]] {
			continue
		}
		f := extra[i]
		if f == "-j" && i+1 < len(extra) {
			f += " " + extra[i+1]
		}
		return fmt.Errorf("%s is not accepted by psql (a plain SQL restore); use the custom format for pg_restore options", f)
	}
	return nil
}

// AllowedExtraFor is AllowedExtra narrowed to the tool that runs, like CommonOptionsForFormat.
func AllowedExtraFor(driver string, restore bool, filename, format string) []string {
	if driver == config.Postgres && restore && pgRestoreTool(filename, format) == "psql" {
		return []string{"--single-transaction"}
	}
	return AllowedExtra(driver, restore)
}

// AllowedExtra is the exact allow-list of Options.Extra tokens for driver's dump or restore tool: the
// CommonOptions flags plus a few vetted ones, and "-j N" / "--jobs=N" (numeric N) for pg_restore.
// Anything else is refused by Plan, so no entry can redirect output (-f, -r, -L, --result-file), change the
// host or database (-d, --dbname, -h), run a command (-c) or read another config (--defaults-*).
func AllowedExtra(driver string, restore bool) []string {
	seen := map[string]bool{}
	var out []string
	add := func(f string) {
		if f != "" && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	for _, o := range CommonOptions(driver, restore) {
		if f, _, spaced := strings.Cut(o.Flag, " "); !spaced {
			add(f)
		}
	}
	if driver == config.Postgres && restore {
		add("-j N")
		add("--jobs=N")
	}
	key := driver
	if restore {
		key += "/restore"
	}
	for _, f := range safeExtra[key] {
		add(f)
	}
	return out
}

func checkExtra(driver string, restore bool, extra []string) ([]string, error) {
	allowed := map[string]bool{}
	for _, f := range AllowedExtra(driver, restore) {
		allowed[f] = !strings.ContainsAny(f, " =") && f != "-j N"
	}
	jobsOK := driver == config.Postgres && restore
	numeric := func(s string) bool {
		n, err := strconv.Atoi(s)
		return err == nil && n >= 1 && n <= 64 && strconv.Itoa(n) == s
	}
	var toks []string
	for _, e := range extra {
		toks = append(toks, strings.Fields(e)...)
	}
	var out []string
	for i := 0; i < len(toks); i++ {
		a := toks[i]
		switch {
		case !strings.HasPrefix(a, "-"):
			return nil, fmt.Errorf("extra option %q must start with -", a)
		case jobsOK && a == "-j" && i+1 < len(toks) && numeric(toks[i+1]):
			out = append(out, a, toks[i+1])
			i++
		case jobsOK && strings.HasPrefix(a, "--jobs=") && numeric(a[len("--jobs="):]):
			out = append(out, a)
		case allowed[a]:
			out = append(out, a)
		default:
			return nil, fmt.Errorf("extra option %q is not allowed", a)
		}
	}
	return out, nil
}

type Tool struct {
	Name  string
	Path  string
	Found bool
}

// candidates are tried in order with exec.LookPath.
func candidates(driver string, restore bool) []string {
	switch driver {
	case config.Postgres:
		return []string{map[bool]string{false: "pg_dump", true: "pg_restore"}[restore]}
	case config.MySQL:
		return []string{map[bool]string{false: "mysqldump", true: "mysql"}[restore]}
	case config.MariaDB:
		return []string{map[bool]string{false: "mariadb-dump", true: "mariadb"}[restore], map[bool]string{false: "mysqldump", true: "mysql"}[restore]}
	case config.ClickHouse:
		return []string{"clickhouse-client"}
	case config.Mongo:
		return []string{map[bool]string{false: "mongodump", true: "mongorestore"}[restore]}
	case config.Redis:
		return []string{"redis-cli"}
	}
	return nil
}

// ToolFor names the executable a driver needs. For pg plain-SQL restore Plan uses psql instead of
// pg_restore (found separately).
func ToolFor(driver string, restore bool) Tool {
	return ToolForConn(config.Connection{Driver: driver}, restore)
}

// ToolForConn is ToolFor honouring the connection's `tools` overrides (a path used instead of the PATH lookup).
func ToolForConn(cn config.Connection, restore bool) Tool {
	c := candidates(cn.Driver, restore)
	for _, n := range c {
		if p := cn.Tools[n]; p != "" {
			if _, err := os.Stat(p); err == nil {
				return Tool{Name: n, Path: p, Found: true}
			}
			return Tool{Name: n, Path: p}
		}
		if p, err := exec.LookPath(n); err == nil {
			return Tool{Name: n, Path: p, Found: true}
		}
	}
	if len(c) == 0 {
		return Tool{}
	}
	return Tool{Name: c[0]}
}

func (o Options) port() string {
	if p := o.Conn.Port; p != 0 {
		return strconv.Itoa(p)
	}
	return strconv.Itoa(config.DefaultPort(o.Conn.Driver))
}

func (o Options) db() string {
	if o.Database != "" {
		return o.Database
	}
	return o.Conn.Database
}

// job is the file plumbing Plan attaches to a cmd; Run finishes it (gzip, .part rename, temp files).
// shortcut: entries live until Run, so a planned-but-never-run cmd leaks one small map entry.
type job struct {
	prep func(*exec.Cmd) (func(error) error, error) // opens files before Start; the returned func runs after Wait
	show string                                     // appended to Display
	note string                                     // first line of the job output (what Plan left out)
}

var (
	jobsMu sync.Mutex
	jobs   = map[*exec.Cmd]*job{}
)

func attach(cmd *exec.Cmd, j *job) *exec.Cmd {
	jobsMu.Lock()
	jobs[cmd] = j
	jobsMu.Unlock()
	return cmd
}

// dump makes cmd write File+".part" (itself, or via stdout through gzip) and renames it on success.
func dump(cmd *exec.Cmd, file string, viaStdout, gz bool) *exec.Cmd {
	part := file + ".part"
	return attach(cmd, &job{show: "> " + file, prep: func(c *exec.Cmd) (func(error) error, error) {
		var f *os.File
		var zw *gzip.Writer
		if viaStdout {
			var err error
			if f, err = os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600); err != nil {
				return nil, err
			}
			c.Stdout = f
			if gz {
				zw = gzip.NewWriter(f)
				c.Stdout = zw
			}
		} else {
			// The tool writes the .part itself (pg_dump -f, mongodump --archive, redis-cli --rdb) and would create
			// it with the umask (0644): create it 0600 first, the tool truncates and keeps the mode.
			pf, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
			if err != nil {
				return nil, err
			}
			pf.Close()
		}
		return func(err error) error {
			if zw != nil {
				if e := zw.Close(); err == nil {
					err = e
				}
			}
			if f != nil {
				if e := f.Close(); err == nil {
					err = e
				}
			}
			if err == nil {
				err = os.Chmod(part, 0o600) // a dump is as sensitive as the data, whichever way it was written
			}
			if err == nil {
				err = os.Rename(part, file)
			}
			if err != nil {
				os.Remove(part)
			}
			return err
		}, nil
	}})
}

// load feeds cmd's stdin from file, gunzipped when gz.
func load(cmd *exec.Cmd, file string, gz bool) *exec.Cmd {
	return attach(cmd, &job{show: "< " + file, prep: func(c *exec.Cmd) (func(error) error, error) {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		c.Stdin = f
		if gz {
			zr, err := gzip.NewReader(f)
			if err != nil {
				f.Close()
				return nil, err
			}
			c.Stdin = zr
		}
		return func(err error) error { f.Close(); return err }, nil
	}})
}

func plain(o Options, env []string, name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	return cmd
}

var sslModes = map[string]string{"disable": "DISABLED", "prefer": "PREFERRED", "require": "REQUIRED",
	"verify-ca": "VERIFY_CA", "verify-full": "VERIFY_IDENTITY", "verify-identity": "VERIFY_IDENTITY"}

// OutputPath is the file Plan reads (restore) or writes (backup) once Filename is applied.
func OutputPath(o Options) string {
	if o.Filename == "" {
		return o.File
	}
	name := o.Filename
	if strings.Contains(name, "{") {
		name = export.Filename(name, map[string]string{"connection": o.Conn.Name, "database": o.db(), "table": o.db(), "tag": o.Tag})
	}
	name = filepath.Base(name)
	if o.File == "" {
		return name
	}
	return filepath.Join(filepath.Dir(o.File), name)
}

// Plan builds the command; the caller runs it with Run.
func Plan(o Options, restore bool) (*exec.Cmd, error) {
	extra, err := checkExtra(o.Conn.Driver, restore, o.Extra)
	if err != nil {
		return nil, err
	}
	o.File = OutputPath(o)
	if restore && o.Conn.Driver == config.Postgres && o.Format != "custom" && pgRestoreTool(o.File, "") == "pg_restore" {
		o.Format = "custom" // a .dump / .tar archive or a directory: psql cannot read it
	}
	if restore && o.Conn.Driver == config.Postgres && o.Format != "custom" { // plain SQL goes to psql, which takes few of pg_restore's flags
		if err := checkPsqlExtra(extra); err != nil {
			return nil, err
		}
	}
	cmd, err := plan(o, restore)
	if err != nil || len(extra) == 0 {
		return cmd, err
	}
	psql := restore && o.Conn.Driver == config.Postgres && o.Format != "custom"
	at := len(cmd.Args)
	if psql {
		for i, a := range cmd.Args {
			if a == "-f" {
				at = i
				break
			}
		}
	}
	for i, a := range cmd.Args {
		if a == "--" {
			at = i
			break
		}
	}
	if at == len(cmd.Args) && restore && o.Conn.Driver == config.Postgres && o.Format == "custom" {
		at-- // pg_restore ends with the archive file
	}
	args := append(append(append([]string(nil), cmd.Args[:at]...), extra...), cmd.Args[at:]...)
	cmd.Args = args
	return cmd, nil
}

func plan(o Options, restore bool) (*exec.Cmd, error) {
	if restore && o.NewDatabase != "" {
		o.Database = o.NewDatabase
	}
	c, d := o.Conn, o.db()
	if o.File == "" && c.Driver != config.ClickHouse {
		return nil, errors.New("backup needs a file")
	}
	if strings.HasPrefix(o.File, "-") {
		return nil, errors.New("file name must not start with -")
	}
	if o.DataOnly && o.SchemaOnly {
		return nil, errors.New("data-only and schema-only exclude each other")
	}
	tool := ToolForConn(c, restore)
	if tool.Name == "" {
		return nil, fmt.Errorf("backup is not supported for %s", c.Driver)
	}
	if !tool.Found {
		return nil, fmt.Errorf("%s not found (on PATH or at its configured path)", tool.Name)
	}
	switch c.Driver {
	case config.Postgres:
		env := []string{"PGPASSWORD=" + c.Password, "PGDATABASE=" + d} // env, not -d: -d parses "host=..." as a conninfo
		if m := c.SSLMode; m != "" {
			env = append(env, "PGSSLMODE="+strings.Replace(m, "verify-identity", "verify-full", 1))
		}
		conn := []string{"-h", c.Host, "-p", o.port(), "-U", c.User}
		scope := map[bool][]string{true: {"-a"}, false: nil}[o.DataOnly]
		if o.SchemaOnly {
			scope = []string{"-s"}
		}
		if !restore {
			f := "p"
			if o.Format == "custom" {
				f = "c"
			}
			args := append(append(conn, scope...), "-F", f, "-f", o.File+".part")
			if o.Gzip && f == "p" {
				args = append(args, "-Z", "6") // plain-format compression needs pg_dump 16+
			}
			return dump(plain(o, env, tool.Path, args...), o.File, false, false), nil
		}
		if o.Format == "custom" {
			conn = append(conn, "-d", "dbname='"+strings.NewReplacer(`\`, `\\`, "'", `\'`).Replace(d)+"'") // pg_restore without -d prints the script instead of restoring
			return plain(o, env, tool.Path, append(append(conn, scope...), o.File)...), nil
		}
		psql := c.Tools["psql"]
		if psql == "" {
			var err error
			if psql, err = exec.LookPath("psql"); err != nil {
				return nil, errors.New("psql not found on PATH")
			}
		}
		args := []string{"-h", c.Host, "-p", o.port(), "-U", c.User, "-v", "ON_ERROR_STOP=1"}
		if o.Gzip {
			return load(plain(o, env, psql, args...), o.File, true), nil
		}
		return plain(o, env, psql, append(args, "-f", o.File)...), nil
	case config.MySQL, config.MariaDB:
		env := []string{"MYSQL_PWD=" + c.Password}
		args := []string{"-h", c.Host, "-P", o.port(), "-u", c.User}
		if m := c.SSLMode; m != "" {
			v, ok := sslModes[m]
			if !ok {
				return nil, fmt.Errorf("unsupported ssl_mode %q", m)
			}
			args = append(args, "--ssl-mode="+v)
		}
		if !restore {
			if o.DataOnly {
				args = append(args, "--no-create-info")
			}
			if o.SchemaOnly {
				args = append(args, "--no-data")
			}
			args = append(args, "--single-transaction", "--routines", "--events", "--", d)
			return dump(plain(o, env, tool.Path, args...), o.File, true, o.Gzip), nil
		}
		// shortcut: data-only/schema-only cannot filter a restore; they are ignored here.
		return load(plain(o, env, tool.Path, append(args, "--", d)...), o.File, o.Gzip), nil
	case config.Mongo:
		u := url.URL{Scheme: "mongodb", Host: c.Host + ":" + o.port(), Path: "/" + d}
		if c.User != "" {
			u.User = url.User(c.User)
		}
		if as := c.Options["authSource"]; as != "" {
			u.RawQuery = url.Values{"authSource": {as}}.Encode()
		}
		args := []string{"--uri=" + u.String()}
		if restore {
			args = append(args, "--archive="+o.File)
		} else {
			args = append(args, "--archive="+o.File+".part")
		}
		if o.Gzip {
			args = append(args, "--gzip")
		}
		cmd := plain(o, nil, tool.Path, args...)
		cfgJob := func(inner *job) *job { // password via a 0600 --config file, not argv
			return &job{show: inner.show, prep: func(cm *exec.Cmd) (func(error) error, error) {
				fin := func(err error) error { return err }
				if inner.prep != nil {
					var err error
					if fin, err = inner.prep(cm); err != nil {
						return nil, err
					}
				}
				if c.Password == "" {
					return fin, nil
				}
				f, err := os.CreateTemp("", "webtuidb-mongo-*.yaml") // CreateTemp is 0600
				if err != nil {
					return nil, err
				}
				_, err = f.WriteString("password: " + strconv.Quote(c.Password) + "\n")
				if e := f.Close(); err == nil {
					err = e
				}
				if err != nil {
					os.Remove(f.Name())
					return nil, err
				}
				cm.Args = append(cm.Args, "--config="+f.Name())
				return func(err error) error { os.Remove(f.Name()); return fin(err) }, nil
			}}
		}
		if restore {
			return attach(cmd, cfgJob(&job{})), nil
		}
		dump(cmd, o.File, false, false)
		jobsMu.Lock()
		jobs[cmd] = cfgJob(jobs[cmd])
		jobsMu.Unlock()
		return cmd, nil
	case config.Redis:
		if restore {
			return nil, errors.New("redis restore: stop the server and copy the RDB file into its data directory")
		}
		args := []string{"-h", c.Host, "-p", o.port()}
		if c.User != "" {
			args = append(args, "--user", c.User)
		}
		if c.SSLMode != "" && c.SSLMode != "disable" {
			args = append(args, "--tls")
		}
		return dump(plain(o, []string{"REDISCLI_AUTH=" + c.Password}, tool.Path, append(args, "--rdb", o.File+".part")...), o.File, false, false), nil
	case config.ClickHouse:
		// shortcut: server-side BACKUP/RESTORE to a disk named "backups" in the server config; File is the
		// name on that disk, not a local path. Upgrade to a client-side dump if servers lack the disk.
		verb, prep := "BACKUP", "TO"
		if restore {
			verb, prep = "RESTORE", "FROM"
		}
		q := fmt.Sprintf("%s DATABASE %s %s Disk('backups', %s)", verb, db.ClickHouse.Quote(d), prep, db.ClickHouse.Literal(o.File))
		args := []string{"--host", c.Host, "--port", o.port(), "--query", q}
		if c.User != "" {
			args = append(args, "--user", c.User)
		}
		return plain(o, []string{"CLICKHOUSE_PASSWORD=" + c.Password}, tool.Path, args...), nil
	}
	return nil, fmt.Errorf("backup is not supported for %s", c.Driver)
}

// Run starts cmd and sends each line of its combined output to out; cancelling ctx kills it.
// It finishes the file plumbing Plan attached (gzip, .part rename, temp files).
func Run(ctx context.Context, cmd *exec.Cmd, out func(line string)) error {
	jobsMu.Lock()
	j := jobs[cmd]
	delete(jobs, cmd)
	jobsMu.Unlock()
	if j != nil && j.note != "" {
		out(j.note)
	}
	r, w := io.Pipe()
	cmd.Stdout, cmd.Stderr = w, w
	cmd.WaitDelay = time.Second // closes the pipe even if a grandchild keeps it open
	inOwnGroup(cmd)
	fin := func(err error) error { return err }
	if j != nil && j.prep != nil {
		var err error
		if fin, err = j.prep(cmd); err != nil {
			return err
		}
	}
	if err := cmd.Start(); err != nil {
		return fin(err)
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			killGroup(cmd)
		case <-done:
		}
	}()
	waitErr := make(chan error, 1)
	go func() { err := cmd.Wait(); w.Close(); waitErr <- err }()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		out(sc.Text())
	}
	io.Copy(io.Discard, r) // an over-long line stops the scanner; keep draining so the child is not blocked
	err := <-waitErr
	close(done)
	if ctx.Err() != nil {
		fin(ctx.Err())
		return ctx.Err()
	}
	return fin(err)
}

var uriPass = regexp.MustCompile(`(://[^:/@\s]*):[^@\s]*@`)

// Display is the command line for a confirm dialog; URI passwords are replaced (the rest travel in env).
func Display(cmd *exec.Cmd) string {
	parts := make([]string, len(cmd.Args))
	for i, a := range cmd.Args {
		a = uriPass.ReplaceAllString(a, "$1:***@")
		if a == "" || strings.ContainsAny(a, " \t\"'$<>|;&()") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		parts[i] = a
	}
	jobsMu.Lock()
	defer jobsMu.Unlock()
	if j := jobs[cmd]; j != nil && j.show != "" {
		parts = append(parts, j.show)
	}
	return strings.Join(parts, " ")
}
