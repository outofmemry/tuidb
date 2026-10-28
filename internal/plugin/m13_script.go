package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"

	"webtuidb/internal/db"
)

// jsTimeout bounds one script run; the script is interrupted, not left spinning.
var jsTimeout = 10 * time.Second

// jsMemCap bounds how much the heap may grow during one run (`for(;;) s = s + s` would otherwise take the
// whole process down well inside jsTimeout).
var jsMemCap uint64 = 256 << 20

const maxScript = 1 << 20

// loadJS reads a script's header: leading "// key: alt+P", "// name: ...", "// contexts: table,result" comments.
func loadJS(path string) Plugin {
	p := Plugin{Path: path, JS: true, Output: "text", Name: strings.TrimSuffix(filepath.Base(path), ".js")}
	b, err := os.ReadFile(path)
	if err != nil {
		p.Err = err
		return p
	}
	if len(b) > maxScript {
		p.Err = fmt.Errorf("%s: script larger than %d bytes", path, maxScript)
		return p
	}
	p.src = b
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		rest, ok := strings.CutPrefix(line, "//")
		if !ok {
			break
		}
		k, v, ok := strings.Cut(rest, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "key":
			p.Key = v
		case "name":
			p.Name = v
		case "output":
			p.Output = v
		case "contexts":
			for _, c := range strings.Split(v, ",") {
				if c = strings.TrimSpace(c); c != "" {
					p.Contexts = append(p.Contexts, c)
				}
			}
		}
	}
	return p
}

// runJS evaluates the script. API: context, query(sql) -> {columns, rows}, copy(text), open(schema, table),
// notify(text), sql(text). A string as the script's final expression becomes Output.Text.
func (p Plugin) runJS(ctx context.Context, in Context, host Host) (Output, error) {
	if p.Err != nil {
		return Output{}, p.Err
	}
	src := p.src
	if src == nil { // a Plugin not made by loadJS: read it, with the same size limit
		var err error
		if src, err = os.ReadFile(p.Path); err != nil {
			return Output{}, err
		}
		if len(src) > maxScript {
			return Output{}, fmt.Errorf("%s: script larger than %d bytes", p.Path, maxScript)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, jsTimeout)
	defer cancel()
	vm := goja.New()
	var out Output
	fail := func(err error) { panic(vm.NewGoError(err)) }
	var cx map[string]any
	data, _ := json.Marshal(in)
	_ = json.Unmarshal(data, &cx)
	set := func(name string, f any) { _ = vm.Set(name, f) }
	set("context", cx)
	set("query", func(sql string) any {
		if host == nil {
			fail(fmt.Errorf("query is not available here"))
		}
		cols, rows, err := host.Query(ctx, sql)
		if err != nil {
			fail(err)
		}
		return map[string]any{"columns": cols, "rows": rows}
	})
	add := func(e Effect) {
		if len(out.Effects) >= MaxEffects {
			fail(fmt.Errorf("too many effects (at most %d)", MaxEffects))
		}
		out.Effects = append(out.Effects, e)
	}
	effect := func(kind string) func(string) {
		return func(text string) { add(Effect{Kind: kind, Text: text}) }
	}
	set("copy", effect("copy"))
	set("notify", effect("notify"))
	set("sql", effect("sql"))
	set("open", func(schema, table string) {
		add(Effect{Kind: "open", Ref: db.ObjRef{Schema: schema, Name: table, Kind: db.KindTable}})
	})
	done := make(chan struct{})
	defer close(done)
	var memHit atomic.Bool
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	go func() {
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		var ms runtime.MemStats
		for {
			select {
			case <-ctx.Done():
				vm.Interrupt("plugin stopped")
				return
			case <-done:
				return
			case <-tick.C:
				runtime.ReadMemStats(&ms)
				if ms.HeapAlloc > base.HeapAlloc && ms.HeapAlloc-base.HeapAlloc > jsMemCap {
					memHit.Store(true)
					vm.Interrupt("plugin used too much memory")
					return
				}
			}
		}
	}()
	v, err := vm.RunString(string(src))
	if err != nil {
		if memHit.Load() {
			return out, fmt.Errorf("%s: stopped, it used more than %d MiB of memory", p.Name, jsMemCap>>20)
		}
		if ctx.Err() != nil {
			return out, fmt.Errorf("%s: %w", p.Name, ctx.Err())
		}
		return out, fmt.Errorf("%s: %w", p.Name, err)
	}
	if s, ok := v.Export().(string); ok {
		out.Text = clip(s)
	}
	for i := range out.Effects {
		out.Effects[i].Text = clip(out.Effects[i].Text)
	}
	return out, nil
}

// clip cuts s to maxOut bytes, like a command plugin's output, on a rune boundary.
func clip(s string) string {
	if len(s) <= maxOut {
		return s
	}
	return strings.ToValidUTF8(s[:maxOut], "")
}
