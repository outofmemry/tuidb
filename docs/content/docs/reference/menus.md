---
title: Context menus
description: Every context menu in webtuidb, in the terminal and in the browser, with each entry in order, when it is shown and which UI has it.
weight: 30
---

Both UIs build their context menus from one list in the engine (`internal/menus`), so the entries, their order and the conditions that hide them are the same in the terminal and in the browser. The web reads the list from `GET /api/meta`; a parity test fails the build when either UI renders a menu that differs from it. This page lists every menu entry by entry. Each entry runs an [action](../actions/); entries that also have a key show it in the help screens.

{{< ui tui="reference-menu-sidebar-table" web="reference-menu-sidebar-table" >}}The menu of the `orders` table in the sidebar. In the terminal it opens with `m` on the selected item; in the browser, with a right-click.{{< /ui >}}

## How to open a menu

| Menu | Terminal | Web |
|---|---|---|
| Sidebar object, saved query, history entry | `m` on the selected sidebar row | right-click the item, or focus it and press the menu key or {{< key "S-F10" >}} |
| Sidebar "+" (new object) | `+` in the sidebar, or "New… (table, view, function, schema, database, group)" in {{< key "C-d p" >}} | the **+** button at the top of the sidebar |
| Database | {{< key "C-d k" >}}, select a database, then {{< key "C-Enter" >}} or {{< key "M-Enter" >}} | right-click a database in the database switcher |
| Grid cell / row | right-click a cell of a table's data tab | right-click a cell, or {{< key "S-F10" >}} on the focused cell |
| Query result cell | right-click a result cell | right-click a result cell |
| Column header | {{< key "C-d M-H" >}} (`column_menu`) on the column under the cursor, in the grid or in results | right-click a column header, or {{< key "C-d M-H" >}} |
| Filter bar | right-click the filter bar | the bar's own buttons; the same entries |
| Connection | `m` on a connection in the connection list | right-click a connection card, or its **⋯** button |
| Tab | right-click a tab, or "Tab menu" in {{< key "C-d p" >}} | right-click a tab |
| Structure row | `m` on a row of the structure editor (columns, indexes, foreign keys) | right-click a row of the structure editor |
| Process list | `m` on a process ({{< key "C-d ." >}} opens the list) | right-click a process, or its actions button |
| Console | `m` (`console_menu`, no prefix) while the console has focus, or right-click it | right-click the console, or `m` while it has focus |
| Jobs centre | `Enter` or `m` on a job ({{< key "C-d M-J" >}} opens the centre) | the job's buttons in the jobs drawer |

In the terminal a menu is a picker: {{< key "Up" >}} / {{< key "Down" >}} (or {{< key "C-p" >}} / {{< key "C-n" >}}) move, {{< key "PgUp" >}} / {{< key "PgDn" >}} jump eight rows, {{< key "Enter" >}} runs the entry, {{< key "Esc" >}} closes the menu. In the browser, the arrow keys move, {{< key "Enter" >}} runs, {{< key "Esc" >}} closes, and a click outside closes the menu.

### Entries one UI cannot run

An entry that only one UI can carry out stays visible in the other, so both menus keep the same shape:

- In the **terminal**, a web-only entry is dimmed with `web only:` and the reason beside it; choosing it shows the reason instead of running.
- In the **web**, a terminal-only entry is greyed out; hovering it shows the reason.

The reasons are listed in the **UI** column of each table below.

### Conditions

The **Shown** column says when an entry appears. The conditions are:

