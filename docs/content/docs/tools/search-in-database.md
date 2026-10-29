---
title: Search in Database
description: Find a value in every column of every table of a schema (or of every schema), see which tables hold it and how many rows, and open a hit as a filtered table, in the terminal and in the browser.
weight: 10
---

Search in Database looks for one value in every column of every table, table by table, and lists the tables
that hold it with the number of matching rows. Picking a hit opens that table with the search already in its
filter bar, so you see the rows themselves. Use it when you know a value (an email, an order number, a SKU)
but not where it is stored.

{{< ui tui="tools-search" web="tools-search" >}}The **Search in database** dialog: the value, the operator and which tables to search.{{< /ui >}}

## How to open it

| What | Terminal | Web |
| --- | --- | --- |
| Open the dialog | {{< key "C-d /" >}} (`search_in_database`) with the sidebar, a table or the welcome tab focused | {{< key "C-d /" >}} |
| From Open Anything | not listed | {{< key "C-d p" >}}, then **search in database** |
| See it running | {{< key "C-d M-J" >}} (`jobs`): the jobs centre lists the search | {{< key "C-d M-J" >}} or **Jobs** in the status bar |

{{< callout type="info" >}}
In the SQL editor {{< key "C-d /" >}} is `toggle_comment` (an editor binding wins over a global one with the same
key), so it comments the line instead of opening the search. Move the focus to the sidebar first
({{< key "C-h" >}} or {{< key "C-d M-h" >}}) first. In the browser, click outside the editor or open the
search from Open Anything.
{{< /callout >}}

## The dialog

The terminal shows a form; the browser shows a dialog with the same choices.

