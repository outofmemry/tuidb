---
title: Known limits
description: Every documented limit and shortcut in webtuidb, grouped by area, with what you notice and how to work around it.
weight: 60
---

webtuidb marks each place where it takes a simpler route than the full solution with a `shortcut:` comment in the code. This page collects all of them that you can notice, plus the limits that come from the engines themselves, grouped by area. Each entry says what you see and, where there is one, the workaround. Engine-specific gaps (what a driver cannot do at all) are in [Engines](../../engines/).

## Keys and terminal input

| Limit | What you notice | Workaround |
|---|---|---|
| `C-[`, `C-i` and `C-d` cannot be bound | A terminal sends `C-[` as Esc and `C-i` as Tab, and `C-d` is the prefix. The table views therefore use {{< key "C-d (" >}}, {{< key "C-d )" >}}, {{< key "C-d M-i" >}} and {{< key "C-d M-d" >}} instead of TablePlus's Cmd+[, Cmd+], Cmd+i, Cmd+d. | Rebind them under `[keys]` if you prefer other keys. |
| Ctrl+h/j/k/l move between panes | These four work without the prefix (`focus_left` … `focus_right`), so they take the editor's Ctrl+h and Ctrl+k. | Rebind the focus actions, for example `focus_left = "M-h"`: overriding an action drops all its default keys. See [Troubleshooting](../troubleshooting/#ctrlhjkl-do-nothing-inside-tmux). |
| Mouse support is partial in the terminal | A left click selects only in the sidebar list and a table's data grid; tabs, query results and split panes ignore it. A right click opens the tab, grid cell, result, filter bar and console menus. Dialogs ignore the mouse. | Use the keys; every mouse action has one. |
| One cursor in the terminal editor | `select_next_occurrence` ({{< key "C-d D" >}}) moves the selection to the next match instead of adding a second caret, as the web does. | Use find and replace ({{< key "C-d M-f" >}}) for several occurrences. |
| Vim keys: `:wq` on a new buffer | `:wq` on a never-saved buffer opens Save As and leaves the tab open. | Run `:q` after saving. |
| Vim keys and soft wrap | With soft wrap on, a click on a wrapped row lands on the first part of that line. | Turn wrap off ({{< key "C-d M-g" >}}) for precise clicks. |
| Shell completion | Bash inserts a connection name that contains a space without quotes. | Quote it by hand: `webtuidb "Shop (Postgres)"`. |

## SQL editor

