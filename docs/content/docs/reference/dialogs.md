---
title: Dialogs
description: Every dialog in webtuidb, with what opens it, its fields, and the keys that confirm or cancel it, in the terminal and in the browser.
weight: 40
---

Every dialog exists in both UIs. The engine keeps a list of dialog pairs (`internal/menus/dialogs.go`), one terminal dialog and its web twin per entry, and a parity test fails the build when one half is missing. This page lists all 31 pairs: what opens each one, what it asks for, and how to confirm or cancel it. The feature pages linked in the last column describe each dialog in full.

{{< ui tui="reference-dialog-help" web="reference-dialog-help" >}}The key bindings dialog ({{< key "C-d ?" >}}), the dialog both UIs keep identical: the focused pane's bindings first, then the global ones, with the same names, descriptions and order.{{< /ui >}}

## Keys inside dialogs

The terminal has five kinds of dialog, and each kind answers the same keys wherever it appears:

| Kind | Confirm | Cancel | Move and change |
|---|---|---|---|
| **Form** (fields to fill in) | {{< key "Enter" >}} | {{< key "Esc" >}} | {{< key "Tab" >}} / {{< key "Down" >}} next field, {{< key "S-Tab" >}} / {{< key "Up" >}} previous; {{< key "Space" >}}, {{< key "Left" >}} or {{< key "Right" >}} toggle a yes/no field or step through a choice. A failed check keeps the form open with the error under it, nothing you typed is lost. |
| **Picker** (a list to choose from) | {{< key "Enter" >}} | {{< key "Esc" >}} | {{< key "Up" >}} / {{< key "Down" >}} or {{< key "C-p" >}} / {{< key "C-n" >}}, {{< key "PgUp" >}} / {{< key "PgDn" >}} by eight; in a searchable picker, typing filters the list. Some pickers add keys of their own, shown in their bottom edge. |
| **Confirmation** (yes or no) | `y`, or {{< key "Enter" >}} | `n`, `q` or {{< key "Esc" >}} | {{< key "Up" >}} / {{< key "Down" >}} (or `k` / `j`) scroll long text. On a destructive confirmation (drawn with a red edge) {{< key "Enter" >}} means *no*: only `y` goes ahead, so a reflexive Enter never drops or deletes. Keys pressed in the first moment after the dialog appears are ignored. |
| **Checklist** (several boxes) | {{< key "Enter" >}} | {{< key "Esc" >}} | {{< key "Space" >}} or `x` ticks the row, `a` ticks all (or clears all when all are ticked), `A` clears all, `j` / `k` move. |
| **Text view** (read-only text) | | {{< key "Esc" >}} or `q` | {{< key "Up" >}} / {{< key "Down" >}}, {{< key "PgUp" >}} / {{< key "PgDn" >}} scroll; extra actions are named in the bottom edge. |

In the **web**, every dialog closes with {{< key "Esc" >}} (an open drop-down inside it takes the first Esc), {{< key "Tab" >}} cycles through its controls without leaving it, and the buttons do the rest. A dialog that is a form submits with {{< key "Enter" >}} from a text field. Focus returns to where it was when the dialog closes. The primary button is the highlighted one; the destructive ones are red.

## All dialogs

