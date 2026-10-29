---
title: Data grid
description: The rows of a table in a ledger-style grid, with row numbers, NULL marks, paging, counts, column widths and the keys and mouse actions that move around it, in the terminal and in the browser.
weight: 10
---

Opening a table, view or materialized view shows its rows in the data grid: one page of rows at a time,
a row-number gutter on the left, the column names on top and a status line underneath. Everything else
in this section (sorting, filters, find, quick look, row detail) works on this grid.

{{< ui tui="browsing-grid" web="browsing-grid" >}}The `orders` table of the demo shop. The terminal marks the primary key with `⚷`, the browser with `◆`; both show `NULL` dim and italic.{{< /ui >}}

## How to open it

| What | Terminal | Web |
| --- | --- | --- |
| Open a table | highlight it in the sidebar and press {{< key "Enter" >}}, or {{< key "C-d p" >}} (`open_anything`) and type its name | click it in the sidebar, or {{< key "C-d p" >}} |
| Back to the rows from Structure, Index or DDL | {{< key "C-d (" >}} (`show_data`) | {{< key "C-d (" >}}, or pick **Data** in the view menu of the toolbar |
| Reload the rows | {{< key "C-d M-r" >}} (`reload_tab`) | {{< key "C-d M-r" >}}, or **Reload** in the toolbar |

Opening a table replaces the current table tab unless that tab has pending changes, in which case it
opens in a new tab. The workspace page covers tabs in general; this page covers what is inside a table tab.

### The views of a table tab

A table tab has several views. The data grid is the first; the others belong to
[Structure](../../structure/).

| View | Terminal | Web |
| --- | --- | --- |
| Data | {{< key "C-d (" >}} (`show_data`) | {{< key "C-d (" >}} or **Data** in the view menu |
| Structure | {{< key "C-d )" >}} (`show_structure`) | {{< key "C-d )" >}} or **Structure** |
| Index | {{< key "C-d M-i" >}} (`show_index`) | {{< key "C-d M-i" >}} opens Structure scrolled to **Indexes** |
| DDL | {{< key "C-d M-d" >}} (`show_ddl`) | {{< key "C-d M-d" >}} or **DDL** |
| Json, Tree (MongoDB, Redis, DynamoDB) | {{< key "C-d J" >}}, {{< key "C-d T" >}} | {{< key "C-d J" >}}, {{< key "C-d T" >}}, or **JSON** / **Tree** |

The terminal lists the views at the bottom left of the tab with their keys (`Data   Structure C-d )
Index C-d M-i   DDL C-d M-d`); the browser has a view drop-down in the toolbar. Json and Tree are
described in [Document views](../document-views/).

## Reading the grid

| What you see | Terminal | Web |
| --- | --- | --- |
| Row numbers | gutter on the left, counting on across pages (page 2 starts at 301) | sticky `#` column; the number restarts at 1 on every page |
| Primary key column | `⚷` before the name, in the warning colour | `◆` before the name, in the accent colour |
| `NULL` | the word `NULL`, dim italic | `NULL`, dim italic |
| Empty string | `''`, dim italic | `''`, dim italic |
| Pending `DEFAULT` or expression | `DEFAULT` / the expression text, dim italic | `DEFAULT`, dim italic |
| Numbers | flush right when the value arrives as a number (integers, floats); `numeric` values arrive as text and stay left | flush right when the column's type is numeric (`int`, `numeric`, `decimal`, `float`, `double`, `real`, `money`, `serial`…) |
| Booleans | keyword colour | plain text |
| Alternating rows | every other row shaded | every other row shaded |
| Current row | the whole row tinted, the cursor cell highlighted | the row highlighted with an accent bar in the gutter, the cursor cell outlined |
| Sorted column | name in the accent colour with `▲` / `▼` (numbered `▲1 ▼2` for several keys) | the same marks after the name |
| Long values | cut at the column width | cut with `…`; hover a cell to see up to 200 characters |
| Line breaks in a value | shown on one line | shown on one line |

Hovering a header in the browser shows the column's name, type, whether it is nullable, its default and
its key (`id`, `integer, not null, default nextval(...), primary key`) plus "Click to sort, shift-click
to add a column". The terminal shows the same line (`type · nullable · default · key`) at the top of the
[row detail](../row-detail/) pane for the column the cursor is on.

