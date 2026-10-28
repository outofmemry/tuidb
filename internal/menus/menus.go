// Package menus is the single list of context-menu entries both UIs build their menus from (M14 check 4).
// The terminal pickers and the web right-click menus render these labels in this order; GET /api/meta
// exports All() so the web never carries its own copy. A test in each UI asserts the rendered menu
// matches the list for the same object kind and capabilities.
package menus

import "strings"

// Item is one menu entry. Action is the keymap action when one exists (so the help screen can show the
// key), else a menu-only id both UIs dispatch on. When is a ';'-separated list of predicates that must
// all hold: an object kind (table, view, matview, function, procedure, sequence, type, collection, key,
// schema, database, query, history), "tableish" (table | view | matview), "definition" (view | matview |
// function | procedure), "editable" (an editable result), "cap:<Name>" (a db.Capabilities bool field),
// "marked" (the item is in the sidebar multi-select) or "group" (the item is in a custom group).
// UI is "" for both, "web" or "tui" when a UI cannot do it (the other UI greys it out and says why in Why).
type Item struct {
	Label  string `json:"label"`
	Action string `json:"action"`
	When   string `json:"when,omitempty"`
	UI     string `json:"ui,omitempty"`
	Why    string `json:"why,omitempty"`
}

// Env is what the predicates are evaluated against.
type Env struct {
	Kind     string          // object kind
	Caps     map[string]bool // capability name -> on
	Marked   bool
	InGroup  bool
	Editable bool
}

// Holds reports whether every predicate in it.When holds in env.
func (it Item) Holds(env Env) bool {
	if it.When == "" {
		return true
	}
	for _, p := range strings.Split(it.When, ";") {
		p = strings.TrimSpace(p)
		switch {
		case p == "":
		case p == "tableish":
			if env.Kind != "table" && env.Kind != "view" && env.Kind != "matview" {
				return false
			}
		case p == "definition":
			if env.Kind != "view" && env.Kind != "matview" && env.Kind != "function" && env.Kind != "procedure" {
				return false
			}
		case p == "editable":
			if !env.Editable {
				return false
			}
		case p == "marked":
			if !env.Marked {
				return false
			}
		case p == "group":
			if !env.InGroup {
				return false
			}
		case strings.HasPrefix(p, "cap:"):
			if !env.Caps[strings.TrimPrefix(p, "cap:")] {
				return false
			}
		default: // an object kind
			if env.Kind != p {
				return false
			}
		}
	}
	return true
}

// Filter returns the items of list that hold in env, in order.
func Filter(list []Item, env Env) []Item {
	var out []Item
	for _, it := range list {
		if it.Holds(env) {
			out = append(out, it)
		}
	}
	return out
}

// SidebarObject is the menu on a table, view, function... in the sidebar.
var SidebarObject = []Item{
	{Label: "Open", Action: "open"},
	{Label: "Open in new tab", Action: "open_new_tab"},
	{Label: "New table…", Action: "new_table"},
	{Label: "Open structure", Action: "show_structure", When: "tableish"},
	{Label: "Edit definition", Action: "edit_definition", When: "definition"},
	{Label: "Triggers…", Action: "triggers", When: "table;cap:Triggers"},
	{Label: "Show DDL", Action: "show_ddl"},
	{Label: "Copy name", Action: "copy_name"},
	{Label: "Copy script: CREATE", Action: "script_create"},
	{Label: "Copy script: DROP", Action: "script_drop"},
	{Label: "Copy script: TRUNCATE", Action: "script_truncate", When: "table"},
	{Label: "Copy script: SELECT top 100", Action: "script_select100", When: "table"},
	{Label: "Export…", Action: "export"},
	{Label: "Export several tables…", Action: "export_many", When: "tableish"},
	{Label: "Import into this table…", Action: "import", When: "table"},
	{Label: "Clone…", Action: "clone", When: "table"},
	{Label: "Rename…", Action: "rename", When: "table"},
	{Label: "Truncate…", Action: "truncate", When: "table"},
	{Label: "Delete…", Action: "delete_item"},
	{Label: "Pin / unpin", Action: "pin"},
	{Label: "Mark / unmark", Action: "mark"},
	{Label: "Add to group…", Action: "group_add"},
	{Label: "Remove from group", Action: "group_remove", When: "group"},
	{Label: "Diagram", Action: "diagram", When: "tableish"},
	{Label: "Set colour…", Action: "set_color", When: "tableish"},
	{Label: "New schema…", Action: "new_schema", When: "cap:Schemas"},
	{Label: "Drop schema…", Action: "drop_schema", When: "cap:Schemas"},
}

