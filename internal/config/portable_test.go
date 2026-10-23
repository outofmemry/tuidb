package config

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestExportImportRoundTrip(t *testing.T) {
	cs := []Connection{{Name: "a", Driver: Postgres, Host: "h", Password: "secret", SSH: &SSH{Host: "b", Password: "sp"}}}
	for _, pass := range []string{"", "pw"} {
		var buf bytes.Buffer
		if err := ExportConnections(&buf, cs, pass, true); err != nil {
			t.Fatal(err)
		}
		if pass != "" && bytes.Contains(buf.Bytes(), []byte("secret")) {
			t.Error("encrypted export leaks the password")
		}
		got, _, err := ImportConnectionsOpts(&buf, pass, true)
		if err != nil || len(got) != 1 || got[0].Password != "secret" || got[0].SSH.Password != "sp" {
			t.Fatalf("pass %q: %v %+v", pass, err, got)
		}
	}
	var buf bytes.Buffer
	ExportConnections(&buf, cs, "pw", false)
	if _, err := ImportConnections(bytes.NewReader(buf.Bytes()), "bad"); err == nil {
		t.Error("wrong passphrase accepted")
	}
	got, _ := ImportConnections(&buf, "pw")
	if got[0].Password != "" || got[0].SSH.Password != "" || cs[0].Password != "secret" || cs[0].SSH.Password != "sp" {
		t.Errorf("passwords: %+v (input mutated: %+v)", got[0], cs[0])
	}
}

func TestImportDropsCommandsUnlessAllowed(t *testing.T) {
	cs := []Connection{{Name: "a", Driver: Postgres, Host: "h", PasswordCmd: "sh -c x", Bootstrap: []string{"set x"}, SSH: &SSH{Host: "b", Key: "/k"}}}
	var buf bytes.Buffer
	ExportConnections(&buf, cs, "", true)
	got, dropped, err := ImportConnectionsOpts(bytes.NewReader(buf.Bytes()), "", false)
	if err != nil || got[0].PasswordCmd != "" || got[0].Bootstrap != nil || got[0].SSH.Key != "" || len(dropped) != 3 {
		t.Errorf("%v %+v %v", err, got, dropped)
	}
	got, dropped, _ = ImportConnectionsOpts(&buf, "", true)
	if got[0].PasswordCmd == "" || len(dropped) != 0 {
		t.Errorf("allowCommands: %+v %v", got, dropped)
	}
}

func TestExportKeepsAskAndBlanksOptionPassword(t *testing.T) {
	cs := []Connection{{Name: "a", Driver: Postgres, Host: "h", Password: PasswordAsk, Options: map[string]string{"password": "x"}}}
	var buf bytes.Buffer
	ExportConnections(&buf, cs, "", false)
	got, _, _ := ImportConnectionsOpts(&buf, "", false)
	if got[0].Password != PasswordAsk || got[0].Options["password"] != "" || cs[0].Options["password"] != "x" {
		t.Errorf("%+v", got[0])
	}
}

func TestFormatURLIPv6AndFilePath(t *testing.T) {
	if u := (Connection{Name: "n", Driver: Postgres, Host: "::1", Port: 5432}).FormatURL(); !strings.HasPrefix(u, "postgresql://[::1]:5432/") {
		t.Error(u)
	}
	if u := (Connection{Name: "n", Driver: SQLite, Database: "/tmp/a?b#c.db"}).FormatURL(); !strings.Contains(u, "/tmp/a%3Fb%23c.db?") {
		t.Error(u)
	}
}

func TestURLOptionsOpenTargetAndAsk(t *testing.T) {
	c, err := ParseURL("postgresql://u@h/d?driverVersion=2&lazyload=true&schema=public&table=users&column=id&operation=%3D&value=7&password=ask&sslmode=require")
	if err != nil {
		t.Fatal(err)
	}
	if c.DriverVersion != "2" || !c.LazyLoad || c.Open == nil || *c.Open != (OpenTarget{"public", "users", "id", "=", "7", ""}) || len(c.Options) != 0 {
		t.Errorf("%+v %+v", c, c.Open)
	}
	if _, err := c.ResolvePassword(t.Context()); !errors.Is(err, ErrPasswordAsk) {
		t.Errorf("ask: %v", err)
	}
}

