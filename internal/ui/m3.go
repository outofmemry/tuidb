package ui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"webtuidb/internal/ai"
	"webtuidb/internal/board"
	"webtuidb/internal/db"
	"webtuidb/internal/diagram"
	"webtuidb/internal/keymap"
	"webtuidb/internal/plugin"
	"webtuidb/internal/store"
)

// m3State is the per-workspace state of the milestone-3 features.
type m3State struct {
	// go back / forward: hist[:cur] is the path, hist[cur-1] the open object
	hist []db.ObjRef
	cur  int
	nav  bool // a back/forward is opening an object: do not record it

	// split pane: tabs stay in Workspace.tabs, grp says which group (0 left, 1 right) each is in.
	// Workspace.cur is always the active tab of the focused group (pane); otherID is the other group's.
	split   bool
	pane    int
	otherID int
	grp     map[int]int

	chat     []ai.Message // the assistant conversation lives as long as the workspace
	model    string       // model picked in the assistant for this session; "" = [ai] model
	provider string       // [ai.providers.<name>] picked for the chat; "" = the default
	curChat  *store.Chat  // the saved record of chat (nil until the first exchange)
}

func (m *m3State) reset() {
	*m = m3State{chat: m.chat, model: m.model, provider: m.provider, curChat: m.curChat}
}

func (m *m3State) place(t tabber) {
	if m.split {
		m.grp[t.ID()] = m.pane
	}
}

func (m *m3State) visit(ref db.ObjRef) {
	if m.nav || m.cur > 0 && m.hist[m.cur-1] == ref {
		return
	}
	m.hist = append(m.hist[:m.cur], ref)
	if len(m.hist) > 50 {
		m.hist = m.hist[1:]
	}
	m.cur = len(m.hist)
}

func (w *Workspace) pane(t tabber) int {
	if !w.m3.split {
		return 0
	}
	return w.m3.grp[t.ID()]
}

// groupIdx lists the indexes (into w.tabs) of the tabs in group g.
func (w *Workspace) groupIdx(g int) []int {
	var out []int
	for i, t := range w.tabs {
		if w.pane(t) == g {
			out = append(out, i)
		}
	}
	return out
}

func (w *Workspace) indexByID(id int) int {
	for i, t := range w.tabs {
		if t.ID() == id {
			return i
		}
	}
	return -1
}

func (w *Workspace) stepTab(d int) {
	idx := w.groupIdx(w.m3.pane)
	for p, i := range idx {
		if i == w.cur {
			w.selectTab(idx[(p+d+len(idx))%len(idx)])
			return
		}
	}
	w.selectTab(w.cur + d)
}

// fixSplit repairs the split after tabs moved or closed: an empty group closes the split, the active
// tab is in the focused group and otherID names a tab of the other one.
func (w *Workspace) fixSplit() {
	m := &w.m3
	if !m.split {
		return
	}
	l, r := w.groupIdx(m.pane), w.groupIdx(1-m.pane)
	if len(l) == 0 || len(r) == 0 {
		m.split, m.pane, m.grp = false, 0, nil
		return
	}
	if w.cur < 0 || w.cur >= len(w.tabs) || w.pane(w.tabs[w.cur]) != m.pane {
		w.cur = l[len(l)-1]
	}
	if o := w.indexByID(m.otherID); o < 0 || w.pane(w.tabs[o]) == m.pane {
		m.otherID = w.tabs[r[len(r)-1]].ID()
	}
}

func (w *Workspace) splitRight() tea.Cmd {
	t, m := w.tab(), &w.m3
	if t == nil {
		return w.a.say("open a tab first")
	}
	if !m.split {
		if len(w.tabs) < 2 {
			return w.a.say("open a second tab to split the pane")
		}
		m.split, m.grp = true, map[int]int{}
		for _, x := range w.tabs {
			m.grp[x.ID()] = 0
		}
		m.grp[t.ID()], m.pane = 1, 1
		m.otherID = -1
	} else { // move the active tab to the other group; an emptied group closes the split
		m.grp[t.ID()] = 1 - m.pane
		m.pane = 1 - m.pane
		m.otherID = -1
	}
	w.fixSplit()
	return nil
}

// closePane closes the focused split pane: its tabs join the other group (no tab is closed).
func (w *Workspace) closePane() tea.Cmd {
	m := &w.m3
	if !m.split {
		return w.a.say("there is one pane: nothing to close")
	}
	if o := w.indexByID(m.otherID); o >= 0 { // the other pane's active tab stays the active one
		w.cur = o
	}
	m.split, m.grp, m.pane, m.otherID = false, nil, 0, -1
	w.focusSB = false
	return nil
}

