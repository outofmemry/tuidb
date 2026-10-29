---
title: MongoDB
description: What webtuidb supports on MongoDB, from authSource and TLS to collections as tables, sampled columns, raw filter documents, update documents, the shell evaluator and its safe-mode allow-list.
weight: 90
---

MongoDB (`driver = "mongodb"`) uses the official Go driver. In webtuidb a **collection is a table** and a document is a row. Its columns are the fields found in a sample of the collection, and nested documents and arrays show as JSON, with Json and Tree views for whole documents. The editor runs MongoDB shell JavaScript (`db.orders.find({...})`) or raw command documents. Your edits become `insertOne`, `updateOne` and `deleteOne` calls.

{{< ui tui="engines-mongodb" web="engines-mongodb" >}}The `customers` collection of the demo `shop` database. `_id` is the key; `tags` (an array) and `address` (a document) show as JSON. The web sidebar calls the group **Collections**; the dimmed numbers are estimated document counts.{{< /ui >}}

## Connecting

| Field | Default | Meaning on MongoDB |
|---|---|---|
| `host` / `port` | `127.0.0.1` / `27017` | One server, connected directly (no replica-set discovery). `mongodb+srv://` URLs are refused: `mongodb+srv:// is not supported (DNS seed lists); use mongodb://host:port`. |
| `user` / `password` | — | SCRAM login. Without a user, webtuidb still checks that the server lets you list collections, so a server that needs a login fails at connect, not later. |
| `database` | `test` | The database to open. Switch with {{< key "C-d k" >}}, or `use shop` in the editor. |
| `options.authSource` | `admin` | The database the user is defined in. It is the only option the driver reads. |
| `ssl_mode`, `ssl_ca`, `ssl_cert`, `ssl_key` | plain text | `require` encrypts without checking the certificate, `verify-ca` checks the chain, `verify-full` or `verify-identity` the host name too. |
| `bootstrap` | — | Shell statements run once after connecting. |
| `read_only` | off | Checked by the web UI only; see the [capability matrix](../capabilities/#safety-and-transport). |

The pool holds eight connections and server selection times out after 10 seconds. A URL may carry `?authSource=admin&tls=true` (`ssl=true` works too).

{{< callout type="info" >}}
MongoDB has no opportunistic TLS: `ssl_mode = prefer` and an empty `ssl_mode` connect in **plain text**.
{{< /callout >}}

## Sidebar

Collections and views of the current database, sorted by name, without `system.*`. Each collection shows `estimatedDocumentCount()`, fetched eight at a time. {{< key "C-d k" >}} lists the databases you are authorised for.

## Columns, values and paging

The columns come from a sample of the first 100 documents: `_id` first, then every field in the order it was first seen. Each field gets the type it had in the sample (`string`, `int`, `double`, `objectId`, `date`, …), or `mixed` when documents disagree. The sample is cached per collection, so a field that appears only in later documents is not a column until the table is reopened.

Rows come from `find(filter).sort(…).skip(offset).limit(n)`. The footer count is `estimatedDocumentCount()`. **Count exactly**, and any filtered count, run `countDocuments(filter)`.

{{< ui tui="engines-mongodb-json" web="engines-mongodb-json" >}}The Json view ({{< key "C-d J" >}}) shows every document whole, in extended JSON: a date is `{"$date": "2023-02-11T00:00:00Z"}`.{{< /ui >}}

## Filters

The filter bar builds a query document. Values are converted to the field's sampled type: `5` against an `int` field is the number 5. On a `mixed` field a number matches both the number and the text (`{"$in": [5, "5"]}`).

| Operator | Query |
|---|---|
| `=`, `<>`, `<`, `>`, `<=`, `>=` | `$eq`, `$ne`, `$lt`, `$gt`, `$lte`, `$gte` |
| `IN`, `NOT IN` | `$in`, `$nin` (comma-separated, quotes allowed) |
| `IS NULL`, `IS NOT NULL` | `{field: null}`, `{"$ne": null}` |
| `BETWEEN a AND b` | `{"$gte": a, "$lte": b}` (`$not` for NOT BETWEEN) |
| `LIKE` | an anchored regex from `%` and `_` |
| Contains, Not contains, Has prefix, Has suffix | case-insensitive regexes |

The **Any column** filter joins one condition per sampled field with `$or`. For negative operators (Not contains, `<>`, `NOT IN`, NOT BETWEEN) it uses `$and`, so "no field contains x".

The **raw filter** takes a query document. Extended JSON (`{"total": {"$gt": 100}}`) and shell style (`{total: {$gt: 100}, _id: ObjectId("…")}`) both work. Shell-style text is parsed as a document literal and must contain nothing executable. Only values and the BSON helpers `ObjectId`, `ISODate`, `NumberLong`, `NumberInt`, `NumberDecimal`, `Date`, `RegExp`, `UUID`, `BinData`, `Timestamp`, `MinKey`, `MaxKey`, `Long`, `Int32`, `Double` and `Decimal128` are allowed. Anything else is refused with `not a plain document: only values and BSON helpers like ObjectId() are allowed here`. Operators that run JavaScript on the server are refused too: `$where, $function and $accumulator are not allowed in a filter`.

## How edits become statements

Edits become shell statements, which the commit runs through the same evaluator as the editor:

- Insert: `db.customers.insertOne({"_id": ObjectId("…"), "email": "…", …})`. A new `_id` is generated when you leave it empty.
- Update: `db.customers.updateOne({"_id": …}, {"$set": {"country": "CA"}})`. A cell set to DEFAULT goes into `$unset` instead.
- Delete: `db.customers.deleteOne({"_id": …})`.

Values are converted to the field's sampled type, so a number stays a number and `_id` keeps its BSON type (`{"$numberLong": "1"}` in the preview). A collection whose name is not a plain identifier, or clashes with a `db` method, is written `db.getCollection("name")`.

{{< ui tui="engines-mongodb-preview" web="engines-mongodb-preview" >}}One edited field becomes an `updateOne` with `$set`. The web dialog adds that MongoDB applies statements one at a time.{{< /ui >}}

MongoDB commits are **not atomic**: statements run one after another, and the first failure stops the commit with the earlier ones applied.

## Collection operations

| Operation | Statement |
|---|---|
| Drop | `db.c.drop()` |
| Truncate | `db.c.deleteMany({})` |
| Rename | `db.c.renameCollection("new")` |
| Clone with data | `db.c.aggregate([{"$match": {}}, {"$out": "new"}])` |
| Clone without data | `db.createCollection("new")` |
| Create database | `db.getSiblingDB("d").createCollection("first")`, since MongoDB creates databases lazily: give the first collection's name |
| Drop database | `db.getSiblingDB("d").dropDatabase()` |

Rename and clone refuse an existing target (`collection "new" already exists`), since `$out` would silently replace it. **DDL** prints the collection's options and `createIndex` calls for each index except `_id_`; a view prints `db.createView(name, source, pipeline)`. The **Index** view lists the indexes. Collections have no fixed structure, so the structure editor answers `MongoDB collections have no fixed structure to edit`.

## Shell editor

The editor evaluates MongoDB shell JavaScript in an embedded JavaScript engine (goja), bound to the current database:

- `db.<collection>` and `db.getCollection("name")`, `db.getSiblingDB("other")`, `db.getCollectionNames()`, `db.createCollection()`, `db.createView()`, `db.dropDatabase()`, `db.runCommand({...})`, `db.adminCommand({...})`.
- On a collection: `find`, `findOne`, `aggregate`, `countDocuments` (also `count`), `estimatedDocumentCount`, `distinct`, `insertOne`, `insertMany` (also `insert`), `updateOne`, `updateMany`, `replaceOne`, `deleteOne`, `deleteMany` (also `remove`), `createIndex`, `dropIndex`, `getIndexes`, `drop`, `renameCollection`.
- On a cursor: `sort`, `limit`, `skip`, `project`/`projection`, `pretty`, `toArray`, `forEach`, `map`, `hasNext`, `next`, `itcount`, `count`.
- `use db`, `show dbs`, `show databases`, `show collections` and `show tables`, as in `mongosh`.
- `print()` output is shown. A raw command document, `{"listCollections": 1}`, is sent as `runCommand`, and the cursor's further batches are read up to the row limit and then closed on the server.

Completion offers these methods after `db.<collection>.` and after a closing `)`: `find`, `findOne`, `aggregate`, `count`, `insertOne`, `insertMany`, `updateOne`, `updateMany`, `deleteOne`, `deleteMany`, `sort`, `limit`, `skip` and `project`. Write results read like `insertedId: …`, `inserted 3 documents`, `matched 1, modified 1` or `deleted 2`.

## Safe mode

A shell statement counts as read-only only when it starts with `db` and every call in it is on the allow-list: `find`, `findOne`, `countDocuments`, `count`, `estimatedDocumentCount`, `distinct`, `aggregate`, `getIndexes`, `getCollectionNames`, `getCollectionInfos`, `getCollection`, `getName`, `stats`, the cursor methods (`sort`, `limit`, `skip`, `project`, `toArray`, `explain`, `hint`, `batchSize`, …) and the BSON helpers. The allow-list fails safe. `runCommand`, `eval`, `forEach` with a function, `deleteMany`, and member access by string (`db.c["deleteMany"]()`) all ask first. So does any text with a backtick, `=>`, `function`, `for`, `while`, `eval`, `$out`, `$merge`, `$where`, `$function` or `$accumulator`, even inside a string. `use db` and `show …` are reads. A command document is read-only when its first key is `find`, `aggregate`, `count`, `distinct`, `listCollections`, `listDatabases`, `ping`, `buildInfo`, `serverStatus`, `dbStats`, `collStats`, `listIndexes`, `hello` or `isMaster`.

## Tools

- **Process list**: `currentOp`, with the op type as state and the command as JSON. Kill runs `killOp` with the numeric id, or the `shard:123` string on mongos.
- **Backup / restore**: `mongodump` and `mongorestore` (archive files).
- No user management, explain or triggers.

## Limits

- `toArray()` and `forEach()` stop at 50,000 documents.
- Columns come from the first 100 documents. Fields that appear only later can be filtered by name or seen in the Json view.
- A commit is not a transaction.
