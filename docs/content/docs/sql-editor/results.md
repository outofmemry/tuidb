---
title: Results
description: The results pane under the SQL editor, with a tab per statement, pinning, find, copy as, export, load more and the Message view, in the terminal and in the browser.
weight: 70
---

Every statement you run gets its own result tab in the pane under the editor. A tab holds the rows, the
row count and timing, any server messages, or the error. You can pin a tab so the next run keeps it,
search it, copy rows in seven formats, export it, fetch more rows, chart it, and edit it when it comes
from a single table.

{{< ui tui="results-tabs" web="results-tabs" >}}A file with three statements run with `run_all`: one result tab each, the first two named by their `-- name:` comments.{{< /ui >}}

## How to open it

The pane fills when you run something: {{< action "run_current" >}} or {{< action "run_all" >}} (see
[Query tab](../query-tab/)). To work inside it:

| What | Terminal | Web |
| --- | --- | --- |
| Move the focus into the results | {{< key "C-d M-j" >}} (`focus_down`), or click the pane | click a cell, or {{< key "C-d M-j" >}} |
| Back to the editor | {{< key "Esc" >}} (the first one clears a selection), or {{< key "C-d M-k" >}} | click the editor, or {{< key "C-d M-k" >}} |
| Hide or show the pane | {{< key "C-d `" >}} (`toggle_results`) | {{< key "C-d `" >}} |
| Result menu | right-click the pane | right-click a cell, the {{< key "ContextMenu" >}} key, or {{< key "S-F10" >}} |

Keys that act on the results (`chart`, `load_more`, `copy_rows`…) are bound in the `results` context,
so they work while the focus is in the pane. The terminal has no key for the result menu: it opens on a
right-click, and every entry in it also has its own key.

## The result tabs

Each statement makes one tab, in the order it ran. The tab is named by a `-- name:` (or `label:`,
`tab:`) comment on the line above the statement; without one the terminal calls it `Query 1`,
`Query 2`… and the browser shows the tab's number and the first 28 characters of the statement (hover a
tab for the whole statement).

| Mark | Terminal | Web |
| --- | --- | --- |
| Statement failed | `✗` before the name | red `✗` (green `✓` otherwise) |
| Pinned | `◆` before the name | `pin` |
| Pending edits in an editable result | — | `●` |

When a statement fails, the failed tab is the one shown after the run, not whatever ran after it. A run
of more than 100 statements keeps one **Summary** tab instead of a tab per statement (see
[Result modes](../result-modes/)).

Switch tabs with {{< key "[" >}} and {{< key "]" >}} (or {{< key "C-Left" >}} and
{{< key "C-Right" >}}) while the focus is in the terminal pane; in the browser, click the tab.

### Row count and timing

| | Terminal | Web |
| --- | --- | --- |
| A result with rows | `6 rows · 2.5ms` at the right of the tab bar | `6 rows in 9 ms` under the grid |
| The row limit cut it short | `⚠ limit reached` before the count | a `stopped at the row limit` chip and a **Load more** button |
| No rows (INSERT, UPDATE, DDL) | `✓ 3 rows affected · 1.2ms` in the pane | the Message view, with the count and the time |
| The whole run | `✓ 3 statements · 19.9ms`, `✗ failed` or `✗ 1 of 3 failed` on the run bar | the progress bar while it runs |

The time is the server round trip for that statement. The terminal rounds it the way you read it
(`455µs`, `1.5ms`, `1.23s`); the browser shows whole milliseconds.

### Pinning a result

A pinned result tab stays when you run again; every unpinned tab is replaced by the new run (unless
`[editor] result_mode = "append"`, where all tabs stay). Use it to keep a baseline while you change the
query.

{{< ui tui="results-pinned" web="results-pinned" >}}`By status` is pinned and survived a second run, which added its own tabs after it.{{< /ui >}}

| Terminal | Web |
| --- | --- |
| {{< key "p" >}} with the focus in the results (no prefix), or **Pin result** in the result menu | the **Pin result** / **Unpin result** button at the right of the result tabs, or **Pin result** in the cell menu |

The terminal confirms with `By status pinned: it stays when you run again`. This pin is about the
result tab only. Pinning the *connection* of a query tab, so `BEGIN` and `SET` carry over, is a
different feature, described in [Query tab](../query-tab/).

## Moving around and reading values

In the terminal, with the focus in the results:

