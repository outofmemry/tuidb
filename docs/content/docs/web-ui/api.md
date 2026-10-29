---
title: HTTP API
description: "The JSON and event-stream API behind the web UI, for scripts that talk to a running webtuidb --frontend server: authentication, sessions, every endpoint, errors and a curl example."
weight: 50
---

`webtuidb --frontend` serves a JSON API under `/api/` next to the web app. The browser app is its only first-party client, and everything the app does goes through it, including safe mode.

{{< callout type="warning" >}}
**This API is internal and may change.** It exists to serve the web app that ships in the same binary, and the two are versioned together. There is no stability promise: endpoints, field names and status codes can change in any release. If you script against it, pin the webtuidb version, and check `GET /api/meta` (`version`) first. For a stable integration use the MCP server (`webtuidb --mcp`), which is designed for it.
{{< /callout >}}

## Conventions

| | |
|---|---|
| Base | `http://127.0.0.1:7788/api` (or your `--listen` address; `https://` with TLS). |
| Bodies | JSON, sent with `Content-Type: application/json`. Any other type on a request that has a body is `415` (`Content-Type must be application/json`). A JSON body is limited to 32 MB; uploads go through multipart instead (limit 512 MB). |
| Field names | Request bodies use the Go field names (`Name`, `Ref`, `Filters`, `Limit`). Go's decoder ignores case, so `name` and `Name` both work. Responses are a mix: session info and results use camelCase (`resultId`, `durationMs`); database objects keep the Go names (`Schema`, `Name`, `Kind`). Cells are strings; SQL `NULL` is JSON `null`. |
| Success | `200` with the JSON result, or `{"ok": true}` when there is nothing to return. |
| Caching | Every response has `Cache-Control: no-store`. |
| Unknown paths | `404 {"error": "no such endpoint"}`. |

### Authentication

Send the access token on every request except `POST /api/login`:

```
Authorization: Bearer <token>
```

The token is the one printed at start (`--token`, `WEBTUIDB_TOKEN`). With `[web] password` set you can instead `POST /api/login` with `{"Password": "…"}`; the answer sets an `HttpOnly` cookie, `webtuidb_session`, that works in place of the header for 12 hours.

