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
	o, err := ps[0].Run(context.Background(), Context{Connection: "c", Context: "editor", Selection: "select 1"}, nil)
	out := o.Text
	if err != nil || !strings.Contains(out, `"selection":"select 1"`) {
		t.Errorf("%q %v", out, err)
	}
	bad := Plugin{Name: "x", Command: "sh", Args: []string{"-c", "echo boom >&2; exit 3"}}
	if _, err := bad.Run(context.Background(), Context{}, nil); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("stderr: %v", err)
	}
	slow := Plugin{Name: "s", Command: "sleep", Args: []string{"5"}}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	st := time.Now()
	if _, err := slow.Run(ctx, Context{}, nil); err == nil || time.Since(st) > 3*time.Second {
		t.Errorf("timeout: %v", err)
	}
}

func TestPluginCap(t *testing.T) {
	big := Plugin{Name: "b", Command: "sh", Args: []string{"-c", "head -c 3000000 /dev/zero"}}
	o, err := big.Run(context.Background(), Context{}, nil)
	if out := o.Text; err != nil || len(out) != 1<<20 {
		t.Errorf("%d %v", len(out), err)
	}
	st := time.Now()
	kid := Plugin{Name: "k", Command: "sh", Args: []string{"-c", "sleep 5 & echo hi"}}
	if out, err := kid.Run(context.Background(), Context{}, nil); time.Since(st) > 3*time.Second {
		t.Errorf("hung on child: %q %v", out.Text, err)
	}
}

type fakeHost struct{}

func (fakeHost) Query(_ context.Context, sql string) ([]string, [][]string, error) {
	return []string{"n"}, [][]string{{"1"}, {"2"}}, nil
}

func TestJSPlugin(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	os.MkdirAll(Dir(), 0o755)
	os.WriteFile(filepath.Join(Dir(), "count.js"), []byte(`// key: alt+P
// name: Row counter
// contexts: table, result
var r = query("select n from t");
copy(r.rows.length + " rows");
open("public", context.object ? context.object.Name : "t");
notify("done"); sql("select 1");
"cols=" + r.columns.join() + " db=" + context.database;
`), 0o600)
	os.WriteFile(filepath.Join(Dir(), "loop.js"), []byte("while (true) {}\n"), 0o600)
	os.WriteFile(filepath.Join(Dir(), "nohost.js"), []byte("query('select 1')\n"), 0o600)
	ps, _ := List()
	by := map[string]Plugin{}
	for _, p := range ps {
		by[p.Name] = p
	}
	p := by["Row counter"]
	if !p.JS || p.Key != "alt+P" || len(p.Contexts) != 2 || p.Contexts[1] != "result" || p.Output != "text" {
		t.Fatalf("%+v", p)
	}
	o, err := p.Run(context.Background(), Context{Database: "app"}, fakeHost{})
	if err != nil || o.Text != "cols=n db=app" || len(o.Effects) != 4 || o.Effects[0].Text != "2 rows" ||
		o.Effects[1].Kind != "open" || o.Effects[1].Ref.Name != "t" || o.Effects[3].Kind != "sql" {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := by["nohost"].Run(context.Background(), Context{}, nil); err == nil {
		t.Error("query without host accepted")
	}
	old := jsTimeout
	jsTimeout = 200 * time.Millisecond
	defer func() { jsTimeout = old }()
	st := time.Now()
	if _, err := by["loop"].Run(context.Background(), Context{}, nil); err == nil || time.Since(st) > 3*time.Second {
		t.Errorf("infinite loop not interrupted: %v after %v", err, time.Since(st))
	}
}

// R-020: a script cannot pile up effects without bound.
func TestJSPluginEffectCap(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	os.MkdirAll(Dir(), 0o755)
	os.WriteFile(filepath.Join(Dir(), "spam.js"), []byte("// name: spam\nwhile (true) { copy('x') }\n"), 0o600)
	ps, _ := List()
	for _, p := range ps {
		if p.Name == "spam" {
			o, err := p.Run(context.Background(), Context{}, nil)
			if err == nil || !strings.Contains(err.Error(), "too many effects") || len(o.Effects) != MaxEffects {
				t.Fatalf("%v %d", err, len(o.Effects))
			}
			return
		}
	}
	t.Fatal("plugin not listed")
}

func loadOne(t *testing.T, name, src string) Plugin {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	os.MkdirAll(Dir(), 0o755)
	path := filepath.Join(Dir(), name+".js")
	os.WriteFile(path, []byte(src), 0o600)
	return loadJS(path)
}

func TestJSPluginMemoryCap(t *testing.T) {
	p := loadOne(t, "grow", "var a = []; for (;;) a.push('x'.repeat(1000000) + a.length);\n")
	old := jsMemCap
	jsMemCap = 64 << 20
	defer func() { jsMemCap = old }()
	st := time.Now()
	_, err := p.Run(context.Background(), Context{}, nil)
	if err == nil || !strings.Contains(err.Error(), "memory") || time.Since(st) > 8*time.Second {
		t.Errorf("runaway allocation not stopped: %v after %v", err, time.Since(st))
	}
}

func TestJSPluginOutputTruncated(t *testing.T) {
	p := loadOne(t, "big", "copy('y'.repeat(3000000)); 'x'.repeat(3000000)\n")
	o, err := p.Run(context.Background(), Context{}, nil)
	if err != nil || len(o.Text) != maxOut || len(o.Effects[0].Text) != maxOut {
		t.Fatalf("%v %d %d", err, len(o.Text), len(o.Effects[0].Text))
	}
}

func TestJSPluginHonoursLoadVerdict(t *testing.T) {
	p := loadOne(t, "ok", "'one'\n")
	os.WriteFile(p.Path, []byte("'two'\n"), 0o600) // edited after load: the vetted bytes still run
	if o, err := p.Run(context.Background(), Context{}, nil); err != nil || o.Text != "one" {
		t.Errorf("%q %v", o.Text, err)
	}
	big := loadOne(t, "huge", "// name: huge\n"+strings.Repeat("x", maxScript))
	if big.Err == nil {
		t.Fatal("oversized script loaded")
	}
	if _, err := big.Run(context.Background(), Context{}, nil); err == nil {
		t.Error("script with a load error ran")
	}
	// a Plugin built by hand is size-checked at run time too
	hand := Plugin{Name: "h", JS: true, Path: big.Path}
	if _, err := hand.Run(context.Background(), Context{}, nil); err == nil {
		t.Error("oversized script ran")
	}
}