| Key | Does |
| --- | --- |
| {{< key "j" >}} {{< key "k" >}} {{< key "h" >}} {{< key "l" >}} or the arrows | move the cursor; hold {{< key "Shift" >}} to grow a selection |
| {{< key "PgDn" >}} {{< key "PgUp" >}} | a page down or up |
| {{< key "Home" >}} {{< key "End" >}} | first or last column |
| {{< key "g" >}} {{< key "G" >}} | first or last row |
| {{< key "Enter" >}} | quick look on the cell (on an editable result: edit it) |
| {{< key "e" >}} | edit the cell inline (editable results only) |
| {{< key "/" >}} {{< key "n" >}} {{< key "N" >}} | find, next match, previous match |
| {{< key "c" >}} | copy the selected cells as plain text |
| {{< key "y" >}} | copy the selected rows as CSV |
| {{< key "m" >}} | switch between the rows and the Message view |
| {{< key "p" >}} | pin or unpin this result tab |
| {{< key "Esc" >}} | clear the selection, then go back to the editor |

The browser grid takes the same plain keys: arrows or {{< key "h" >}} {{< key "j" >}} {{< key "k" >}}
{{< key "l" >}}, {{< key "Shift" >}} to select, {{< key "PgUp" >}} / {{< key "PgDn" >}} (20 rows),
{{< key "Home" >}} / {{< key "End" >}}, {{< key "Enter" >}} to open the cell, {{< key "/" >}}
{{< key "n" >}} {{< key "N" >}} to find, {{< key "Esc" >}} to clear the selection. Clicking a column
header sorts the loaded rows (ascending, descending, off); {{< key "Shift" >}}-click adds a column to the
sort.

### Quick look

{{< key "Enter" >}} on a cell (or **Quick look** in the cell menu) opens the value in the quick look
viewer: long text wrapped, JSON as a tree, binary as a hex dump, geometry as WKT, images previewed in the
browser. On a read-only result it only shows the value; on an [editable result](../../editing-data/editable-results/)
the same key edits the cell. Quick look itself is described in [Browsing data](../../browsing-data/).

{{< ui tui="results-quick-look" web="results-quick-look" >}}Quick look on a `revenue` cell of the read-only "By status" result.{{< /ui >}}

The "Latest orders" result in the same file reads one table with its key, so it is editable: there
{{< key "Enter" >}} opens the **Edit placed_at** editor instead (the footer then shows `C-d s commit`).

## Find in results

{{< key "/" >}} in the results (or {{< action "find" >}} while the focus is there) searches the loaded rows
of the current tab.

{{< ui tui="results-find" web="results-find" >}}Find in this result: text, column, match operator and Match case.{{< /ui >}}

| Field | Terminal (dialog "Find in this result") | Web (find bar above the grid) |
| --- | --- | --- |
| Text | **Text** | the **Find** box |
| Column | **Column**: `(any column)` or one column | **Any column** or one column |
| Match | **Match**: contains, not contains, has prefix, has suffix | the same four |
| Match case | **Match case**, or {{< key "M-c" >}} in the dialog | the **Match case** box |
| Next / previous | {{< key "Enter" >}} finds, then {{< key "n" >}} / {{< key "N" >}} | {{< key "Enter" >}} / {{< key "S-Enter" >}}, **Next** / **Prev**, or {{< key "n" >}} / {{< key "N" >}} in the grid |

The terminal's title shows the case setting (`Find in this result · match case off`). {{< key "n" >}} before
any search says `press / to search this result`. On an editable result the dialog adds **Replace with**: {{< key "M-r" >}} replaces the match under the
cursor and moves on, {{< key "M-a" >}} replaces them all (the browser has **Replace** and **Replace all**
buttons). Replacements are pending edits, nothing is sent until you commit, and "any column" leaves
primary-key cells alone. A search that finds nothing says `"x" is not in the loaded rows`. Find only sees
the rows that were fetched; use **Load more** or a `WHERE` for the rest.

## Copy and copy as

| Action | Terminal | Web | Copies |
| --- | --- | --- | --- |
| {{< action "copy_rows" >}} | the selected rows, as CSV | the selected row, or every row when none is selected, tab-separated | whole rows |
| {{< action "copy_rows_as" >}} | the selected rows | the selected row, or every row | whole rows, in a format you pick |
| {{< action "copy_cells" >}} | the selected cells, plain | the cell under the cursor | values only |
| {{< action "copy_cells_as" >}} | the selected cells | the selected cells | a rectangle, in a format you pick |
| {{< action "copy_column" >}} | every loaded value of the cursor's column, one per line | the same | one column |

{{< ui tui="results-copy-as" web="results-copy-as" >}}Copy rows as: the seven formats.{{< /ui >}}

