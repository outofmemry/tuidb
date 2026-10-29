---
title: Find and replace
description: Search a table or a query result for a value, step through the matches, and replace one or all of them as pending edits.
weight: 50
---

Press {{< key "/" >}} in a grid to search the loaded page for text; {{< key "n" >}} and {{< key "N" >}} step to the next and previous match. After the prefix the same actions are {{< key "C-d M-/" >}} (find), {{< key "C-d M-g" >}} (next) and {{< key "C-d M-G" >}} (previous), so the plain {{< key "C-d /" >}} and {{< key "C-d n" >}} keep their global meanings (search in database, new session). In the browser the search box opens from the toolbar or with the same prefixed keys.

The find box takes a column (or all columns), an operator (contains, equals, starts with, regular expression) and a **Match case** toggle. Matches are highlighted and the cursor jumps between them; the count is shown next to the box.

**Replace** adds a second field. *Replace* changes the current match and moves on; *Replace all* changes every match on the page. Each replacement becomes an ordinary pending edit, amber in the grid, so nothing reaches the database until you commit ({{< key "C-d s" >}}) and you can still discard or undo.

Find works the same way in a query result; a result that is not editable allows find but greys out replace. Search only covers the rows already loaded: for a whole-table search use the [filter bar](../filter-bar/) or [Search in database](../../tools/search-in-database/).
