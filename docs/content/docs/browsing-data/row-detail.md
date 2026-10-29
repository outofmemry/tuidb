---
title: Row detail
description: One record as a vertical form beside the grid, with quick values and editing of several selected rows at once.
weight: 70
---

{{< key "C-d Space" >}} (`toggle_row_detail`) opens the row-detail pane: every column of the current row as a label and a value, one per line, which is easier to read than a wide grid for tables with many columns. Moving in the grid updates the pane; {{< key "C-d C-l" >}}-style focus keys move into it. The browser shows the same pane on the right.

Inside the pane you can edit a value (an ordinary pending edit), apply a **quick value** (NULL, EMPTY, DEFAULT, now) and open quick look on long values with PRETTY / MINIFY for JSON. With several rows selected in the grid the pane edits all of them: a value you set is applied to every selected row.

What is saved with a tab (split, detail pane) is described in [Panes and focus](../../workspace/panes/).
