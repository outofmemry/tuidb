---
title: Edit a table's structure
linkTitle: Structure editor
description: Add, rename, retype and drop columns, change the primary key, foreign keys and indexes, and review the ALTER statements before they run.
weight: 20
---

The Structure and Index views are also the table designer. You change cells the way you change data: every edit is
pending until you send it, and what is sent is a list of `ALTER TABLE` (and `CREATE INDEX` / `DROP INDEX`) statements
the engine plans from the difference between the table as it was loaded and the table as you left it. Nothing reaches
the server before you commit (terminal) or press **Send changes** (web), and safe mode applies as for any other change.

{{< ui tui="structure-editor-pending" web="structure-editor-pending" >}}Three pending changes on `orders`: `coupon` renamed to `coupon_code`, `total` widened to `numeric(12,2)`, and a new column `notes`. The terminal marks changed rows with `●` and new rows with `+`; the web keeps the draft in the inputs and says "Not sent to the database yet."{{< /ui >}}

## How to open it

Open the table's Structure view ({{< key "C-d )" >}}) or Index view ({{< key "C-d M-i" >}}); see
[Structure, Index and DDL views](../structure-view/). Only tables are editable. Views, materialized views, and the
collections and keys of MongoDB and Redis are shown read-only.

| Action | Terminal | Web |
|---|---|---|
| Edit a cell | {{< key "Enter" >}} or {{< key "e" >}} on a name, type or default cell; type; {{< key "Enter" >}} keeps it, {{< key "Esc" >}} drops it | click the input and type |
| Toggle `is_nullable`, `primary_key`, `is_unique` | {{< key "Enter" >}} on the cell | the checkbox |
| Pick a type or a default from a list | {{< key "C-d M-Enter" >}} (`set_value`) on the cell | the input offers the list as you type |
| Edit the whole column in a form | {{< key "Space" >}} on the row | {{< key "C-d Space" >}} (`toggle_row_detail`) on the row |
| Add a column (Structure) or index (Index) | {{< key "i" >}} or {{< key "C-d i" >}} (`add_row`) | **Add column**, **Add index**, **Add foreign key**, or {{< key "C-d i" >}} (adds to the section you last clicked in) |
| Drop a column or index | {{< key "Delete" >}}, {{< key "Backspace" >}} or {{< key "C-d Delete" >}} (`delete_rows`) | **Delete** at the end of the row, or {{< key "C-d Delete" >}}; **Keep** brings it back |
| Move a column (MySQL, MariaDB, ClickHouse) | {{< key "M-Up" >}} / {{< key "M-Down" >}} | the ▲ ▼ buttons or drag the ⠿ handle |
| Row menu | {{< key "m" >}} | right click a row |
| Preview the statements | {{< key "C-d P" >}} (`preview`) | **Preview changes** |
| Send them | {{< key "C-d s" >}} (`commit`), or {{< key "Enter" >}} in the preview | **Send changes** in the preview dialog |
| Throw the edits away | {{< key "C-d Backspace" >}} (`discard`) | **Discard** |
| Undo, redo | {{< key "C-d z" >}}, {{< key "C-d Z" >}} | (undo the input itself with the browser's own undo) |

{{< callout type="info" >}}
In the web, {{< key "C-d s" >}}, {{< key "C-d P" >}}, {{< key "C-d Backspace" >}} and {{< key "C-d z" >}} act on the data
grid's staged row edits, not on the structure draft: use the **Preview changes** and **Discard** buttons above the
columns. The terminal keys act on whichever view is showing.
{{< /callout >}}

## The terminal grid

The Structure grid has the columns `#`, `column_name`, `data_type`, `is_nullable`, `column_default`, `primary_key`,
`extra` and, on engines with foreign keys, `foreign_key`. A two-cell rail on the left marks each row so it does not
rely on colour alone:

| Mark | Means |
|---|---|
| `+` | a new column or index, not on the server yet |
| `✗` | marked for dropping (the row is struck through) |
| `●` | changed |
| `⚷` | part of the primary key |

The tab title gets a `●` and the status line `● unsaved` while anything is pending. The bottom edge lists the keys:
`↵ edit/toggle   C-d i add   Del remove   C-d s commit`.

Keys inside the grid, without the prefix:

| Key | Does |
|---|---|
| {{< key "j" >}} {{< key "k" >}} {{< key "h" >}} {{< key "l" >}}, arrows | Move. With {{< key "S-Up" >}} / {{< key "S-Down" >}} / {{< key "S-Left" >}} / {{< key "S-Right" >}} extend a selection. |
| {{< key "PgDn" >}} / {{< key "PgUp" >}} | Page down / up. |
| {{< key "Esc" >}} | Clear the selection. |
| {{< key "Enter" >}} / {{< key "e" >}} | On `column_name`, `data_type`, `column_default` (Index view: `index_name`, `column_name`): edit the text. On `is_nullable`, `primary_key` (Index view: `is_unique`): toggle `YES` / `NO`. On `foreign_key`: open the foreign key form. |
| {{< key "Space" >}} | Open the structure-row form for the column. |
| {{< key "i" >}} | Add a column (or index) at the end, with the cursor on its name. |
| {{< key "Delete" >}} / {{< key "Backspace" >}} | Mark the selected rows (or the row under the cursor) for dropping. A row added in this session is removed outright. |
| {{< key "M-Up" >}} / {{< key "M-Down" >}} | Move the column up or down where the engine can place columns; elsewhere the status line says `postgres cannot reorder columns`. |
| {{< key "c" >}} | Copy the selected cells as plain text. |
| {{< key "m" >}} | The row menu. |

The `extra` cell is not edited in place: change it in the structure-row form.

## Columns

### Add a column

A new column starts as `new_column`, the engine's first data type (`integer` on PostgreSQL), nullable, not in the primary
key, no default. Give it a name and a type at least. On PostgreSQL a new column whose `extra` is `serial` is created as
`serial` / `bigserial` / `smallserial`, so it gets its own sequence.

### Rename a column

Change the name. The editor remembers the column's original name, so the plan renames it
(`ALTER TABLE … RENAME COLUMN coupon TO coupon_code` on PostgreSQL, `CHANGE COLUMN` on MySQL) instead of dropping and
adding it: the data stays.

### Change the type

Type the new type, or pick one:

- **Terminal**: {{< key "C-d M-Enter" >}} on `data_type` opens the **Data type** picker (type to filter). The list is the
  engine's own (`db.DataTypesOf`): for PostgreSQL `integer`, `bigint`, `smallint`, `serial`, `bigserial`, `text`,
  `varchar(255)`, `char(1)`, `boolean`, `numeric(10,2)`, `real`, `double precision`, `money`, `date`, `time`,
  `timestamp`, `timestamptz`, `interval`, `uuid`, `json`, `jsonb`, `bytea`, `inet`, `cidr`, `macaddr`, `xml`; MySQL and
  MariaDB from `tinyint` to `set('a','b')`; ClickHouse from `UInt8` to `LowCardinality(String)`; Cassandra's CQL types
  including `list<text>` and `map<text,text>`; DynamoDB's `S`, `N`, `B`. The last entry, **Other…**, asks for any type
  (`e.g. varchar(64), numeric(12,4)`).
- **Web**: the Type input suggests the same list as you type; you can type anything else.

{{< ui tui="structure-editor-type-picker" >}}The Data type picker on `data_type`, filtered by typing; Other… takes any type.{{< /ui >}}

On PostgreSQL a type change is `ALTER COLUMN total TYPE numeric(12,2) USING total::numeric(12,2)`, so a conversion
the server can cast works in one step.

### Nullability

Toggle `is_nullable` (Null in the web). PostgreSQL gets `SET NOT NULL` / `DROP NOT NULL`; MySQL rewrites the column with
`MODIFY COLUMN`. Making a column `NOT NULL` fails on the server when it holds NULLs: fill them first (see
[Bulk update](../../editing-data/)).

### Default

Type an SQL expression, or pick one:

- **Terminal**: {{< key "C-d M-Enter" >}} on `column_default` opens **Default value**: `(none)`, `NULL`,
  `CURRENT_TIMESTAMP`, `0`, `''  (empty string)`, `true`, `false`; on PostgreSQL also `gen_random_uuid()`, `now()` and a
  `nextval('schema.sequence')` entry for every sequence in the sidebar; then **Advanced…**, which asks for any
  expression.
- **Web**: the Default input suggests `NULL`, `''`, `0`, `true`, `false`, `CURRENT_TIMESTAMP`, `now()`,
  `gen_random_uuid()` and `uuid()`; empty means no default (the placeholder says `none`).

{{< ui tui="structure-editor-default-picker" >}}The Default value picker, with PostgreSQL's functions and one entry per sequence.{{< /ui >}}

The default is an expression, not a value: write `'pending'` with quotes for a string. MySQL needs non-literal
defaults in parentheses; webtuidb adds them (`DEFAULT (uuid())`) and leaves literals, `NULL`, `CURRENT_TIMESTAMP` and
`now()` as they are. Removing a default is `DROP DEFAULT` (PostgreSQL) or `REMOVE DEFAULT` (ClickHouse).

### Primary key

Toggle `primary_key` on each column that belongs to the key (PK in the web). The key is the set of ticked columns in
table order. When it changes, the plan drops the old constraint (under its real name, read from the catalog) and adds
`PRIMARY KEY (…)` after the columns are altered. ClickHouse refuses with `ClickHouse sorting key cannot be altered`,
Cassandra and DynamoDB with their own messages (a key cannot change after creation there).

### Extra, identity and generated columns

`extra` carries what the engine adds after the type: `serial`, identity and generated clauses, MySQL's
`auto_increment`, `on update …` and `COMMENT '…'`. On MySQL and MariaDB the editor rebuilds a changed column from type,
nullability, default and extra, so its comment, its collation (part of the type when it differs from the table's) and
its generated expression survive a `MODIFY` or `CHANGE`. On PostgreSQL identity and generated columns cannot be
altered here: changing their `extra` stops the plan with
`column "id": editing identity/generated attributes is not supported`.

### Move a column

On engines that can place columns (MySQL, MariaDB, ClickHouse; capability `ReorderColumns`) you can change the order:
{{< key "M-Up" >}} / {{< key "M-Down" >}} or **Move up** / **Move down** in the row menu in the terminal, the ▲ ▼ buttons
or the ⠿ drag handle in the web. Several moves and other edits are planned together; the plan adds `FIRST` or
`AFTER col` only to the columns whose position really changed. PostgreSQL and the other engines have no such clause, so
the arrows are not offered there.

### Drop a column

Mark the row (terminal {{< key "Delete" >}}, web **Delete**). The plan drops it with `DROP COLUMN`, after dropping the
indexes, keys and constraints that depend on it, so the order is valid.

{{< callout type="warning" >}}
Dropping a column deletes its data. Preview the plan and keep a backup ([Backup](../../import-export/)) before you send it
on anything that matters.
{{< /callout >}}

## The structure-row form

Edit every attribute of one column in a dialog instead of cell by cell.

{{< ui tui="structure-editor-row-form" web="structure-editor-row-form" >}}The structure-row form on `total`: the terminal form edits the row as pending cells; the web dialog previews one ALTER right away.{{< /ui >}}

| | Terminal ({{< key "Space" >}} on a row) | Web ({{< key "C-d Space" >}} on a row, or a double click on a row of the read-only page) |
|---|---|---|
| Title | `Column total` | `Column total` |
| Fields | Name, Data type, Nullable, Default (`empty: none`), Primary key, Extra | Name, Type, Nullable (`Allows NULL`), Default (`none; an SQL expression`) |
| Primary key and extra | editable | shown as a note (`Primary key. Extra: serial. These are not changed here.`) |
| Result | the grid cells change and stay pending with the rest of your edits | **Preview statement** plans this one change and opens the send dialog |
| Validation | `a column needs a name and a data type` | the button stays disabled until name and type are set and something changed |

The terminal form's hint reminds you that the foreign key is edited with {{< key "Enter" >}} on its cell. In the web,
{{< key "C-d Space" >}} without a selected column says `Click a column in the structure first`.

## Foreign keys

{{< ui tui="structure-editor-fk-form" web="structure-editor-fk" >}}The foreign key of `orders.customer_id`: referenced table and columns, ON UPDATE and ON DELETE.{{< /ui >}}

**Terminal**: {{< key "Enter" >}} (or {{< key "C-d M-Enter" >}}) on the `foreign_key` cell opens **Foreign key on customer_id**:

| Field | Values |
|---|---|
| Referenced table | the tables of the current schema; a referenced table elsewhere is kept in the list |
| Referenced columns | comma-separated, `id` by default |
| On update, On delete | `NO ACTION`, `RESTRICT`, `CASCADE`, `SET NULL`, `SET DEFAULT` |
| Delete this key | shown when the column already has a key; ticked, the key is dropped |

A new key covers the one column the cursor is on. Editing an existing multi-column key keeps its columns and name. An
empty referenced column list is refused with `name the referenced column(s)`. The cell then shows
`→ customers(id)  upd:NO ACTION del:CASCADE` and the change is pending.

**Web**: the **Foreign keys** section lists every key with Name, Columns, References (suggests the tables of the
schema), Ref. columns, On update and On delete, and **Add foreign key**. Multi-column keys are typed as `a, b`. Before
planning, the web checks that every key has columns, a referenced table and referenced columns
(`Every foreign key needs columns, a referenced table and referenced columns`), that the counts match
(`Foreign key fk_x: 2 column(s) cannot reference 1`) and that the columns exist
(`Foreign key fk_x names a column that is not in the table`). A key without a name gets one from the server.

A changed key is dropped and added again (on MySQL the drop is its own `ALTER TABLE`, since a key cannot be dropped
and re-added under the same name in one statement). You can also add a key by dragging between tables in the
[ER diagram](../../tools/er-diagram/).

{{< callout type="info" >}}
The foreign-key editor exists only where the engine has foreign keys (capability `ForeignKeys`). SingleStore documents
no enforced foreign keys, so its Structure view has no `foreign_key` column and no Foreign keys section, and the
terminal says `this engine has no foreign keys`. ClickHouse, Cassandra, DynamoDB, MongoDB and Redis have none either.
Redshift keeps its (informational) foreign keys but has no indexes, so it has no Index view.
{{< /callout >}}

## Indexes

In the Index view (terminal) or the Indexes section (web):

- **Add**: {{< key "i" >}} in the terminal adds `orders_idx3` (the table name, `_idx` and the row number), not unique; the
  web's **Add index** adds an empty row. Give it a name and the columns, comma separated.
- **Unique**: toggle `is_unique` / the Unique checkbox.
- **Method and condition**: the web's Algorithm (`btree` by default) and Condition (`WHERE …`, a partial index) inputs
  are editable. In the terminal these two cells are shown but not editable: a new index uses the server's default
  method, and an existing one keeps its own.
- **Drop**: mark the row; the plan uses `DROP INDEX`, or `DROP CONSTRAINT` for an index behind a `UNIQUE` or `EXCLUDE`
  constraint.
- **Change**: a changed index is dropped and created again.

Engine notes: on PostgreSQL an entry that is not a column of the table is an expression and is written as is
(`lower(email)`). On MySQL the Algorithm `FULLTEXT` or `SPATIAL` makes a `FULLTEXT INDEX` / `SPATIAL INDEX`, and a column
may carry a prefix length or direction (`name(10)`, `placed_at DESC`) or be a functional part in parentheses. On
Cassandra an index covers exactly one column (`KEYS(m)` and `VALUES(m)` work for collections) and Algorithm `SAI` makes
a storage-attached index (`SASI indexes are not offered: use SAI`). On DynamoDB the Algorithm is `GSI` (default) or
`LSI`, the Condition is the projection (`ALL`, `KEYS_ONLY`, `INCLUDE a, b`), and an index cannot be unique; local
indexes exist only from table creation. ClickHouse indexes are not edited here.

The web refuses an index that names a column the table does not have (`Index x names a column that is not in the
table`), except for entries that are expressions and rows you did not touch.

## Rename the table

**Web**: change the Name in the Overview and preview. The rename is the last statement of the plan (everything before
it uses the old name). After it is sent the tab still points at the old name, so the web says
`After the rename, reopen the table as orders_v2 from the sidebar`.

**Terminal**: the Structure view has no name field; use **Rename…** from the sidebar menu
([Table operations](../table-operations/#rename)), which closes the table's tabs.

## The row menu

{{< ui tui="structure-editor-menu" web="structure-editor-menu" >}}The structure row menu. Move up and Move down appear only on engines that can reorder columns.{{< /ui >}}

Both UIs build it from the same list (`menus.Structure`):

| Entry | Does |
|---|---|
| Add | `add_row`: a new column (or index). |
| Delete | `delete_rows`: mark the row for dropping. |
| Set value / toggle | `set_value`: in the terminal the type or default picker, the toggle, or the foreign key form, depending on the cell; in the web it toggles Null on a column row. |
| Move up, Move down | Only with `ReorderColumns` (MySQL, MariaDB, ClickHouse). |
| Copy as… | Copy the definitions; see [Copy the structure](../structure-view/#copy-the-structure). |

## From edits to a plan

When you preview or commit, the edited grid becomes a whole-table definition (columns with their original names,
primary key, indexes, foreign keys). The engine's planner compares it with the table as it is and writes the
statements. In the web the draft is posted to `POST /api/sessions/{id}/structure/plan` as `{old, new}`; the server
re-reads the table from the catalog instead of trusting the browser's copy of "old", then plans with the same code
the terminal calls.

Before planning, both UIs check the draft:

| Message | Cause |
|---|---|
| `every column needs a name and a data type` | an empty name or type |
| `duplicate column name "x"` | two columns with the same name (case-insensitive) |
| `a table needs at least one column` | every column marked for dropping |
| `every index needs a name and at least one column` | an empty index row |
| `invalid data type "…" for column "…"` | web only: the server accepts a type name of letters, digits, `_` and spaces, an optional `(n)` or `(n,m)` and an optional `[]`, at most 64 characters, so a type cannot carry SQL |
| `no structure changes` | nothing differs from the table as loaded |

On PostgreSQL the statements come in an order that is always valid: changed foreign keys, the primary key and changed
indexes are dropped first; then columns are dropped, renamed, altered and added; then the primary key, indexes and
foreign keys are added; the table rename comes last. MySQL and MariaDB get one `ALTER TABLE` with every clause (plus a
separate one for dropped foreign keys); on a system-versioned MariaDB table each statement is prefixed with
`SET STATEMENT system_versioning_alter_history=KEEP FOR`, which MariaDB requires.

### Preview, safe mode and sending

{{< ui tui="structure-editor-preview" web="structure-editor-preview" >}}The planned statements before they are sent: `Preview: 3 statement(s)` in the terminal, `Send 3 statements?` in the web.{{< /ui >}}

- **Terminal**: {{< key "C-d P" >}} opens `Preview: N statement(s)` with every statement; {{< key "Enter" >}} commits from
  there. {{< key "C-d s" >}} commits without the preview: the safe-mode level decides whether it asks first (the
  confirmation lists the statements; levels that need the password ask for it). After success the status line says
  `structure of orders saved`, and the sidebar, structure and rows reload.
- **Web**: **Preview changes** always opens the send dialog (`Send 3 statements?`), with the statements, **Open in
  window**, the safe-mode fields when the level needs them, **Cancel** and **Send changes**. After success the draft
  reloads from the server.

Every statement is written to the console and to the connection's history. See [Safe mode](../../safety/).

{{< callout type="warning" >}}
**MySQL, MariaDB and SingleStore run DDL outside a transaction.** They commit implicitly around every `CREATE`,
`ALTER`, `DROP`, `RENAME` and `TRUNCATE`, so webtuidb sends a batch that holds DDL one statement at a time and stops at
the first error. The statements before it stay applied, and the error says so:
`statement 2 (…): … (1 of 3 statements were already applied and stay applied: mysql cannot roll back DDL)`. The
terminal's error dialog is titled `Statement failed (earlier statements in the batch may have been applied)`. On
PostgreSQL a structure plan runs in one transaction and a failure leaves the table as it was (the terminal still uses
the cautious title for any batch with DDL, because some PostgreSQL DDL, such as `CREATE DATABASE` or `CONCURRENTLY`,
runs outside a transaction). ClickHouse, Cassandra and DynamoDB have no transactions; the web's send
dialog warns there: `This database applies statements one at a time. If one fails, the ones before it stay applied.`
{{< /callout >}}

## Engine differences

{{< tabs items="PostgreSQL,MySQL / MariaDB,ClickHouse,Cassandra,DynamoDB" >}}
{{< tab >}}
Everything on this page. Type changes use `USING col::type`. Identity and generated columns cannot be altered. A column
whose default is `nextval()` of a sequence it owns is shown with `extra` = `serial`. Greenplum and Vertica use the same
planner; Redshift too, without indexes.
{{< /tab >}}
{{< tab >}}
One `ALTER TABLE` per plan; `CHANGE COLUMN` for renames, `MODIFY COLUMN` for other changes, `FIRST` / `AFTER` for
moves. Comments, collations, `on update` and generated expressions are kept. DDL is not transactional (see the warning
above). SingleStore has no foreign keys.
{{< /tab >}}
{{< tab >}}
Columns can be added, dropped, renamed, retyped, given or relieved of a default, and moved (`FIRST` / `AFTER`).
Nullable is the `Nullable(T)` wrapper: ticking Null writes `Nullable(String)`, unticking unwraps it (`Array`, `Map`,
`Tuple` and `LowCardinality` are never wrapped). The sorting key (primary key) cannot change, indexes are not edited
here, and there are no foreign keys. A table rename is `RENAME TABLE`.
{{< /tab >}}
{{< tab >}}
You can add and drop regular columns (with `STATIC` when `extra` says `static`) and rename primary key columns. A
column's type, its key role, NOT NULL and static setting cannot change; there are no defaults and no foreign keys; the
primary key and the table name are fixed. Each refusal names the alternative, for example
`column total: Cassandra cannot change a column's type (int to bigint): add a new column, copy the data, drop the old one`.
Indexes are dropped before and created after the column changes, because Cassandra refuses to drop an indexed column.
{{< /tab >}}
{{< tab >}}
A DynamoDB table declares only its key attributes and index keys (types `S`, `N`, `B`). On an existing table you can add
or drop global secondary indexes, and add an attribute only as the key of a new index. Attributes cannot be renamed,
retyped or dropped, the primary key cannot change, and the table cannot be renamed; each refusal says why (`types
belong to the items, not the table`).
{{< /tab >}}
{{< /tabs >}}

MongoDB and Redis have no table structure to edit.
