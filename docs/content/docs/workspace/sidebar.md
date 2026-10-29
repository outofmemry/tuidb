---
title: Sidebar
description: The object tree on the left, its search, pins, recent items, groups, multi-select, menus, schemas and databases, plus the Queries and History tabs.
weight: 20
---

The sidebar lists what is in the database you are connected to: tables, views, functions and the rest, grouped
by kind, with your pinned and recent items on top. It is where you open things, find things by name and reach
the menu of an object.

{{< ui tui="workspace-sidebar-menu" web="workspace-sidebar-menu" >}}The sidebar of the demo database with the menu of the `orders` table open.{{< /ui >}}

## How to reach it

| | Terminal | Web |
|---|---|---|
| Show / hide | {{< key "C-d 0" >}} (`toggle_sidebar`) | {{< key "C-d 0" >}}; on a phone the **Objects** button |
| Give it the keys | {{< key "C-d M-h" >}} or {{< key "C-d C-h" >}} (`focus_left`) | the same keys, or click in it |
| Search by name | {{< key "/" >}} while it has the keys, or {{< key "C-d F" >}} (`sidebar_search`) from it | {{< key "C-d F" >}} focuses the **Filter objects** box |
| Item menu | {{< key "m" >}}, or right click | right click, the **⋯** button on the row, {{< key "Shift+F10" >}} or the menu key |
| Create menu | {{< key "+" >}} | the **+** button |

## What it lists

The first line under the tabs says where you are (`shop.public` and the hint `s switch schema` in the
terminal; the schema picker in the web). Then, in this order:

| Section | Contents |
|---|---|
| **★ Pinned** | Items you pinned, for this connection. |
| **Recent** | The objects you opened last, newest first (the last 10 per connection; the web shows the first 8). Pinned items are not repeated here. `[sidebar] show_recent = false` hides the section. |
| Your groups (**▣ name · count**) | Custom item groups, see below. |
| **Tables · N**, **Views**, **Materialized views**, **Functions**, **Procedures**, **Sequences**, **Types** | Every object of the current schema by kind, sorted by name, with a count. The web calls the tables of MongoDB **Collections**. |

Each row has an icon by kind (`▦` table, `◫` view, `◩` materialized view, `ƒ` function or procedure, `#`
sequence, `T` type) and, dimmed after the name, the detail the driver reports: a table's size on disk
(`orders 312 kB`), a function's arguments (`customer_order_count (p_customer_id integer)`), a type's kind
(`order_status enum`). In the terminal a pinned item has `★` before it and a marked item `✓`; in the web a
pinned item has a filled star on hover and a marked item a `●` and an accent stripe.

Which kinds show depends on the engine and the settings: functions and procedures need an engine that has them
and `show_functions`, sequences need `show_sequences`, types need `show_types`. All three default to on.

### Redis keys

On Redis the sidebar lists keys instead, as a tree split on `:` under **Keys (N)**: `shop:orders:1` sits in the
folder `shop` › `orders`. Each folder shows how many keys it holds. A chain of folders with a single child is
merged into one row (`user:1:name` alone shows as one folder, not three). A key that is also a prefix of other
keys is a folder you can open as a key too: {{< key "Enter" >}} opens it, {{< key "h" >}} / {{< key "l" >}}
fold and unfold it. A search lists matching keys flat.

{{< ui tui="workspace-sidebar-redis" web="workspace-sidebar-redis" >}}The "Cache (Redis)" connection: keys grouped into folders by `:`.{{< /ui >}}

### Columns under a table

With `[sidebar] show_columns = true` every table, view and materialized view gets an arrow (`▸`), and
{{< key "Space" >}} (terminal) or a click on the arrow (web) unfolds its columns with their types. The columns
are fetched the first time you unfold a table. `[sidebar] sort_columns = true` lists them alphabetically
instead of in table order. In the web a primary-key column is marked `◆`.

## Keys in the terminal

These work while the sidebar has the keys.

