package config

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
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
