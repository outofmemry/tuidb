---
title: Cassandra
description: What webtuidb supports on Apache Cassandra, from keyspaces and driver options to partition and clustering keys, ALLOW FILTERING, counters, logged batches and CQL safe mode.
weight: 60
---

Cassandra (`driver = "cassandra"`) uses the Apache gocql driver and CQL over the native protocol. A "database" is a **keyspace**. You can browse and edit tables, browse materialized views, list functions, aggregates and user types, create and drop keyspaces, edit a table's columns and SAI indexes, and run CQL. Cassandra has no joins, no foreign keys, no transactions and no rename, so webtuidb plans only what CQL can do. It refuses the rest with a message that says what to do instead.

{{< ui tui="engines-cassandra" web="engines-cassandra" >}}The structure of `events` in the `webtuidb` keyspace on Cassandra 5.0: `tenant` is the partition key, `ts` a clustering column in ascending order, `tenant_name` a static column. The sidebar shows materialized views, functions (aggregates are marked) and user types; the `~` counts are partition estimates.{{< /ui >}}

## Connecting

| Field | Default | Meaning on Cassandra |
|---|---|---|
| `host` / `port` | `127.0.0.1` / `9042` | One contact point; the driver discovers the other nodes from it. |
| `user` / `password` | — | Password authentication when set. |
| `database` | `options.keyspace`, else none | The keyspace to open. Without one, objects and rows say `no keyspace selected: open one first`; pick a keyspace with {{< key "C-d k" >}}. |
| `ssl_mode`, `ssl_ca`, `ssl_cert`, `ssl_key` | plain text | See below. |
| `bootstrap` | — | `USE ks` picks the keyspace and `CONSISTENCY level` the default consistency. Anything else runs once, right after connecting. |

`options` holds the driver settings. Any other key is an error: `unknown cassandra option "x" (want consistency, local_dc, keyspace, timeout, protocol_version)`.

| Option | Default | Meaning |
|---|---|---|
| `consistency` | `LOCAL_QUORUM` | Consistency level for every query: `ONE`, `QUORUM`, `LOCAL_ONE`, `ALL` and the other CQL levels. |
| `local_dc` | — | Prefer this datacenter (token-aware, DC-aware round robin). |
| `keyspace` | — | Keyspace when `database` is empty. |
| `timeout` | the driver's | Per-request timeout as a Go duration, such as `10s`. |
| `protocol_version` | negotiated | Force a native protocol version, such as `4`. |

The connect timeout is 10 seconds. The status line shows `Cassandra <release_version>` from `system.local`.

### TLS

| `ssl_mode` | Behaviour |
|---|---|
| empty, `disable` | Plain text. |
| `prefer`, `require` | TLS, certificate not checked. There is no fallback: a node without TLS fails to connect. |
| `verify-ca` | TLS, chain checked against `ssl_ca`, host name not checked. |
| `verify-full`, `verify-identity` | TLS, chain and host name checked. |

## Sidebar

Tables, materialized views, functions and aggregates (listed as `name(arg types)`, aggregates marked `aggregate`), and user-defined types of the current keyspace. Tables and views show `~N`: the estimated **partition** count from `system.size_estimates`, which Cassandra refreshes every few minutes.

## Browsing, paging and counting

Rows come from `SELECT … LIMIT n`, with double-quoted identifiers and `?` parameters. CQL has no `OFFSET`: page 3 reads the rows of pages 1 and 2 and drops them, so deep pages cost as much as everything before them. Sort works only on clustering columns inside one partition, and the server's error says so.

Filters must suit CQL:

- Operators: `=`, `<`, `>`, `<=`, `>=`, `IN` and `LIKE` (`LIKE` needs an SAI or SASI index). There is no `<>`, `IS NULL`, `BETWEEN`, Contains or prefix test.
- **Match any** (OR) with more than one filter is refused: `CQL has no OR: use one filter, or an IN filter`.
- The **Any column** filter is refused: `CQL cannot search every column at once: pick a column for the filter`. For the same reason, search in database reports every Cassandra table as an error.
- When no key or index covers the filter, the server wants `ALLOW FILTERING`. webtuidb adds it and runs the query again, and the result message says so: `ALLOW FILTERING was added: no key or index covers this filter, so Cassandra reads the rows and filters them server-side (slow on big tables)`.
- Filter values are converted to the column's CQL type first. A bad value fails before anything is sent: `filter on id: "x" is not a uuid`.

The unfiltered count is the partition estimate. When that is 0, and for every filtered count, `SELECT COUNT(*)` runs. It reads the whole table, or every matching row.

## How edits become statements

Values are bound in their CQL types. Collections, tuples, user types and vectors are written with `fromJson(?)`, so the JSON text the grid shows can be edited and sent back. **Preview** shows the values inlined.