### JSON cells

With `[table] auto_pretty_json = true`, JSON objects and arrays in a cell are re-spaced so they read
better on one line (`{"a":1,"b":[1,2]}` shows as `{"a": 1, "b": [1, 2]}`):

- In the terminal a cell is re-spaced only when the spaced version still fits the column width.
- In the browser a cell is re-spaced only when the result is 20 characters or fewer.
- The row detail pane shows JSON indented over several lines in both UIs.

The setting is off by default. [Quick look](../quick-look/) always pretty-prints JSON, whatever the setting.

### Geometry cells

With `[table] decode_spatial = true` (the default), a binary cell that holds WKB or EWKB geometry shows
as WKT text (`POINT(13.4 52.5)`) instead of hex. The terminal caches the decoded text per cell. Set it
to `false` to see the raw bytes. Quick look draws the shape on a map in the browser.

### Read-only grids

The terminal names the grid read-only in its hints (`↵ quick look` instead of `↵ edit`) and the browser
shows a **read only** chip in the toolbar when the rows cannot be edited: a view, a materialized view, a
read-only connection, a table whose primary key column is hidden, or (in the browser) a table without a
primary key. Editing is covered in [Editing data](../../editing-data/).

## Moving around

The grid keys are plain keys, active while the grid has the focus.

| Key | Terminal | Web |
| --- | --- | --- |
| {{< key "j" >}} {{< key "k" >}} {{< key "h" >}} {{< key "l" >}}, arrows | one row or column | one row or column |
| {{< key "PgDn" >}} / {{< key "PgUp" >}} | one screen of rows | 20 rows |
| {{< key "Home" >}} / {{< key "End" >}} | first / last column | first / last column |
| {{< key "g" >}} / {{< key "G" >}} | first / last loaded row | — (use {{< key "C-d Up" >}} / {{< key "C-d Down" >}}) |
| {{< key "C-d Up" >}} / {{< key "C-d Down" >}} | first / last row of the page (`first_row`, `last_row`) | the same |
| {{< key "S-Down" >}}, {{< key "S-Right" >}}… | extend a rectangular selection | extend a rectangular selection |
| {{< key "Esc" >}} | drop the selection | drop the selection, then the row selection |
| {{< key "Space" >}} | show or hide the row detail pane | show or hide the row detail pane |
| {{< key "Enter" >}} | edit the cell in a popover (quick look when the grid is read-only) | open the cell popover (read-only when the grid is) |
| {{< key "/" >}}, {{< key "n" >}}, {{< key "N" >}} | [find](../find/), next, previous | the same |
| {{< key "s" >}} | [sort](../sorting/) by the cursor column (asc, desc, off) | — (click the header) |
| {{< key ">" >}} / {{< key "<" >}} | widen / narrow the cursor column by 2 cells | — (drag the header edge) |
| {{< key "ContextMenu" >}}, {{< key "S-F10" >}} | — | open the cell menu at the cursor |

The terminal also turns the mouse wheel into arrow keys: one tick scrolls three rows, a horizontal tick
moves one column. There is no "go to row number" command; page with the keys below or set an offset with
{{< key "C-d L" >}}.

### Mouse

