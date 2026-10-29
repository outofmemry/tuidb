---
title: Structure, Index and DDL views
linkTitle: Structure view
description: See a table's columns, keys, indexes, triggers and CREATE script, in the terminal and in the browser.
weight: 10
---

Every table tab has more than its rows. The **Structure** view lists the columns with their type, nullability,
default, primary key, extra attributes and foreign key; the **Index** view lists the indexes; the **DDL** view shows
the `CREATE` script the server would need to rebuild the table. The same views are where you change a table: see
[Edit a table's structure](../structure-editor/).

{{< ui tui="structure-view" web="structure-view" >}}The Structure view of `orders`. The terminal shows one grid with a rail of marks on the left; the browser shows an overview, then the columns, indexes, foreign keys and triggers as editable tables.{{< /ui >}}

## How to open it

| What | Terminal | Web |
|---|---|---|
| Data view (rows) | {{< key "C-d (" >}} | {{< key "C-d (" >}}, or **Data** in the view selector of the table toolbar |
| Structure view | {{< key "C-d )" >}} | {{< key "C-d )" >}}, or **Structure** in the view selector |
| Index view | {{< key "C-d M-i" >}} | {{< key "C-d M-i" >}} opens the Structure view and scrolls to **Indexes** |
| DDL view | {{< key "C-d M-d" >}} | {{< key "C-d M-d" >}}, or **DDL** in the view selector |
| Open a table straight in Structure | sidebar menu ({{< key "m" >}}) **Open structure** | right click the table in the sidebar, **Open structure** |

The actions are `show_data`, `show_structure`, `show_index` and `show_ddl`. They are global keys, so they work while
the sidebar has the focus too, and act on the open table tab. The obvious keys (`C-[`, `C-i`, `C-d`) cannot be bound
in a terminal (they are Esc, Tab and the prefix itself), which is why the views use `(`, `)`, `M-i` and `M-d`.

The terminal shows the view names along the bottom of the tab, with their keys:
`Data C-d (   Structure   Index C-d M-i   DDL C-d M-d`. The active one is highlighted. On MongoDB and Redis two more
views follow, **Json** and **Tree** (see [Browsing data](../../browsing-data/)).

The web has no separate Index view: the Structure page holds every section (Overview, Columns, Indexes, Foreign keys,
Triggers), and {{< key "C-d M-i" >}} or the sidebar's **Triggers…** entry scroll it to the right section.

## The Structure view

The structure loads in the background the first time you open one of the three views. Columns, indexes, foreign keys
and triggers are fetched in parallel; the DDL arrives separately, so a slow catalog query never holds up the grid. The
terminal shows `loading structure…` meanwhile, and the error in red if the load fails.

### Columns

| Terminal column | Web column | What it shows |
|---|---|---|
| `#` | (row order) | Position of the column in the table, starting at 1. |
| `column_name` | Name | The column name. |
| `data_type` | Type | The type as the server spells it (`integer`, `timestamp with time zone`, `varchar(255)`, `order_status`). On MySQL a collation that differs from the table's is part of the type (`varchar(64) COLLATE utf8mb4_bin`). |
| `is_nullable` | Null | `YES` or `NO`; a checkbox in the web. |
| `column_default` | Default | The default expression (`now()`, `0`, `'new'::order_status`, `nextval('orders_id_seq'::regclass)`). An empty default shows as `NULL` in the terminal and as a dim `none` in the web. |
| `primary_key` | PK | `YES` for every column in the primary key. The terminal also marks those rows with a key symbol `⚷` in the rail. |
| `extra` | Extra | What the engine adds after type and default: `serial`, `GENERATED ALWAYS AS IDENTITY`, `GENERATED ALWAYS AS (expr) STORED` on PostgreSQL; `auto_increment`, `on update CURRENT_TIMESTAMP`, the `GENERATED ALWAYS AS (…) VIRTUAL` clause and `COMMENT '…'` on MySQL and MariaDB; `partition key`, `clustering asc` and `static` on Cassandra. |
| `foreign_key` | (Foreign keys section) | `→ customers(id)  upd:NO ACTION del:RESTRICT` for a column that references another table. The terminal shows this column only on engines with foreign keys. |

There is no separate column for a comment, a collation or a generated expression: they are part of `extra` (comment,
generated clause) or of `data_type` (MySQL collation).

The web's read-only variant of the page (views, materialized views, MongoDB collections, Redis keys) has a **Key**
column (`PK`) and a **References** column (`customers(id)`) instead of the editable inputs, and an Overview with the
column count, primary key, number of foreign keys and indexes, and the row count.

### Overview (web)

The web page starts with an **Overview**: the table name (an input, so you can rename the table, see
[Edit a table's structure](../structure-editor/#rename-the-table)), the kind (`table`, `view`, `matview`) and the row
count (`~1200` when it is the planner's estimate). The terminal has the same facts in **Item overview**, an
[Open Anything](../../workspace/) entry ({{< key "C-d p" >}}, type `overview`): rows, size, then every column with its
flags, indexes and foreign keys.

### Triggers

A table's triggers are listed at the bottom of the web page (name, then timing and event, for example
`orders_audit  AFTER INSERT OR UPDATE OF status`). The terminal lists them at the end of the DDL view, as comments
followed by the trigger body. Editing triggers is covered in
[Views, functions and triggers](../views-functions-triggers/#triggers).

## The Index view

{{< ui tui="structure-index" web="structure-index" >}}The indexes of `orders`. The primary key is not listed: it is edited with the `primary_key` column of the Structure view.{{< /ui >}}

| Terminal column | Web column | What it shows |
|---|---|---|
| `index_name` | Name | The index name. |
| `is_unique` | Unique | `YES` / `NO`, a checkbox in the web. |
| `column_name` | Columns | The indexed columns, comma separated. An expression index shows its expression (`lower(email)`). |
| `algorithm` | Algorithm | The access method: `btree`, `hash`, `gin`, `gist` on PostgreSQL; `BTREE`, `FULLTEXT`, `SPATIAL` on MySQL; `SAI` on Cassandra; `GSI` or `LSI` on DynamoDB. |
| `condition` | Condition | The `WHERE` of a partial index (PostgreSQL), or the projection of a DynamoDB index (`ALL`, `KEYS_ONLY`, `INCLUDE a, b`). |

The primary-key index is left out on purpose. Indexes that back a `UNIQUE` or `EXCLUDE` constraint are listed; changing
one drops and re-adds the constraint (see the editor page).

An engine without indexes (ClickHouse, Redshift) has no Index view: the terminal shows `clickhouse has no Index tab`
and the web leaves the Indexes section out. A table with none shows `no indexes · add one with the add-row key`.

## The DDL view

{{< ui tui="structure-ddl" web="structure-ddl" >}}The DDL view: the CREATE script of `orders`, with its sequence, constraints, indexes and the sequence ownership.{{< /ui >}}

The DDL is built from the catalog, not stored text, so it reflects the table as it is now:

- **PostgreSQL**: `CREATE SEQUENCE IF NOT EXISTS` for each serial column first, then `CREATE TABLE` with every column,
  every constraint (primary key, unique, check, exclude, foreign keys, in that order), `PARTITION BY` for a partitioned
  table, then `CREATE INDEX` for every index that is not a constraint, then `ALTER SEQUENCE … OWNED BY`, the way
  `pg_dump` orders it. A foreign table or a partition is refused with
  `orders_2024 is a partition: a create script is not supported for it`, because a plain `CREATE TABLE` would describe
  a different object.
- **Views** are `CREATE OR REPLACE VIEW … AS …;`; a **materialized view** adds its indexes after the `CREATE`.
- **Functions and procedures** come from `pg_get_functiondef`, one per overload, each ending in `;`.
- **Sequences** are one `CREATE SEQUENCE … AS … INCREMENT BY … MINVALUE … MAXVALUE … START WITH … CACHE … [NO] CYCLE;`.
- **Types**: an enum is `CREATE TYPE … AS ENUM (…)`, a composite type `CREATE TYPE … AS (…)`, a domain `CREATE DOMAIN`.
- **MySQL, MariaDB, ClickHouse, Cassandra** show what the server's own `SHOW CREATE` / `DESCRIBE` returns.

In the terminal the DDL view is a read-only code pane with line numbers and syntax highlighting, and the table's
triggers appended as `-- trigger name: TIMING EVENT` comments followed by their body. The web shows the same script
without the trigger comments (triggers have their own section on the Structure page).

### Keys in the terminal DDL view

| Key | Does |
|---|---|
| {{< key "j" >}} / {{< key "Down" >}}, {{< key "k" >}} / {{< key "Up" >}} | Scroll one line. |
| {{< key "PgDn" >}} or {{< key "Space" >}}, {{< key "PgUp" >}} | Scroll 15 lines. |
| {{< key "g" >}} / {{< key "Home" >}}, {{< key "G" >}} / {{< key "End" >}} | Top, bottom. |
| {{< key "c" >}} | Copy the whole DDL (`copied DDL`). |
| {{< key "e" >}} | On a view or materialized view: open its definition as runnable SQL in a query tab (see [Views, functions and triggers](../views-functions-triggers/)). On a table it says `tables are changed in the Structure view; e edits views`. |

In the web, select the text and copy it, or use **Copy script: CREATE** from the sidebar menu (see
[Table operations](../table-operations/#copy-scripts)).

## Copy the structure

You can copy the column (or index) definitions as text, in the terminal and in the web:

| | Terminal | Web |
|---|---|---|
| Open it | {{< key "m" >}} on the Structure or Index grid, then **Copy as…** | **Copy as…** button above the Columns table, or right click a row, **Copy as…** |
| Formats | CSV with Header, JSON, Markdown Table, Plain Text, HTML | the seven formats of the Copy as dialog: Plain, JSON, HTML, Markdown Table, CSV, CSV with Header, INSERT Statement |
| Rows | the selected rows ({{< key "S-Down" >}} / {{< key "S-Up" >}} extend a selection), else all | every column that is not marked for deletion |
| What is copied | the grid as it shows: `#`, `column_name`, `data_type`, … (the index columns in the Index view) | Name, Type, Null, Default, PK, Extra |

{{< ui tui="structure-copy-as" web="structure-copy-as" >}}Copy the column definitions as Markdown, JSON or CSV.{{< /ui >}}

{{< key "c" >}} on the terminal grid copies the selected cells as plain text, without opening the picker. The status line
confirms with `copied column definitions` (or `copied index definitions`); the web says `Copied 8 columns as JSON`.

## The column detail line

In the Data view, both UIs tell you what the column under the cursor is, without leaving the rows:

- **Terminal**: the row detail pane ({{< key "C-d Space" >}}) starts with a dim line for the current column, for example
  `customer_id: integer · not null · no default · foreign key → customers`. The parts are type, `nullable` or
  `not null`, `default …` or `no default`, and `primary key`, `foreign key → table` or `no key`.
- **Web**: hover a column header. The tooltip has the column name, then
  `integer, not null, no default, foreign key to customers(id)`.

The column header menu's **Copy column definition** copies the column as it would sit in a `CREATE TABLE`
(`customer_id integer NOT NULL`); see [Browsing data](../../browsing-data/).

{{< ui tui="structure-detail-line" >}}The row detail pane names the current column and its type and nullability on its first line; the pane is narrow, so the default and the key are cut off with `…`.{{< /ui >}}

## Details and limits

- **Reload**: {{< key "C-d M-r" >}} (`reload_tab`) in the Structure or Index view reloads the structure, not the rows. With
  pending structure changes the terminal asks `Reload and discard pending changes?` first. In the web, **Reload** in
  the toolbar reloads the table, and leaving the Structure view with unsent changes asks `Discard the unsent structure changes?`.
- **Views and materialized views** have a Structure view (their columns) and a DDL view, but nothing in them is
  editable: the terminal grid refuses edits with `paid_orders cannot be edited`, and the web shows the read-only page.
- **Engines without a fixed structure**: on MongoDB and Redis the web shows the read-only page; the terminal shows the
  grid of inferred fields, but a commit fails with `MongoDB collections have no fixed structure to edit` (or
  `redis keys have no table structure to edit`).
- Index column lists come from the catalog without sort direction: the index `(status, placed_at DESC)` is listed as
  `status, placed_at`. The DDL view shows the real definition. An index you leave untouched is never re-created, so the
  direction is kept unless you edit that index.
