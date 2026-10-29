---
title: Import data
description: Load a CSV, TSV, JSON or SQL dump file into a table, with a preview, column mapping, NULL handling and one transaction per file, in the terminal and in the browser.
weight: 10
---

The import wizard reads a file, shows you the first rows the way the table will receive them, lets you map each file column to a table column, and then loads the whole file in the background. It takes CSV (and TSV or any other single-character delimiter), JSON and SQL dump files. On PostgreSQL and MySQL / MariaDB a CSV or JSON file goes in as one transaction: every row or none.

{{< ui tui="io-import-dialog" web="io-import-dialog" >}}The import wizard opened on `docs_io_suppliers`. The terminal asks for a file path first; the browser uploads the file and shows the preview and the column mapping in the same dialog.{{< /ui >}}

## How to open it

| | Terminal | Web |
|---|---|---|
| Keyboard | {{< key "C-d I" >}} (`import`) | the same {{< key "C-d I" >}} |
| Sidebar | right-click or {{< key "m" >}} on a table: **Import into this table…** | right-click a table: **Import into this table…** |
| Grid | the row menu of a table's data tab: **Import…** | the **…** button of the table toolbar, or the row menu: **Import…** |
| Drag and drop | not available | drop a `.csv`, `.tsv`, `.json` or `.sql` file on a table in the sidebar, or on the grid of an open table |
| Metrics board | an **import** panel opens the wizard on the panel's table | the same |

The wizard aims at the table that is highlighted in the sidebar, or the table of the active tab. In the terminal you can still type another table name in the **Table** field (it is created in the current schema when **Create the table** is ticked). In the browser the dialog is always tied to the table it was opened on: its title is **Import into *table***, and only tables can be imported into (a view answers "Only tables can be imported into").

Dropping a file anywhere else in the browser opens a `.sql` file in a new editor tab and answers "Drop a CSV, JSON or SQL file to import it into this table" for other files on a table tab. Importing a `.sql` file as a *saved query* is a different feature: see [SQL files](../../sql-editor/sql-files/).

## Step 1: the file and how to read it

### Terminal

The terminal wizard is a form. {{< key "Tab" >}} or the arrow keys move between fields, {{< key "←" >}} / {{< key "→" >}} change a choice or a checkbox, {{< key "Enter" >}} goes on, {{< key "Esc" >}} cancels. Fields that do not apply to the chosen format are hidden.

