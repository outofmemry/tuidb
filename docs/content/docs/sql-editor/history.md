---
title: History
description: Every statement that ran on a connection, kept per connection in a JSONL file, searchable from the sidebar and the browser drawer, with Save As and Import.
weight: 100
---

webtuidb writes every statement that ran successfully to a history file for that connection: what you
ran in the editor, and the SQL generated when you commit edits in a grid. Both UIs read the same file, so
a query you ran in the terminal is in the browser's history too. Open an entry to run it again, insert it
into the editor, save it as a query, or export the whole history.

{{< ui tui="results-history" web="results-history" >}}The history of "Shop (Postgres)", newest first: the sidebar's History tab and the browser's History drawer.{{< /ui >}}

## How to open it

| Terminal | Web |
| --- | --- |
| The sidebar's **3 History** tab: focus the sidebar ({{< key "C-d M-h" >}}, or {{< key "C-d 0" >}} if it is hidden), then {{< key "3" >}}, or {{< key "Tab" >}} to cycle 1 Objects → 2 Queries → 3 History | the **History** button in the top bar (on a narrow window, in the **☰** menu). Click it again or **✕** to close the drawer |

## What the list shows

| | Terminal | Web |
| --- | --- | --- |
| Order | newest first (the header says `Shop (Postgres)  newest first`) | newest first |
| Entries loaded | the newest 500 | the newest 200 (of those matching the search) |
| Each entry | the time (`14:05` for today, else `Oct 28`) and the statement on one line | the statement as written, the date and time, and **Run**, **⋯**, **Delete** buttons |
| Empty | `Nothing run yet` and the key for a new query | an empty list |

Statements that failed are not in the history (they are in the console log). A statement with
[query parameters](../parameters/) is logged differently by the two UIs: the terminal writes it as you
typed it (`… ILIKE :pattern`), the browser writes it with the values filled in.

### Search

| Terminal | Web |
| --- | --- |
| {{< key "/" >}} in the sidebar, then type: the list keeps entries whose SQL contains the text, any case. {{< key "Enter" >}} keeps the filter and goes back to the list, {{< key "Esc" >}} clears it. | the **Search history** box at the top of the drawer, same rule (case-insensitive "contains"), searched on the server over the whole file |

## Using an entry

| Do | Terminal | Web |
| --- | --- | --- |
| Open in a new tab | {{< key "Enter" >}}, {{< key "l" >}} or {{< key "o" >}} on the entry | click the entry, or {{< key "Enter" >}} when it has the focus |
| Run it | open it, then {{< key "C-d Enter" >}} | **Run**: a new tab that runs at once (safe mode still applies) |
| Menu | {{< key "m" >}} | **⋯**, a right-click, the {{< key "ContextMenu" >}} key or {{< key "S-F10" >}} |
| Delete one entry | not available | **Delete** |

{{< ui tui="results-history-menu" web="results-history-menu" >}}The history entry menu.{{< /ui >}}

The menu has the same entries in both UIs:

| Entry | Does |
| --- | --- |
| Insert into editor | types the statement at the cursor of the current SQL editor; with no editor open it opens one holding it |
| Run in a new tab | the browser opens a tab and runs the statement; the terminal opens the tab with the statement and leaves running it to you |
| Add to saved queries… | the save dialog: in the terminal **Name**, **Keywords** and **Folder**; in the browser a name ("Save query as"). See [Saved queries](../saved-queries/) |
| Copy | the statement to the clipboard |
| Clear all… | deletes the whole history of this connection after a confirmation (terminal: "Clear all history of Shop (Postgres)? This cannot be undone."; browser: "Clear history for this connection?") |

The browser also has a **Clear** button in the drawer's toolbar, with the same confirmation.

{{< callout type="warning" >}}
Clear all deletes the connection's history file. There is no undo; use Save As first if you may want it
back.
{{< /callout >}}

## Save As and Import

| | Terminal | Web |
| --- | --- | --- |
| Save As | {{< key "S" >}} on the History tab: form **Save history as** with **Format** (`csv` or `json`) and **File** (`history.csv`); asks before overwriting | **Save As…** in the drawer: downloads `Shop (Postgres)-history.csv` |
| Import | {{< key "i" >}} on the History tab: form **Import history** with **File (csv or json)** | **Import…** in the drawer: pick a `.csv` or `.json` file |

