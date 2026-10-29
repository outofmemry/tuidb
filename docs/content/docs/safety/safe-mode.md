---
title: Safe mode
description: The five safe mode levels, what counts as a read-only statement, the confirmation and password prompts, and every place safe mode applies, in the terminal and in the browser.
weight: 10
---

Safe mode decides what happens between "run" and the statement reaching the server: nothing, a
confirmation, or the database password. Each connection has its own level, so a production connection can
ask before every write while your local database sends queries straight away. Both UIs apply the same
level, and the web server checks it again on its side, so the browser cannot skip it.

{{< ui tui="safety-safe-mode-alert" web="safety-safe-mode-alert" >}}Alert Mode 2 stops an `UPDATE` before it is sent. Terminal: the read-only production replica; web: Shop (Postgres) set to Alert Mode 2.{{< /ui >}}

## The five levels

| Level (`safe_mode`) | Label | Read-only statements | Every other statement |
|---|---|---|---|
| `silent` | Silent Mode | sent | sent |
| `alert1` | Alert Mode 1 | confirm | confirm |
| `alert2` | Alert Mode 2 | sent | confirm |
| `safe1` | Safe Mode 1 | database password | database password |
| `safe2` | Safe Mode 2 | sent | database password |

"Read-only" means the statement classifier below calls it SELECT/EXPLAIN/SHOW-like. The descriptions in the
level picker say the same in short: "send queries with no warning", "confirm every query", "confirm all but
SELECT/EXPLAIN/SHOW", "password for every query", "password for all but SELECT/EXPLAIN/SHOW".

A batch is judged as a whole: you get one prompt for everything you sent, and the batch counts as
read-only only when every statement in it is. One `DELETE` among ten `SELECT`s makes the whole batch ask.

The password levels ask for the password that was used to open the connection: the `password` from
`connections.toml`, the output of `password_cmd`, or what you typed for a `password = "ask"` connection.
Typing it is the confirmation; there is no separate "send?" step.

## Changing the level

{{< ui tui="safety-safe-mode-picker" web="safety-safe-mode-picker" >}}The level picker on the production replica. The current level is marked with a dot.{{< /ui >}}

| | Terminal | Web |
|---|---|---|
| Open the picker | {{< key "C-d S" >}} (`safe_mode`) | {{< key "C-d S" >}}, or click the level chip ("Alert Mode 2") in the top bar |
| Pick a level | `j`/`k` or arrows, {{< key "Enter" >}} | click the row |
| Cancel | {{< key "Esc" >}} | {{< key "Esc" >}} or ✕ |

The new level applies at once to the open connection and is written to that connection's `safe_mode` in
`connections.toml`, so the toolbar, the config file and the next session agree. A connection that comes from
a `[[source]]` folder lives in a file webtuidb does not write: the level changes for this session only. In the
browser, if `connections.toml` no longer parses, the level also changes for the session only and the file is
left alone.

### Lowering a password level

Moving away from Safe Mode 1 or Safe Mode 2, to any other level, asks for the database password first. Moving
up never asks.

{{< ui tui="safety-safe-mode-change" web="safety-safe-mode-change" >}}Leaving Safe Mode 2 asks for the database password.{{< /ui >}}

- Terminal: a form titled "Enter the database password to change safe mode" with one Password field; a wrong
  password shows "wrong password" and keeps the form open.
- Web: the picker turns into "Enter the database password to change safe mode." with a password field,
  **Back** and **Change**; a wrong password shows "Wrong password".
- When the connection has no password at all, there is nothing to check. The terminal shows a red dialog
  "This connection has no stored password. Change safe mode to …?" where only `y` changes the level; the
  web shows "confirm changing safe mode from Safe Mode 2" with **Change**.

## What the prompts look like

### Confirmation (Alert Mode 1 and 2)