| Field | Shown for | What it does |
|---|---|---|
| **File** | always | Path of the file on this machine. Relative paths are read from the folder you started `webtuidb` in. Typing a path ending in `.json` or `.sql` switches **Format**; `.csv`, `.tsv` and `.txt` switch it to `csv`. |
| **Format** | always | `csv`, `json` or `sql`. |
| **First row is the header** | csv | On (default): the first row names the columns. Off: the columns are called `column1`, `column2`… and the first row is data. |
| **Delimiter** | csv | One character. Starts with `[csv] delimiter` from `config.toml` (`,` by default). |
| **File encoding** | always | `utf-8`, `latin1`, `windows-1252`, `utf-16le`, `utf-16be`, `shift_jis`, `gbk`. |
| **NULL as** | csv, json | Cell text that becomes NULL, for example `\N` or `NULL`. Empty: for CSV the `[csv] null_as` setting is used; for JSON nothing is converted. |
| **Empty is NULL** | csv, json | An empty cell (or a JSON `""`) becomes NULL instead of an empty string. This is how the CSV export writes NULL when `null_as` is empty, so turn it on to read such an export back. |
| **Keep formula apostrophe** | csv, json | Off (default): one leading `'` in front of `=`, `+`, `-`, `@`, a tab or a carriage return is removed from CSV cells and CSV header names, which undoes the formula guard the CSV export adds (see [Export](../export/#formula-guard)). A number after the apostrophe (`'-12`) is left alone. On: the apostrophe is kept. The field is also shown for JSON, but JSON values are never changed by it. |
| **Create the table** | csv, json | Create the table first, with one column per file column. See [Creating the table](#creating-the-table). |
| **Table** | csv, json | The target table. Pre-filled with the table the wizard was opened on. |

{{< key "Enter" >}} checks the form before moving on: "cannot read that file" when the path is missing or a folder, "table name required" for an empty **Table**, and any error the reader meets in the first 100 rows (a bad delimiter, a blank header, a wrong encoding). The form stays open with your answers, and the wizard remembers them for the next import in the same workspace (except **Table**, which follows the table you open it on). Reading the preview gives up after 30 seconds.

### Web

Pick the file with **Choose File** (or drop it). The format follows the file name (`.json` is JSON, `.sql` is a SQL dump, anything else CSV) and can be changed in the first drop-down.

| Control | Shown for | What it does |
|---|---|---|
| Format (**CSV** / **JSON** / **SQL dump**) | always | Same as the terminal's **Format**. |
| **Encoding** | CSV, JSON | **UTF-8**, **Latin-1**, **UTF-16 LE**, **UTF-16 BE**, **Windows-1252**. (The browser list has no Shift JIS or GBK; the terminal has.) |
| **Header row** | CSV | Same as **First row is the header**. |
| **Delimiter** | CSV | One character; empty uses `[csv] delimiter`. |
| **Create the table** | CSV, JSON | See [Creating the table](#creating-the-table). |
| **NULL as**, **Empty is NULL**, **Keep formula apostrophe** | CSV, JSON | Same as in the terminal. |
| **Match columns by** | CSV, JSON, existing table | How the file columns find table columns, see [Step 2](#step-2-preview-and-column-mapping). |

The preview refreshes a quarter of a second after each change. For a CSV file larger than 1 MiB only the first MiB is sent for the preview (cut at its last line break), so the preview is quick on a big file; the import itself reads the whole file.

### TSV and other delimiters

A TSV file is a CSV file with a tab delimiter. Neither UI switches the delimiter by itself when the file ends in `.tsv`, and a tab cannot be typed into the **Delimiter** field (the Tab key moves to the next field). The reliable way is the setting: put `delimiter = "\t"` under `[csv]` in `config.toml`. The terminal field then starts with a tab, and the browser uses the setting whenever its **Delimiter** field is empty. Pasting a tab character into the field also works.

### What the reader accepts

- **CSV**: a UTF-8 byte-order mark is skipped. Quotes follow `[csv] quote` (default `"`), which must be one printable ASCII character that is not the delimiter. Quoting is strict: a stray quote stops the import with its line number instead of swallowing the rest of the file into one cell. Every row must have as many fields as the header: "line 7 has 4 fields, expected 5". A header cell that is empty stops the preview with "column 3 has no name; give it a name in the file". The line break is whatever the file uses; the `[csv] line_break` setting only applies to export.
- **JSON**: either an array of objects (`[{...}, {...}]`) or objects one after another (newline-delimited JSON). The keys of the **first** object are the columns, in their order. A later object may leave keys out (they become NULL) but may not add one: "unexpected key "x" (not in the first object)". `null` is NULL, `true` / `false` stay booleans, numbers are passed as their exact text, and nested objects or arrays are stored as JSON text.
- **SQL**: a script of statements; see [SQL dumps](#sql-dumps).
- **Encodings**: a file read with the wrong encoding is refused ("the file is not valid utf-16le: some bytes cannot be decoded, pick another encoding") instead of loading garbled rows.

## Step 2: preview and column mapping

{{< ui tui="io-import-mapping" >}}The terminal's second step: the first five rows as they will be stored (the empty rating already shows as NULL because **Empty is NULL** is on), then one choice per file column.{{< /ui >}}

The preview applies the same transformations as the import (NULL as, empty is NULL, the formula apostrophe), so what you see is what the table gets. Then every file column gets a target:

- **Terminal**: a second form, **Import *file***, headed **Preview (*n* rows read)** with up to five rows, then **Column mapping: file column → *table***. Each file column is a choice of `(skip)` or one of the table's columns; it starts on the table column with the same name, ignoring case, or `(skip)` when there is none. {{< key "Enter" >}} starts the import.
- **Web**: a drop-down above each column of the preview table, with **skip** and the table's columns. **Match columns by** fills them in:
  - **Name, then order** (default): same name ignoring case first, then the file columns left over take the table columns left over, in order.
  - **Name (ignore case)**: only same-name matches; the rest is skipped.
  - **Order**: the first file column goes to the first table column, and so on.

  There is no fuzzy matching in either UI: names match exactly, ignoring case.

Two file columns may not go to the same table column. The terminal refuses the form ("file columns "a" and "b" both map to "x""), the server refuses the import ("file columns 1 ("a") and 3 ("a") both go to table column "a"; map one to another column or skip it"). A file whose header repeats a name therefore needs one of the two skipped or mapped elsewhere when you import into an existing table.

Skipped columns are not sent at all, so the table's default applies to the columns nobody maps.

## Creating the table

With **Create the table** ticked the new table gets one column per file column, all nullable and of type `TEXT`, and the mapping offers the file's own column names. A repeated header name gets a numeric suffix (`name`, `name_2`), and **skip** leaves a column out of the new table. Change the types afterwards in the [structure editor](../../structure/structure-editor/), or send typed columns through the API (`column_types`, see [API](#api)).

{{< ui tui="io-import-create" web="io-import-create" >}}**Create the table** with a JSON file. In the terminal the new name goes in the **Table** field; the browser dialog has no name field.{{< /ui >}}

{{< callout type="warning" >}}
In the browser, **Create the table** creates a table with the name of the table the dialog was opened on, because the dialog has no field for a new name. Opened from the sidebar or a table tab, that table already exists, so the import stops with "relation … already exists" (PostgreSQL) or "Table … already exists" (MySQL) and nothing is imported. Create a new table from a file in the terminal for now, or create an empty table first and import into it.
{{< /callout >}}

On PostgreSQL the `CREATE TABLE` runs inside the import transaction, so a failed import takes the new table with it. MySQL and MariaDB commit `CREATE TABLE` on their own: the table is created before the transaction opens and stays when the data fails ("rolled back; the new table stays, MySQL commits CREATE TABLE").

## Step 3: the import runs

Importing writes, so it passes the connection's [safe mode](../../safety/safe-mode/) first. The terminal shows the statement `IMPORT <file> INTO <table>` in the confirmation (or asks for the database password, depending on the level). The browser checks the statement kinds the import will run (`INSERT`, plus `UPDATE` for an upsert and `CREATE TABLE` when creating) and shows the confirmation or password field inside the import dialog. On a `read_only` connection the import is refused: the browser before any of the file is uploaded ("this connection is read-only; data-changing statements are refused"), the terminal when the first batch reaches the driver.

{{< ui tui="io-import-done" web="io-import-done" >}}A finished import: the terminal's status line says "processed 8 rows into docs_io_suppliers" and the jobs centre ({{< key "C-d M-J" >}}) lists the job; the browser dialog says "Done. 8 rows in 1 batches." and a toast says "Processed 8 rows".{{< /ui >}}

- Rows are sent in batches of 500. Progress shows in the status line ("importing… 1500 rows in 3 batches") or in the dialog ("Importing. 1500 rows in 3 batches."), and in the jobs centre.
- On **PostgreSQL** and **MySQL / MariaDB** a CSV or JSON import pins one connection and wraps the whole file in `BEGIN` … `COMMIT`: a failure anywhere rolls everything back and the message ends in "(rolled back, no rows were imported)".
- On other engines each batch is committed on its own. A failure after the first batch ends in "(processed 1000 rows; rows before the failure were committed)".
- The count says *processed*, not *inserted*: the database reports no per-row count back, so with an upsert that skips rows the number can be higher than what really changed.
- When it is done, the terminal reloads every open tab of that table (and the sidebar when a table was created); the browser refreshes the grid or the sidebar.
- The import runs in the background. In the terminal one background job runs at a time per workspace ("another export, import or search is still running (cancel it first)"); cancel it with {{< key "C-d M-x" >}} (`cancel_queries`) or {{< key "s" >}} in the jobs centre. The browser runs up to four jobs per connection; stop one from the jobs centre. While an import runs, the browser dialog cannot be closed.

### Errors and row numbers

A failed batch names the rows it covered and the statement that failed, with the database's own message:

{{< ui tui="io-import-error" web="io-import-error" >}}A rating of `excellent` in a numeric column. The browser dialog shows the whole message: rows 1-3 of the file were in the batch, statement 2 is the second row of that batch, and nothing was imported.{{< /ui >}}

`rows 1-3` is the range of file rows in the failing batch (counting data rows, not lines), and `statement 2` is the position inside that batch, so the bad row is row 2 of the file here. The terminal shows the message in the status line and in the jobs centre; the browser shows it in the dialog under **Stopped:**.

## SQL dumps

With the **sql** format the file is run as it is, statement by statement, on the connection's current database. There is no table, mapping or NULL option.

{{< ui tui="io-import-sql" web="io-import-sql" >}}A SQL dump. The browser preview lists the first statements; the terminal goes straight to the import after **Enter**.{{< /ui >}}

- The file is read in 1 MiB chunks and split into statements the same way the SQL editor splits them for the connection's dialect: semicolons inside quotes, comments and PostgreSQL dollar quotes do not split, and MySQL `DELIMITER` lines switch the terminator (the `DELIMITER` lines themselves are dropped).
- Statements are sent in batches of 500. Each batch is one transaction where the engine has them, but the file as a whole is **not** one transaction: a failure leaves the earlier batches applied. The counts in the progress and the final message are statements, not rows.
- On-conflict modes do not apply to SQL dumps.
- PostgreSQL `COPY … FROM stdin` blocks, which `pg_dump` writes by default, cannot be run this way; dump with `--inserts`, or use [Restore](../backup-restore/) with `psql`.

{{< callout type="info" >}}
The browser preview always splits as PostgreSQL, because the real dialect is only applied when the import runs. A MySQL `DELIMITER $$` block can therefore look like one long statement in the preview and still import correctly.

A MySQL `DELIMITER` set in one 1 MiB chunk of a very large file is forgotten when the next chunk starts; keep routine definitions in smaller files.
{{< /callout >}}

## Limits

| Limit | Value |
|---|---|
| Rows per batch | 500 |
| Preview | 100 rows read (the terminal shows 5 of them), 30 s in the terminal |
| Browser upload | 512 MiB per file (multipart upload, streamed to a temporary file on the server) |
| Browser temporary disk | 1 GiB per connection for upload spools and finished, not yet downloaded exports together; older finished exports are deleted to make room, and an import that still does not fit is refused with "this session's temporary files are over their 1024 MB budget; download or delete finished exports and retry" |
| Background jobs | terminal: one at a time per workspace; browser: four running per connection |
| Quote character | one ASCII character |

## Upserts and on-conflict modes

Neither dialog offers an on-conflict choice: both import with plain `INSERT`s and stop at the first error, such as a duplicate key. The engine supports two more modes, reachable through the HTTP API (`on_conflict`):

| `on_conflict` | PostgreSQL | MySQL / MariaDB |
|---|---|---|
| `""` or `stop` | plain `INSERT`; the first error stops the import | same |
| `update` or `replace` | `INSERT … ON CONFLICT (primary key) DO UPDATE SET` every imported non-key column; `ON CONFLICT DO NOTHING` when only key columns are imported | `INSERT … ON DUPLICATE KEY UPDATE col = VALUES(col)` |

Both upsert modes are the same upsert. `replace` does not use MySQL's `REPLACE INTO`, which deletes the old row first (losing the columns the file does not carry and firing cascades). An upsert needs a primary key, needs every key column among the imported columns ("primary key column "id" is not among the imported columns, so no row could ever conflict"), cannot be combined with creating the table, and is refused for SQL dumps and for Redshift, Greenplum and Vertica, which speak PostgreSQL but have no `ON CONFLICT`.

## API

The browser uses `POST /api/sessions/{sid}/import/preview` and `POST /api/sessions/{sid}/import`, as `multipart/form-data` with an `options` part (JSON) **before** the `file` part, or as a JSON body with the file in `content`. The options are `source` (`csv`, `json`, `sql`), `header`, `delimiter`, `quote`, `encoding`, `null_as`, `empty_is_null`, `keep_formula_quote`, `createTable`, `ref` (the table), `mapping` (file column → table column, `""` to skip), `on_conflict`, `column_types` (column → SQL type, for a created table), `confirmed` and `password` (the safe-mode answer). The import answers `{"job": id}`; follow it at `GET …/jobs/{id}/events`. See the [API reference](../../reference/).

## Settings

| Setting | Effect on import |
|---|---|
| `[csv] delimiter` | default delimiter (the browser uses it when its field is empty) |
| `[csv] quote` | quote character |
| `[csv] null_as` | cell text read as NULL when **NULL as** is empty |
| `[web] notifications` | desktop notification when a browser import finishes while the page is hidden |

See the [settings reference](../../configuration/settings-reference/).
