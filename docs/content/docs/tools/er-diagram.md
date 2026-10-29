---
title: ER diagram
description: Draw the tables of a schema with their columns and foreign keys, coloured by group of related tables; filter, sort, switch schema, colour a table, copy a column name, add a foreign key, delete a table and open the structure editor, in the terminal and in the browser.
weight: 40
---

The ER diagram draws every table of a schema as a box with its columns, marks primary and foreign keys, and
colours the tables by cluster: tables joined by foreign keys share a colour. In the terminal it is text boxes in a
scrollable view; in the browser it is a canvas you pan, zoom and rearrange, with a list view beside it.

{{< ui tui="tools-diagram" web="tools-diagram" >}}The `public` schema of the demo `shop` database. The terminal shot is filtered on `order`: the matching tables plus their foreign-key neighbours (`customers`, `payments`, `products`). The web canvas shows every table, one colour per group of tables joined by foreign keys.{{< /ui >}}

## How to open it

| What | Terminal | Web |
| --- | --- | --- |
| Diagram of the current schema | {{< key "C-d d" >}} (`diagram`) | {{< key "C-d d" >}}: opens (or shows) the **Diagram** tab |
| Focused on one table | sidebar menu of a table or view → **Diagram** | right-click a table in the sidebar → **Diagram** |
| From Open Anything | {{< key "C-d p" >}} → **ER diagram** | {{< key "C-d p" >}} → **ER diagram** |

Opened from a table's menu, the filter starts as that table's name (so you see it and its direct neighbours in
the terminal) and the view scrolls to it; the web canvas centres it at full size.

The diagram loads the tables of the schema shown in the sidebar, with their columns, and their foreign keys
where the engine has them. A schema without tables says `no tables to draw`.

## Reading the diagram

| | Terminal | Web |
| --- | --- | --- |
| A table | a box: the name, then `pk`, `fk` or blank, the column name and its type; then one `-> table (columns)` line per foreign key | a box: the name and an **Open** button, then `#` (primary key), `→` (foreign key column) or blank, the name and the type |
| Relationships | the `->` lines inside the box | curved arrows from the first column of the key to the referenced column |
| Colour | the box header in the cluster colour, or the table's own colour | the box border and header tinted by cluster, or the table's own colour |
| Layout | boxes left to right, wrapping at the window width | tables of one cluster together, in rows of about √n boxes; you drag them where you want |

Long names are cut with `…` in the web boxes (the full name is in the list view).

## Terminal keys

The diagram is a scrollable view over the workspace.

