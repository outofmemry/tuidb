package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlugin(t *testing.T) {
	d := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", d)
	os.MkdirAll(Dir(), 0o755)
	os.WriteFile(filepath.Join(Dir(), "a.toml"), []byte(`name="Echo"
command="sh"
args=["-c","cat"]
contexts=["editor"]
`), 0o600)
	os.WriteFile(filepath.Join(Dir(), "b.toml"), []byte("name = ["), 0o600)
	ps, err := List()
	if err != nil || len(ps) != 2 || ps[0].Name != "Echo" || ps[0].Output != "text" || ps[1].Err == nil {
		t.Fatalf("%v %+v", err, ps)
	}
	out, err := ps[0].Run(context.Background(), Context{Connection: "c", Context: "editor", Selection: "select 1"})
	if err != nil || !strings.Contains(out, `"selection":"select 1"`) {
		t.Errorf("%q %v", out, err)
	}
	bad := Plugin{Name: "x", Command: "sh", Args: []string{"-c", "echo boom >&2; exit 3"}}
	if _, err := bad.Run(context.Background(), Context{}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("stderr: %v", err)
	}
	slow := Plugin{Name: "s", Command: "sleep", Args: []string{"5"}}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	st := time.Now()
	if _, err := slow.Run(ctx, Context{}); err == nil || time.Since(st) > 3*time.Second {
		t.Errorf("timeout: %v", err)
	}
}

func TestPluginCap(t *testing.T) {
	big := Plugin{Name: "b", Command: "sh", Args: []string{"-c", "head -c 3000000 /dev/zero"}}
	out, err := big.Run(context.Background(), Context{})
	if err != nil || len(out) != 1<<20 {
		t.Errorf("%d %v", len(out), err)
	}
	st := time.Now()
	kid := Plugin{Name: "k", Command: "sh", Args: []string{"-c", "sleep 5 & echo hi"}}
	if out, err := kid.Run(context.Background(), Context{}); time.Since(st) > 3*time.Second {
		t.Errorf("hung on child: %q %v", out, err)
	}
}
