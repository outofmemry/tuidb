package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil || c.General.Prefix != "C-d" || c.General.RowLimit != 300 {
		t.Fatalf("%+v %v", c.General, err)
	}
}

func TestLoadOverridesAndKeys(t *testing.T) {
	c, err := Load(write(t, "[general]\nprefix = \"C-a\"\nrow_limit = 50\n[keys]\ncommit = \"x\"\nrun_all = [\"M-Enter\", \"-n f5\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.General.Prefix != "C-a" || c.General.RowLimit != 50 || !c.General.KeepAlive {
		t.Errorf("merge broke defaults: %+v", c.General)
	}
	m, err := c.Keymap()
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := m.Lookup("grid", "x"); a != "commit" {
		t.Errorf("x -> %q", a)
	}
}

func TestLoadErrors(t *testing.T) {
	for name, body := range map[string]string{
		"unknown key":   "[general]\nbogus = 1\n",
		"bad safe mode": "[general]\ndefault_safe_mode = \"loud\"\n",
		"bad action":    "[keys]\nnope = \"x\"\n",
		"bad key type":  "[keys]\ncommit = 5\n",
		"zero limit":    "[general]\nrow_limit = 0\n",
	} {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestConnectionsRoundTripAndPerms(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "connections.toml")
	in := []Connection{{Name: "a", Driver: Postgres, Host: "h", Password: "pw", SSH: &SSH{Host: "j", User: "u"}}, {Name: "b", Driver: Redis, Host: "r", Group: "x/y"}}
	if err := SaveConnections(p, in); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("perms %v", st.Mode().Perm())
	}
	out, err := LoadConnections(p)
	if err != nil || len(out) != 2 || out[0].Password != "pw" || out[0].SSH == nil || out[1].Group != "x/y" {
		t.Fatalf("%+v %v", out, err)
	}
	if g := Groups(out); len(g) != 2 || g[0] != "x" || g[1] != "x/y" {
		t.Errorf("groups %v", g)
	}
}

func TestConnectionsRejectBad(t *testing.T) {
	if _, err := LoadConnections(write(t, "[[connection]]\nname='a'\ndriver='oracle'\n")); err == nil {
		t.Error("unknown driver")
	}
	if _, err := LoadConnections(write(t, "[[connection]]\nname='a'\ndriver='redis'\n[[connection]]\nname='a'\ndriver='redis'\n")); err == nil {
		t.Error("duplicate names")
	}
}

func TestURL(t *testing.T) {
	c, err := ParseURL("postgresql://bob:s%40cret@db.example.com:6543/acme?name=Prod&env=production&statusColor=c0392b")
	if err != nil {
		t.Fatal(err)
	}
	if c.Driver != Postgres || c.User != "bob" || c.Password != "s@cret" || c.Port != 6543 || c.Database != "acme" || c.Name != "Prod" || c.Tag != "production" || c.Color != "#c0392b" {
		t.Fatalf("%+v", c)
	}
	back, err := ParseURL(c.FormatURL())
	if err != nil || !reflect.DeepEqual(back, c) {
		t.Errorf("round trip: %+v vs %+v (%v)", back, c, err)
	}
	if _, err := ParseURL("oracle://x"); err == nil {
		t.Error("unsupported scheme")
	}
	if c, _ := ParseURL("redis://localhost"); c.Name != "localhost" || c.EffectivePort() != 6379 {
		t.Errorf("%+v", c)
	}
}

func TestPasswordCmd(t *testing.T) {
	c := Connection{Name: "a", Driver: Redis, PasswordCmd: "printf 'hunter2\\n'"}
	if pw, err := c.ResolvePassword(context.Background()); err != nil || pw != "hunter2" {
		t.Errorf("%q %v", pw, err)
	}
	c.PasswordCmd = "exit 3"
	if _, err := c.ResolvePassword(context.Background()); err == nil {
		t.Error("failing cmd must error")
	}
}

func TestURLOptionsRoundTrip(t *testing.T) {
	c, err := ParseURL("mongodb://u:p@h:27017/d?authSource=admin&name=M")
	if err != nil || c.Options["authSource"] != "admin" || c.Name != "M" {
		t.Fatalf("%+v %v", c, err)
	}
	back, err := ParseURL(c.FormatURL())
	if err != nil || !reflect.DeepEqual(back, c) {
		t.Errorf("round trip %+v vs %+v (%v)", back, c, err)
	}
}

func TestURLTLSSettingsAreNeverDroppedSilently(t *testing.T) {
	for url, want := range map[string]string{
		"postgresql://u@h/d?sslmode=require":        "require",
		"postgresql://u@h/d?sslmode=verify-full":    "verify-full",
		"mysql://u@h/d?ssl-mode=REQUIRED":           "require",
		"mysql://u@h/d?ssl-mode=VERIFY_IDENTITY":    "verify-identity",
		"mysql://u@h/d?tls=true":                    "require",
		"clickhouse://u@h/d?secure=true":            "require",
		"mongodb://u@h/d?tls=true&authSource=admin": "require",
		"mongodb://u@h/d?ssl=true":                  "require",
		"redis://h/0":                               "",
		"rediss://h/0":                              "require",
		"postgresql://u@h/d":                        "",
	} {
		c, err := ParseURL(url)
		if err != nil || c.SSLMode != want {
			t.Errorf("%s: SSLMode %q (err %v), want %q", url, c.SSLMode, err, want)
		}
		for _, k := range []string{"sslmode", "ssl-mode", "tls", "ssl", "secure"} {
			if _, kept := c.Options[k]; kept {
				t.Errorf("%s: %q must be consumed, not left as an ignored option", url, k)
			}
		}
	}
	c, _ := ParseURL("mongodb://u@h/d?tls=true&authSource=admin")
	if c.Options["authSource"] != "admin" {
		t.Errorf("unrelated options survive: %+v", c.Options)
	}
	back, err := ParseURL(c.FormatURL())
	if err != nil || back.SSLMode != "require" || !reflect.DeepEqual(back, c) {
		t.Errorf("Copy as URL must keep the TLS mode: %+v vs %+v (%v)", back, c, err)
	}
	if _, err := ParseURL("postgresql://u@h/d?sslmode=bogus"); err == nil {
		t.Error("an unknown ssl mode must be an error, not plaintext")
	}
	if _, err := ParseURL("mongodb+srv://u@cluster.example.com/d"); err == nil {
		t.Error("mongodb+srv is unsupported and must say so instead of parsing as a plain host")
	}
}

func TestConnectionsValidateSafetySettings(t *testing.T) {
	for name, body := range map[string]string{
		"safe_mode typo": "[[connection]]\nname='a'\ndriver='postgres'\nsafe_mode='Safe2'\n",
		"ssl_mode typo":  "[[connection]]\nname='a'\ndriver='postgres'\nssl_mode='verifyfull'\n",
		"misspelled key": "[[connection]]\nname='a'\ndriver='postgres'\nsslMode='require'\n",
		"bad ssh port":   "[[connection]]\nname='a'\ndriver='postgres'\n[connection.ssh]\nhost='h'\nport=99999\n",
	} {
		if _, err := LoadConnections(write(t, body)); err == nil {
			t.Errorf("%s: must be rejected, not silently ignored", name)
		}
	}
	if _, err := LoadConnections(write(t, "[[connection]]\nname='a'\ndriver='mongodb'\n[connection.options]\nauthSource='admin'\n")); err != nil {
		t.Errorf("options sub-table is fine: %v", err)
	}
}

func TestLoadConnectionsFixesWorldReadablePermissions(t *testing.T) {
	p := write(t, "[[connection]]\nname='a'\ndriver='redis'\npassword='pw'\n")
	os.Chmod(p, 0o644)
	if _, err := LoadConnections(p); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("a file with passwords must be made owner-only, got %v", st.Mode().Perm())
	}
}