| Key | Does |
| --- | --- |
| {{< key "↑" >}} {{< key "↓" >}} {{< key "←" >}} {{< key "→" >}} | scroll |
| {{< key "s" >}} | sort by name, or by column count (most columns first); press again to switch back |
| {{< key "/" >}} | filter: **Name contains** (empty = all). The matching tables *and their direct foreign-key neighbours* stay |
| {{< key "e" >}} | pick a table and open its structure editor (add a column, a foreign key, drop …) |
| {{< key "n" >}} | new table (the new-table dialog) |
| {{< key "a" >}} | add a foreign key (the form below) |
| {{< key "x" >}} | delete a table |
| {{< key "S" >}} | switch schema (a picker of the connection's schemas; `mysql has no schemas` on an engine without them) |
| {{< key "y" >}} | copy a column name: a filterable list of `table.column` with its type |
| {{< key "c" >}} | colour a table |
| {{< key "Esc" >}} | close |

The title shows the schema, the number of tables, the sort and the filter:
`ER diagram · public · 10 tables · sort: name · filter: orders`.

## Web canvas

| Do | How |
| --- | --- |
| Pan | drag the background |
| Zoom | the mouse wheel (around the pointer), or **+** / **−** at the bottom right (20 % to 250 %) |
| Fit everything | **Fit** |
| Move a table | drag its header |
| Forget the arrangement | **Reset layout** (lays the tables out again and fits) |
| Open a table's structure | **Open** in its header, or right-click → **Open structure** |
| Add a foreign key | drag from the dot at the end of a column onto another table (shown only when the engine has foreign keys) |
| Right-click a table | **Copy column name** (when you clicked a column), **Open structure**, **Colour** (a colour picker), **Reset colour** (when it has one), **Delete table…** |

The toolbar above the canvas:

| Control | Does |
| --- | --- |
| **Filter tables** | dims the tables (and arrows) whose name does not contain the text; in the list view they are hidden |
| **Sort by name** / **Sort by columns** | the order in the list view |
| schema drop-down | switch schema (shown when the connection has more than one) |
| **Reload** | read the tables again |
| **List** / **Canvas** | switch to a grid of cards (name, **Structure** button, columns, one line per foreign key) and back; remembered in this browser |

The web filter matches table names only; it does not keep foreign-key neighbours the way the terminal filter does.

## Colours

By default each cluster of tables joined by foreign keys gets its own colour; a table without keys is a cluster of
its own. You can give a table its own colour on top of that:

- **Terminal**: {{< key "c" >}}, pick the table, then `default` (the cluster colour), `blue`, `orange`, `green`, `pink`,
  `yellow`, `red`, `cyan`, `purple` or `grey`. The sidebar's **Set colour…** on a table sets the same colour, so a
  table has one colour in both places. The colours are saved per connection in `state.json`
  (`diagram_colors`, keyed `schema.table`, stored as ANSI 256 codes; older palette names and sidebar hex
  colours are converted when read).
- **Web**: right-click → **Colour** opens a colour picker; **Reset colour** goes back to the cluster colour. The
  colours live in this browser's `localStorage` (`webtuidb-er-colors:<connection>/<database>/<schema>`), apart
  from the sidebar's own colours and from the terminal's.

The web also remembers where you dragged each table, per connection, database and schema, in `localStorage`
(`webtuidb-er:<connection>/<database>/<schema>`). Tables that appear later get a place; places you set are kept.

## Adding a foreign key

{{< ui tui="tools-diagram-fk" web="tools-diagram-fk" >}}Adding a foreign key from `reviews.customer_id` to `customers.id`: the terminal form ({{< key "a" >}}), and the web dialog that opens after a drag from the column's dot onto `customers`, with the name `fk_reviews_customer_id` filled in.{{< /ui >}}

**Terminal** ({{< key "a" >}}): a form with

| Field | |
| --- | --- |
| **Table** | the table that gets the key (←/→ to pick) |
| **Columns** | its columns, comma separated |
| **References table** | the referenced table |
| **Referenced columns** | comma separated, the same number as **Columns** |
| **ON UPDATE**, **ON DELETE** | `NO ACTION` (default), `RESTRICT`, `CASCADE`, `SET NULL`, `SET DEFAULT` |
| **Constraint name** | empty: the database generates one |

The form checks that every column exists (`orders has no column "x"`) and that both lists have the same length.

**Web**: drag from the dot at the end of a column onto the referenced table. The **Add foreign key** dialog opens
with that column paired with the referenced table's primary key (or a column of the same name). You can change
each pair, **Add column pair** for a composite key, **Remove** a pair, edit the **Name** (default
`fk_<table>_<column>`) and pick **On update** / **On delete**.

Both plan the key as a structure change of the table (the table as loaded, plus the key) and show the statements
before anything is sent: the terminal Preview ({{< key "Enter" >}} applies), the web **Preview statement** then the
usual send dialog. A key the table already has, by name or by the same columns and referenced columns, is not
added twice. After it is applied the terminal diagram reloads with the new key.

## Deleting a table

Terminal {{< key "x" >}}: pick the table, then type its name to confirm. Web: right-click → **Delete table…**, type
the name. Both plan the same `DROP TABLE` as the sidebar's delete and send it through safe mode; open tabs of the
table close.

{{< callout type="warning" >}}
Deleting a table from the diagram drops it with its data. Safe mode decides whether you are asked again.
{{< /callout >}}

## Details and limits

- Only tables are drawn; views and other objects are not.
- Engines without foreign keys (ClickHouse, SingleStore, Redis, MongoDB …) draw boxes without arrows, every table
  in its own colour, and the web canvas hides the drag dots.
- Terminal: a box wider than the window overflows and is reached by scrolling sideways; the column to copy is
  picked from a list rather than a cursor inside the box.
- The terminal's load times out after 60 seconds.
- Web: positions and colours are per browser, not shared with the terminal or other machines.
