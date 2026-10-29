package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"webtuidb/internal/db"
	"webtuidb/internal/keymap"
	"webtuidb/internal/mcp"
	"webtuidb/internal/plugin"
)

// pluginBindings are the shortcuts of the installed plugins as keymap bindings (action "plugin:<name>").
func pluginBindings() []keymap.Binding { return plugin.Bindings() }

// pluginHost lets a .js plugin run read-only SQL on the session it was started from.
type pluginHost struct {
	ss *session
}

func (h pluginHost) Query(ctx context.Context, sql string) ([]string, [][]string, error) {
	sess := h.ss.o.Sess
	dialect := sess.Caps().Dialect
	st := db.Split(dialect, sql)
	if len(st) != 1 || !db.IsReadOnly(dialect, st[0].Text) {
		return nil, nil, errors.New("query(): only a single read-only statement is allowed")
	}
	if fn := mcp.SideEffect(dialect, st[0].Text); fn != "" {
		return nil, nil, fmt.Errorf("query(): %s is not allowed in a plugin query", fn)
	}
	run, release, err := mcp.ReadOnlyTx(ctx, sess, dialect, h.ss.conn.ReadOnly) // a read-only transaction, so a side effect cannot commit
	if err != nil {
		return nil, nil, err
	}
	defer release()
	res, err := run.Exec(ctx, st[0].Text, h.ss.rowLimit())
	if err != nil {
		return nil, nil, err
	}
	cols := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		cols[i] = c.Name
	}
	rows := make([][]string, len(res.Rows))
	for i, r := range res.Rows {
		rows[i] = make([]string, len(r))
		for k, v := range r {
			if v != nil {
				rows[i][k] = fmt.Sprint(v)
			}
		}
	}
	return cols, rows, nil
}

func effectsJSON(l []plugin.Effect) []map[string]any {
	out := make([]map[string]any, 0, len(l))
	for _, e := range l {
		out = append(out, map[string]any{"kind": e.Kind, "text": e.Text, "ref": e.Ref})
	}
	return out
}

func (s *Server) plugins(r *http.Request) (any, error) {
	l, err := plugin.List()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, p := range l {
		e := ""
		if p.Err != nil {
			e = p.Err.Error()
		}
		out = append(out, map[string]any{"name": p.Name, "contexts": nonNil(p.Contexts), "output": p.Output, "key": p.Key, "js": p.JS, "error": e})
	}
	return out, nil
}

func (s *Server) pluginRun(ss *session, r *http.Request) (any, error) {
	var req struct {
		Name, Context, Selection, Cell string
		Object                         *db.ObjRef
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	l, err := plugin.List() // the command comes from the plugin file, never from the browser
	if err != nil {
		return nil, err
	}
	for _, p := range l {
		if p.Name != req.Name || p.Err != nil {
			continue
		}
		ctx, cancel := ss.ctx(r)
		defer cancel()
		out, err := p.Run(ctx, plugin.Context{Connection: ss.conn.Name, Driver: ss.conn.Driver, Database: ss.o.Sess.CurrentDatabase(), Context: req.Context, Object: req.Object, Selection: req.Selection, Cell: req.Cell}, pluginHost{ss})
		if err != nil {
			return nil, bad("%v", err)
		}
		return map[string]any{"output": p.Output, "text": out.Text, "effects": effectsJSON(out.Effects)}, nil
	}
	return nil, bad("no plugin named %q", req.Name)
}
