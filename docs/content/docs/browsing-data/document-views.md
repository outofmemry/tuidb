---
title: Document views
description: The Json and Tree views for MongoDB, Redis and DynamoDB, how Redis key types are shown, and what the grid means for Cassandra.
weight: 110
---

Engines without fixed columns get two extra views of a collection, key or item set next to the grid: {{< key "C-d J" >}} (`show_json`) shows each record as JSON, {{< key "C-d T" >}} (`show_tree`) as a collapsible tree. The browser has the same two buttons in the tab's toolbar. Editing in these views produces the same pending marks as the grid.

**MongoDB**: a collection is paged as documents; the grid flattens the top-level fields into columns and nested fields show as JSON cells. The filter bar accepts field conditions or a raw filter document, which is parsed as a document (never evaluated), with `$expr` limited to an allow-list. Edits become update documents keyed by `_id`.

**Redis**: the sidebar folds keys on `:` into folders. Each key type has its own shape: strings are one value, hashes become field/value rows, lists and sets one element per row, sorted sets score/member rows, and streams are paged by entry id. The key's TTL is shown and editable. Deleting list elements is done by index, and every write checks the key type first before running inside `MULTI`.

**DynamoDB**: a table is scanned a page at a time; items show as JSON with their attribute types, and edits become `Put`/`Update`/`Delete` on the item's key. The Json and Tree views apply here as on MongoDB.

**Cassandra**: tables are relational enough for the ordinary grid; the sidebar is organised by keyspace and the preview shows CQL. Paging a query result further is not available on Cassandra and DynamoDB, which have no subqueries.

Engine specifics are collected under [Engines](../../engines/).
