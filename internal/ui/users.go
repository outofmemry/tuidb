// users.go: User administration: users, attributes, grants, resource limits and the SQL they produce.

package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

var (
	pgAttrs    = []string{"SUPERUSER", "CREATEDB", "CREATEROLE", "LOGIN", "REPLICATION", "BYPASSRLS"}
	mysqlPrivs = []string{"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "ALTER", "INDEX", "ALL"}
)

func userLabel(u db.User) string {
	if u.Host != "" {
		return u.Name + "@" + u.Host
	}
	return u.Name
}

func (w *Workspace) usersAction() tea.Cmd {
	ua, ok := w.sess.(db.UserAdmin)
	if !ok || !w.caps.Users {
		return w.a.say("user management is not supported by %s", w.conn.Driver)
	}
	return w.liveCmd(func() func() tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		us, err := ua.Users(ctx)
		return func() tea.Cmd {
			if err != nil {
				return w.a.fail(err)
			}
			items := []pickItem{{Label: "＋ New user…", Detail: "create"}}
			for _, u := range us {
				items = append(items, pickItem{Label: userLabel(u), Detail: fmt.Sprintf("%s · %d grant(s)", strings.Join(u.Attrs, " "), len(u.Grants)), Value: u})
			}
			// every action closes the list first: the change's result must not wait behind an open dialog
			w.a.push(&pickerDlg{title: fmt.Sprintf("Users (%d)", len(us)), filter: true, items: items, hint: "Enter: manage   Esc: close",
				onPick: func(it pickItem, _ bool) tea.Cmd {
					if u, ok := it.Value.(db.User); ok {
						return w.userMenu(ua, u)
					}
					return w.userCreate(ua)
				}})
			return nil
		}
	})
}

func (w *Workspace) isPG() bool { return w.caps.Dialect == "postgres" }

func (w *Workspace) attrItems(have []string) []checkItem {
	opts := mysqlPrivs
	if w.isPG() {
		opts = pgAttrs
	}
	var out []checkItem
	for _, o := range opts {
		on := false
		for _, h := range have {
			on = on || strings.EqualFold(h, o)
		}
		out = append(out, checkItem{Label: o, Checked: on})
	}
	return out
}

func (w *Workspace) userCreate(ua db.UserAdmin) tea.Cmd {
	f := newForm("New user", []formField{
		{Key: "name", Label: "Name", Kind: fText},
		{Key: "host", Label: "Host", Kind: fText, Value: "%", Show: func(map[string]string) bool { return !w.isPG() }},
		{Key: "pw", Label: "Password", Kind: fSecret},
	}, func(v map[string]string) tea.Cmd {
		u := db.User{Name: strings.TrimSpace(v["name"])}
		if !w.isPG() {
			u.Host = strings.TrimSpace(v["host"])
		}
		w.a.push(&checklistDlg{title: "Attributes for " + u.Name, items: w.attrItems(nil), onOK: func(items []checkItem) tea.Cmd {
			var attrs []string
			for _, it := range items {
				if it.Checked {
					attrs = append(attrs, it.Label)
				}
			}
			chs := []db.UserChange{{Kind: "create", User: u, Password: v["pw"]}}
			if w.isPG() {
				chs[0].User.Attrs = attrs
			} else if len(attrs) > 0 {
				chs = append(chs, db.UserChange{Kind: "attrs", User: db.User{Name: u.Name, Host: u.Host, Attrs: attrs}})
			}
			return w.userApply(ua, "created "+userLabel(u), chs...)
		}})
		return nil
	})
	f.validate = func(v map[string]string) error {
		if strings.TrimSpace(v["name"]) == "" {
			return fmt.Errorf("user name required")
		}
		return nil
	}
	w.a.push(f)
	return nil
}

