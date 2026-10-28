package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/keymap"
	"webtuidb/internal/mcp"
	"webtuidb/internal/plugin"
)

// M13.6 plugins: shortcuts ("plugin:<name>" actions), JS plugins with a host that can query, and their effects.

// pluginBindings are the Global bindings of the plugins that declare a key; the app passes them to config.KeymapWith.
func pluginBindings() []keymap.Binding { return plugin.Bindings() }

// pluginBindingsChecked builds cfg's keymap with the plugin shortcuts added one at a time (same code path as the
// web /api/meta): a plugin key that clashes is skipped and listed in conflicts instead of dropping every plugin key.
func pluginBindingsChecked(cfg config.Config) (*keymap.Map, []string, error) {
	return cfg.KeymapChecked(pluginBindings())
}

// logPluginConflicts shows skipped plugin shortcuts in the status toast (no timer: it stays until the next toast).
func (a *App) logPluginConflicts(conflicts []string) {
	if len(conflicts) == 0 {
		return
	}
	a.toast, a.toastErr = "plugin shortcut skipped: "+strings.Join(conflicts, "; "), true
	a.toastSeq++
}

// pluginDetail is the second column of the plugins picker: kind (js / output), key and command.
func pluginDetail(p plugin.Plugin) string {
	kind := p.Output
	if p.JS {
		kind = "js"
	}
	parts := []string{kind}
	if p.Key != "" {
		parts = append(parts, p.Key)
	}
	if !p.JS {
		parts = append(parts, p.Command)
	}
	return strings.Join(parts, " · ")
}

// sessionHost lets a JS plugin run read-only statements on the workspace's session.
type sessionHost struct {
	sess     db.Session
	readOnly bool // the connection is marked read_only
}

func (h sessionHost) Query(ctx context.Context, sql string) ([]string, [][]string, error) {
	if !db.IsReadOnly(h.sess.Caps().Dialect, sql) {
		return nil, nil, fmt.Errorf("plugins may only run read-only statements")
	}
	dialect := h.sess.Caps().Dialect
	if fn := mcp.SideEffect(dialect, sql); fn != "" {
		return nil, nil, fmt.Errorf("%s is not allowed in a plugin query", fn)
	}
	run, release, err := mcp.ReadOnlyTx(ctx, h.sess, dialect, h.readOnly) // a read-only transaction, so a side effect cannot commit
	if err != nil {
		return nil, nil, err
	}
	defer release()
	res, err := run.Exec(ctx, sql, 1000)
	if err != nil {
		return nil, nil, err
	}
	var cols []string
	for _, c := range res.Columns {
		cols = append(cols, c.Name)
	}
	rows := make([][]string, len(res.Rows))
	for i, r := range res.Rows {
		for _, c := range r {
			rows[i] = append(rows[i], plain(c))
		}
	}
	return cols, rows, nil
}

func (w *Workspace) runPlugin(p plugin.Plugin, ctxName string) tea.Cmd {
	in := plugin.Context{Connection: w.conn.Name, Driver: w.conn.Driver, Database: w.sess.CurrentDatabase(), Context: ctxName}
	if r, ok := w.targetRef(); ok {
		in.Object = &r
	}
	var g *Grid
	switch t := w.tab().(type) {
	case *queryTab:
		if !w.focusSB {
			in.Selection = t.ed.Selection()
			if rv := t.curResult(); t.focusResults && rv != nil {
				g = rv.grid
			}
		}
	case *tableTab:
		if !w.focusSB && t.view == viewData {
			g = t.grid
		}
	}
	if g != nil {
		if v, ok := g.CursorValue(); ok {
			in.Cell = plain(v)
		}
	}
	host := sessionHost{sess: w.sess, readOnly: w.conn.ReadOnly}
	return w.liveCmd(func() func() tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		out, err := p.Run(ctx, in, host)
		return func() tea.Cmd {
			if err != nil {
				return w.a.fail(err)
			}
			return w.pluginOutput(p, out)
		}
	})
}

// pluginOutput shows the text (or inserts it for sql plugins) and applies the effects in order.
func (w *Workspace) pluginOutput(p plugin.Plugin, out plugin.Output) tea.Cmd {
	var cmds []tea.Cmd
	text := strings.TrimRight(out.Text, "\n")
	switch {
	case strings.TrimSpace(text) == "":
		if len(out.Effects) == 0 {
			return w.a.say("%s printed nothing", p.Name)
		}
	case p.Output == "sql":
		if q, ok := w.tab().(*queryTab); ok && !w.focusSB {
			q.ed.Handle(tea.PasteMsg{Content: text})
			cmds = append(cmds, q.afterEdit(), w.a.say("%s: inserted into the editor", p.Name))
		} else {
			cmds = append(cmds, w.newQueryTab(text))
		}
	default:
		w.a.push(&textViewDlg{title: p.Name, lines: strings.Split(text, "\n"), actions: []viewAction{{Key: "c", Label: "c: copy", Do: func() tea.Cmd { return setClip(out.Text) }}}})
	}
	for _, e := range out.Effects {
		switch e.Kind {
		case "copy":
			cmds = append(cmds, setClip(e.Text), w.a.say("%s: copied", p.Name))
		case "open":
			ref := e.Ref
			if ref.Kind == "" {
				ref.Kind = db.KindTable
			}
			if ref.Schema == "" {
				ref.Schema = w.sess.CurrentSchema()
			}
			cmds = append(cmds, w.openObject(ref, true))
		case "notify":
			cmds = append(cmds, w.a.say("%s", e.Text))
		case "sql":
			cmds = append(cmds, w.newQueryTab(e.Text))
		}
	}
	return tea.Batch(cmds...)
}
