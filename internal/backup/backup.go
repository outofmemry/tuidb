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

// Plan builds the command; the caller runs it with Run.
func Plan(o Options, restore bool) (*exec.Cmd, error) {
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
	r, w := io.Pipe()
	cmd.Stdout, cmd.Stderr = w, w
	cmd.WaitDelay = time.Second // closes the pipe even if a grandchild keeps it open
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
			_ = cmd.Process.Kill()
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