| Field | Terminal | Web | What it does |
| --- | --- | --- | --- |
| Value | **Value** | the text box (Enter starts the search) | The text to look for. The web refuses an empty value ("a value is required"). |
| Operator | **Operator** (←/→ to cycle) | the drop-down beside the value | `Contains` (default), `Not contains`, `=`, `Has prefix`, `Has suffix`. |
| Tables | **Tables**: empty for every table, or `a, b, c` | **All tables** / **Selected tables** with a checkbox per table | Limits the search to the named tables. The web's **Search** button stays disabled until at least one table is ticked. |
| All schemas | **All schemas** checkbox (only on engines with schemas) | not offered | Walks every schema instead of the one shown in the sidebar. See [All schemas](#all-schemas). |

### Operators

| Operator | Matches a column when |
| --- | --- |
| `Contains` | the value appears anywhere in it |
| `Not contains` | the value does not appear in it (see the note below) |
| `=` | its text is exactly the value |
| `Has prefix` | it starts with the value |
| `Has suffix` | it ends with the value |

How each operator is written for the engine:

- **Contains, Has prefix, Has suffix** become `LIKE` with `%` around the value; `%`, `_` and `\` in your value are
  escaped, so they match literally. On PostgreSQL they use `ILIKE` and so ignore case. On MySQL and MariaDB
  case follows the column's collation (the default collations ignore case); on ClickHouse `LIKE` is
  case-sensitive.
- **`=`** compares the column as text on every SQL engine (`CAST(col AS TEXT)` on PostgreSQL, `CAST(col AS CHAR)`
  on MySQL, `toString(col)` on ClickHouse). That keeps a text value from matching every `0` in a numeric column
  on MySQL, but it also means the value must be spelled the way the database prints it: `10.50`, not `10.5`, for
  a `numeric(10,2)`.
- Each column is cast to text for the `LIKE` operators, so numbers, dates, UUIDs, enums and JSON are searched too.
- **Not contains** on SQL engines counts a row when *at least one* of its columns does not contain the value, so it
  matches almost every row. On MongoDB it counts the documents where *no* field contains it.

Not every engine has every operator: the search uses the same operator list as the
[filter bar](../../browsing-data/filter-bar/). Cassandra has only `=` of the five (CQL has no substring match),
and DynamoDB has no `Has suffix`. An operator the connection lacks stops the search with
`search operator "Contains" is not supported on this connection`.

### Columns and objects that are searched

- Only tables are searched (MongoDB collections and Redis keys count as tables); views, materialized views and
  other objects are not.
- Every column of a table is searched, except types that are huge or meaningless as text:
  PostgreSQL `bytea`, `tsvector`, `tsquery` and `oid`; MySQL and MariaDB `blob` types, `binary`, `varbinary` and
  the spatial types; ClickHouse `Map`, `Array`, `Tuple`, `Nested`, `JSON`, `Object` and aggregate-function columns.
- The numbers are exact counts (`SELECT count(*) … WHERE …`), one query per table.

### All schemas

With **All schemas** ticked, the terminal walks every schema the connection lists and skips the system ones:
`pg_catalog`, `information_schema`, `pg_toast*` and `pg_temp*` on PostgreSQL. The checkbox is shown only where the
connection has schemas (PostgreSQL and its relatives), so on MySQL, MariaDB, ClickHouse, Redis or MongoDB the
terminal always searches the current database.

The engine can do more than the dialogs offer: through the API (`AllSchemas: true`, see
[Details and limits](#details-and-limits)) a search on MySQL, MariaDB or ClickHouse walks every *database*, skipping
`mysql`, `sys`, `performance_schema` and `information_schema` (and `system` on ClickHouse); on Redis, MongoDB and
the other engines without schemas it searches the current database once. A MySQL login that can list no database
gets `searching all schemas is not supported on this connection`.

The hits are grouped by schema and each is labelled `schema.table`. Without **All schemas**, the search runs
in the schema shown in the sidebar, and a hit is labelled with its schema only when that is not the
session's current schema.

{{< callout type="info" >}}
The web dialog has no **All schemas** checkbox: it searches the schema shown in the sidebar. Switch the
sidebar's schema to search another one, or use the terminal for a search across schemas.
{{< /callout >}}

## Progress and results

The search runs as a background job, one table at a time, so the rest of the workspace stays usable.

{{< ui tui="tools-search-results" web="tools-search-results" >}}A finished search: the terminal title counts the tables checked and the tables with hits; the web shows `checked/total tables` and one line per hit with its row count.{{< /ui >}}

| | Terminal | Web |
| --- | --- | --- |
| Progress | the list title: `Search "london": 7/10 tables · 1 with hits`, then `· done` | `7/10 tables…` under the form; `. No matches.` when it ends empty |
| A hit | `table` (or `schema.table`) and `24 rows` | the table name and `24 rows` |
| A table that failed | listed with `error: …` as its detail; picking it still opens the table | listed as **failed** in red, with the error as the tooltip; it cannot be clicked |
| Pause / resume | not available | **Pause** and **Resume** while it runs; a search paused for more than 10 minutes is stopped and frees its job slot |
| Stop | {{< key "Esc" >}} closes the list and stops the search | **Stop**, or **Close** (which stops it too) |
| Open a hit | {{< key "Enter" >}} | click the hit |

Picking a hit in the terminal stops a search that is still running. In the web, clicking a hit closes the
dialog but the search keeps running as a job in the jobs centre, where you can stop it.

The search checks for cancellation between tables and during each count, so **Stop** takes effect at once. The
terminal runs one background job at a time (export, import, search, diff): starting a search while another runs
says `another export, import or search is still running (cancel it first)`. The web allows four running jobs
per session.

## Opening a hit

A hit opens the table in a new tab with the filter bar shown and one checked filter: **Any column**, the operator
you chose and the value. The grid, the row count and an export of the tab all follow that filter, and you can
edit or extend it like any other [filter](../../browsing-data/filter-bar/).

{{< ui tui="tools-search-hit" web="tools-search-hit" >}}The hit opened as a table: `customers` filtered on **Any column** `Contains` `london`.{{< /ui >}}

## Details and limits

- Nothing is changed: the search only counts rows.
- The terminal's table count in the title is known up front for the current schema or a list of tables; with
  **All schemas** it shows only the tables checked so far.
- Web only: the API (`POST /api/sessions/{id}/search` with `Value`, `Op`, `Schema`, `Tables`, `AllSchemas`) accepts
  an all-schemas search even though the dialog does not offer it; progress is read by polling the job, there is
  no streaming.
- To find a column by name rather than a value, use the row detail's **Search for field…** or the sidebar search.