func (w *Workspace) cyclePane() tea.Cmd {
	m := &w.m3
	o := w.indexByID(m.otherID)
	if !m.split || o < 0 || w.tab() == nil {
		return w.a.say("there is one pane: %s \" splits it", keymap.Display(w.a.d.Keys.Prefix))
	}
	m.otherID, w.cur, m.pane = w.tab().ID(), o, 1-m.pane
	w.focusSB = false
	return nil
}

// splitFrames draws the two groups side by side.
func (w *Workspace) splitFrames(th Theme, W, H int) string {
	lw := (W - 1) / 2
	var frames []string
	for g, fw := range []int{lw, W - 1 - lw} {
		i := w.cur
		if w.m3.pane != g {
			i = w.indexByID(w.m3.otherID)
		}
		var t tabber
		if i >= 0 && i < len(w.tabs) {
			t = w.tabs[i]
		}
		mine := w.m3.pane == g
		frames = append(frames, w.paneFrame(th, t, g, fw, H, mine && !w.focusSB && !w.focusDetail, mine && !w.focusSB))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, frames[0], " ", frames[1])
}

// goHist re-opens the previous (d=-1) or next (d=1) object.
func (w *Workspace) goHist(d int) tea.Cmd {
	m := &w.m3
	n := m.cur + d
	if n < 1 || n > len(m.hist) {
		return w.a.say("nothing to go %s to", map[int]string{-1: "back", 1: "forward"}[d])
	}
	m.cur, m.nav = n, true
	defer func() { m.nav = false }()
	return w.openObject(m.hist[n-1], false)
}

func (w *Workspace) m3Action(name string) tea.Cmd {
	switch name {
	case "users":
		return w.usersAction()
	case "diagram":
		return w.diagramAction("")
	case "metrics":
		return w.metricsAction()
	case "backup", "restore":
		return w.backupAction(name == "restore")
	case "split_right":
		return w.splitRight()
	case "close_pane":
		return w.closePane()
	case "assistant_new":
		return w.assistantNew()
	case "assistant_clear":
		return w.assistantClear()
	case "assistant_history":
		return w.assistantHistory()
	case "next_pane", "prev_pane": // two groups: next and previous are the same move
		return w.cyclePane()
	case "assistant":
		return w.assistantAction()
	case "plugins":
		return w.pluginsAction("")
	case "go_back":
		return w.goHist(-1)
	case "go_forward":
		return w.goHist(1)
	case "mcp_log":
		return w.mcpLog()
	}
	return nil
}

// ---- bootstrap --------------------------------------------------------------------------

// boot runs the connection's bootstrap statements, then next(). A failing statement is shown, not fatal.
func (w *Workspace) boot(next func() tea.Cmd) tea.Cmd {
	stmts, sess := w.conn.Bootstrap, w.sess
	if len(stmts) == 0 {
		return next()
	}
	return liveCmd(func() func() tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		type done struct {
			sql string
			dur time.Duration
			err error
		}
		var out []done
		for _, s := range stmts {
			start := time.Now()
			_, err := sess.Exec(ctx, s, 1)
			out = append(out, done{s, time.Since(start), err})
		}
		return func() tea.Cmd {
			var bad []string
			for _, d := range out {
				w.log(d.sql, d.dur, d.err)
				if d.err != nil {
					bad = append(bad, d.sql+"\n  "+d.err.Error())
				}
			}
			if len(bad) > 0 {
				w.a.errorDialog("Bootstrap command failed (the connection is still open)", fmt.Errorf("%s", strings.Join(bad, "\n")))
			}
			return next()
		}
	})
}

// ---- users ------------------------------------------------------------------------------

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
					return fmt.Errorf("type %q to confirm", u.Name)
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
		lines = append(lines, s.Shown()+";", "")
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

// ---- backup / restore -------------------------------------------------------------------

// ---- diagram ----------------------------------------------------------------------------

// paneDlg is a big scrollable text window with its own keys (diagram, metrics board).
type paneDlg struct {
	title, hint string
	lines       func(th Theme, W int) []string
	keys        map[string]func() tea.Cmd
	top, left   int
	done        bool // a key handler sets this to close the window
	onClose     func()
}

func (d *paneDlg) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}
	ks := k.Keystroke()
	if f, ok := d.keys[ks]; ok {
		cmd := f()
		if d.done {
			d.close()
			return nil, cmd
		}
		return d, cmd
	}
	switch ks {
	case "esc", "q":
		d.close()
		return nil, nil
	case "down", "j":
		d.top++
	case "up", "k":
		d.top = max(0, d.top-1)
	case "pgdown", "space":
		d.top += 10
	case "pgup":
		d.top = max(0, d.top-10)
	case "home", "g":
		d.top, d.left = 0, 0
	case "end", "G":
		d.top = 1 << 30
	case "right", "l":
		d.left += 8
	case "left", "h":
		d.left = max(0, d.left-8)
	}
	return d, nil
}