| Limit | What you notice | Workaround |
|---|---|---|
| Statements run on a pooled connection | Without a pinned connection, each statement can run on a different server connection, so `BEGIN`, `SET` and temporary tables do not carry over to the next run. | "Pin connection to this tab" in {{< key "C-d p" >}} (`pin_connection`). |
| A pinned connection stays on its server | After a reconnect or a database switch, a pinned tab keeps using the old server connection and database. | Unpin the tab and pin again. |
| Wide (CJK) text in the editor | The terminal editor scrolls by character count, and CJK characters take two cells, so on long lines of wide text the cursor can scroll out of view. One-line fields (filters, forms) measure by cell and are not affected. | Turn on line wrap ({{< key "C-d M-g" >}}), or edit long CJK lines in the web. |
| A line taller than the pane | With wrap on, the terminal editor scrolls by whole lines: a single line taller than the pane keeps its end off screen. | Turn wrap off ({{< key "C-d M-g" >}}) to reach it. |
| Replace with several lines | In the terminal, a replacement that contains line breaks is inserted on one line (breaks become spaces). | Use the web, or paste the multi-line text. |
| Folds follow the statement text | Two identical statements in one script fold together. | None needed for most scripts. |
| Web folds from a gutter click | A fold made by clicking the gutter is saved with the next edit or fold command, not at once; closing the tab right after the click loses it. | Fold with {{< key "C-d M-z" >}}, which saves at once. |
| Overwrite check for saved queries | The terminal asks "Overwrite saved query *name*?" when a query of that name exists in the folder, but checks by listing the folder first, so a query another process saves in between is overwritten without asking. | |
| Split down | In the web, the second editor's text is not saved with the tab, and a run sends the focused editor's text. In the terminal, closing a tab compares only the focused editor with its file. | Save the second editor's text to a saved query or file. |
| Rectangular selection (web) | Columns count characters, so tabs and wide characters shift the rectangle. | Avoid block selection over tabs. |
| Autocomplete aliases | In `FROM a x, b y` only the first table's alias resolves. | Use `JOIN` syntax. |
| Parameters inside `$tag$` strings | A `:name` inside a PostgreSQL dollar-quoted string can be mistaken for a parameter, or a real one missed. | Turn `[editor] query_params` off for such scripts. `editor.params_in_strings` is accepted but not used by either UI yet. |
| MySQL `DELIMITER` | Routine bodies that contain `;` are split at each `;` by the editor (no `DELIMITER` support when editing a definition). An import keeps `DELIMITER` within one chunk of the file only. | Run such routines with the mysql client, or keep the body free of `;`. |
| The formatter knows tokens, not grammar | Beautify handles clauses, CASE blocks, ON CONFLICT and FOR UPDATE; more complex constructs come out flat. | Format those by hand. |
| MongoDB shell scripts | `toArray()` / `forEach()` on a cursor stop after 50,000 documents. In shell one-liners, a `/` after `)` or `}` is read as division, not a regular expression. | Add a `limit()`, or wrap the regex in `RegExp(...)`. |
| Opening a file from the connection list | The terminal shows it in a read-only viewer, because the welcome screen has no tabs. | Connect, then open the file ({{< key "C-d O" >}}) to edit or run it. |
| AI assistant | The reply appears all at once (not streamed). ACP custom agents and Copilot / Codex sign-in are not available. | |

## Results and paging

| Limit | What you notice | Workaround |
|---|---|---|
| Load more re-runs the statement | Load more ({{< key "C-d M-m" >}}) runs the statement again from row 1 with a larger limit, so a slow query is slow again each time. Only read-only statements are re-run ("load more only re-runs read-only statements"). | Add a `LIMIT` / `WHERE` that returns what you need. |
| Paging uses a subquery | Each page re-runs the statement wrapped in a subquery (no server cursor). Statements whose ordering a subquery would lose are not paged. | |
| Web: a grown result is read-only | After Load more, a web result can no longer be edited. | Re-run the statement with a larger limit ({{< key "C-d L" >}}). |
| Web result editing | Delete, duplicate and set value act on the selected row only, and undo has no redo. | Edit several rows in the table's data tab, or in the terminal. |
| Editable results are recognised strictly | A result is editable only for a plain `SELECT` on one table with its primary key; any `(`, `JOIN` or `WITH` anywhere in the statement, even inside a string, makes it read-only. An alias that renames a column to another column's name is not detected. | Open the table instead ({{< key "C-d (" >}}). |
| Web results stream in batches | The server fetches a statement's rows, then sends them in batches. | |
| Geometry in the web | The web decodes Point, LineString, Polygon, the Multi* types and GeometryCollection (WKB and EWKB, Z and M) to WKT; curve and surface types (CircularString, TIN, …) stay as hex in the grid and in quick look. | Select `ST_AsText(column)` in a query. |
| Terminal charts | A line chart is drawn as bars in the terminal; the web draws a line. Pie charts on a metrics board are shown as bars. | |
| Header menu in results | Filter, hide and show columns belong to table tabs (greyed out in the web, a note in the terminal); the terminal sorts results by one column. | Add a `WHERE` or select fewer columns in the query. |

## Editing data

