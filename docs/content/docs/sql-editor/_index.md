---
title: SQL editor
weight: 7
---

A query tab is an editor on top and a results pane below. You type SQL (or Redis commands, or MongoDB
shell calls), run the statement under the cursor or the whole buffer, and every statement gets its own
result tab, which you can pin, search, copy, export, chart or edit. Everything you run is kept in a
per-connection history, and the statements you want again live as saved queries: plain `.sql` files you
can open, share and expand from autocomplete. The terminal and the browser use the same engine, the same
files and the same keys (`C-d e` opens a tab in both).

{{< cards >}}
  {{< card link="query-tab" title="Query tab" subtitle="Open an editor, run the current statement or all of them, cancel, row limit." >}}
  {{< card link="editing" title="Editing" subtitle="Autocomplete, find and replace, beautify, comments, folding, Vim keys, split down." >}}
  {{< card link="parameters" title="Parameters" subtitle="Named and positional query parameters and the Variables dialog." >}}
  {{< card link="result-modes" title="Result modes" subtitle="Tabs or append, return on error, statement labels, summaries of long runs." >}}
  {{< card link="keyboard" title="Keyboard" subtitle="Every editor and results key in both UIs." >}}
  {{< card link="results" title="Results" subtitle="Result tabs, pinning, find, copy as, export, load more, the Message view." >}}
  {{< card link="charts" title="Charts" subtitle="Bar, line and pie charts of a result, -- barchart hints, Add to Metrics Board." >}}
  {{< card link="explain" title="Explain" subtitle="The query plan as a tree with costs and row estimates." >}}
  {{< card link="history" title="History" subtitle="Every statement you ran, searchable; Save As and Import." >}}
  {{< card link="saved-queries" title="Saved queries" subtitle=".sql files with names, keywords and folders; shared folders; deep links." >}}
  {{< card link="sql-files" title="SQL files" subtitle="Open a .sql file, Open recent, the read-only SQL viewer, drag and drop." >}}
{{< /cards >}}
