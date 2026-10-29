---
title: Export data
description: Write a table, a filtered table, a page, a query result or several tables to CSV, JSON, SQL INSERTs or Excel, with gzip and file-name patterns, in the terminal and in the browser.
weight: 20
---

Export writes rows to a file: a whole table (with the filters you applied), the page you are looking at, a query result, every result tab, or several tables at once. The formats are CSV, JSON, SQL `INSERT` statements and Excel (`.xlsx`); CSV, JSON and SQL can be gzipped. The terminal writes the file on this machine; the browser downloads it.

{{< ui tui="io-export-sql" web="io-export-sql" >}}The export dialog on `orders` with the SQL format, **Include structure** and **Drop table first** ticked.{{< /ui >}}

## How to open it

| | Terminal | Web |
|---|---|---|
| Keyboard | {{< key "C-d E" >}} (`export`) | the same {{< key "C-d E" >}} |
| Table tab | the row menu ({{< key "m" >}}): **Export…** | the **Export** toolbar button, or the row menu: **Export…** |
| Query result | the result menu: **Export…** | the result menu: **Export…**; **Export all tabs** in the result toolbar |
| Sidebar | right-click or {{< key "m" >}} on an object: **Export…**, **Export several tables…** | right-click an object: **Export…**, **Export several tables…** |
| Filter bar | {{< key "M-e" >}} (`filter_export`, no prefix) or the bar menu: **Export filtered rows** | the same {{< key "M-e" >}}, or the **Export filtered rows** button |
| Marked tables | mark tables in the sidebar with {{< key "Space" >}} (`mark`), then {{< key "C-d E" >}} with the sidebar focused | the same |

What gets exported depends on where you are:

- **Sidebar focused** (or no tab open): the marked tables, one file each; with nothing marked, the highlighted object.
- **A table's data tab**: that table, with the filters you applied and the columns you have not hidden.
- **A query tab with a result**: that result. With nothing to export the terminal says "open a table or run a query first"; the browser says "Run a query first, then export its result".

## What you can do

### Terminal

The dialog is titled **Export *table*** (or **Export result**). Fields that do not apply to the chosen format are hidden.