| Limit | What you notice | Workaround |
|---|---|---|
| The commit log keeps keys only | The pre-image logged for a change holds the row's key values (every original value on a table without a primary key), not the whole row. | |
| Set value with a file (web, not PostgreSQL) | A file that is not valid UTF-8 is sent as `\x<hex>` text, which other engines store as text. | Load binary data in the terminal or with the engine's client. |
| Advanced expressions are token-checked | An expression in Set value is checked token by token, not parsed. | |
| Web cell editor | Tab always moves to the next cell, so a literal tab cannot be typed. | Paste the tab character. |
| MySQL batches on a pinned connection | A pinned MySQL connection runs a batch without its own transaction (a `BEGIN` would commit yours), so a failure midway leaves the earlier statements applied or in your open transaction. A pinned MySQL query is drained, not cancelled, when stopped. | |
| MySQL DDL commits implicitly | A failed multi-statement structure change can be partly applied; each statement's result is reported. | Review the preview before committing. |
| Cassandra | Two inserts of the same key in one save tie on the timestamp; zero values (0, the zero UUID) inside a UDT map cannot be told apart from NULL. | |
| Redis streams | After an `XDEL` in the middle of a stream on Redis 6, row marks can shift. Filtering or sorting loads at most 100,000 elements of one key, and hash / set pages cannot start deeper than that. | Reload from the first page. |

## Structure and definitions

| Limit | What you notice | Workaround |
|---|---|---|
| Data types list | The type picker offers a fixed list per engine, not the types defined in your database. | Type the type name. |
| CREATE templates (web) | "New view / materialized view / function" opens one template per engine family; you finish the body. | |
| Vertica | Schemas, views, object listing and DDL are limited: Vertica is read through PostgreSQL catalog views it lacks. `SET` in one statement leaks to the next (no `DISCARD`). | |
| PostgreSQL users | Sequence and schema privileges are not listed (tables, views and foreign tables only); a schema name with a dot confuses column grants. | Grant those with SQL. |
| MySQL users | Privileges come from `SHOW GRANTS`, one query per account; roles are skipped. | |
| Sidebar groups | A group with no items is not kept, so a group is created from marked items. | Mark items first (`space`). |
| ER diagram | In the terminal, "copy column" is a filtered list of `table.column` rather than a cursor in the drawing; a box wider than the screen scrolls sideways. | |

## Import, export and backup

| Limit | What you notice | Workaround |
|---|---|---|
| CSV quote character | Only an ASCII quote character is supported; `[csv] line_break` is ignored when importing. | |
| SQL import preview | The preview splits statements as PostgreSQL; the real dialect applies when the import runs. | |
| Restore filters | Data only / Schema only do not filter a restore (they are ignored). | Use the tool's own flags in Extra options. |
| ClickHouse backups | Backup and restore run server-side to a disk named `backups` in the server's configuration; File is a name on that disk, not a local path. | Configure the `backups` disk on the server. |
| Web backups | Output past 5,000 lines is dropped from the job view; a file pattern without an extension gets `.sql` (or `.sql.gz`) whatever the format; a failed restore into a new database leaves that database in place and says so. | Drop the database yourself if you do not want it. |
| Cancelling on Windows | Only the direct child process is stopped, not the processes it started. | |
| Data diff | Each side is read up to the connection's row limit; the terminal refuses a bigger table, the web reports that the comparison was cut short. | Raise `[general] row_limit` or compare filtered subsets. |
| Board export | A board panel export is capped at the row limit. | |

## Search and tools

| Limit | What you notice | Workaround |
|---|---|---|
| System schemas are recognised by name | Search skips `pg_catalog` and `information_schema`; the web sidebar treats `information_schema`, `pg_*`, `mysql`, `performance_schema`, `sys` and `system` as system schemas. | |
| Assistant chat search | Searching chats reads every chat file of the connection. | |
| Saved-query rename | Renaming is a copy and a delete; a crash between the two leaves both files. Saved queries track no creation time, so "Sort by created" uses the modified time. | Delete the leftover file. |

## Connections and engines

