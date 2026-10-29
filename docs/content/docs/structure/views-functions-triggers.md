---
title: Views, functions and triggers
description: Definitions open as runnable SQL in a query tab so safe mode applies; materialized views, procedures, sequences and types.
weight: 40
---

Views, materialized views, functions, procedures and triggers are not edited in a form. Opening one from the sidebar (or {{< key "C-d M-t" >}}, `triggers`, from a table tab for its triggers) puts its definition into a query tab as the `CREATE OR REPLACE` statement the engine understands. You edit the text and run it like any statement, which means the change goes through the connection's [safe mode](../../safety/safe-mode/) and `read_only` checks and lands in the history.

Engine notes:

- **PostgreSQL** uses `CREATE OR REPLACE` for views and functions and `CREATE OR REPLACE TRIGGER` where the server supports it. A materialized view offers *Refresh* from its menu.
- **MySQL and MariaDB** cannot replace a routine in place, so the generated tab creates the new routine under a temporary name, drops the old one, recreates it under the original name and drops the temporary one, all in one run, with `DELIMITER` handling in the splitter. If the first `CREATE` is rejected the old routine is untouched.
- **ClickHouse** views and materialized views open as their `CREATE` statement.
- The terminal also has a grid editor for a table's triggers (event, timing, function) that writes the same statements.

Sequences and custom types are listed in the sidebar with their current value or definition; `[sidebar] show_sequences` and `show_types` control whether they appear.
