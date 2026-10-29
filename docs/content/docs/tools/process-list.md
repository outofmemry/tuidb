---
title: Process list
description: See the sessions and queries running on the server, sort them, look at a query, copy or export the list, and kill one or several, on PostgreSQL, MySQL, MariaDB, ClickHouse, MongoDB, Redis and more, in the terminal and in the browser.
weight: 20
---

The process list shows what the database server is doing right now: every session or running query with its id,
user, database, state, how long it has been running and its query text. You can kill a runaway query from it.
It opens on a connection of its own, so it works while your workspace is busy with a long query.

{{< ui tui="tools-processes" web="tools-processes" >}}The process list on the MySQL demo database. The web shot shows two rows ticked and the right-click menu.{{< /ui >}}

## How to open it

| What | Terminal | Web |
| --- | --- | --- |
| Open the process list | {{< key "C-d ." >}} (`process_list`) when nothing is running | {{< key "C-d ." >}} when nothing is running |
| From Open Anything | {{< key "C-d p" >}} → **Process list** | {{< key "C-d p" >}} → **process list** |

{{< key "C-d ." >}} has two jobs: while a query or a background job of this workspace runs, it cancels it
(`cancel_query`); otherwise it opens the process list. To see the process list *while* something runs, use Open
Anything.

## Which engines have it

| Engine | Source | Columns filled from | Kill sends |
| --- | --- | --- | --- |
| PostgreSQL, Greenplum | `pg_stat_activity`, sessions whose state is not `idle`, your own session left out, oldest query first | `pid`, `usename`, `datname`, `state`, time since `query_start`, `query` | `pg_cancel_backend(pid)`: cancels the running query, the session stays connected |
| MySQL, MariaDB, SingleStore | `SHOW FULL PROCESSLIST` (every connection, idle `Sleep` ones too) | `Id`, `User`, `db`, `State` (or `Command` when the state is empty), `Time`, `Info` | `KILL QUERY id`: stops the statement, not the connection |
| ClickHouse | `system.processes` without the listing query itself | `query_id`, `user`, `current_database`, `Running` / `Cancelled`, `elapsed`, `query` | `KILL QUERY WHERE query_id = … SYNC` |
| MongoDB | `currentOp` on `admin` | `opid`, the first effective user, the database part of `ns`, `op` (or `desc`), `secs_running`, the `command` as JSON | `killOp` with that `opid` (string ids such as `shard:123` on mongos are kept) |
| Redis | `CLIENT LIST` | `id`, `user`, `db`, `addr` plus the client `name`, `age`, the last `cmd` | `CLIENT KILL ID id` (disconnects the client) |

Redshift, Vertica, Cassandra and DynamoDB have no process list: the terminal says
`the process list is not supported by redshift`, the web dialog says
`The process list is not supported by redshift.`

{{< callout type="info" >}}
On PostgreSQL and MySQL a connection marked **read only** refuses to kill (`connection is read_only`), even though
listing works.
{{< /callout >}}

## What you can do

### Terminal

The list is a picker; each line is `id user db state elapsed` with the query text dimmed after it. The title says
how it is sorted, and how many rows are marked.

| Key | Does |
| --- | --- |
| {{< key "↑" >}} {{< key "↓" >}} | move |
| {{< key "Enter" >}} | quick look at the full query of the highlighted process |
| {{< key "Space" >}} | mark / unmark the process (✓) and move down |
| {{< key "k" >}} or {{< key "Delete" >}} | kill the marked processes, or the highlighted one when none is marked |
| {{< key "s" >}} | sort: pick `elapsed`, `id`, `user`, `db` or `state`; picking the current column again reverses it |
| {{< key "c" >}} | copy as CSV with header, JSON or Markdown (the marked rows, else every row) |
| {{< key "e" >}} | export to a CSV file (asks for the path, `processes.csv` by default) |
| {{< key "m" >}} | the process menu |
| {{< key "r" >}} | refresh |
| {{< key "Esc" >}} or {{< key "q" >}} | dismiss |

The process menu ({{< key "m" >}}) has the entries the web's right-click menu has, in the same order:
**Kill selected**, **Quick look**, **Copy as…**, **Export…**, **Dismiss**.

{{< ui tui="tools-processes-menu" >}}The process menu in the terminal.{{< /ui >}}

### Web

The dialog is a table with a checkbox per row and one in the header to tick every row.

| Control | Does |
| --- | --- |
| click **Id**, **User**, **Database**, **State** or **Elapsed** | sort by it; click again to reverse (▾ / ▴ shows the direction) |
| the checkbox of a row, or of the header | select rows for **Kill selected**, **Copy as…** and **Export…** |
| click the query text | quick look at the whole query (read-only) |
| **Kill** at the end of a row | kill that one process |
| right-click a row | **Kill selected** (the clicked row when it is not ticked), **Quick look**, **Copy as…**, **Export…**, **Dismiss** |
| **Refresh** | load the list again |
| **Kill selected (n)** | kill the ticked rows (disabled with none ticked) |
| **Copy as…** | Plain, JSON, HTML, Markdown Table, CSV, CSV with Header or INSERT Statement, for the ticked rows, or every row |
| **Export…** | download the ticked rows, or every row, as `processes.csv` |
| **Dismiss** | close |

The web shows **Elapsed** in seconds with one decimal (`262.0s`); the terminal writes a duration (`4m22s`,
`16h0m7s`).

### Sorting

Both UIs sort by **elapsed**, slowest first, when the list opens. Text columns sort alphabetically. The web sorts
`id` numerically; the terminal compares ids as text (so `10` comes before `9`).

## Killing

Killing asks first: `Kill process 12883?` or `Kill 2 processes?` with each victim listed as
`id  user@db  query` in the terminal. In the terminal {{< key "y" >}} kills and {{< key "Enter" >}},
{{< key "Esc" >}} or {{< key "n" >}} cancel; in the web, **Kill** confirms.

{{< ui tui="tools-processes-kill" >}}Two marked sessions and the kill confirmation.{{< /ui >}}

The kill then goes through the connection's [safe mode](../../safety/) like a write: each process is a
`KILL <id>` statement for the gate, so a safe mode that confirms writes asks again, and one that wants the
password asks for it (the web shows the password field under the table and a **Kill …** button). Each kill is
logged in the console; the web records `kill process <id>` in the query history.

Several processes are killed one after the other. In the terminal a failure does not stop the rest: the errors
are listed together and the list refreshes. In the web the loop stops at the first error and shows it; a gate
that asks for confirmation stops the loop, and the rest follows once you confirm. After a kill the killed ids
are unmarked and the list reloads.

## Copy, export and quick look

- Copy and export act on the marked (terminal) or ticked (web) rows, and on every row when none is.
- Columns: `id`, `user`, `db`, `state`, `elapsed` and `query`. The terminal writes elapsed as a duration
  (`2m35s`) and puts each query on one line in copies; the web writes `elapsed_ms` in milliseconds.
- The terminal export writes the CSV atomically to the path you give; the web downloads it.
- Quick look shows the query text in the read-only value viewer.

## Details and limits

- The list opens a second connection with the workspace's own settings and password (and its current
  database), the first time you open it; it stays open until the workspace closes. If the web server cannot open
  a second connection, it falls back to the workspace's own.
- The list does not refresh on its own: press {{< key "r" >}} or **Refresh**.
- Timeouts: the terminal gives each load or kill 30 seconds.
- PostgreSQL lists only active (non-idle) sessions; MySQL lists idle connections too.
