---
title: Schemas and databases
description: Create, rename and drop schemas and databases, switch the active schema, and show or hide system schemas.
weight: 50
---

**Switch schema** with {{< key "s" >}} in the sidebar (the picker lists the schemas of the current database); **open another database** of the same connection with {{< key "C-d k" >}} (`open_database`), which opens a new workspace so tabs of the two databases stay apart.

**Create** a schema with {{< key "C-d M-n" >}} (`new_schema`) in the sidebar or from the **+** menu; create a database from the same menu, with encoding and collation fields on engines that take them. **Rename** and **drop** are in the database picker's menu and the sidebar menu. Dropping a schema asks whether to cascade (the browser asks the same question instead of always cascading); dropping a database requires typing its name, and {{< key "Enter" >}} never confirms it. Every statement is previewed and goes through safe mode.

System schemas (`pg_catalog`, `information_schema`, `sys`, MySQL's `mysql` and `performance_schema`) are hidden unless `[sidebar] load_system_schemas` is on. On ClickHouse switching the database issues `USE`; on MongoDB the picker lists databases and collections; Redis has numbered databases selected in the connection.
