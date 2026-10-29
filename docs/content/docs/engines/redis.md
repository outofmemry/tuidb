---
title: Redis
description: What webtuidb supports on Redis, from db indexes, TLS and ACL users to keys as tables, folders by colon, stream paging, list deletes, MULTI/EXEC under WATCH and bounded replies.
weight: 80
---

Redis (`driver = "redis"`) uses go-redis. In webtuidb every **key is a table**. The sidebar lists keys, folded into folders at each `:`. Opening a key shows its elements as rows whose columns depend on the key's type, and your edits become Redis commands that run in one `MULTI`/`EXEC`. The "database" is the **db index**. The SQL editor takes Redis commands, one per statement.

{{< ui tui="engines-redis" web="engines-redis" >}}The hash `shop:product:1`, opened from the folders `shop` › `product`. A hash has the columns `field` and `value`; the sidebar shows each key's type and, for keys that expire, the time left (`ttl 25207s`).{{< /ui >}}

## Connecting

| Field | Default | Meaning on Redis |
|---|---|---|
| `host` / `port` | `127.0.0.1` / `6379` | The server. A single node: Cluster and Sentinel are not supported. |
| `user` | — | The ACL user name (Redis 6+). Leave it empty for `requirepass`. |
| `password` | — | `AUTH` password. |
| `database` | `0` | The db index, a number. Anything else fails with `redis database must be a db index, got "x"`. |
| `ssl_mode`, `ssl_ca`, `ssl_cert`, `ssl_key` | plain text | See below. |
| `bootstrap` | — | Commands sent on every new connection (`CLIENT SETNAME webtuidb`). |
| `read_only` | off | Checked by the web UI only; see the [capability matrix](../capabilities/#safety-and-transport). |
| `encoding`, `options` | — | Not used. |

The URL is `redis://user:password@host:6379/2` (db index 2) or `rediss://…`, which sets `ssl_mode = require`. The pool holds eight connections; the dial timeout is five seconds.

{{< callout type="info" >}}
Redis has no opportunistic TLS: `ssl_mode = prefer` (and an empty `ssl_mode`) connect in **plain text**. Use `require` for TLS without certificate checks, `verify-ca` to check the chain against `ssl_ca`, `verify-full` or `verify-identity` to check the host name too.
{{< /callout >}}

## Db indexes

{{< key "C-d k" >}} lists the db indexes `0` to `databases - 1`, from `CONFIG GET databases`, or 16 when the server does not answer. Opening one reconnects to it. In the editor, `SELECT 3` switches the whole session and answers `OK, now using db 3`. **Drop database** runs `FLUSHDB`, and only on the db index you are in: `switch to db 3 first: FLUSHDB only empties the current db`.

## Sidebar: keys and folders

Keys are listed with `SCAN … COUNT 1000`, sorted by name, at most 10,000 of them, each with its `TYPE` and, when it expires, its TTL in seconds. Names are split at every `:` into folders (`shop:product:1` sits in `shop` › `product`, labelled `1`). A folder shows how many keys it holds. A folder with a single key inside is merged into one line (`events:stream`), and a name that is both a key and a folder prefix shows both.

In the terminal, `l` or Right opens a folder, `h` or Left closes it, Space toggles it and `Enter` opens a key. In the web, click a folder to open or close it.

## Key types as tables

| Type | Columns | Read with | Notes |
|---|---|---|---|
| `string` | `value` | `GET` | One row. |
| `hash` | `field`, `value` | `HSCAN` | |
| `list` | `index`, `value` | `LRANGE` | `index` is the element's position. |
| `set` | `member` | `SSCAN` | |
| `zset` | `member`, `score` | `ZRANGE … WITHSCORES` | Ordered by score. |
| `stream` | `id`, `fields` | `XRANGE` | `fields` is the entry as JSON. Read-only. |
| `ReJSON-RL` (RedisJSON) | `value` | `JSON.GET` | Read-only. |

Other types (`vectorset`, module types) fail with `unsupported redis type "x"`. A key that has gone says `key "x" does not exist`. Values that are not valid UTF-8 are kept as bytes. The Json and Tree views ({{< key "C-d J" >}}, {{< key "C-d T" >}}) show each row as a document.

## Paging, filtering and counting

- Lists and sorted sets page by position (`LRANGE start stop`).
- Hashes and sets have no stable positions: each page scans from the start and skips what came before. A page cannot start deeper than 100,000 elements (`offset 200000 is too deep for a hash key (at most 100000): its elements have no stable positions, filter instead`).
- Streams page by entry ID. webtuidb remembers where the last pages started and ended, so the next page continues with `XRANGE key (lastid + COUNT n` instead of reading from the head again. The marks are dropped when the stream's first entry or (Redis 7) its max-deleted ID changes.
- Filters and sorts are applied in memory, on at most 100,000 elements of the key (the result is marked truncated beyond that). Supported operators: `=`, `<>`, `LIKE` (`%` and `_`), Contains, Not contains, Has prefix and Has suffix, all case-sensitive. Raw filters are refused (`raw filters are not supported for redis`), and so are the other operators the filter bar lists (`filter operator "<" is not supported for redis`). A sort compares numbers as numbers and anything else as text.
- The count is exact: `HLEN`, `LLEN`, `SCARD`, `ZCARD` or `XLEN`, 1 for a string, or the number of filtered elements.

## How edits become statements

Each change becomes one or more commands. **Preview** ({{< key "C-d P" >}}) shows them exactly as they will be sent:

| Type | Insert | Update | Delete |
|---|---|---|---|
| `string` | `SET key value` | `SET key value KEEPTTL` (the TTL stays) | `DEL key` |
| `hash` | `HSET key field value` | `HSET key field value`; a renamed field is `HDEL key old` + `HSET key new value` (refused when `new` exists) | `HDEL key field` |
| `list` | `RPUSH key value` | `LSET key index value` (the index itself cannot change) | `LSET key index <marker>` + `LREM key 1 <marker>` |
| `set` | `SADD key member` | `SREM key old` + `SADD key new` | `SREM key member` |
| `zset` | `ZADD key score member` (score 0 when empty) | `ZADD key score member`; a renamed member is `ZREM` + `ZADD`, keeping its score | `ZREM key member` |

Redis has no NULL and no DEFAULT. Setting a cell to either fails with `redis has no NULL` / `redis has no DEFAULT`. Editing a stream or a JSON key fails with `a stream key is read-only`.

{{< ui tui="engines-redis-list-delete" web="engines-redis-list-delete" >}}Deleting the element at index 1 of a list. `LREM` removes by value and would hit the first duplicate, so the chosen index is first overwritten with a unique marker, and the marker is removed.{{< /ui >}}

### Commit: MULTI/EXEC under WATCH

A commit sends every command in one `MULTI`/`EXEC`, under `WATCH` of every key it touches. First it checks that each key still has the type the plan assumed. `MULTI` only aborts on errors found while queueing, so a wrong type would otherwise fail halfway:

- A key whose type changed: `key "k" is now a list, not a hash; nothing was applied`.
- A key changed by someone else between the check and `EXEC`: `a key changed while applying; nothing was applied`.
- A command that fails inside `EXEC` (Redis runs the others anyway): `statement 2 (…): …; statements 1, 3 were applied and cannot be rolled back`.

## Key operations

| Operation | Command |
|---|---|
| Rename | `RENAME old new`. Refused when `new` exists (`key "new" already exists`), since `RENAME` would overwrite it. |
| Clone | `COPY key new` (Redis 6.2+); always with data: `redis cannot clone a key without its data`. |
| Drop, Truncate | `DEL key` |
| **DDL** | `TYPE`, `TTL`, `ENCODING` (`OBJECT ENCODING`) and `MEMORY … bytes` (`MEMORY USAGE`) |
| Create script | The commands that recreate the key (`HSET key f1 v1 f2 v2 …`, `RPUSH`, `SADD`, `ZADD`, one `XADD` per entry, `JSON.SET`) plus `EXPIRE` when it has a TTL. A key with more than 100,000 elements is `too large to script`. |
| Select script | `HSCAN key 0 COUNT 100`, `LRANGE key 0 99`, `SSCAN`, `ZRANGE key 0 99 WITHSCORES`, `XRANGE key - + COUNT 100`, `JSON.GET` or `GET` |

Redis keys have no structure to edit: the structure editor answers `redis keys have no table structure to edit`.

## Command editor

Each statement is one command line, split like `redis-cli` does: `"double quotes"` with `\n`, `\t` and `\xHH` escapes, `'single quotes'` taken literally, and a closing quote must be followed by a space (`closing quote must be followed by a space`). Command names are highlighted.

Every command runs on its own single-connection client. Cancel ({{< key "C-d ." >}}) closes it, which is the only way to stop a blocking command such as `BLPOP` or `SUBSCRIBE`. Replies are rendered by shape:

- `OK` and nil (`(nil)`) as a message;
- a string as one `value` row;
- an array as `index`/`value` rows, a map as `key`/`value` rows sorted by key, nested replies as JSON text;
- empty replies as `(empty array)` or `(empty map)`.

Replies are bounded: an array or map longer than the row limit ({{< key "C-d L" >}}) stops there and is marked truncated, so `KEYS *` or `HGETALL` on a huge key does not flood the UI.

## Safe mode

Redis commands are classified with an allow-list of read commands: `GET`, `MGET`, `GETRANGE`, `STRLEN`, `EXISTS`, `TYPE`, `TTL`, `PTTL`, `EXPIRETIME`, `KEYS`, `SCAN`, `RANDOMKEY`, `DBSIZE`, the `H*`, `L*`, `S*` and `Z*` readers (`HGETALL`, `LRANGE`, `SMEMBERS`, `ZRANGE`, `ZSCORE`, …), `XLEN`, `XRANGE`, `XREVRANGE`, `XINFO`, `PING`, `INFO`, `TIME`, `ECHO`, `BITCOUNT`, `BITPOS`, `GETBIT`, `PFCOUNT`, `GEODIST`, `GEOPOS`, `GEOSEARCH`, `JSON.GET` and `LASTSAVE`. `CONFIG`, `CLIENT`, `MEMORY` and `OBJECT` count as reads only with a reading subcommand (`GET`, `LIST`, `INFO`, `USAGE`, `ENCODING`, `FREQ`, `IDLETIME`). Every other command asks under Alert Mode 2 and Safe Mode 2.

## Tools

- **Process list** ({{< key "C-d ." >}}): `CLIENT LIST`, with address and name as the state and the last command as the query. Kill sends `CLIENT KILL ID <id>`.
- **Backup** ({{< key "C-d b" >}}): `redis-cli --rdb <file>` with the password in `REDISCLI_AUTH` and `--tls` when `ssl_mode` is set. **Restore** is refused: `redis restore: stop the server and copy the RDB file into its data directory`.
- No user management, explain, triggers or indexes.

## Limits

- The sidebar stops at 10,000 keys. Use the sidebar filter or `SCAN 0 MATCH prefix:* COUNT 1000` in the editor for larger keyspaces.
- Filtering, sorting and the create script load at most 100,000 elements of one key.
- On Redis 6, deleting an entry in the middle of a stream (`XDEL`) is not noticed by the stream marks. Reloading from the first page resets them.
- `json:doc` in the demo data is a plain `string` because the test server has no RedisJSON module; JSON keys need the module loaded.
