---
title: Tools
weight: 10
---

The tools around the data: find a value anywhere in the database, watch and stop what the server is running,
manage users and their privileges, draw the tables and their foreign keys, build dashboards out of read-only
queries, and compare two schemas or two tables. Each tool works the same in the terminal and in the browser,
and anything that changes the database goes through the Preview and the [safe mode](../safety/) gate first.
Bulk update and fake data live with the other editing tools in [Editing data](../editing-data/).

{{< cards >}}
  {{< card link="search-in-database" title="Search in Database" subtitle="Find a value in every table of a schema, then open the hit as a filtered table." >}}
  {{< card link="process-list" title="Process list" subtitle="The server's running sessions and queries, with sort, copy, export and kill." >}}
  {{< card link="users" title="Users and privileges" subtitle="PostgreSQL roles and MySQL accounts: create, password, attributes, grants, limits." >}}
  {{< card link="er-diagram" title="ER diagram" subtitle="Tables, columns and foreign keys as boxes; add a key, colour a table, open its structure." >}}
  {{< card link="metrics-boards" title="Metrics boards" subtitle="Dashboards of read-only query panels in a TOML file, with variables and auto refresh." >}}
  {{< card link="schema-diff" title="Schema diff" subtitle="The SQL that turns one schema into another, across schemas or connections." >}}
  {{< card link="data-diff" title="Data diff" subtitle="The inserts, updates and deletes that make one table hold the rows of another." >}}
{{< /cards >}}
