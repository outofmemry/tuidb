---
title: ClickHouse
description: What webtuidb supports on ClickHouse, from the native-protocol connection and TLS to mutations, the sorting key, USE, server-side BACKUP and the process list.
weight: 50
---

ClickHouse (`driver = "clickhouse"`) connects over the native protocol with clickhouse-go. You can browse and edit tables, views and materialized views, reorder and edit columns, rename databases, run SQL, kill queries and back up a database with the server's own `BACKUP`. ClickHouse has no transactions, foreign keys, triggers, stored functions or editable indexes, so those parts of the UI are hidden.

{{< ui tui="engines-clickhouse" web="engines-clickhouse" >}}`events` in the `webtuidb` database on ClickHouse 24.8. The key marks on `ts` and `kind` are the table's sorting key, which ClickHouse reports as its primary key.{{< /ui >}}

## Connecting

| Field | Default | Meaning on ClickHouse |
|---|---|---|
| `host` / `port` | `127.0.0.1` / `9000` | The **native** TCP port: 9000 plain, usually 9440 with TLS. The HTTP port 8123 does not work. |
| `user` / `password` | `default` / — | An empty user logs in as `default`. |
| `database` | the user's default database | Switch with {{< key "C-d k" >}} or `USE db` in the editor. |
| `ssl_mode`, `ssl_ca`, `ssl_cert`, `ssl_key` | plain TCP | See below. `ssl_cert` and `ssl_key` must be set together. |
| `bootstrap` | — | `SET name = value` lines become client settings sent with every query on every pooled connection. Any other statement runs once, on one connection, right after connecting. |
| `encoding`, `options`, `read_only` | — | `encoding` and `options` are not used. `read_only` is enforced by the web UI only; see the [capability matrix](../capabilities/#safety-and-transport). |

The pool keeps at most eight connections open (two idle). A URL may say `clickhouse://user:password@host:9000/db?secure=true`, where `secure=true` means `ssl_mode = require`.

{{< callout type="info" >}}
ClickHouse has no opportunistic TLS. `ssl_mode = prefer` (and an empty `ssl_mode`) connect in **plain text**. Use `require` to encrypt without checking the certificate, `verify-ca` to check the chain against `ssl_ca`, and `verify-full` or `verify-identity` to check the host name too.
{{< /callout >}}

## Sidebar

Tables, views (`engine = 'View'`) and materialized views (`MaterializedView`) of the current database, with `system.tables.total_rows` as the dimmed count. The `.inner` tables that back materialized views are hidden. There are no schemas: a schema is a database, and **Create schema** answers `clickhouse has no schemas: a schema is a database, use create / drop database`.

## Browsing, paging and counting

Rows come from `SELECT … LIMIT n OFFSET m`, with backtick quoting and `?` parameters. Without a sort the rows come in server order, so unsorted pages of a table that is being written to may overlap. The footer count is `total_rows` from `system.tables`, shown as an estimate. Where `total_rows` is `NULL` (views, and engines that do not track it), the count is exact, and so is a filtered count, which runs `SELECT count()`.

Contains, Has prefix and Has suffix use `LIKE` on the column cast with `toString()`, and are case-sensitive. The **Any column** filter leaves out `Map`, `Array`, `Tuple`, `Nested`, `JSON`, `Object` and aggregate-function columns: converting those to text for every row is slow enough to get the query killed. Filter on them by name instead.

Values keep their types. `Decimal` keeps its scale as text (`12.50`), `UUID` and `IPv4`/`IPv6` become text, `Float32` shows its shortest form (`0.1`, not `0.10000000149`), and `Array`, `Map` and `Tuple` show as JSON.

## How edits become statements

ClickHouse changes rows with *mutations*, not `UPDATE` and `DELETE`:

- Insert: ``INSERT INTO `events` (`ts`, `kind`, `n`) VALUES (?, ?, ?)``. A column left at DEFAULT is omitted, because `VALUES` has no `DEFAULT` keyword. A row with nothing filled in is refused: `ClickHouse cannot insert a row without values: fill at least one column`.
- Update: ``ALTER TABLE `events` UPDATE `n` = ? WHERE `ts` = ? AND `kind` = ?``.
- Delete: ``ALTER TABLE `events` DELETE WHERE …``.

Every statement runs with `mutations_sync = 1`, so the grid shows the result as soon as the commit returns. Statements run one after another, and the first failure stops the commit. The ones before it stay applied, since ClickHouse has no transactions.

{{< callout type="warning" >}}
A ClickHouse primary key is a sorting key, not a uniqueness constraint. An edit or delete matches the row by the key columns, so it changes **every row with the same key values**, not only the one you selected. In the `events` table above, deleting one row deletes every event with the same `ts` and `kind`. Before you commit on a table whose sorting key can repeat, check the statements in **Preview** ({{< key "C-d P" >}}), or edit with a `WHERE` that names a unique column in the SQL editor.
{{< /callout >}}

## Structure

The structure view reads `system.columns`. `Nullable(…)` types show as nullable, a `DEFAULT` expression as the default, and `MATERIALIZED`, `ALIAS` and `EPHEMERAL` expressions and column comments in Extra.

The structure editor can add, drop, rename and move columns (`ADD COLUMN … AFTER`, `RENAME COLUMN`, `MODIFY COLUMN … FIRST`) and drop a default (`MODIFY COLUMN c REMOVE DEFAULT`). These are refused:

- changing the sorting key: `ClickHouse sorting key cannot be altered`;
- editing or creating data-skipping indexes: `ClickHouse indexes cannot be edited here`;
- foreign keys: `ClickHouse has no foreign keys`.

A new table is created as `ENGINE = MergeTree ORDER BY (the key columns)`, or `ORDER BY tuple()` without a key. **DDL** is `SHOW CREATE TABLE`, which works for views and materialized views too.

| Operation | Statement |
|---|---|
| Create / drop database | ``CREATE DATABASE `d` `` / ``DROP DATABASE `d` `` |
| Rename database | ``RENAME DATABASE `d` TO `e` ``; only Atomic databases allow it, and the server says so otherwise |
| Truncate | `TRUNCATE TABLE t` (no cascade) |
| Clone | `CREATE TABLE copy AS t`, plus `INSERT INTO copy SELECT * FROM t` with data |
| Rename table | `RENAME TABLE a TO b` |
| Drop view or materialized view | `DROP VIEW v` |

## SQL editor

- `USE db` switches the session, not one pooled connection, and answers `Database changed`. Comments before `USE` are fine.
- A read-only statement (see below) returns rows. Anything else is executed and reports the rows written, from the server's progress packets (`5 rows affected`), or `Statement executed`.
- {{< key "C-d X" >}} runs `EXPLAIN <statement>` and shows the text plan indented as a tree.
- `SET name = value` in the editor applies to one pooled connection only. Put session settings in `bootstrap` instead.

## Safe mode

The tokenizer follows ClickHouse: a backslash escapes inside quotes and inside backticks, and `#` and `--` start comments. `SELECT`, `SHOW`, `DESCRIBE`, `EXISTS`, `EXPLAIN` and `WITH` without a write keyword are reads; `ALTER TABLE … UPDATE` and `… DELETE` are writes.

## Tools

- **Process list**: `system.processes` without your own query, longest first. Kill runs `KILL QUERY WHERE query_id = ? SYNC`.
- **Backup / restore**: server side, through `clickhouse-client`. Backup runs ``BACKUP DATABASE `d` TO Disk('backups', 'file')`` and restore runs ``RESTORE DATABASE `d` FROM Disk('backups', 'file')``. The file name is a name on the server's `backups` disk, not a local path, so the server config must define that disk. The password travels in `CLICKHOUSE_PASSWORD`, not on the command line.
- **Users**, triggers, foreign keys, functions and sequences are not available.

## Limits

- `bootstrap` statements other than `SET` run once per session, not on every pooled connection, because the native client has no per-connection hook.
- Backup needs a server disk named `backups`; there is no client-side dump.
