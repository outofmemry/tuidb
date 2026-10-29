---
title: Sorting
description: Sort a table by one column or several, from a key, a header click or the multi-column sort dialog, and how the sort is remembered per table, in the terminal and in the browser.
weight: 20
---

Sorting a table tab asks the database for the rows in that order (`ORDER BY` on SQL engines), so the
sort covers the whole table, not just the page on screen, and paging walks through the sorted rows.

{{< ui tui="browsing-sort" web="browsing-sort" >}}`orders` sorted by `total`, descending. The sorted column's name turns to the accent colour with `▼`.{{< /ui >}}

## How to sort

| What | Terminal | Web |
| --- | --- | --- |
| Sort by the cursor column | {{< key "s" >}} (plain key in the grid): ascending, then descending, then off | — (the browser has no plain key for it; click the header) |
| Sort by a column with the mouse | click its header: ascending, descending, off | click its header: ascending, descending, off |
| Add a column to the sort | — (use the dialog) | shift-click its header |
| Column menu | {{< key "C-d M-H" >}} (`column_menu`) → **Sort ascending**, **Sort descending**, **Remove sort** | right-click the header, or {{< key "C-d M-H" >}} |
| Several columns at once | {{< key "C-d M-o" >}} (`sort_columns`) | {{< key "C-d M-o" >}}, the column menu, or **⋯ → Sort by several columns…** |

{{< key "s" >}} in the grid is a plain key, not a prefix action, so it cannot be rebound; prefixed,
{{< key "C-d s" >}} is `commit`. In the browser the plain {{< key "s" >}} does nothing in the grid,
which is why the header click and the dialog are the ways to sort there.

### One column

A click (or {{< key "s" >}}) on a column that is not the first sort key sorts by that column alone,
ascending. The next press on the same column sorts descending, the third removes the sort. Every change
goes back to the first page and puts the cursor on the first row.

In the browser, shift-click works on a list: a column not in the sort is added at the end, ascending; a
second shift-click turns it descending (and moves it to the end); a third removes it. A plain click
always replaces the whole sort with that one column.

The column menu's **Remove sort** clears the whole sort in the terminal; in the browser it takes only
that column out of the sort.

### Several columns

{{< ui tui="browsing-sort-multi" web="browsing-sort-multi" >}}The multi-column sort dialog: a checklist followed by a direction form in the terminal, a list of column and direction rows in the browser.{{< /ui >}}

**Terminal.** {{< key "C-d M-o" >}} opens **Sort by (ticked columns, in this order)**, a checklist of
every column. Columns in the current sort come first, already ticked, in their sort order; the rest
follow in table order.

| Key | Action |
| --- | --- |
| {{< key "j" >}} / {{< key "k" >}}, arrows | move |
| {{< key "Space" >}} or {{< key "x" >}} | tick or untick |
| {{< key "a" >}} | tick all (or untick all when all are ticked) |
| {{< key "A" >}} | untick all |
| {{< key "Enter" >}} | go on |
| {{< key "Esc" >}} | cancel |

The ticked columns become the sort keys in the order you ticked them (the columns already in the sort
keep their place at the top). {{< key "Enter" >}} then opens **Direction**, one line per ticked column
with `asc` or `desc`: {{< key "Tab" >}} moves between lines, {{< key "Space" >}} /
{{< key "Right" >}} / {{< key "Left" >}} switch the direction, {{< key "Enter" >}} applies. Unticking
every column and pressing {{< key "Enter" >}} removes the sort. With the columns still loading the
dialog does not open (`the columns are still loading`). A saved sort on a column that was renamed or
dropped is left out of the list.

**Web.** **Sort by several columns** shows one row per sort key: its position, a column drop-down, a
direction drop-down (**Ascending** / **Descending**) and **Remove**. **Add column** adds a row for the
first column not yet in the list. **Sort** applies the rows top to bottom (a column listed twice counts
once, at its first place); removing every row and pressing **Sort** removes the sort. **Cancel** or
{{< key "Esc" >}} closes it unchanged.

{{< ui tui="browsing-sort-multi-result" web="browsing-sort-multi-result" >}}Sorted by `status` ascending, then `total` descending: each sorted header carries its direction and its position.{{< /ui >}}

With more than one key both UIs number the marks: `status ▲1`, `total ▼2`.

## How the sort is remembered

The sort is saved per connection, database and table in `state.json` (see
[Files and folders](../../configuration/files-and-folders/)) as soon as you change it, and comes back
the next time you open the table, in either UI. The terminal and the browser read and write the same
entry, so a sort chosen in one shows up in the other.

- When a table has no saved sort, `[table] default_sort` decides: `none` (the default), `pk_asc` or
  `pk_desc` sort by the primary key columns once they are known. A table without a primary key opens
  unsorted.
- The browser saves only a sort you chose; the primary-key default is not written back. When you remove
  the sort in the browser it records that, so `default_sort` does not bring the key sort back next time.
- Without any sort, PostgreSQL and MySQL still page in primary-key order, so a row does not show up twice
  or go missing between pages.

## Sorting with pending changes

A sort reloads the rows, so unsent edits would be lost. When the grid has pending changes both UIs ask
**Discard pending changes?** first; answer no and nothing changes. See [Editing data](../../editing-data/).

## Engine notes

{{< callout type="info" >}}
- **SQL engines** sort on the server: `ORDER BY "status", "total" DESC`.
- **MongoDB** sorts with a sort document (`{status: 1, total: -1}`).
- **Redis** has no server-side order: a sorted key is read whole (up to 100,000 elements) and sorted in
  memory, numbers numerically and everything else as text. Bigger keys are cut at that cap.
- **Query results** in the SQL editor sort the loaded rows locally (NULLs first, numbers numerically,
  text case-insensitively), one column at a time; see [Results](../../sql-editor/results/).
{{< /callout >}}

## Settings

| Setting | Values | Effect |
| --- | --- | --- |
| `[table] default_sort` | `none` (default), `pk_asc`, `pk_desc` | sort a table that has no saved sort by its primary key |

See the [settings reference](../../configuration/settings-reference/).