func (d *paneDlg) close() {
	if d.onClose != nil {
		d.onClose()
	}
}

func (d *paneDlg) View(th Theme, maxW, maxH int) string {
	w, vis := maxW-2, max(4, maxH-6)
	all := d.lines(th, w-4)
	d.top = clamp(d.top, 0, max(0, len(all)-vis))
	body := all[d.top:min(len(all), d.top+vis)]
	out := make([]string, len(body))
	for i, l := range body {
		out[i] = fit(ansi.TruncateLeft(l, d.left, ""), w-4)
	}
	hint := d.hint
	if len(all) > vis {
		hint += fmt.Sprintf("   (%d-%d/%d)", d.top+1, d.top+len(body), len(all))
	}
	return box(th, d.title, out, hint, w)
}

var clusterColors = []string{"39", "208", "41", "170", "220", "203"}

var headerRe = regexp.MustCompile(`\| [^|]+ +\|`)

// colorHeaders tints each table's header row (the line after a box's top rule) by its FK cluster, or by the
// colour override (ANSI code per table name) when there is one.
func colorHeaders(text string, byName map[string]int, override ...map[string]string) []string {
	var ov map[string]string
	if len(override) > 0 {
		ov = override[0]
	}
	lines := strings.Split(text, "\n")
	start, hdr := true, -1
	for i, l := range lines {
		if l == "" {
			start = true
			continue
		}
		if start {
			start, hdr = false, i+1
		}
		if i != hdr {
			continue
		}
		lines[i] = headerRe.ReplaceAllStringFunc(l, func(m string) string {
			name := strings.TrimSpace(m[1 : len(m)-1])
			col := ""
			if c, ok := byName[name]; ok {
				col = clusterColors[c%len(clusterColors)]
			}
			if c := ov[name]; c != "" {
				col = c
			}
			if col == "" {
				return m
			}
			return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("16")).Background(lipgloss.Color(col)).Render(m)
		})
	}
	return lines
}

// diagramAction opens the ER diagram; focus (a table name) presets the filter to that table and scrolls to it.
func (w *Workspace) diagramAction(focus string) tea.Cmd { return w.diagramFor(w.sb.schema, focus) }

func (w *Workspace) showDiagram(tbls []diagram.Table, schema, focus string, schemas []string) {
	sortBy, filter := "name", focus
	cl := diagram.Cluster(tbls)
	byName := map[string]int{}
	for _, t := range tbls {
		byName[t.Ref.Name] = cl[t.Ref.Schema+"."+t.Ref.Name]
	}
	colors := w.diagramColors() // "schema.table" -> ANSI code
	d := &paneDlg{hint: "↑↓←→: scroll  s: sort  /: filter  e: edit table  n: new table  a: add FK  x: delete table  S: schema  y: copy column  c: colour  Esc: close"}
	retitle := func() {
		d.title = fmt.Sprintf("ER diagram · %s · %d tables · sort: %s", orDefault(schema, w.sess.CurrentDatabase()), len(tbls), sortBy)
		if filter != "" {
			d.title += " · filter: " + filter
		}
	}
	retitle()
	d.lines = func(th Theme, W int) []string {
		ov := map[string]string{}
		for _, t := range tbls {
			if c := colors[t.Ref.Schema+"."+t.Ref.Name]; c != "" {
				ov[t.Ref.Name] = c
			}
		}
		lines := colorHeaders(diagram.Render(tbls, max(40, W), sortBy, filter), byName, ov)
		if focus != "" { // centre the focused table once: its box is the first one naming it
			for i, l := range lines {
				if strings.Contains(ansi.Strip(l), "| "+focus+" ") {
					d.top = max(0, i-3)
					break
				}
			}
			focus = ""
		}
		return lines
	}
	reopen := func() tea.Cmd { return w.diagramFor(schema, "") }
	handover := func(f func() tea.Cmd) func() tea.Cmd {
		return func() tea.Cmd { d.done = true; return f() }
	}
	switchSchema := handover(func() tea.Cmd {
		return w.diagramSchemaPicker(schemas, schema, func(s string) tea.Cmd { return w.diagramFor(s, "") })
	})
	d.keys = map[string]func() tea.Cmd{
		"s": func() tea.Cmd {
			sortBy = map[string]string{"name": "columns", "columns": "name"}[sortBy]
			retitle()
			return nil
		},
		"/": func() tea.Cmd {
			w.a.push(newForm("Filter tables", []formField{{Key: "f", Label: "Name contains", Kind: fText, Value: filter, Hint: "empty = all; FK neighbours stay"}}, func(v map[string]string) tea.Cmd {
				filter, d.top, d.left = strings.TrimSpace(v["f"]), 0, 0
				retitle()
				return nil
			}))
			return nil
		},
		// the structure editor adds columns and keys, so these keys hand over to it
		"n": func() tea.Cmd { d.done = true; return w.newTableDialog() },
		"e": func() tea.Cmd {
			d.done = true
			var items []pickItem
			for _, t := range tbls {
				items = append(items, pickItem{Label: t.Ref.Name, Detail: fmt.Sprintf("%d columns · %d foreign keys", len(t.Columns), len(t.FKs)), Value: t.Ref})
			}
			w.a.push(&pickerDlg{title: "Edit structure (add column / foreign key / drop)", filter: true, items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
				return w.openStructure(it.Value.(db.ObjRef))
			}})
			return nil
		},
		"a":       handover(func() tea.Cmd { return w.diagramAddFK(tbls, schema, reopen) }),
		"x":       handover(func() tea.Cmd { return w.diagramDropTable(tbls, reopen) }),
		"S":       switchSchema,
		"shift+S": switchSchema,
		"y":       func() tea.Cmd { return w.diagramCopyColumn(tbls) },
		"c":       func() tea.Cmd { return w.diagramSetColor(tbls, schema, colors) },
	}
	w.a.push(d)
}

