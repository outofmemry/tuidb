---
title: Quick filter
description: Filter a table on the value under the cursor in one step, and how it differs from the filter bar, in the terminal and in the browser.
weight: 40
---

Quick filter turns the cell under the cursor into a filter: put the cursor on `paid` in the `status`
column, press {{< key "C-d x" >}}, and the grid shows only the paid orders. It is a shortcut into the
[filter bar](../filter-bar/), not a separate search: the result is an ordinary filter you can change,
switch off or clear in the bar.

{{< ui tui="browsing-quick-filter" web="browsing-quick-filter" >}}Quick filter on a `status` cell: the terminal asks which condition to use, the browser applies `= paid` at once.{{< /ui >}}

## How to open it

| What | Terminal | Web |
| --- | --- | --- |
| Quick filter on the cursor cell | {{< key "C-d x" >}} (`quick_filter`) | {{< key "C-d x" >}} |
| From the cell menu | right-click → **Quick filter on this cell** | right-click → **Quick filter on this cell** |

## What it does

**Terminal.** {{< key "C-d x" >}} opens **Quick filter: status** with the conditions it can build from
the value:

| Entry | Filter |
| --- | --- |
| `= paid` | `status = paid` |
| `<> paid` | `status <> paid` |
| `Contains paid` | `status Contains paid` |
| `Has prefix paid` | `status Has prefix paid` |
| `IN (paid)` | `status IN paid` (add more values in the bar afterwards) |
| `IS NULL` | `status IS NULL` |
| `IS NOT NULL` | `status IS NOT NULL` |

On a `NULL` cell only `IS NULL` and `IS NOT NULL` are offered. Picking one **replaces every filter** in
the bar with this single filter, shows the bar and applies it.

**Web.** {{< key "C-d x" >}} applies `column = value` straight away (`column IS NULL` on a `NULL` cell),
without a menu. It replaces only the filters on the same column, keeps the filters on other columns,
joins them with AND, and shows the bar. With no cell selected it says `Select a cell first`.

In both UIs the value is the cell's text as the grid shows it (with pending edits applied). Applying
goes back to the first page; with pending changes you are asked **Discard pending changes?** first.

## Quick filter, filter bar or find?

| You want | Use |
| --- | --- |
| rows whose column equals (or starts with, contains…) the value you are looking at | quick filter |
| several conditions, other operators, OR, a raw condition | the [filter bar](../filter-bar/) |
| a text anywhere in the table | the filter bar with **Any column** and `Contains` |
| a text in the rows already loaded, without reloading | [find](../find/) |

The filter is remembered with the table like any other filter (see
[How filters are remembered](../filter-bar/#how-filters-are-remembered)).
