---
title: Editable results
description: A query result can be edited like a table when it selects from one table and includes the whole primary key.
weight: 50
---

A result tab is editable when the statement is a plain `SELECT … FROM table` over a single table and the select list contains every primary-key column. `db.EditableSelect` decides: joins, aggregates, `DISTINCT`, set operations, subqueries in the select list and expressions in place of key columns make a result read-only. The strict fallback rule is a `SELECT *` or an explicit column list straight from one table.

When a result is editable the same keys apply as in a table tab: edit a cell, {{< key "C-d i" >}} to add a row, {{< key "Delete" >}} (`delete_rows`) to delete, {{< key "M-Enter" >}} for Set Value, and {{< key "C-d s" >}} / {{< key "C-d P" >}} / {{< key "C-d Backspace" >}} to commit, preview and discard. The statements are built against the underlying table, not the query, so they are the same `UPDATE … WHERE pk = …` a table tab would send.

Two limits: **Load more** in the web makes the result read-only, because the extra rows come from a re-run inside a subquery; and in the web, delete, duplicate and Set Value act on one row at a time in a result tab. Re-running the query clears the marks, and a pinned result ({{< key "p" >}}) keeps its own edits while the next run opens beside it.
