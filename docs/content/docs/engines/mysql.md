---
title: MySQL
description: What webtuidb supports on MySQL, from connection fields, TLS and charset to implicit DDL commits, generated columns, DELIMITER and pinned sessions.
weight: 30
---

MySQL (`driver = "mysql"`) uses go-sql-driver/mysql. It supports tables, views, functions, procedures and triggers, the structure editor with column reordering and foreign keys, users, the process list and JSON plans. The same session code serves [MariaDB](../mariadb/) and the [SingleStore profile](../profiles/#singlestore). It tells the flavour apart by the server's version string, so the status line says "MySQL 8.4.13" or "MariaDB 11.4.13".

{{< ui tui="engines-mysql" web="engines-mysql" >}}`orders` in the demo `shop` database on MySQL. The number next to each table is the server's row estimate (`TABLE_ROWS`).{{< /ui >}}

## Connecting

| Field | Default | Meaning on MySQL |
|---|---|---|
| `host` / `port` | `127.0.0.1` / `3306` | The server, over TCP. A Unix socket is not used. |
| `user` / `password` | — | The account. |
| `database` | none | The database to open. Without one the sidebar says `no database selected`: pick one with {{< key "C-d k" >}}. |
| `encoding` | the server's default | Sent as the connection's `charset`, for example `utf8mb4` or `latin1`. It must be a plain charset name. |
| `ssl_mode`, `ssl_ca`, `ssl_cert`, `ssl_key` | plain text | See [TLS](#tls). |
| `iam_auth` | off | An AWS RDS IAM token is signed for every new connection and sent as a clear-text password over TLS. |
| `bootstrap` | — | Runs on every new physical connection. |
| `read_only` | off | Adds `SET SESSION TRANSACTION READ ONLY` to every connection, so the server refuses writes. The driver also refuses non-read-only statements with `connection is read_only`; `USE` is allowed. |
| `options` | — | Not used by this driver. |

Dates are read in UTC. A zero date comes back as the text `0000-00-00` (or `0000-00-00 00:00:00`) rather than an error. The URL form is `mysql://user:password@host:3306/db`. A pool keeps up to eight connections and closes idle ones after five minutes.

### TLS

| `ssl_mode` | Behaviour |
|---|---|
| empty, `disable` | Plain text. |
| `prefer` | TLS when the server offers it, without certificate checks. Falls back to plain text. |
| `require` | TLS, certificate not checked. |
| `verify-ca` | TLS, the chain is checked against `ssl_ca`, the host name is not. |
| `verify-full`, `verify-identity` | TLS with chain and host name checked. |

A pasted URL may use MySQL's own spellings: `ssl-mode=REQUIRED` (`DISABLED`, `PREFERRED`, `VERIFY_CA`, `VERIFY_IDENTITY`) or `tls=true`, `tls=skip-verify`, `tls=preferred`. With `iam_auth` an empty `ssl_mode` becomes `require`, and `disable` or `prefer` are refused.

## Sidebar

Tables (with the `TABLE_ROWS` estimate), views, functions and procedures of the current database. MySQL has no schemas, so the database is the namespace. Opening another database reopens the connection pool, because `USE` would change only one pooled connection.

## Browsing, paging and counting

Rows come from `SELECT … LIMIT n OFFSET m`, ordered by the primary key when you have not chosen a sort. The footer count is `information_schema.tables.TABLE_ROWS`, which InnoDB only estimates. A view, or a table whose estimate is `0`, is counted exactly, and so is any filtered count. {{< key "C-d #" >}} or **Count exactly** runs `SELECT COUNT(*)`.

Contains and the prefix and suffix tests use `LIKE`, so case sensitivity follows the column's collation. The **Any column** filter casts columns with `CAST(… AS CHAR)` and leaves out blob, binary and spatial columns.

## How edits become statements

Identifiers are quoted with backticks and values are bound as `?` parameters. They are interpolated on the client with the connection's charset, so each statement is one round trip. In **Preview**, backslashes in string literals are shown doubled, because MySQL reads a backslash as an escape.

- Insert: ``INSERT INTO `orders` (`status`, …) VALUES (?, …)``, or `INSERT INTO t () VALUES ()` when you filled nothing in.
- Update / delete: ``UPDATE … SET … WHERE `id` = ?`` and `DELETE FROM … WHERE …`, matched by the primary key or, without one, by every original value.

A commit of data changes runs in one transaction.

{{< callout type="warning" >}}
MySQL commits DDL implicitly: a transaction cannot undo `CREATE`, `ALTER`, `DROP`, `RENAME` or `TRUNCATE`. A batch containing any of them runs one statement at a time without a transaction and stops at the first error. The error says what stays applied: `statement 3 (ALTER TABLE …): … (2 of 5 statements were already applied and stay applied: MySQL cannot roll back DDL)`. Check the table before you run the structure change again.
{{< /callout >}}

## Structure

The structure editor plans one `ALTER TABLE` with every change, plus a separate one first when a foreign key must be dropped and added under the same name. A rename is `CHANGE COLUMN`. A changed column is `MODIFY COLUMN`, and a moved one (drag it, or use the arrows) gets `FIRST` or ``AFTER `previous` ``. The primary key is dropped and added whole.

- **Generated columns** show their full clause in Extra, `GENERATED ALWAYS AS (expr) VIRTUAL` or `STORED`, instead of the bare `STORED GENERATED` that `information_schema` reports. Clone with data copies every other column, because `INSERT` cannot set a generated one.
- **Expression defaults** (`DEFAULT (uuid())`, `CURRENT_TIMESTAMP`) keep their SQL. A binary column's literal default is read from `SHOW CREATE TABLE`, so it survives a `MODIFY`.
- **Collation** is listed only where a column differs from its table's, so a `MODIFY` that does not mention it keeps the table default.

**DDL** ({{< key "C-d M-d" >}}) is `SHOW CREATE TABLE`, `VIEW`, `FUNCTION` or `PROCEDURE` (and `SEQUENCE` on MariaDB). A missing definition (usually a missing privilege) says `server returned no definition for function "f" (missing privilege?)`.

| Operation | Statement or answer |
|---|---|
| Create database (encoding, collation) | ``CREATE DATABASE `d` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci`` |
| Drop database | ``DROP DATABASE `d` `` |
| Rename database | refused: `MySQL cannot rename a database: create a new one and move the tables with RENAME TABLE` |
| Create / drop schema | refused: a schema is a database |
| Truncate | `TRUNCATE TABLE t`; with cascade: `MySQL has no TRUNCATE ... CASCADE: empty the child tables first` |
| Rename table or view | `RENAME TABLE a TO b` |
| Clone | `CREATE TABLE copy LIKE t`, then `INSERT INTO copy (cols) SELECT cols FROM t` |

## SQL editor

- `USE db` switches the whole session (it reopens the pool on that database) and answers `Database changed`.
- Scripts may change the statement terminator with `DELIMITER $$` lines, so routine bodies can contain `;`. The `DELIMITER` lines themselves are not sent.
- Editing a function or procedure opens its definition in an editor tab, wrapped in `DELIMITER $$`. Running it creates the new body under the temporary name `<name>__webtuidb_new`, and drops the old routine only once that worked. MySQL cannot rename routines, so the routine keeps the temporary name until you change it back to the original name in the tab and run it again.
- **Pin connection to this tab** keeps one connection for the tab, so `START TRANSACTION`, `SET @x` and temporary tables carry over. If that connection drops, the next statement fails with `the pinned connection was lost (its transaction is gone); close and pin again` instead of quietly running outside your transaction. While a tab is pinned, a structure change is refused (`structure changes are DDL, which MySQL commits implicitly together with your open transaction: release the pin first`). Grid commits on the pin run inside your transaction without a `BEGIN` of their own.
- {{< key "C-d X" >}} runs `EXPLAIN FORMAT=JSON` and shows the plan as a tree.

## Safe mode

The read-only test follows MySQL's lexer. Backslash escapes are honoured, `#` and `-- ` start comments, and `/*! … */` is executable code, not a comment. So `SELECT 1 /*! ; DROP TABLE t */` is a write. `SELECT`, `SHOW`, `DESCRIBE`, `EXPLAIN` and `WITH` without a write keyword are reads. A `SELECT` that calls `NEXTVAL()`, `SETVAL()` or `NEXT VALUE FOR` counts as a write, because it moves a MariaDB sequence.

## Tools

- **Process list**: `SHOW FULL PROCESSLIST`. Kill sends `KILL QUERY <id>`, which stops the statement and keeps the connection. Refused on `read_only`.
- **Users**: accounts with host, global privileges, database and table grants (parsed from `SHOW GRANTS`), and the limits `MAX_QUERIES_PER_HOUR`, `MAX_UPDATES_PER_HOUR`, `MAX_CONNECTIONS_PER_HOUR` and `MAX_USER_CONNECTIONS`.
- **Backup / restore**: `mysqldump` and `mysql`.
- **Import**: on-conflict `update` and `replace` both become `INSERT … ON DUPLICATE KEY UPDATE` over the mapped columns. MySQL's `REPLACE` would delete the row first, wiping unmapped columns and firing cascades. The rows of a file go into one transaction. A new table is created before the transaction opens, because MySQL commits `CREATE TABLE`: after a failure the error ends with `(rolled back; the new table stays, MySQL commits CREATE TABLE)`.

## Limits

- **Users** reads `SHOW GRANTS` once per account and skips roles.
- A truncated result on a pinned connection is drained to the end rather than cancelled, since cancelling would end the transaction.
