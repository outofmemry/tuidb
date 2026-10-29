---
title: Backup and restore
description: Dump a whole database with pg_dump, mysqldump, mongodump, redis-cli or ClickHouse BACKUP, and restore it into the same or a new database, with live tool output and cancel, in the terminal and in the browser.
weight: 30
---

Backup and restore run the database's own command-line tools (`pg_dump`, `mysqldump`, `mongodump`…) with the connection's host, user and password, and show their output while they run. webtuidb does not ship these tools: they must be installed on the machine that runs `webtuidb` (for the browser, the machine running the server), on `PATH` or at a path you set per connection.

{{< ui tui="io-backup-run" web="io-backup-run" >}}A Redis backup with `redis-cli --rdb`. The terminal shows the command line, the tool's output and "✓ done", with **r** to reveal the file; the browser shows the output as it arrives, with **Cancel** while it runs.{{< /ui >}}

## How to open it

| | Terminal | Web |
|---|---|---|
| Backup | {{< key "C-d b" >}} (`backup`) | the same {{< key "C-d b" >}} |
| Restore | {{< key "C-d B" >}} (`restore`) | the same {{< key "C-d B" >}} |
| Jobs centre | {{< key "C-d M-J" >}} (`jobs`) | the same, or **Jobs** in the status bar |

Both work on the connection's current database. Switch database first to dump another one.

## Tools per engine

| Engine | Backup runs | Restore runs | Password goes through |
|---|---|---|---|
| PostgreSQL | `pg_dump -F p` (plain SQL) or `-F c` (custom archive) | `psql -v ON_ERROR_STOP=1` for a plain `.sql` / `.sql.gz` file; `pg_restore -d …` for a custom archive | `PGPASSWORD` (and `PGDATABASE`, `PGSSLMODE`) |
| MySQL | `mysqldump --single-transaction --routines --events -- <db>` | `mysql -- <db>`, the file fed on standard input | `MYSQL_PWD` |
| MariaDB | `mariadb-dump`, else `mysqldump` (same flags) | `mariadb`, else `mysql` | `MYSQL_PWD` |
| MongoDB | `mongodump --uri=… --archive=<file>` | `mongorestore --uri=… --archive=<file>` | a temporary YAML file (`--config`, mode 0600) deleted after the run |
| Redis | `redis-cli --rdb <file>` (`--user` for an ACL user, `--tls` when `ssl_mode` is set) | not supported | `REDISCLI_AUTH` |
| ClickHouse | `clickhouse-client --query "BACKUP DATABASE … TO Disk('backups', '<file>')"` | `… RESTORE DATABASE … FROM Disk('backups', '<file>')` | `CLICKHOUSE_PASSWORD` |

Other engines answer "backup is not supported for *driver*". A Redis restore answers "redis restore: stop the server and copy the RDB file into its data directory". Passwords never appear on the command line; the command shown in the dialogs has the MongoDB URI password replaced by `***`.

{{< callout type="info" >}}
**PostgreSQL restore picks the tool by the file.** A name ending in `.dump`, `.tar`, `.backup`, `.pgdump` or `.custom` (with or without `.gz`), a folder, or **Format** `custom` goes to `pg_restore`; anything else (`.sql`, `.sql.gz`) goes to `psql`. `psql` stops at the first error (`ON_ERROR_STOP=1`).

**ClickHouse** backs up on the server, not on your machine: the server needs a disk named `backups` in its configuration, and **File** is the name on that disk.

**SSL**: PostgreSQL gets `PGSSLMODE` from `ssl_mode` (`verify-identity` becomes `verify-full`); MySQL gets `--ssl-mode=DISABLED|PREFERRED|REQUIRED|VERIFY_CA|VERIFY_IDENTITY`, and any other `ssl_mode` stops with "unsupported ssl_mode".
{{< /callout >}}

## Backup

### Terminal

{{< ui tui="io-backup-form" >}}The **Backup 0** form on the Redis connection (database `0`), with the default dated file name.{{< /ui >}}

