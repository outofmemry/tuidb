package keymap

import "fmt"

// Contexts a binding can live in. The focused context is searched first, then Global.
const (
	Global    = "global"
	Sidebar   = "sidebar"
	Grid      = "grid"
	Editor    = "editor"
	Results   = "results"
	Structure = "structure"
	FilterBar = "filterbar"
	Console   = "console"
)

// Binding is one default (or user) binding. NoPrefix mirrors tmux `bind -n`.
type Binding struct {
	Action   string
	Key      string // canonical, see Parse
	Context  string
	NoPrefix bool
	Desc     string
}

type def struct {
	action, key, ctx, desc string
}

// noPrefixDefaults work without the prefix, like tmux `bind -n`.
var noPrefixDefaults = []def{
	{"focus_left", "ctrl+h", Global, "focus pane left"},
	{"focus_down", "ctrl+j", Global, "focus pane down"},
	{"focus_up", "ctrl+k", Global, "focus pane up"},
	{"focus_right", "ctrl+l", Global, "focus pane right"},
	// filter-bar keys work without the prefix in both UIs, as the bar's own widget keys always did
	{"filter_clear_all", "alt+c", FilterBar, "clear all filters"},
	{"filter_export", "alt+e", FilterBar, "export the filtered rows"},
	{"filter_add_copy", "alt+n", FilterBar, "add a filter as a copy of this one"},
	// console pane (P2-72): bare keys, like the sidebar's, answered while the console has focus
	{"console_find", "/", Console, "console: find"},
	{"console_next", "n", Console, "console: next match"},
	{"console_prev", "N", Console, "console: previous match"},
	{"console_copy", "y", Console, "console: copy line"},
	{"console_errors", "e", Console, "console: errors only"},
	{"console_menu", "m", Console, "console: menu"},
}