func (w *Workspace) userMenu(ua db.UserAdmin, u db.User) tea.Cmd {
	items := []pickItem{{Label: "Details", Value: "details"}, {Label: "Change password…", Value: "password"}, {Label: "Attributes…", Value: "attrs"},
		{Label: "Grant privileges…", Value: "grant"}, {Label: "Revoke privileges…", Value: "revoke"}, {Label: "Drop user…", Value: "drop"}}
	if !w.isPG() {
		items = append(items[:3], append([]pickItem{{Label: "Resource limits…", Value: "limits"}}, items[3:]...)...)
	} else {
		items = append(items[:3], append([]pickItem{{Label: "Connection limit…", Value: "limits"}}, items[3:]...)...)
	}
	w.a.push(&pickerDlg{title: userLabel(u), items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		switch it.Value.(string) {
		case "details":
			lines := []string{"Name: " + u.Name, "Attributes: " + strings.Join(u.Attrs, " "), "", "Grants:"}
			for _, g := range u.Grants {
				lines = append(lines, "  "+g.Object+": "+grantText(g))
			}
			if len(u.Grants) == 0 {
				lines = append(lines, "  (none)")
			}
			w.a.push(&textViewDlg{title: "User " + userLabel(u), lines: lines})
		case "password":
			f := newForm("Password for "+userLabel(u), []formField{{Key: "pw", Label: "New password", Kind: fSecret}, {Key: "pw2", Label: "Repeat", Kind: fSecret}},
				func(v map[string]string) tea.Cmd {
					return w.userApply(ua, "password changed for "+userLabel(u), db.UserChange{Kind: "password", User: u, Password: v["pw"]})
				})
			f.validate = func(v map[string]string) error {
				if v["pw"] == "" || v["pw"] != v["pw2"] {
					return fmt.Errorf("the two passwords must match and not be empty")
				}
				return nil
			}
			w.a.push(f)
		case "attrs":
			w.a.push(&checklistDlg{title: "Attributes of " + userLabel(u), items: w.attrItems(u.Attrs), onOK: func(items []checkItem) tea.Cmd {
				return w.userApply(ua, "attributes changed for "+userLabel(u), w.attrChanges(u, items)...)
			}})
		case "limits":
			return w.userLimits(ua, u)
		case "grant":
			obj := "public"
			if r, ok := w.targetRef(); ok {
				obj = r.Schema + "." + r.Name
			}
			if !w.isPG() {
				obj = w.sess.CurrentDatabase() + ".*"
			}
			w.grantForm(ua, u, "grant", obj, "SELECT", "")
		case "revoke":
			if len(u.Grants) == 0 {
				return w.a.say("%s has no table grants to revoke", userLabel(u))
			}
			var gi []pickItem
			for _, g := range u.Grants {
				gi = append(gi, pickItem{Label: g.Object, Detail: grantText(g), Value: g})
			}
			w.a.push(&pickerDlg{title: "Revoke from " + userLabel(u), filter: true, items: gi, onPick: func(it pickItem, _ bool) tea.Cmd {
				g := it.Value.(db.Grant)
				w.grantForm(ua, u, "revoke", g.Object, strings.Join(g.Privs, ", "), strings.Join(g.Columns, ", "))
				return nil
			}})
		case "drop":
			f := newForm("Drop user "+userLabel(u)+"?", []formField{
				{Kind: fLabel, Value: "This cannot be undone. Type the name to confirm."},
				{Key: "confirm", Label: "Type its name", Kind: fText},
			}, func(map[string]string) tea.Cmd {
				return w.userApply(ua, "dropped "+userLabel(u), db.UserChange{Kind: "drop", User: u})
			})
			f.validate = func(v map[string]string) error {
				if v["confirm"] != u.Name {
					return fmt.Errorf("type “%s” to confirm", u.Name)
				}
				return nil
			}
			f.submit = "Enter: drop"
			w.a.push(f)
		}
		return nil
	}})
	return nil
}

