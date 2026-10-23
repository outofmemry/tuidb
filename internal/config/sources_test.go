package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAllConnections(t *testing.T) {
	d := t.TempDir()
	main := filepath.Join(d, "connections.toml")
	os.WriteFile(main, []byte("[[connection]]\nname = \"a\"\ndriver = \"postgres\"\n"), 0o600)
	src := filepath.Join(d, "src")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "x.toml"), []byte("[[connection]]\nname = \"a\"\ndriver = \"mysql\"\n[[connection]]\nname = \"b\"\ndriver = \"mysql\"\n"), 0o600)
	cs, err := LoadAllConnections(main, []Source{{Path: src}})
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].Driver != "postgres" || cs[1].Name != "b" || cs[1].Source == "" {
		t.Fatalf("got %+v", cs)
	}
}
