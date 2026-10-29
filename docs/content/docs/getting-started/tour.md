---
title: Ten-minute tour
description: A walk through the everyday actions on the demo shop database, with the terminal keys and the browser clicks for each step.
weight: 40
---

The screenshots use the demo "shop" database that ships with the documentation tooling (`docs/tools/seed.sh`); any PostgreSQL or MySQL database works the same way.

## 1. Open a table

Connect, then press {{< key "Enter" >}} on `orders` in the sidebar (or click it). The grid shows the first page of rows with the primary key marked, NULLs dimmed and numbers flush right. The status line tells you where you are: `rows 1–300 of 1200`.

{{< ui tui="browsing-grid" web="browsing-grid" >}}The orders table.{{< /ui >}}

## 2. Sort and page

Move to the `total` column and press {{< key "s" >}} to sort by it; press again to flip the direction. In the browser click the column header. {{< key "L" >}} (`page_settings`) sets the row limit and offset; {{< key "#" >}} counts the rows exactly. See [Sorting](../../browsing-data/sorting/).

{{< ui tui="browsing-sort" >}}Sorted by total.{{< /ui >}}

## 3. Filter

{{< key "C-d f" >}} opens the filter bar. Pick a column, an operator and a value (`status = paid`), and apply. {{< key "C-d x" >}} is the quick filter: it filters on the value under the cursor in one keystroke. The filter bar's SQL popover shows the `WHERE` clause that was generated. See [Filter bar](../../browsing-data/filter-bar/).

## 4. Edit a cell, then look before you send

Press {{< key "e" >}} on a cell and type a new value; {{< key "Enter" >}} opens the cell in a popover editor instead (JSON is pretty-printed, {{< key "C-s" >}} saves). The cell turns amber and keeps its old value underneath. Nothing has been sent yet.

{{< ui tui="editing-pending-marks" >}}Pending marks: an edited cell and a deleted row.{{< /ui >}}

{{< key "C-d P" >}} previews the exact `UPDATE` the commit would run. {{< key "C-d s" >}} sends it (and a connection in an alert or safe mode asks first); {{< key "C-d Backspace" >}} throws the edits away. For this tour, discard. See [Editing data](../../editing-data/).

## 5. Run a query

{{< key "C-d e" >}} opens a SQL tab. Type:

```sql
-- barchart x: month, y: revenue
SELECT date_trunc('month', placed_at)::date AS month, sum(total) AS revenue
FROM orders WHERE status <> 'cancelled' GROUP BY 1 ORDER BY 1;
```

{{< key "M-Enter" >}} runs everything in the tab; the result opens in its own tab under the editor, and the `-- barchart` comment turns it into a chart. See [SQL editor](../../sql-editor/).

## 6. Look at the structure

{{< key "C-d )" >}} switches the table tab to its structure (columns, types, defaults, keys), {{< key "C-d M-i" >}} to its indexes and {{< key "C-d M-d" >}} to the DDL; {{< key "C-d (" >}} returns to the data. See [Structure](../../structure/).

## 7. Jump anywhere

{{< key "C-d p" >}} opens Open Anything: type a few letters of a table, a saved query or an action and press {{< key "Enter" >}}. When you are done, {{< key "C-d w" >}} closes a tab and {{< key "C-d K" >}} opens another connection.

From here the [Workspace](../../workspace/), [Browsing data](../../browsing-data/) and [Editing data](../../editing-data/) sections cover every detail of what you just did.
