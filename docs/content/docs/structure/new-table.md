---
title: Create a table
linkTitle: New table
description: Design a new table column by column, preview the CREATE TABLE and send it, in the terminal and in the browser.
weight: 30
---

The table designer is the [structure editor](../structure-editor/) started on an empty table: you name the table, add
columns, keys and indexes, preview the `CREATE TABLE` the engine plans, and send it. Nothing exists on the server until
you send it, and safe mode applies.

{{< ui tui="structure-new-table" web="structure-new-table" >}}A new table `docs_structure_invoices` in the designer: an `id` primary key to start from, then the columns you add.{{< /ui >}}

## How to open it

| | Terminal | Web |
|---|---|---|
| From the sidebar | {{< key "N" >}} with the sidebar focused, or {{< key "+" >}} then **New table…** | the **+** button next to the filter box ("Create a database, schema or table"), **New table…** |
| From an object's menu | {{< key "m" >}} on any object, **New table…** | right click any object, **New table…** |
| From Open Anything | {{< key "C-d p" >}}, the entry `New… (table, view, function, schema, database, group)`, then **New table…** | (no palette entry: use the **+** button) |

Both UIs first ask for the name: the terminal's **New table** form has one field, **Table name**; the web asks
`New table name`. An empty name is refused (`a table needs a name`). The table is created in the schema the sidebar
shows (PostgreSQL) or the database that is open.

{{< ui tui="structure-new-table-name" web="structure-new-table-name" >}}The name comes first.{{< /ui >}}

Then:

- **Terminal**: a new table tab opens in the Structure view with the line
  `+ new table docs_structure_invoices · add columns with the add-row key, then commit to create it` and one column:
  `id`, the engine's first type (`integer` on PostgreSQL), `NO` for nullable, `YES` for primary key.
- **Web**: a dialog titled `New table docs_structure_invoices` holds the designer: the Overview with the name (still
  editable), the Columns table starting with the same `id` row, **Add column**, and the Indexes and Foreign keys sections
  where the engine has them. The line above says `Describe the table, then preview the statements.`

## What you can do

Everything the [structure editor](../structure-editor/) does applies: add columns ({{< key "i" >}} / **Add column**),
name and type them (the type picker {{< key "C-d M-Enter" >}} in the terminal, suggestions in the web), set nullability,
defaults and the primary key, add foreign keys and indexes (the terminal's Index view {{< key "C-d M-i" >}} works in the
new tab too). On engines that can place columns the order is simply the order of the rows.

| | Terminal | Web |
|---|---|---|
| Preview the CREATE | {{< key "C-d P" >}} | **Preview CREATE** |
| Create it | {{< key "C-d s" >}}, or {{< key "Enter" >}} in the preview | **Send changes** in the send dialog; **Cancel** goes back to the designer with your draft |
| Give up | close the tab ({{< key "C-d w" >}}); it counts as unsaved, so you are asked first | close the dialog; once you edited past the first column it asks `Discard the new table?` |

{{< ui tui="structure-new-table-preview" web="structure-new-table-preview" >}}The planned CREATE TABLE before it is sent.{{< /ui >}}

After the table is created the terminal tab turns into a normal table tab (the structure and rows load, the sidebar
refreshes, the status line says `structure of docs_structure_invoices saved`); the web closes the dialog, refreshes the
sidebar and says `Changes sent`. Open the table from the sidebar.

In the web, a read-only connection leaves **New table…** out of the menus.

## What the CREATE looks like