func TestTags(t *testing.T) {
	if m := Tags([]Tag{{"qa", "#fff"}}); m["qa"] != "#fff" || m["local"] != "" || len(m) != len(DefaultTags)+1 {
		t.Errorf("%v", m)
	}
	if ValidateTags([]Tag{{"a", ""}, {"a", ""}}) == nil {
		t.Error("duplicate accepted")
	}
}

func TestParseURLDropsPasswordOption(t *testing.T) {
	c, err := ParseURL("postgresql://u@h/d?password=hunter2")
	if err != nil || c.Options["password"] != "" || c.Password != "" {
		t.Errorf("%v %+v", err, c)
	}
}

func TestSourceFilesAreNotChmodded(t *testing.T) {
	dir := t.TempDir()
	f := dir + "/team.toml"
	os.WriteFile(f, []byte("[[connection]]\nname='a'\ndriver='postgres'\nhost='h'\n"), 0o644)
	if _, err := LoadAllConnections(dir+"/none.toml", []Source{{Path: dir}}); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(f); st.Mode().Perm() != 0o644 {
		t.Errorf("source file mode changed to %v", st.Mode().Perm())
	}
}

func TestURLSSHAndTablePlusOptions(t *testing.T) {
	c, err := ParseURL("postgresql+ssh://u:pw@db/d?ssh_host=bast&ssh_user=me&ssh_key=/k&ssh_port=2222&usePrivateKey=true&tLSMode=require&x=1")
	if err != nil || c.Driver != Postgres || c.SSLMode != "require" ||
		!reflect.DeepEqual(c.SSH, &SSH{Host: "bast", User: "me", Key: "/k", Port: 2222}) || !reflect.DeepEqual(c.Options, map[string]string{"x": "1"}) {
		t.Fatalf("%+v %+v %v", c, c.SSH, err)
	}
	back, err := ParseURL(c.FormatURL())
	if err != nil || !strings.HasPrefix(c.FormatURL(), "postgresql+ssh://") || !reflect.DeepEqual(back.SSH, c.SSH) {
		t.Errorf("round trip: %+v %v", back.SSH, err)
	}
	if c, _ = ParseURL("mysql+ssh://h/d?ssh_host=b&ssh_key=/k&usePrivateKey=false"); c.SSH == nil || c.SSH.Key != "" {
		t.Errorf("password auth keeps no key: %+v", c.SSH)
	}
	if _, err = ParseURL("postgresql+ssh://h/d"); err == nil {
		t.Error("+ssh without ssh_host")
	}
}

func TestToolPathOK(t *testing.T) {
	for p, want := range map[string]bool{"/opt/pg/pg_dump": true, "/opt/pg/pg_dump-16": true, "/opt/pg/pg_dump.exe": true, "/tmp/pg_dump_x": false, "/bin/sh": false,
		"pg_dump": false, "//srv/share/pg_dump": false, "/a/../b/pg_dump": false, "/x/pg_dump-": false} {
		if ToolPathOK("pg_dump", p) != want {
			t.Errorf("%q want %v", p, want)
		}
	}
	if ToolPathOK("mysql", "/usr/bin/mysqld_safe") || ToolPathOK("sh", "/bin/sh") {
		t.Error("prefix or unknown tool accepted")
	}
}

func TestParseURLDropsSSHPassword(t *testing.T) {
	c, err := ParseURL("postgresql+ssh://u@db/d?ssh_host=b&ssh_password=s3&sshPassword=s4&x=1")
	if err != nil || len(c.Options) != 1 || c.Options["x"] != "1" || c.SSH.Password != "" {
		t.Errorf("%v %+v", err, c)
	}
}