func TestPasswordCmdRunsWithoutAShell(t *testing.T) {
	c := Connection{Name: "a", Driver: Redis, PasswordCmd: `printf 'a b'`}
	if pw, err := c.ResolvePassword(context.Background()); err != nil || pw != "a b" {
		t.Errorf("quoted argument: %q %v", pw, err)
	}
	marker := filepath.Join(t.TempDir(), "pwned")
	c.PasswordCmd = "echo x; touch " + marker
	c.ResolvePassword(context.Background())
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("shell metacharacters must not be interpreted")
	}
	c.PasswordCmd = `echo "unbalanced`
	if _, err := c.ResolvePassword(context.Background()); err == nil {
		t.Error("unbalanced quote is an error")
	}
	if argv, _ := splitCommand(`op read "op://Private/my item/password" --no-newline`); len(argv) != 4 || argv[2] != "op://Private/my item/password" {
		t.Errorf("argv %q", argv)
	}
}

func TestWriteFileAtomicKeepsOldFileOnFailure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := WriteFileAtomic(p, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(p, []byte("two")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "two" {
		t.Errorf("%q", b)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("perm %v", st.Mode().Perm())
	}
	if entries, _ := os.ReadDir(filepath.Dir(p)); len(entries) != 1 {
		t.Errorf("temp files left behind: %d entries", len(entries))
	}
}

func TestUnimplementedSettingsAreRejected(t *testing.T) {
	for _, body := range []string{"[editor]\nwrap = true\n", "[sidebar]\nlazy_load = true\n", "[editor]\nquery_params = true\n"} {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("%q is not implemented and must be reported, not ignored", body)
		}
	}
}

