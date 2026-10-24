// Package plugin runs plugins from <ConfigDir>/plugins: *.toml (an external command) and *.js (a goja script).
package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/keymap"
)

type Plugin struct {
	Name     string
	Command  string
	Args     []string
	Contexts []string
	Output   string // "text" | "sql"
	Key      string // keymap spelling, e.g. "alt+P"; bound as the action "plugin:<Name>"
	JS       bool   `toml:"-"` // a .js plugin: Path is the script, Command is unused
	Path     string `toml:"-"`
	Err      error  `toml:"-"`
	src      []byte // the script as loadJS vetted it (size and all); runJS runs these bytes, not a fresh read of the file
}

type Context struct {
	Connection string     `json:"connection"`
	Driver     string     `json:"driver"`
	Database   string     `json:"database"`
	Context    string     `json:"context"`
	Object     *db.ObjRef `json:"object,omitempty"`
	Selection  string     `json:"selection,omitempty"`
	Cell       string     `json:"cell,omitempty"`
}

// Effect is something the UI does after a plugin ran. Kind: copy | open | notify | sql.
// MaxEffects caps the effects one script run may produce; the next one throws.
const MaxEffects = 100

type Effect struct {
	Kind string    `json:"kind"`
	Text string    `json:"text,omitempty"`
	Ref  db.ObjRef `json:"ref,omitempty"` // open: the table to open
}

// Output is a plugin's result: Text (stdout of a command, or the script's final string) plus Effects.
type Output struct {
	Text    string   `json:"text"`
	Effects []Effect `json:"effects"`
}

// Host lets a JS plugin query the connection it runs on. A nil Host means query() throws.
type Host interface {
	Query(ctx context.Context, sql string) (cols []string, rows [][]string, err error)
}

func Dir() string { return filepath.Join(config.ConfigDir(), "plugins") }

// List parses every *.toml in Dir; a bad file becomes a Plugin with Err set.
func List() ([]Plugin, error) {
	files, err := filepath.Glob(filepath.Join(Dir(), "*.toml"))
	if err != nil {
		return nil, err
	}
	var out []Plugin
	for _, f := range files {
		p := Plugin{Path: f}
		if _, err := toml.DecodeFile(f, &p); err != nil {
			p = Plugin{Name: filepath.Base(f), Path: f, Err: err}
		} else if p.Name == "" || p.Command == "" {
			p.Err = fmt.Errorf("%s: name and command are required", f)
		}
		if p.Output == "" {
			p.Output = "text"
		}
		out = append(out, p)
	}
	js, _ := filepath.Glob(filepath.Join(Dir(), "*.js"))
	for _, f := range js {
		out = append(out, loadJS(f))
	}
	return out, nil
}

// Run runs the plugin. A command plugin execs without a shell (JSON context on stdin, stdout is Output.Text);
// a .js plugin runs in-process with the API described in m13_script.go.
func (p Plugin) Run(ctx context.Context, in Context, host Host) (Output, error) {
	if p.JS {
		return p.runJS(ctx, in, host)
	}
	text, err := p.runCommand(ctx, in)
	return Output{Text: text}, err
}

func (p Plugin) runCommand(ctx context.Context, in Context) (string, error) {
	data, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	out, errb := &capBuf{}, &capBuf{}
	cmd := exec.CommandContext(ctx, p.Command, p.Args...)
	cmd.WaitDelay = time.Second // a child holding the pipes must not hang Run
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(data), out, errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("%s: %w: %s", p.Name, err, strings.TrimSpace(errb.b.String()))
	}
	return out.b.String(), nil
}

const maxOut = 1 << 20

// capBuf keeps the first maxOut bytes and silently drops the rest, so a chatty plugin cannot exhaust memory.
type capBuf struct{ b bytes.Buffer } // not embedded: Buffer.ReadFrom would bypass Write

func (b *capBuf) Write(p []byte) (int, error) {
	if room := maxOut - b.b.Len(); room > 0 {
		b.b.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// Bindings are the shortcuts of the installed plugins as Global keymap bindings (action "plugin:<name>"). The keys
// are as written in the plugin file; the keymap parses them and reports the ones it cannot take.
func Bindings() []keymap.Binding {
	l, _ := List()
	var out []keymap.Binding
	for _, p := range l {
		if p.Err == nil && p.Key != "" {
			out = append(out, keymap.Binding{Action: "plugin:" + p.Name, Key: p.Key, Context: keymap.Global, Desc: "plugin: " + p.Name})
		}
	}
	return out
}
