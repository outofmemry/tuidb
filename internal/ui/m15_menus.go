package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/menus"
	"webtuidb/internal/store"
)

// M14.4: every context menu of this file is built from internal/menus, so the web shows the same labels in the
// same order (a test compares them). A menu is a pickerDlg whose rows are menus.Filter(list, env); the dispatch
// is on Item.Action.

// capsMap names the capability booleans the way menus.Item.When does ("Triggers" for "cap:Triggers").
func capsMap(c db.Capabilities) map[string]bool {
	out := map[string]bool{}
	v := reflect.ValueOf(c)
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Kind() == reflect.Bool {
			out[v.Type().Field(i).Name] = v.Field(i).Bool()
		}
	}
	return out
}

// menuRows renders the items that hold in env; an item the terminal cannot do stays in the list, dimmed, with
// the reason (menus.Item.UI == "web").
func menuRows(list []menus.Item, env menus.Env) []pickItem {
	var rows []pickItem
	for _, it := range menus.Filter(list, env) {
		row := pickItem{Label: it.Label, Value: it}
		if it.UI == "web" {
			row.Dim, row.Detail = true, "web only: "+it.Why
		}
		rows = append(rows, row)
	}
	return rows
}

// pushMenu opens a context menu; do receives the chosen item (never a web-only one: that only explains itself).
func (a *App) pushMenu(title string, list []menus.Item, env menus.Env, do func(it menus.Item) tea.Cmd) {
	a.push(&pickerDlg{title: title, items: menuRows(list, env), onPick: func(row pickItem, _ bool) tea.Cmd {
		it := row.Value.(menus.Item)
		if it.UI == "web" {
			return a.say("%s: web only, %s", it.Label, it.Why)
		}
		return do(it)
	}})
}

// ---- sidebar: objects -------------------------------------------------------------------------------

func (s *sidebar) objectEnv(r sbRow) menus.Env {
	ref := r.obj.ObjRef
	return menus.Env{Kind: string(ref.Kind), Caps: capsMap(s.w.caps), Marked: s.marks[objKey(ref)],
		InGroup: r.group != "" || len(s.groupsOf(objKey(ref))) > 0}
}

// objectMenu is the menu on a table, view, function... (menus.SidebarObject).
func (s *sidebar) objectMenu(r sbRow) tea.Cmd {
	s.w.a.pushMenu(r.obj.Name, menus.SidebarObject, s.objectEnv(r), func(it menus.Item) tea.Cmd { return s.objectAction(it.Action, r) })
	return nil
}

func (s *sidebar) objectAction(act string, r sbRow) tea.Cmd {
	ref := r.obj.ObjRef
	w := s.w
	switch act {
	case "open":
		return w.openObject(ref, false)
	case "open_new_tab":
		return w.openObject(ref, true)
	case "new_table":
		return w.newTableDialog()
	case "show_structure":
		return w.openStructure(ref)
	case "edit_definition":
		return w.editDefinition(ref)
	case "triggers":
		return w.triggerMenu(ref)
	case "show_ddl":
		return w.showDDL(ref)
	case "copy_name":
		return tea.Batch(setClip(ref.Name), w.a.say("copied %s", ref.Name))
	case "script_create", "script_drop", "script_truncate", "script_select100":
		kind := db.ScriptKind(strings.TrimPrefix(act, "script_"))
		return w.async(-1, func(ctx context.Context, sess db.Session) tea.Msg {
			text, err := sess.Script(ctx, kind, ref)
			return scriptMsg{text: text, err: err}
		})
	case "export":
		return w.exportAction()
	case "export_many":
		return w.pickExportTables(&ref)
	case "import":
		return w.importWizard(ref, true)
	case "clone":
		return s.cloneDialog(ref)
	case "rename":
		return s.renameDialog(ref)
	case "truncate":
		return s.adminOp(db.OpTruncate)
	case "delete_item":
		return s.adminOp(db.OpDropObject)
	case "pin":
		return s.togglePin()
	case "mark":
		return s.toggleMark()
	case "group_add":
		return s.addToGroup(ref)
	case "group_remove":
		return s.leaveGroup(r)
	case "diagram":
		return w.diagramAction(ref.Name)
	case "set_color":
		return s.pickItemColour(ref)
	case "new_schema":
		return s.newSchema()
	case "drop_schema":
		return s.dropSchema()
	}
	return w.a.say("%q is not available here", act)
}

