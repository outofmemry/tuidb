---
title: Metrics boards
description: Dashboards of read-only query panels - numbers, tables, bar and line charts, inputs, drop-downs, buttons, importers and exporters - kept as TOML files, with $variables, refresh_on wiring, auto refresh and a connection binding, in the terminal and in the browser.
weight: 50
---

A metrics board is a dashboard: a set of panels, each one a read-only query shown as a number, a table or a
chart, plus interactive panels that set variables, run other panels, import or export. Boards are plain TOML
files, so you can write them by hand, keep them in version control and share them; both UIs read the same files
and can create, rename, duplicate and delete them.

{{< ui tui="tools-board" web="tools-board" >}}The **Shop overview** demo board: a status drop-down, revenue and order counts, orders per day, top products and the latest orders.{{< /ui >}}

## How to open it

| What | Terminal | Web |
| --- | --- | --- |
| Boards | {{< key "C-d m" >}} (`metrics`): the list of boards | {{< key "C-d m" >}}: the **Metrics** tab, showing the first readable board |
| From Open Anything | {{< key "C-d p" >}} → **Metrics board** | {{< key "C-d p" >}} → **metrics board** |
| From a chart | chart view of a result → {{< key "b" >}}, or the result menu → **Add to metrics board…** | chart view → **Add to Metrics Board** |

{{< ui tui="tools-board-list" >}}The board list in the terminal, with its keys.{{< /ui >}}

## The board file

Boards live in the `boards` folder of the data directory, one `*.toml` file per board:
`~/.local/share/webtuidb/boards/` (or `$XDG_DATA_HOME/webtuidb/boards/`). Every `*.toml` there is a board. A file
that does not parse is listed in the terminal as `error: …` under its file name; the web leaves it out (and says
`None of the boards can be read.` when no file parses).

This is the demo board, exactly as the app saved it (`boards/Shop overview.toml`):

```toml
Name = "Shop overview"
refresh_secs = 60
connection = ""

[[var]]
  Name = "days"
  Label = "Days"
  Default = "30"
  Options = ["7", "30", "90"]

[[panel]]
  Title = "Status"
  Kind = "dropdown"
  Query = ""
  Chart = ""
  var = "status"
  options = ["all", "new", "paid", "shipped", "delivered", "cancelled", "refunded"]
  default = "all"

[[panel]]
  Title = "Revenue"
  Kind = "number"
  Query = "SELECT to_char(coalesce(sum(total), 0), 'FM999G999D00') AS revenue FROM orders WHERE ($status = 'all' OR status::text = $status) AND placed_at > (SELECT max(placed_at) FROM orders) - interval '1 day' * $days"
  Chart = ""
  refresh_on = "Orders"

[[panel]]
  Title = "Orders"
  Kind = "number"
  Query = "SELECT count(*) FROM orders WHERE ($status = 'all' OR status::text = $status) AND placed_at > (SELECT max(placed_at) FROM orders) - interval '1 day' * $days"
  Chart = ""

[[panel]]
  Title = "Orders per day"
  Kind = "line"
  Query = "SELECT to_char(placed_at, 'MM-DD') AS day, count(*) AS orders FROM orders WHERE ($status = 'all' OR status::text = $status) AND placed_at > (SELECT max(placed_at) FROM orders) - interval '1 day' * $days GROUP BY 1 ORDER BY 1"
  Chart = "x: day, y: orders"

[[panel]]
  Title = "Top products"
  Kind = "bar"
  Query = "SELECT p.name, sum(i.quantity * i.unit_price) AS revenue FROM order_items i JOIN products p ON p.id = i.product_id JOIN orders o ON o.id = i.order_id WHERE ($status = 'all' OR o.status::text = $status) GROUP BY p.name ORDER BY revenue DESC LIMIT 6"
  Chart = "x: name, y: revenue"

[[panel]]
  Title = "Latest orders"
  Kind = "table"
  Query = "SELECT id, status, total, to_char(placed_at, 'YYYY-MM-DD') AS placed FROM orders WHERE ($status = 'all' OR status::text = $status) ORDER BY placed_at DESC LIMIT 6"
  Chart = ""

[[panel]]
  Title = "Recount"
  Kind = "button"
  Query = ""
  Chart = ""
  target = "Orders"

[[panel]]
  Title = "Export orders"
  Kind = "exporter"
  Query = "SELECT * FROM orders WHERE ($status = 'all' OR status::text = $status) ORDER BY placed_at DESC"
  Chart = ""
  format = "csv"
```