| Status | Meaning |
|---|---|
| `401` | `{"error": "unauthorized", "login": false}`: no or wrong token. `login` is true when a password sign-in exists. |
| `403` | `origin not allowed`: the `Origin` header did not match (see [Behind a reverse proxy](../serving/#behind-a-reverse-proxy)), or the request is `Sec-Fetch-Site: cross-site`. A script that sends no `Origin` header is not affected. |

### Errors

An error is a JSON object with a message, and sometimes extra fields the client acts on:

```json
{"error": "no such session (it was closed or the server restarted)"}
```

| Status | Used for | Extra fields |
|---|---|---|
| `400` | A bad request, or the database refused it. The text is the reason. | `partial` on `/apply`: true when part of a change may have run (no transactions, or DDL). |
| `403` | The connection is read only and the statement writes. | |
| `404` | No such session, job or endpoint. | |
| `409` | A safe mode gate; a name that exists; a stale version. | `gate` `{confirm, password}`, `level`, `statements` for safe mode; `exists` for a saved name; `stamp` for the connection list. |
| `415` | Wrong `Content-Type`. | |
| `422` | More input needed. | `passwordRequired` (the connection asks for its password); `params` (the statements have query parameters: send `Vars`). |
| `500` | A server fault; the text names the crash log. | |
| `507` | The session's temporary disk budget (1 GB for import spools and kept exports) is used up. | |

**Safe mode applies to the API.** A statement that the connection's safe mode level gates is refused with `409` and `{"gate": {"confirm": true, "password": false}, "level": "Alert Mode 2", "statements": [...]}`. Repeat the same request with `"Confirmed": true` (alert levels) or `"Password": "…"` (safe levels). A read-only connection refuses every write with `403`. The browser cannot skip the gate and neither can a script.

## `GET /api/meta`

The first call the app makes. It returns what the pages need to draw themselves:

| Key | Holds |
|---|---|
| `version` | The webtuidb version. |
| `drivers`, `sslModes`, `safeModes`, `fileBased` | Engines, SSL modes, safe mode names (`silent`, `alert1`, `alert2`, `safe1`, `safe2`), engines whose connection is a file. |
| `operators` | Filter operators per dialect (`postgres`, `mysql`, `clickhouse`, `redis`, `mongo`, `sqlite`, `duckdb`, `cql`, `partiql`). |
| `keymap` | `{prefix, bindings: [{Action, Key, Context, NoPrefix, Desc}]}`: the resolved keymap, your overrides and plugin shortcuts included. |
| `menus` | The context menus (`welcome`, `tab`, `grid`, `sidebar_object` …), shared with the terminal. |
| `config` | The `general`, `editor`, `table`, `sidebar` and `csv` sections, each key in Go and in camelCase spelling. |
| `web` | `fontSize`, `lineHeight`, `notifications`, `autoHideScrollbars`, `toolbar`. |
| `theme`, `themes` | The resolved palette (`{Name, Colors}`) and installed theme names. |
| `security` | `{passcode: bool, lockAfterMins}`; never the hash. |
| `login` | Whether a sign-in password is set. |
| `rowLimit`, `query_timeout_secs` | `[general] row_limit` and `query_timeout_secs`. |
| `tags`, `export`, `queriesDir`, `mcp`, `ai`, `passwordCmdPresets`, `pluginConflicts`, `csvWarning` | Tag colours, the export file-name pattern, the saved-queries folder, the MCP listener and the assistant provider, the password-command presets, plugin shortcuts that were skipped for a clash, a CSV settings warning. |

## Sessions

Nearly everything works on a **session**: `POST /api/sessions` opens one, and the returned `id` goes in the path of the rest (`/api/sessions/{id}/…`). Sessions are server-side connection pools; they close on `DELETE`, after an hour with no requests, or when the server stops. There are at most 32.

```sh
curl -s -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"Name": "Shop (Postgres)"}' $BASE/api/sessions
```

```json
{"id": "78b27992b82e537e", "name": "Shop (Postgres)", "driver": "postgres", "serverInfo": "PostgreSQL 16.15",
 "database": "shop", "schema": "public", "level": "silent", "levelLabel": "Silent Mode", "rowLimit": 300,
 "color": "#7aa2f7", "tag": "development", "readOnly": false, "host": "127.0.0.1", "port": 55432,
 "caps": {"Schemas": true, "Transactions": true, "Dialect": "postgres", "Editor": "sql", "...": "..."}}
```

`Name` is a saved connection; `Password` is needed for a connection whose password is `ask` (otherwise `422 passwordRequired`). `caps` says what the engine supports (schemas, indexes, transactions, process list, users, document model …) and is what the app uses to hide features.

### Reading data

```sh
curl -s -H "Authorization: Bearer $TOKEN" "$BASE/api/sessions/$SID/objects?schema=public"
# [{"Schema":"public","Name":"audit_log","Kind":"table","Detail":""}, {"Schema":"public","Name":"customers","Kind":"table","Detail":"24 kB"}, …]

curl -s -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"Ref": {"Schema": "public", "Name": "orders", "Kind": "table"}, "Limit": 2}' \
  $BASE/api/sessions/$SID/rows
```

```json
{"resultId": "d153b10853e5", "columns": [{"name": "id", "type": "int4", "pk": true}, {"name": "customer_id", "type": "int4"}],
 "cells": [["1", "8", "paid"], ["2", "15", "shipped"]], "affected": 2, "hasRows": true, "durationMs": 1, "truncated": false, "editable": true}
```

`Limit` is clamped to `[general] row_limit` (a missing or zero limit means the row limit, never "everything"); `truncated` is true when that clamp cut the result. `editable` and `resultId` appear for plain tables with a primary key on a connection that is not read only; the id refers to a copy of the rows kept on the server (at most 50 per session, oldest evicted) that edits are planned against.

## All endpoints

Methods and paths exactly as the server registers them. `{id}` is a session id.

### Global

| Endpoint | What it does |
|---|---|
| `POST /api/login` | Trade the `[web] password` for the session cookie. `404` when no password is set; five wrong tries in ten minutes lock sign-in for 60 s. |
| `GET /api/meta` | The configuration and keymap the app starts from (above). |
| `GET /api/connections` | The saved connections, each with `lastConnected`. The `ETag` header is the version of the connection files. |
| `PUT /api/connections` | Replace the whole list, as the terminal's editor saves it. `If-Match` with the `ETag` makes a stale write fail with `409`. `password_cmd` and `bootstrap` can only be changed by editing the file. |
| `POST /api/connections/test` | Open a connection from a body (not saved) and ping it with a 10 s limit; returns `serverInfo`. |
| `POST /api/connections/parse` | Turn a connection URL (`{"URL": …}`) into a connection object. |
| `POST /api/connections/format` | The reverse: a connection object into a URL (including the password). |
| `GET /api/connections/export`, `POST /api/connections/export` | Download `connections.wtdb`; optional `Passphrase`, `Passwords` (needs a passphrase), `Group`. The `POST` form keeps the passphrase out of access logs. |
| `POST /api/connections/import` | Add the connections of an exported file (`Content` text or `ContentBase64`, `Passphrase`). Existing names are skipped; commands and keys in the file are dropped. Answers `added`, `skipped`, `dropped`. |
| `POST /api/sessions` | Open a session. |
| `POST /api/passcode/check` | Check the lock-screen passcode (`{"Passcode": …}`) → `{ok}`; after three misses from one address every try waits a second. |
| `POST /api/reveal` | Open a file's folder in the server's file manager; only for a loopback request (else `403`). |

### Settings, plugins, MCP

| Endpoint | What it does |
|---|---|
| `GET /api/settings` | The resolved config as TOML with every secret blanked; read only. |
| `GET /api/settings/schema` | Every editable setting with its current value, type, choices and documentation, plus the config path. |
| `PUT /api/settings` | Save `{"Values": {"section.key": "text"}}` to `config.toml` after validation; comments in the file are not kept (a `warning` says so). Settings that name files (`web.tls_cert`, `theme.file`, …) are refused. |
| `PUT /api/settings/keys` | Save `{"Keys": {"commit": ["x"]}}` to `[keys]`; a conflict is `409` with the message. |
| `GET /api/plugins` | The installed plugins. |
| `POST /api/sessions/{id}/plugins/run` | Run a plugin by `Name` with its context; returns its text and effects. The command comes from the plugin file, never from the request. |
| `GET /api/mcp/log` | The MCP activity log (`?format=csv` downloads it). |
| `GET /api/mcp/clients` | MCP clients seen in the last ten minutes. |
| `GET /api/mcp/approvals` | Statements an MCP client is waiting to run. |
| `POST /api/mcp/approvals/{id}` | Answer one: `{"Approve": true}`. |
| `GET /api/assistant/models` | The models the configured AI provider lists. |

### Session

| Endpoint | What it does |
|---|---|
| `GET /api/sessions/{id}` | The session info above. |
| `DELETE /api/sessions/{id}` | Close the session and its pins and jobs. |
| `POST /api/sessions/{id}/safe-mode` | Set the level (`Level`: `silent`, `alert1`, `alert2`, `safe1`, `safe2`); also saves it in `connections.toml`. Leaving a safe level needs `Password` (or `Confirmed` when no password is stored). |
| `POST /api/sessions/{id}/color` | Set the connection's colour (`Color`, `#rgb` or `#rrggbb`, empty for none); saved to `connections.toml`. |
| `POST /api/sessions/{id}/ping` | Check the connection is alive (10 s limit). |
| `GET /api/sessions/{id}/databases` | The databases and the current one. |
| `POST /api/sessions/{id}/database` | Switch database (`Name`); clears the session's caches. |
| `GET /api/sessions/{id}/schemas` | The schemas and the current one. |
| `POST /api/sessions/{id}/schema` | Switch the current schema (`Name`). |
| `GET /api/sessions/{id}/objects` | The objects of a schema (`?schema=`): tables, views, functions, sequences and so on. |
| `GET /api/sessions/{id}/object/{what}` | Metadata of one object (`?schema=&name=&kind=`); `{what}` is `columns`, `indexes`, `triggers`, `fks`, `ddl` or `script` (with `?script=`). |
| `GET /api/sessions/{id}/datatypes` | The column types the engine offers. |
| `GET /api/sessions/{id}/diagram` | The ER diagram data of a schema (`?schema=`): tables, columns, foreign keys and cluster numbers. |
| `GET /api/sessions/{id}/processes`, `POST …/processes/kill` | The server's process list; kill one (`ID`), gated like a write. |
| `GET /api/sessions/{id}/users`, `POST …/users/plan` | Database users and their grants; plan a change (the statements go through `/apply`). |

### Rows, queries and edits

| Endpoint | What it does |
|---|---|
| `POST /api/sessions/{id}/rows` | One page of a table: `Ref`, `Filters`, `Sort`, `Columns`, `Limit`, `Offset`. |
| `POST /api/sessions/{id}/count` | Row count of a table with the same filters; `Estimate: true` allows the statistics estimate. Answers `{n, exact}`. |
| `POST /api/sessions/{id}/filters/sql` | The `WHERE` clause a filter set becomes. |
| `POST /api/sessions/{id}/exec` | Run SQL (or the engine's language): `Text`, `Current` + `Offset` for one statement, `Limit` (`0` = the row limit, negative = none), `Vars` for query parameters, `Pin`/`Tab` for a pinned connection, `Syntax` for Mongo, `Confirmed`/`Password` for the gate. Returns one result per statement; more than 100 statements return one summary. |
| `POST /api/sessions/{id}/exec/stream` | The same as an event stream (below). |
| `POST /api/sessions/{id}/exec/page` | Rows `Offset`… of one read-only `SELECT` without re-running it (`supported: false` when the statement cannot be paged). |
| `GET /api/sessions/{id}/notices/{rid}` | The server notices (PostgreSQL `RAISE NOTICE`) of a result, by its `noticeId`. |
| `POST /api/sessions/{id}/explain` | `EXPLAIN` one statement as a tree of nodes. |
| `DELETE /api/sessions/{id}/results/{rid}` | Forget a cached result. |
| `POST /api/sessions/{id}/pin/{tab}`, `DELETE …/pin/{tab}` | Pin a query tab to its own connection (so `BEGIN` and temp tables carry over) or release it. At most eight pins; an idle pin is dropped after ten minutes and a transaction in it rolls back. |
| `POST /api/sessions/{id}/plan` | Plan edits to a cached result (`ResultID`, `Updates`, `Deletes`, `Inserts`); stores the SQL and returns a `planId`, the statements and the `gate`. |
| `POST /api/sessions/{id}/plan/admin` | Plan an administrative operation (drop, truncate, rename, create database and so on) for one or several objects. |
| `POST /api/sessions/{id}/plan/table` | Plan one structure edit of a table (add a foreign key, move a column). |
| `POST /api/sessions/{id}/structure/plan` | Plan a whole table definition change (`Old` null creates). |
| `POST /api/sessions/{id}/apply` | Run a stored plan (`PlanID`, plus `Confirmed` or `Password` for the gate). Only SQL the server planned can run here. |
| `POST /api/sessions/{id}/tools/schema-diff`, `…/data-diff`, `…/bulk-update`, `…/fake-rows` | Plan a schema diff, a data diff, a bulk `UPDATE` or generated rows; each returns a plan for `/apply`. |

### Import, export, backup, search and jobs

| Endpoint | What it does |
|---|---|
| `POST /api/sessions/{id}/export` | Download a table (`Mode: "table"`), the loaded page (`"page"`) or a read-only query (`"query"`) as `csv`, `json`, `sql` or `xlsx`. The file is streamed as an attachment. `Job: true` on a table export runs it in the background instead. |
| `POST /api/sessions/{id}/import/preview` | Parse the first rows of an upload and propose a column mapping. |
| `POST /api/sessions/{id}/import` | Start an import as a job. JSON with `Content`, or `multipart/form-data` with an `options` part and then the `file` part. |
| `POST /api/sessions/{id}/search` | Start a search across tables as a job; `…/search/{job}/pause` and `…/search/{job}/resume` hold it between tables. |
| `GET /api/sessions/{id}/backup/options` | The flags the backup (`?restore=1` for restore) dialog may offer for this engine. |
| `POST /api/sessions/{id}/backup`, `POST …/restore` | Run the engine's dump or restore tool as a job (`Run: false` only shows the command line). Files live in the data folder's `backups/`. |
| `GET /api/sessions/{id}/jobs` | All jobs of the session, newest first. |
| `GET /api/sessions/{id}/jobs/{jid}` | One job's state: progress, hits, output lines. |
| `GET /api/sessions/{id}/jobs/{jid}/events` | The same as an event stream (below). |
| `POST /api/sessions/{id}/jobs/{jid}/cancel` | Stop a running job; it stays listed. |
| `DELETE /api/sessions/{id}/jobs/{jid}` | Dismiss a job (and delete its kept file). |
| `GET /api/sessions/{id}/jobs/{jid}/file` | Download a finished export's file once. |

### Boards, charts and the assistant

| Endpoint | What it does |
|---|---|
| `GET /api/boards`, `POST /api/boards` | List the boards; add a chart to a board (creating it, never overwriting). |
| `PUT /api/boards/{name}`, `DELETE /api/boards/{name}` | Save a whole board (a new name renames it); delete it. |
| `POST /api/boards/duplicate` | Copy a board under a new name. |
| `POST /api/sessions/{id}/boards/run` | Run a board's read-only panels. |
| `POST /api/sessions/{id}/boards/panel` | Run one panel; names the panels that refresh with it. |
| `GET /api/sessions/{id}/boards/{name}/options` | A dropdown panel's options (`?panel=`). |
| `POST /api/sessions/{id}/boards/{name}/export` | Run an exporter panel's query as an export job. |
| `POST /api/chart` | Draw a chart from cells the client already holds. |
| `GET /api/sessions/{id}/chats`, `POST …/chats` | List (`?q=` searches) and start assistant conversations. |
| `GET`, `PUT`, `DELETE /api/sessions/{id}/chats/{cid}` | Read, save or delete one. |
| `POST /api/sessions/{id}/assistant` | Send the conversation (`Messages`, optional `Model`, `Provider`) to the configured AI provider. |

### Saved queries, history and shared state

| Endpoint | What it does |
|---|---|
| `GET /api/queries` | The saved queries, each with its `etag`. |
| `GET /api/queries/{path…}` | One saved query; the `ETag` header is its version. |
| `POST /api/queries` | Save a query (`Folder`, `Name`, `SQL`, `Keywords`); an existing name is `409 {exists: true}` unless `Overwrite`. |
| `PUT /api/queries/{path…}` | Replace a query's SQL. With `If-Match: <etag>` a query that changed since you read it is `409`; the answer carries the new `ETag`. |
| `DELETE /api/queries/{path…}` | Delete a saved query file (only a `.sql` file). |
| `POST /api/queries/rename` | Move or rename (`From`, `To`); an existing target is `409`; shared queries are read only. |
| `GET /api/queries/export`, `POST /api/queries/import-file` | Download all queries; import a `.sql` file as a saved query. |
| `GET /api/history`, `DELETE /api/history` | A connection's history (`?conn=&q=&limit=`); delete one entry (`?id=`) or all (`?all=1`). |
| `GET /api/history/export`, `POST /api/history/import` | Download as CSV or JSON; import. |
| `GET /api/state/table`, `PUT /api/state/table` | A table's saved filters, sort, hidden columns and widths (`?conn=&db=&table=`). |
| `GET /api/state/shortcuts`, `POST /api/state/pin`, `POST /api/state/touch` | Pinned and recent items of a connection; toggle a pin; record a use. |
| `GET`, `PUT /api/state/groups` | Sidebar groups of a connection. |
| `GET`, `PUT /api/state/tabs` | The editor tabs saved for `auto_save_tabs`. |
| `GET`, `PUT /api/state/folds` | Folded editor statements. |
| `GET`, `PUT /api/state/recent-files`, `POST /api/files/open` | The shared recent `.sql` files; read one that is on the list (a `.sql` file up to 8 MB). |
| `GET`, `PUT /api/state/connection-sort` | The start page's sort order. |
| `GET`, `PUT /api/state/backup-options` | The backup dialog's last options. |
| `GET /api/console`, `DELETE /api/console` | The server's console log (`?conn=`); clear it. |
| `POST /api/split`, `/api/format`, `/api/params` | Split SQL into statements (UTF-16 offsets for CodeMirror); beautify or uglify it; list its query parameters. |

## Event streams

Two endpoints answer with `text/event-stream` (Server-Sent Events). Both are plain HTTP responses, but `EventSource` cannot send an `Authorization` header, so a script reads the body itself (`curl -N`, or `fetch` and a reader). A failure before the stream starts is the usual JSON error.

**`POST /api/sessions/{id}/exec/stream`** takes the same body as `/exec`, runs the same gate, and sends, per statement `i`:

| Event | Data |
|---|---|
| `columns` | `{i, stmt, label, columns}` |
| `rows` | `{i, cells}`: batches of up to 100 rows. |
| `done` | `{i, stmt, label, error, affected, message, noticeId, durationMs, truncated, hasRows, docs, editable, table, resultId}` |
| `end` | `{pinned, inTx, autoPinned}`: once, after the last statement (also after a cancel). |

The rows of a statement are fetched whole and then sent in batches, so the stream shows structure early but is not a database cursor.

**`GET /api/sessions/{id}/jobs/{jid}/events`** follows a job: `progress` (counters and hits, when they change), `line` (a new line of backup or restore output) and a final `done` (`{error, download}`). The server samples the job every 150 ms. `GET …/jobs/{jid}` gives the same state when you would rather poll.

## Cancelling

Closing the connection cancels a running statement: abort the HTTP request. The app's **Cancel** does exactly that, then opens the process list on PostgreSQL and MySQL to show what is still running on the server.

## Example: list the tables

```sh
BASE=http://127.0.0.1:7788
TOKEN=...   # the value after #token= in the address webtuidb printed
auth="Authorization: Bearer $TOKEN"

# open a session on a saved connection
SID=$(curl -s -H "$auth" -H 'Content-Type: application/json' \
        -d '{"Name": "Shop (Postgres)"}' "$BASE/api/sessions" | jq -r .id)

# list the tables of the public schema
curl -s -H "$auth" "$BASE/api/sessions/$SID/objects?schema=public" \
  | jq -r '.[] | select(.Kind == "table") | .Name'
# audit_log
# categories
# customers
# ...

# run a query
curl -s -H "$auth" -H 'Content-Type: application/json' \
  -d '{"Text": "select status, count(*) from orders group by status"}' \
  "$BASE/api/sessions/$SID/exec" | jq '.results[0].cells'

# close the session
curl -s -X DELETE -H "$auth" "$BASE/api/sessions/$SID"
```

Without `jq`, read the JSON by eye; the field names are the ones above. `Content-Type: application/json` matters on every request with a body.

## See also

- [Serving the web UI](../serving/): the token, TLS and the origin check.
- [Development](../development/): running the app against a dev API.
- [Safety](../../safety/): what the safe mode gate asks.
