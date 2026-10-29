---
title: Table operations
description: Truncate, drop, rename, duplicate, import into, export, diagram and the other entries of a table's menu, in both UIs.
weight: 60
---

The table menu opens with {{< key "m" >}} or a right-click on a table in the sidebar, and on the tab in the browser. The entries come from one list shared by both UIs (`internal/menus`), so the two menus match; entries an engine cannot do are greyed out with the reason.

| Entry | What it does |
|---|---|
| Open, open in new tab | the data grid |
| Structure, Index, DDL | the structure views ({{< key "C-d )" >}}, {{< key "C-d M-i" >}}, {{< key "C-d M-d" >}}) |
| Truncate ({{< key "C-d M-Delete" >}} in the sidebar) | empties the table; you type its name to confirm |
| Drop | drops it; typed-name confirmation; several marked tables ask for the count |
| Rename | renames the table (the web asks you to reopen the tab) |
| Duplicate | clones the table, with or without its rows |
| Import into this table… | the [import wizard](../../import-export/import/) with the target fixed |
| Export… ({{< key "C-d E" >}}) | the [export dialog](../../import-export/export/) for this table |
| Diagram ({{< key "C-d d" >}}) | the [ER diagram](../../tools/er-diagram/) centred on this table |
| Edit triggers ({{< key "C-d M-t" >}}) | see [Views, functions and triggers](../views-functions-triggers/) |
| Copy name, copy DDL | to the clipboard |
| Pin / unpin, add to group | sidebar organisation |

Every destructive entry is previewed and gated by [safe mode](../../safety/safe-mode/); on a `read_only` connection they are refused. The full list per object kind (view, function, sequence, collection, key, schema, database) is in the [menus reference](../../reference/menus/).