| Field | Shown for | What it does |
|---|---|---|
| **Format** | always | `csv`, `json`, `sql` or `xlsx`. |
| **Header row** | csv | Write the column names first. Starts from `[export] header` (on by default). |
| **NULL written as** | csv | The text written for NULL. Starts from `[csv] null_as` (empty: a NULL is an empty cell). |
| **Line breaks** | csv | `LF` or `CRLF`. Starts from `[csv] line_break`. |
| ⚠ warning | csv | "delimiter and decimal separator are both ",": decimal numbers will be quoted or split", when `[csv] delimiter` and `[csv] decimal` are both a comma. |
| **Gzip** | csv, json, sql | Compress the file; `.gz` is added to the default name. |
| **Include structure** | sql, a table | Write the table's `CREATE TABLE` (its DDL) before the rows. |
| **Drop table first** | sql, a table | Write `DROP TABLE IF EXISTS` first. |
| **Current page only** | a table | Only the rows loaded in the grid, instead of the whole table. |
| **Pick several tables (all schemas, one file each)** | a table, engines with schemas | Opens the table checklist instead, see [Several tables](#several-tables). |
| **All result tabs (one file each: name-1, name-2...)** | a query with more than one result tab | Writes every result tab with rows to its own file. |
| **File** | always | Where to write. Starts with the [file-name pattern](#file-names) plus the extension, relative to the folder you started `webtuidb` in. The default name follows the format and gzip until you edit it. |

{{< key "Enter" >}} starts the export ("file name required" for an empty **File**). When the file exists you are asked **Overwrite *path*?** first.

The **File** field may itself hold a pattern: `{connection}`, `{database}`, `{table}`, `{tag}`, `{date}` and `{time}` in the file-name part are expanded, the folder part is kept as typed. `exports/{table}-{date}.csv.gz` writes `exports/orders-2026-10-29.csv.gz`.

{{< ui tui="io-export-done" >}}A gzipped CSV export of the filtered `orders` table: the status line says "exported 150 rows to …" and the jobs centre lists the finished job, with **o** to open its folder.{{< /ui >}}

### Web

{{< ui tui="io-export-dialog" web="io-export-dialog" >}}**Export orders** with its defaults: CSV with a header row, the file name from `[export] filename`.{{< /ui >}}

| Control | Shown for | What it does |
|---|---|---|
| **Format** | always | **CSV**, **JSON**, **SQL INSERTs**, **Excel (xlsx)**. |
| **Rows** | a table | **Whole table (filters applied)** or **Current page only**. |
| warning | CSV | The same delimiter / decimal warning as the terminal. |
| **Header row** | CSV | On by default. |
| **NULL as** | CSV | Empty: the `[csv] null_as` setting (the placeholder shows it). |
| **Line breaks** | CSV | **As in the settings**, **LF (Unix)**, **CRLF (Windows)**. |
| **Compress with gzip** | not Excel | Same as **Gzip**. |
| **Include the table structure (CREATE TABLE)**, **Drop the table first** | SQL, a table | Same as in the terminal. |
| **File name** | always | The name the download gets. Empty: the `[export] filename` pattern (shown as the placeholder) plus the extension. Tokens are expanded here too. {{< key "Enter" >}} in this field exports. |

Under the fields the dialog says how many columns go out: "*n* visible columns." when you hid some, otherwise "All columns.". A whole-table export runs as a background job and shows "*n* rows written…" while it runs; the file downloads when it is done and a toast says "Export saved". **Cancel** stops it.

There is no "All result tabs" checkbox in the browser; the result toolbar has an **Export all tabs** button instead (see [Query results](#query-results)).

## What each scope exports

| Scope | Terminal | Web |
|---|---|---|
| Whole table | Every row matching the applied filters, read 1,000 rows at a time ordered by the primary key (by every column when there is none). The grid's sort is not used. | The same, as a background job on the server. |
| Current page | The rows loaded in the grid as they are on screen, including pending edits you have not committed; rows marked for deletion are left out. | The page is read again from the database with the grid's filters, sort, page size and offset, so pending edits are **not** in the file. |
| Filtered rows ({{< key "M-e" >}}) | The filters that were last applied; edits in the bar that you have not applied are ignored. With no filter it is the whole table. | The filters as they stand in the bar, applied or not, over the whole table (not just the page). |
| Query result | The rows the result tab holds (up to the row limit it ran with). | The statement is **run again** on the server and its rows are exported, up to the tab's row limit. Only one read-only statement can be exported: "only a single read-only statement can be exported", and "the statement returned no rows" for one that returns none. |
| Columns | The columns not hidden in the grid. For a result, all of them. | The same. |

### Query results

{{< ui tui="io-export-result" web="io-export-result" >}}Exporting a query result. The table-only fields (structure, drop, page) are not offered; the default name is `result-<date>`.{{< /ui >}}

- **All result tabs** (terminal): one file per result tab with rows, numbered before the extension: `report.csv` becomes `report-1.csv`, `report-2.csv`… (`.csv.gz`, `.json.gz` and `.sql.gz` count as one extension). The status line says "exported *n* files next to *path*".
- **Export all tabs** (web): one CSV download per result tab without an error, named `result-1.csv`, `result-2.csv`… after the tab's position, each statement run again. No options; the toast says "Exported *n* result tabs".
- A SQL export of a result writes `INSERT INTO result (…)`, because a result has no table name; edit the name before running the file.

### The Message view

A statement without rows (or with PostgreSQL notices) shows the Message view. It can be saved as plain text: in the terminal press {{< key "s" >}} in the Message view ({{< key "m" >}} switches between rows and messages), which asks for a **File** (default `message.txt`, with the same overwrite question); in the browser use **Save as text** under the message, which downloads `message.txt`. **Copy** copies it instead.

### Several tables

{{< ui tui="io-export-many" web="io-export-many" >}}Picking tables across schemas. The terminal lists every table of every schema as `schema.table`; the browser groups them by schema with an **All tables of *schema*** box.{{< /ui >}}

- **Terminal**: **Pick several tables** (or **Export several tables…** in the sidebar menu) opens **Export which tables? (all schemas)**, with the current table ticked. {{< key "Space" >}} toggles, {{< key "a" >}} checks all, {{< key "A" >}} unchecks all, {{< key "Enter" >}} goes on ("no table picked" when none is). With tables marked in the sidebar, {{< key "C-d E" >}} skips the checklist. The next form, **Export *n* tables: one file each**, has **Format**, **Gzip**, **Include structure** (SQL) and **Folder** (default `.`, created when missing). Each table goes to `<folder>/<pattern>.<ext>` with `{table}` set to `schema.table`, all in one background job ("exported 2 of 5 tables…"). There is no header, NULL or drop choice here: the header row is always written.
- **Web**: **Export tables** lists the schemas (system schemas hidden unless `[sidebar] load_system_schemas` is on; the current one always shows), with the format, **gzip**, **structure** (SQL), **Header row** (CSV) and a **File name** pattern applied per table. Each table is its own background job and its own download, so the browser may ask to allow several downloads. Without a pattern, tables from more than one schema are named `schema.table.ext`; with one schema the `[export] filename` pattern is used.

System schemas (`information_schema`, `pg_*`, `mysql`, `performance_schema`, `sys`, `system`) are left out of both lists unless `[sidebar] load_system_schemas` is on.

## Formats

### CSV

- Delimiter, quote and quoting follow `[csv] delimiter`, `[csv] quote` and `[csv] quote_mode`: `needed` (default) quotes only fields that hold the delimiter, the quote, a CR or a LF; `always` quotes every non-empty field (an empty field stays empty).
- NULL is written as **NULL written as** / **NULL as**, or `[csv] null_as`, or an empty cell. An empty cell reads back as NULL with **Empty is NULL** in the [import wizard](../import/).
- `[csv] decimal` replaces the decimal point of floating-point values (not of `numeric` / `decimal` columns, which arrive as text).
- A row with one empty column is written as `""`, so it is not a blank line that an import would skip.
- Binary values are written as hex.

#### Formula guard

A text cell (and a header name) that starts with `=`, `+`, `-`, `@`, a tab or a carriage return gets a leading `'`, so a spreadsheet does not run it as a formula: `=SUM(A1)` is written as `'=SUM(A1)`. Values that parse as numbers (`-12.5`) are never changed. The guard is always on: the engine has an option to turn it off, but neither dialog nor the API offers it. The import wizard removes the apostrophe again unless **Keep formula apostrophe** is ticked.

### JSON

An array with one object per row, one row per line:

```json
[
  {"id": 1, "status": "paid", "shipping_address": {"zip": "10037", "city": "Springfield"}},
  {"id": 2, "status": "shipped", "shipping_address": null}
]
```

NULL is `null`, booleans and integers stay JSON values, `NaN` and infinities become `null`, other values (dates, decimals, UUIDs) are strings. A `json` / `jsonb` column is written as nested JSON, not as a string. The [import wizard](../import/) reads this format back.

### SQL INSERTs

One `INSERT INTO "schema"."table" (columns) VALUES (…);` per row, quoted for the connection's dialect. With **Drop table first** the file starts with `DROP TABLE IF EXISTS`, then with **Include structure** the table's DDL, as the [structure view](../../structure/structure-view/) shows it. On PostgreSQL a whole-table export ends with a `SELECT setval(pg_get_serial_sequence(…))` for each `serial` / identity column, so inserting after a reload does not collide with the copied ids (as `pg_dump` does). Binary values are hex literals, `NaN` / `Infinity` are quoted, and timestamps are written with their offset.

### Excel (xlsx)

One sheet named after the table (characters `: \ / ? * [ ]` become `_`, cut to 31 characters), the header row first, numbers and booleans as Excel values, NULL as an empty cell. Gzip does not apply. The header and NULL options are CSV-only and are ignored. Excel files are built in memory and written when the last row is in, so a very large table takes memory; the other formats are written as the rows arrive.

## File names

The default name comes from `[export] filename`, default `{table}-{date}`, and the extension of the format (`csv`, `csv.gz`, `json`, `json.gz`, `sql`, `sql.gz`, `xlsx`) is added unless the pattern already ends with it.

| Token | Value |
|---|---|
| `{connection}` | the connection's name |
| `{database}` | the current database |
| `{table}` | the table name; `result` for a query result; `schema.table` for several tables in the terminal |
| `{tag}` | the connection's tag (`development`, `production`…) |
| `{date}` | today, `YYYY-MM-DD` |
| `{time}` | now, `HHMMSS` |

An empty token takes one neighbouring `-`, `_` or space with it, so `{tag}-{table}` on an untagged connection is `orders`, not `-orders`. Unknown tokens stay as typed. `/ \ : * ? " < > |` and control characters become `_`; a name that ends up empty is `export`; Windows device names (`CON`, `NUL`, `COM1`…) get a leading `_`.

## Where the file goes

- **Terminal**: the path in **File**, relative to the folder you started `webtuidb` in. The file is written to a hidden temporary file in the same folder (`.webtuidb-*`, mode 0600) and renamed into place when complete, so a failed or cancelled export never leaves a half-written file or destroys the old one. The jobs centre ({{< key "C-d M-J" >}}) keeps the full path; {{< key "o" >}} there (or **Open folder** in its menu) opens it in the file manager.
- **Web**: the file is downloaded by the browser into its download folder. A whole-table export is first written to a temporary file on the server; it is deleted after its one download. Up to three finished, not yet downloaded exports are kept per connection (older ones are deleted when a new job starts), and they count towards the 1 GiB temporary-file budget shared with import uploads ("this session's temporary files are over their 1024 MB budget; download or delete finished exports and retry"). A page or query export is streamed straight into the download. In the jobs centre, **Download** fetches a finished export that the dialog did not.

## Details and limits

- An export only reads; it does not pass the [safe mode](../../safety/safe-mode/) gate and works on `read_only` connections.
- The terminal runs one background job (export, import, backup, search) at a time per workspace: "another export, import or search is still running (cancel it first)". Cancel it with {{< key "C-d M-x" >}} (`cancel_queries`) or {{< key "s" >}} in the jobs centre. The browser runs up to four per connection: "4 background jobs are already running on this session; wait or stop one".
- Paging uses `LIMIT`/`OFFSET`. Rows inserted or deleted in the table while a long export runs can be skipped or written twice.
- The process list has its own CSV export (**Export…** in its menu, {{< key "e" >}} in the terminal: `processes.csv`), and a metrics board **exporter** panel opens this dialog on the panel's query (at most 100,000 rows, and the dialog says so when the query returns more). See [Process list](../../tools/process-list/) and [Metrics boards](../../tools/metrics-boards/).

### Copy as versus export

**Copy rows as…** ({{< key "C-d M-c" >}}, `copy_rows_as`) and **Copy selected cells as…** ({{< key "C-d M-C" >}}) put the selected rows or cells on the clipboard as Plain Text, JSON, HTML, Markdown Table, CSV, CSV with Header or INSERT Statement. Use them for a few selected rows; there is no "export the selection" to a file. Export works on whole tables, pages and results, and writes a file. See [Results](../../sql-editor/results/).

## Settings

| Setting | Default | Effect |
|---|---|---|
| `[export] filename` | `{table}-{date}` | the file-name pattern; empty means the default |
| `[export] header` | `true` | the terminal's **Header row** default, and the header for API calls that do not say. The browser's checkbox always starts ticked. |
| `[csv] delimiter`, `quote`, `quote_mode` | `,`, `"`, `needed` | CSV layout |
| `[csv] line_break` | `\n` | CSV line break |
| `[csv] decimal` | `.` | decimal separator of floating-point values |
| `[csv] null_as` | empty | text written for NULL |
| `[sidebar] load_system_schemas` | `false` | list system schemas in the several-tables picker |

See the [settings reference](../../configuration/settings-reference/). The HTTP endpoint is `POST /api/sessions/{sid}/export` (`mode` `table`, `page` or `query`; `job: true` runs a table export in the background, fetched from `GET …/jobs/{id}/file`); see the [API](../../web-ui/api/).
