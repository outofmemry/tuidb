---
title: Actions
description: Every action name in webtuidb, alphabetically, with its default key, the pane it works in, what it does, which UI has it and where it is documented.
weight: 20
---

Every key binding, every context-menu entry and every Open anything feature runs an *action*, and each action has a name. The name is what you write under `[keys]` in `config.toml` to rebind it, what the help screens list ({{< key "C-d ?" >}}), and what both UIs dispatch on. Look a name up here to find its key and the page that explains it.

## How to read the table

- **Key**: the default binding. {{< key "C-d" >}} is the default prefix, so {{< key "C-d f" >}} means press Ctrl+d, release, then press f. *(no prefix)* marks a key that works on its own, like a tmux `bind -n` key. *(menu only)* entries have no key: you reach them from a right-click or `m` menu (see [Context menus](../menus/)). *(Open anything)* entries are features listed in {{< key "C-d p" >}}.
- **Context**: where the key is answered. `global` works everywhere; `sidebar`, `grid` (a table's data tab), `editor` (the SQL editor), `results` (query results), `structure` (the structure editor), `filterbar` and `console` only work while that pane has focus. The focused pane is searched first, then `global`, so the same key can mean different actions in different panes (`f` is `filters` in the grid and `find` in the editor). An action bound in several contexts has one row per context.
- **Web**: *Both* means the terminal UI and the web UI do the same thing with the same key. Every one of the 162 keyed actions is implemented in the web (the web help dialog greys out none). Where the two differ, or an entry exists in only one UI, the row says how and why.
- **Documented in**: the section that explains the action in full.

To change a key, see [Key bindings](../../configuration/key-bindings/). Rebinding an action replaces all of its default keys, including a no-prefix one. The list of keys grouped by pane is on the [Shortcuts](../shortcuts/) page.

## All actions

| Action | Key | Context | What it does | Web | Documented in |
|---|---|---|---|---|---|
| `add_row` | {{< key "C-d i" >}} | grid | Add row | Both | [Editing data](../../editing-data/) |
| `add_row` | {{< key "C-d i" >}} | results | Add row (editable result) | Both | [SQL editor](../../sql-editor/) |
| `add_row` | {{< key "C-d i" >}} | structure | Add column / index | Both | [Structure](../../structure/) |
| `add_to_board` | (menu only) | menu: result cell | Add the result as a panel of a metrics board | Terminal only (greyed out in the web): the web board form takes no query yet. | [Tools](../../tools/) |
| `add_to_queries` | {{< key "C-d a" >}} | editor | Add to saved queries | Both | [SQL editor](../../sql-editor/) |
| `apply_filters` | {{< key "C-d Enter" >}} | filterbar | Apply checked filters | Both | [Browsing data](../../browsing-data/) |
| `assistant` | {{< key "C-d A" >}} | global | AI assistant | Both | [AI and automation](../../automation/) |
| `assistant_clear` | {{< key "C-d M-X" >}} | global | Assistant: clear chat | Both | [AI and automation](../../automation/) |
| `assistant_history` | {{< key "C-d M-Y" >}} | global | Assistant: chat history | Both | [AI and automation](../../automation/) |
| `assistant_new` | {{< key "C-d M-N" >}} | global | Assistant: new chat | Both | [AI and automation](../../automation/) |
| `backup` | {{< key "C-d b" >}} | global | Backup database | Both | [Import, export](../../import-export/) |
| `beautify` | {{< key "C-d i" >}} | editor | Beautify SQL | Both | [SQL editor](../../sql-editor/) |
| `block_comment` | {{< key "C-d M-/" >}} | editor | Toggle block comment | Both | [SQL editor](../../sql-editor/) |
| `block_select` | {{< key "C-d V" >}} | editor | Rectangular block selection | Both | [SQL editor](../../sql-editor/) |
| `bulk_update` | {{< key "C-d M-b" >}} | grid | Bulk update rows matching the filter | Both | [Editing data](../../editing-data/) |
| `cancel` | (menu only) | menu: job | Stop a running background job (jobs centre "Stop") | Both | [Workspace](../../workspace/) |
| `cancel_queries` | {{< key "C-d M-x" >}} | global | Cancel running queries | Both | [SQL editor](../../sql-editor/) |
| `cancel_query` | {{< key "C-d ." >}} | editor | Cancel running query | Both | [SQL editor](../../sql-editor/) |
| `capitalize` | {{< key "C-d M-u" >}} | editor | Capitalize selection | Both | [SQL editor](../../sql-editor/) |
| `chart` | {{< key "C-d g" >}} | results | Toggle chart view | Both. The terminal draws a line chart as bars; the web draws a line. | [SQL editor](../../sql-editor/) |
| `cleanup_variables` | {{< key "C-d M-V" >}} | editor | Forget remembered variables | Both | [SQL editor](../../sql-editor/) |
| `clear_all` | (menu only) | menu: history entry | Clear the whole query history of this connection, after a confirmation | Both | [SQL editor](../../sql-editor/) |
| `clear_console` | (menu only) | menu: console | Clear the console log | Both | [Workspace](../../workspace/) |
| `clone` | (menu only) | menu: sidebar object | Copy a table under a new name, with or without its rows | Both | [Structure](../../structure/) |
| `close` | (menu only) | menu: job, process | Dismiss a finished job, or close the process list | Both | [Workspace](../../workspace/) |
| `close_all_tabs` | {{< key "C-d M-A" >}} | global | Close all tabs | Both | [Workspace](../../workspace/) |
| `close_other_connections` | {{< key "C-d M-Q" >}} | global | Close other connections | Both | [Connections](../../connections/) |
| `close_other_tabs` | {{< key "C-d M-w" >}} | global | Close other tabs | Both | [Workspace](../../workspace/) |
| `close_pane` | {{< key "C-d M-q" >}} | global | Close this split pane | Both | [Workspace](../../workspace/) |
| `close_tab` | {{< key "C-d w" >}} | global | Close tab | Both | [Workspace](../../workspace/) |
| `close_tabs_right` | {{< key "C-d M-W" >}} | global | Close tabs to the right | Both | [Workspace](../../workspace/) |
| `column_menu` | {{< key "C-d M-H" >}} | grid | Column header menu (sort, filter, hide) | Both | [Browsing data](../../browsing-data/) |
| `column_menu` | {{< key "C-d M-H" >}} | results | Column header menu (sort, filter, hide) | Both. In results, Filter / Hide / Show all columns are greyed out in the web and answered with a note in the terminal (they belong to table tabs). | [SQL editor](../../sql-editor/) |
| `columns` | {{< key "C-d M-f" >}} | grid | Choose visible columns | Both | [Browsing data](../../browsing-data/) |
| `commit` | {{< key "C-d s" >}} | global | Commit changes | Both | [Editing data](../../editing-data/) |
| `complete` | {{< key "C-d C-Space" >}} | editor | Open autocomplete | Both | [SQL editor](../../sql-editor/) |
| `connect` | (menu only) | menu: connection | Connect to the selected saved connection | Both | [Connections](../../connections/) |
| `connection_details` | {{< key "C-d M-K" >}} | global | Connection details | Both | [Connections](../../connections/) |
| `console_copy` | {{< key "y" >}} (no prefix) | console | Console: copy line | Both | [Workspace](../../workspace/) |
| `console_errors` | {{< key "e" >}} (no prefix) | console | Console: errors only | Both | [Workspace](../../workspace/) |
| `console_find` | {{< key "/" >}} (no prefix) | console | Console: find | Both | [Workspace](../../workspace/) |
| `console_menu` | {{< key "m" >}} (no prefix) | console | Console: menu | Both | [Workspace](../../workspace/) |
| `console_next` | {{< key "n" >}} (no prefix) | console | Console: next match | Both | [Workspace](../../workspace/) |
| `console_prev` | {{< key "N" >}} (no prefix) | console | Console: previous match | Both | [Workspace](../../workspace/) |
| `copy` | (menu only) | menu: console, history entry | Copy the console line, or the history statement | Both | [Workspace](../../workspace/) |
| `copy_cells` | {{< key "C-d C" >}} | grid | Copy selected cells | Both | [Browsing data](../../browsing-data/) |
| `copy_cells` | {{< key "C-d C" >}} | results | Copy selected cells | Both | [SQL editor](../../sql-editor/) |
| `copy_cells_as` | {{< key "C-d M-C" >}} | grid | Copy selected cells as... | Both | [Browsing data](../../browsing-data/) |
| `copy_cells_as` | {{< key "C-d M-C" >}} | results | Copy selected cells as... | Both | [SQL editor](../../sql-editor/) |
| `copy_column` | {{< key "C-d M-y" >}} | grid | Copy all column values | Both | [Browsing data](../../browsing-data/) |
| `copy_column` | {{< key "C-d M-y" >}} | results | Copy all column values | Both | [SQL editor](../../sql-editor/) |
| `copy_definition` | (menu only) | menu: column header | Copy the column definition (name, type, NOT NULL, DEFAULT, PRIMARY KEY) | Both | [Structure](../../structure/) |
| `copy_link` | (menu only) | menu: tab | Copy a link to the tab (table or saved query) or the workspace, without the token | Web only: the terminal has no URLs. | [Web UI](../../web-ui/) |
| `copy_name` | (menu only) | menu: column header, database, sidebar object | Copy the object, database or column name | Both | [Workspace](../../workspace/) |
| `copy_rows` | {{< key "C-d c" >}} | grid | Copy rows | Both | [Browsing data](../../browsing-data/) |
| `copy_rows` | {{< key "C-d c" >}} | results | Copy rows | Both | [SQL editor](../../sql-editor/) |
| `copy_rows_as` | {{< key "C-d M-c" >}} | grid | Copy rows as... | Both | [Browsing data](../../browsing-data/) |
| `copy_rows_as` | {{< key "C-d M-c" >}} | results | Copy rows as... | Both | [SQL editor](../../sql-editor/) |
| `copy_url` | (menu only) | menu: connection | Copy the connection as a URL, password left out | Both | [Connections](../../connections/) |
| `copy_url_password` | (menu only) | menu: connection | Copy the connection as a URL including the password | Both | [Connections](../../connections/) |
| `count_exact` | {{< key "C-d #" >}} | grid | Count rows exactly | Both | [Browsing data](../../browsing-data/) |
| `data_diff` | (Open anything) | Open anything | Compare the rows of two tables | Both | [Tools](../../tools/) |
| `delete_item` | {{< key "C-d Delete" >}} | sidebar | Delete item | Both | [Structure](../../structure/) |
| `delete_rows` | {{< key "C-d Delete" >}} | grid | Delete rows | Both | [Editing data](../../editing-data/) |
| `delete_rows` | {{< key "C-d Delete" >}} | results | Delete rows (editable result) | Both. The web acts on the selected row only (no multi-select in results). | [SQL editor](../../sql-editor/) |
| `delete_rows` | {{< key "C-d Delete" >}} | structure | Delete column / index | Both | [Structure](../../structure/) |
| `diagram` | {{< key "C-d d" >}} | global | ER diagram | Both | [Tools](../../tools/) |
| `discard` | {{< key "C-d Backspace" >}} | global | Discard changes | Both | [Editing data](../../editing-data/) |
| `disconnect` | {{< key "C-d M-." >}} | global | Disconnect | Both | [Connections](../../connections/) |
| `download` | (menu only) | menu: job | Download the file a finished job wrote | Web only: the terminal reveals the file instead. | [Workspace](../../workspace/) |
| `drop` | (menu only) | menu: database | Drop the database, after typing its name | Both | [Structure](../../structure/) |
| `drop_schema` | (menu only) | menu: sidebar object | Drop a schema (the name typed twice, CASCADE asked separately) | Both | [Structure](../../structure/) |
| `duplicate` | (menu only) | menu: connection | Copy the saved connection under a new name | Both | [Connections](../../connections/) |
| `duplicate_rows` | {{< key "C-d D" >}} | grid | Duplicate rows | Both | [Editing data](../../editing-data/) |
| `duplicate_rows` | {{< key "C-d D" >}} | results | Duplicate rows (editable result) | Both. The web acts on the selected row only. | [SQL editor](../../sql-editor/) |
| `edit` | (menu only) | menu: connection | Edit the saved connection | Both | [Connections](../../connections/) |
| `edit_definition` | (menu only) | menu: sidebar object | Open the view, materialized view, function or procedure definition in an editor tab | Both | [Structure](../../structure/) |
| `errors_only` | (menu only) | menu: console | Show only failed statements in the console | Both | [Workspace](../../workspace/) |
| `explain` | {{< key "C-d X" >}} | editor | Explain the current statement | Both | [SQL editor](../../sql-editor/) |
| `export` | {{< key "C-d E" >}} | global | Export | Both | [Import, export](../../import-export/) |
| `export_connections` | (menu only) | menu: connection | Export saved connections to a file, optionally with passwords under a passphrase | Both | [Connections](../../connections/) |
| `export_many` | (menu only) | menu: sidebar object | Export several tables at once, one file each | Both | [Import, export](../../import-export/) |
| `fake_rows` | (Open anything) | Open anything | Generate fake rows for a table | Both | [Tools](../../tools/) |
| `filter_add` | (menu only) | menu: filter bar | Add an empty filter row to the filter bar | Both | [Browsing data](../../browsing-data/) |
| `filter_add_copy` | {{< key "M-n" >}} (no prefix) | filterbar | Add a filter as a copy of this one | Both | [Browsing data](../../browsing-data/) |
| `filter_clear_all` | {{< key "M-c" >}} (no prefix) | filterbar | Clear all filters | Both | [Browsing data](../../browsing-data/) |
| `filter_column` | (menu only) | menu: column header | Open the filter bar on this column | Both | [Browsing data](../../browsing-data/) |
| `filter_export` | {{< key "M-e" >}} (no prefix) | filterbar | Export the filtered rows | Both | [Browsing data](../../browsing-data/) |
| `filter_sql` | {{< key "C-d s" >}} | filterbar | Show the generated WHERE clause | Both | [Browsing data](../../browsing-data/) |
| `filters` | {{< key "C-d f" >}} | grid | Show filters | Both | [Browsing data](../../browsing-data/) |
| `find` | {{< key "C-d f" >}} | editor | Find | Both | [SQL editor](../../sql-editor/) |
| `find` | {{< key "C-d M-/" >}} | grid | Find in the grid | Both | [Browsing data](../../browsing-data/) |
| `find` | {{< key "C-d f" >}} | results | Search results | Both | [SQL editor](../../sql-editor/) |
| `find_next` | {{< key "C-d n" >}} | editor | Find next | Both | [SQL editor](../../sql-editor/) |
| `find_next` | {{< key "C-d M-g" >}} | grid | Find next | Both | [Browsing data](../../browsing-data/) |
| `find_prev` | {{< key "C-d N" >}} | editor | Find previous | Both | [SQL editor](../../sql-editor/) |
| `find_prev` | {{< key "C-d M-G" >}} | grid | Find previous | Both | [Browsing data](../../browsing-data/) |
| `find_replace` | {{< key "C-d M-f" >}} | editor | Find and replace | Both | [SQL editor](../../sql-editor/) |
| `first_row` | {{< key "C-d Up" >}} | grid | First row | Both | [Browsing data](../../browsing-data/) |
| `focus_down` | {{< key "C-d M-j" >}} | global | Focus pane down | Both | [Workspace](../../workspace/) |
| `focus_down` | {{< key "C-j" >}} (no prefix) | global | Focus pane down | Both | [Workspace](../../workspace/) |
| `focus_left` | {{< key "C-d M-h" >}} | global | Focus pane left | Both | [Workspace](../../workspace/) |
| `focus_left` | {{< key "C-h" >}} (no prefix) | global | Focus pane left | Both | [Workspace](../../workspace/) |
| `focus_right` | {{< key "C-d M-l" >}} | global | Focus pane right | Both | [Workspace](../../workspace/) |
| `focus_right` | {{< key "C-l" >}} (no prefix) | global | Focus pane right | Both | [Workspace](../../workspace/) |
| `focus_up` | {{< key "C-d M-k" >}} | global | Focus pane up | Both | [Workspace](../../workspace/) |
| `focus_up` | {{< key "C-k" >}} (no prefix) | global | Focus pane up | Both | [Workspace](../../workspace/) |
| `fold` | {{< key "C-d M-z" >}} | editor | Fold / unfold the current statement | Both | [SQL editor](../../sql-editor/) |
| `follow_fk` | {{< key "C-d o" >}} | grid | Open the row a foreign key points to | Both | [Browsing data](../../browsing-data/) |
| `go_back` | {{< key "C-d <" >}} | global | Go back | Both | [Workspace](../../workspace/) |
| `go_forward` | {{< key "C-d >" >}} | global | Go forward | Both | [Workspace](../../workspace/) |
| `group_add` | (menu only) | menu: sidebar object | Put the object in a custom sidebar group | Both | [Workspace](../../workspace/) |
| `group_remove` | (menu only) | menu: sidebar object | Take the object out of a custom group (one entry per group it is in) | Both | [Workspace](../../workspace/) |
| `help` | {{< key "C-d ?" >}} | global | List key bindings | Both | [Getting started](../../getting-started/) |
| `hide_column` | {{< key "C-d _" >}} | grid | Hide this column | Both | [Browsing data](../../browsing-data/) |
| `import` | {{< key "C-d I" >}} | global | Import | Both | [Import, export](../../import-export/) |
| `import_connections` | (menu only) | menu: connection | Import connections from a file exported earlier | Both | [Connections](../../connections/) |
| `import_url` | (menu only) | menu: connection | Create a connection from a pasted URL | Both | [Connections](../../connections/) |
| `insert` | (menu only) | menu: history entry, saved query | Insert the saved query or history statement into the open editor | Both | [SQL editor](../../sql-editor/) |
| `invisibles` | {{< key "C-d M-s" >}} | editor | Show invisible characters | Both | [SQL editor](../../sql-editor/) |
| `item_overview` | (Open anything) | Open anything | Show an overview of the current object (the web opens its structure) | Both. The web entry is named "Item overview" and opens the structure view. | [Structure](../../structure/) |
| `jobs` | {{< key "C-d M-J" >}} | global | Background jobs (import, export, backup, restore, search) | Both | [Workspace](../../workspace/) |
| `jump_to_selection` | {{< key "C-d M-;" >}} | editor | Jump to selection | Both | [SQL editor](../../sql-editor/) |
| `kill` | (menu only) | menu: process | Kill the marked server processes, after a confirmation | Both | [Tools](../../tools/) |
| `last_row` | {{< key "C-d Down" >}} | grid | Last row | Both | [Browsing data](../../browsing-data/) |
| `load_more` | {{< key "C-d M-m" >}} | results | Load more rows | Both. In the web a result that grew by Load more is no longer editable. | [SQL editor](../../sql-editor/) |
| `lock` | {{< key "C-d M-L" >}} | global | Lock with the passcode | Both. In the web the access token stays in the tab while locked: it keeps a bystander out of the page, not out of the API. | [Safety](../../safety/) |
| `lower_case` | {{< key "C-d u" >}} | editor | Selection to lower case | Both | [SQL editor](../../sql-editor/) |
| `mark` | {{< key "C-d Space" >}} | sidebar | Mark / unmark item (multi-select) | Both | [Workspace](../../workspace/) |
| `mcp_log` | (Open anything) | Open anything | Show what MCP clients ran (MCP activity) | Both | [AI and automation](../../automation/) |
| `metrics` | {{< key "C-d m" >}} | global | Metrics board | Both | [Tools](../../tools/) |
| `move` | (menu only) | menu: saved query | Move a saved query to another folder | Both | [SQL editor](../../sql-editor/) |
| `move_down` | (menu only) | menu: structure row | Move the column one place down | Both | [Structure](../../structure/) |
| `move_tab_left` | {{< key "C-d M-{" >}} | global | Move tab left | Both | [Workspace](../../workspace/) |
| `move_tab_right` | {{< key "C-d M-}" >}} | global | Move tab right | Both | [Workspace](../../workspace/) |
| `move_up` | (menu only) | menu: structure row | Move the column one place up | Both | [Structure](../../structure/) |
| `new_database` | (menu only) | menu: database, sidebar + | Create a database, with engine options (encoding, collation, replication) | Both | [Structure](../../structure/) |
| `new_function` | (menu only) | menu: sidebar + | Open a CREATE FUNCTION / PROCEDURE template in an editor tab | Both | [Structure](../../structure/) |
| `new_group` | (menu only) | menu: sidebar + | Create a custom sidebar group from the marked items | Both | [Workspace](../../workspace/) |
| `new_matview` | (menu only) | menu: sidebar + | Open a CREATE MATERIALIZED VIEW template in an editor tab | Both | [Structure](../../structure/) |
| `new_menu` | (Open anything) | Open anything | Open the sidebar "+" menu (new table, view, function, schema, database, group) | Terminal. The web opens the same menu from the sidebar + button. | [Structure](../../structure/) |
| `new_query_tab` | {{< key "C-d e" >}} | global | New SQL editor tab | Both | [SQL editor](../../sql-editor/) |
| `new_schema` | {{< key "C-d M-n" >}} | sidebar | Create schema | Both | [Structure](../../structure/) |
| `new_session` | {{< key "C-d n" >}} | global | New session (connection, then database) | Both | [Workspace](../../workspace/) |
| `new_tab` | {{< key "C-d t" >}} | global | New tab | Both | [Workspace](../../workspace/) |
| `new_table` | (menu only) | menu: sidebar +, sidebar object | Create a table with the New table dialog | Both | [Structure](../../structure/) |
| `new_view` | (menu only) | menu: sidebar + | Open a CREATE VIEW template in an editor tab | Both | [Structure](../../structure/) |
| `next_page` | {{< key "C-d Right" >}} | grid | Next page | Both | [Browsing data](../../browsing-data/) |
| `next_pane` | {{< key "C-d M-]" >}} | global | Next pane | Both | [Workspace](../../workspace/) |
| `next_tab` | {{< key "C-d ]" >}} | global | Next tab | Both | [Workspace](../../workspace/) |
| `next_workspace` | {{< key "C-d }" >}} | global | Next workspace | Both | [Workspace](../../workspace/) |
| `open` | (menu only) | menu: database, saved query, sidebar object | Open the object, saved query or database in the current tab | Both | [Workspace](../../workspace/) |
| `open_anything` | {{< key "C-d p" >}} | global | Open anything | Both | [Workspace](../../workspace/) |
| `open_connection` | {{< key "C-d K" >}} | global | Open connection | Both | [Connections](../../connections/) |
| `open_database` | {{< key "C-d k" >}} | global | Open database | Both | [Workspace](../../workspace/) |
| `open_docs` | (Open anything) | Open anything | Open the project README in a browser | Both | [Getting started](../../getting-started/) |
| `open_file` | {{< key "C-d O" >}} | global | Open a SQL file | Both | [SQL editor](../../sql-editor/) |
| `open_new_tab` | (menu only) | menu: history entry, saved query, sidebar object | Open the object or saved query in a new tab; run a history statement in a new tab | Both | [Workspace](../../workspace/) |
| `open_recent` | {{< key "C-d M-O" >}} | global | Open a recent file | Both | [SQL editor](../../sql-editor/) |
| `open_window` | (menu only) | menu: tab | Open the workspace in a new browser tab | Web only: the terminal has one window. | [Web UI](../../web-ui/) |
| `open_workspace` | (menu only) | menu: database | Open the database in a new workspace | Both | [Workspace](../../workspace/) |
| `page_settings` | {{< key "C-d L" >}} | grid | Set row limit and offset | Both | [Browsing data](../../browsing-data/) |
| `paste_cells` | {{< key "C-d V" >}} | grid | Paste into selected cells | Both | [Editing data](../../editing-data/) |
| `paste_rows` | {{< key "C-d v" >}} | grid | Paste rows | Both | [Editing data](../../editing-data/) |
| `pin` | (menu only) | menu: result cell, sidebar object | Pin or unpin: a sidebar object stays at the top; a pinned result survives the next run | Both | [Workspace](../../workspace/) |
| `pin_connection` | (Open anything) | Open anything | Pin a server connection to this editor tab, so BEGIN / SET / temp tables carry across runs | Both | [SQL editor](../../sql-editor/) |
| `plugins` | {{< key "C-d M-p" >}} | global | Run a plugin | Both | [AI and automation](../../automation/) |
| `prev_page` | {{< key "C-d Left" >}} | grid | Previous page | Both | [Browsing data](../../browsing-data/) |
| `prev_pane` | {{< key "C-d M-[" >}} | global | Previous pane | Both | [Workspace](../../workspace/) |
| `prev_tab` | {{< key "C-d [" >}} | global | Previous tab | Both | [Workspace](../../workspace/) |
| `prev_workspace` | {{< key "C-d {" >}} | global | Previous workspace | Both | [Workspace](../../workspace/) |
| `preview` | {{< key "C-d P" >}} | global | Preview changes as SQL | Both | [Editing data](../../editing-data/) |
| `process_list` | {{< key "C-d ." >}} | global | Process list | Both | [Tools](../../tools/) |
| `quick_filter` | {{< key "C-d x" >}} | grid | Quick filter on this cell | Both. The terminal asks for the mode (=, <>, Contains, Has prefix, IN, IS NULL, IS NOT NULL); the web applies = (IS NULL for a NULL cell) at once. | [Browsing data](../../browsing-data/) |
| `quick_look` | {{< key "C-d Enter" >}} | grid | Quick look | Both | [Browsing data](../../browsing-data/) |
| `quit` | {{< key "C-d q" >}} | global | Quit | Both | [Workspace](../../workspace/) |
| `reconnect` | {{< key "C-d R" >}} | global | Reconnect | Both | [Connections](../../connections/) |
| `redo` | {{< key "C-d Z" >}} | global | Redo | Both | [Editing data](../../editing-data/) |
| `reload` | {{< key "C-d r" >}} | global | Reload workspace | Both | [Workspace](../../workspace/) |
| `reload_tab` | {{< key "C-d M-r" >}} | global | Reload current tab | Both | [Workspace](../../workspace/) |
| `rename` | (menu only) | menu: database, saved query, sidebar object | Rename the table, database (where the engine can) or saved query | Both | [Structure](../../structure/) |
| `rename_tab` | {{< key "C-d M-n" >}} | global | Rename tab | Both | [Workspace](../../workspace/) |
| `restore` | {{< key "C-d B" >}} | global | Restore database | Both | [Import, export](../../import-export/) |
| `reveal` | (menu only) | menu: job, saved query | Show the file in the file manager (saved query) or open the folder of a job's file | Both; "Open folder" is terminal only (the browser downloads the file instead). | [SQL editor](../../sql-editor/) |
| `run_all` | {{< key "C-d M-Enter" >}} | editor | Run all statements | Both | [SQL editor](../../sql-editor/) |
| `run_current` | {{< key "C-d Enter" >}} | editor | Run current statement/selection | Both | [SQL editor](../../sql-editor/) |
| `safe_mode` | {{< key "C-d S" >}} | global | Change safe mode | Both | [Safety](../../safety/) |
| `save_sql_as` | {{< key "C-d W" >}} | editor | Save SQL to file | Both | [SQL editor](../../sql-editor/) |
| `schema_diff` | (Open anything) | Open anything | Compare two schemas and generate the migration SQL | Both | [Tools](../../tools/) |
| `script_create` | (menu only) | menu: sidebar object | Copy the CREATE statement of the object | Both | [Structure](../../structure/) |
| `script_drop` | (menu only) | menu: sidebar object | Copy a DROP statement for the object | Both | [Structure](../../structure/) |
| `script_select100` | (menu only) | menu: sidebar object | Copy a SELECT of the first 100 rows | Both | [Structure](../../structure/) |
| `script_truncate` | (menu only) | menu: sidebar object | Copy a TRUNCATE statement for the table | Both | [Structure](../../structure/) |
| `search_in_database` | {{< key "C-d /" >}} | global | Search in database | Both | [Tools](../../tools/) |
| `select_all` | {{< key "C-d M-a" >}} | editor | Select all | Both | [SQL editor](../../sql-editor/) |
| `select_all_occurrences` | {{< key "C-d C-g" >}} | editor | Select all occurrences | Both | [SQL editor](../../sql-editor/) |
| `select_next_occurrence` | {{< key "C-d D" >}} | editor | Select next occurrence (terminal: one cursor, jumps; web: adds a caret) | Both. The terminal editor has one cursor, so the selection jumps; the web adds a caret. | [SQL editor](../../sql-editor/) |
| `select_word` | {{< key "C-d M-D" >}} | editor | Select word | Both | [SQL editor](../../sql-editor/) |
| `set_color` | (menu only) | menu: sidebar object | Give a table, view or materialized view a colour in the sidebar and the ER diagram | Both | [Workspace](../../workspace/) |
| `set_limit` | {{< key "C-d L" >}} | editor | Set row limit | Both | [SQL editor](../../sql-editor/) |
| `set_value` | {{< key "C-d M-Enter" >}} | grid | Set value (NULL/DEFAULT/...) | Both | [Editing data](../../editing-data/) |
| `set_value` | {{< key "C-d M-Enter" >}} | results | Set value (editable result) | Both. The web acts on the selected row only. | [SQL editor](../../sql-editor/) |
| `set_value` | {{< key "C-d M-Enter" >}} | structure | Set value / toggle | Both | [Structure](../../structure/) |
| `settings` | {{< key "C-d ," >}} | global | Settings | Both. The web cannot edit settings that name a path on the server; most changes apply after a restart. | [Configuration](../../configuration/) |
| `show_all_columns` | {{< key "C-d +" >}} | grid | Show all columns | Both | [Browsing data](../../browsing-data/) |
| `show_data` | {{< key "C-d (" >}} | global | Show table data | Both | [Structure](../../structure/) |
| `show_ddl` | {{< key "C-d M-d" >}} | global | Show DDL | Both | [Structure](../../structure/) |
| `show_index` | {{< key "C-d M-i" >}} | global | Show indexes | Both | [Structure](../../structure/) |
| `show_json` | {{< key "C-d J" >}} | global | Show Json view (MongoDB, Redis) | Both | [Browsing data](../../browsing-data/) |
| `show_structure` | {{< key "C-d )" >}} | global | Show table structure | Both | [Structure](../../structure/) |
| `show_tree` | {{< key "C-d T" >}} | global | Show Tree view (MongoDB, Redis) | Both | [Browsing data](../../browsing-data/) |
| `sidebar_search` | {{< key "C-d F" >}} | sidebar | Search sidebar | Both | [Workspace](../../workspace/) |
| `sort` | (menu only) | menu: connection | Sort the connection list by name, tag, driver or recent use | Both | [Connections](../../connections/) |
| `sort_asc` | (menu only) | menu: column header | Sort by this column, ascending | Both | [Browsing data](../../browsing-data/) |
| `sort_columns` | {{< key "C-d M-o" >}} | grid | Sort by several columns | Both | [Browsing data](../../browsing-data/) |
| `sort_desc` | (menu only) | menu: column header | Sort by this column, descending | Both | [Browsing data](../../browsing-data/) |
| `sort_none` | (menu only) | menu: column header | Remove the sort on this column | Both | [Browsing data](../../browsing-data/) |
| `split_down` | {{< key `C-d M-"` >}} | global | Split pane down | Both. In the web the second editor's text is not saved with the tab. | [Workspace](../../workspace/) |
| `split_right` | {{< key `C-d "` >}} | global | Split pane right | Both | [Workspace](../../workspace/) |
| `ssh_log` | (Open anything) | Open anything | Turn the SSH tunnel debug log on or off (writes ssh.log in the data folder) | Terminal. The web entry "Enable SSH log" only explains that the server needs --ssh-log. | [Connections](../../connections/) |
| `switch_session` | {{< key "C-d l" >}} | global | Switch session | Both | [Workspace](../../workspace/) |
| `tab_1` | {{< key "C-d 1" >}} | global | Go to tab 1 | Both | [Workspace](../../workspace/) |
| `tab_2` | {{< key "C-d 2" >}} | global | Go to tab 2 | Both | [Workspace](../../workspace/) |
| `tab_3` | {{< key "C-d 3" >}} | global | Go to tab 3 | Both | [Workspace](../../workspace/) |
| `tab_4` | {{< key "C-d 4" >}} | global | Go to tab 4 | Both | [Workspace](../../workspace/) |
| `tab_5` | {{< key "C-d 5" >}} | global | Go to tab 5 | Both | [Workspace](../../workspace/) |
| `tab_6` | {{< key "C-d 6" >}} | global | Go to tab 6 | Both | [Workspace](../../workspace/) |
| `tab_7` | {{< key "C-d 7" >}} | global | Go to tab 7 | Both | [Workspace](../../workspace/) |
| `tab_8` | {{< key "C-d 8" >}} | global | Go to tab 8 | Both | [Workspace](../../workspace/) |
| `tab_9` | {{< key "C-d 9" >}} | global | Go to tab 9 | Both | [Workspace](../../workspace/) |
| `tab_menu` | (Open anything) | Open anything | Open the tab menu for the current tab | Terminal. The web opens the tab menu by right-clicking a tab. | [Workspace](../../workspace/) |
| `test` | (menu only) | menu: connection | Test the saved connection without opening a workspace | Both | [Connections](../../connections/) |
| `toggle_comment` | {{< key "C-d /" >}} | editor | Toggle line comment | Both | [SQL editor](../../sql-editor/) |
| `toggle_console` | {{< key "C-d C-c" >}} | global | Toggle console log | Both | [Workspace](../../workspace/) |
| `toggle_panels` | {{< key "C-d M-0" >}} | global | Zen: hide sidebar, console, row detail and status | Both | [Workspace](../../workspace/) |
| `toggle_results` | {{< key "C-d `" >}} | global | Toggle results pane | Both | [SQL editor](../../sql-editor/) |
| `toggle_row_detail` | {{< key "C-d Space" >}} | global | Toggle row detail | Both | [Browsing data](../../browsing-data/) |
| `toggle_sidebar` | {{< key "C-d 0" >}} | global | Toggle left sidebar | Both | [Workspace](../../workspace/) |
| `triggers` | {{< key "C-d M-t" >}} | global | Edit this table's triggers | Both | [Structure](../../structure/) |
| `truncate` | {{< key "C-d M-Delete" >}} | sidebar | Truncate table | Both | [Structure](../../structure/) |
| `uglify` | {{< key "C-d I" >}} | editor | Uglify SQL | Both | [SQL editor](../../sql-editor/) |
| `undo` | {{< key "C-d z" >}} | global | Undo | Both | [Editing data](../../editing-data/) |
| `upper_case` | {{< key "C-d U" >}} | editor | Selection to upper case | Both | [SQL editor](../../sql-editor/) |
| `use_selection_find` | {{< key "C-d M-e" >}} | editor | Use selection for find | Both | [SQL editor](../../sql-editor/) |
| `users` | {{< key "C-d u" >}} | global | User management | Both | [Tools](../../tools/) |
| `variables` | {{< key "C-d M-v" >}} | editor | Query parameters (variables) | Both | [SQL editor](../../sql-editor/) |
| `vim_mode` | {{< key "C-d v" >}} | editor | Toggle Vim keys | Both | [SQL editor](../../sql-editor/) |
| `workspaces` | {{< key `C-d \` >}} | global | Show all workspaces | Both | [Workspace](../../workspace/) |
| `wrap_lines` | {{< key "C-d M-g" >}} | editor | Toggle line wrap | Both | [SQL editor](../../sql-editor/) |

## Keys that cannot be bound

`C-[`, `C-i` and `C-d` cannot be used as action keys: a terminal sends `C-[` as Esc and `C-i` as Tab, and `C-d` is the prefix itself. That is why the table views use {{< key "C-d (" >}} (`show_data`), {{< key "C-d )" >}} (`show_structure`), {{< key "C-d M-i" >}} (`show_index`) and {{< key "C-d M-d" >}} (`show_ddl`) instead of the TablePlus-style Cmd+[ / Cmd+] / Cmd+i / Cmd+d. Binding `esc` to an action is refused at startup in both UIs.

## Plugin actions

An installed plugin adds an action named `plugin:<name>`. It has no default key unless the plugin declares one, can be rebound under `[keys]` like any other action, and runs in both UIs (the web sends it to the server). A plugin key that clashes with an existing binding is skipped; `webtuidb -keys` prints `plugin shortcut skipped:` with the reason. See [AI and automation](../../automation/).

## Counts

| Kind | Number |
|---|---|
| Keyed actions (distinct names in the keymap) | 162 |
| Bindings (an action can have several keys or contexts) | 183 |
| Menu-only action ids | 56 |
| Open anything features without a key | 10 |