// SidebarQuery is the menu on a saved query.
var SidebarQuery = []Item{
	{Label: "Open", Action: "open"},
	{Label: "Open in new tab", Action: "open_new_tab"},
	{Label: "Insert into editor", Action: "insert"},
	{Label: "Rename…", Action: "rename"},
	{Label: "Move to folder…", Action: "move"},
	{Label: "Reveal in file manager", Action: "reveal"},
	{Label: "Delete…", Action: "delete_item"},
}

// SidebarHistory is the menu on a history entry.
var SidebarHistory = []Item{
	{Label: "Insert into editor", Action: "insert"},
	{Label: "Run in a new tab", Action: "open_new_tab"},
	{Label: "Add to saved queries…", Action: "add_to_queries"},
	{Label: "Copy", Action: "copy"},
	{Label: "Clear all…", Action: "clear_all"},
}

// SidebarNew is the sidebar "+" menu.
var SidebarNew = []Item{
	{Label: "New table…", Action: "new_table"},
	{Label: "New view…", Action: "new_view", When: "cap:Views"},
	{Label: "New materialized view…", Action: "new_matview", When: "cap:MatViews"},
	{Label: "New function / procedure…", Action: "new_function", When: "cap:Functions"},
	{Label: "New schema…", Action: "new_schema", When: "cap:Schemas"},
	{Label: "New database…", Action: "new_database"},
	{Label: "New item group…", Action: "new_group"},
}

// Database is the database picker's menu.
var Database = []Item{
	{Label: "Open in current tab", Action: "open"},
	{Label: "Open in new workspace", Action: "open_workspace"},
	{Label: "Copy database name", Action: "copy_name"},
	{Label: "New database…", Action: "new_database"},
	{Label: "Rename…", Action: "rename", When: "cap:RenameDB"},
	{Label: "Drop…", Action: "drop"},
}

// Grid is the data tab's cell / row menu.
var Grid = []Item{
	{Label: "Quick look", Action: "quick_look"},
	{Label: "Set value…", Action: "set_value"},
	{Label: "Add row", Action: "add_row"},
	{Label: "Duplicate rows", Action: "duplicate_rows"},
	{Label: "Delete rows", Action: "delete_rows"},
	{Label: "Copy rows", Action: "copy_rows"},
	{Label: "Copy rows as…", Action: "copy_rows_as"},
	{Label: "Copy selected cells", Action: "copy_cells"},
	{Label: "Copy selected cells as…", Action: "copy_cells_as"},
	{Label: "Copy all column values", Action: "copy_column"},
	{Label: "Paste rows", Action: "paste_rows"},
	{Label: "Paste into selected cells", Action: "paste_cells"},
	{Label: "Quick filter on this cell", Action: "quick_filter"},
	{Label: "Open the row a foreign key points to", Action: "follow_fk"},
	{Label: "Hide this column", Action: "hide_column"},
	{Label: "Show all columns", Action: "show_all_columns"},
	{Label: "Sort by several columns…", Action: "sort_columns"},
	{Label: "Bulk update…", Action: "bulk_update"},
	{Label: "Row limit and offset…", Action: "page_settings"},
	{Label: "Count rows exactly", Action: "count_exact"},
	{Label: "Export…", Action: "export"},
	{Label: "Import…", Action: "import"},
}

// Result is the query result's cell / row menu.
var Result = []Item{
	{Label: "Quick look", Action: "quick_look"},
	{Label: "Set value…", Action: "set_value", When: "editable"},
	{Label: "Add row", Action: "add_row", When: "editable"},
	{Label: "Duplicate rows", Action: "duplicate_rows", When: "editable"},
	{Label: "Delete rows", Action: "delete_rows", When: "editable"},
	{Label: "Copy rows", Action: "copy_rows"},
	{Label: "Copy rows as…", Action: "copy_rows_as"},
	{Label: "Copy selected cells", Action: "copy_cells"},
	{Label: "Copy selected cells as…", Action: "copy_cells_as"},
	{Label: "Copy all column values", Action: "copy_column"},
	{Label: "Toggle chart", Action: "chart"},
	{Label: "Load more", Action: "load_more"},
	{Label: "Pin result", Action: "pin"},
	{Label: "Export…", Action: "export"},
	{Label: "Add to metrics board…", Action: "add_to_board", UI: "tui", Why: "the web board form takes no query yet"},
}

