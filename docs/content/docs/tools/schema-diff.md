---
title: Schema diff
description: Compare two schemas, of one connection or of two connections with the same SQL dialect, and get the CREATE, ALTER and DROP statements that turn the first into the second, as an editor tab you run yourself (terminal) or a plan you preview and send (web).
weight: 60
---

Schema diff compares the structure of two schemas and writes the SQL that makes the first (FROM) look like the
second (TO): new tables, changed columns, indexes and foreign keys, and tables to drop. Use it to bring a staging
schema in line with production, or to see what a migration would have to do. Nothing is sent until you run or send
the statements yourself.

{{< ui tui="tools-schema-diff" web="tools-schema-diff" >}}Comparing `docs_tools_v1` with `docs_tools_v2` on the demo database.{{< /ui >}}

## How to open it

Schema diff has no default key binding.

| What | Terminal | Web |
| --- | --- | --- |
| Open it | {{< key "C-d p" >}} → **Schema diff** | {{< key "C-d p" >}} → **Schema diff** |
| Bind a key | add `schema_diff = "…"` under `[keys]` in `config.toml` ([key bindings](../../configuration/key-bindings/)) | the same binding applies |

It is for SQL engines; elsewhere the terminal says `schema diff is for SQL engines`.

## The form

| Terminal field | Web field | Meaning |
| --- | --- | --- |
| **FROM schema (the SQL runs here)** | **Change schema** | the schema that the statements change; prefilled with the current schema. Shown only on engines with schemas. |
| **TO is in** | — | `this connection`, or another saved connection of the same SQL family (PostgreSQL / CockroachDB, MySQL / MariaDB, or the same driver) |
| **TO schema** | **to look like** | the schema to copy; in the terminal, empty means that connection's current schema |
| **Include DROP TABLE for tables missing in TO** | — | off (default): the drops are written as comments |

The web compares two schemas of the current connection only: both lists are the connection's schemas, the second
leaves out the first, and **Plan changes** stays disabled until both are picked and differ. On an engine without
schemas the web dialog says `mysql has no schemas to compare.`; the terminal can still compare the current database
with another connection's (for example `Shop (MySQL)` with `Shop (MariaDB)`).

Errors you may meet:

- `pick a schema or a connection to compare with`: terminal, same schema on the same connection (or no schemas and
  no other connection).
- `Shop (MariaDB) is not the same SQL dialect as Shop (Postgres)`: the other connection speaks another dialect.
- `pick two different schemas to compare` and `schema "x" does not exist`: web.
- `timed out after 10 minutes reading the schemas`: terminal, reading both sides took longer than 10 minutes.

## What is compared

For each table on both sides the diff reads the columns (name, type, nullability, default, primary key), the
indexes and the foreign keys, then plans the change through the same planner the
[structure editor](../../structure/structure-editor/) uses:

- tables only in TO are created (a table referenced by another is created first);
- tables in both are altered: added, dropped and changed columns, primary key, indexes, foreign keys;
- tables only in FROM are dropped (a referencing table is dropped before the table it references).

Tables are matched by name. Views, functions, sequences, triggers and other objects are not compared. Foreign keys in
TO that point into TO's own schema are rewritten to point into FROM, so a created table references FROM's tables;
keys into a third schema are kept as they are.

{{< callout type="info" >}}
Engines that lack index or foreign-key metadata skip them on both sides rather than planning a drop for every one:
Redshift has no indexes, SingleStore no foreign keys (nor ClickHouse). Their diff covers tables and columns, and keys
where they exist.
{{< /callout >}}

## The result

{{< ui tui="tools-schema-diff-result" web="tools-schema-diff-result" >}}The statements that turn `docs_tools_v1` into `docs_tools_v2`: the terminal writes them to a new editor tab, the web opens them in the send dialog.{{< /ui >}}

**Terminal.** The statements open in a new SQL editor tab, nothing is sent. The first line is a header:

```sql
-- schema diff: 16 statements; 2 DROP TABLEs are commented out below (tables only in FROM): remove the leading -- to drop them
```

Then the creates and alters, each ending with `;`, then the drops: commented out (`-- DROP TABLE …;`) unless you
ticked **Include DROP TABLE…**, in which case the header says `plus 2 DROP TABLEs: they DELETE DATA` and the drops are
live. Run the tab like any other ({{< key "C-d M-Enter" >}} runs all statements), edit it first, or save it as a
migration file. The status line counts the drops too, commented or not (`schema diff: 18 statements written to a
new editor tab` for the header above), or says `the schemas are the same`.

**Web.** **Plan changes** returns the plan and opens the usual **Send n statements?** dialog (`Send 18 statements?`
for the same two schemas). The drops are part of the plan, as live statements at the end. **Send changes** runs them against FROM through [safe mode](../../safety/); **Open in window** shows them in a
larger view; **Cancel** sends nothing. When nothing differs the dialog says
`No differences: the two schemas have the same structure.`

{{< callout type="warning" >}}
The web plan includes `DROP TABLE` for every table that exists only in the first schema, with its data. Read the plan
before **Send changes**; the terminal comments these out unless you ask for them.
{{< /callout >}}

{{< callout type="warning" >}}
On PostgreSQL (and the engines that share its column loader) the current build plans every column of a table that
exists on both sides as a `DROP COLUMN` followed by an `ADD COLUMN`, even when the column is unchanged: in the
screenshots `customers` gets `DROP COLUMN "id"`, `"email"`, `"name"` and then adds them back, which empties those
columns and loses the primary key. Do not run such a plan as it is: delete the drop/add pairs of unchanged columns
in the terminal's editor tab and keep only the real changes (here `ADD COLUMN "country"` and the unique index). MySQL
and MariaDB plans compare columns by name and are not affected.
{{< /callout >}}

## Progress and cancelling

The terminal runs the diff as a background job and reports `schema diff: reading FROM, table 3 of 9` while it reads;
it is listed in the jobs centre ({{< key "C-d M-J" >}}) and {{< key "C-d ." >}} or **Stop** cancels it between two
tables. The other connection is opened for the diff and closed when it ends. The web plans the diff in one request
(no progress, no cancel).

## Details and limits

- Drops in the generated SQL quote identifiers for the engine (`"x"` on PostgreSQL, `` `x` `` on MySQL).
- A table renamed between the two schemas shows up as one drop and one create.
