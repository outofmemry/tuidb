---
title: Filter bar
description: Narrow a table with one or more column conditions, every operator per engine, raw conditions, the generated WHERE clause, and how filters are remembered, in the terminal and in the browser.
weight: 30
---

The filter bar sits above the grid and holds a list of conditions: a column, an operator and a value
each, joined by AND (or OR). The database does the filtering, so the filters cover the whole table and
the row count follows them.

{{< ui tui="browsing-filter-bar" web="browsing-filter-bar" >}}`orders` filtered to `status = paid`: the bar above the grid, the count under it is the number of matching rows.{{< /ui >}}

## How to open it

| What | Terminal | Web |
| --- | --- | --- |
| Show / focus / hide the bar | {{< key "C-d f" >}} (`filters`) | {{< key "C-d f" >}}, or **Filters** in the toolbar |
| Start a filter on one column | column menu → **Filter with this column** | right-click the header → **Filter with this column** |
| Filter on the value under the cursor | {{< key "C-d x" >}} ([quick filter](../quick-filter/)) | {{< key "C-d x" >}} |
| Filter bar menu | right-click the bar | — (the bar's buttons) |

In the terminal {{< key "C-d f" >}} does three things in turn: it shows the bar (adding a first filter
when there is none) with the cursor in the value, focuses the bar when it is shown but the grid has the
focus, and hides the bar when it already has the focus. In the browser {{< key "C-d f" >}} and the
**Filters** button show and hide the bar; click a field to work in it.

A new filter starts on the column and operator the settings say: by default the table's first primary
key column (or **Any column** when there is no key) and `=`.

## What you can do

### The fields of a filter

| Field | Terminal | Web |
| --- | --- | --- |
| On / off | `✓` (on) or `○` (off): {{< key "Space" >}} on that field toggles it | the checkbox; typing a value ticks it |
| Column | `id ▾`: {{< key "Space" >}}, {{< key "Left" >}} or {{< key "Right" >}} opens the **Filter column** picker (type to narrow it, each column with its type) | the column drop-down |
| Operator | `= ▾`: {{< key "Space" >}}, {{< key "Left" >}} or {{< key "Right" >}} opens the **Filter operator** picker | the operator drop-down |
| Value | a text field (`value…` when empty, `(no value)` for `IS NULL` / `IS NOT NULL`) | a text box (hidden for `IS NULL` / `IS NOT NULL`) |
| Remove | {{< key "M-d" >}} or {{< key "M-x" >}} | **✕** at the end of the row |

The column list starts with **Any column** and **Raw SQL** (the browser calls it **Raw condition**),
then the table's columns in table order, or by name with `[table] filter_column_sort = "name"`. A
filter that is off stays in the bar, struck through, and is not applied.

{{< ui tui="browsing-filter-operators" web="browsing-filter-operators" >}}The operator list of a PostgreSQL table.{{< /ui >}}

### Keys in the bar

These work while the bar has the focus (the `filterbar` context). The {{< key "M-" >}} keys are
no-prefix bindings in both UIs.

| Key | Action | Terminal | Web |
| --- | --- | --- | --- |
| {{< key "Enter" >}} or {{< key "C-d Enter" >}} | `apply_filters`: load the rows with the ticked filters | ✓ | ✓ (Enter in any field, or **Apply**) |
| {{< key "Tab" >}} / {{< key "S-Tab" >}} | next / previous field (on, column, operator, value; a raw row has no operator) | ✓ | the browser's own focus order |
| {{< key "Up" >}} / {{< key "Down" >}} | previous / next filter | ✓ | — |
| {{< key "M-n" >}} | `filter_add_copy`: add a filter below this one, same column and operator | ✓ (empty value) | ✓ (value copied too) |
| {{< key "M-d" >}}, {{< key "M-x" >}} | remove this filter | ✓ | — (**✕**) |
| {{< key "M-o" >}} | switch AND / OR | ✓ | — (**Match any**) |
| {{< key "M-s" >}} or {{< key "C-d s" >}} | `filter_sql`: show the WHERE clause | ✓ | ✓ ({{< key "C-d s" >}}, or **Show SQL**) |
| {{< key "M-c" >}} | `filter_clear_all`: remove every filter | ✓ | ✓ (or **Clear all**) |
| {{< key "M-e" >}} | `filter_export`: export the filtered rows | ✓ | ✓ (or **Export filtered rows**) |
| {{< key "Esc" >}} | hide the bar (the filters stay applied) | ✓ | — (**Filters** or {{< key "C-d f" >}}) |

Inside the bar {{< key "C-d s" >}} is `filter_sql`, not `commit`: the bar's binding shadows the global
one while the bar has the focus.

To add a blank filter in the terminal, right-click the bar and choose **Add filter**, or copy one with
{{< key "M-n" >}} and change its column. The browser has an **Add filter** button. The terminal bar shows
five filters at a time and scrolls with {{< key "Up" >}} / {{< key "Down" >}}.

The terminal's filter bar menu (right-click) has **Apply**, **Add filter**, **Add a filter as a copy**,
**Show SQL**, **Clear all** and **Export filtered rows**, the same words and order as the browser's
buttons.

### AND or OR

All ticked filters are joined by AND. {{< key "M-o" >}} in the terminal (the bar's title shows
`Filters (AND)` or `Filters (OR)`) or **Match any** in the browser joins them by OR instead.

## Operators

The operator list depends on the engine. PostgreSQL has the longest list:

| Operator | Value | Condition on PostgreSQL |
| --- | --- | --- |
| `=`, `<>`, `<`, `>`, `<=`, `>=` | one value | `"total" > $1` |
| `IN`, `NOT IN` | a comma-separated list; quote an item to keep a comma in it (`'a,b', c`), double a quote inside quotes | `"status" IN ($1, $2)` |
| `IS NULL`, `IS NOT NULL` | none | `"shipped_at" IS NULL` |
| `BETWEEN`, `NOT BETWEEN` | `low AND high` (the `and` in any case) | `"total" BETWEEN $1 AND $2` |
| `LIKE` | a pattern you write, with `%` and `_` | `"email" LIKE $1` |
| `ILIKE` | a pattern, case-insensitive (PostgreSQL only) | `"email" ILIKE $1` |
| `Contains`, `Not contains` | plain text | `CAST("email" AS TEXT) ILIKE '%text%'` |
| `Has prefix`, `Has suffix` | plain text | `… ILIKE 'text%'`, `… ILIKE '%text'` |

- Values are sent as parameters, never pasted into the SQL.
- `Contains`, `Not contains`, `Has prefix` and `Has suffix` escape `%`, `_` and `\` in what you type, so
  they match the text literally. They use `ILIKE` (case-insensitive) on PostgreSQL and DuckDB and `LIKE`
  elsewhere (on MySQL and MariaDB case follows the column's collation).
- On PostgreSQL, ClickHouse and DuckDB the `LIKE` family casts a non-text column to text first, so
  `Contains 12` works on a number column.
- `BETWEEN` without the `AND` fails with `BETWEEN needs two values: low AND high`.
- An empty value is a value: `= ` with nothing typed compares with the empty string. In the browser a
  row with an empty value is left out when you apply.

### Any column

**Any column** applies the operator to every column of the table and joins the results with OR:
`(CAST("id" AS TEXT) ILIKE … OR CAST("status" AS TEXT) ILIKE … OR …)`. `LIKE`-style operators and `=` compare each column
as text (so `= 42` finds 42 in a text column and `= bob` does not match every zero on MySQL); `<`, `>`
and the other comparisons compare in each column's own type. Hidden columns are searched too. On
MongoDB a negative operator (`Not contains`, `<>`, `NOT IN`, `NOT BETWEEN`) must hold for every field
instead ("no field contains x").

### Raw SQL

**Raw SQL** (**Raw condition** in the browser) takes a whole condition instead of column, operator and
value: `total > 500 AND coupon IS NOT NULL`. It is put into the WHERE clause in parentheses, as you typed
it, joined with the other filters. It has no operator field. A raw row with nothing typed is ignored.

{{< callout type="warning" >}}
A raw condition is SQL you write; it is not checked before it runs. A typo shows up as the load error.
{{< /callout >}}

### Engine differences

{{< tabs items="PostgreSQL,MySQL / MariaDB / ClickHouse / SQLite,MongoDB,Redis,DynamoDB,Cassandra" >}}
{{< tab >}}All operators above, including `ILIKE`. `Contains` and friends are case-insensitive.{{< /tab >}}
{{< tab >}}Every operator except `ILIKE`. `Contains` and friends use `LIKE`. ClickHouse casts with `toString()`, MySQL with `CAST(… AS CHAR)`.{{< /tab >}}
{{< tab >}}The same list as MySQL. Filters become a query document:

- `=`, `<>`, `<`… compare with the value converted to the field's type (the type is sampled from the
  first 100 documents); a field with mixed types matches the number or the text.
- `IS NULL` matches null and missing fields.
- `LIKE` becomes an anchored regular expression; `Contains`, `Not contains`, `Has prefix`, `Has suffix`
  are case-insensitive regular expressions.
- **Raw SQL** takes a JSON query document instead of SQL: `{"total": {"$gt": 500}}`. `$where`,
  `$function` and `$accumulator` are refused (`$where, $function and $accumulator are not allowed in a
  filter`). A document that does not parse fails with `raw filter: …`.
- The filter SQL popover is not available (no SQL dialect).{{< /tab >}}
{{< tab >}}A key's elements are filtered in memory: the key is read whole (up to 100,000 elements) and
each row compared as text, case-sensitively. Only `=`, `<>`, `LIKE`, `Contains`, `Not contains`,
`Has prefix` and `Has suffix` work; the others are listed but fail with
`filter operator "<" is not supported for redis`. **Raw SQL** fails with
`raw filters are not supported for redis`. When the key holds more than 100,000 elements the filtered
count is marked as an estimate.{{< /tab >}}
{{< tab >}}PartiQL has no `LIKE`: the list leaves out `LIKE`, `ILIKE` and `Has suffix`. `Contains` and
`Not contains` become `contains(col, ?)`, `Has prefix` becomes `begins_with(col, ?)`.{{< /tab >}}
{{< tab >}}CQL only offers `=`, `<`, `>`, `<=`, `>=`, `IN` and `LIKE` (`LIKE` needs a SASI index on the
column). A raw condition is added without parentheses, since CQL has none.{{< /tab >}}
{{< /tabs >}}

## Apply and applied

Typing in the bar does not reload anything; the rows change when you apply.

- **Terminal.** The bar edits the filters in place, but the grid keeps showing the rows of the last
  applied set. When the bar holds changes that are not applied yet, the hidden-bar line says
  `· not applied`. Export, bulk update and the SQL popover always use the applied filters, the ones the
  rows on screen came from.
- **Web.** The bar edits a draft. **Apply** (or {{< key "Enter" >}}) sends it; rows with no value are
  dropped from it (except `IS NULL` / `IS NOT NULL`), and so is an empty raw condition. **Show SQL** and
  **Export filtered rows** use what the bar holds now.

Applying goes back to the first page and keeps the sort. With pending changes on the grid you are asked
**Discard pending changes?** first.

### When the bar is hidden

{{< ui tui="browsing-filter-pills" >}}The bar hidden with {{< key "Esc" >}}: the terminal keeps one line with the applied filters as pills.{{< /ui >}}

Hiding the bar does not remove the filters. The terminal shows one line above the grid,
`Filters  status = paid   C-d f edit`, with each filter as a pill (column bold, operator dim, value
plain), joined by `and` / `or`; a filter that is off is struck through, a raw one shows as `sql …`. The
browser shows the number of active filters on the **Filters** button.

## The generated WHERE clause

{{< ui tui="browsing-filter-sql" web="browsing-filter-sql" >}}`filter_sql` shows the WHERE clause with every value written in, ready to copy into the editor.{{< /ui >}}

{{< key "M-s" >}} (or {{< key "C-d s" >}} in the bar, **Show SQL** in the browser) shows the conditions
as SQL, with the values inlined as literals: `WHERE "status" = 'paid' OR "status" = 'shipped'`. Nothing
runs it; it is for reading and copying.

| | Terminal | Web |
| --- | --- | --- |
| Shows | the applied filters; when the bar has unapplied changes a note says so | the bar's current filters |
| Copy | {{< key "c" >}} copies the `WHERE …` line | **Copy** copies the condition |
| No filters | `(no active filters)` | `No active filters: no WHERE clause.` |
| Not an SQL engine | `filter SQL is only shown for SQL engines` | `no SQL dialect for mongo` |

## Clear all and export

- **Clear all** ({{< key "M-c" >}}, `filter_clear_all`) removes every filter and reloads. The terminal
  closes the bar; the browser keeps it open with one blank filter.
- **Export filtered rows** ({{< key "M-e" >}}, `filter_export`) opens the export dialog for this table
  with the filters, all matching rows rather than the page. The terminal uses the applied filters (no
  filter: the whole table); the browser uses the bar's current filters. The dialog is described in
  [Import, export, backup](../../import-export/).

## How filters are remembered

The applied filters and whether the bar was open are saved per connection, database and table in
`state.json` (`filters`, `show_filters`) and come back the next time you open the table, in either UI.
The saved filters are applied on open.

- The terminal saves when you apply, clear, hide the bar or change the sort; the browser saves half a
  second after the applied filters or the bar change.
- `[table] filter_state` decides the bar's visibility on open: `remember` (the default) uses the saved
  `show_filters`, `show` always opens it, `hide` always closes it. The filters apply either way.
- When a load fails and a filter is active, the terminal opens the bar so you can fix or clear the
  filter (a saved filter on a renamed column is the usual cause).
- Opening a table from a [foreign key](../foreign-keys/) or a database search brings its own filter,
  shown in an open bar.

## Settings

| Setting | Values | Effect |
| --- | --- | --- |
| `[table] filter_default_column` | `pk` (default), `any`, `raw` | the column a new filter starts on: the first primary key column (Any column without one), Any column, or Raw SQL |
| `[table] filter_default_operator` | `=` (default), `contains` | the operator a new filter starts with |
| `[table] filter_state` | `remember` (default), `show`, `hide` | the bar's visibility when a table opens |
| `[table] filter_column_sort` | `ordinal` (default), `name` | the order of the column list |

See the [settings reference](../../configuration/settings-reference/).
