---
title: MariaDB
description: What webtuidb supports on MariaDB, which reuses the MySQL driver and adds sequences, native UUID and INET types, JSON detection and system-versioned tables.
weight: 40
---

MariaDB (`driver = "mariadb"`) speaks the MySQL protocol and dialect, so it runs the [MySQL](../mysql/) session code. Everything on that page applies here: connection fields, TLS, paging, implicit DDL commits, `DELIMITER`, pinned sessions and safe mode. This page lists only what differs. The driver notices it is talking to MariaDB from the `-MariaDB` suffix of the server version, and error messages say "MariaDB" where they name the engine.

{{< ui tui="engines-mariadb" web="engines-mariadb" >}}The structure of `devices` on MariaDB 11.4: native `uuid`, `inet4` and `inet6` columns, a `json` column, an `INVISIBLE` column, and a default taken from the sequence `invoice_seq`, which the sidebar lists under Sequences.{{< /ui >}}

## What MariaDB adds

| Feature | Behaviour |
|---|---|
| Sequences | Listed in their own sidebar group; the row count is not shown, because a sequence is a one-row table. **DDL** is `SHOW CREATE SEQUENCE`, rename is `RENAME TABLE`. |
| Column types | The structure editor and **Set Value** offer MariaDB 11's types: `tinyint` … `bigint`, `decimal(10,2)`, `float`, `double`, `bit(1)`, `boolean`, `char(36)`, `varchar(255)`, the text and blob sizes, `binary(16)`, `varbinary(255)`, `enum('a','b')`, `set('a','b')`, `date`, `time`, `datetime`, `timestamp`, `year`, `json`, `uuid`, `inet4`, `inet6`, `point`, `linestring`, `polygon`, `geometry`. |
| JSON columns | MariaDB stores `JSON` as `LONGTEXT COLLATE utf8mb4_bin` plus a `CHECK (json_valid(col))`. Such columns are reported as `json`, so a `MODIFY` never spells them `longtext` and drops the check. |
| System-versioned tables | Recognised from `TABLE_TYPE = 'SYSTEM VERSIONED'`. MariaDB refuses `TRUNCATE` on them (error 4137), so truncate runs `DELETE FROM t` and then `DELETE HISTORY FROM t`. Every `ALTER` gets the prefix `SET STATEMENT system_versioning_alter_history=KEEP FOR`, because MariaDB refuses to alter them otherwise (error 4119). |
| Process list | MariaDB's extra `Progress` column is read and ignored. |
| Backup / restore | `mariadb-dump` and `mariadb` when found on `PATH`, else `mysqldump` and `mysql`. A path in the connection's `tools` table wins over both. |

## Safe mode and sequences

Reading a sequence moves it. A `SELECT` or `WITH` that contains `NEXTVAL(…)`, `SETVAL(…)` or `NEXT VALUE FOR` is therefore not read-only. Alert Mode 2 and Safe Mode 2 ask before running it, as they would for an `UPDATE`. `SET STATEMENT … FOR ALTER …` counts as DDL, so a batch containing it runs without a transaction, as described under [MySQL](../mysql/#how-edits-become-statements).

## Limits

- A sequence's options (`INCREMENT`, `CACHE`, `CYCLE`) are changed in the SQL editor, starting from its DDL.
- MariaDB has no schemas and no materialized views. Its `RENAME DATABASE` does not exist, so rename is refused as on MySQL.
