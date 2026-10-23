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
		{"pg restore custom", Options{Conn: conn("postgres"), File: "f", Format: "custom", DataOnly: true}, true, "-h h -p 1 -U u -a f"},
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
