package backup

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/config"
)

// fakeTools puts stub executables on PATH so Plan finds them.
func fakeTools(t *testing.T, names ...string) {
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPlan(t *testing.T) {
	fakeTools(t, "pg_dump", "pg_restore", "psql", "mysqldump", "mysql", "mongodump", "mongorestore", "redis-cli", "clickhouse-client")
	conn := func(d string) config.Connection {
		return config.Connection{Driver: d, Host: "h", Port: 1, User: "u", Password: "s3cr&t", Database: "d"}
	}
	cases := []struct {
		name    string
		o       Options
		restore bool
		want    string // argv after the executable path
	}{
		{"pg plain", Options{Conn: conn("postgres"), File: "f.sql", SchemaOnly: true}, false, "-h h -p 1 -U u -s -F p -f f.sql.part"},
		{"pg custom gz", Options{Conn: conn("postgres"), File: "f", Format: "custom", Gzip: true}, false, "-h h -p 1 -U u -F c -f f.part"},
		{"pg restore custom", Options{Conn: conn("postgres"), File: "f", Format: "custom", DataOnly: true}, true, "-h h -p 1 -U u -d dbname='d' -a f"},
		{"pg restore plain", Options{Conn: conn("postgres"), File: "f.sql"}, true, "-h h -p 1 -U u -v ON_ERROR_STOP=1 -f f.sql"},
		{"mysql", Options{Conn: conn("mysql"), File: "f.sql"}, false, "-h h -P 1 -u u --single-transaction --routines --events -- d"},
		{"mysql gz", Options{Conn: conn("mysql"), File: "f.gz", Gzip: true}, false, "--routines --events -- d"},
		{"mongo", Options{Conn: conn("mongodb"), File: "f", Gzip: true}, false, "--uri=mongodb://u@h:1/d --archive=f.part --gzip"},
		{"redis", Options{Conn: conn("redis"), File: "f.rdb"}, false, "-h h -p 1 --user u --rdb f.rdb.part"},
		{"clickhouse", Options{Conn: conn("clickhouse"), File: "n"}, true, "--host h --port 1 --query RESTORE DATABASE `d` FROM Disk('backups', 'n') --user u"},
	}
	for _, c := range cases {
		cmd, err := Plan(c.o, c.restore)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got := strings.Join(cmd.Args[1:], " ")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: argv %q, want %q", c.name, got, c.want)
		}
		if d := Display(cmd); strings.Contains(d, "s3cr") {
			t.Errorf("%s: password in display %q", c.name, d)
		}
	}
	if _, err := Plan(Options{Conn: conn("redis"), File: "f"}, true); err == nil {
		t.Error("redis restore accepted")
	}
	if _, err := Plan(Options{Conn: conn("sqlite"), File: "f"}, false); err == nil {
		t.Error("sqlite accepted")
	}
	cmd, _ := Plan(Options{Conn: conn("postgres"), File: "f"}, false)
	if !strings.Contains(strings.Join(cmd.Env, "\n"), "PGPASSWORD=s3cr&t") {
		t.Error("password not in env")
	}
}

