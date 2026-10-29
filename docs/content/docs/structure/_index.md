---
title: Structure
weight: 6
---

Beside the data, every table tab has a structure side: its columns with types, defaults and keys, its indexes, and the CREATE script the server would produce. The structure editor turns changes to those into a plan of ALTER statements that you review before they run, the same way a data commit is previewed. The pages here cover the views, the editor, creating a table, views and functions and triggers, schemas and databases, and the operations in a table's menu.

{{< cards >}}
  {{< card link="structure-view" title="Structure, Index and DDL views" subtitle="Columns, keys, indexes, triggers and the CREATE script of a table." >}}
  {{< card link="structure-editor" title="Structure editor" subtitle="Add, rename, retype and drop columns; primary key, foreign keys and indexes; review the ALTER statements before they run." >}}
  {{< card link="new-table" title="Create a table" subtitle="Design a table column by column, preview the CREATE TABLE and send it." >}}
  {{< card link="views-functions-triggers" title="Views, functions and triggers" subtitle="Definitions opened as runnable SQL so safe mode applies; materialized views, procedures, sequences and types." >}}
  {{< card link="schemas-and-databases" title="Schemas and databases" subtitle="Create, rename and drop schemas and databases, switch schema, the system schemas." >}}
  {{< card link="table-operations" title="Table operations" subtitle="Truncate, drop, rename, duplicate, import into, export, diagram and the rest of the table menu." >}}
{{< /cards >}}
