---
title: PostgreSQL
description: What webtuidb supports on PostgreSQL, from connection fields and TLS to schemas, type handling, pinned transactions and server-side read-only.
weight: 20
---

PostgreSQL has the fullest support of any engine: schemas, every object kind, the structure editor with foreign keys, users, the process list, a plan tree for `EXPLAIN`, and atomic commits. The driver (`driver = "postgres"`) is built on pgx and also carries the Redshift, Greenplum and Vertica [profiles](../profiles/).

{{< ui tui="engines-postgresql" web="engines-postgresql" >}}`orders` in the demo `shop` database. The sidebar groups tables, views, materialized views, functions, procedures and sequences of the current schema. `timestamptz` values keep their offset (`+00`).{{< /ui >}}

## Connecting

| Field | Default | Meaning on PostgreSQL |
|---|---|---|
| `host` / `port` | `127.0.0.1` / `5432` | The server. With an SSH tunnel the host name is resolved on the far side. |
| `user` / `password` | — | The login. `password_cmd` and `password = "ask"` work as for every engine. |
| `database` | the server's default for the user | The database to open. Switch later with {{< key "C-d k" >}}. |
| `ssl_mode`, `ssl_ca`, `ssl_cert`, `ssl_key` | libpq's `prefer` | See [TLS](#tls). The files map to `sslrootcert`, `sslcert` and `sslkey`. |
| `encoding` | UTF-8 | Only UTF-8 is accepted. Anything else fails with `encoding "latin1" is not supported by PostgreSQL connections (only UTF-8; leave it empty)`, because pgx does no transcoding. |
| `options` | — | Every entry is passed as a libpq connection parameter, for example `application_name = "webtuidb"` or `connect_timeout = "5"`. |
| `iam_auth` | off | Signs a 15-minute AWS RDS IAM token as the password for every new connection. `options.region` names the token's region and is not sent to the server. |
| `bootstrap` | — | Statements run on every new pooled connection, and again after the connection is reset. |
| `read_only` | off | Sets `default_transaction_read_only = on` on every connection, so the server refuses writes; the driver also refuses non-read-only statements with `connection is read_only`. |

The connect timeout is 15 seconds unless `options.connect_timeout` says otherwise. The URL form is `postgresql://user:password@host:5432/db?sslmode=require`; `postgres://` works too. `driver_version` is kept for TablePlus URLs and has no effect.

### TLS

`ssl_mode` takes `disable`, `prefer`, `require`, `verify-ca` or `verify-full`, with libpq's meaning. With no `ssl_mode` the server decides: libpq's `prefer` tries TLS and falls back to plain text. `verify-identity` (a MySQL name) is refused: `ssl_mode "verify-identity" is not supported by PostgreSQL (use disable, prefer, require, verify-ca or verify-full)`.

With `iam_auth`, TLS is required: an empty `ssl_mode` becomes `require`, and `disable`, `prefer` or `allow` fail with `iam_auth needs TLS: ssl_mode "prefer" would send the IAM token unencrypted`.

## Sidebar and schemas

The sidebar lists one schema at a time. The schema picker hides `pg_toast*`, `pg_temp*`, `information_schema` and `pg_catalog`. Switch schemas with `s` on the schema line in the terminal sidebar, or with the schema drop-down above the web sidebar. Create one with {{< key "C-d M-n" >}}.

| Group | What is listed | Dimmed detail |
|---|---|---|
| Tables | ordinary, partitioned and foreign tables | size estimate from the last `VACUUM`/`ANALYZE` (`312 kB`) |
| Views | views | — |
| Materialized views | materialized views | size estimate |
| Functions / Procedures | `pg_proc` entries, one per overload | the argument list `(p_customer_id integer)` |
| Sequences | sequences, including the ones behind `serial` columns | — |
| Types | enums, domains and standalone composite types | `enum`, `domain` or `composite` |

Objects that belong to an extension (PostGIS tables, `pg_stat_statements` views) are left out.

Switching the schema changes the session's `search_path` to that schema, then `public`, on every pooled connection. The SQL editor, completion, search in database and the schema diff therefore resolve unqualified names in the schema on screen. Back on the schema you connected with, the server's default path applies again.

## Browsing, paging and counting