func (w *Workspace) grantForm(ua db.UserAdmin, u db.User, kind, obj, privs, cols string) {
	title := map[string]string{"grant": "Grant to ", "revoke": "Revoke from "}[kind] + userLabel(u)
	f := newForm(title, []formField{
		{Key: "obj", Label: "Object", Kind: fText, Value: obj, Hint: "schema.table (PostgreSQL), db.* or db.table (MySQL)"},
		{Key: "privs", Label: "Privileges", Kind: fText, Value: privs, Hint: "comma separated"},
		{Key: "cols", Label: "Columns", Kind: fText, Value: cols, Hint: "optional: column-level (SELECT, INSERT, UPDATE, REFERENCES)"},
	}, func(v map[string]string) tea.Cmd {
		var ps []string
		for _, p := range strings.Split(v["privs"], ",") {
			if p = strings.ToUpper(strings.TrimSpace(p)); p != "" {
				ps = append(ps, p)
			}
		}
		return w.userApply(ua, kind+" done for "+userLabel(u), db.UserChange{Kind: kind, User: u, Grant: db.Grant{Object: strings.TrimSpace(v["obj"]), Privs: ps, Columns: splitList(v["cols"])}})
	})
	f.validate = func(v map[string]string) error {
		if strings.TrimSpace(v["obj"]) == "" || strings.TrimSpace(v["privs"]) == "" {
			return fmt.Errorf("object and privileges are required")
		}
		return nil
	}
	w.a.push(f)
}

// attrChanges turns the ticked boxes into changes: PostgreSQL flips flags (NOLOGIN to clear one),
// MySQL grants the new global privileges and revokes the cleared ones.
func (w *Workspace) attrChanges(u db.User, items []checkItem) []db.UserChange {
	have := map[string]bool{}
	for _, a := range u.Attrs {
		have[strings.ToUpper(a)] = true
	}
	var add, del []string
	for _, it := range items {
		switch {
		case it.Checked && !have[it.Label]:
			add = append(add, it.Label)
		case !it.Checked && have[it.Label]:
			del = append(del, it.Label)
		}
	}
	if w.isPG() {
		attrs := add
		for _, d := range del {
			attrs = append(attrs, "NO"+d)
		}
		if len(attrs) == 0 {
			return nil
		}
		return []db.UserChange{{Kind: "attrs", User: db.User{Name: u.Name, Attrs: attrs}}}
	}
	var out []db.UserChange
	if len(add) > 0 {
		out = append(out, db.UserChange{Kind: "attrs", User: db.User{Name: u.Name, Host: u.Host, Attrs: add}})
	}
	if len(del) > 0 {
		out = append(out, db.UserChange{Kind: "revoke", User: u, Grant: db.Grant{Object: "*.*", Privs: del}})
	}
	return out
}

// userApply plans the changes, shows them in a Preview and sends them through the normal commit path
// (safe mode gate, Apply, history). Enter in the preview applies.
func (w *Workspace) userApply(ua db.UserAdmin, done string, chs ...db.UserChange) tea.Cmd {
	var stmts []db.Statement
	for _, c := range chs {
		s, err := ua.PlanUser(c)
		if err != nil {
			return w.a.fail(err)
		}
		stmts = append(stmts, s...)
	}
	if len(stmts) == 0 {
		return w.a.say("no changes")
	}
	var lines []string
	for _, s := range stmts {
		lines = append(append(lines, sqlLines(s.Shown()+";")...), "")
	}
	w.a.push(&textViewDlg{title: fmt.Sprintf("Preview: %d statement(s)", len(stmts)), lines: lines, actions: []viewAction{{Key: "enter", Label: "Enter: apply", Do: func() tea.Cmd {
		return w.execute(stmts, func(err error) tea.Cmd {
			if err != nil {
				return nil
			}
			return tea.Batch(w.a.say("%s", done), w.usersAction())
		})
	}}}})
	return nil
}

