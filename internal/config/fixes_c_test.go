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