{{< tabs items="PostgreSQL,MySQL / MariaDB,ClickHouse,Cassandra,DynamoDB,MongoDB and Redis" >}}
{{< tab >}}
```sql
CREATE TABLE "public"."docs_structure_invoices" (
  "id" serial NOT NULL,
  "customer_id" integer NOT NULL,
  "amount" numeric(10,2) DEFAULT 0 NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_inv_customer" FOREIGN KEY ("customer_id") REFERENCES "public"."customers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
)
```
Columns, then `PRIMARY KEY`, then the foreign keys inside the statement; each index is its own `CREATE INDEX` after it.
A column typed `serial` / `bigserial` gets its own sequence. Greenplum, Vertica and Redshift plan the same way.
{{< /tab >}}
{{< tab >}}
One `CREATE TABLE` with the columns (`NULL` / `NOT NULL`, `DEFAULT`, extra such as `AUTO_INCREMENT`), `PRIMARY KEY`, the
indexes (`UNIQUE INDEX`, `FULLTEXT INDEX`, `SPATIAL INDEX`) and the foreign keys inline. Non-literal defaults are put in
parentheses.
{{< /tab >}}
{{< tab >}}
```sql
CREATE TABLE `analytics`.`docs_structure_events` (
  `id` UInt64,
  `name` Nullable(String)
) ENGINE = MergeTree ORDER BY (`id`)
```
The engine clause is always `ENGINE = MergeTree`; the primary key columns become `ORDER BY`, or `ORDER BY tuple()`
when no column is ticked. Change the engine by editing the statement in a query tab (copy it from the preview).
Ticking Null wraps the type in `Nullable(…)`. Indexes and foreign keys are refused
(`ClickHouse indexes cannot be created here`, `ClickHouse has no foreign keys`).
{{< /tab >}}
{{< tab >}}
The table goes into the keyspace that is open. It needs a primary key
(`a Cassandra table needs a primary key: mark the partition key column first, then any clustering columns`). Tick the
key columns in order. By default the first is the partition key and the rest are clustering columns; to say otherwise
write `partition key` or `clustering asc` / `clustering desc` in a column's `extra` (and `static` for a static column).
A descending clustering column adds `WITH CLUSTERING ORDER BY (…)`. Every index is a `CREATE INDEX` after the table
(one column each; Algorithm `SAI` for a storage-attached index). There are no defaults and no foreign keys.
{{< /tab >}}
{{< tab >}}
A DynamoDB table declares only its keys. Tick one column (the partition key, `HASH`) or two (partition and sort key,
`RANGE`), typed `S`, `N` or `B`. Any other column must be the key of an index you add, or the preview stops with
`column "x" cannot be declared: DynamoDB tables only declare their key attributes (and index keys); other attributes
exist once an item has them`. Indexes are global (`GSI`, the default) or local (`LSI`, only at creation), with the
projection in Condition (`ALL`, `KEYS_ONLY`, `INCLUDE a, b`). The table is created with on-demand billing
(`PAY_PER_REQUEST`). The preview shows the request as readable JSON. In the terminal a new index's Algorithm and
Condition cells are not editable, so local indexes and projections other than `ALL` need the web designer.
{{< /tab >}}
{{< tab >}}
There is no designer. The terminal says `mongo has no table designer; use the editor to create keys/collections`, the
web `mongo has no table designer: create it with a query`. Create a MongoDB collection in a query tab with
`db.createCollection("name")`, or by inserting a first document; a Redis key exists once you write it (`SET`, `HSET`,
`LPUSH`…).
{{< /tab >}}
{{< /tabs >}}

## The "+" menu

{{< ui tui="structure-plus-menu" web="structure-plus-menu" >}}The "+" menu. Entries follow what the engine has: views, materialized views, functions and schemas only where they exist.{{< /ui >}}

Both UIs list the same entries (`menus.SidebarNew`):

| Entry | Shown when | Does |
|---|---|---|
| New table… | always | the table designer |
| New view… | the engine has views | a query tab with a `CREATE VIEW` template |
| New materialized view… | the engine has materialized views | a query tab with a `CREATE MATERIALIZED VIEW` template |
| New function / procedure… | the engine has functions | a query tab with a `CREATE FUNCTION` template |
| New schema… | the engine has schemas | see [Schemas and databases](../schemas-and-databases/#create-a-schema) |
| New database… | always | see [Schemas and databases](../schemas-and-databases/#create-a-database) |
| New item group… | always | a custom sidebar group made from the marked items (see [Workspace](../../workspace/)) |

The view, materialized view and function entries open a template you finish and run; see
[Views, functions and triggers](../views-functions-triggers/#create-a-view-or-function).