| Limit | What you notice | Workaround |
|---|---|---|
| `ssl_mode = prefer` without TLS | ClickHouse, Redis, MongoDB and DynamoDB have no opportunistic TLS: `prefer` connects in plain text there. On Cassandra, `prefer` means TLS without certificate checks. | Use `require` or `verify-full` when the server must be encrypted. |
| PostgreSQL bootstrap on a reused connection | After a statement changes session state (`SET`, `SET ROLE`, temporary tables), the pooled connection is reset and the `bootstrap` statements are replayed before it is used again. If a replayed statement fails, that connection is closed without a message and the next statement gets a fresh one. | Check a bootstrap that fails only some of the time in the console of a new session. |
| Bootstrap statements | ClickHouse turns `SET k = v` bootstrap statements into client settings and runs any other statement once, on one connection. Cassandra runs bootstrap once on connect (`USE ks`, `CONSISTENCY level`). The web runs bootstrap statements one after another on the pool. | Keep bootstrap to `SET` / `USE`. |
| A URL's `ssh_key` | A pasted URL's `ssh_key` is kept as given. | Check the path after importing a URL. |
| The MCP server sees startup connections | Connections added after `webtuidb --frontend` started are not visible to its MCP server. | Restart the server. |

## Web UI

| Limit | What you notice | Workaround |
|---|---|---|
| Browser storage | The access token (`sessionStorage`), the last workspaces, sidebar item colours, ER diagram positions and colours and the metrics board layout live in this browser (`localStorage`). Another browser or port starts without them; private mode keeps the token only until reload. | Reopen the printed `#token=` URL. |
| The lock screen | The passcode lock keeps a bystander out of the page, not out of the API: the token stays in the tab while locked. | Close the tab, or stop the server. |
| Login lockout | Five wrong passwords within ten minutes lock logins for 60 seconds, for everyone, including you. | Wait a minute. |
| Settings | Settings that name a path on the server cannot be changed from the browser, and most changes apply when the server restarts. | Edit `config.toml` and restart. |
| Theme colours | The web uses a small copy of each palette's colours, not the terminal's full tables. | |
| Column widths | The web assumes an 8-pixel terminal cell when it saves column widths, so widths round-trip with the terminal approximately. | |
| SSH log | "Enable SSH log" cannot switch the log on from a page. | Start the server with `--ssh-log FILE`. |
| "Add to metrics board…" | Not in the web result menu: the web board form takes no query yet. | Use the terminal. |
| Console "Errors only" | Matches lines the server marked as failed, or whose text says error, failed or exception. | |
| Job progress | The jobs drawer samples a running job every 150 ms, so very short steps can be skipped in its output. | The final result is always shown. |

## Clipboard

| Limit | What you notice | Workaround |
|---|---|---|
| OSC 52 length | Some terminals drop OSC 52 copies near 100 KB. | Export instead of copying large results. |
| tmux | tmux with `set-clipboard off` swallows OSC 52 and webtuidb cannot tell. | `set -g set-clipboard on` in `~/.tmux.conf`. |
| Windows | `clip.exe` reads the ANSI code page, so non-ASCII text may arrive garbled. | |
| Interleaving | An OSC 52 sequence can interleave with a screen redraw. | Copy again. |

## Safety and AI

| Limit | What you notice | Workaround |
|---|---|---|
| Read-only detection is not a parser | Safe Mode 2, Alert Mode 2 and read-only checks classify statements by keywords. A disguised write could be taken for a read. | Use `read_only = true` on the connection: it is enforced by the database session. |
| MCP read-only queries | `execute_read_query` runs inside a database-enforced read-only transaction where the engine supports one; elsewhere only the keyword check guards it, and it is refused outside PostgreSQL, MySQL and MariaDB unless such a transaction exists. Redaction does not recognise backslash escapes or `$tag$` strings. | Give MCP tokens the `read-only` scope. |
| MCP approvals | If the request ends before you answer an approval sheet in the terminal, the sheet stays until you dismiss it and the answer is dropped. MCP clients cannot be disconnected one by one. | Remove the token from `[[mcp.token]]` and restart. |

## Crashes

A panic inside a background command of the terminal UI ends the process without a crash log; panics in the main loop and in web requests are logged to `crash.log`. A panic before the first workspace opens is logged without console lines, because the console belongs to a workspace. See [Troubleshooting](../troubleshooting/#crashes-and-bug-reports).