The **Copy as** list has seven formats in both UIs: Plain (the terminal says **Plain Text**; values
separated by tabs), JSON, HTML, Markdown Table, CSV, CSV with Header and INSERT Statement. CSV follows the `[csv]` settings (delimiter, quote, line break, quote mode);
INSERT names the table the result reads when it reads one, `result` (terminal) or `table_name` (web)
otherwise. Copying is also in the result menu: **Copy rows**, **Copy rows as…**, **Copy selected cells**,
**Copy selected cells as…**, **Copy all column values**.

## Column header menu

{{< action "column_menu" >}} with the cursor in a column (or a right-click on a header in the browser)
opens the menu for that column.

{{< ui tui="results-column-menu" web="results-column-menu" >}}The column menu on a result: sorting is local to the loaded rows; filtering and hiding belong to table tabs.{{< /ui >}}

| Entry | Terminal | Web |
| --- | --- | --- |
| Sort ascending / Sort descending / Remove sort | sorts the loaded rows | the same |
| Sort by several columns… | not on results: says to use the table's data tab | opens the sort dialog for the loaded rows |
| Filter with this column | opens Find on that column | greyed out: "add a WHERE to the query" |
| Hide this column / Show all columns | not on results: says to use the table's data tab | greyed out: "select fewer columns in the query" |
| Copy column name | yes | yes |
| Copy all column values | yes | yes |
| Copy column definition | the name and type | the name and type, plus `PRIMARY KEY` for a key column |

Neither UI can hide a column of a query result: select fewer columns in the query instead. Sorting here
never re-runs the statement.

## Export

{{< action "export" >}} with a result tab showing (or **Export…** in the result menu) opens the export
dialog for that result. Formats, file names and options are described in
[Import, export, backup](../../import-export/); what is particular to results:

{{< ui tui="results-export" web="results-export" >}}Exporting a result. With more than one result tab the terminal offers "All result tabs".{{< /ui >}}

The terminal's form is titled **Export result** and has **Format**, **Header row**, **NULL written as**,
**Line breaks**, **Gzip**, **All result tabs (one file each: name-1, name-2...)** (only with more than one
tab of rows) and **File** (`result-2026-10-29.csv`).

- **One tab.** The terminal writes the rows it already holds (it does not run the statement again), as a
  background job (`exported 6 rows to result-2026-10-29.csv`). The default file name comes from
  `[export] filename` with `{table}` set to `result`. The browser runs the statement again on the server
  with the tab's row limit and downloads the file.
- **All tabs.** The terminal's dialog has **All result tabs (one file each: name-1, name-2...)** when more
  than one tab has rows: every tab is written to its own file, `-1`, `-2`… before the extension, and you
  are asked before any of them is overwritten. The browser's **Export all tabs** button downloads one CSV
  per tab with rows, named `result-1.csv`, `result-2.csv`… by tab position; each statement runs again for
  its file.
- **The Message view.** In the terminal press {{< key "s" >}} while the Message view is showing: the form
  **Save message as text** asks for a file (`message.txt`). The browser's Message view has **Save as text**
  (downloads `message.txt`) and **Copy**.

## Load more

When a result stops at the row limit, {{< action "load_more" >}} (or **Load more** in the result menu, or
the **Load more** button under a browser result) fetches more rows.

{{< ui tui="results-load-more" web="results-load-more" >}}`SELECT * FROM orders` (1,200 rows) stopped at the row limit: `⚠ limit reached` in the terminal, the `stopped at the row limit` chip and **Load more** in the browser.{{< /ui >}}