func TestRun(t *testing.T) {
	var lines []string
	cmd := commandSh("echo one; echo two >&2")
	if err := Run(context.Background(), cmd, func(l string) { lines = append(lines, l) }); err != nil || strings.Join(lines, ",") != "one,two" {
		t.Fatalf("lines %v err %v", lines, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := Run(ctx, commandSh("exec sleep 5"), func(string) {}); err == nil || time.Since(start) > 3*time.Second {
		t.Errorf("cancel: %v after %v", err, time.Since(start))
	}
}

// A failed gzip dump must leave the previous backup alone and no .part; a good one is a valid gzip.
func TestDumpGzipAtomic(t *testing.T) {
	file := filepath.Join(t.TempDir(), "b.sql.gz")
	os.WriteFile(file, []byte("old"), 0o644)
	if err := Run(context.Background(), dump(commandSh("echo hi; exit 3"), file, true, true), func(string) {}); err == nil {
		t.Fatal("failure not reported")
	}
	if b, _ := os.ReadFile(file); string(b) != "old" {
		t.Errorf("old backup clobbered: %q", b)
	}
	if _, err := os.Stat(file + ".part"); err == nil {
		t.Error(".part left behind")
	}
	cmd := dump(commandSh("echo hi"), file, true, true)
	if d := Display(cmd); !strings.HasSuffix(d, "> "+file) {
		t.Errorf("display %q", d)
	}
	if err := Run(context.Background(), cmd, func(string) {}); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(file)
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(zr); string(b) != "hi\n" {
		t.Errorf("content %q", b)
	}
}

func TestPlanGuards(t *testing.T) {
	fakeTools(t, "pg_dump", "mysqldump")
	c := config.Connection{Driver: "postgres", Host: "h", User: "u", Database: "host=evil"}
	if _, err := Plan(Options{Conn: c, File: "-x"}, false); err == nil {
		t.Error("dash file accepted")
	}
	cmd, _ := Plan(Options{Conn: c, File: "f"}, false)
	if strings.Contains(strings.Join(cmd.Args, " "), "evil") || !strings.Contains(strings.Join(cmd.Env, "\n"), "PGDATABASE=host=evil") {
		t.Error("database in argv")
	}
	c = config.Connection{Driver: "mysql", Host: "h", User: "u", Database: "d", SSLMode: "verify-full"}
	cmd, _ = Plan(Options{Conn: c, File: "f"}, false)
	if !strings.Contains(strings.Join(cmd.Args, " "), "--ssl-mode=VERIFY_IDENTITY") {
		t.Error(cmd.Args)
	}
}

func TestPlanToolsOverrideAndNewDatabase(t *testing.T) {
	tool := filepath.Join(t.TempDir(), "my_pg_restore")
	os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", t.TempDir()) // nothing on PATH
	c := config.Connection{Driver: "postgres", Host: "h", Port: 1, User: "u", Database: "d", Tools: map[string]string{"pg_restore": tool}}
	if got := ToolForConn(c, true); !got.Found || got.Path != tool {
		t.Fatalf("%+v", got)
	}
	cmd, err := Plan(Options{Conn: c, File: "f", Format: "custom", NewDatabase: "fresh"}, true)
	if err != nil || cmd.Path != tool || !strings.Contains(strings.Join(cmd.Env, " "), "PGDATABASE=fresh") {
		t.Fatalf("%v %v", err, cmd)
	}
	if _, err := Plan(Options{Conn: c, File: "f"}, false); err == nil {
		t.Fatal("pg_dump is not configured and not on PATH")
	}
}

func TestExtraFilenameCommonOptions(t *testing.T) {
	fakeTools(t, "pg_dump", "pg_restore", "mysqldump", "mysql")
	conn := func(d string) config.Connection {
		return config.Connection{Name: "prod", Driver: d, Host: "h", Port: 1, User: "u", Password: "x", Database: "d"}
	}
	cmd, err := Plan(Options{Conn: conn("postgres"), File: "out/f", Extra: []string{"--no-owner", "--inserts"}}, false)
	if err != nil || !strings.HasSuffix(strings.Join(cmd.Args, " "), "-f out/f.part") && !strings.Contains(strings.Join(cmd.Args, " "), "--no-owner --inserts") {
		t.Fatalf("%v %v", cmd, err)
	}
	cmd, _ = Plan(Options{Conn: conn("mysql"), File: "f", Extra: []string{"--no-data"}}, false)
	a := cmd.Args
	if a[len(a)-1] != "d" || a[len(a)-2] != "--" || a[len(a)-3] != "--no-data" {
		t.Errorf("extra must precede --: %v", a)
	}
	cmd, _ = Plan(Options{Conn: conn("postgres"), File: "f", Format: "custom", Extra: []string{"--clean"}}, true)
	if a := cmd.Args; a[len(a)-1] != "f" || a[len(a)-2] != "--clean" {
		t.Errorf("pg_restore keeps file last: %v", a)
	}
	for _, bad := range []string{"-f", "--file=x", "-h", "-hfoo", "-pSECRET", "--password", "-o x", "--out=/tmp/x"} {
		if _, err := Plan(Options{Conn: conn("postgres"), File: "f", Extra: []string{bad}}, false); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	cmd, _ = Plan(Options{Conn: conn("postgres"), File: "dir/old.sql", Filename: "{connection}-{database}.sql"}, false)
	if !strings.HasSuffix(cmd.Args[len(cmd.Args)-1], "dir/prod-d.sql.part") {
		t.Errorf("filename: %v", cmd.Args)
	}
	cmd, _ = Plan(Options{Conn: conn("postgres"), Filename: "../x.sql"}, false)
	if !strings.HasSuffix(cmd.Args[len(cmd.Args)-1], "x.sql.part") || strings.Contains(cmd.Args[len(cmd.Args)-1], "..") {
		t.Errorf("filename escaped: %v", cmd.Args)
	}
	if len(CommonOptions("postgres", false)) != 5 || len(CommonOptions("postgres", true)) != 4 || len(CommonOptions("redis", false)) != 0 ||
		CommonOptions("mysql", true)[0].Flag != "--force" || len(CommonOptions("mongodb", true)) != 3 {
		t.Error("common options")
	}
}

// Cancel must take the whole process group down, not only the shell.
func TestRunCancelKillsChildren(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := Run(ctx, commandSh("sleep 30 & echo started; wait"), func(string) {})
	if err == nil || time.Since(start) > 3*time.Second {
		t.Errorf("cancel: %v after %v", err, time.Since(start))
	}
}

func mkConn(d string) config.Connection {
	return config.Connection{Name: "prod", Driver: d, Host: "h", Port: 1, User: "u", Password: "x", Database: "d"}
}

// R-001: Extra is an exact allow-list; nothing can leave the backups folder, change host/db or run a command.
func TestExtraAllowList(t *testing.T) {
	fakeTools(t, "pg_dump", "pg_restore", "mysqldump", "mysql", "mongodump", "mongorestore", "psql")
	bad := []string{"-c", "-d", "--dbname=host=evil", "-L", "-r", "-T", "--command=x", "--log-file=x", "--defaults-file=x", "--tab=/x", "--result-file=/x",
		"--fil=x", "--hos=evil", "-f", "-h", "--clean=1", "-j", "-j x", "--jobs=x", "--jobs=-1", "x", "--no-owner x"}
	for _, drv := range []string{"postgres", "mysql", "mongodb"} {
		for _, restore := range []bool{false, true} {
			for _, b := range bad {
				if _, err := Plan(Options{Conn: mkConn(drv), File: "f", Extra: []string{b}}, restore); err == nil {
					t.Errorf("%s restore=%v: %q accepted", drv, restore, b)
				}
			}
		}
	}
	if _, err := Plan(Options{Conn: mkConn("postgres"), File: "f", Format: "custom", Extra: []string{"-j", "4", "--jobs=2"}}, true); err != nil {
		t.Errorf("-j N on pg_restore: %v", err)
	}
	if _, err := Plan(Options{Conn: mkConn("postgres"), File: "f", Extra: []string{"-j 4"}}, false); err == nil {
		t.Error("-j on pg_dump accepted")
	}
	for _, drv := range []string{"postgres", "mysql", "mongodb"} {
		for _, restore := range []bool{false, true} {
			for _, o := range CommonOptions(drv, restore) {
				if _, err := Plan(Options{Conn: mkConn(drv), File: "f", Format: "custom", Extra: []string{o.Flag}}, restore); err != nil {
					t.Errorf("%s: common option %q refused: %v", drv, o.Flag, err)
				}
			}
		}
	}
}

// R-009: a custom-format restore names its target database.
func TestPgRestoreHasDatabase(t *testing.T) {
	fakeTools(t, "pg_restore")
	c := mkConn("postgres")
	c.Database = "it's"
	cmd, err := Plan(Options{Conn: c, File: "f", Format: "custom"}, true)
	if err != nil {
		t.Fatal(err)
	}
	a := strings.Join(cmd.Args, " ")
	if !strings.Contains(a, `-d dbname='it\'s'`) {
		t.Errorf("no -d: %v", cmd.Args)
	}
}

// R-013: OutputPath is the file Plan writes, and {tag} is expanded.
func TestOutputPath(t *testing.T) {
	fakeTools(t, "pg_dump")
	o := Options{Conn: mkConn("postgres"), File: "dir/old.sql", Filename: "{connection}-{database}-{tag}.sql", Tag: "prod"}
	p := OutputPath(o)
	if !strings.HasSuffix(p, "dir/prod-d-prod.sql") && !strings.HasSuffix(p, "dir/prod-d-prod.sql") {
		t.Errorf("OutputPath %q", p)
	}
	cmd, _ := Plan(o, false)
	if !strings.HasSuffix(cmd.Args[len(cmd.Args)-1], p+".part") {
		t.Errorf("Plan writes %v, OutputPath %q", cmd.Args, p)
	}
}

// P2-09 / P2-08: restore flags follow the tool that runs.
func TestRestoreFlagsFollowTool(t *testing.T) {
	fakeTools(t, "pg_restore", "psql", "mongorestore", "mysql")
	hasOplog := func(o []Option, flag string) bool {
		for _, x := range o {
			if x.Flag == flag {
				return true
			}
		}
		return false
	}
	if !hasOplog(CommonOptions("mongodb", true), "--oplogReplay") || hasOplog(CommonOptions("mongodb", true), "--oplog") {
		t.Error("mongorestore takes --oplogReplay, not --oplog")
	}
	if _, err := Plan(Options{Conn: mkConn("mongodb"), File: "f", Extra: []string{"--oplogReplay"}}, true); err != nil {
		t.Errorf("--oplogReplay refused: %v", err)
	}
	if _, err := Plan(Options{Conn: mkConn("mysql"), File: "f", Extra: []string{"--single-transaction"}}, true); err == nil {
		t.Error("mysqldump-only flag accepted for the mysql client")
	}
	// plain SQL restore runs psql: pg_restore-only flags are dropped, with a line saying so
	cmd, err := Plan(Options{Conn: mkConn("postgres"), File: "f.sql", Extra: []string{"--clean", "-j", "4", "--single-transaction"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(cmd.Args, " ")
	if strings.Contains(line, "--clean") || strings.Contains(line, "-j") || !strings.Contains(line, "--single-transaction") {
		t.Errorf("psql args: %s", line)
	}
	var out []string
	_ = Run(context.Background(), cmd, func(l string) { out = append(out, l) })
	if len(out) == 0 || !strings.Contains(out[0], "--clean") || !strings.Contains(out[0], "-j 4") {
		t.Errorf("no dropped-flags note: %v", out)
	}
	// custom restore keeps them
	cmd, err = Plan(Options{Conn: mkConn("postgres"), File: "f.dump", Format: "custom", Extra: []string{"--clean"}}, true)
	if err != nil || !strings.Contains(strings.Join(cmd.Args, " "), "--clean") {
		t.Errorf("pg_restore: %v %v", cmd, err)
	}
}

// P2-10: a dump the tool writes itself ends up 0600, not umask-readable.
func TestToolWrittenDumpIs0600(t *testing.T) {
	dir := t.TempDir()
	bin := t.TempDir()
	script := "#!/bin/sh\nwhile [ $# -gt 0 ]; do if [ \"$1\" = -f ]; then f=$2; fi; shift; done\necho data > \"$f\"\nchmod 644 \"$f\" 2>/dev/null; exit 0\n"
	// the stub re-chmods only to prove the final chmod, not luck, sets the mode
	os.WriteFile(filepath.Join(bin, "pg_dump"), []byte(script), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	file := filepath.Join(dir, "out.sql")
	cmd, err := Plan(Options{Conn: mkConn("postgres"), File: file}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), cmd, func(string) {}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(file)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v err %v", st.Mode(), err)
	}
}
