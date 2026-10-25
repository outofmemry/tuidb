// Package plugin runs external commands configured in <ConfigDir>/plugins/*.toml.
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
)

type Plugin struct {
	Name     string
	Command  string
	Args     []string
	Contexts []string
	Output   string // "text" | "sql"
	Path     string `toml:"-"`
	Err      error  `toml:"-"`
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
	return out, nil
}

// Run execs the command without a shell: JSON context on stdin, stdout is the result.
func (p Plugin) Run(ctx context.Context, in Context) (string, error) {
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
