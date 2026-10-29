---
title: Browsing data
weight: 4
---

A table tab shows rows in a ledger-style grid: row numbers on the left, the primary key marked, NULL dimmed, numbers flush right, one page at a time. Around the grid sit the tools for finding what you want without writing SQL: sorting by one or several columns, the filter bar with every operator the engine supports, the one-key quick filter, find and replace, quick look for long or binary values, the row-detail view, column choice, following foreign keys, copying cells in several formats, and the document views for MongoDB, Redis and DynamoDB. Everything here is read-only; changing rows is covered in [Editing data](../editing-data/).

{{< cards >}}
  {{< card link="data-grid" title="Data grid" subtitle="Row numbers, NULL marks, paging, counts, column widths, navigation keys and mouse actions." >}}
  {{< card link="sorting" title="Sorting" subtitle="One column or several, from a key, a header click or the sort dialog; how the sort is remembered." >}}
  {{< card link="filter-bar" title="Filter bar" subtitle="Column conditions, every operator per engine, raw conditions, the generated WHERE clause, remembered filters." >}}
  {{< card link="quick-filter" title="Quick filter" subtitle="Filter on the value under the cursor in one step." >}}
  {{< card link="find" title="Find and replace" subtitle="Search the grid or a result by column and operator; replace one match or all as pending edits." >}}
  {{< card link="quick-look" title="Quick look" subtitle="Raw, tree, pretty and hex views of one value; images, PDFs, geometry and encodings." >}}
  {{< card link="row-detail" title="Row detail" subtitle="One record as a form, quick values, several rows at once." >}}
  {{< card link="columns" title="Columns" subtitle="Choose visible columns, hide and show, widths, copy a column." >}}
  {{< card link="foreign-keys" title="Foreign keys" subtitle="Follow a key to the row it points to and come back." >}}
  {{< card link="copy-as" title="Copy as" subtitle="Cells, rows and columns as CSV, TSV, JSON, SQL and more; the CSV settings; the clipboard over SSH." >}}
  {{< card link="document-views" title="Document views" subtitle="Json and Tree views for MongoDB, Redis and DynamoDB; Redis key types; Cassandra." >}}
{{< /cards >}}