| Dialog | Opens from | Fields and choices | Terminal | Web | Documented in |
|---|---|---|---|---|---|
| **Set value** | `set_value` ({{< key "C-d M-Enter" >}}) on a grid cell, "Set value…" in the cell menu | NULL, DEFAULT, EMPTY, NOW() (`now()` on ClickHouse), Date picker… (date `YYYY-MM-DD`, optional time `HH:MM:SS`), Add a file… (up to 16 MB, held until commit), Advanced… (an SQL expression, checked before it is used) | picker, then a form for the three extras | one dialog with buttons EMPTY, NULL, DEFAULT, NOW() and on PostgreSQL gen_random_uuid(); a date-time field, a file field, an expression field and, when the schema has sequences, a Sequence list that sets `nextval(…)`. Only common functions (`now()`, `uuid()`, …) run without the server's advanced flag | [Editing data](../../editing-data/) |
| **Quick filter modes** | `quick_filter` ({{< key "C-d x" >}}), "Quick filter on this cell" | `=`, `<>`, Contains, Has prefix, `IN (…)` with the cell value; IS NULL, IS NOT NULL | picker titled "Quick filter: *column*" | no dialog: the web applies `=` with the cell value (IS NULL for a NULL cell) at once | [Browsing data](../../browsing-data/) |
| **Page settings** | `page_settings` ({{< key "C-d L" >}}), "Row limit and offset…" | Limit, Offset | form "Rows per page and offset" | "Row limit and offset" with two number fields | [Browsing data](../../browsing-data/) |
| **Count rows exactly** | `count_exact` ({{< key "C-d #" >}}), "Count rows exactly" | none: the estimated count is replaced by an exact `COUNT(*)` | status line "counting rows…", then the count; "the count is already exact" when it was | the footer's "Count exactly" link turns into the count | [Browsing data](../../browsing-data/) |
| **Clone** | "Clone…" on a table in the sidebar | New name (default *table*_clone), Duplicate content (rows too, or structure only) | form "Clone *table*" | a name prompt "Clone as", then "Copy the rows too, or only the structure?" (Structure and rows / Structure only); on Cassandra only the structure | [Structure](../../structure/) |
| **Truncate options** | `truncate` ({{< key "C-d M-Delete" >}} in the sidebar), "Truncate…" | Cascade; Restart identity (PostgreSQL); the table name typed to confirm | one form with both switches and the name field | a typed-name prompt, then "Also truncate everything that depends on…?" and, on PostgreSQL, "Also reset the sequences of the table (restart identity)?" | [Structure](../../structure/) |
| **Delete / drop with the typed name** | `delete_item` ({{< key "C-d Delete" >}} in the sidebar), "Delete…", "Drop…" on a database, "Drop schema…", "Drop user…" | Cascade; "This cannot be undone. Type the name to confirm."; the name | form; a name that does not match changes nothing | prompt "Delete *name*? This cannot be undone. Type its name to confirm", then the dependents question on SQL engines; a schema needs its name typed twice | [Structure](../../structure/) |
| **Safe-mode gate** | sending statements while the connection's safe mode asks: commit, run, any admin change | the statements; the database password at Safe Mode 1 and 2 | confirmation "*level*: send to the server?" or form "*level*: enter the database password"; with no stored password, a red confirmation "*level*: this connection has no stored password. Send?" | "Run these statements?" with the statements, a password field when needed, Cancel / Run; the server checks the gate again, the page cannot skip it | [Safety](../../safety/) |
| **Safe-mode level picker** | `safe_mode` ({{< key "C-d S" >}}) | Silent Mode, Alert Mode 1, Alert Mode 2, Safe Mode 1, Safe Mode 2, each with its rule; the current one marked • | picker "Safe mode for *connection*" | dialog of the same name; leaving a password level asks for the password (or a confirmation when none is stored) | [Safety](../../safety/) |
| **Taking too long** | a statement still running after `[general] query_timeout_secs` | Keep waiting, Cancel the query | picker "The server is taking too long (over *N*s)"; {{< key "Esc" >}} keeps waiting; it closes by itself when the statement finishes | dialog of the same title; Esc or "Keep waiting" waits another period | [SQL editor](../../sql-editor/) |
| **Bootstrap command failure** | a connection's `bootstrap` statement failed on connect | the failed statements and their errors | error box "Bootstrap command failed (the connection is still open)" | a box with the same title; OK and Cancel both continue | [Connections](../../connections/) |
| **Import wizard** | `import` ({{< key "C-d I" >}}), "Import…", "Import into this table…" | File, Format (csv, json, sql), First row is the header, Delimiter, File encoding (utf-8, latin1, windows-1252, utf-16le, utf-16be, shift_jis, gbk), NULL as, Empty is NULL, Keep formula apostrophe, Create the table, Table; then column matching | form "Import data", then "Import *file*" | "Import into *table*" with Source, File, Delimiter, Encoding, NULL as, Match columns by and the same switches | [Import, export](../../import-export/) |
| **Export** | `export` ({{< key "C-d E" >}}), "Export…", `filter_export` | Format (csv, json, sql, xlsx), Header row, NULL written as, Line breaks (LF, CRLF), Gzip, Include structure, Drop table first, Current page only, Pick several tables, All result tabs, File | form "Export *name*" | "Export *table*" with Format, Rows, NULL as, Line breaks, File name; the file downloads | [Import, export](../../import-export/) |
| **Backup** | `backup` ({{< key "C-d b" >}}) | File, File-name pattern, Extra options (with a checklist of common options, {{< key "C-o" >}}), Gzip, Format (plain, custom on PostgreSQL), Data only, Schema only; Tool paths | form, then a confirmation and a live output window | "Back up database"; files live in the `backups` folder of the data directory | [Import, export](../../import-export/) |
| **Restore** | `restore` ({{< key "C-d B" >}}) | File, Extra options, Gzip, Format, Restore into a new database | form, then a red confirmation; if a restore into a new database fails, "Drop the new database *name*?" | "Restore database"; a failed restore into a new database reports that the database is still there | [Import, export](../../import-export/) |
| **Users** | `users` ({{< key "C-d u" >}}) | the user list; New user (Name, Host on MySQL, Password, attributes); per user: Details, Change password, Attributes, Resource / Connection limit, Grant (Object, Privileges, Columns), Revoke, Drop user | picker "Users (*n*)" and forms; every change ends in "Preview: *n* statement(s)" | "Users" with a user form; the same preview and gate | [Tools](../../tools/) |
| **Process list** | `process_list` ({{< key "C-d ." >}}) | the server's sessions | picker "Processes": Enter shows the query, Space marks, `k` kills, `s` sorts, `c` copy as, `e` export, `m` menu, `r` refresh | "Process list" with select-all, sortable headers and an actions button per row | [Tools](../../tools/) |
| **Search in database** | `search_in_database` ({{< key "C-d /" >}}) | Value, Operator (Contains, Not contains, =, Has prefix, Has suffix), Tables (empty: all), All schemas | form; the search runs as a background job | "Search in database"; pause and resume | [Tools](../../tools/) |
| **Variables** | `variables` ({{< key "C-d M-v" >}}), or running a statement with parameters | one field per parameter; the parameter format | form; Enter saves the values and runs with them bound | "Query parameters" with a Format list | [SQL editor](../../sql-editor/) |
| **Find and replace** | `find_replace` ({{< key "C-d M-f" >}}) | Find, Replace with | form "Find and replace"; a multi-line replacement is inserted on one line | the editor's own find and replace panel | [SQL editor](../../sql-editor/) |
| **Settings** | `settings` ({{< key "C-d ," >}}) | every `config.toml` setting by section; a Keys tab | `Enter` edit, `s` save, `r` revert, `/` find, {{< key "Tab" >}} keys, `e` open `$EDITOR` | "Settings" with sections and a search; settings that name a server path are read-only here | [Configuration](../../configuration/) |
| **Keymap editor** | the Keys tab of Settings | Key (e.g. `alt+x`, `ctrl+g`; empty restores the default), pressed after the prefix unless no-prefix | form; a key another action holds is refused with its name | key editor with an action search; the same check | [Configuration](../../configuration/) |
| **Jobs centre** | `jobs` ({{< key "C-d M-J" >}}) | running and finished imports, exports, backups, restores, searches, diffs | list; Enter or `m` opens the job menu | "Jobs" drawer | [Workspace](../../workspace/) |
| **Passcode lock** | `lock` ({{< key "C-d M-L" >}}), or `[security] lock_after_mins` idle minutes | passcode | "🔒 Locked": Enter unlocks; three wrong passcodes refuse input for 10 seconds | full-page lock with a passcode field | [Safety](../../safety/) |
| **Assistant chat history** | `assistant_history` ({{< key "C-d M-Y" >}}) | saved chats of the connection | picker "Assistant chats": type to search, Enter open, {{< key "C-x" >}} delete | chat history list | [AI and automation](../../automation/) |
| **Foreign key form** | "Add foreign key" in the ER diagram | Table, Columns, References table, Referenced columns, ON UPDATE, ON DELETE, Constraint name (empty: generated) | form "Add foreign key" | "Add foreign key" with Name, On update, On delete | [Tools](../../tools/) |
| **New database** | "New database…" (sidebar "+" or database menu) | Name; Encoding and Collation (PostgreSQL, MySQL); Replication strategy and Replication (Cassandra) | form "New *database*" (the engine's word: keyspace, …) | the same fields | [Structure](../../structure/) |
| **New table** | "New table…" | Table name, then the structure editor | form "New table" | "New table" | [Structure](../../structure/) |
| **Column header menu** | `column_menu` ({{< key "C-d M-H" >}}), right-click a header | see [Context menus](../menus/#column-header) | picker "Column *name*" | right-click menu | [Browsing data](../../browsing-data/) |
| **Open anything** | `open_anything` ({{< key "C-d p" >}}) | tables, views, saved queries, databases, features, actions; `^name` anchors the start, `name$` the end | searchable picker; {{< key "C-Enter" >}} / {{< key "M-Enter" >}} opens in a new tab | "Open anything" palette | [Workspace](../../workspace/) |
| **Key bindings help** | `help` ({{< key "C-d ?" >}}) | the focused pane's bindings, then global | `/` search, Enter runs the selected action, Esc closes (or clears the search) | "Key bindings" with a search field | [Getting started](../../getting-started/) |

## Dialogs worth a picture

{{< ui tui="reference-dialog-safe-mode" web="reference-dialog-safe-mode" >}}The safe-mode level picker ({{< key "C-d S" >}}). The current level is marked; each level states its rule.{{< /ui >}}

{{< ui tui="reference-dialog-delete" web="reference-dialog-delete" >}}"Delete…" on the `orders` table ({{< key "C-d Delete" >}} in the sidebar). The terminal asks Cascade and the typed name in one form; the web asks for the name first and the Cascade question after it. Nothing is sent: after the name, the statement still goes through the preview and the safe-mode gate. The capture was cancelled.{{< /ui >}}

## Differences between the twins

- **Quick filter**: the terminal offers seven modes; the web applies `=` (or IS NULL) directly. To filter with another operator in the web, open the filter bar ({{< key "C-d f" >}}).
- **Set value**: the web adds gen_random_uuid() (PostgreSQL) and a Sequence list; in the terminal, type `nextval('schema.seq'::regclass)` under Advanced…, or pick a sequence default for the column in the structure editor.
- **Truncate and delete**: the terminal asks everything in one form; the web asks one question at a time.
- **Bootstrap failure**: the web shows the text in a confirmation box whose buttons both continue.
- **Settings**: the web cannot change settings that name a path on the server (they stay read-only there), and most changes take effect when the server restarts. Saving from either UI rewrites `config.toml` and drops its comments; both UIs warn when the file had some.

## See also

- [Context menus](../menus/) for the menus that open most of these dialogs.
- [Actions](../actions/) for every action name and key.