When you write a file by hand, key names are matched without regard to case (`name` and `Name` are the same),
empty keys can be left out, and TOML's `'''…'''` multi-line strings are fine for long queries. The app rewrites
the file in the form above when it saves it (rename, add panel, duplicate).

### Board keys

| Key | Meaning |
| --- | --- |
| `Name` | the board's name, shown in the lists. A board saved by the app is stored as `<Name>.toml` (`/`, `\` and NUL become `_`). |
| `refresh_secs` | auto refresh interval in seconds; `0` or missing: no auto refresh |
| `connection` | the saved connection the board runs on; empty: the connection you open it from (see [Connection](#connection)) |
| `[[var]]` | a board variable (any number) |
| `[[panel]]` | a panel (any number), shown in file order |

### Variable keys (`[[var]]`)

| Key | Meaning |
| --- | --- |
| `Name` | used as `$Name` in queries |
| `Label` | the label in the UI; empty: the name |
| `Default` | the starting value |
| `Options` | a fixed list: the UI offers a choice instead of a text box |

### Panel keys (`[[panel]]`)

| Key | Used by | Meaning |
| --- | --- | --- |
| `Title` | all | the panel's title; titles must be unique, since buttons and `refresh_on` name panels by title |
| `Kind` | all | `number`, `table`, `bar`, `line`, `button`, `import`, `input`, `dropdown` or `exporter` |
| `Query` | number, table, bar, line, exporter, dropdown | a read-only query, with `$variables` |
| `Chart` | bar, line | the columns to plot: `x: day, y: orders` or several series `x: day, y: orders, refunds`; empty: Y is the first numeric column and X the first other non-numeric one |
| `target` | button | the title of the panel the button runs |
| `refresh_on` | number, table, bar, line | run this panel again whenever the named panel runs, or the named button is pressed |
| `table` | import | the table the import wizard writes into: `table` or `schema.table` |
| `var` | input, dropdown | the variable the panel sets (declared by the panel itself; no `[[var]]` needed) |
| `default` | input, dropdown | the variable's starting value |
| `options` | dropdown | a fixed list of choices; empty: the first column of `Query` |
| `format` | exporter | `csv`, `json`, `sql` or `excel` |

### Panel kinds

| Kind | Shows | Does |
| --- | --- | --- |
| `number` | the first column of the first row, large | |
| `table` | the result as a table | |
| `bar` | a bar chart | |
| `line` | a line chart in the web; horizontal bars (one row per point) in the terminal | |
| `button` | a button | runs the `target` panel, then the panels that refresh on it |
| `import` | a button | opens the import wizard on `table` |
| `input` | a text box with the variable's value | sets `$var`, then the board runs again |
| `dropdown` | a choice | sets `$var`, then the board runs again; choices from `options` or the query (up to 5000; a longer list is cut and the UI says `only the first 5000 options are listed`, NULL values left out) |
| `exporter` | a button | runs the query with the current variables and exports the rows |

## Variables

`$name` in a query is replaced by the variable's current value before the query runs, as a literal: a plain number
(`30`, `-5`, `0.5`) stays a number, anything else becomes a quoted string with quotes doubled (`'it''s'`;
backslashes are doubled too on MySQL, MariaDB and ClickHouse), and `null` stays `NULL`. Long digit strings and
ones with a leading zero (`007`) are quoted so they keep their digits. `$name` inside a string literal or a comment
is left alone.

Only declared variables bind: a board `[[var]]`, or the `var` of an input or drop-down panel. A query that uses an
undeclared one fails with `unknown variable $region`. On MongoDB, where `$match` and friends are operators, nothing
is bound and a panel that mentions a declared variable fails with `variable $status cannot be bound in a mongo
panel`.

Where you set them:

| | Terminal | Web |
| --- | --- | --- |
| Board variables (`[[var]]`) | {{< key "v" >}}: a form with one field per variable (a choice when it has `Options`); OK runs the board | in the toolbar: a text box (Enter runs the board) or a drop-down |
| Input panel | {{< key "Enter" >}} on its tile: a one-line form | its text box in the toolbar (Enter runs the board) |
| Drop-down panel | {{< key "Enter" >}} on its tile: a filterable list, the current value marked `•` | its drop-down in the toolbar; picking runs the board |

The board header in the terminal shows the board variables as `days=30`; input and drop-down tiles show
`$status = all`.

{{< ui tui="tools-board-dropdown" >}}Enter on the **Status** drop-down tile lists its options.{{< /ui >}}

## Running, refresh and the status line

The whole board runs when it opens, then every `refresh_secs` while it is open (the web runs it only while the tab
is on screen). One panel failing does not stop the others; its error shows in its tile.

| | Terminal | Web |
| --- | --- | --- |
| Run everything | {{< key "r" >}} | **Refresh** (also reloads drop-down options) |
| Run one panel | {{< key "Tab" >}} / {{< key "S-Tab" >}} to highlight it (▸), then {{< key "Enter" >}}: it runs with its `refresh_on` dependants | ↻ in the panel header: it runs, then its dependants |
| Press a button | {{< key "Enter" >}} on its tile (`[ ↵ ] run Orders`) | its button in the toolbar |
| Last run | header: `updated 12:43:08 · refresh every 60s` (`running…` while it runs) | `Updated 12:43:08 PM` and `every 60s` in the toolbar |
| Errors | header: `2 errors (e: list)`; {{< key "e" >}} lists each failed panel with its message | a red `2 errors` button; click it for the list |
| Close | {{< key "Esc" >}} | close the tab |

A run asked for while another is still going waits and starts when it ends, with the newest variables. Each run
gives the queries 60 seconds in the terminal. Rows per panel: 100 in the terminal, the connection's row limit in
the web.

**`refresh_on` wiring.** When panel A runs (by {{< key "Enter" >}}, ↻ or a button), every panel with
`refresh_on = "A"` runs after it, and the panels that refresh on those, each once, so a cycle ends. A button runs
its target and the target's dependants; in the terminal the panels with `refresh_on` set to the button's own title
run as well. In the demo, **Recount** runs **Orders**, and **Revenue** (`refresh_on = "Orders"`) follows.

## Importer and exporter panels

- **Import**: the terminal opens the [import wizard](../../import-export/) on `table`; the web opens the import dialog
  and runs the board again when it is done. In the terminal an import panel refuses to run when the board is bound
  to another connection (`this board runs on X, but import writes into Y: open the board from X`).
- **Export**: the terminal runs the query (up to 100 000 rows) and opens the export form on the rows, with the
  panel's `format` preselected. The web shows a format drop-down (CSV, JSON, SQL INSERTs, Excel) beside the button,
  runs the export as a background job capped at the connection's row limit, and downloads the file.

## Connection

With `connection` empty the board runs on the connection you open it from. In the terminal a board with
`connection = "Shop (Postgres)"` runs on that connection's open workspace instead; if it is not open, the board says
`connection "Shop (Postgres)" is not open: connect to it first (this board runs there)`.

In the web, the board runs on the current connection; when more than one connection is open, a **Run on** drop-down
picks another. The web keeps the `connection` value when it saves the board but does not switch to it.

## Creating and editing boards

| Action | Terminal (board list) | Web (Metrics toolbar) |
| --- | --- | --- |
| New | {{< key "n" >}}: **Board name**, **First panel title**, **Panel kind** and the fields that kind needs | **New board…**: **Board name**, **Refresh (seconds)** (default 30), then the first panel |
| Add a panel | edit the file | **Add panel…**: one more panel on the open board |
| Rename | {{< key "e" >}}: **New name** (saved under the new name, the old file removed) | **Rename…** |
| Duplicate | {{< key "d" >}}: saved as `<name> copy` (`copy 2`, `copy 3` … when taken) | **Duplicate**: asks for the name (default `<name> copy`) |
| Delete | {{< key "x" >}} or {{< key "Delete" >}}: type the name to confirm; removes the file | **Delete…**: type the name |
| Open | {{< key "Enter" >}} | the board drop-down |

{{< ui tui="tools-board-new" web="tools-board-new" >}}A new board with a first panel.{{< /ui >}}

The panel fields only show when the chosen kind uses them:

| Terminal (**New metrics board**) | Web (**New board…** / **Add panel…**) | Kinds |
| --- | --- | --- |
| **Board name** | **Board name**, **Refresh (seconds)** | the board |
| **First panel title** | **Panel title** | all |
| **Panel kind** (←/→, default `table`) | the kind drop-down | all |
| **Read-only query** | **Query** (**Options query** for a drop-down) | all but input (and button, import in the web) |
| **Chart columns** (`x: a, y: b, c`) | **Chart columns** (`x: day, y: orders, refunds`) | bar, line |
| **Variable** (the `$var` it sets) | **Variable** | input, dropdown |
| **Options** (`a, b, c`; empty: first column of the query) | **Fixed options** | dropdown |
| **Default** | **Default** | input, dropdown |
| **Export format** (`csv`, `json`, `sql`, `excel`) | **Format** | exporter |
| — | **Runs panel** | button |
| — | **Import into** | import |
| — | **Refresh on** (a panel to follow) | number, table, bar, line |

The terminal's new-board form offers the kinds `number`, `table`, `bar`, `line`, `input`, `dropdown` and `exporter`
(no `button` or `import`: add those in the file), and writes no `refresh_secs`, so the new board does not refresh by
itself until you add it to the file.

The web checks the whole board when it saves: every panel needs its own title; number, table, bar, line and
exporter panels need a query; a button must target a panel that runs a query; an import panel needs a table; an
input needs a variable; a drop-down needs a variable and options or a query; an exporter's format must be `csv`,
`json`, `sql` or `excel`; `refresh_on` must name a panel. A new board whose name is taken is refused
(`a board named "x" already exists`).

### Arranging panels (web)

The web lays panels out in columns (one per 320 px of width). Drag a panel by its header onto another to move it,
or use ◀ / ▶; − / + make it narrower or wider (in columns); drag the bottom-right corner to change its height.
**Reset layout** forgets the arrangement. The arrangement is kept in this browser's `localStorage`
(`webtuidb-board:<name>`), not in the file. The terminal tiles panels two per row on a window 100 columns or wider,
one per row otherwise, ten lines each.

## Add to Metrics Board from a chart

Chart a query result (see [charts](../../sql-editor/charts/)), then add it to a board:

- **Terminal**: {{< key "b" >}} in the chart view, or the result menu → **Add to metrics board…**, asks for a
  **Board name** and saves a *new* one-panel board: the panel is titled like the board, its kind is the chart's
  (`bar` or `line`; a pie chart becomes `bar`), its query is the statement, its `Chart` the X and Y columns, and the
  board refreshes every 30 seconds. A board of that name already existing is an error (`board "x" already exists`).
- **Web**: **Add to Metrics Board** in the chart view asks for a board name; the panel is *appended* to the board of
  that name, or a new board is made. The result menu's **Add to metrics board…** is terminal-only.

## Safety

Every query on a board must be read-only, checked with the same classifier the SQL editor uses for safe mode: a
panel that would write fails with `panels must be read-only`, and the web refuses to run a board that has one
(`panel "x" is not read-only; boards only run read-only queries`). Boards therefore never go through the safe-mode
gate; writes only happen through the import panel, which uses the normal import path.

## MCP

The [MCP server](../../automation/) exposes boards to AI clients: `list_boards` (name, connection, panel titles) and
`run_board` (`name`, optional `connection`, default the board's own, and `vars`). `run_board` needs the connection to
be open (`open_connection` first), runs the panels through the read-only runner and refuses side-effect functions.

## Details and limits

- Panels and variables are edited in the file (or added one at a time with the web's **Add panel…**); neither UI
  edits an existing panel.
- Pie charts have no panel kind.
- The web's layout and the terminal's tile order follow the file's panel order.