// leaveGroup removes ref from the group it is shown under, or asks which when it is in several.
func (s *sidebar) leaveGroup(r sbRow) tea.Cmd {
	ref := r.obj.ObjRef
	groups := s.groupsOf(objKey(ref))
	if r.group != "" {
		return s.removeFromGroup(ref, r.group)
	}
	switch len(groups) {
	case 0:
		return s.w.a.say("%s is in no group", ref.Name)
	case 1:
		return s.removeFromGroup(ref, groups[0])
	}
	var items []pickItem
	for _, g := range groups {
		items = append(items, pickItem{Label: g, Value: g})
	}
	s.w.a.push(&pickerDlg{title: "Remove " + ref.Name + " from", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		return s.removeFromGroup(ref, it.Value.(string))
	}})
	return nil
}

// pickItemColour is "Set colour…": the same per-table colour the diagram uses (store.State.DiagramColors,
// key "schema.table"), so a table has one colour in both places.
func (s *sidebar) pickItemColour(ref db.ObjRef) tea.Cmd {
	key := objKey(ref)
	items := []pickItem{{Label: "default", Detail: "no colour", Value: ""}}
	for _, c := range diagramPalette[1:] { // the same palette the diagram's `c` offers; the value stored is its ANSI code
		items = append(items, pickItem{Label: c.Name, Detail: c.Code, Value: c.Code})
	}
	s.w.a.push(&pickerDlg{title: "Colour of " + ref.Name, items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		st, conn := s.w.a.d.State, s.w.conn.Name
		cols := s.w.diagramColors() // migrated to codes
		if code := it.Value.(string); code == "" {
			delete(cols, key)
		} else {
			cols[key] = code
		}
		if err := st.SetDiagramColors(conn, cols); err != nil {
			return s.w.a.fail(err)
		}
		return s.w.a.say("colour of %s saved", ref.Name)
	}})
	return nil
}

// itemColour is the colour set for an object ("" = none); the sidebar paints a dot with it.
func (s *sidebar) itemColour(o db.ObjRef) string {
	return colourCode(s.w.a.d.State.DiagramColors(s.w.conn.Name)[objKey(o)])
}

// ---- sidebar: saved queries and history ---------------------------------------------------------------

func (s *sidebar) queryMenu(q store.Query) tea.Cmd {
	list := menus.SidebarQuery
	if q.Shared { // a shared folder is read-only
		list = nil
		for _, it := range menus.SidebarQuery {
			if it.Action != "rename" && it.Action != "move" && it.Action != "delete_item" {
				list = append(list, it)
			}
		}
	}
	s.w.a.pushMenu(q.Name, list, menus.Env{Kind: "query"}, func(it menus.Item) tea.Cmd {
		w := s.w
		switch it.Action {
		case "open":
			return w.openSaved(q)
		case "open_new_tab":
			return w.openSaved(q)
		case "insert":
			return w.insertIntoEditor(q.SQL)
		case "rename":
			return s.renameQuery(q)
		case "move":
			return s.moveQuery(q)
		case "reveal":
			return w.reveal(s.queryFile(q))
		case "delete_item":
			w.a.push(&confirmDlg{title: "Delete saved query " + q.Name + "?", yes: "Delete", danger: true, onYes: func() tea.Cmd {
				if err := w.a.d.Queries.Delete(q.Path); err != nil {
					return w.a.fail(err)
				}
				s.queries, _ = w.a.d.Queries.List()
				return nil
			}})
		}
		return nil
	})
	return nil
}

// queryFile is the absolute path of a saved query's file.
func (s *sidebar) queryFile(q store.Query) string {
	if q.Shared || filepath.IsAbs(q.Path) {
		return q.Path
	}
	return filepath.Join(s.w.a.d.Queries.Root, filepath.FromSlash(q.Path))
}

func (s *sidebar) moveQuery(q store.Query) tea.Cmd {
	s.w.a.push(newForm("Move "+q.Name, []formField{{Key: "folder", Label: "Folder", Kind: fText, Value: q.Folder, Hint: "empty: the top level"}}, func(v map[string]string) tea.Cmd {
		if err := s.w.a.d.Queries.Move(q.Path, strings.Trim(v["folder"], "/ ")); err != nil {
			return s.w.a.fail(err)
		}
		s.queries, _ = s.w.a.d.Queries.List()
		return s.w.a.say("moved %s", q.Name)
	}))
	return nil
}

// reveal shows path in the file manager (store.Reveal); where that fails the path is in the status line.
func (w *Workspace) reveal(path string) tea.Cmd {
	if err := store.Reveal(path); err != nil {
		return w.a.say("could not open the file manager (%v): %s", err, path)
	}
	return nil
}