The form is titled **Backup *database***. {{< key "Enter" >}} continues, {{< key "Esc" >}} cancels, {{< key "C-t" >}} opens [tool paths](#tool-paths), {{< key "C-o" >}} opens the common options.

| Field | What it does |
|---|---|
| **File** | Where the dump goes, relative to the folder you started `webtuidb` in. Default: the `[export] filename` pattern with `{table}` set to the database name, plus `.sql`, for example `shop-2026-10-29.sql`. Any name works; the extension does not choose the format. |
| **File-name pattern** | Optional. `{connection}`, `{database}` (also `{table}`), `{tag}`, `{date}`, `{time}`. When set, it replaces the file name of **File** and keeps its folder: **File** `dumps/x.sql` with pattern `{database}-{date}-{time}.dump` writes `dumps/shop-2026-10-29-153000.dump`. |
| **Extra options** | More tool flags, separated by spaces, from an [allow-list](#extra-options). |
| **Gzip** | Compress, see [Gzip](#gzip). |
| **Format** (PostgreSQL) | `plain` (SQL text) or `custom` (a `pg_dump` archive for `pg_restore`). |
| **Data only** / **Schema only** | `pg_dump -a` / `-s`; `mysqldump --no-create-info` / `--no-data`. Ticking one hides the other. MongoDB, Redis and ClickHouse ignore them. |

{{< key "Enter" >}} builds the command and asks **Backup *database*?** with the full command line (and `> file` at the end). **Backup** ({{< key "Enter" >}} / {{< key "y" >}}) runs it; when the file already exists you are asked **Overwrite *path*?** first.

{{< ui tui="io-backup-confirm" >}}The confirmation shows exactly what will run. The dump is written to `cache.rdb.part` and renamed to `cache.rdb` when the tool succeeds.{{< /ui >}}

### Common options

{{< ui tui="io-backup-options" >}}{{< key "C-o" >}} on a MySQL backup: the usual `mysqldump` flags as a checklist. Ticked flags are added to **Extra options**, unticked ones removed from it.{{< /ui >}}

The checklist ({{< key "Space" >}} toggles, {{< key "a" >}} / {{< key "A" >}} check / uncheck all, {{< key "Enter" >}} applies) is built from the **File** and **Format** at the moment it opens, so a PostgreSQL restore offers `psql`'s flags for a `.sql` file and `pg_restore`'s for an archive. Without common options for the engine it says "no common options for *driver*".

### Web

{{< ui web="io-backup-dialog" >}}**Back up database** on Redis after **Review command**: the tool that will run, and the command line.{{< /ui >}}

| Control | What it does |
|---|---|
| file name | The backup file's name in the [backups folder](#where-backups-go). |
| **gzip**, format (`plain` / `custom`, PostgreSQL), **data only**, **schema only** | As in the terminal. |
| **File name pattern** | Starts as `{database}-{date}.sql`. Used only when the file name field is empty; a pattern without a `.` gets `.sql` (`.sql.gz` with gzip) whatever the format. |
| **Common options** | A checkbox per common flag, with its description. |
| **Extra options** | More flags; double quotes group a value with spaces. "Also accepted:" lists every flag the server takes for this tool. |
| **Tool paths…** | See [Tool paths](#tool-paths). |

**Review command** asks the server to build the command without running it: it shows "Tool: *path*", "This will run:" and the command, plus the [safe mode](../../safety/safe-mode/) confirmation or password field when the connection's level asks for one. Changing any field hides the review again. **Back up** runs it; when the file already exists the browser asks "That backup file already exists in the backups folder. Overwrite it?". {{< key "Enter" >}} in the file name field reviews, then runs.

## Restore

{{< ui tui="io-restore-dialog" web="io-restore-dialog" >}}Restoring into a new database `shop_io_restored`. The terminal takes the name in **Restore into a new database**; the browser has a checkbox, the name, and optional encoding and collation.{{< /ui >}}

- **Terminal**: the form **Restore *database*** has **File** (default `<database>.sql`, must exist: "cannot read that file"), **Extra options**, **Gzip** (the file is gzip-compressed), **Format** (PostgreSQL) and **Restore into a new database** (empty: into the current one). The confirmation **Restore *database*?** shows the command, and then the restore passes the [safe mode](../../safety/safe-mode/) gate like a destructive statement, with the command line as the statement.
- **Web**: **Restore database** has the file name (it must be in the backups folder: "*name* does not exist in the backups folder"), **gzip**, format, **Restore into a new database, created first** with its name, **encoding** and **collation** (PostgreSQL and MySQL), common and extra options. **Review command** shows "First this is sent to the server:" with the `CREATE DATABASE` and the restore command, and the safe-mode fields; **Restore** runs both.

Data only / schema only are not offered for a restore (a MySQL restore could not filter anyway).

### Restoring into a new database

The `CREATE DATABASE` runs first, through the same gate, and the restore runs into the new database. If the restore then fails, the new database is still there, empty or half filled:

- **Terminal**: after the failure it asks **Drop the new database *name*?** with the `DROP DATABASE` statement; **Drop** sends it through the safe-mode gate.
- **Web**: the error ends with "(database *name* was created before the restore and is still there; drop it if you do not want it)". Drop it yourself from the sidebar or the editor.

{{< callout type="warning" >}}
A restore into the current database writes over its data: what happens to existing objects is up to the dump (for example `--clean` drops them first). With PostgreSQL's `psql` and MySQL's `mysql`, statements before a failure stay applied unless you add `--single-transaction` (PostgreSQL); `mysql --force` goes on after errors.
{{< /callout >}}

## Gzip

| Engine | Backup | Restore |
|---|---|---|
| PostgreSQL plain | `pg_dump -Z 6` (needs `pg_dump` 16 or later) | webtuidb unzips the file into `psql` |
| PostgreSQL custom | ignored (the archive is compressed already) | ignored |
| MySQL / MariaDB | webtuidb compresses the tool's output | webtuidb unzips the file into `mysql` |
| MongoDB | `--gzip` | `--gzip` |
| Redis, ClickHouse | ignored | not applicable |

Add `.gz` to the file name yourself; the tick does not change the name (except the web pattern without an extension).

## Extra options

Extra flags are checked against an exact allow-list per tool, so no option can redirect the output, change the host or database, run a command or read another config file. Anything else is refused: "extra option "--foo" is not allowed", or "extra option "x" must start with -".

| Tool | Allowed |
|---|---|
| `pg_dump` | `--no-owner` `--no-privileges` `--clean` `--if-exists` `--inserts` `--no-comments` `--no-tablespaces` `--no-acl` `--column-inserts` `--quote-all-identifiers` |
| `pg_restore` | `--clean` `--if-exists` `--no-owner` `-j N` / `--jobs=N` (N from 1 to 64) `--no-comments` `--no-tablespaces` `--no-acl` `--single-transaction` `--exit-on-error` `--no-privileges` |
| `psql` (plain restore) | `--single-transaction` only; others answer "… is not accepted by psql (a plain SQL restore); use the custom format for pg_restore options" |
| `mysqldump` / `mariadb-dump` | `--no-data` `--no-create-info` `--single-transaction` `--routines` `--triggers` `--events` `--skip-add-drop-table` `--no-tablespaces` `--skip-lock-tables` `--skip-triggers` `--compact` `--hex-blob` |
| `mysql` / `mariadb` | `--force` `--binary-mode` |
| `mongodump` | `--gzip` `--oplog` |
| `mongorestore` | `--gzip` `--drop` `--oplogReplay` |
| `redis-cli`, `clickhouse-client` | none |

The common options (the checklist and the web checkboxes) are a subset of these with a description. The flags go after webtuidb's own flags and before the database name or file.

## Tool paths

{{< ui tui="io-backup-missing" web="io-backup-missing" >}}`mysqldump` and `pg_dump` are not installed on this machine. The terminal says so in the form; the browser says it after **Review command**.{{< /ui >}}

A missing tool shows as "*tool* not found (on PATH or at its configured path): press Ctrl+t to set its path" in the terminal form (the form still opens so you can set the path), and as "*tool* is not on PATH. Install it, or set its path." (or "was not found at *path*") in the browser. Running anyway fails with "*tool* not found (on PATH or at its configured path)".

{{< ui tui="io-backup-tools" >}}{{< key "C-t" >}} in the backup form: one path per tool the engine uses; empty looks the tool up on `PATH`.{{< /ui >}}

- **Terminal**: {{< key "C-t" >}} in the backup or restore form opens **Tool paths for *connection*** with a field per tool (PostgreSQL: `pg_dump`, `pg_restore`, `psql`; MySQL: `mysqldump`, `mysql`; MariaDB: `mariadb-dump`, `mariadb`, `mysqldump`, `mysql`; ClickHouse: `clickhouse-client`; MongoDB: `mongodump`, `mongorestore`; Redis: `redis-cli`). A path must name an existing file ("*path*: no such file"). The paths are saved on the connection ("tool paths saved"); for a connection that comes from a `[[source]]` folder they last for this session only.
- **Web**: **Tool paths…** opens the same list. The browser may only set an absolute path to a file named like the tool, optionally with a version: `/usr/lib/postgresql/16/bin/pg_dump` or `/opt/bin/pg_dump-16` ("tool … must be an absolute path to a file named like pg_dump"), because the server runs it. A connection from a source file must be edited in that file.

In `connections.toml` the paths are the connection's `tools` table, keyed by tool name:

```toml
[[connection]]
name = "Shop (Postgres)"
driver = "postgres"
tools = { pg_dump = "/opt/homebrew/opt/postgresql@16/bin/pg_dump", psql = "/opt/homebrew/opt/postgresql@16/bin/psql" }
```

`tools = {}` (or leaving it out) means every tool is looked up on `PATH`. MariaDB tries `mariadb-dump` / `mariadb` first, then `mysqldump` / `mysql`.

## While it runs

- **Terminal**: the **Backup *file*** (or **Restore *file***) window shows `$ command`, every line the tool prints, and finally "✓ done", "✗ *error*" or "-- cancelled". It keeps the last 2,000 lines; {{< key "↑" >}} {{< key "↓" >}} / {{< key "k" >}} {{< key "j" >}}, {{< key "PgUp" >}} {{< key "PgDn" >}} ({{< key "Space" >}}), {{< key "g" >}} / {{< key "Home" >}} and {{< key "G" >}} / {{< key "End" >}} scroll. {{< key "Esc" >}} or {{< key "C-c" >}} while it runs asks **Cancel the backup?** ("The tool is stopped; a partial file may be left behind."); **Cancel it** stops the tool and its child processes. When it is over, {{< key "r" >}} reveals a finished backup in the file manager and {{< key "Esc" >}} closes. The status line says "Backup finished: *file*".
- **Web**: the dialog turns into the output log (the last 5,000 lines are kept), with **Cancel** while it runs; it cannot be closed meanwhile. Then "Done." or the error, and **Close**. A toast (and a desktop notification with `[web] notifications`) says "Backup finished" or "Backup failed: …".
- Both list the run in the [jobs centre](../../workspace/jobs-and-notifications/), where it can be stopped too. The terminal runs one background job at a time per workspace ("another export, import, backup or search is still running (cancel it first)"); the browser up to four per connection.

{{< ui web="io-jobs" >}}The browser's jobs centre after the Redis backup: the job, its status and its last output lines.{{< /ui >}}

### The output file

A dump is written to `<file>.part` (mode 0600, created before the tool starts) and renamed to `<file>` only when the tool exits successfully; the final file is mode 0600, because a dump holds all the data. On a failure or cancel the `.part` file is removed, and an existing backup of the same name is left as it was. (PostgreSQL custom archives, MongoDB archives and Redis RDB files are written by the tool itself into the `.part` file; MySQL output passes through webtuidb.)

## Where backups go

- **Terminal**: anywhere you name in **File**, relative to the folder you started `webtuidb` in.
- **Web**: only the `backups` folder of the data directory, `~/.local/share/webtuidb/backups` (or `$XDG_DATA_HOME/webtuidb/backups`), created with mode 0700 on first use. Give a plain file name. An absolute path elsewhere, a subfolder or `..` is refused ("backup files live in *folder*; give a file name there", "*name*: .. is not allowed"), as are a symlinked folder and a name that is not a regular file. The finished file stays on the server; the browser does not download a backup. ClickHouse is the exception: its name is on the server's `backups` disk and is passed as is (it cannot start with `-`).

## Remembered options

The options you used last are remembered per connection and per direction (backup / restore) in the shared UI state, so the next dialog opens with them, in either UI:

- The terminal remembers **File**, **File-name pattern**, **Extra options**, **Gzip**, **Format**, **Data only** and **Schema only**. A **File** left at its dated default is remembered as empty, so tomorrow's default carries tomorrow's date.
- The browser remembers the backup file's name, gzip, format, data only and schema only (for a restore: gzip and format), and fills in those it finds, including a file name the terminal remembered.

## Gates and safe mode

- A `read_only` connection refuses both, because the tools log in on their own: "*connection* is read-only: backup and restore are refused" (terminal), "this connection is read only; backup and restore are refused" (web).
- **Terminal**: a backup asks for confirmation and, for an existing file, for the overwrite; it does not go through safe mode. A restore (and the `CREATE DATABASE` before it) passes the safe-mode gate.
- **Web**: both a backup and a restore pass the safe-mode gate, with the command line as the statement, because one overwrites a file and the other data.
- Successful runs are written to the query [history](../../sql-editor/history/) in the browser as the command line.

{{< ui tui="io-backup-readonly" web="io-backup-readonly" >}}**Shop prod replica** is `read_only`: the terminal refuses at once in the status line; the browser after **Review command**.{{< /ui >}}

## Details and limits

- `pg_dump -Z` on plain format needs `pg_dump` 16 or later; older versions fail with their own error.
- A MySQL restore cannot be limited to data or schema.
- ClickHouse backup needs the server-side `backups` disk; there is no client-side dump.
- The browser keeps 5,000 output lines, the terminal 2,000.
- Restoring into a new database that then fails leaves that database behind in the browser (the terminal offers to drop it).

## Settings

| Setting | Effect |
|---|---|
| `[export] filename` | the terminal's default backup **File** (`{table}` is the database) |
| `tools` on a connection | tool paths, see [the connections file](../../configuration/connections-file/) |
| `read_only`, `safe_mode` on a connection | the gates above |
| `[web] notifications` | desktop notification when a browser backup or restore finishes |

See the [settings reference](../../configuration/settings-reference/). The HTTP endpoints are `POST /api/sessions/{sid}/backup` and `…/restore` (`Run: false` reviews, `Run: true` starts a job) and `GET …/backup/options`; see the [API](../../web-ui/api/).