// defaults: TablePlus ⌘X -> prefix x, ⌘⇧X -> prefix X, ⌥⌘X -> prefix M-x.
// shortcut: C-[ / C-i / C-d can't be bound (Esc / Tab / the prefix itself), so show_* use ( ) M-i M-d.
var defaults = []def{
	{"help", "?", Global, "list key bindings"},
	{"open_anything", "p", Global, "open anything"},
	{"open_connection", "K", Global, "open connection"},
	{"new_session", "n", Global, "new session (connection, then database)"},
	{"open_database", "k", Global, "open database"},
	{"new_query_tab", "e", Global, "new SQL editor tab"},
	{"new_tab", "t", Global, "new tab"},
	{"close_tab", "w", Global, "close tab"},
	{"next_tab", "]", Global, "next tab"},
	{"prev_tab", "[", Global, "previous tab"},
	{"next_workspace", "}", Global, "next workspace"},
	{"prev_workspace", "{", Global, "previous workspace"},
	{"workspaces", `\`, Global, "show all workspaces"},
	{"switch_session", "l", Global, "switch session"},
	{"focus_left", "alt+h", Global, "focus pane left"},
	{"focus_down", "alt+j", Global, "focus pane down"},
	{"focus_up", "alt+k", Global, "focus pane up"},
	{"focus_right", "alt+l", Global, "focus pane right"},
	{"toggle_sidebar", "0", Global, "toggle left sidebar"},
	{"toggle_row_detail", "space", Global, "toggle row detail"},
	{"toggle_console", "ctrl+c", Global, "toggle console log"},
	{"toggle_results", "`", Global, "toggle results pane"},
	{"commit", "s", Global, "commit changes"},
	{"preview", "P", Global, "preview changes as SQL"},
	{"discard", "backspace", Global, "discard changes"},
	{"undo", "z", Global, "undo"},
	{"redo", "Z", Global, "redo"},
	{"reload", "r", Global, "reload workspace"},
	{"reload_tab", "alt+r", Global, "reload current tab"},
	{"reconnect", "R", Global, "reconnect"},
	{"disconnect", "alt+.", Global, "disconnect"},
	{"process_list", ".", Global, "process list"},
	{"safe_mode", "S", Global, "change safe mode"},
	{"search_in_database", "/", Global, "search in database"},
	{"users", "u", Global, "user management"},
	{"diagram", "d", Global, "ER diagram"},
	{"metrics", "m", Global, "metrics board"},
	{"backup", "b", Global, "backup database"},
	{"restore", "B", Global, "restore database"},
	{"export", "E", Global, "export"},
	{"import", "I", Global, "import"},
	{"assistant", "A", Global, "AI assistant"},
	{"close_other_tabs", "alt+w", Global, "close other tabs"},
	{"close_tabs_right", "alt+W", Global, "close tabs to the right"},
	{"close_all_tabs", "alt+A", Global, "close all tabs"},
	{"rename_tab", "alt+n", Global, "rename tab"},
	{"cancel_queries", "alt+x", Global, "cancel running queries"},
	{"connection_details", "alt+K", Global, "connection details"},
	{"plugins", "alt+p", Global, "run a plugin"},
	{"go_back", "<", Global, "go back"},
	{"go_forward", ">", Global, "go forward"},
	{"split_right", `"`, Global, "split pane right"},
	{"split_down", "alt+\"", Global, "split pane down"},
	{"close_other_connections", "alt+Q", Global, "close other connections"},
	{"open_file", "O", Global, "open a SQL file"},
	{"open_recent", "alt+O", Global, "open a recent file"},
	{"settings", ",", Global, "settings"},
	{"next_pane", "alt+]", Global, "next pane"},
	{"prev_pane", "alt+[", Global, "previous pane"},
	{"quit", "q", Global, "quit"},

	{"run_current", "enter", Editor, "run current statement/selection"},
	{"run_all", "alt+enter", Editor, "run all statements"},
	{"cancel_query", ".", Editor, "cancel running query"},
	{"beautify", "i", Editor, "beautify SQL"},
	{"uglify", "I", Editor, "uglify SQL"},
	{"toggle_comment", "/", Editor, "toggle line comment"},
	{"block_comment", "alt+/", Editor, "toggle block comment"},
	{"save_sql_as", "W", Editor, "save SQL to file"},
	{"set_limit", "L", Editor, "set row limit"},
	{"add_to_queries", "a", Editor, "add to saved queries"},
	{"find", "f", Editor, "find"},
	{"find_replace", "alt+f", Editor, "find and replace"},
	{"select_next_occurrence", "D", Editor, "select next occurrence (terminal: one cursor, jumps; web: adds a caret)"},
	{"select_all_occurrences", "ctrl+g", Editor, "select all occurrences"},
	{"variables", "alt+v", Editor, "query parameters (variables)"},
	{"vim_mode", "v", Editor, "toggle Vim keys"},
	{"invisibles", "alt+s", Editor, "show invisible characters"},
	{"fold", "alt+z", Editor, "fold / unfold the current statement"},
	{"wrap_lines", "alt+g", Editor, "toggle line wrap"},
	{"cleanup_variables", "alt+V", Editor, "forget remembered variables"},
	{"upper_case", "U", Editor, "selection to upper case"},
	{"lower_case", "u", Editor, "selection to lower case"},
	{"capitalize", "alt+u", Editor, "capitalize selection"},
	{"use_selection_find", "alt+e", Editor, "use selection for find"},
	{"jump_to_selection", "alt+;", Editor, "jump to selection"},
	{"complete", "ctrl+space", Editor, "open autocomplete"},

	{"find", "f", Results, "search results"},
	{"add_row", "i", Results, "add row (editable result)"},
	{"duplicate_rows", "D", Results, "duplicate rows (editable result)"},
	{"delete_rows", "delete", Results, "delete rows (editable result)"},
	{"set_value", "alt+enter", Results, "set value (editable result)"},
	{"copy_rows", "c", Results, "copy rows"},
	{"copy_rows_as", "alt+c", Results, "copy rows as..."},
	{"copy_cells", "C", Results, "copy selected cells"},
	{"chart", "g", Results, "toggle chart view"},
	{"load_more", "alt+m", Results, "load more rows"},

	{"add_row", "i", Grid, "add row"},
	{"duplicate_rows", "D", Grid, "duplicate rows"},
	{"delete_rows", "delete", Grid, "delete rows"},
	{"copy_rows", "c", Grid, "copy rows"},
	{"copy_rows_as", "alt+c", Grid, "copy rows as..."},
	{"copy_cells", "C", Grid, "copy selected cells"},
	{"paste_rows", "v", Grid, "paste rows"},
	{"paste_cells", "V", Grid, "paste into selected cells"},
	{"quick_look", "enter", Grid, "quick look"},
	{"set_value", "alt+enter", Grid, "set value (NULL/DEFAULT/...)"},
	{"filters", "f", Grid, "show filters"},
	{"columns", "alt+f", Grid, "choose visible columns"},
	{"quick_filter", "x", Grid, "quick filter on this cell"},
	{"follow_fk", "o", Grid, "open the row a foreign key points to"},
	{"first_row", "up", Grid, "first row"},
	{"last_row", "down", Grid, "last row"},
	{"next_page", "right", Grid, "next page"},
	{"prev_page", "left", Grid, "previous page"},
	{"count_exact", "#", Grid, "count rows exactly"},
	{"page_settings", "L", Grid, "set row limit and offset"},
	{"hide_column", "_", Grid, "hide this column"},
	{"show_all_columns", "+", Grid, "show all columns"},
	{"sort_columns", "alt+o", Grid, "sort by several columns"},
	{"explain", "X", Editor, "explain the current statement"},
	{"find_next", "n", Editor, "find next"},
	{"find_prev", "N", Editor, "find previous"},
	{"block_select", "V", Editor, "rectangular block selection"},
	{"filter_sql", "s", FilterBar, "show the generated WHERE clause"},
	{"mark", "space", Sidebar, "mark / unmark item (multi-select)"},
	{"new_schema", "alt+n", Sidebar, "create schema"},
	{"bulk_update", "alt+b", Grid, "bulk update rows matching the filter"},

	{"apply_filters", "enter", FilterBar, "apply checked filters"},

	{"find", "alt+/", Grid, "find in the grid"},
	{"find_next", "alt+g", Grid, "find next"},
	{"find_prev", "alt+G", Grid, "find previous"},

	{"add_row", "i", Structure, "add column / index"},
	{"delete_rows", "delete", Structure, "delete column / index"},
	{"set_value", "alt+enter", Structure, "set value / toggle"},
	{"show_data", "(", Global, "show table data"},
	{"show_structure", ")", Global, "show table structure"},
	{"show_index", "alt+i", Global, "show indexes"},
	{"show_ddl", "alt+d", Global, "show DDL"},
	{"triggers", "alt+t", Global, "edit this table's triggers"},
	{"show_json", "J", Global, "show Json view (MongoDB, Redis)"},
	{"show_tree", "T", Global, "show Tree view (MongoDB, Redis)"},

	{"sidebar_search", "F", Sidebar, "search sidebar"},
	{"truncate", "alt+delete", Sidebar, "truncate table"},
	{"delete_item", "delete", Sidebar, "delete item"},
}

func init() {
	for i := 1; i <= 9; i++ {
		defaults = append(defaults, def{fmt.Sprintf("tab_%d", i), fmt.Sprint(i), Global, fmt.Sprintf("go to tab %d", i)})
	}
}

// DefaultBindings returns a copy of the built-in table.
func DefaultBindings() []Binding {
	out := make([]Binding, len(defaults))
	for i, d := range defaults {
		out[i] = Binding{Action: d.action, Key: d.key, Context: d.ctx, Desc: d.desc}
	}
	// Ctrl+hjkl move between panes directly; this takes the editor's Ctrl+h and Ctrl+k (rebind in [keys] to get them back)
	for _, d := range noPrefixDefaults {
		out = append(out, Binding{Action: d.action, Key: d.key, Context: d.ctx, Desc: d.desc, NoPrefix: true})
	}
	return out
}
