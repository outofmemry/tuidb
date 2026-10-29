package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyDriverLoadsAsUnusable(t *testing.T) {
	p := filepath.Join(t.TempDir(), "connections.toml")
	os.WriteFile(p, []byte("[[connection]]\nname='old'\ndriver='sqlite'\ndatabase='/x.db'\n[[connection]]\nname='pg'\ndriver='postgres'\nhost='h'\n"), 0o600)
	cs, err := LoadConnections(p)
	if err != nil || len(cs) != 2 || cs[0].Unusable == "" || cs[1].Unusable != "" {
		t.Fatalf("%v %+v", err, cs)
	}
}

func TestSaveConnectionsRefusesCorruptDisk(t *testing.T) {
	p := filepath.Join(t.TempDir(), "connections.toml")
	if err := SaveConnections(p, []Connection{{Name: "a", Driver: Postgres, Host: "h"}}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p, []byte("[[connection\n"), 0o600)
	if err := SaveConnections(p, []Connection{{Name: "a", Driver: Postgres, Host: "h"}}); err == nil {
		t.Fatal("overwrote an unparsable file")
	}
}

func TestAIProviderValidated(t *testing.T) {
	a := AI{Provider: "anthropic", Providers: map[string]AIProvider{"x": {Provider: "opneai"}}}
	if _, err := AIFor(a, "x"); err == nil {
		t.Fatal("typo accepted")
	}
	c := Defaults()
	c.AI = a
	if err := c.validate(); err == nil {
		t.Fatal("validate accepted a typo")
	}
}

func TestLoadChmodsConfigWithSecrets(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("[web]\npassword='s'\n"), 0o644)
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm()&0o077 != 0 {
		t.Fatalf("mode %v", st.Mode())
	}
}

func TestImportDropsToolsWithoutAllowCommands(t *testing.T) {
	_, dropped, err := ImportConnectionsOpts(strings.NewReader("[[connection]]\nname='a'\ndriver='postgres'\nhost='h'\n[connection.tools]\npg_dump='/tmp/evil/pg_dump'\n"), "", false)
	if err != nil || len(dropped) == 0 || !strings.Contains(dropped[0], "tools") {
		t.Fatalf("%v %v", err, dropped)
	}
}

func TestWriteFileAtomicEmptyPath(t *testing.T) {
	if WriteFileAtomic("", []byte("x")) == nil {
		t.Fatal("empty path accepted")
	}
}

func TestCSVAndExportFilenameValidation(t *testing.T) {
	for _, c := range []CSV{{Delimiter: "||"}, {Quote: "ab"}, {Quote: ","}, {Delimiter: ";", Quote: ";"}, {Quote: "\n"}, {Quote: "é"}} {
		if c.validate() == nil {
			t.Errorf("%+v must be rejected", c)
		}
	}
	if err := (CSV{Delimiter: ";", Quote: "'"}).validate(); err != nil {
		t.Error(err)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[export]\nfilename = \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil || c.Export.Filename != Defaults().Export.Filename {
		t.Errorf("empty filename: %v %q", err, c.Export.Filename)
	}
}

// DynamoDB's host may be a bare host or carry one http(s):// scheme; anything else is refused when the
// connection is validated, not later as "Post http://http://...: no such host".
func TestDynamoDBHostIsValidatedAndSplit(t *testing.T) {
	for _, c := range []struct{ host, scheme, name, port string }{
		{"localhost", "", "localhost", ""},
		{"localhost:8000", "", "localhost", "8000"},
		{"http://localhost:8000", "http", "localhost", "8000"},
		{"HTTPS://db.example.com/", "https", "db.example.com", ""},
		{"::1", "", "::1", ""},
		{"http://[::1]:8000", "http", "::1", "8000"},
		{"", "", "", ""},
	} {
		s, h, p, err := SplitEndpointHost(c.host)
		if err != nil || s != c.scheme || h != c.name || p != c.port {
			t.Errorf("%q = %q %q %q %v, want %q %q %q", c.host, s, h, p, err, c.scheme, c.name, c.port)
		}
		if err := (Connection{Name: "d", Driver: DynamoDB, Host: c.host}).Validate(); err != nil {
			t.Errorf("%q refused: %v", c.host, err)
		}
	}
	for _, bad := range []string{"http://http://localhost:8000", "ftp://x", "http://x/tables", "http://u:p@x", "http://", "a b"} {
		if _, _, _, err := SplitEndpointHost(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
		if err := (Connection{Name: "d", Driver: DynamoDB, Host: bad}).Validate(); err == nil || !strings.Contains(err.Error(), "host") {
			t.Errorf("Validate accepted %q: %v", bad, err)
		}
	}
	// other engines keep their own host rules
	if err := (Connection{Name: "p", Driver: Postgres, Host: "http://x"}).Validate(); err != nil {
		t.Errorf("postgres host: %v", err)
	}
}
