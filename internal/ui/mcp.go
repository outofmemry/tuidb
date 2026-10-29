// mcp.go: The MCP server: address, log, exported rows, clients.

package ui

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/mcp"
)

// mcpAddr is where the MCP listener binds: [mcp] listen, else 127.0.0.1:port.
func (a *App) mcpAddr() string {
	if l := a.d.Cfg.MCP.Listen; l != "" {
		return l
	}
	return fmt.Sprintf("127.0.0.1:%d", a.d.Cfg.MCP.Port)
}

func (w *Workspace) mcpLog() tea.Cmd {
	a := w.a
	if a.mcp == nil {
		return a.say("the MCP server is off: set enabled = true under [mcp]")
	}
	scheme := "http"
	if a.d.Cfg.MCP.TLSCert != "" {
		scheme = "https (TLS)"
	}
	lines := []string{"listening on " + a.mcpAddr() + " over " + scheme, mcpToolsHelp, ""}
	if a.mcpErr != nil {
		lines = []string{"✗ could not listen: " + a.mcpErr.Error(), ""}
	}
	es := a.mcp.Log()
	for i := len(es) - 1; i >= 0; i-- {
		e := es[i]
		l := fmt.Sprintf("%s  %s  %s  %s  %s", e.Time.Format("15:04:05"), e.Token, e.Tool, e.Connection, oneLine(e.SQL))
		if e.Err != "" {
			l += "  ✗ " + e.Err
		}
		lines = append(lines, l)
	}
	if len(es) == 0 {
		lines = append(lines, "no requests yet")
	}
	a.push(&textViewDlg{title: "MCP activity", lines: lines, actions: []viewAction{
		{Key: "e", Label: "e: export CSV", Do: func() tea.Cmd { return w.mcpExportCSV(es) }},
		{Key: "c", Label: "c: clients", Do: func() tea.Cmd { return w.mcpClients(es) }},
	}})
	return nil
}

func (w *Workspace) mcpExportCSV(es []mcp.Entry) tea.Cmd {
	w.a.push(newForm("Export the MCP log as CSV", []formField{{Key: "path", Label: "File", Kind: fText, Value: "mcp-log.csv"}}, func(v map[string]string) tea.Cmd {
		path := strings.TrimSpace(v["path"])
		return w.a.ifFree(func() tea.Cmd {
			err := writeAtomic(path, func(f io.Writer) error {
				cw := csv.NewWriter(f)
				cw.Write([]string{"time", "token", "tool", "connection", "sql", "error"})
				for _, e := range es {
					cw.Write([]string{e.Time.Format(time.RFC3339), e.Token, e.Tool, e.Connection, e.SQL, e.Err})
				}
				cw.Flush()
				return cw.Error()
			})
			if err != nil {
				return w.a.fail(err)
			}
			return w.a.say("saved %s to %s", plural(len(es), "request"), path)
		}, path)
	}))
	return nil
}

// mcpClients lists the tokens seen in the log. shortcut: the MCP server has no per-token
// disconnect, so this view only shows them; remove the token from [[mcp.token]] and restart to cut one off.
func (w *Workspace) mcpClients(es []mcp.Entry) tea.Cmd {
	type seen struct {
		n    int
		last time.Time
		conn string
	}
	by := map[string]*seen{}
	var order []string
	for _, e := range es {
		s := by[e.Token]
		if s == nil {
			s = &seen{}
			by[e.Token] = s
			order = append(order, e.Token)
		}
		s.n++
		s.last, s.conn = e.Time, e.Connection
	}
	lines := []string{"Clients seen in the activity log (disconnecting one is not available here)", ""}
	for _, t := range order {
		s := by[t]
		lines = append(lines, fmt.Sprintf("%-20s %4d requests   last %s on %s", t, s.n, s.last.Format("15:04:05"), orDefault(s.conn, "-")))
	}
	if len(order) == 0 {
		lines = append(lines, "no clients yet")
	}
	w.a.push(&textViewDlg{title: "MCP clients", lines: lines})
	return nil
}

// stackLabel names a workspace in the session switcher: [general] show_connection_name / show_database_name
// pick the parts; with neither set both are shown.
func (a *App) stackLabel(w *Workspace) string {
	g := a.d.Cfg.General
	switch {
	case g.ShowConnectionName && !g.ShowDatabaseName:
		return w.conn.Name
	case g.ShowDatabaseName && !g.ShowConnectionName:
		return w.sess.CurrentDatabase()
	}
	return w.conn.Name + " / " + w.sess.CurrentDatabase()
}

// mcpToolsHelp lists what an MCP client can call, for the top of the activity view.
const mcpToolsHelp = "Tools: list_connections, list_open_connections, open_connection, disconnect, execute_read_query, execute_query, " +
	"list_saved_queries, get_saved_query, list_history, list_boards, run_board"
