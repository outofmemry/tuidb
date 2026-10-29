---
title: Explain
description: Show the query plan of the statement under the cursor as a tree, with the planner's cost and row estimates, on PostgreSQL, MySQL, ClickHouse and more.
weight: 90
---

Explain asks the server how it would run a statement, without running it, and shows the plan as an
indented tree: each step, the table or index it reads, and the planner's cost and row estimate. Use it
to see whether a query scans a whole table or uses an index before you run it on real data.

{{< ui tui="results-explain" web="results-explain" >}}The plan of the "Top customers" query on PostgreSQL: a sort over a hash aggregate over a hash join.{{< /ui >}}

## How to open it

| Terminal | Web |
| --- | --- |
| {{< key "C-d X" >}} (`explain`) in the editor | the **Explain** button on the query toolbar, or {{< key "C-d X" >}} |

It explains the selection when there is one, else the statement under the cursor.

## What the tree shows

Each line is one step of the plan, indented under the step that consumes its rows, with `└` before
nested steps. On the right:

| Column | Meaning |
| --- | --- |
| `cost` | the planner's estimated total cost of the step (PostgreSQL: `Total Cost`; MySQL: `query_cost`, `prefix_cost` or `read_cost`) |
| `rows` | the planner's row estimate (PostgreSQL: `Plan Rows`; MySQL: `rows_produced_per_join` or `rows_examined_per_scan`) |

These are estimates from table statistics, not measurements: Explain never runs `EXPLAIN ANALYZE`.

The browser dialog also shows the exact statement it sent, for example
`EXPLAIN (FORMAT JSON, ANALYZE false) SELECT …` (comments that lead the statement, such as `-- name:`,
are sent too), says `Planning…` while it waits, and `The plan is empty.` when there are no steps. Close
it with **Close** or {{< key "Esc" >}}. The terminal shows the tree in an **Explain** box that scrolls
with {{< key "Up" >}} {{< key "Down" >}} {{< key "PgUp" >}} {{< key "PgDn" >}} and closes with
{{< key "Esc" >}} or {{< key "q" >}}. With no statement under the cursor the terminal says
`nothing to explain`.

## Engines

| Engine | Statement sent | Tree |
| --- | --- | --- |
| PostgreSQL, Greenplum | `EXPLAIN (FORMAT JSON, ANALYZE false) …` | node type, `using <index>`, `on <table>`, cost and rows |
| MySQL, MariaDB | `EXPLAIN FORMAT=JSON …` | one line per JSON object: `table orders (ALL)` with the access type, or the block name (`query_block`, `nested_loop`, `ordering_operation`…), cost and rows |
| ClickHouse | `EXPLAIN …` | the plain-text plan, one step per line |
| Redshift, Vertica, SingleStore | `EXPLAIN …` | the plain-text plan, one step per line |
| Cassandra, DynamoDB, Redis, MongoDB | none | not available |

For a plain-text plan each line becomes a step and its leading spaces (two per level) give the depth;
there are no separate `cost` and `rows` columns, the numbers stay in the line's own text.

{{< ui tui="results-explain-mysql" web="results-explain-mysql" >}}The same kind of query on "Shop (MySQL)": one line per JSON block, tables with their access type.{{< /ui >}}

{{< callout type="info" >}}
On an engine without EXPLAIN the terminal says `explain is not available for redis` and the browser
`EXPLAIN is not supported by cassandra`. The browser shows the **Explain** button on every SQL editor, so
on Cassandra and DynamoDB it is there but answers with that message.
{{< /callout >}}

## Details and limits

- Explain plans one statement. The browser refuses a selection holding several with
  `explain needs exactly one statement`, and `no statement at the cursor` when the cursor is on nothing.
- It only plans, so safe mode never asks, even for an `UPDATE` or `DELETE`.
- An error from the server (a typo, a missing table) is shown instead of the tree; a plan the parser
  cannot read says `explain: not a PostgreSQL JSON plan` (or MySQL), and an empty answer
  `explain: empty plan`.
- The terminal runs the EXPLAIN on the tab's own connection when the tab is pinned (so temporary tables
  are visible) and writes it to the [history](../history/); the browser plans on the shared pool and does
  not log it.
