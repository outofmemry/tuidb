---
title: DynamoDB
description: What webtuidb supports on Amazon DynamoDB and DynamoDB Local, from regions, endpoints and credentials to Query or Scan browsing, PartiQL edits, ExecuteTransaction and table specs.
weight: 70
---

DynamoDB (`driver = "dynamodb"`) uses the AWS SDK for Go v2. It works against AWS and against DynamoDB Local or any compatible endpoint. DynamoDB has no logins, hosts or databases, so webtuidb reuses the connection fields: the "database" is a **region**, tables are the only objects, and the SQL editor speaks **PartiQL**. You can browse tables (with Query when your filters pin the partition key), edit items, see Json and Tree views, add and drop global secondary indexes, create, clone and truncate tables, and commit atomically.

{{< ui tui="engines-dynamodb" >}}A table on DynamoDB Local: the partition key `user` and sort key `started` come first, then every attribute seen in the items. Sets, lists and maps show as JSON; an item without an attribute shows `NULL`. The sidebar shows the item count and size from `DescribeTable`.{{< /ui >}}

## Connecting

| Field | Meaning on DynamoDB |
|---|---|
| `host` / `port` | Empty: the AWS endpoint for the region, always over TLS. Set: a custom endpoint such as DynamoDB Local, `http://host:port` unless `ssl_mode` is set (not `disable`) or `options.scheme = "https"`. **Write the host name only**, never `http://…`. |
| `user` / `password` | Access key id and secret access key. Give both, or neither to use the default AWS credential chain (environment, `~/.aws` shared config, SSO, instance role). One without the other fails with `give both the access key id (user) and the secret access key (password), or neither to use the default AWS credential chain`. |
| `database` | The region, such as `us-east-1`. `options.region` wins when both are set. |
| `ssl_mode`, `ssl_ca`, `ssl_cert`, `ssl_key` | TLS settings for a custom `https` endpoint. `prefer` and an empty mode add no settings of their own, but `prefer` still switches a custom endpoint to `https`. `require` skips certificate checks, `verify-ca` checks the chain only, `verify-full` checks the host too. |
| `bootstrap` | PartiQL or table statements run once after connecting. |

| Option | Meaning |
|---|---|
| `region` | The AWS region. Must look like `us-east-1`; otherwise `region "mars" is not an AWS region name (like us-east-1)`. |
| `profile` | A profile in the shared AWS config. |
| `session_token` | For temporary credentials, with `user` and `password`. |
| `endpoint` | A full URL (`http://localhost:8000`) that replaces `host` and `port`. It must not contain credentials. |
| `scheme` | `http` or `https` for an endpoint built from `host` and `port`. |

Without a region and without a custom endpoint the connection fails with `no region: add ?region=us-east-1 (or set the Database field / AWS_REGION)`. A custom endpoint without a region uses `us-east-1`. Missing credentials fail with `no AWS credentials: give an access key and secret, or configure the default chain (AWS_ACCESS_KEY_ID, ~/.aws, SSO)`. Against `localhost` or `127.0.0.1` webtuidb uses the dummy pair `local`/`local` instead, since DynamoDB Local accepts anything. Throttling (`ProvisionedThroughputExceeded` and friends) is retried up to eight times with backoff, at most five seconds apart.

The URL form is `dynamodb://ACCESSKEY:SECRET@host:port/?region=us-east-1`, or `dynamodb:///?region=eu-west-1&profile=work` for AWS.

{{< ui tui="engines-dynamodb-endpoint-error" web="engines-dynamodb-endpoint-error" >}}The mistake to avoid: `host = "http://127.0.0.1:58000"` becomes the endpoint `http://http://127.0.0.1:58000`. Use `host = "127.0.0.1"` with `port = 58000`, or `options.endpoint = "http://127.0.0.1:58000"`.{{< /ui >}}

## Regions as databases

{{< key "C-d k" >}} lists the AWS regions from a built-in list, since listing them needs `ec2:DescribeRegions`, which a DynamoDB-only login lacks. Opening one points the client at that region. Creating, dropping or renaming a region is refused: `a region is not something DynamoDB lets you create, drop or rename; open one with the region picker`.

## Sidebar

Every table of the region. The first 100 tables get `N items, size` from `DescribeTable`, fetched eight at a time. AWS refreshes those numbers about every six hours.

## Browsing, paging and counting

Items have no fixed schema. The grid's columns are the key attributes first, then every attribute seen in the first 100 items and in each page fetched since. An attribute's type (`S`, `N`, `B`, `BOOL`, `SS`, `NS`, `BS`, `L`, `M`, `NULL`) comes from the items, or is `mixed` when they disagree.

- **Query or Scan**: when the filters pin the partition key with `=` (plus at most one condition on the sort key, and no **Match any**), the table is read with `Query`. Otherwise it is read with `Scan`. Every filter becomes a native `KeyConditionExpression` or `FilterExpression`, with every attribute behind a `#name` placeholder, so reserved words and odd characters never break it.
- **Sort**: a `Query` sorted by the sort key is ordered by the server. Any other sort orders only the rows of the fetched page.
- **Paging**: there is no `OFFSET`. Page 3 reads and discards the items of pages 1 and 2, which costs their read capacity.
- **Count**: the estimate is `DescribeTable`'s `ItemCount`. When that is 0, and for every filtered count, the table is counted exactly with a `Select = COUNT` Scan or Query, which reads every item.
- A read with no row limit stops at 50,000 items.

