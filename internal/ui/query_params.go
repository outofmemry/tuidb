// query_params.go: Query parameters: finding :name / ? placeholders in the editor and asking for their values.

package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

var paramFormats = []string{"colon", "dollar", "at", "brace", "mustache", "question"}

func (t *queryTab) paramFormat() string { return t.ws.a.d.Cfg.Editor.ParamFormat }

func (t *queryTab) paramNames(stmts []db.Stmt) []string {
	return t.paramNamesAs(stmts, t.paramFormat())
}

func (t *queryTab) paramNamesAs(stmts []db.Stmt, format string) []string {
	var names []string
	seen := map[string]bool{}
	for _, s := range stmts {
		for _, n := range db.Params(s.Text, db.ParamFormat(format), t.ws.caps.Dialect) {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	return names
}

func (t *queryTab) bindAs(stmts []db.Stmt, vals map[string]string, format string) []db.Stmt {
	out := make([]db.Stmt, len(stmts))
	for i, s := range stmts {
		typed := s.Text
		s.Text = db.Bind(s.Text, db.ParamFormat(format), vals, t.ws.caps.Dialect)
		if s.Text != typed {
			if t.unbound == nil {
				t.unbound = map[string]string{}
			}
			t.unbound[s.Text] = typed
		}
		out[i] = s
	}
	return out
}

// typed is the statement as the user wrote it, before variable values were substituted (what history records).
func (t *queryTab) typed(bound string) string {
	if s, ok := t.unbound[bound]; ok {
		return s
	}
	return bound
}

// formatKey names the Variables dialog's format picker among the variable fields.
const formatKey = "\x00format"

// askVars shows one field per parameter, pre-filled with what was typed earlier this session, and a format picker
// that overrides [editor] param_format for this run: picking a format with other variables asks again.
func (t *queryTab) askVars(stmts []db.Stmt, format string, then func(vals map[string]string, format string) tea.Cmd) tea.Cmd {
	names := t.paramNamesAs(stmts, format)
	fields := []formField{{Key: formatKey, Label: "Format", Kind: fChoice, Options: paramFormats, Value: format}}
	for _, n := range names {
		fields = append(fields, formField{Key: n, Label: n, Kind: fText, Value: t.ws.vars[n]})
	}
	f := newForm("Variables", fields, func(v map[string]string) tea.Cmd {
		if t.ws.vars == nil {
			t.ws.vars = map[string]string{}
		}
		for k, x := range v {
			if k != formatKey {
				t.ws.vars[k] = x
			}
		}
		if chosen := v[formatKey]; chosen != format && strings.Join(t.paramNamesAs(stmts, chosen), ",") != strings.Join(names, ",") {
			return t.askVars(stmts, chosen, then) // the other format sees other variables
		}
		return then(v, v[formatKey])
	})
	f.submit = "Enter: run"
	if len(names) > 0 {
		f.idx = 1 // the first variable, not the format picker
	}
	t.ws.a.push(f)
	return nil
}

// editVars is the `variables` action: set the values without running anything.
func (t *queryTab) editVars() tea.Cmd {
	stmts := db.Split(t.ws.caps.Dialect, t.ed.Text())
	format := t.paramFormat()
	for _, f := range append([]string{format}, paramFormats...) { // the configured format first, else whichever finds variables
		if len(t.paramNamesAs(stmts, f)) > 0 {
			format = f
			break
		}
	}
	if len(t.paramNamesAs(stmts, format)) == 0 {
		return t.ws.a.say("no parameters in the editor (format: %s)", t.paramFormat())
	}
	// Enter saves the values and runs with them bound, also with [editor] query_params off: the user filled the dialog
	return t.askVars(stmts, format, func(vals map[string]string, f string) tea.Cmd {
		run := stmts
		if t.ws.a.d.Cfg.Editor.RunDefault != "all" {
			run = t.currentStmts()
		}
		if len(run) == 0 {
			return t.ws.a.say("variables saved (nothing to run at the cursor)")
		}
		if t.running {
			return t.ws.a.say("variables saved; a query is already running")
		}
		return t.runBound(t.bindAs(run, vals, f))
	})
}

// paramsOffHint: with [editor] query_params off, a run whose statements use variables the Variables dialog knows
// says how to bind them, instead of only failing on the ":".
func (t *queryTab) paramsOffHint(stmts []db.Stmt) tea.Cmd {
	for _, n := range t.paramNames(stmts) {
		if _, ok := t.ws.vars[n]; ok {
			return t.ws.a.say("Parameters are off: set [editor] query_params = true to bind :name on every run")
		}
	}
	return nil
}