| Action | Terminal | Web |
| --- | --- | --- |
| Click a cell | moves the cursor there | selects the row and the cell |
| Shift-click a cell | — | extends the rectangle from the cursor |
| Double-click a cell | same as {{< key "Enter" >}} | opens the cell popover (to edit, or read-only) |
| Ctrl-click (Cmd-click) or middle-click a cell | — | opens the cell read-only (quick look) |
| Click a header | sorts by that column: ascending, descending, off | the same; shift-click adds the column to the sort |
| Drag a header's right edge | resizes the column | resizes the column |
| Double-click a header's right edge | — | resets the column to its automatic width |
| Right-click a header | column menu | column menu |
| Right-click a cell | cell menu | cell menu |
| Right-click the filter bar | filter bar menu | — (the bar's buttons) |
| Click a row number | — | selects the row |
| Double-click below the last row | — | adds a row (editable tables) |
| Double-click beside the last header | — | adds a column in the structure editor (SQL tables) |

{{< ui web="browsing-grid-cell-menu" >}}Right-click on a cell in the browser opens the cell menu; the terminal opens the same menu on a right-click.{{< /ui >}}

The cell menu lists, in this order: Quick look, Set value…, Add row, Duplicate rows, Delete rows, Copy
rows, Copy rows as…, Copy selected cells, Copy selected cells as…, Copy all column values, Paste rows,
Paste into selected cells, Quick filter on this cell, Open the row a foreign key points to, Hide this
column, Show all columns, Sort by several columns…, Bulk update…, Row limit and offset…, Count rows
exactly, Export…, Import…. Every entry is also an action with its own key.

## Column widths

A column starts as wide as its name and the longest value among the first rows, within limits:

| | Terminal | Web |
| --- | --- | --- |
| Automatic width | name + 2 or the widest of the first 100 values, at most 40 cells, at least 4 | name or the widest of the first 200 values, 64 to 360 px |
| Keyboard | {{< key ">" >}} / {{< key "<" >}}: ±2 cells, 4 to 120 (not saved) | — |
| Mouse | drag the separator right of a header (4 to 120 cells) | drag the header edge (40 to 960 px); double-click it to reset |
| Saved | a drag is saved when you let go | a drag is saved half a second after you let go |

Widths dragged with the mouse are saved per table in `state.json` (under `widths`, in terminal cells)
and restored the next time the table opens, in either UI. The browser converts at 8 px per cell, so a
width set in one UI comes back about the same in the other. Widths set with {{< key ">" >}} /
{{< key "<" >}} last until the tab closes.

## The column menu

{{< ui tui="browsing-grid-column-menu" web="browsing-grid-column-menu" >}}The column menu on `status`: {{< key "C-d M-H" >}} in the terminal, a right-click on the header in the browser.{{< /ui >}}

Open it with {{< key "C-d M-H" >}} (`column_menu`) for the cursor column, or right-click a header in
either UI.

| Entry | What it does |
| --- | --- |
| Sort ascending / Sort descending | sort by this column alone |
| Remove sort | terminal: clears the sort; web: removes this column from the sort |
| Sort by several columns… | opens the [multi-column sort](../sorting/#several-columns) dialog |
| Filter with this column | opens the [filter bar](../filter-bar/) on a new filter for this column, cursor in the value |
| Hide this column | hides it (see [Columns](../columns/)) |
| Show all columns | brings back hidden columns (the browser lists it only while a column is hidden) |
| Copy column name | the name |
| Copy all column values | every loaded value, one per line |
| Copy column definition | `name type NOT NULL DEFAULT x PRIMARY KEY` as in a `CREATE TABLE` |

## Paging

The grid loads one page of rows. The page size is `[general] row_limit` (300 by default).

{{< ui tui="browsing-grid-page-settings" web="browsing-grid-page-settings" >}}{{< key "C-d L" >}} (`page_settings`) asks for the row limit and the offset.{{< /ui >}}

| What | Terminal | Web |
| --- | --- | --- |
| Next page | {{< key "C-d Right" >}} (`next_page`) | {{< key "C-d Right" >}} or **Next ›** |
| Previous page | {{< key "C-d Left" >}} (`prev_page`) | {{< key "C-d Left" >}} or **‹ Prev** |
| Limit and offset | {{< key "C-d L" >}}: the **Rows per page and offset** form | {{< key "C-d L" >}}: the **Row limit and offset** dialog, or **Rows per page** in the status bar |

- **Next page** does nothing when the page came back shorter than the limit (there is nothing after it);
  **Previous page** does nothing at offset 0.
- The limit must be a whole number of 1 or more and the offset 0 or more. The terminal form says
  `limit must be 1 or more, offset 0 or more`; the browser disables **Apply**, and the **Rows per page**
  box says `Rows per page must be a whole number of 1 or more.`
- The browser never fetches more than `row_limit` rows at once: a larger limit is cut to `row_limit` and
  the status bar says `capped at 300 per page by row_limit`. The terminal fetches the limit you typed.
- Changing the page, the limit, the sort, the filters or the hidden columns reloads the rows. With
  pending changes on the grid you are asked first: **Discard pending changes?** (the edits are lost if
  you go on).
- The cursor goes back to the first row when a different page or query loads, and stays where it was on a
  plain reload.

Query results have **Load more** instead of pages; see [Results](../../sql-editor/results/).

## The status line and the row count

The terminal shows, at the bottom right of the tab:

```
● 2 pending · 1 hidden col(s) · rows 1–300 of 1200 · 21ms ·  C-d L  pages
```

- `● n pending`: rows with uncommitted changes (only when there are some).
- `n hidden col(s)`: hidden columns (only when there are some).
- `rows a–b of n`: the rows on screen and the total. The total is `…` until the count arrives, and
  `≈n` when it is an estimate.
- the time the page took to load.
- `C-d L pages` when there is more than one page.
- a spinner and `loading` while rows load.

The browser's status bar shows **‹ Prev**, `1–300 of ≈1,200`, **Next ›**, **Count exactly** (when the
total is an estimate), `page 1 of 4`, the **Rows per page** box, `Loading…` while it fetches, and a
**stopped at the row limit** chip when `row_limit` cut the page short.

### Estimated and exact counts

Counting every row of a large table is slow, so the total may come from the planner's statistics:

- With no filter, the count is first asked as an estimate (PostgreSQL `reltuples`, MongoDB's estimated
  document count). A table that was never analysed is counted exactly.
- In the terminal, when the estimate is below `[general] estimate_count_over` (1,000,000 by default), the
  grid counts exactly right away, so small tables always show an exact number.
- In the browser, a table without filters keeps the estimate (shown `≈`) until you press
  **Count exactly**. With filters, the count is exact unless the table's estimate is above
  `estimate_count_over`, in which case the filtered count is estimated too.
- With filters, the terminal always counts exactly.
- {{< key "C-d #" >}} (`count_exact`) runs a real `COUNT(*)` in either UI. When the count is already
  exact the terminal says `the count is already exact: 1200`; while counting it says `counting rows…`.

Set `estimate_count_over = 0` to turn the threshold off (the terminal then keeps estimates as they are).

### Loading and errors

While the first page loads the terminal shows `loading orders…` and the browser `Loading rows…` with a
progress bar. In the terminal {{< key "C-d M-x" >}} (`cancel_queries`) stops the load, and so does
{{< key "C-d ." >}}, which cancels while something runs and opens the process list otherwise; the
status says `load of orders cancelled`.

When the rows cannot be loaded the terminal shows `✗ Could not load orders` with the database's error.
If a filter is active it adds `A filter is active and may be the cause.`, opens the filter bar so you
can fix it, and offers {{< key "C-d f" >}} edit it, {{< key "M-c" >}} clear all and
{{< key "C-d M-r" >}} reload. The Structure and DDL views stay usable. The browser shows the error text
in red above the grid.

An empty page says `No rows match the filters` (terminal) / `No rows match these filters.` with a
**Clear the filters** button (web), or `No rows yet` / `This table has no rows yet.` with an
**Add a row** shortcut on an editable table.

## Item overview

**Item overview** is a summary of the table: estimated rows, size, columns with their types and
`PK` / `NOT NULL`, indexes and foreign keys.

{{< ui tui="browsing-grid-overview" web="browsing-grid-overview" >}}Item overview of `orders`: a text window in the terminal; in the browser the Overview block at the top of the Structure view.{{< /ui >}}

- Terminal: {{< key "C-d p" >}} and pick **Item overview** (a feature entry). It describes the open table,
  or the table highlighted in the sidebar when the sidebar has the focus; with neither it says
  `select or open a table first`. Size is what the sidebar shows, or `not reported`.
- Web: **Item overview** in Open Anything opens the Structure view, whose **Overview** block lists name,
  kind, number of columns, primary key, foreign keys, indexes and rows.

## What gets logged

Every page load is written to the console log as a comment line, for example
`-- browse public.orders where 1 filter(s) order by total desc limit 300 offset 0`, with its duration.
A failed load is logged with the error.

## Settings

| Setting | Default | Effect |
| --- | --- | --- |
| `[general] row_limit` | `300` | rows per page; also the browser's maximum page size |
| `[general] estimate_count_over` | `1000000` | above this many rows the total stays an estimate |
| `[table] alternating_rows` | `true` | shade every other row |
| `[table] auto_pretty_json` | `false` | re-space JSON cells in the grid, indent them in row detail |
| `[table] decode_spatial` | `true` | show geometry bytes as WKT |
| `[table] default_sort` | `none` | see [Sorting](../sorting/#settings) |

See the [settings reference](../../configuration/settings-reference/) for every setting.
