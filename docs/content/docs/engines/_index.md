---
title: Engines
weight: 12
---

webtuidb talks to eight database servers with drivers of its own: PostgreSQL, MySQL, MariaDB, ClickHouse, Cassandra, DynamoDB, Redis and MongoDB. Four more names, Redshift, Greenplum and Vertica (on the PostgreSQL driver) and SingleStore (on the MySQL driver), are profiles: the same driver with the capabilities that engine really has. Both front ends use the same drivers. What a page says about an engine is true in the terminal and in the browser, unless it says otherwise.

Each page covers the connection fields and defaults, TLS, what the sidebar lists, how rows are paged and counted, how your edits become statements, what the SQL editor accepts, how safe mode reads statements, and the engine's limits.

{{< cards >}}
  {{< card link="capabilities" title="Capability matrix" subtitle="Every feature against every engine, on one page." >}}
  {{< card link="postgresql" title="PostgreSQL" subtitle="Schemas, sequences, types, pinned transactions, server-side read-only." >}}
  {{< card link="mysql" title="MySQL" subtitle="Implicit DDL commits, charset override, generated columns, DELIMITER." >}}
  {{< card link="mariadb" title="MariaDB" subtitle="The MySQL driver plus sequences, UUID and INET types, system-versioned tables." >}}
  {{< card link="clickhouse" title="ClickHouse" subtitle="Native protocol, mutations, USE, server-side BACKUP." >}}
  {{< card link="cassandra" title="Cassandra" subtitle="Keyspaces, partition and clustering keys, ALLOW FILTERING, logged batches." >}}
  {{< card link="dynamodb" title="DynamoDB" subtitle="Regions, Query or Scan, PartiQL, ExecuteTransaction, table specs." >}}
  {{< card link="redis" title="Redis" subtitle="Keys as tables, folders by colon, MULTI/EXEC, stream paging." >}}
  {{< card link="mongodb" title="MongoDB" subtitle="Collections, shell JavaScript, raw filter documents, update documents." >}}
  {{< card link="profiles" title="Profile drivers" subtitle="Redshift, Greenplum, Vertica and SingleStore: what is switched off and why." >}}
{{< /cards >}}