- Insert: `INSERT INTO "ks"."events" ("tenant", "ts", "kind") VALUES (?, ?, ?)`. NULL and DEFAULT cells are left out; CQL has no column defaults. A counter column cannot be inserted: `cannot insert into counter column hits: counters are only changed with UPDATE`.
- Update: `UPDATE … SET "kind" = ? WHERE "tenant" = ? AND "ts" = ?`. The full primary key is required, and a key column cannot change in place: `primary key column ts of events cannot be changed in place: insert a new row and delete the old one`.
- Counter columns change by a delta: enter `+5` or `-3`, which becomes `"hits" = "hits" + ?`. A plain number is refused: `counter column hits can only be incremented: enter +5 or -3 (Cassandra cannot set a counter to a value)`.
- Delete: `DELETE FROM … WHERE <full primary key>`.
- The grid's `NOW()` becomes `toTimestamp(now())` for a `timestamp` column and `toDate(now())` for a `date`.

Cassandra has no transactions. When a commit holds 2 to 100 `INSERT`, `UPDATE` and `DELETE` statements, and none is a counter update, they go in one **LOGGED BATCH**, which the cluster applies all or nothing. A failure says `(a logged batch is all-or-nothing, nothing was applied)`. A timeout says `the batch may still be applied, verify the data before retrying`. Any other commit runs statement by statement and stops at the first error: `(2 of 5 statements were already applied and stay applied: Cassandra cannot roll back)`. A delete that shares a table with another statement of the same commit always runs one by one. A batch has a single write timestamp, so the delete would swallow the insert.

## Structure

Each column's Extra shows its role: `partition key`, `clustering asc` or `clustering desc`, or `static`. The structure editor can add a column (optionally `STATIC`), drop a regular column, rename a primary key column, and create or drop indexes by name. Anything else is refused with the reason:

| Change | Answer |
|---|---|
| Change a column type | `Cassandra cannot change a column's type (int to bigint): add a new column, copy the data, drop the old one` |
| Change key membership, NOT NULL or static | `Cassandra cannot change a column's key membership, NOT NULL or static setting` |
| Change the primary key, add or drop a key column | `Cassandra cannot change the primary key: create a new table and copy the data` |
| Rename a regular column | `Cassandra can only rename primary key columns` |
| Set a default | `Cassandra has no column defaults` |
| Rename the table | `Cassandra cannot rename a table: create a new one and copy the data` |
| An index on several columns | `Cassandra indexes exactly one column`; SASI is not offered, use SAI |

A new table needs a partition key: mark the partition key column first, then any clustering columns. **DDL** uses the server's `DESCRIBE`. On servers without it, the statement is rebuilt from `system_schema` and ends with `-- Rebuilt from system_schema because DESCRIBE is unavailable: table options and indexes are missing.` Functions and aggregates are rebuilt from `system_schema` too.

| Operation | Statement or answer |
|---|---|
| Create keyspace | `CREATE KEYSPACE "k" WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}`. Options: `strategy` (`SimpleStrategy`, default, or `NetworkTopologyStrategy`), `replication` (a factor, or `dc1:3,dc2:2`), `durable_writes` |
| Drop keyspace | `DROP KEYSPACE "k"` |
| Rename keyspace | refused: create a new one and copy the tables |
| Truncate | `TRUNCATE "ks"."t"` (no cascade) |
| Clone | the structure only, from `DESCRIBE` without its indexes (index names are keyspace-wide). With data: `Cassandra cannot copy a table's rows in one statement: clone the structure, then copy the rows with cqlsh COPY or a script` |
| Rename any object | refused |
| Triggers | A trigger is a Java class on the server. The trigger editor writes `CREATE TRIGGER t ON tbl USING 'com.example.MyTrigger'` |

## SQL editor

- `USE ks` (quoted names keep their case, bare ones fold to lower case) reopens the session on that keyspace: `Keyspace changed`.
- `CONSISTENCY QUORUM` is a cqlsh command the server does not know. webtuidb handles it and answers `Consistency level is now QUORUM`.
- `BEGIN BATCH … APPLY BATCH` is one statement, even though it contains `;`.
- Comments may be `--`, `//` or `/* */`; strings are single-quoted without backslash escapes, and `$$ … $$` is a string.
- Server warnings are added to the result message. Explain is not available.

## Safe mode

CQL is classified with an allow-list. Only `SELECT`, `DESCRIBE`/`DESC` and `LIST` are read-only. Everything else asks under Alert Mode 2 and Safe Mode 2, including `USE`, `BEGIN BATCH` and every DDL statement.

## Limits

- No process list, user management, backup, plain views or explain.
- In the web UI, paging an editor result further (the next page of a `SELECT`) fails with `line 1:14 no viable alternative at input '('`: the page is requested as a subquery, which CQL does not have. Raise the editor's row limit ({{< key "C-d L" >}}) instead. The terminal re-runs the statement with a doubled limit and is not affected.
- Two inserts of the same key in one commit tie on the batch timestamp. The grid never produces this.
- In a user type value, a zero number or zero UUID cannot be told apart from NULL and stays as the value.
