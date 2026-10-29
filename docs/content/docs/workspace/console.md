---
title: Console
description: The log of every statement sent to the server, with timings, failures and PostgreSQL notices, and its find, copy, errors-only and clear.
weight: 60
---

The console is a log of the statements the workspace sent to the server, newest at the bottom: what you ran in
SQL editors, what commits sent, and in the terminal also the queries behind every table you browse. It is the
place to check exactly what went to the database and how long it took.

{{< ui tui="workspace-console" web="workspace-console" >}}The console after browsing two tables and running two statements, one of which failed.{{< /ui >}}

## How to open it

| | Terminal | Web |
|---|---|---|
| Show / hide | {{< key "C-d C-c" >}} (`toggle_console`) | {{< key "C-d C-c" >}}; **✕** on the pane closes it |
| Give it the keys | {{< key "C-d M-j" >}} (`focus_down`; from a SQL editor with results, the first press goes to the results), or click in it | click in it |
| Leave it | {{< key "Esc" >}}, or {{< key "C-d M-k" >}} / {{< key "C-d M-h" >}} / {{< key "C-d M-l" >}} | click elsewhere |

The terminal draws it across the bottom of the workspace, a quarter of the height (5 to 12 lines), and only when
the workspace is at least 14 lines tall. Its title counts the statements (`Console · 12 statements`) and adds
`· errors only` and `· /text` while those are on. The web pane is 200 px tall, under the tabs.

## What is logged

| Line | Terminal | Web |
|---|---|---|
| Statements run in a SQL editor | yes, with `✓` or `✗` and the time it took | yes; a failed one as `ERROR: <message>` followed by the statement |
| Statements sent by a commit, a structure change, a drop… | yes, one line each | yes |
| Browsing a table | `-- browse public.orders where 2 filter(s) order by total desc limit 300 offset 0`, and `-- browse public.orders: <error>` when it fails | no |
| Background jobs | `-- export orders-2026-10-29.csv` when it ends, with its duration, `✗` when it failed | an import as `import <file> into <table>`, a backup or restore as the command it ran |
| Bootstrap statements of the connection | yes | yes |
| Kills from the process list | `KILL <id>` | `kill process <id>` |
| PostgreSQL notices | each `NOTICE`, `WARNING`, `INFO`, `LOG` or `DEBUG` line as a comment, `-- NOTICE: …` | (shown in the result's Message view) |
| A pinned query tab that sat idle too long | — | `-- a pinned query tab idle for … was closed; anything it had open (BEGIN, temp tables) was rolled back` |

Every line starts with the time (`15:04:05`). The SQL is shown on one line (runs of spaces and line breaks
collapse to one space) with syntax highlighting. A statement larger than 64 KB is logged as `<N KB statement>`.
The console keeps the last 1000 lines.

The terminal's console belongs to the workspace and is empty when the workspace opens. The web's console is kept
by the server: it shows the lines of the current connection from every browser tab, keeps them across page
reloads, and refreshes every 2 seconds.

## Keys and menu

While the console has the keys (terminal), or the pointer was last in it (web):

| Key | Action | What it does |
|---|---|---|
| {{< key "/" >}} | `console_find` | Find: type the text and press {{< key "Enter" >}}; the cursor jumps to the next line containing it (ignoring case) and every match is highlighted. `"text" is not in the console` when nothing matches. In the web, the **Find** box filters the list to matching lines. |
| {{< key "n" >}} / {{< key "N" >}} | `console_next` / `console_prev` | Next / previous match, wrapping. Without a search: `no match: press / to find`. In the web they move the selection. |
| {{< key "y" >}} | `console_copy` | Copy the selected line (`copied the line`). |
| {{< key "e" >}} | `console_errors` | Errors only: show only failed statements (again: all). |
| {{< key "m" >}} | `console_menu` | The console menu. |
| ↑ ↓, {{< key "j" >}} {{< key "k" >}}, {{< key "PgUp" >}} {{< key "PgDn" >}}, {{< key "g" >}} {{< key "G" >}} | | Move the cursor (terminal); {{< key "G" >}} goes back to following the newest line. |
| {{< key "Esc" >}} | | Clear the search; again, leave the console. |

These are no-prefix keys of the console context, so `[keys]` can rebind them. In the web they do nothing while
the cursor is in the Find box (they type there).

The console menu (also on right click) has **Find…**, **Copy line**, **Errors only** and **Clear**. The web pane
also has an **Errors only** toggle, **Clear**, and a **Copy** button that appears on each line under the pointer.

{{< ui tui="workspace-console-menu" web="workspace-console-menu" >}}The console menu.{{< /ui >}}

{{< ui tui="workspace-console-errors" web="workspace-console-errors" >}}Errors only: the failed statement is all that is left.{{< /ui >}}

{{< ui tui="workspace-console-find" web="workspace-console-find" >}}Finding `orders`: the terminal highlights the matches and jumps between them; the web filters the list.{{< /ui >}}

## Clear

**Clear** in the menu, or **Clear console** in Open Anything (`clear_console`, no key), empties the console
(`console cleared`).

{{< callout type="info" >}}
In the web, the console lives on the server and **Clear** empties it for every connection and every browser tab,
not only the one you are looking at. Errors only in the web matches lines that say `error`, `failed` or
`exception` (failed statements start with `ERROR:`), so a statement that merely contains one of those words also
stays in the list.
{{< /callout >}}

## PostgreSQL notices

On PostgreSQL, `RAISE NOTICE`, `INFO`, `WARNING` and `LOG` messages from a statement you ran are shown in the
result's Message view in both UIs ([Results](../../sql-editor/results/)); the terminal also writes each of them
to the console as a comment line, so you can find them later.
