package ui

import (
	"encoding/csv"
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

// M15.12 / M15.3 process list helpers.

// dismissOnQ lets q close a picker like Esc does (Dismiss in the process menu).
type dismissOnQ struct{ *pickerDlg }

func (d dismissOnQ) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok && k.Keystroke() == "q" {
		return d.pickerDlg.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	}
	o, cmd := d.pickerDlg.Update(msg)
	if o == Overlay(d.pickerDlg) {
		return d, cmd
	}
	return o, cmd
}

var procHeader = []string{"id", "user", "db", "state", "elapsed", "query"}

func procFields(x db.ProcessInfo) []string {
	return []string{x.ID, x.User, x.DB, x.State, x.Elapsed.String(), strings.Join(strings.Fields(x.Query), " ")}
}

// processAs renders the rows as CSV (header first), JSON (array of objects) or a Markdown table.
func processAs(ps []db.ProcessInfo, f CopyFormat) string {
	switch f {
	case FmtJSON:
		out := make([]map[string]string, len(ps))
		for i, x := range ps {
			out[i] = map[string]string{}
			for j, v := range procFields(x) {
				out[i][procHeader[j]] = v
			}
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return string(b)
	case FmtMarkdown:
		lines := []string{"| " + strings.Join(procHeader, " | ") + " |", "|" + strings.Repeat(" --- |", len(procHeader))}
		for _, x := range ps {
			cells := procFields(x)
			for i := range cells {
				cells[i] = strings.ReplaceAll(cells[i], "|", `\|`)
			}
			lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
		}
		return strings.Join(lines, "\n")
	}
	var sb strings.Builder
	cw := csv.NewWriter(&sb)
	cw.Write(procHeader)
	for _, x := range ps {
		cw.Write(procFields(x))
	}
	cw.Flush()
	return strings.TrimRight(sb.String(), "\n")
}

func (w *Workspace) copyProcessesAs(ps []db.ProcessInfo) tea.Cmd {
	if len(ps) == 0 {
		return w.a.say("no processes to copy")
	}
	w.a.pushCopyAs("Copy processes as", []CopyFormat{FmtCSVHeader, FmtJSON, FmtMarkdown}, func(f CopyFormat) tea.Cmd {
		return w.a.copyText(processAs(ps, f), plural(len(ps), "process"))
	})
	return nil
}
