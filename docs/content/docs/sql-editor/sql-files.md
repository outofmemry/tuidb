---
title: SQL files
description: Open a .sql file into an editor tab, reopen recent files, read a file in the connectionless SQL viewer, drop files on the browser, and import a file as a saved query.
weight: 120
---

Besides [saved queries](../saved-queries/), you can work on any `.sql` file on disk: open it into an
editor tab, save it back, reopen it from the recent list, or just read it in a viewer before you have
connected. The terminal reads and writes paths directly; the browser can only read files you pick or
drop, so it never writes a file back.

{{< ui tui="results-files-open" >}}Open a SQL file: the terminal asks for a path.{{< /ui >}}

## How to open it

| Action | Terminal | Web |
| --- | --- | --- |
| {{< action "open_file" >}} | form **Open a SQL file** with **File** (a path, relative to the directory webtuidb started in) | the browser's file picker (`.sql` and plain text) |
| {{< action "open_recent" >}} | picker **Recent files** | dialog **Open a recent file** |
| Open a file from the Queries list | {{< key "f" >}} on the sidebar's Queries tab: form **Open a file in a tab** | **Open file…** in the Saved queries drawer |
| Import a file as a saved query | {{< key "i" >}} on the Queries tab | **Import…** in the Saved queries drawer |
| Drop a file | — | drag a `.sql` file onto the workspace |

Both keys work on the connection list too: there the file opens in the read-only SQL viewer instead of an
editor tab.

## Opening a file into an editor tab

Inside a workspace, {{< action "open_file" >}} opens the file in a new SQL editor tab.

| | Terminal | Web |
| --- | --- | --- |
| Tab name | the file's name (`orders-report.sql`) | `Query N`, like any new tab |
| Tied to the file | yes: a `●` after the name marks unsaved changes | no: the text is copied into the tab |
| Saving | {{< action "save_sql_as" >}} (form **Save SQL to file**, the tab's own path filled in); writing to the same path clears the `●` | {{< action "save_sql_as" >}} downloads the text (asks a file name, `query.sql`) |
| Closing with changes | asks **Save changes to orders-report.sql?**: Save, Don't save, Cancel (quitting asks too) | nothing to ask: the file was never linked |
| Added to the recent list | yes, as you typed the path | the file's name, in this browser only |

The terminal refuses a folder (`docs is a folder`) and files over 8 MB
(`big.sql is 12 MB: files over 8 MB are not opened`). {{< key "f" >}} on the Queries tab opens a file in a
tab without tying the tab to it and without adding it to the recent list.

## Recent files

{{< ui tui="results-files-recent" web="results-files-recent" >}}The recent files list.{{< /ui >}}

The list is shared through the state file: the terminal adds every file it opens with
{{< action "open_file" >}} or {{< action "open_recent" >}} to the front, keeping `[general] recent_files`
entries (10 by default; 0 keeps none).

| | Terminal | Web |
| --- | --- | --- |
| Shows | the entries stored as absolute paths: the file name, with the path beside it. Type to filter. Empty: `no files opened yet` | the terminal's paths, then the names of files picked in this browser (kept in the browser, the last 10). Empty: "No file was opened yet." |
| Picking one | opens it like {{< action "open_file" >}} (an editor tab, or the viewer on the connection list) | a terminal path is read by the server and moved to the front; a bare name asks for the file again with the picker |

The server only reads a file for the browser when it is in the recent list, has an absolute path, ends
in `.sql`, and is at most 8 MB. When it refuses (the file moved, was deleted, or is only a name), the
browser does not show an error: it opens the file picker so you can pick the file again. The browser
cannot add paths to the shared list: a page only learns a picked file's name, never its path.

{{< callout type="info" >}}
The terminal stores the path as you typed it. A file opened with a relative path (`orders-report.sql`)
is stored that way and is left out of the recent list in both UIs, which only show absolute paths. Type
the full path when you want the file to come back in Open recent.
{{< /callout >}}

## The SQL viewer

On the connection list (before you connect) {{< action "open_file" >}} and {{< action "open_recent" >}}
open the file in a read-only viewer, so you can read a script before deciding where to run it.

{{< ui tui="results-files-viewer" web="results-files-viewer" >}}A file in the SQL viewer, highlighted, with line numbers, read-only.{{< /ui >}}

| | Terminal | Web |
| --- | --- | --- |
| Where | an overlay titled **SQL viewer · name.sql (read-only)** | the page `/sql`, one tab per opened file, each with **✕** |
| Shows | the file with line numbers and SQL highlighting | the file in a read-only editor with line numbers, highlighting and folding |
| Keys and buttons | {{< key "Up" >}} {{< key "Down" >}} {{< key "PgUp" >}} {{< key "PgDn" >}} scroll, {{< key "c" >}} copies the whole file, {{< key "Esc" >}} closes | **Connections** (back to the list), **Open file…**, **Recent…**; {{< key "C-d O" >}} and {{< key "C-d M-O" >}} work here too |

Nothing in the viewer runs: connect, then open the file from the workspace to edit or run it. The
browser keeps the viewer's files until the page is reloaded.

## Drag and drop (web)

Drop a file on the area under the tab bar of a workspace:

- a `.sql` file opens in a new SQL editor tab;
- a `.csv`, `.tsv`, `.json` or `.sql` file dropped on a table's data tab opens the import wizard for that
  table instead (see [Import, export, backup](../../import-export/)); so does a file dropped on a table in
  the sidebar;
- anything else says "Drop a .sql file to open it" (or, on a table, "Drop a CSV, JSON or SQL file to
  import it into this table").

The terminal has no drag and drop; use {{< action "open_file" >}}.

## Importing a file as a saved query

{{< key "i" >}} on the sidebar's Queries tab (form **Import a .sql file as a saved query**) or
**Import…** in the browser's Saved queries drawer saves a copy of the file as a saved query; later
changes to the file are not followed. Details are in
[Saved queries](../saved-queries/#importing-a-sql-file-as-a-query).

## Settings

| Setting | Effect |
| --- | --- |
| `[general] recent_files` | how many opened files to remember (default 10, 0 = none) |

See the [configuration reference](../../configuration/).
