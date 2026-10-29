---
title: Profile drivers
description: Redshift, Greenplum and Vertica on the PostgreSQL driver and SingleStore on the MySQL driver. Which base driver each uses, which capabilities are switched off and why.
weight: 100
---

Four engines speak the wire protocol of another: Redshift, Greenplum and Vertica speak PostgreSQL's, and SingleStore speaks MySQL's. webtuidb connects to them with the [PostgreSQL](../postgresql/) or [MySQL](../mysql/) driver under their own name (`driver = "redshift"`, `"greenplum"`, `"vertica"`, `"singlestore"`). It switches off the capabilities the engine lacks, so the UI does not offer features that would only fail. Everything else, from connection fields and TLS to paging, edits and safe mode, is the base driver's and is described on its page.

{{< ui tui="engines-profiles-vertica" web="engines-profiles-vertica" >}}A new connection with the Vertica driver. The terminal fills in Vertica's default port 5433. In the web an empty port means the same default. AWS IAM auth is offered only for PostgreSQL, MySQL and MariaDB.{{< /ui >}}

| Driver | Base | Default port | Switched off | `EXPLAIN` | Reset between statements |
|---|---|---|---|---|---|
| `redshift` | PostgreSQL | 5439 | triggers, materialized views, indexes, process list | plain text | `RESET ALL` |
| `greenplum` | PostgreSQL | 5432 | nothing | JSON plan tree | `DISCARD ALL` |
| `vertica` | PostgreSQL | 5433 | schemas, views, triggers, materialized views, indexes, foreign keys, functions, sequences, rename database, transactions, process list, users, **catalog browsing** | plain text | none |
| `singlestore` | MySQL | 3306 | triggers, foreign keys | plain text | — |

The status line names the driver. The server version comes from the base protocol, so Redshift, Greenplum and Vertica show "PostgreSQL" followed by the version the server reports.

{{< callout type="info" >}}
No test server for these engines runs in the documentation environment. The behaviour on this page is taken from the driver code, and the screenshots show only the connection form.
{{< /callout >}}

## Redshift

Redshift has no indexes and no `pg_stat_activity` the way PostgreSQL has, and its `EXPLAIN` prints text, not JSON. With those capabilities off:

- The **Index** view and the structure editor's index section are hidden, and so are the Triggers popover and the materialized-views group.
- **Process list** is not offered.
- {{< key "C-d X" >}} runs a plain `EXPLAIN <statement>`. The text plan is shown as a tree by its indentation.
- A connection that ran your SQL is reset with `RESET ALL` (Redshift has no `DISCARD ALL`) before it goes back to the pool.
- Foreign keys stay on: Redshift stores them as informational constraints, so the foreign-key editor and the ER diagram's lines work, but Redshift does not enforce them.

## Greenplum

Greenplum keeps every PostgreSQL capability: schemas, indexes, triggers, materialized views, functions, sequences, users, the process list and the JSON plan tree. The only differences are the name and the defaults.

## Vertica

Vertica speaks the PostgreSQL protocol, but its catalogs are `v_catalog` and `v_monitor`, not `pg_catalog`. Every catalog query of the PostgreSQL driver would fail there, so the Vertica session refuses them up front:

- The sidebar, the table list, a table's columns, its DDL and the database list fail with `Vertica catalog browsing is not implemented; use the SQL editor`.
- Indexes, triggers and foreign keys come back empty instead of failing, and the schema picker is empty.
- The structure editor can **create** a table (that statement is built from the definition alone). Changing an existing table needs the catalog and fails with the same message.
- Of the Copy Script As kinds, the create script is refused. The drop, truncate and select scripts work.

Vertica has no `BEGIN`, so its capabilities say "no transactions": a commit runs its statements one at a time, and the first failure leaves the earlier ones applied. Vertica also has no `DISCARD`. A `SET` you run in the editor stays on that pooled connection and leaks into later statements that happen to use it. **Pin connection to this tab** still works and keeps a session's settings on one connection.

The SQL editor is fully usable. Run `SELECT * FROM v_catalog.tables` to see what the sidebar cannot show.

## SingleStore

SingleStore documents no triggers and no enforced foreign keys, and its `EXPLAIN` is text. With triggers and foreign keys off:

- The Triggers popover, the foreign-key section of the structure editor and the ER diagram's drag-to-link are not offered, and the diagram draws no foreign-key lines.
- {{< key "C-d X" >}} runs a plain `EXPLAIN`.

Everything else, including implicit DDL commits, `DELIMITER`, pinned sessions, users, the process list and import with on-conflict `update`, works as on [MySQL](../mysql/).

## What profiles do not have

- **Backup and restore** know the dump tools of PostgreSQL, MySQL, MariaDB, ClickHouse, MongoDB and Redis only. On a profile driver they answer `backup is not supported for redshift`.
- **Import on conflict**: `update` and `replace` need `ON CONFLICT`, which Redshift, Greenplum and Vertica lack even though they report the PostgreSQL dialect. On them the import answers `on conflict "update": not supported for postgres`. SingleStore uses MySQL's `ON DUPLICATE KEY UPDATE` and supports it.
- `iam_auth` is hidden in the connection form for the profiles; RDS IAM tokens are an RDS feature.
- `driver_version` is stored with the connection for TablePlus URLs and is not used by any driver.