{{< ui tui="results-history-save" >}}Save history as, in the terminal.{{< /ui >}}

Save As writes the whole history of the connection, oldest first:

- **CSV** has the header `time,database,sql,duration_ms,source`; the time is RFC 3339 with nanoseconds.
- **JSON** is an array of the stored entries, with every field (including `orig`, below).

Import reads either format; which one is told by the first byte (`[` is JSON). A CSV must start with the
`time` header, otherwise it is refused with `not a history export: expected a JSON array or a CSV with the
header [time database sql duration_ms source]`, and a short line names its line number.

The two UIs import differently:

- **Terminal**: a background job (`import history`, see {{< action "jobs" >}}) that reads the file and
  writes it in batches of 500, reporting `importing history… 1000 of 2400 read, 950 new`. Entries already in
  the history are skipped, matched by their id or by their content, so importing the same file twice adds
  nothing. The batch is stored in time order, so the newest entries survive the size limit. A cancel or a
  failure says how far it got (`stopped after 500 of 2400 entries`). It ends with
  `imported 950 new entries (2400 in the file)`.
- **Web**: the file is sent to the server and appended entry by entry; entries with no SQL are skipped and
  an entry without a source becomes `editor`. A second import of the same file adds the entries again.

## The history file

Each connection has one file, `~/.local/share/webtuidb/history/<connection>.jsonl` (under
`$XDG_DATA_HOME` when that is set). A name with characters other than letters, digits, `.`, `_` and `-`
is percent-encoded, so "Shop (Postgres)" is `Shop%20%28Postgres%29.jsonl`. Every line is one JSON entry:

```json
{"id":"3f1c9a…","time":"2026-10-29T14:05:12.31+01:00","db":"shop","sql":"SELECT status, count(*) …","dur":2512000,"src":"editor"}
```

| Field | Meaning |
| --- | --- |
| `id` | 24 hex characters, a hash of the connection, database, SQL, `orig`, time and duration. The same entry always gets the same id, which is how an import recognises entries it already has |
| `time` | when it ran |
| `db` | the database it ran in |
| `sql` | the statement; one over 64 KiB is cut and ends with `-- (truncated)` |
| `dur` | how long it took, in nanoseconds |
| `src` | `editor` for the SQL editor (and the terminal's Explain), `gui` for statements the app generated: a commit in a grid, a structure change, a drop or truncate from the sidebar. The browser also writes `bootstrap` (the connection's bootstrap statements, run when a session opens; written even when one fails), `backup` (the backup or restore command it ran) and `import` (one line per import, `import <source> into <table>`) |
| `orig` | with `log_original_values` on, the values of the changed rows before the commit (below) |

The history is bounded: it keeps the newest 5000 entries, and a file that grows past 16 MiB is trimmed to
about half of that. Writes are locked, so the terminal and the browser can append at the same time.

### Commits and original values

When you commit edits from a grid, every generated statement is written with `src` `gui`. With
`[general] log_original_values = true`, the first statement of the commit also carries `orig`: a CSV with
a line `update orders,id` (or `delete …`) naming the table and key columns, then the key values of each
changed row. Only the key values are kept, not the whole old row (on a table without a primary key, every
original value is the key). Turn it on when you need to know which rows a commit touched; leave it off if
the history must not hold row data.

### MCP

Statements that an MCP client runs through webtuidb's MCP server are not written to the history; they
appear in the MCP server's own activity log (the last 200 calls). An MCP client with a full-scope token
can read the history with the `list_history` tool; a read-only token cannot, because the history holds
literals and row values. See [AI and automation](../../automation/).

## Details and limits

- The terminal's list is read when you open the History tab; after you run something, switch to another
  sidebar tab and back (or press {{< key "3" >}} again) to see the new entry. The browser drawer reloads
  when you search or reopen it.
- Entries cannot be edited. The terminal cannot delete a single entry; the browser can.
- The history follows the connection name: renaming a connection starts a new file.

## Settings

| Setting | Effect |
| --- | --- |
| `[general] log_original_values` | write the key values of changed rows (`orig`) with each commit (default off) |

See the [configuration reference](../../configuration/).
