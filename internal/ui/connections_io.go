// connections_io.go: Exporting and importing connections, and opening a connection target.

package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

// exportConns is x on the welcome screen: all connections, or one group, to a passphrase-protected file.
func (w *welcome) exportConns() tea.Cmd {
	scopes := []string{"all"}
	scopes = append(scopes, config.Groups(w.a.d.Conns)...)
	w.a.push(newForm("Export connections", []formField{
		{Key: "scope", Label: "What", Kind: fChoice, Options: scopes, Value: "all"},
		{Key: "path", Label: "File", Kind: fText, Value: "connections.wtdb"},
		{Key: "pass", Label: "Passphrase", Kind: fSecret, Hint: "required with passwords; empty writes plain TOML"},
		{Key: "pw", Label: "Include passwords", Kind: fBool, Value: "false"},
	}, func(v map[string]string) tea.Cmd {
		var cs []config.Connection
		for _, c := range w.a.d.Conns {
			if v["scope"] == "all" || c.Group == v["scope"] || strings.HasPrefix(c.Group, v["scope"]+"/") {
				cs = append(cs, c)
			}
		}
		path := strings.TrimSpace(v["path"])
		return w.a.ifFree(func() tea.Cmd {
			err := writeAtomic(path, func(f io.Writer) error { return config.ExportConnections(f, cs, v["pass"], v["pw"] == "true") })
			if err != nil {
				return w.a.fail(err)
			}
			return w.a.say("exported %s to %s", plural(len(cs), "connection"), path)
		}, path)
	}).checked(func(v map[string]string) error {
		if strings.TrimSpace(v["path"]) == "" {
			return errors.New("file name required")
		}
		if v["pw"] == "true" && v["pass"] == "" {
			return errors.New("include passwords needs a passphrase")
		}
		return nil
	}))
	return nil
}

// importConns is I on the welcome screen. Names already taken get " (imported)" appended.
func (w *welcome) importConns() tea.Cmd {
	w.a.push(newForm("Import connections", []formField{
		{Key: "path", Label: "File", Kind: fText},
		{Key: "pass", Label: "Passphrase", Kind: fSecret},
	}, func(v map[string]string) tea.Cmd {
		f, err := os.Open(strings.TrimSpace(v["path"]))
		if err != nil {
			return w.a.fail(err)
		}
		defer f.Close()
		cs, dropped, err := config.ImportConnectionsOpts(f, v["pass"], false)
		if err != nil {
			return w.a.fail(err)
		}
		taken := map[string]bool{}
		for _, c := range w.a.d.Conns {
			taken[c.Name] = true
		}
		for _, c := range cs {
			for taken[c.Name] {
				c.Name += " (imported)"
			}
			taken[c.Name] = true
			c.Source = ""
			w.a.d.Conns = append(w.a.d.Conns, c)
		}
		if err := w.a.saveConns(); err != nil {
			return w.a.fail(err)
		}
		msg := "imported " + plural(len(cs), "connection")
		if len(dropped) > 0 { // password_cmd, bootstrap and key files from a shared file are not trusted
			msg += "; left out for safety: " + strings.Join(dropped, ", ")
		}
		return w.a.say("%s", msg)
	}).checked(func(v map[string]string) error { // a missing file or a wrong passphrase keeps the form open
		f, err := os.Open(strings.TrimSpace(v["path"]))
		if err != nil {
			return err
		}
		defer f.Close()
		_, _, err = config.ImportConnectionsOpts(f, v["pass"], false)
		return err
	}))
	return nil
}

// openAliases maps the spellings a URL may use for operation= to a filter operator.
var openAliases = map[string]string{"eq": "=", "equal": "=", "equals": "=", "ne": "<>", "neq": "<>", "!=": "<>", "gt": ">", "lt": "<", "gte": ">=", "lte": "<=",
	"contain": "Contains", "contains": "Contains", "like": "LIKE", "in": "IN", "isnull": "IS NULL", "isnotnull": "IS NOT NULL", "startswith": "Has prefix", "endswith": "Has suffix"}

// openTarget opens the table a URL named (schema, table), filtered by column / operation / value;
// condition "or" joins the filters with OR.
func (w *Workspace) openTarget(t config.OpenTarget) tea.Cmd {
	ref := db.ObjRef{Schema: t.Schema, Name: t.Table, Kind: db.KindTable}
	if ref.Schema == "" && w.caps.Schemas {
		ref.Schema = w.sess.CurrentSchema()
	}
	var use tea.Cmd
	if w.caps.Schemas && ref.Schema != w.sb.schema { // the sidebar, status line and editor follow the URL's schema
		use = w.useSchema(ref.Schema)
	}
	if t.Column == "" {
		return tea.Batch(use, w.openObject(ref, false))
	}
	if t.Column == db.RawColumn || strings.IndexFunc(t.Column, unicode.IsControl) >= 0 {
		return w.a.fail(fmt.Errorf("the URL names an invalid filter column"))
	}
	op := orDefault(t.Operation, "=")
	if a, ok := openAliases[strings.ToLower(op)]; ok {
		op = a
	}
	for _, o := range db.Operators(w.caps.Dialect) {
		if strings.EqualFold(o, op) {
			op = o
		}
	}
	return tea.Batch(use, w.openFiltered(ref, db.FilterSet{Or: strings.EqualFold(t.Condition, "or"), Filters: []db.Filter{{Column: t.Column, Op: op, Value: t.Value, Enabled: true}}}))
}