- an object kind ("only on tables"): the menu was opened on that kind of object;
- "tables, views and materialized views" (the engine's *tableish* kinds) and "views, materialized views, functions and procedures" (objects with an editable *definition*);
- a capability of the connection ("only when the engine has triggers"): what the database engine supports, as listed in [Engines](../../engines/);
- "the result is editable": a query result that comes from a plain `SELECT` on one table that includes its primary key;
- "in a custom group": the object is a member of a sidebar group.

MongoDB collections and Redis keys are shown as tables in the sidebar, so they get the table menu, with the entries their engine cannot do refused when chosen.

## Sidebar object

The menu on a table, view, materialized view, function, procedure, sequence or type in the sidebar (`sidebar_object`).

| # | Entry | Action | Shown | UI |
|---|---|---|---|---|
| 1 | Open | `open` | always | Both |
| 2 | Open in new tab | `open_new_tab` | always | Both |
| 3 | New table… | `new_table` | always | Both |
| 4 | Open structure | `show_structure` | only on tables, views and materialized views | Both |
| 5 | Edit definition | `edit_definition` | only on views, materialized views, functions and procedures | Both |
| 6 | Triggers… | `triggers` | only on tables; only when the engine has triggers | Both |
| 7 | Show DDL | `show_ddl` | always | Both |
| 8 | Copy name | `copy_name` | always | Both |
| 9 | Copy script: CREATE | `script_create` | always | Both |
| 10 | Copy script: DROP | `script_drop` | always | Both |
| 11 | Copy script: TRUNCATE | `script_truncate` | only on tables | Both |
| 12 | Copy script: SELECT top 100 | `script_select100` | only on tables | Both |
| 13 | Export… | `export` | always | Both |
| 14 | Export several tables… | `export_many` | only on tables, views and materialized views | Both |
| 15 | Import into this table… | `import` | only on tables | Both |
| 16 | Clone… | `clone` | only on tables | Both |
| 17 | Rename… | `rename` | only on tables | Both |
| 18 | Truncate… | `truncate` | only on tables | Both |
| 19 | Delete… | `delete_item` | always | Both |
| 20 | Pin / unpin | `pin` | always | Both |
| 21 | Mark / unmark | `mark` | always | Both |
| 22 | Add to group… | `group_add` | always | Both |
| 23 | Remove from group | `group_remove` | only when the item is in a custom group (one entry per group) | Both |
| 24 | Diagram | `diagram` | only on tables, views and materialized views | Both |
| 25 | Set colour… | `set_color` | only on tables, views and materialized views | Both |
| 26 | New schema… | `new_schema` | only when the connection has schemas (PostgreSQL and its relatives) | Both |
| 27 | Drop schema… | `drop_schema` | only when the connection has schemas (PostgreSQL and its relatives) | Both |

### By object kind

What the menu holds for each kind of object, in order. A name in brackets after an entry is the capability (or condition) it needs.

| Object | Entries |
|---|---|
| Tables | Open, Open in new tab, New table…, Open structure, Triggers… *(Triggers)*, Show DDL, Copy name, Copy script: CREATE, Copy script: DROP, Copy script: TRUNCATE, Copy script: SELECT top 100, Export…, Export several tables…, Import into this table…, Clone…, Rename…, Truncate…, Delete…, Pin / unpin, Mark / unmark, Add to group…, Remove from group *(in a group)*, Diagram, Set colour…, New schema… *(Schemas)*, Drop schema… *(Schemas)* |
| Views | Open, Open in new tab, New table…, Open structure, Edit definition, Show DDL, Copy name, Copy script: CREATE, Copy script: DROP, Export…, Export several tables…, Delete…, Pin / unpin, Mark / unmark, Add to group…, Remove from group *(in a group)*, Diagram, Set colour…, New schema… *(Schemas)*, Drop schema… *(Schemas)* |
| Materialized views | Open, Open in new tab, New table…, Open structure, Edit definition, Show DDL, Copy name, Copy script: CREATE, Copy script: DROP, Export…, Export several tables…, Delete…, Pin / unpin, Mark / unmark, Add to group…, Remove from group *(in a group)*, Diagram, Set colour…, New schema… *(Schemas)*, Drop schema… *(Schemas)* |
| Functions | Open, Open in new tab, New table…, Edit definition, Show DDL, Copy name, Copy script: CREATE, Copy script: DROP, Export…, Delete…, Pin / unpin, Mark / unmark, Add to group…, Remove from group *(in a group)*, New schema… *(Schemas)*, Drop schema… *(Schemas)* |
| Procedures | Open, Open in new tab, New table…, Edit definition, Show DDL, Copy name, Copy script: CREATE, Copy script: DROP, Export…, Delete…, Pin / unpin, Mark / unmark, Add to group…, Remove from group *(in a group)*, New schema… *(Schemas)*, Drop schema… *(Schemas)* |
| Sequences | Open, Open in new tab, New table…, Show DDL, Copy name, Copy script: CREATE, Copy script: DROP, Export…, Delete…, Pin / unpin, Mark / unmark, Add to group…, Remove from group *(in a group)*, New schema… *(Schemas)*, Drop schema… *(Schemas)* |
| Types | Open, Open in new tab, New table…, Show DDL, Copy name, Copy script: CREATE, Copy script: DROP, Export…, Delete…, Pin / unpin, Mark / unmark, Add to group…, Remove from group *(in a group)*, New schema… *(Schemas)*, Drop schema… *(Schemas)* |

### Read-only connections

On a connection with `read_only = true`, the **web** leaves out the entries that would change the database: New table…, New schema…, New database…, Drop schema…, Import into this table…, Clone…, Rename…, Truncate…, Delete…. The **terminal** keeps them in the menu; the connection's read-only guard refuses the statement when it is sent. See [Safety](../../safety/).

### Marked items

`Mark / unmark` (or `space`, the `mark` action, in the terminal sidebar) adds the item to a multi-selection. With items marked, Delete… and Truncate… act on every marked item (Truncate only on the tables among them), and "New item group…" in the "+" menu turns the marked items into a group. A group with no items is not kept, so a group always starts from marked items.

## Saved query

The menu on a saved query in the sidebar (terminal) or the Saved queries drawer (web) (`sidebar_query`).

| # | Entry | Action | Shown | UI |
|---|---|---|---|---|
| 1 | Open | `open` | always | Both |
| 2 | Open in new tab | `open_new_tab` | always | Both |
| 3 | Insert into editor | `insert` | always | Both |
| 4 | Rename… | `rename` | always | Both |
| 5 | Move to folder… | `move` | always | Both |
| 6 | Reveal in file manager | `reveal` | always | Both |
| 7 | Delete… | `delete_item` | always | Both |

A query from a shared folder (`[queries] shared_dirs`) is read-only: the terminal leaves Rename…, Move to folder… and Delete… out of its menu; the web shows them greyed out with "a shared query is read only". "Reveal in file manager" in the web opens the folder when the browser runs on the server's machine; otherwise it copies the full path and says the browser cannot open a folder on another machine.

## History entry

The menu on a statement in the query history (`sidebar_history`).

| # | Entry | Action | Shown | UI |
|---|---|---|---|---|
| 1 | Insert into editor | `insert` | always | Both |
| 2 | Run in a new tab | `open_new_tab` | always | Both |
| 3 | Add to saved queries… | `add_to_queries` | always | Both |
| 4 | Copy | `copy` | always | Both |
| 5 | Clear all… | `clear_all` | always | Both |

## Sidebar "+" (new object)

The **+** menu of the sidebar (`sidebar_new`). New view…, New materialized view… and New function / procedure… open a `CREATE` template for the engine in a new editor tab, which you finish and run.

| # | Entry | Action | Shown | UI |
|---|---|---|---|---|
| 1 | New table… | `new_table` | always | Both |
| 2 | New view… | `new_view` | only when the engine has views | Both |
| 3 | New materialized view… | `new_matview` | only when the engine has materialized views | Both |
| 4 | New function / procedure… | `new_function` | only when the engine has functions and procedures | Both |
| 5 | New schema… | `new_schema` | only when the connection has schemas (PostgreSQL and its relatives) | Both |
| 6 | New database… | `new_database` | always | Both |
| 7 | New item group… | `new_group` | always | Both |

## Database

The menu on a database in the database picker (`database`).

| # | Entry | Action | Shown | UI |
|---|---|---|---|---|
| 1 | Open in current tab | `open` | always | Both |
| 2 | Open in new workspace | `open_workspace` | always | Both |
| 3 | Copy database name | `copy_name` | always | Both |
| 4 | New database… | `new_database` | always | Both |
| 5 | Rename… | `rename` | only when the engine can rename a database | Both |
| 6 | Drop… | `drop` | always | Both |

## Grid cell and row

The menu on a cell of a table's data tab (`grid`). Row entries act on the selected rows, cell entries on the selected cells.

{{< ui tui="reference-menu-grid" web="reference-menu-grid" >}}The cell menu of the `orders` data tab. The terminal opens it with a right-click on a cell; the browser too.{{< /ui >}}

| # | Entry | Action | Shown | UI |
|---|---|---|---|---|
| 1 | Quick look | `quick_look` | always | Both |
| 2 | Set value… | `set_value` | always | Both |
| 3 | Add row | `add_row` | always | Both |
| 4 | Duplicate rows | `duplicate_rows` | always | Both |
| 5 | Delete rows | `delete_rows` | always | Both |
| 6 | Copy rows | `copy_rows` | always | Both |
| 7 | Copy rows as… | `copy_rows_as` | always | Both |
| 8 | Copy selected cells | `copy_cells` | always | Both |
| 9 | Copy selected cells as… | `copy_cells_as` | always | Both |
| 10 | Copy all column values | `copy_column` | always | Both |
| 11 | Paste rows | `paste_rows` | always | Both |
| 12 | Paste into selected cells | `paste_cells` | always | Both |
| 13 | Quick filter on this cell | `quick_filter` | always | Both |
| 14 | Open the row a foreign key points to | `follow_fk` | always | Both |
| 15 | Hide this column | `hide_column` | always | Both |
| 16 | Show all columns | `show_all_columns` | always | Both |
| 17 | Sort by several columns… | `sort_columns` | always | Both |
| 18 | Bulk update… | `bulk_update` | always | Both |
| 19 | Row limit and offset… | `page_settings` | always | Both |
| 20 | Count rows exactly | `count_exact` | always | Both |
| 21 | Export… | `export` | always | Both |
| 22 | Import… | `import` | always | Both |

On a read-only table (a view, a table without a primary key, a read-only connection) the editing entries are refused with "*name* is read-only".

## Query result cell

The menu on a cell of a query result (`result`). The editing entries appear only for an editable result.

| # | Entry | Action | Shown | UI |
|---|---|---|---|---|
| 1 | Quick look | `quick_look` | always | Both |
| 2 | Set value… | `set_value` | only when the result is editable (a plain single-table SELECT with its primary key) | Both |
| 3 | Add row | `add_row` | only when the result is editable (a plain single-table SELECT with its primary key) | Both |
| 4 | Duplicate rows | `duplicate_rows` | only when the result is editable (a plain single-table SELECT with its primary key) | Both |
| 5 | Delete rows | `delete_rows` | only when the result is editable (a plain single-table SELECT with its primary key) | Both |
| 6 | Copy rows | `copy_rows` | always | Both |
| 7 | Copy rows as… | `copy_rows_as` | always | Both |
| 8 | Copy selected cells | `copy_cells` | always | Both |
| 9 | Copy selected cells as… | `copy_cells_as` | always | Both |
| 10 | Copy all column values | `copy_column` | always | Both |
| 11 | Toggle chart | `chart` | always | Both |
| 12 | Load more | `load_more` | always | Both |
| 13 | Pin result | `pin` | always | Both |
| 14 | Export… | `export` | always | Both |
| 15 | Add to metrics board… | `add_to_board` | always | Terminal only; greyed out in the web: the web board form takes no query yet |

Differences between the two UIs:

- **Add to metrics board…** is terminal only: the web board form takes no query yet. Create the panel in the terminal, or add the query to a board in the board's own editor.
- **Duplicate rows**, **Delete rows** and **Set value…** act on every selected row in the terminal and on the selected row only in the web.
- **Load more** re-runs the statement for more rows; in the web the grown result is no longer editable.
- **Toggle chart**: the terminal draws a line chart as bars.

## Column header

The menu on a column header (`header`), shared by a table's data tab and query results.

| # | Entry | Action | Shown | UI |
|---|---|---|---|---|
| 1 | Sort ascending | `sort_asc` | always | Both |
| 2 | Sort descending | `sort_desc` | always | Both |
| 3 | Remove sort | `sort_none` | always | Both |
| 4 | Sort by several columns… | `sort_columns` | always | Both |
| 5 | Filter with this column | `filter_column` | always | Both |
| 6 | Hide this column | `hide_column` | always | Both |
| 7 | Show all columns | `show_all_columns` | always | Both |
| 8 | Copy column name | `copy_name` | always | Both |
| 9 | Copy all column values | `copy_column` | always | Both |
| 10 | Copy column definition | `copy_definition` | always | Both |

In **query results** some entries belong to table tabs. The web greys out *Filter with this column* ("Filters belong to table tabs: add a WHERE to the query"), *Hide this column* and *Show all columns* ("Hiding columns applies to table tabs: select fewer columns in the query"). The terminal keeps them and answers with a note; its *Filter with this column* opens the result's find prompt on that column, and *Sort by several columns…* points you to the table's data tab, because terminal results sort by one column. In the web, results can be sorted by several columns.

## Filter bar

The filter bar's menu and buttons (`filter_bar`). Three of them also have keys that work without the prefix while the bar has focus: {{< key "M-c" >}} (`filter_clear_all`), {{< key "M-e" >}} (`filter_export`) and {{< key "M-n" >}} (`filter_add_copy`).

| # | Entry | Action | Shown | UI |
|---|---|---|---|---|
| 1 | Apply | `apply_filters` | always | Both |
| 2 | Add filter | `filter_add` | always | Both |
| 3 | Add a filter as a copy | `filter_add_copy` | always | Both |
| 4 | Show SQL | `filter_sql` | always | Both |
| 5 | Clear all | `filter_clear_all` | always | Both |
| 6 | Export filtered rows | `filter_export` | always | Both |

## Connection

The menu on a saved connection, on the terminal's connection list and the web's connections page (`welcome`).

| # | Entry | Action | Shown | UI |
|---|---|---|---|---|
| 1 | Connect | `connect` | always | Both |
| 2 | Edit… | `edit` | always | Both |
| 3 | Duplicate | `duplicate` | always | Both |
| 4 | Delete… | `delete_item` | always | Both |
| 5 | Test | `test` | always | Both |
| 6 | Copy URL | `copy_url` | always | Both |
| 7 | Copy URL with password | `copy_url_password` | always | Both |
| 8 | Import URL… | `import_url` | always | Both |
| 9 | Export connections… | `export_connections` | always | Both |
| 10 | Import connections… | `import_connections` | always | Both |
| 11 | Sort by… | `sort` | always | Both |

A connection that comes from a `[[source]]` folder is read-only here: the web greys out Edit… and Delete… with "this connection comes from a connection source file: edit that file"; the terminal refuses the change with "*name* comes from *file*: edit that file (duplicate makes a local copy)".

## Tab

The menu on a tab in the tab bar (`tab`).

{{< ui tui="reference-menu-tab" web="reference-menu-tab" >}}The tab menu. The last two entries are web only: the terminal shows them dimmed with the reason.{{< /ui >}}

| # | Entry | Action | Shown | UI |
|---|---|---|---|---|
| 1 | Close | `close_tab` | always | Both |
| 2 | Close other tabs | `close_other_tabs` | always | Both |
| 3 | Close tabs to the right | `close_tabs_right` | always | Both |
| 4 | Close all tabs | `close_all_tabs` | always | Both |
| 5 | Rename tab… | `rename_tab` | always | Both |
| 6 | Move left | `move_tab_left` | always | Both |
| 7 | Move right | `move_tab_right` | always | Both |
| 8 | Open workspace in a new browser tab | `open_window` | always | Web only; dimmed in the terminal: the terminal has one window |
| 9 | Copy link | `copy_link` | always | Web only; dimmed in the terminal: the terminal has no URLs |

## Structure row

The menu on a row of the structure editor: a column, an index or a foreign key (`structure`).

| # | Entry | Action | Shown | UI |
|---|---|---|---|---|
| 1 | Add | `add_row` | always | Both |
| 2 | Delete | `delete_rows` | always | Both |
| 3 | Set value / toggle | `set_value` | always | Both |
| 4 | Move up | `move_up` | only when the engine can reorder columns (MySQL, MariaDB, ClickHouse) | Both |
| 5 | Move down | `move_down` | only when the engine can reorder columns (MySQL, MariaDB, ClickHouse) | Both |
| 6 | Copy as… | `copy_rows_as` | always | Both |

## Process list

The menu on a server process in the process list (`process_list`).

| # | Entry | Action | Shown | UI |
|---|---|---|---|---|
| 1 | Kill selected | `kill` | always | Both |
| 2 | Quick look | `quick_look` | always | Both |
| 3 | Copy as… | `copy_rows_as` | always | Both |
| 4 | Export… | `export` | always | Both |
| 5 | Dismiss | `close` | always | Both |

## Console

The console pane's menu (`console`). The same four things have no-prefix keys while the console has focus: `/` find, `y` copy line, `e` errors only, `m` this menu (`n` / `N` step through find matches).

| # | Entry | Action | Shown | UI |
|---|---|---|---|---|
| 1 | Find… | `find` | always | Both |
| 2 | Copy line | `copy` | always | Both |
| 3 | Errors only | `errors_only` | always | Both |
| 4 | Clear | `clear_console` | always | Both |

In the web, *Errors only* keeps the lines that failed, or whose text says `error`, `failed` or `exception` when the server did not mark them.

## Jobs centre

The menu on a background job: an import, export, backup, restore, search or diff (`jobs`).

| # | Entry | Action | Shown | UI |
|---|---|---|---|---|
| 1 | Stop | `cancel` | always | Both |
| 2 | Open folder | `reveal` | always | Terminal only; greyed out in the web: the browser downloads the file instead |
| 3 | Download | `download` | always | Web only; dimmed in the terminal: the terminal reveals the file instead |
| 4 | Dismiss | `close` | always | Both |

## See also

- [Actions](../actions/): every action name, its key and where it is documented.
- [Dialogs](../dialogs/): what the `…` entries open.
- [Key bindings](../../configuration/key-bindings/): giving a menu entry a key.
