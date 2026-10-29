---
title: Capability matrix
description: Which webtuidb features work on which database engine, in the terminal and in the browser, and why some are missing.
weight: 10
---

webtuidb runs one engine for both front ends, so a feature works the same in the terminal and in the browser. What varies is the database. Each driver declares a set of capabilities, and both UIs use them to hide what an engine cannot do: a sidebar group, a structure tab, a menu entry. This page lists them side by side. The engine pages explain each entry in detail.

Twelve drivers are compiled in. Eight talk to their own server. Four are *profiles*: the PostgreSQL or MySQL driver under another name, with some capabilities switched off (see [Profile drivers](../profiles/)).

## Data and objects

✓ supported · – not offered · notes in brackets.

| Feature | PostgreSQL | MySQL | MariaDB | ClickHouse | Cassandra | DynamoDB | Redis | MongoDB |
|---|---|---|---|---|---|---|---|---|
| Browse rows in a grid | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ (Query or Scan) | ✓ (one key at a time) | ✓ |
| Edit rows (pending changes, preview, commit) | ✓ | ✓ | ✓ | ✓ (mutations) | ✓ (full key needed) | ✓ | ✓ (not streams or JSON keys) | ✓ |
| Json and Tree views ({{< key "C-d J" >}} {{< key "C-d T" >}}) | – | – | – | – | – | ✓ | ✓ | ✓ |
| Structure editor (columns) | ✓ | ✓ (reorder too) | ✓ (reorder too) | ✓ (reorder too) | ✓ (add, drop, rename key columns) | key attributes only | – | – |
| Index editor | ✓ | ✓ | ✓ | – | ✓ (one column, SAI) | ✓ (GSI; LSI at create) | – | read-only list |
| Foreign key editor | ✓ | ✓ | ✓ | – | – | – | – | – |
| Triggers | ✓ | ✓ | ✓ | – | ✓ (Java classes) | – | – | – |
| Views | ✓ | ✓ | ✓ | ✓ | – | – | – | ✓ |
| Materialized views | ✓ | – | – | ✓ | ✓ | – | – | – |
| Functions | ✓ | ✓ | ✓ | – | ✓ (and aggregates) | – | – | – |
| Procedures | ✓ | ✓ | ✓ | – | – | – | – | – |
| Sequences | ✓ | – | ✓ | – | – | – | – | – |
| Types | ✓ (enum, domain, composite) | – | – | – | ✓ (user types) | – | – | – |
| Schemas | ✓ | – | – | – | – | – | – | – |
| Switch database ({{< key "C-d k" >}}) | database | database | database | database | keyspace | region | db index | database |
| Create / drop database | ✓ / ✓ | ✓ / ✓ | ✓ / ✓ | ✓ / ✓ | ✓ / ✓ (keyspace) | – | – / FLUSHDB | ✓ / ✓ |
| Rename database | ✓ | – | – | ✓ (Atomic only) | – | – | – | – |

| Feature | Redshift | Greenplum | Vertica | SingleStore |
|---|---|---|---|---|
| Based on | PostgreSQL | PostgreSQL | PostgreSQL | MySQL |
| Browse and edit rows | ✓ | ✓ | SQL editor only | ✓ |
| Structure editor | ✓ | ✓ | create only | ✓ |
| Index editor and Index tab | – | ✓ | – | ✓ |
| Foreign key editor | ✓ | ✓ | – | – |
| Triggers | – | ✓ | – | – |
| Views / materialized views | ✓ / – | ✓ / ✓ | – / – | ✓ / – |
| Functions, procedures | ✓ | ✓ | – | ✓ |
| Sequences, types | ✓ | ✓ | – | – |
| Schemas | ✓ | ✓ | – | – |

## Tools

| Feature | PostgreSQL | MySQL | MariaDB | ClickHouse | Cassandra | DynamoDB | Redis | MongoDB |
|---|---|---|---|---|---|---|---|---|
| Process list ({{< key "C-d ." >}}) | ✓ | ✓ | ✓ | ✓ | – | – | ✓ (clients) | ✓ (currentOp) |
| User management ({{< key "C-d u" >}}) | ✓ | ✓ | ✓ | – | – | – | – | – |
| Backup ({{< key "C-d b" >}}) | `pg_dump` | `mysqldump` | `mariadb-dump` | server `BACKUP` | – | – | `redis-cli --rdb` | `mongodump` |
| Restore ({{< key "C-d B" >}}) | `pg_restore` / `psql` | `mysql` | `mariadb` | server `RESTORE` | – | – | – (copy the RDB file) | `mongorestore` |
| Explain ({{< key "C-d X" >}}) | JSON plan tree | JSON plan tree | JSON plan tree | text plan | – | – | – | – |
| Import | ✓ (on conflict update) | ✓ (on conflict update) | ✓ (on conflict update) | ✓ | ✓ | ✓ | ✓ | ✓ |
| Export | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Search in database ({{< key "C-d /" >}}) | ✓ (all schemas too) | ✓ | ✓ | ✓ | – (no any-column filter) | ✓ (Scan) | ✓ | ✓ |
| ER diagram ({{< key "C-d d" >}}) | ✓ with FK lines | ✓ with FK lines | ✓ with FK lines | tables only | tables only | tables only | every key as a table | collections only |
| Schema diff | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | – | – |

