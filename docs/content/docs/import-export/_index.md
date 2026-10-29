---
title: Import, export and backup
weight: 9
---

Move data in and out of a database. The import wizard loads CSV, TSV, JSON and SQL dump files into a table; export writes tables, filtered rows, pages and query results to CSV, JSON, SQL or Excel; backup and restore run the engine's own dump tools on a whole database. All three run in the background, show up in the jobs centre ({{< key "C-d M-J" >}}), and work the same way in the terminal and in the browser, with the differences named on each page.

{{< cards >}}
  {{< card link="import/" title="Import data" subtitle="CSV, TSV, JSON and SQL dumps: preview, column mapping, NULL handling, one transaction per file." >}}
  {{< card link="export/" title="Export data" subtitle="Tables, filtered rows, pages, results and several tables to CSV, JSON, SQL INSERTs or Excel, with gzip and name patterns." >}}
  {{< card link="backup-restore/" title="Backup and restore" subtitle="pg_dump, mysqldump, mongodump, redis-cli and ClickHouse BACKUP, with tool paths, live output and restore into a new database." >}}
{{< /cards >}}
