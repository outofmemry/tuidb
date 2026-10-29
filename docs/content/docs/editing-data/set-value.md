---
title: Set Value
description: Set a cell, or every selected cell, to NULL, EMPTY, DEFAULT, a literal, an expression, a date or a file.
weight: 30
---

Set Value is the one dialog for values you cannot type into a cell. Open it from the cell's menu ({{< key "m" >}} or right-click), or with {{< key "M-Enter" >}} in an editable result.

{{< ui tui="editing-set-value" >}}The Set Value dialog.{{< /ui >}}

| Choice | What is sent |
|---|---|
| **Literal** | the text, quoted for the column type |
| **NULL** | `NULL` |
| **EMPTY** | the empty string `''` |
| **DEFAULT** | the column default, the sequence's next value for serial and identity columns |
| **Advanced** | an expression sent verbatim, for example `now()` or `price * 1.1`; the preview shows it unquoted |
| **Date picker** | a date or timestamp chosen from a calendar, formatted for the column |
| **Add a file** | the file's bytes, for binary columns |

{{< ui tui="editing-set-value-date" >}}The date picker.{{< /ui >}}

The choice applies to every cell in the current selection, so a column can be cleared or reset for a block of rows in one step. The result is an ordinary pending mark: review it with {{< key "C-d P" >}} and commit with {{< key "C-d s" >}}.