func (s *sidebar) historyMenu(h store.HistoryEntry) tea.Cmd {
	conn := s.w.conn.Name
	s.w.a.pushMenu("History", menus.SidebarHistory, menus.Env{Kind: "history"}, func(it menus.Item) tea.Cmd {
		w := s.w
		switch it.Action {
		case "insert":
			return w.insertIntoEditor(h.SQL)
		case "open_new_tab":
			return w.newQueryTab(h.SQL)
		case "add_to_queries":
			return s.addHistoryToQueries(h.SQL)
		case "copy":
			return setClip(h.SQL)
		case "clear_all":
			w.a.push(&confirmDlg{title: "Clear all history of " + conn + "?", lines: []string{"This cannot be undone."}, yes: "Clear", danger: true, onYes: func() tea.Cmd {
				if err := w.a.d.Hist.Clear(conn); err != nil {
					return w.a.fail(err)
				}
				s.hist, s.sel, s.top = nil, 0, 0
				return w.a.say("history cleared")
			}})
		}
		return nil
	})
	return nil
}

// ---- sidebar: "+" ---------------------------------------------------------------------------------------

// newMenu is the "+" menu (key + in the sidebar, or the Open Anything entry "New…").
func (s *sidebar) newMenu() tea.Cmd {
	s.w.a.pushMenu("New", menus.SidebarNew, menus.Env{Caps: capsMap(s.w.caps)}, func(it menus.Item) tea.Cmd {
		w := s.w
		switch it.Action {
		case "new_table":
			return w.newTableDialog()
		case "new_view":
			return w.newQueryTab(s.createTemplate("view"))
		case "new_matview":
			return w.newQueryTab(s.createTemplate("matview"))
		case "new_function":
			return w.newQueryTab(s.createTemplate("function"))
		case "new_schema":
			return s.newSchema()
		case "new_database":
			return s.newDatabase()
		case "new_group":
			return s.newGroup()
		}
		return nil
	})
	return nil
}

// createTemplate is the CREATE statement a "+" entry opens in a SQL tab, in the dialect of the connection.
func (s *sidebar) createTemplate(kind string) string {
	name := "new_" + kind
	if kind == "matview" {
		name = "new_matview"
	}
	if s.w.caps.Schemas && s.schema != "" {
		name = s.schema + "." + name
	}
	switch s.w.caps.Dialect {
	case "postgres":
		switch kind {
		case "view":
			return "CREATE VIEW " + name + " AS\nSELECT 1 AS id;\n"
		case "matview":
			return "CREATE MATERIALIZED VIEW " + name + " AS\nSELECT 1 AS id\nWITH DATA;\n"
		}
		return "CREATE OR REPLACE FUNCTION " + name + "(arg integer)\nRETURNS integer\nLANGUAGE plpgsql AS $$\nBEGIN\n    RETURN arg;\nEND;\n$$;\n"
	case "mysql":
		if kind == "function" {
			return "CREATE FUNCTION " + name + "(arg INT)\nRETURNS INT\nDETERMINISTIC\nRETURN arg;\n"
		}
	case "clickhouse":
		if kind == "matview" {
			return "CREATE MATERIALIZED VIEW " + name + "\nENGINE = MergeTree ORDER BY tuple()\nAS SELECT 1 AS id;\n"
		}
	}
	if kind == "function" {
		return "CREATE FUNCTION " + name + "(arg INT) RETURNS INT\nRETURN arg;\n"
	}
	return "CREATE " + map[string]string{"view": "VIEW", "matview": "MATERIALIZED VIEW"}[kind] + " " + name + " AS\nSELECT 1 AS id;\n"
}

// newGroup names a custom item group and puts the marked tables (or the one under the cursor) in it.
// shortcut: a group without items is not kept (State.Groups drops empty ones), so it needs at least one object.
func (s *sidebar) newGroup() tea.Cmd {
	refs := s.markedRefs()
	if r, ok := s.current(); ok && r.kind == sbObject && len(refs) == 0 {
		refs = []db.ObjRef{r.obj.ObjRef}
	}
	if len(refs) == 0 {
		return s.w.a.say("select an object (or mark some) first: a group is made from the items you put in it")
	}
	s.w.a.push(newForm("New item group", []formField{{Key: "name", Label: "Name", Kind: fText}}, func(v map[string]string) tea.Cmd {
		name := strings.TrimSpace(v["name"])
		if name == "" {
			return nil
		}
		err := s.editGroups(func(gs []store.ItemGroup) []store.ItemGroup {
			for i := range gs {
				if gs[i].Name == name {
					for _, ref := range refs {
						if !contains(gs[i].Items, objKey(ref)) {
							gs[i].Items = append(gs[i].Items, objKey(ref))
						}
					}
					return gs
				}
			}
			g := store.ItemGroup{Name: name}
			for _, ref := range refs {
				g.Items = append(g.Items, objKey(ref))
			}
			return append(gs, g)
		})
		if err != nil {
			return s.w.a.fail(err)
		}
		return s.w.a.say("group %s has %s", name, plural(len(refs), "item"))
	}))
	return nil
}

