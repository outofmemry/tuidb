---
title: Charts
description: Draw a query result as a bar, line or pie chart, name the columns with a -- barchart comment, save the chart as PNG and add it to a metrics board.
weight: 80
---

Any result with a label column and a number column can be drawn as a chart, without leaving the query
tab. A comment in the statement (`-- barchart x: month, y: revenue`) says which columns to use and which
kind of chart to draw; without one, webtuidb picks the columns for you. The chart can be saved as a panel
of a [metrics board](../../tools/metrics-boards/) that re-runs the query on a timer.

{{< ui tui="results-chart-bar" web="results-chart-bar" >}}The saved query "Monthly revenue", whose `-- barchart x: month, y: revenue` comment picks the columns.{{< /ui >}}

## How to open it

| Terminal | Web |
| --- | --- |
| {{< key "C-d g" >}} (`chart`) with the focus in the results; {{< key "C-d g" >}} again goes back to the grid. Also **Toggle chart** in the result menu (right-click). | the **Chart** button at the right of the result tabs (pressed while the chart shows), {{< key "C-d g" >}}, or **Toggle chart** in the cell menu |

A result without rows cannot be charted (`nothing to chart: this result has no rows`); the browser only
offers **Chart** on a result with rows. Running again closes the chart in the browser; in the terminal a
result refreshed in place (load more, commit) keeps it.

## Choosing the columns and the kind

Put a comment line in the statement, usually right above it:

```sql
-- barchart x: month, y: revenue
SELECT date_trunc('month', placed_at)::date AS month, sum(total) AS revenue, count(*) AS orders
FROM orders WHERE status <> 'cancelled' GROUP BY 1 ORDER BY 1;
```

| Part | Meaning |
| --- | --- |
| `barchart`, `linechart`, `piechart` | the kind of chart (case does not matter) |
| `x: column` | the labels along the axis (or the slices of a pie) |
| `y: column` | the values |
| `y: a, b, c` | several series: a name without `key:` continues the previous key |

Column names match without regard to case. A name that is not in the result stops the chart with
`chart: no column "x"`. The comment may sit next to a `-- name:` label; both are read from the
statement's leading comments.

{{< ui tui="results-chart-multi" web="results-chart-multi" >}}`-- barchart x: month, y: revenue, orders`: two series, with a legend (`orders` is `count(*) * 100` here so both series share one scale; every series is drawn against the largest value of all of them).{{< /ui >}}

Without a comment (or with `x:` or `y:` left out) the columns are chosen like this:

- **Y** is the first numeric column: every value that is not NULL reads as a number, and there is at
  least one.
- **X** is the first other column that is not numeric, else the first other column.
- A result that has no such pair says `chart: need one label column and one numeric column` (the
  browser: "Nothing to draw: the result has no numeric column." when no value is left).

So the order of the select list is a way to pick columns too. There is no column picker in either UI;
change the comment or the select list.

Rows whose first Y value is not a number are skipped; a missing value in another series counts as 0. A
NULL label is drawn as `NULL`, and timestamps as RFC 3339 text.

### Kinds

| Kind | Terminal | Web |
| --- | --- | --- |
| bar | horizontal bars scaled to the pane, label, bar, value | vertical columns with the value above each (one series) or grouped columns in a colour per series |
| line | drawn as bars (the terminal cannot draw lines) | a line per series with a dot per point, min and max on the axis, at most 12 labels along it |
| pie | a list of bars sorted by value, with each share in whole percent | a list of bars sorted by value, with each share to one decimal |

The terminal takes the kind from the comment only (bar when there is none). The browser has **bar**,
**line** and **pie** buttons above the chart that override the comment for this view.

{{< ui tui="results-chart-pie" web="results-chart-pie" >}}"Orders by status" with `-- piechart`: shares sorted from the largest.{{< /ui >}}

## What you can do in the chart view

| Terminal | Web |
| --- | --- |
| The first line names the kind and the number of points, and lists the series in a legend (`█`, `▓`, `▒`, `░` for the first four). Labels are cut to a third of the width; points that do not fit end with `… N more`. | Hover a bar or a point for its label, series and value. |
| {{< key "b" >}} adds the chart to a metrics board. | **Save as PNG** downloads `chart.png` (twice the drawn size, the page's colours, on the canvas background; a pie is drawn as its bar list). |
| {{< key "C-d g" >}} goes back to the grid. | **Add to Metrics Board** asks for a board name. |

## Add to Metrics Board

{{< ui tui="results-chart-board" web="results-chart-board" >}}Add to Metrics Board asks for the board's name.{{< /ui >}}

| | Terminal | Web |
| --- | --- | --- |
| How | {{< key "b" >}} in the chart view, or **Add to metrics board…** in the result menu | **Add to Metrics Board** in the chart view |
| Asks | **Board name** (form "Add to Metrics Board") | "Metrics board name" |
| A new name | a new board file with one panel | the same |
| An existing name | refused: `board "x" already exists` | the panel is added to that board |
| Says | `saved board x: open it with the metrics key` | `Added to board x` |

The panel is titled with the board name, runs the statement (the whole text, comment included), and
uses the comment's `x:` and `y:` columns. The terminal takes the kind from the comment; the browser takes
the **bar** / **line** / **pie** button you picked. Bar and line charts keep their kind; a pie becomes a bar panel,
because boards have no pie panel. The board refreshes every 30 seconds. Boards are TOML files under
`~/.local/share/webtuidb/boards/`; open them with {{< action "metrics" >}}. See
[Metrics boards](../../tools/metrics-boards/).

The **Add to metrics board…** entry of the browser's result cell menu is greyed out; use the button in
the chart view.

## Details and limits

- The chart uses the rows the result holds, up to the row limit; **Load more** first if the result was
  cut.
- The browser sends the cells as the text the grid shows, so numbers stored as text chart as numbers in
  both UIs.
- A pie with negative values draws them as empty bars; their share counts as zero.