// Header is the column header menu (M15 item 5), shared by the data tab and results.
var Header = []Item{
	{Label: "Sort ascending", Action: "sort_asc"},
	{Label: "Sort descending", Action: "sort_desc"},
	{Label: "Remove sort", Action: "sort_none"},
	{Label: "Sort by several columns…", Action: "sort_columns"},
	{Label: "Filter with this column", Action: "filter_column"},
	{Label: "Hide this column", Action: "hide_column"},
	{Label: "Show all columns", Action: "show_all_columns"},
	{Label: "Copy column name", Action: "copy_name"},
	{Label: "Copy all column values", Action: "copy_column"},
	{Label: "Copy column definition", Action: "copy_definition"},
}

// FilterBar is the filter bar's menu / button row.
var FilterBar = []Item{
	{Label: "Apply", Action: "apply_filters"},
	{Label: "Add filter", Action: "filter_add"},
	{Label: "Add a filter as a copy", Action: "filter_add_copy"},
	{Label: "Show SQL", Action: "filter_sql"},
	{Label: "Clear all", Action: "filter_clear_all"},
	{Label: "Export filtered rows", Action: "filter_export"},
}

// Welcome is the connection menu on the welcome screen / connections page.
var Welcome = []Item{
	{Label: "Connect", Action: "connect"},
	{Label: "Edit…", Action: "edit"},
	{Label: "Duplicate", Action: "duplicate"},
	{Label: "Delete…", Action: "delete_item"},
	{Label: "Test", Action: "test"},
	{Label: "Copy URL", Action: "copy_url"},
	{Label: "Copy URL with password", Action: "copy_url_password"},
	{Label: "Import URL…", Action: "import_url"},
	{Label: "Export connections…", Action: "export_connections"},
	{Label: "Import connections…", Action: "import_connections"},
	{Label: "Sort by…", Action: "sort"},
}

// Tab is the tab-bar menu.
var Tab = []Item{
	{Label: "Close", Action: "close_tab"},
	{Label: "Close other tabs", Action: "close_other_tabs"},
	{Label: "Close tabs to the right", Action: "close_tabs_right"},
	{Label: "Close all tabs", Action: "close_all_tabs"},
	{Label: "Rename tab…", Action: "rename_tab"},
	{Label: "Move left", Action: "move_tab_left"},
	{Label: "Move right", Action: "move_tab_right"},
	{Label: "Open workspace in a new browser tab", Action: "open_window", UI: "web", Why: "the terminal has one window"},
	{Label: "Copy link", Action: "copy_link", UI: "web", Why: "the terminal has no URLs"},
}

// Structure is the structure editor's row menu (columns / indexes / foreign keys).
var Structure = []Item{
	{Label: "Add", Action: "add_row"},
	{Label: "Delete", Action: "delete_rows"},
	{Label: "Set value / toggle", Action: "set_value"},
	{Label: "Move up", Action: "move_up", When: "cap:ReorderColumns"},
	{Label: "Move down", Action: "move_down", When: "cap:ReorderColumns"},
	{Label: "Copy as…", Action: "copy_rows_as"},
}

// ProcessList is the process list's row menu.
var ProcessList = []Item{
	{Label: "Kill selected", Action: "kill"},
	{Label: "Quick look", Action: "quick_look"},
	{Label: "Copy as…", Action: "copy_rows_as"},
	{Label: "Export…", Action: "export"},
	{Label: "Dismiss", Action: "close"},
}

// Console is the console pane's menu.
var Console = []Item{
	{Label: "Find…", Action: "find"},
	{Label: "Copy line", Action: "copy"},
	{Label: "Errors only", Action: "errors_only"},
	{Label: "Clear", Action: "clear_console"},
}

// Jobs is the background-jobs centre's row menu (M15 item 16).
var Jobs = []Item{
	{Label: "Stop", Action: "cancel"},
	{Label: "Open folder", Action: "reveal", UI: "tui", Why: "the browser downloads the file instead"},
	{Label: "Download", Action: "download", UI: "web", Why: "the terminal reveals the file instead"},
	{Label: "Dismiss", Action: "close"},
}

// All returns every list by name, for GET /api/meta.
func All() map[string][]Item {
	return map[string][]Item{
		"sidebar_object": SidebarObject, "sidebar_query": SidebarQuery, "sidebar_history": SidebarHistory, "sidebar_new": SidebarNew,
		"database": Database, "grid": Grid, "result": Result, "header": Header, "filter_bar": FilterBar, "welcome": Welcome,
		"tab": Tab, "structure": Structure, "process_list": ProcessList, "console": Console, "jobs": Jobs,
	}
}

// Labels returns the labels of the items that hold in env, the shape both UIs' parity tests compare.
func Labels(list []Item, env Env) []string {
	var out []string
	for _, it := range Filter(list, env) {
		out = append(out, it.Label)
	}
	return out
}