A table opens with `SELECT … LIMIT n OFFSET m`. With no sort chosen, rows are ordered by the primary key, so pages do not overlap. A table without a primary key stays in server order.

The row count in the footer is an estimate from `pg_class.reltuples` (shown as `≈1,200` in the web). When the table was never analyzed (`-1`), or the statistics say `0`, the count is exact instead. **Count exactly** in the web, or {{< key "C-d #" >}} in the terminal grid, runs `SELECT count(*)` with the current filters. With a filter the count is always exact.

Filters use `ILIKE` for Contains, Not contains, Has prefix and Has suffix, so they ignore case. Non-text columns are cast to text first. The **Any column** filter casts every column to text, except `bytea`, `tsvector`, `tsquery` and `oid`.

## How edits become statements

Identifiers are quoted with double quotes and values travel as `$1`, `$2`… parameters. **Preview** ({{< key "C-d P" >}}) shows the statements with the values inlined. A changed row is matched by its primary key. In a table without one, every original value of the row goes into the `WHERE`.

- Insert: `INSERT INTO "public"."orders" ("status", …) VALUES ($1, …)`, or `DEFAULT VALUES` when you filled nothing in.
- Update: `UPDATE … SET "status" = $1 WHERE "id" = $2`.
- Delete: `DELETE FROM … WHERE "id" = $1`.

Values are sent untyped, so `7` into an `integer` column, `2024-01-02` into a `date` and `t` into a `boolean` work as typed. A commit runs every statement in one transaction. If one fails, the error names the statement and nothing is applied. The exceptions are `CREATE DATABASE`, `DROP DATABASE`, anything with `CONCURRENTLY` and `VACUUM`, which PostgreSQL refuses inside a transaction. A batch containing one of them runs without a transaction.

## Type handling

| Type | Shown as | Notes |
|---|---|---|
| `timestamptz` | the server's text, `2024-09-12 02:56:00+00` | The offset stays, so the cell, a filter on it and a key written back all mean the same instant whatever the client's time zone. |
| `timestamp`, `date` | a date-time | `infinity` and `-infinity` stay text. |
| `bytea` | bytes (hex in the grid, image preview in quick look) | |
| `json`, `jsonb` | text | The cell editor pretty-prints JSON. Write back valid JSON text. |
| `boolean`, integers, `oid`, floats | typed values | `numeric` stays text, so no digits are lost. |
| enums, domains, extension types | text, with the type's real name in the column header | The name comes from `format_type()` once per connection pool. |

## Structure

The structure view ({{< key "C-d )" >}}) shows each column's type (`format_type`), nullability and default. The Extra column marks:

- `serial`: the default is `nextval()` of a sequence the column owns.
- `GENERATED ALWAYS AS IDENTITY` / `GENERATED BY DEFAULT AS IDENTITY`.
- `GENERATED ALWAYS AS (expr) STORED`.

The structure editor plans `ALTER TABLE` statements in dependency order. Foreign keys, the primary key and indexes are dropped first, then columns are dropped, renamed, altered and added. Then the primary key, indexes and foreign keys are added, and the table is renamed last. A type change is `ALTER COLUMN "c" TYPE t USING "c"::t`. A unique constraint's backing index is dropped with `DROP CONSTRAINT`, by its real name. Changing a column's identity or generated attribute is refused: `column "id": editing identity/generated attributes is not supported`.

A `serial` column copied into another table (schema diff, the table designer) is written as `serial`, `bigserial` or `smallserial`, so the copy gets its own sequence. **DDL** ({{< key "C-d M-d" >}}) rebuilds the table from the catalog: `CREATE SEQUENCE IF NOT EXISTS` for each serial first, then `CREATE TABLE` with every constraint, its indexes, `PARTITION BY` when partitioned, and `ALTER SEQUENCE … OWNED BY` last. Partitions and foreign tables have no create script (`orders_2024 is a partition: a create script is not supported for it`). Views come out as `CREATE OR REPLACE VIEW`, materialized views with their indexes, functions from `pg_get_functiondef`, sequences with every option, enums as `CREATE TYPE … AS ENUM`, and domains as `CREATE DOMAIN`.

## Database and object operations