func TestSaveConnectionsKeepsOtherProcessAdditions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "connections.toml")
	mk := func(n string) Connection { return Connection{Name: n, Driver: SQLite, Database: n + ".db"} }
	if err := SaveConnections(p, []Connection{mk("a"), mk("b")}); err != nil {
		t.Fatal(err)
	}
	mine, _ := LoadConnections(p)
	b, _ := os.ReadFile(p) // the other process adds c behind our back
	os.WriteFile(p, append(b, "\n[[connection]]\nname = \"c\"\ndriver = \"sqlite\"\ndatabase = \"c.db\"\n"...), 0o600)
	if err := SaveConnections(p, mine[1:]); err != nil { // we delete a, never having seen c
		t.Fatal(err)
	}
	got, _ := LoadConnections(p)
	if len(got) != 2 || got[0].Name != "b" || got[1].Name != "c" {
		t.Errorf("want b,c (a deleted, c kept): %+v", got)
	}
}

func TestParseURLErrorDoesNotLeakPassword(t *testing.T) {
	_, err := ParseURL("postgresql://bob:p@ss%word@db/x")
	if err == nil || strings.Contains(err.Error(), "ss%wo") || strings.Contains(err.Error(), "bob") {
		t.Errorf("error must not echo the URL: %v", err)
	}
}

func TestURLTLSVariantsAndUnknownValues(t *testing.T) {
	if c, err := ParseURL("mysql://u@h/d?ssl-mode=verify-identity"); err != nil || c.SSLMode != "verify-identity" {
		t.Errorf("%+v %v", c, err)
	}
	if c, err := ParseURL("mongodb://u@h/d?tls=TRUE"); err != nil || c.SSLMode != "require" {
		t.Errorf("%+v %v", c, err)
	}
	for _, u := range []string{"mysql://u@h/d?ssl-mode=VERIFY-FULL", "clickhouse://u@h/d?secure=maybe"} {
		if _, err := ParseURL(u); err == nil {
			t.Errorf("%s must be rejected, not downgraded", u)
		}
	}
}

func TestEnumAndUnimplementedSettingsRejected(t *testing.T) {
	for _, body := range []string{"[editor]\nindent = \"tab\"\n", "[editor]\ncomplete_key = \"ente\"\n", "[sidebar]\nmatching = \"fuzzyy\"\n",
		"[general]\nlog_original_values = true\n", "[csv]\ndelimiter = \";\"\n"} {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("%q must be rejected", body)
		}
	}
}

func TestRelativeXDGIsIgnored(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "rel")
	if !filepath.IsAbs(ConfigDir()) {
		t.Skip("no home directory in this environment")
	}
	if strings.HasPrefix(ConfigDir(), "rel") {
		t.Error("relative XDG_CONFIG_HOME must be ignored")
	}
}

func TestMigrateLegacyDirs(t *testing.T) {
	cfg, data := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_DATA_HOME", data)
	if err := os.MkdirAll(filepath.Join(cfg, "tuidb"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "tuidb", "connections.toml"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(data, "tuidb", "history"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := MigrateLegacy(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join(cfg, "webtuidb", "connections.toml"),
		filepath.Join(data, "webtuidb", "history"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s must exist after the migration: %v", p, err)
		}
	}
	for _, p := range []string{filepath.Join(cfg, "tuidb"), filepath.Join(data, "tuidb")} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s must be gone after the migration", p)
		}
	}

	if err := MigrateLegacy(); err != nil { // a second run has nothing to do and must not fail
		t.Fatal(err)
	}
}

func TestMigrateLegacyKeepsNewDir(t *testing.T) {
	cfg, data := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_DATA_HOME", data)
	for _, d := range []string{"tuidb", "webtuidb"} {
		if err := os.MkdirAll(filepath.Join(cfg, d), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfg, d, "config.toml"), []byte(d), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := MigrateLegacy(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(cfg, "webtuidb", "config.toml"))
	if err != nil || string(got) != "webtuidb" {
		t.Errorf("existing webtuidb config must win, got %q (%v)", got, err)
	}
	if _, err := os.Stat(filepath.Join(cfg, "tuidb", "config.toml")); err != nil {
		t.Errorf("legacy dir must be left untouched when webtuidb exists: %v", err)
	}
}