func stmtTexts(stmts []db.Statement) []string {
	out := make([]string, len(stmts))
	for i, s := range stmts {
		out[i] = s.Shown()
	}
	return out
}

// grantText shows a grant's privileges: "SELECT, INSERT", or "SELECT (a, b), INSERT (a, b)" for a column grant.
func grantText(g db.Grant) string {
	if len(g.Columns) == 0 {
		return strings.Join(g.Privs, ", ")
	}
	out := make([]string, len(g.Privs))
	for i, p := range g.Privs {
		out[i] = p + " (" + strings.Join(g.Columns, ", ") + ")"
	}
	return strings.Join(out, ", ")
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var mysqlLimitNames = []string{"MAX_QUERIES_PER_HOUR", "MAX_UPDATES_PER_HOUR", "MAX_CONNECTIONS_PER_HOUR", "MAX_USER_CONNECTIONS"}

// userLimits is the four-number form; 0 means unlimited (the server default for MAX_USER_CONNECTIONS). It plans one UserChange{Kind: "limits"}.
func (w *Workspace) userLimits(ua db.UserAdmin, u db.User) tea.Cmd {
	if w.isPG() {
		return w.pgConnLimit(ua, u)
	}
	if u.Limits == nil { // the max_* columns could not be read: showing zeros and saving them would clear the quotas
		return w.a.say("resource limits could not be read for %s", userLabel(u))
	}
	var fs []formField
	for _, n := range mysqlLimitNames {
		hint := "0 = unlimited"
		if n == "MAX_USER_CONNECTIONS" {
			hint = "0 = the server default" // not "no limit": the global max_user_connections applies
		}
		fs = append(fs, formField{Key: n, Label: strings.ToLower(strings.ReplaceAll(n, "_", " ")), Kind: fText, Value: strconv.Itoa(u.Limits[n]), Hint: hint})
	}
	f := newForm("Resource limits of "+userLabel(u), fs, func(v map[string]string) tea.Cmd {
		lim := map[string]int{}
		for _, n := range mysqlLimitNames {
			x, _ := strconv.Atoi(strings.TrimSpace(v[n]))
			lim[n] = x
		}
		return w.userApply(ua, "limits changed for "+userLabel(u), db.UserChange{Kind: "limits", User: db.User{Name: u.Name, Host: u.Host, Limits: lim}})
	})
	f.validate = func(v map[string]string) error {
		for _, n := range mysqlLimitNames {
			if x, err := strconv.Atoi(strings.TrimSpace(v[n])); err != nil || x < 0 {
				return fmt.Errorf("%s must be a whole number, 0 or more", n)
			}
		}
		return nil
	}
	w.a.push(f)
	return nil
}

// pgConnLimit is the Postgres role's CONNECTION LIMIT (-1 = no limit); it plans one UserChange{Kind: "limits"}.
func (w *Workspace) pgConnLimit(ua db.UserAdmin, u db.User) tea.Cmd {
	cur, ok := u.Limits["CONNECTION_LIMIT"]
	if !ok {
		cur = -1
	}
	f := newForm("Connection limit of "+userLabel(u), []formField{{Key: "n", Label: "connection limit", Kind: fText, Value: strconv.Itoa(cur), Hint: "-1 = no limit"}},
		func(v map[string]string) tea.Cmd {
			n, _ := strconv.Atoi(strings.TrimSpace(v["n"]))
			return w.userApply(ua, "connection limit changed for "+userLabel(u), db.UserChange{Kind: "limits", User: db.User{Name: u.Name, Host: u.Host, Limits: map[string]int{"CONNECTION_LIMIT": n}}})
		})
	f.validate = func(v map[string]string) error {
		if x, err := strconv.Atoi(strings.TrimSpace(v["n"])); err != nil || x < -1 {
			return fmt.Errorf("the connection limit must be a whole number, -1 or more")
		}
		return nil
	}
	w.a.push(f)
	return nil
}