| Feature | Redshift | Greenplum | Vertica | SingleStore |
|---|---|---|---|---|
| Process list | – | ✓ | – | ✓ |
| User management | ✓ | ✓ | – | ✓ |
| Backup / restore | – | – | – | – |
| Explain | text plan | JSON plan tree | text plan | text plan |
| Import on conflict update | – | – | – | ✓ |

## Safety and transport

| Feature | PostgreSQL | MySQL / MariaDB | ClickHouse | Cassandra | DynamoDB | Redis | MongoDB |
|---|---|---|---|---|---|---|---|
| Commit is atomic | ✓ (one transaction) | ✓ (unless the batch has DDL) | – (one statement at a time) | logged batch for up to 100 DML statements | ✓ (ExecuteTransaction, up to 100) | ✓ (MULTI/EXEC under WATCH) | – (one statement at a time) |
| Pinned editor session (`BEGIN` carries over) | ✓ | ✓ | – | – | – | – | – |
| `read_only` enforced by the server | ✓ | ✓ | – | – | – | – | – |
| `read_only` checked by webtuidb | both UIs | both UIs | web | web | web | web | web |
| Safe mode classifier | SQL | SQL (`/*! */` is code) | SQL | CQL allow-list | PartiQL: only `SELECT` | command allow-list | shell-method allow-list |
| `ssl_mode = prefer` | TLS if offered | TLS if offered, else plain | plain | TLS, no fallback | `https` custom endpoint | plain | plain |
| `verify-identity` | error | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| SSH tunnel and SOCKS5 proxy | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| AWS IAM auth (`iam_auth`) | ✓ | ✓ | – | – | AWS credentials | – | – |

The profile drivers inherit the transport column of their base driver. Vertica has no transactions, so its commits run statement by statement.

{{< callout type="warning" >}}
`read_only = true` is enforced in three layers. PostgreSQL and MySQL-family servers refuse writes themselves. The drivers refuse non-read-only statements before sending them. The web server checks every batch for all engines. On ClickHouse, Cassandra, DynamoDB, Redis and MongoDB only the web check exists: the terminal UI sends such writes (it still asks first if safe mode says so). Do not rely on `read_only` alone for those engines in the terminal; use a database login that cannot write.
{{< /callout >}}

## How the UIs use capabilities

- The sidebar shows a group (Views, Materialized views, Functions, Procedures, Sequences, Types) only when the engine has it; `[sidebar] show_functions`, `show_sequences` and `show_types` hide them on engines that do.
- The table tab shows the **Index** view ({{< key "C-d M-i" >}}) and the structure editor's index and foreign key sections only where the engine has indexes and foreign keys.
- A context-menu entry with a `cap:` condition (for example **Rename database**) appears only where it holds. The menus come from one list shared by both UIs, so the terminal pickers and the web right-click menus always match.
- **User management** and **Explain** answer with a short message where the engine lacks them: "explain is not available for cassandra", "user management is not supported by redis".
- The "database" word changes with the engine: **keyspace** on Cassandra, **region** on DynamoDB, **db index** on Redis.

## Filter operators per engine

The filter bar ({{< key "C-d f" >}}) offers the operators the engine can run:

| Dialect | Operators |
|---|---|
| PostgreSQL family | `=` `<>` `<` `>` `<=` `>=` `IN` `NOT IN` `IS NULL` `IS NOT NULL` `BETWEEN` `NOT BETWEEN` `LIKE` `ILIKE` Contains, Not contains, Has prefix, Has suffix |
| MySQL family, ClickHouse, MongoDB, Redis | the same without `ILIKE` |
| Cassandra (CQL) | `=` `<` `>` `<=` `>=` `IN` `LIKE` |
| DynamoDB (PartiQL) | everything except `LIKE`, `ILIKE` and Has suffix |

Contains, Has prefix and Has suffix are case-insensitive on PostgreSQL (`ILIKE`) and MongoDB (a case-insensitive regex). On MySQL they follow the column's collation. On ClickHouse and Redis they are case-sensitive.

{{< callout type="info" >}}
Redis evaluates filters in memory and supports only `=`, `<>`, `LIKE`, Contains, Not contains, Has prefix and Has suffix. The other operators appear in the list but fail with `filter operator "<" is not supported for redis`.
{{< /callout >}}
