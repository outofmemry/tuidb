// board_panels.go: Interactive panels of a metrics board: inputs, dropdowns, exporters, variables and errors.

package ui

import (
	"context"
	"fmt"
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/board"
)

// interactivePanel is a kind the UI answers itself: board.Run skips it.
func interactivePanel(kind string) bool {
	switch kind {
	case "button", "import", "input", "dropdown", "exporter":
		return true
	}
	return false
}

// boardFormat maps a panel's Format (csv|json|sql|excel) to an export format.
func boardFormat(f string) string {
	switch f {
	case "excel", "xlsx":
		return "xlsx"
	case "json", "sql":
		return f
	}
	return "csv"
}

// setVar stores a panel's variable and runs the whole board again, since any query may use it.
func (v *boardView) setVar(name, val string) tea.Cmd {
	v.vals[name] = val
	return v.run()
}

// activateInput is Enter on an input panel: a line edit bound to the panel's Var.
func (v *boardView) activateInput(p board.Panel) tea.Cmd {
	if p.Var == "" {
		return v.w.a.say("the input panel %q names no variable", p.Title)
	}
	v.w.a.push(newForm(p.Title, []formField{{Key: "v", Label: "$" + p.Var, Kind: fText, Value: v.vals[p.Var]}}, func(m map[string]string) tea.Cmd {
		return v.setVar(p.Var, m["v"])
	}))
	return nil
}

// activateDropdown loads the options (fixed, or the first column of the panel's query) and picks one.
func (v *boardView) activateDropdown(p board.Panel) tea.Cmd {
	if p.Var == "" {
		return v.w.a.say("the dropdown panel %q names no variable", p.Title)
	}
	sess := v.session()
	if sess == nil {
		return nil
	}
	b, vals := v.b, v.valsCopy()
	return v.w.liveCmd(func() func() tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		opts, cut, err := board.DropdownOptions(ctx, sess, b, p, vals)
		return func() tea.Cmd {
			if err != nil {
				return v.w.a.fail(err)
			}
			var items []pickItem
			for _, o := range opts {
				label := o
				if o == v.vals[p.Var] {
					label = "• " + o
				}
				items = append(items, pickItem{Label: label, Value: o})
			}
			v.w.a.push(&pickerDlg{title: p.Title + " ($" + p.Var + ")", filter: true, items: items, empty: "no options",
				onPick: func(it pickItem, _ bool) tea.Cmd { return v.setVar(p.Var, it.Value.(string)) }})
			if cut {
				return v.w.a.say("only the first %d options are listed", board.DropdownLimit)
			}
			return nil
		}
	})
}

// boardExportLimit is how many rows an exporter panel reads into memory; the export form says so when it is hit.
const boardExportLimit = 100000

// activateExporter runs the panel's query with the current variables and opens the export dialog on the rows.
func (v *boardView) activateExporter(p board.Panel) tea.Cmd {
	sess := v.session()
	if sess == nil {
		return nil
	}
	b, vals := v.b, v.valsCopy()
	return v.w.liveCmd(func() func() tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		res, err := board.ExportPanel(ctx, sess, b, p, vals, boardExportLimit)
		return func() tea.Cmd {
			if err != nil {
				return v.w.a.fail(err)
			}
			if res == nil {
				return v.w.a.say("%s returned nothing", p.Title)
			}
			return v.w.exportResultForm(p.Title, res, boardFormat(p.Format))
		}
	})
}

// errorCount is how many panels of the last run failed.
func (v *boardView) errorCount() int {
	n := 0
	for _, r := range v.res {
		if r.Err != nil {
			n++
		}
	}
	return n
}

// showErrors lists the failed panels with their messages.
func (v *boardView) showErrors() tea.Cmd {
	var titles []string
	for t, r := range v.res {
		if r.Err != nil {
			titles = append(titles, t)
		}
	}
	if len(titles) == 0 {
		return v.w.a.say("no panel errors")
	}
	sort.Strings(titles)
	var lines []string
	for _, t := range titles {
		lines = append(lines, t+": "+v.res[t].Err.Error(), "")
	}
	v.w.a.push(&textViewDlg{title: fmt.Sprintf("Board %s: %s", v.b.Name, plural(len(titles), "panel error")), danger: true, lines: lines})
	return nil
}

// variablesForm edits the board's declared variables.
func (v *boardView) variablesForm() tea.Cmd {
	b := v.b
	if len(b.Vars) == 0 {
		return v.w.a.say("this board has no variables (input and dropdown panels set theirs: Enter on them)")
	}
	var fs []formField
	for _, x := range b.Vars {
		f := formField{Key: x.Name, Label: orDefault(x.Label, x.Name), Kind: fText, Value: v.vals[x.Name]}
		if len(x.Options) > 0 {
			f.Kind, f.Options = fChoice, x.Options
		}
		fs = append(fs, f)
	}
	v.w.a.push(newForm("Variables of "+b.Name, fs, func(m map[string]string) tea.Cmd {
		for k, x := range m {
			v.vals[k] = x
		}
		return v.run()
	}))
	return nil
}