// ---- database picker --------------------------------------------------------------------------------------

func (s *sidebar) databaseMenu(name string) tea.Cmd {
	s.w.a.pushMenu(name, menus.Database, menus.Env{Kind: "database", Caps: capsMap(s.w.caps)}, func(it menus.Item) tea.Cmd {
		switch it.Action {
		case "open":
			return s.w.openDatabase(name)
		case "open_workspace":
			c := s.w.conn
			c.Database = name
			return s.w.a.openConnection(c)
		case "copy_name":
			return tea.Batch(setClip(name), s.w.a.say("copied %s", name))
		case "new_database":
			return s.newDatabase()
		case "rename":
			if name == s.w.sess.CurrentDatabase() {
				return s.w.a.say("the open database cannot be renamed; open another one first")
			}
			s.w.a.push(newForm("Rename "+name, []formField{{Key: "new_name", Label: "New name", Kind: fText, Value: name}}, func(v map[string]string) tea.Cmd {
				return s.databaseAdmin(db.OpRenameDatabase, name, map[string]string{"new_name": strings.TrimSpace(v["new_name"])}, "renamed")
			}))
		case "drop":
			return s.dropDatabase(name)
		}
		return nil
	})
	return nil
}

func (s *sidebar) dropDatabase(name string) tea.Cmd {
	if name == s.w.sess.CurrentDatabase() {
		return s.w.a.say("the open database cannot be dropped; open another one first")
	}
	f := newForm("Drop "+name+"?", []formField{
		{Key: "warn", Label: "", Kind: fLabel, Value: "This deletes the database and everything in it."},
		{Key: "confirm", Label: "Type its name", Kind: fText},
	}, func(map[string]string) tea.Cmd { return s.databaseAdmin(db.OpDropDatabase, name, nil, "dropped") })
	f.validate = func(v map[string]string) error {
		if v["confirm"] != name {
			return fmt.Errorf("type “%s” to confirm", name)
		}
		return nil
	}
	f.submit = "Enter: drop"
	s.w.a.push(f)
	return nil
}

// ---- tab bar ------------------------------------------------------------------------------------------------

// tabMenu is the menu of tab i (right click on the tab bar, or the Open Anything entry "Tab menu").
func (w *Workspace) tabMenu(i int) tea.Cmd {
	if i < 0 || i >= len(w.tabs) {
		return w.a.say("no tab to open a menu for")
	}
	w.a.pushMenu(w.tabTitle(w.tabs[i]), menus.Tab, menus.Env{}, func(it menus.Item) tea.Cmd {
		w.selectTab(i)
		return w.action(it.Action)
	})
	return nil
}

// ---- welcome: connection menu -------------------------------------------------------------------------------

func (w *welcome) menu() tea.Cmd {
	r, ok := w.current()
	if !ok {
		return nil
	}
	c := r.conn
	w.a.pushMenu(c.Name, menus.Welcome, menus.Env{}, func(it menus.Item) tea.Cmd {
		switch it.Action {
		case "connect":
			return w.a.openConnection(c)
		case "edit":
			return w.edit()
		case "duplicate":
			return w.duplicate()
		case "delete_item":
			return w.remove()
		case "test":
			return w.testConn(c)
		case "copy_url":
			c.Password = ""
			return tea.Batch(setClip(c.FormatURL()), w.a.say("copied %s as URL without the password", c.Name))
		case "copy_url_password":
			return w.input(tea.KeyPressMsg{Code: 'U', Text: "U"})
		case "import_url":
			return w.importURL()
		case "export_connections":
			return w.exportConns()
		case "import_connections":
			return w.importConns()
		case "sort":
			return w.pickSort()
		}
		return nil
	})
	return nil
}

// testConn opens c, reports the server and closes it again (the connection form's ctrl+t, and the menu's Test).
func (w *welcome) testConn(c config.Connection) tea.Cmd {
	connect := w.a.d.Connect
	return tea.Batch(w.a.say("testing %s…", c.Name), func() tea.Msg {
		o, err := connect(context.Background(), c)
		info := ""
		if o != nil {
			info = o.Sess.ServerInfo()
			o.Close()
		}
		return testedMsg{name: c.Name, info: info, err: err}
	})
}