| Operation | Statement |
|---|---|
| Create database (encoding, collation) | `CREATE DATABASE "d" ENCODING 'UTF8' LC_COLLATE 'x' LC_CTYPE 'x' TEMPLATE template0` (the template only when you set an encoding or collation) |
| Rename / drop database | `ALTER DATABASE "d" RENAME TO "e"` / `DROP DATABASE "d"` |
| Create / drop schema | `CREATE SCHEMA "s"` / `DROP SCHEMA "s" [CASCADE]` |
| Truncate | `TRUNCATE TABLE t [RESTART IDENTITY] [CASCADE]` |
| Clone with data | `CREATE TABLE copy (LIKE t INCLUDING ALL)`, `INSERT INTO copy OVERRIDING SYSTEM VALUE SELECT * FROM t`, then a `DO` block that gives each serial column its own sequence and moves every sequence past the copied rows |
| Rename / drop any object | `ALTER <kind> … RENAME TO …` / `DROP <kind> … [CASCADE]` |

## SQL editor

- Errors carry the position: `syntax error at or near "FORM" at line 2, column 15`.
- Server notices (`RAISE NOTICE`, `NOTICE: table does not exist, skipping`) are added to the result message.
- Each statement runs on a pooled connection (at most eight), so `BEGIN` and `SET` do not carry over to the next run. **Pin connection to this tab** (tab menu) lends one connection to the tab: `BEGIN`, `SET` and temporary tables carry over and `ROLLBACK` works. The status shows when a transaction is open. A pinned statement is never cancelled midway, because that would abort the transaction. A plain `SELECT` is stopped at the row limit on the server instead; any other statement's rows are read to the end.
- After a connection ran your SQL, it is reset with `DISCARD ALL` and the bootstrap statements before reuse, so a `SET ROLE` or temporary table never leaks into the next statement.
- A pooled connection the server dropped while idle (restart, idle timeout) is retried once, but only when nothing reached the server or the server terminated it at once. A statement an administrator killed is never run again.
- {{< key "C-d X" >}} explains the current statement with `EXPLAIN (FORMAT JSON, ANALYZE false)` and shows the plan as a tree with cost and rows.

## Safe mode

Alert Mode 2 and Safe Mode 2 let read-only statements through without asking. On PostgreSQL a statement counts as read-only when it starts with `SELECT`, `VALUES`, `TABLE`, `SHOW`, `EXPLAIN` (without `ANALYZE`, or `ANALYZE` of a read), or `WITH` without a write keyword. `SELECT … INTO` and text holding several statements are writes. The tokenizer follows PostgreSQL's rules, so a write cannot hide inside them: `''` escapes only inside `'…'`, backslashes only in `E'…'`, `/* */` nests, `$tag$…$tag$` is a string, and `#` is an operator, not a comment.

## Tools

- **Process list** ({{< key "C-d ." >}}): non-idle backends from `pg_stat_activity`, without your own. Kill runs `pg_cancel_backend(pid)`, which cancels the query and keeps the session. It is refused on a `read_only` connection.
- **Users** ({{< key "C-d u" >}}): roles from `pg_roles` (without the `pg_*` ones) with SUPERUSER, CREATEDB, CREATEROLE, LOGIN, REPLICATION, BYPASSRLS, the connection limit and table privileges.
- **Backup / restore** ({{< key "C-d b" >}} / {{< key "C-d B" >}}): `pg_dump`. A custom-format archive is restored with `pg_restore`, a plain SQL file with `psql`. Both are refused on a `read_only` connection, because the tools log in on their own.
- **Import**: on-conflict `update` and `replace` become `INSERT … ON CONFLICT`. A whole file is imported in one transaction on a pinned connection, so a failure rolls back every row, the table creation included.

## Limits

- Statement parameters (`:name`, `$1` in the editor) do not understand `$tag$` dollar quoting, so a dollar-quoted literal can hide or invent a parameter.
- **Users** lists table, view and foreign-table privileges only, not sequence or schema privileges. A grant on a schema whose name contains a dot is split at the first dot.
- Search in all schemas skips system schemas by name (`pg_catalog`, `information_schema`).
- Paging an editor result ("load more") wraps the statement in a subquery and runs it again for each page; there is no server cursor. A statement with a top-level `ORDER BY`, `LIMIT`, `FOR` or `INTO` is re-run with a doubled limit instead.