| Key | Action |
|---|---|
| {{< key "j" >}} / {{< key "k" >}}, ↓ / ↑ | Move (section headers are skipped) |
| {{< key "PgDn" >}} / {{< key "PgUp" >}} | Move ten rows |
| {{< key "g" >}} / {{< key "Home" >}}, {{< key "G" >}} / {{< key "End" >}} | First / last row |
| {{< key "Enter" >}}, {{< key "l" >}}, → | Open the item (a folder: unfold it) |
| {{< key "o" >}} | Open in a new tab |
| {{< key "h" >}}, ← | Fold the Redis folder under the cursor |
| {{< key "Space" >}} | Fold / unfold a Redis folder, or the columns of a table (`show_columns`) |
| {{< key "/" >}} | Search by name |
| {{< key "Esc" >}} | Clear the search and the marks |
| {{< key "1" >}} {{< key "2" >}} {{< key "3" >}}, {{< key "Tab" >}} | Objects, Queries, History tab (Tab cycles) |
| {{< key "m" >}} | Item menu |
| {{< key "+" >}} | Create menu |
| {{< key "p" >}} | Pin / unpin (`pinned orders`, `unpinned orders`) |
| {{< key "N" >}} | New table |
| {{< key "s" >}} | Switch schema |
| {{< key "r" >}} | Reload the list |
| {{< key "S" >}} | Queries tab: sort; History tab: Save As |
| {{< key "i" >}} | Queries tab: import a `.sql` file; History tab: import history |
| {{< key "f" >}} | Queries tab: open a `.sql` file in a tab |
| {{< key "C-d Space" >}} | `mark`: mark / unmark for multi-select (then moves down) |
| {{< key "C-d Delete" >}} | `delete_item`: delete the item (or every marked table) |
| {{< key "C-d M-Delete" >}} | `truncate`: truncate the table (or every marked table) |
| {{< key "C-d M-n" >}} | `new_schema`: create a schema (in the sidebar this key is not `rename_tab`) |

When the cursor rests on a table for a moment, its column list is fetched in the background, so opening it only
has to load rows. Rows of a view are never read just because the cursor passed over it.

In the web, {{< key "Enter" >}} opens the focused row, ↑ / ↓ move between rows, a single click opens the item
(after a short pause), and a double click opens it in a new tab. Drop a CSV, TSV, JSON or SQL file on a table
row to open the import wizard on that table.

## Search

{{< ui tui="workspace-sidebar-search" web="workspace-sidebar-search" >}}Typing `ord` lists the matching objects flat, best match first.{{< /ui >}}

Typing a search replaces the sections with one flat list of matching objects, best match first (in the web the
Pinned and Recent sections are hidden and the kind sections keep only matches). How names match is
`[sidebar] matching`:

| `matching` | A name matches when |
|---|---|
| `fuzzy` (default) | Its letters contain the typed letters in order (`oit` finds `order_items`). Consecutive letters and letters at the start of a word (after `_`, `.`, a space or `:`) rank higher, and shorter names rank above longer ones. |
| `contains` | It contains the typed text, ignoring case. |

In both modes `^text` matches names that start with `text`, `text$` names that end with it, and `^text$` the
exact name.

In the terminal, {{< key "Enter" >}} ends typing and keeps the filter, ↑ / ↓ move while you type, and
{{< key "Esc" >}} clears the search with the same item still selected. In the web, {{< key "Esc" >}} in the box
or the **✕** clears it. On the Queries tab the search matches query names (always fuzzy); on the History tab it
matches anywhere in the SQL text.

## The item menu

The menu lists the entries below, in this order, leaving out those that do not apply. Both UIs read this list
from the same place.

