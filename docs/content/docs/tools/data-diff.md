---
title: Data diff
description: Compare the rows of two tables by key and get the INSERT, UPDATE and DELETE statements that make the first table hold the same rows as the second, previewed and sent through safe mode, in the terminal and in the browser.
weight: 70
---

Data diff compares two tables row by row, matched on a key, and plans the changes that make the first table (FROM)
hold exactly the rows of the second (TO): inserts for rows only in TO, updates for rows whose values differ, deletes
for rows only in FROM. Use it to sync a lookup table from a staging copy, or to see how an import differs from what
is there.

{{< ui tui="tools-data-diff" web="tools-data-diff" >}}Comparing `customers` with `customers_import` in the `docs_tools_v1` schema.{{< /ui >}}

## How to open it

Data diff has no default key binding.

| What | Terminal | Web |
| --- | --- | --- |
| Open it | {{< key "C-d p" >}} → **Data diff** | {{< key "C-d p" >}} → **Data diff** |
| Bind a key | `data_diff = "…"` under `[keys]` in `config.toml` ([key bindings](../../configuration/key-bindings/)) | the same binding applies |

Both tables come from the schema shown in the sidebar; switch the sidebar's schema first to diff tables of another
schema. The terminal needs at least two tables there (`data diff needs two tables in the sidebar`).

## The form

| Terminal field | Web field | Meaning |
| --- | --- | --- |
| **FROM table** | **Change table** | the table that receives the changes |
| **TO table** | **to match** | the table whose rows FROM should end up with (the web list leaves out the FROM table) |
| **Key columns** | **Key columns** | comma separated; empty: FROM's primary key |

The terminal labels tables `schema.table`; picking the same table twice says `pick two different tables`.

## What is compared

- **Columns**: the columns both tables have, by name, in FROM's order. Columns only on one side are ignored. No
  column in common: `the tables have no column in common`.
- **Key**: the key columns you typed, else FROM's primary key. When FROM has no primary key, or a key column is
  missing on either side, every common column becomes the key, so a changed value shows up as one delete plus one
  insert instead of an update.
- **Values** are compared as text, so `1` and `1.0` differ while a number and its string form are equal; NULL equals
  only NULL.
- **Duplicates**: two rows of one side with the same key (or two identical rows when every column is the key) would
  collapse into one, so the diff stops: `duplicate rows share a key, so they cannot be compared; choose a unique key`.

The result is a list of changes on FROM:

| Change | When | Statement |
| --- | --- | --- |
| insert | the key is only in TO | `INSERT` with every common column of the TO row |
| update | the key is in both and a value differs | `UPDATE … SET` only the changed columns `WHERE` the key |
| delete | the key is only in FROM | `DELETE … WHERE` the key |

The statements come from the same planner as grid edits, so they are written for the engine (quoting, literals,
MongoDB and Redis included).

## The result

{{< ui tui="tools-data-diff-preview" web="tools-data-diff-preview" >}}One insert, three updates and one delete to make `customers` match `customers_import`.{{< /ui >}}

**Terminal.** A preview titled `Data diff: customers -> customers_import (5 statements)` lists the statements.
{{< key "Enter" >}} (or {{< key "a" >}}) applies, {{< key "Esc" >}} closes without sending. When the batch holds a
`DELETE` (or more than 100 statements), applying first asks you to type the number of statements
(`1 statement delete rows or tables. This cannot be undone.`). The statements then go through
[safe mode](../../safety/) and run like a commit; the console and the history record them.

**Web.** **Plan changes** opens the usual **Send n statements?** dialog with **Send changes**, **Open in window** and
**Cancel**; sending goes through the safe-mode gate.

When the tables already hold the same rows: the terminal says `nothing to change: the two sides are the same`, the
web dialog `The tables hold the same rows.`

The changes are not added to a grid's pending changes; to change them before sending, copy the statements from the
preview into the SQL editor.

{{< callout type="warning" >}}
Rows that are only in FROM are deleted. Check the key: a wrong key turns every row into a delete plus an insert.
{{< /callout >}}

## Limits

Each side is read whole, sorted by the key, up to the connection's row limit (the row limit of the grid, set with
{{< key "C-d L" >}} or `[general] row_limit`):

- terminal: a table with more rows than the limit stops the diff with
  `orders has more than 300 rows (the row limit): raise it to diff`; a limit of 0 reads everything;
- web: a table with as many rows as the limit or more stops it with
  `a table has 300 rows or more, the row limit: raise the limit or diff a smaller table`; a limit of 0 means 100 000.

A table is never cut silently: unread rows would otherwise look missing and be planned as deletes.

The terminal runs the diff as a background job (`data diff: reading customers…`), listed in the jobs centre and
cancelled with {{< key "C-d ." >}}; it gives up after 10 minutes. The web plans it in one request.

Both tables are read on the current connection; to compare across connections, export one side and import it into a
scratch table first.