- Terminal: a dialog titled "Alert Mode 2: send to the server?" (the level's label first) with every
  statement of the batch, highlighted as SQL. **Send** ({{< key "Enter" >}} or `y`) sends it, **Cancel**
  ({{< key "Esc" >}}, `n` or `q`) drops it. `j`/`k` or the arrows scroll a long batch.
- Web: the SQL editor shows "Run these statements?" with the statements, a chip with the level and "check the
  statements above, then continue", and **Cancel** / **Run**. Committing table edits shows "Send N
  statements?" with the same chip and **Send changes**; import, kill process and backup/restore show the chip
  inside their own dialog.

### Password (Safe Mode 1 and 2)

{{< ui tui="safety-safe-mode-password" web="safety-safe-mode-password" >}}Safe Mode 2 asks for the database password before an `UPDATE`.{{< /ui >}}

- Terminal: a form titled "Safe Mode 2: enter the database password" with a Password field and, under it,
  the first three statements joined by `|` ("… +N more" when there are more). {{< key "Enter" >}} sends;
  a wrong password shows "wrong password" and keeps the form open with nothing sent.
- Web: the same dialog as the confirmation, with "enter the database password to continue" and a password
  field. The **Run** / **Send changes** button stays disabled until something is typed. A wrong password comes
  back from the server as "wrong password".

### Password level without a stored password

If the connection was opened without a password, a blank field must not pass as "the database password".

- Terminal: a red dialog "Safe Mode 2: this connection has no stored password. Send?". It is a destructive
  dialog: only `y` sends; {{< key "Enter" >}}, {{< key "Esc" >}} and `n` cancel.
- Web: the server turns the password gate into a plain confirmation, so you see the confirmation dialog
  described above.

## What counts as read-only

The classifier reads each statement with the lexical rules of its engine, so that a string or comment cannot
hide a write from it, and answers "read-only" only when it is sure. Anything it cannot decide counts as a
write, which means you are asked once more, never once less.

{{< tabs items="SQL engines,MongoDB,Redis,Cassandra,DynamoDB" >}}

{{< tab >}}
PostgreSQL (and Redshift, Greenplum, Vertica), MySQL (and MariaDB, SingleStore) and ClickHouse:

| Statement | Read-only? |
|---|---|
| `SELECT …`, `VALUES …`, `TABLE …`, also inside leading parentheses `(SELECT 1)` | yes, unless it contains `INTO` (`SELECT … INTO new_table`) |
| `SHOW …`, `DESCRIBE …`, `DESC …`, `EXISTS …` | yes |
| `WITH … SELECT …` | yes, unless any of `INSERT`, `UPDATE`, `DELETE`, `MERGE`, `CREATE`, `DROP`, `ALTER`, `TRUNCATE`, `REPLACE`, `UPSERT`, `INTO`, `GRANT`, `REVOKE`, `COPY`, `EXECUTE`, `CALL`, `DO` appears in it (`replace(` the string function does not count) |
| `EXPLAIN …` | yes |
| `EXPLAIN ANALYZE …` / `EXPLAIN ANALYSE …` | only when the explained statement has none of the words above (it actually runs the statement) |
| several statements in one text (`SELECT 1; DELETE …`) | no |
| everything else (`INSERT`, `UPDATE`, `DELETE`, DDL, `SET`, `VACUUM`, `EXECUTE`, `CALL`, `DO`, `COPY` …) | no |
| empty text or only comments | yes |

Lexical rules that decide what is code:

- PostgreSQL: `''` doubles a quote, a backslash escapes only inside `E'…'` strings, `$$…$$` and `$tag$…$tag$`
  are strings, `/* */` comments nest, and `#` is an operator, not a comment. `SELECT 6 # 3 INTO t` is a
  write; `/* a /* b */ INTO */ SELECT 1` is read-only.
- MySQL / MariaDB: backslash escapes in strings, `#` comments, `--` is a comment only when followed by a space
  (`--1` is arithmetic), and `/*! … */`, `/*!50000 … */` and MariaDB's `/*M! … */` are executed code, not
  comments: `SELECT 1 /*! INTO @x */` is a write. On MariaDB, reading a sequence changes it, so
  `NEXTVAL(…)`, `SETVAL(…)` and `NEXT VALUE FOR …` make a `SELECT` a write (`LASTVAL` does not).
- ClickHouse: backslash escapes everywhere (also in backticks), `#` and `--` comments.

Quoted strings and identifiers are opaque: `select 'delete' as a` and `SELECT 'INTO' AS x, "into" FROM t`
are read-only.

{{< callout type="warning" >}}
The classifier looks at keywords, not at what functions do. A `SELECT` that calls a function with side
effects (`SELECT nextval('invoice_seq')` on PostgreSQL, `SELECT pg_terminate_backend(…)`, a user function
that writes) is "read-only" to it, so Alert Mode 2 and Safe Mode 2 let it through. Use Alert Mode 1 or Safe
Mode 1 on a connection where that matters.
{{< /callout >}}
{{< /tab >}}

{{< tab >}}
MongoDB uses an allow-list: a statement is read-only only if everything it calls is on the list.

- `show collections`, `show dbs`, `show databases`, `show tables`, `show users`, `show roles` and
  `use <db>` are read-only.
- A shell statement must start with `db` and may call only: `find`, `findOne`, `countDocuments`, `count`,
  `estimatedDocumentCount`, `distinct`, `aggregate`, `getIndexes`, `getCollectionNames`,
  `getCollectionInfos`, `getCollection`, `getName`, `stats`, `sort`, `limit`, `skip`, `project`,
  `projection`, `toArray`, `pretty`, `explain`, `hint`, `collation`, `batchSize`, `min`, `max`,
  `maxTimeMS`, `size`, `itcount`, `hasNext`, `next`, `tryNext`, `allowDiskUse`, `comment`, plus the value
  constructors `ObjectId`, `ISODate`, `NumberLong`, `NumberInt`, `NumberDecimal`, `Date`, `RegExp`,
  `UUID`, `BinData`, `Timestamp`, `MinKey`, `MaxKey`, `Long`, `Int32`, `Double`, `Decimal128`.
- A command document (`{"find": "users"}`) is read-only when its first key is one of `find`, `aggregate`,
  `count`, `distinct`, `listCollections`, `listDatabases`, `ping`, `buildInfo`, `serverStatus`,
  `dbStats`, `collStats`, `listIndexes`, `hello`, `isMaster`.
- Always a write: anything with a backtick (template literals run `${…}` before anything else), `$out`,
  `$merge`, `$function`, `$accumulator`, `$where`, `\u` or `\x` escapes, `=>`, `function`, `while`, `for`,
  `eval` anywhere in the text (also inside strings and keys), member access by string
  (`db.c["deleteMany"]()`), and any statement that does not start with `db` (`x = db.c.find()`).

So `db.orders.find({status: "paid"}).sort({id: -1}).limit(5)` is read-only, and
`db.orders.aggregate([{$match: {}}, {$out: "x"}])`, `db.runCommand({drop: "c"})` and
`db.c.find().forEach(…)` are not.
{{< /tab >}}

{{< tab >}}
Redis: the first word decides. Read-only commands are `GET`, `MGET`, `GETRANGE`, `STRLEN`, `EXISTS`,
`TYPE`, `TTL`, `PTTL`, `EXPIRETIME`, `KEYS`, `SCAN`, `RANDOMKEY`, `DBSIZE`, `HGET`, `HMGET`, `HGETALL`,
`HKEYS`, `HVALS`, `HLEN`, `HEXISTS`, `HSCAN`, `HSTRLEN`, `LRANGE`, `LLEN`, `LINDEX`, `SMEMBERS`, `SCARD`,
`SISMEMBER`, `SMISMEMBER`, `SRANDMEMBER`, `SSCAN`, `SINTER`, `SUNION`, `SDIFF`, `ZRANGE`,
`ZRANGEBYSCORE`, `ZRANGEBYLEX`, `ZREVRANGE`, `ZREVRANGEBYSCORE`, `ZCARD`, `ZCOUNT`, `ZSCORE`, `ZRANK`,
`ZREVRANK`, `ZSCAN`, `XLEN`, `XRANGE`, `XREVRANGE`, `XINFO`, `PING`, `INFO`, `TIME`, `ECHO`, `BITCOUNT`,
`BITPOS`, `GETBIT`, `PFCOUNT`, `GEODIST`, `GEOPOS`, `GEOSEARCH`, `JSON.GET`, `LASTSAVE`.

`CONFIG`, `CLIENT`, `MEMORY` and `OBJECT` are read-only only with the subcommands `GET`, `LIST`, `INFO`,
`USAGE`, `ENCODING`, `FREQ` or `IDLETIME` (`CONFIG GET maxmemory` yes, `CONFIG SET …` no). Everything else,
`SET` and `FLUSHALL` included, is a write.
{{< /tab >}}

{{< tab >}}
Cassandra (CQL) uses an allow-list: only statements starting with `SELECT`, `DESCRIBE`, `DESC` or `LIST`
are read-only. `USE`, all DDL and `BEGIN BATCH` (one statement for the editor, many writes for the server)
ask first. CQL strings have no backslash escapes, and `//` starts a comment.
{{< /tab >}}

{{< tab >}}
DynamoDB (PartiQL): only `SELECT` is read-only, and only when it is a single statement (trailing `;` are
allowed). `INSERT`, `UPDATE`, `DELETE`, `EXISTS` (it only exists inside transactions) and the driver's
`CREATE`/`ALTER`/`DROP`/`TRUNCATE TABLE` are writes.
{{< /tab >}}

{{< /tabs >}}

The reads webtuidb makes on its own to fill the grid, the sidebar and the structure views (row pages, counts,
column lists) do not go through safe mode at all.

## Where safe mode applies

| Action | What is checked |
|---|---|
| Running SQL in the editor (`run_current`, `run_all`) | the whole batch, after query parameters are filled in |
| Committing table edits (`commit`) and edits in an editable result | the statements the preview (`preview`) shows |
| Structure changes: columns, indexes, keys, creating, renaming, cloning, truncating or dropping tables, views, schemas and databases | the planned DDL, after any typed-name confirmation (see [Destructive confirmations](../destructive-confirmations/)) |
| Bulk update, fake rows and the other data tools | the generated statements |
| User and role management | the generated `CREATE`/`ALTER`/`DROP`/`GRANT`/`REVOKE` |
| Killing a process | one `KILL <id>` per process |
| Import | a pseudo-statement `IMPORT <file> INTO <table>`, which is never read-only, so Alert Mode 2 and Safe Mode 2 ask too |
| Restore | the restore command line (for example the `pg_restore …` call) as the statement; with "Restore into a new database" the `CREATE DATABASE` goes through the gate first |
| Backup | web: the dump command line is gated like a restore; terminal: a plain "Backup …?" confirmation, not safe mode |
| AI assistant "run" | a statement that changes data first asks "Run a statement that changes data?" (terminal) / "This statement changes data or structure. Open it in a query tab and run it?" (web), then runs in a query tab through the normal gate |

What safe mode does not cover:

- **Bootstrap statements** of a connection run on connect without a prompt (see
  [Destructive confirmations](../destructive-confirmations/#bootstrap-statements)).
- **Plugins** cannot write: a JS plugin's `query()` accepts one read-only statement, refuses known
  side-effecting functions and runs it inside a read-only transaction that is rolled back (or on a
  `read_only` PostgreSQL/MySQL connection). Without either, the query is refused.
- **MCP clients** do not see safe mode at all. They are limited by their token's scope and, with
  `[mcp] approval_for_unsafe = true`, every statement that is not read-only waits for the approval sheet:
  "MCP wants to run a statement" in the terminal and "MCP client wants to run a statement" in the browser,
  naming the token and the connection. **Deny** has the focus and keys and clicks are ignored for the first
  700 ms, so a keystroke meant for the editor cannot approve a write. The web server gives up after 5
  minutes ("approval timed out after 5 minutes"); a denial reaches the client as "denied by the user". Without
  a UI attached (`webtuidb --mcp`), such statements are refused. See the
  [Automation](../../automation/) page.

## The status indicator

Both UIs always show the level of the active connection:

- Terminal: the right end of the status bar shows "⚠ Alert Mode 2" (any level but silent) in the warning
  colour. Silent shows "safe mode off" in dim text, or in the warning colour when the connection is tagged
  `production`, because then nothing asks before a write on production. A `read_only` connection also shows
  "READ-ONLY" after its name.
- Web: a chip in the top bar after the server version shows the label ("Alert Mode 2", "Silent Mode"); click
  it to change the level. A `read_only` connection gets a red "read only" chip next to it.

## Settings

| Setting | Where | Default | Meaning |
|---|---|---|---|
| `safe_mode` | `[[connection]]` in `connections.toml`, or the connection form's "Safe mode" choice | unset | the connection's level: `silent`, `alert1`, `alert2`, `safe1` or `safe2` |
| `default_safe_mode` | `[general]` in `config.toml` | `"silent"` | the level of every connection that sets no `safe_mode` |

The connection form starts the "Safe mode" choice on `default_safe_mode` and saves the chosen value into the
connection, so a connection edited in the form keeps its level even if you change the default later.

Unknown levels fail closed. A misspelled `safe_mode` makes `connections.toml` refuse to load with
`connection "…": safe_mode: unknown safe mode "…" (want silent|alert1|alert2|safe1|safe2)`, and a bad
`default_safe_mode` stops `config.toml` with `general.default_safe_mode: …`. Should an unknown value reach a
session anyway, it opens in Safe Mode 2, never in Silent Mode.

See the [configuration reference](../../configuration/) for the rest of both files.

## Details and limits

- The prompt is per batch, not per statement. A cancelled prompt sends nothing; there is no partial send.
- The web server repeats the check on every write request and answers 409 until the confirmation or the right
  password arrives, so a script talking to the API meets the same rule as the page.
- The level is checked before the statement reaches the driver. On a `read_only` connection the terminal can
  therefore ask "send to the server?" for a write that the connection then refuses; the browser refuses
  such a write before asking. See [read-only connections](../destructive-confirmations/#read-only-connections).
- The password check compares exactly what was used to connect. When `password_cmd` returns a new value
  later (a rotated secret), the session keeps the one it connected with.
- The classifier is a keyword pre-filter, not a parser (marked as a known shortcut in the code). Treat Alert
  Mode 2 and Safe Mode 2 as protection against slips, not against a crafted statement.
