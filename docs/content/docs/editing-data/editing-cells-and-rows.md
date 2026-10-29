---
title: Cells and rows
description: Inline and popover cell editing, NULL and DEFAULT, adding, duplicating, deleting and pasting rows.
weight: 20
---

## Editing a cell

| Action | Terminal | Web |
|---|---|---|
| Edit in place | {{< key "e" >}} on the cell, type, {{< key "Enter" >}} | double-click the cell or press `e`, type, Enter |
| Popover editor | {{< key "Enter" >}} opens the cell; JSON is pretty-printed; {{< key "C-s" >}} saves, {{< key "Esc" >}} cancels | the cell editor dialog with the same keys |
| Move to the next cell | {{< key "Tab" >}} | Tab |
| Quick look (read-only) | {{< key "Enter" >}} on a cell you do not edit, see [Quick look](../../browsing-data/quick-look/) | the eye icon or Enter |

Typing over a cell replaces its value. Boolean and enum columns offer their values; JSON cells are validated before they become a mark; binary cells open in the hex editor. To set a cell to NULL, an empty string or the column default use [Set Value](../set-value/), so an empty edit is never confused with NULL.

## Rows

| Action | Terminal | Web |
|---|---|---|
| Add a row | {{< key "C-d i" >}} | the **+** button or `C-d i` |
| Duplicate / delete | the row menu ({{< key "m" >}} or right-click) | the row's ⋯ menu or right-click |
| Select a range | {{< key "V" >}} style selection with the arrow keys; shift-drag with the mouse | shift-click, drag |
| Paste rows from the clipboard | {{< key "v" >}} (`paste_rows`) | `C-d v` |
| Paste into the selected cells | {{< key "V" >}} (`paste_cells`) | `C-d V` |

Pasted text is read as CSV or TSV with the `[csv]` settings, and values that were neutralised on copy (a leading `'` before `=`, `+`, `-`, `@`) are stripped again so a copy-and-paste round trip is lossless. A new row shows the column defaults in place; a column you leave untouched is sent as `DEFAULT`.

The row detail pane ([Row detail](../../browsing-data/row-detail/)) edits one record as a form and can apply the same value to several selected rows.