Filter operators: `=`, `<>`, `<`, `>`, `<=`, `>=`, `IN`, `NOT IN`, `IS NULL`, `IS NOT NULL`, `BETWEEN`, `NOT BETWEEN`, Contains (`contains()`), Not contains and Has prefix (`begins_with()`). `LIKE` and Has suffix do not exist in DynamoDB: `LIKE is not supported by DynamoDB: use Contains or Has prefix`. On a set, list or map attribute only Contains, Not contains, `IS NULL` and `IS NOT NULL` work. A raw filter is a DynamoDB condition expression with plain names and literals, such as `age < 40 AND begins_with(name, 'A')`: quoted strings, numbers, `true`/`false` and `null` become value placeholders, and bare words become name placeholders.

## How edits become statements

Pending edits become PartiQL statements with bound parameters, so numbers, binary values, sets and nested documents keep their DynamoDB types. **Preview** shows the values inlined:

- Insert: `INSERT INTO "docs_engines_sessions" VALUE {'user': ?, 'started': ?, 'device': ?}`. Every key attribute must have a value: `insert into t needs a value for key attribute "started"`.
- Update: `UPDATE "t" SET "device" = ? WHERE "user" = ? AND "started" = ?`. A cell set to DEFAULT removes the attribute (`REMOVE "device"`). Key attributes cannot change in place: `key attribute "user" of t cannot be changed in place: insert a new item and delete the old one`.
- Delete: `DELETE FROM "t" WHERE <full key>`.

{{< ui tui="engines-dynamodb-preview" >}}Preview of one edited attribute and one deleted item. Both go to the server in one `ExecuteTransaction`.{{< /ui >}}

A commit runs consecutive PartiQL statements in one **ExecuteTransaction**: all or nothing. DynamoDB allows at most 100 statements in a transaction, so a larger commit is refused before anything is sent: `120 statements in one commit exceed DynamoDB's limit of 100 per transaction; commit fewer rows at a time (nothing was applied)`. A cancelled transaction names each statement and its reason, for example `ConditionalCheckFailed`, and says `nothing was applied`.

## Tables, keys and indexes

The structure view lists the key attributes (`partition key`, `sort key`), index keys (`index key: by_device`) and the sampled attributes. The **Index** view shows `PRIMARY`, each GSI and each LSI with its projection (`ALL`, `KEYS_ONLY`, `INCLUDE a, b`). **DDL** is an abridged `aws dynamodb describe-table` in JSON, plus the time-to-live and stream settings.

The structure editor declares only what a DynamoDB table declares: its key attributes (types `S`, `N` or `B`) and its indexes. Adding a global secondary index becomes `ALTER TABLE "t" ADD INDEX {json}`, and dropping one becomes `ALTER TABLE "t" DROP INDEX "name"`. These are refused, each with the reason:

- declaring a non-key attribute: attributes exist once an item has them;
- renaming, retyping or dropping an attribute (`attribute "a" cannot be renamed to "b": DynamoDB stores the name in every item (UPDATE each item instead)`);
- changing the primary key;
- a unique index;
- adding or dropping a local secondary index after the table exists (`LSIs only exist from table creation`).

| Statement | What it does |
|---|---|
| `CREATE TABLE "t" {"keys": [{"name": "user", "type": "S"}, {"name": "started", "type": "N"}], "billing": "PAY_PER_REQUEST", "indexes": [...]}` | Creates a table and waits until it is `ACTIVE`. Keys list the partition key first. `billing` is `PAY_PER_REQUEST` (default) or `PROVISIONED` with `rcu` and `wcu`. An index has `name`, `kind` (`GSI`, the default, or `LSI`), `keys`, `projection` (`ALL`, `KEYS_ONLY`, `INCLUDE` with `include`). |
| `CREATE TABLE "copy" LIKE "t" [WITH DATA]` | Clone: the same keys, billing and indexes, then (with data) every item copied in batches. |
| `TRUNCATE TABLE "t"` | Deletes every item in batches; the table stays. |
| `DROP TABLE "t"` | Deletes the table. |
| `ALTER TABLE "t" ADD INDEX {json}` / `DROP INDEX "name"` | Adds or removes a GSI and waits for it. |

These statements are what **Clone**, **Truncate**, **Drop** and the structure editor produce, and you can type them in the SQL editor too. They are not atomic: a copy that stops midway says `table copy was created but copying stopped after 400 items`. A table cannot be renamed: `DynamoDB cannot rename a table: clone it with its data (Clone…) and then delete the original`.

## SQL editor

The editor runs PartiQL through `ExecuteStatement` and follows `NextToken` until the row limit. The "select" script of a table (Copy Script As) is `SELECT * FROM "t"` without a limit, since PartiQL has no `LIMIT`; the editor's row limit stops the read. A write answers `statement executed (DynamoDB does not report affected rows)`. Explain, the process list and user management do not exist on DynamoDB.

## Safe mode

Only `SELECT` is read-only. `INSERT`, `UPDATE`, `DELETE`, `EXISTS` (transaction-only) and the table statements above are writes, and so is any text holding more than one statement.

## Limits

- Columns come from sampling. An attribute that appears only in items you have not loaded yet is missing from the grid until a page brings one in.
- Deep pages and exact counts read, and pay for, every item before them.
- Commits of more than 100 changed items must be split, because of the transaction limit.