| Entry | Shows for | What it does |
|---|---|---|
| Open | every object | Opens it, replacing the current table tab when it has no pending edits ([tab reuse](../tabs/#tab-reuse)). A function or procedure opens its definition in a SQL editor; a sequence or type shows its DDL. |
| Open in new tab | every object | Always a new tab. |
| New table… | every object | The new-table designer ([Structure](../../structure/)). |
| Open structure | tables, views, materialized views | The table tab on its Structure view. |
| Edit definition | views, materialized views, functions, procedures | The definition as runnable SQL in a SQL editor, so the change goes through safe mode. |
| Triggers… | tables, on engines with triggers | The table's triggers. |
| Show DDL | every object | Terminal: a text window with the DDL (`c` copies). Web: the DDL in a new SQL editor. |
| Copy name | every object | Copies the name (the web copies `schema.name`). |
| Copy script: CREATE / DROP | every object | Copies the statement. |
| Copy script: TRUNCATE, Copy script: SELECT top 100 | tables | Copies the statement. |
| Export… | every object | The export dialog for this object ([Import, export](../../import-export/)). |
| Export several tables… | tables, views, materialized views | A checklist of the tables of every schema, this one ticked; each ticked table goes to its own file. |
| Import into this table… | tables | The import wizard on this table. |
| Clone… | tables | Terminal form **Clone orders**: *New name* (default `orders_clone`) and *Duplicate content* (on, off on Cassandra, which cannot copy rows). Web: asks the name, then `Copy the rows too, or only the structure?`. |
| Rename… | tables | Asks the new name. In the terminal, open tabs of the table close. |
| Truncate… | tables | See [Delete and truncate](#delete-and-truncate). |
| Delete… | every object | See [Delete and truncate](#delete-and-truncate). |
| Pin / unpin | every object | Adds it to or removes it from **★ Pinned**. |
| Mark / unmark | every object | Multi-select, see below. |
| Add to group… | every object | Puts it in a custom group. |
| Remove from group | items in a group | Takes it out of the group (when it is in several: the terminal asks which, the web lists one entry per group). |
| Diagram | tables, views, materialized views | Opens the ER diagram focused on it ([Tools](../../tools/)). |
| Set colour… | tables, views, materialized views | Gives the item a colour. |
| New schema… / Drop schema… | engines with schemas | See [Schemas](#schemas). |

Every change these entries make goes through the safe-mode gate of the connection before it is sent. In the
web, a read-only connection leaves out New table, New schema, Drop schema, Import, Clone, Rename, Truncate and
Delete.

**Set colour…** in the terminal offers the ER diagram's palette (`default` removes it) and paints the item's
name in that colour; the colour is stored with the connection in `state.json` and the diagram uses the same one.
The web offers six swatches, a colour picker and **None**, shows the colour as a dot before the name, and keeps
it in the browser's local storage (per connection, not shared with the terminal).

### Delete and truncate

The terminal opens a form titled **Delete orders?** (or **Truncate orders?**) with:

| Field | Meaning |
|---|---|
| Cascade | Also drop or truncate what depends on it. |
| Restart identity | Truncate on PostgreSQL only: reset the table's sequences. |
| Type its name | The cursor starts here; the form only goes through when you type the exact name (`type “orders” to confirm`). |

The web asks for the name first (`Delete orders? This cannot be undone. Type its name to confirm`; a wrong name
says `Type "orders" to confirm; nothing changed.`), then, on SQL engines, whether to include what depends on it
(**Drop with dependents** / **Drop only this**), and for a PostgreSQL truncate whether to reset the sequences.
It then shows the statements before anything is sent.

In the terminal, the tabs of a deleted table close. {{< key "C-d Delete" >}} and {{< key "C-d M-Delete" >}} act on
the item under the cursor (web: the item you clicked last). Only tables can be truncated.

## Multi-select

Mark several tables, then delete, truncate, export or group them in one go.

{{< ui tui="workspace-sidebar-marks" web="workspace-sidebar-marks" >}}Three tables marked: the terminal ticks them and shows `3 marked` on the sidebar's border; the web adds a bar with Export, Group, Truncate, Drop and Clear.{{< /ui >}}

| | Terminal | Web |
|---|---|---|
| Mark / unmark | {{< key "C-d Space" >}} on the row (moves down after), or Mark / unmark in the menu. Only tables, views and materialized views can be marked (`only tables and views can be marked`). | Mark / unmark in the menu, or {{< key "C-d Space" >}} on the item you clicked last. |
| Delete / truncate | {{< key "C-d Delete" >}} / {{< key "C-d M-Delete" >}}: one form for all of them, with Cascade, and you type the **number** of tables to confirm (`Type 3`), so a stale selection cannot be confirmed by habit. | **Drop…** / **Truncate…** on the bar (truncate takes only the tables): the statements are shown for review. |
| Export | {{< key "C-d E" >}} with the sidebar focused: one form, **Export 3 tables: one file each**: *Format* (`csv`, `json`, `sql`, `xlsx`), *Gzip* (not for xlsx), *Include structure* (sql), *Folder* (default `.`). It runs as one background job. | **Export…** on the bar opens the several-tables export with the marked tables. |
| Group | New item group… in the + menu takes the marked tables. | **Group…** on the bar. |
| Clear | {{< key "Esc" >}} | **Clear** |

Marks are forgotten when the list reloads or you switch database or schema.

{{< ui tui="workspace-sidebar-marks-delete" >}}Deleting two marked tables in the terminal asks you to type the count.{{< /ui >}}

{{< callout type="warning" >}}
On MySQL each `DROP` or `TRUNCATE` commits on its own, so if one statement of a multi-table delete fails, the
ones before it are already applied. The list reloads to show what is left.
{{< /callout >}}

## Custom groups

A group is a named section above the kind sections, holding the objects you put in it (from any kind). Groups
belong to the connection and are stored in `state.json`, so both UIs show the same groups.

- **Add to group…** in the item menu lists the existing groups with their item counts and **＋ New group…**
  (terminal) or a *new group name* box (web).
- **New item group…** in the + menu makes a group from the marked tables; the terminal falls back to the item
  under the cursor (`select an object (or mark some) first: a group is made from the items you put in it`), the
  web needs marks (`Mark some items first (space), then create the group from them`).
- **Remove from group** takes an item out. A group with no items left is removed: there are no empty groups.

## The create menu

{{< ui tui="workspace-sidebar-new" web="workspace-sidebar-new" >}}The + menu on PostgreSQL.{{< /ui >}}

| Entry | Shows when | What it does |
|---|---|---|
| New table… | always | The new-table designer (web: asks the name first; only SQL engines have it, others say `<driver> has no table designer: create it with a query`). |
| New view… | the engine has views | A SQL editor with a `CREATE VIEW` template. |
| New materialized view… | the engine has materialized views | A SQL editor with a `CREATE MATERIALIZED VIEW` template. |
| New function / procedure… | the engine has functions | A SQL editor with a `CREATE FUNCTION` template. |
| New schema… | the engine has schemas | See below. |
| New database… | always | See [Databases](#databases). |
| New item group… | always | See [Custom groups](#custom-groups). |

The templates are written for the connection's dialect and name the object in the current schema, for example
on PostgreSQL:

```sql
CREATE VIEW public.new_view AS
SELECT 1 AS id;
```

The function template on PostgreSQL is a `plpgsql` function returning its argument; MySQL gets a
`DETERMINISTIC` function and ClickHouse a materialized view with `ENGINE = MergeTree ORDER BY tuple()`.
Nothing runs until you run the editor.

The + menu is also an Open Anything entry, **New… (table, view, function, schema, database, group)**.

## Schemas

{{< ui tui="workspace-schema-picker" web="workspace-schema-picker" >}}Switching schema: the terminal picker on `s`, the web's schema list.{{< /ui >}}

{{< key "s" >}} in the terminal opens the **Schema** picker (the current one is marked `current`, type to filter);
the web has the schema list under the filter box. Switching makes that schema the session's current schema, so
the SQL editor, Search in database, autocompletion, Open Anything and the status line follow it, and it survives
a reload.

The server's own schemas are hidden unless `[sidebar] load_system_schemas = true` (the schema you are in always
shows). The terminal treats `information_schema`, `crdb_internal` and every `pg_*` schema as system schemas;
the web also hides `mysql`, `performance_schema`, `sys` and `system`.

With `[sidebar] lazy_load = false` the terminal loads the objects of every schema at start and shows each item's
schema after its name; the default (`true`) loads the current schema only. The web always loads one schema at a
time.

**New schema…** asks for a name (`a schema needs a name`) and runs `CREATE SCHEMA` through the safe-mode gate.
**Drop schema…** in the terminal picks the schema (**Drop which schema?**), then a form with *Cascade (drops
what is in it)* and *Type its name*. In the web, schema create and drop are offered on PostgreSQL only: drop asks
for the schema's name (the current one is filled in), asks you to type it again, then **Drop with its contents**
or **Drop only if empty**, and shows the statements. Dropping the schema you are in moves you to `public`, or
the first other schema. On MySQL and ClickHouse a schema is a database: the terminal says `<driver> has no
schemas (a MySQL or ClickHouse schema is a database)`, the web says `A schema is a database here: use New
database`.

## Databases

{{< ui tui="workspace-database-picker" web="workspace-database-picker" >}}The database picker ({{< key "C-d k" >}}) on the demo server.{{< /ui >}}

`open_database` ({{< key "C-d k" >}}) lists the server's databases (the engine's word is used: database,
keyspace…). In the terminal the first entry is **＋ New database…**, the open one is marked `current`, typing
filters, {{< key "Enter" >}} switches to the database and {{< key "C-Enter" >}} or {{< key "M-Enter" >}} opens
its menu. In the web the database name in the top bar (`shop ▾`, shown only when there is more than one)
opens the list; each row has a **⋯** button, and right click or {{< key "Shift+F10" >}} opens the menu. With a
single database the key says `Only one database here`.

{{< ui tui="workspace-database-menu" web="workspace-database-menu" >}}The menu of a database.{{< /ui >}}

| Entry | Shows when | What it does |
|---|---|---|
| Open in current tab | always | Switches this workspace to it (all tabs close; you are asked first when some have pending edits). |
| Open in new workspace | always | Opens the same connection again, on that database, as a second workspace. |
| Copy database name | always | Copies the name. |
| New database… | always | **New database**: *Name*; *Encoding* and *Collation* on PostgreSQL and MySQL (empty = the server default); *Replication strategy* (default `SimpleStrategy`) and *Replication* (a factor such as `1`, or `dc1:3,dc2:2`) on Cassandra. |
| Rename… | the engine can rename databases | Asks the new name. The open database cannot be renamed (`the open database cannot be renamed; open another one first`). |
| Drop… | always | You type the name to confirm. The open database cannot be dropped. |

All three changes go through the safe-mode gate. What switching does to sessions and tabs is described in
[Sessions and databases](../../connections/sessions-and-databases/).

## Queries and History

The terminal sidebar has two more tabs; the web puts the same lists in drawers opened by the **Saved queries**
and **History** toolbar buttons.

{{< ui tui="workspace-sidebar-queries" web="workspace-sidebar-queries" >}}Saved queries: the sidebar's Queries tab and the web drawer.{{< /ui >}}

**Queries** (`2`) lists the saved queries with their folder and keywords; a query from a `[queries] shared_dirs`
folder has a `⇄` icon (web: `shared`) and is read only. {{< key "Enter" >}} opens it in a new SQL editor that
saves back to the file (not for shared ones). {{< key "S" >}} sorts by name, created or modified; {{< key "i" >}}
imports a `.sql` file as a saved query; {{< key "f" >}} opens a `.sql` file in a tab without saving it. The menu
has Open, Open in new tab, Insert into editor, Rename…, Move to folder…, Reveal in file manager and Delete…
(Rename, Move and Delete are left out, or greyed in the web, for shared queries). The web groups queries by
folder (click a folder to fold it, drag a query onto a folder to move it) and has **Import…**, **Open file…**,
**Open**, **Run** and **Reveal** buttons.

{{< ui tui="workspace-sidebar-history" web="workspace-sidebar-history" >}}History: the sidebar's History tab and the web drawer.{{< /ui >}}

**History** (`3`) lists the statements run on this connection, newest first (the terminal loads the newest 500,
showing the time for today and the date otherwise). {{< key "Enter" >}} opens one in a new SQL editor.
{{< key "S" >}} is **Save history as** (*Format* `csv` or `json`, *File* default `history.csv`) and {{< key "i" >}}
imports a csv or json history file as a background job that reports `imported N new entries (M in the file)`.
The menu has Insert into editor, Run in a new tab, Add to saved queries… (*Name*, *Keywords*, *Folder*), Copy and
Clear all… (`Clear all history of <connection>?`). The web drawer has a search box and **Save As…**, **Import…**,
**Clear**, and **Run**, **⋯** and **Delete** on each entry.

Saved queries and history are covered in depth in [Saved queries](../../sql-editor/saved-queries/) and
[History](../../sql-editor/history/).

## Settings

| Setting | Default | Effect |
|---|---|---|
| `[sidebar] show_recent` | `true` | Show the Recent section. |
| `[sidebar] show_functions` | `true` | Show functions and procedures. |
| `[sidebar] show_sequences` | `true` | Show sequences. |
| `[sidebar] show_types` | `true` | Show types. |
| `[sidebar] matching` | `fuzzy` | `fuzzy` or `contains`, for the search. |
| `[sidebar] show_columns` | `false` | Columns under each table. |
| `[sidebar] sort_columns` | `false` | Those columns alphabetically. |
| `[sidebar] load_system_schemas` | `false` | List the server's own schemas. |
| `[sidebar] lazy_load` | `true` | Terminal only: load one schema's objects at a time (`false` loads every schema at start). |
| `[queries] shared_dirs` | empty | Extra, read-only saved-query folders. |

See the [settings reference](../../configuration/settings-reference/).
