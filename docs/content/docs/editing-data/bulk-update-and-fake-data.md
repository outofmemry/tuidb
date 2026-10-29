---
title: Bulk update and fake data
description: Change every row that matches the current filter in one statement, or fill a table with generated rows.
weight: 60
---

## Bulk update

{{< key "M-b" >}} (`bulk_update`) in a grid opens the Bulk update dialog for the table in the tab. Pick the column, choose a value the same way as in [Set Value](../set-value/) (literal, NULL, EMPTY, DEFAULT or an expression), and the dialog builds one `UPDATE table SET column = … WHERE …` using the filters currently applied in the [filter bar](../../browsing-data/filter-bar/). The statement is shown before it runs, the row count it will touch is estimated from the current filter, and it goes through safe mode like any other write. Without a filter it updates the whole table, and the dialog says so. It is also available from the table's menu in the sidebar and in the web toolbar's ⋯ menu.

## Fake data

Fake data, from the table menu, inserts generated rows into the table in the tab. The dialog lists the table's columns with a generator chosen from the column type (names and e-mails for text, ranges for numbers, recent dates for timestamps, existing keys for foreign-key columns) and lets you change each one, set the number of rows, and preview the first rows before they are inserted. The insert runs in a background job (visible in the [jobs centre](../../workspace/jobs-and-notifications/)) through the normal commit path, so safe mode and `read_only` apply, and the rows land in the history as a GUI commit.

{{< callout type="info" >}}
Both tools are gated by the connection's capabilities: engines without SQL-style updates (Redis, DynamoDB, Cassandra for bulk update across partitions) show the entry greyed out with the reason.
{{< /callout >}}