// ---- metrics boards: see m7_board.go ------------------------------------------------

// addToBoard saves the charted result as a one-panel board.
func (w *Workspace) addToBoard(rv *resultView) tea.Cmd {
	spec, _ := db.ParseChart(rv.stmt)
	if spec.Kind == "" {
		spec.Kind = "bar"
	}
	w.a.push(newForm("Add to Metrics Board", []formField{{Key: "name", Label: "Board name", Kind: fText}}, func(v map[string]string) tea.Cmd {
		name := strings.TrimSpace(v["name"])
		if name == "" {
			return w.a.fail(fmt.Errorf("a board needs a name"))
		}
		nb := board.FromChart(name, rv.stmt, spec)
		if err := nb.Save(); err != nil {
			return w.a.fail(err)
		}
		return w.a.say("saved board %s: open it with the metrics key", name)
	}))
	return nil
}

// ---- plugins ----------------------------------------------------------------------------

func pluginNames() []string {
	ps, _ := plugin.List()
	var out []string
	for _, p := range ps {
		if p.Err == nil {
			out = append(out, p.Name)
		}
	}
	return out
}

func (w *Workspace) pluginContext() string {
	switch w.context() {
	case keymap.Editor:
		return "editor"
	case keymap.Grid, keymap.Results:
		return "grid"
	case keymap.Sidebar:
		return "sidebar"
	}
	return ""
}

// pluginsAction picks a plugin that applies to the focused pane (or runs the named one) and runs it.
func (w *Workspace) pluginsAction(name string) tea.Cmd {
	ps, err := plugin.List()
	if err != nil {
		return w.a.fail(err)
	}
	ctxName := w.pluginContext()
	var items []pickItem
	for _, p := range ps {
		fits := len(p.Contexts) == 0
		for _, c := range p.Contexts {
			fits = fits || c == ctxName
		}
		switch {
		case p.Err != nil:
			continue
		case name != "" && p.Name != name, name == "" && !fits:
			continue
		}
		items = append(items, pickItem{Label: p.Name, Detail: p.Output + " · " + p.Command, Value: p})
	}
	if name != "" && len(items) == 1 {
		return w.runPlugin(items[0].Value.(plugin.Plugin), ctxName)
	}
	w.a.push(&pickerDlg{title: "Plugins (" + orDefault(ctxName, "workspace") + ")", items: items, empty: "no plugins for this pane in " + plugin.Dir(),
		onPick: func(it pickItem, _ bool) tea.Cmd { return w.runPlugin(it.Value.(plugin.Plugin), ctxName) }})
	return nil
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
	return w.liveCmd(func() func() tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		out, err := p.Run(ctx, in)
		return func() tea.Cmd {
			switch {
			case err != nil:
				return w.a.fail(err)
			case strings.TrimSpace(out) == "":
				return w.a.say("%s printed nothing", p.Name)
			case p.Output == "sql":
				if q, ok := w.tab().(*queryTab); ok && !w.focusSB {
					q.ed.Handle(tea.PasteMsg{Content: strings.TrimRight(out, "\n")})
					return tea.Batch(q.afterEdit(), w.a.say("%s: inserted into the editor", p.Name))
				}
				return w.newQueryTab(strings.TrimRight(out, "\n"))
			}
			w.a.push(&textViewDlg{title: p.Name, lines: strings.Split(strings.TrimRight(out, "\n"), "\n"), actions: []viewAction{{Key: "c", Label: "c: copy", Do: func() tea.Cmd { return setClip(out) }}}})
			return nil
		}
	})
}

// ---- MCP --------------------------------------------------------------------------------
