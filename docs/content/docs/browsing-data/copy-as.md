---
title: Copy as
description: Copy cells, rows or a column to the clipboard as CSV, TSV, JSON, SQL and other formats, with the CSV settings and the clipboard over SSH.
weight: 100
---

| Action | Key | Name |
|---|---|---|
| Copy the selected cells as… | {{< key "C-d M-C" >}} | `copy_cells_as` |
| Copy rows / cells (results pane) | {{< key "C-d c" >}} / {{< key "C-d C" >}} | `copy_rows`, `copy_cells` |
| Copy rows as… (results pane) | {{< key "C-d M-c" >}} | `copy_rows_as` |
| Copy a whole column | {{< key "C-d M-y" >}} | `copy_column` |

The *as…* picker offers the formats both UIs implement: plain text, CSV, TSV, JSON (array of objects), SQL `INSERT` statements for the current dialect, Markdown table, and the column names only. CSV and TSV follow the `[csv]` settings (`delimiter`, `quote`, `line_break`, `decimal`, `null_as`, `quote_mode`), and a delimiter or decimal mark that would be ambiguous for a spreadsheet is pointed out once.

Values that a spreadsheet would run as a formula (starting with `=`, `+`, `-` or `@`) are neutralised with a leading `'` on copy and stripped again when pasted back into webtuidb, so a round trip is lossless; negative decimals stay numbers. SQL output quotes identifiers and literals for the engine, which is why the same copy on MySQL and PostgreSQL differs.

In the terminal the clipboard is reached through OSC 52 when the terminal supports it, which also works over SSH; otherwise the platform tool (`pbcopy`, `xclip`, `wl-copy`) is used. The browser uses the clipboard API, which needs a secure context or `localhost`.