| | Terminal | Web |
| --- | --- | --- |
| How | runs the statement again with twice the limit (1000, then 2000, 4000…) and swaps the result in place | asks for the next page: the statement runs inside a subquery, `SELECT * FROM (<statement>) AS "webtuidb_page" LIMIT n OFFSET m`, and the rows are appended. When the statement cannot be paged that way it runs again with twice the rows it has |
| Which statements | read-only ones only | read-only ones; paging needs a single plain `SELECT` or `WITH` without `LIMIT`, `OFFSET`, `FETCH`, `INTO`, `FOR`, `LOCK` or a top-level `ORDER BY` |
| Editable results | stay editable | a paged result becomes read-only (the server's copy holds the first page only); a re-run result stays editable |
| Pending edits | asks **Discard pending result edits?** first | asks "Loading more rows discards the pending changes in this result. Continue?" (**Discard and load**) |

The tab keeps its title, pin, chart and find state. Messages you may meet: `this result is complete` /
`Nothing more to load` (it was not cut), `a query is already running; cancel it first`,
`load more only re-runs read-only statements`, and in the browser "This statement needs confirmation, so
it cannot be re-run to load more" when safe mode would have to ask.

{{< callout type="info" >}}
Load more never continues a server cursor: each page, or each doubled fetch, runs the statement again from
the start. A slow query is slow once per page, and rows that changed in between can appear twice or not
at all.
{{< /callout >}}

## The Message view

The Message view shows what the server said besides rows: the command tag or the affected-row count, the
PostgreSQL notices raised by `RAISE NOTICE` (and `WARNING`, `INFO`, `LOG`, `DEBUG`), and the time.

{{< ui tui="results-message" web="results-message" >}}A `DO` block that raises two notices, in the Message view.{{< /ui >}}

| Terminal | Web |
| --- | --- |
| {{< key "m" >}} with the focus in the results switches between the rows and the messages (`(no messages)` when there are none). Under a grid the first message line is shown with `(+N more lines: m)`. {{< key "s" >}} saves the view as text. | the **Message** button at the right of the result tabs. A statement without rows opens on it. **Save as text** and **Copy** at the bottom. |

Notices also go to the console log (terminal) with a `-- NOTICE:` prefix.

### Errors

A failed statement's tab shows the error instead of rows. The terminal prints the first line in bold red
and indents `HINT`, `DETAIL`, `POSITION` and `WHERE` lines under it; the browser shows the server's text in
red. On PostgreSQL the message ends with where the server found the problem, counted inside that
statement: `at line 2, column 32`.

{{< ui tui="results-error" web="results-error" >}}A statement naming a column that does not exist, with the position PostgreSQL reported.{{< /ui >}}

Failed statements are written to the console log but not to the [history](../history/).

## Editable results

A result is editable when it provably maps to one table: a single-table `SELECT` (no join, `GROUP BY`,
`DISTINCT`, `UNION`, window or `WITH`) whose select list includes the table's whole primary key. Then you
can edit cells, add, duplicate and delete rows, Set Value, and commit with `C-d s`, as in a table tab. The
browser marks such a result `editable` under the grid and shows the editing buttons beside the result
tabs. The rules, the keys and the commit flow are in
[Editable results](../../editing-data/editable-results/).

Running again, loading more or closing the tab with pending result edits asks before dropping them.

## Charts and metrics boards

{{< action "chart" >}} (or **Chart** beside the result tabs in the browser) draws the result as a bar, line
or pie chart, and the chart view has **Add to Metrics Board**. See [Charts](../charts/).

## The result menu

Right-click a result (terminal or browser) for these entries, in this order. Entries marked editable
appear only on an editable result.

| Entry | Action |
| --- | --- |
| Quick look | `quick_look` |
| Set value… (editable) | `set_value` |
| Add row (editable) | `add_row` |
| Duplicate rows (editable) | `duplicate_rows` |
| Delete rows (editable) | `delete_rows` |
| Copy rows | `copy_rows` |
| Copy rows as… | `copy_rows_as` |
| Copy selected cells | `copy_cells` |
| Copy selected cells as… | `copy_cells_as` |
| Copy all column values | `copy_column` |
| Toggle chart | `chart` |
| Load more | `load_more` |
| Pin result | pin / unpin the tab |
| Export… | `export` |
| Add to metrics board… | terminal only; in the browser it is greyed out ("the web board form takes no query yet"): use **Add to Metrics Board** in the chart view |

## Details and limits

- Results hold the rows the server sent up to the row limit ({{< action "set_limit" >}}); find, sort,
  copy and charts work on those rows only.
- In the browser a run whose limit is over 1000 rows (or unlimited) streams: the first rows show while
  the rest arrive, and a cancel keeps the rows that arrived.
- The browser caps every run at the connection's row limit (`[general] row_limit` unless you picked
  another one), so the limit box only offers smaller values.
- The terminal remembers your row limit per connection; its default is 1000.

## Settings

| Setting | Effect |
| --- | --- |
| `[editor] result_mode` | `tabs` (default) replaces unpinned tabs on every run; `append` keeps them all |
| `[editor] return_on_error` | stop the run at the first failing statement |
| `[general] row_limit` | rows per page (default 300) and the browser's cap on a run; the terminal's editor limit ({{< action "set_limit" >}}, default 1000) is separate |
| `[general] query_timeout_secs` | after this long a running statement asks "keep waiting or cancel" |
| `[csv]` | delimiter, quote, line break and quote mode for CSV copy and export |
| `[export] filename`, `[export] header` | default export file name and CSV header row |
| `[table] alternating_rows`, `decode_spatial`, `image_preview` | grid stripes, geometry as WKT, image preview in quick look |

See the [configuration reference](../../configuration/) for every setting.
