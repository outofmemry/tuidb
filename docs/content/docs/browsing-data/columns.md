---
title: Columns
description: Choose which columns are visible, hide and show them, resize them, and copy a whole column.
weight: 80
---

| Action | Key | Name |
|---|---|---|
| Choose visible columns | {{< key "C-d M-f" >}} | `columns` |
| Hide this column | {{< key "C-d _" >}} | `hide_column` |
| Show all columns | {{< key "C-d +" >}} | `show_all_columns` |
| Column header menu (sort, filter, hide) | {{< key "C-d M-H" >}} | `column_menu` |
| Copy all values of the column | {{< key "C-d M-y" >}} | `copy_column` |

The visible-columns picker lists every column with a checkbox and a search box; the order of the list is the table's order. Hidden columns are remembered per table in `state.json`, shared by both UIs, and a hidden primary key is still used to address edits. Columns start at the width of their name and first rows; dragging a header edge in the browser or resizing in the terminal saves the width as `widths`, also per table.

The column header menu in the browser opens with a right-click on the header and offers sort ascending and descending, filter on this column, hide, and copy as. Sorting by several columns is in [Sorting](../sorting/).
