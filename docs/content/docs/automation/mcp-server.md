---
title: MCP server
description: Let Claude Code, Codex, Cursor, VS Code and other MCP clients list your connections and run queries, over stdio (--mcp) or HTTP with bearer tokens and scopes, with read-only enforcement, an approval sheet for writes, and an activity log, in the terminal and in the browser.
weight: 20
---

webtuidb is also a [Model Context Protocol](https://modelcontextprotocol.io) server. An MCP client (Claude Code,
Codex, Cursor, VS Code, any agent that speaks MCP) can list your saved connections, open one, run read-only
queries, and, with the right token and your approval, run statements that change data. It also reads your saved
queries, query history and metrics boards.

There are two ways to serve it:

| Mode | Start | Who can connect | Approval sheet | Activity log |
| --- | --- | --- | --- | --- |
| **stdio** | the client runs `webtuidb --mcp` itself | only that client | no: with `approval_for_unsafe` on, every non-read-only statement is refused | not shown anywhere |
| **HTTP** | `[mcp] enabled = true`, then run the terminal UI or `webtuidb --frontend` | any client with a bearer token from `[[mcp.token]]` | yes, in the UI that runs the server | **MCP activity** in that UI |

## stdio: `webtuidb --mcp`

The client starts webtuidb as a child process and talks JSON-RPC on its standard input and output. Print the
entry for your client with `-mcp-config`:

```console
$ webtuidb -mcp-config claude-code
# put this in .mcp.json (project) or ~/.claude.json
{"mcpServers":{"webtuidb":{"command":"/usr/local/bin/webtuidb","args":["--mcp"]}}}
```

| `-mcp-config` | Output | Where it goes |
| --- | --- | --- |
| `claude-code` | `{"mcpServers":{"webtuidb":{"command":…,"args":["--mcp"]}}}` | `.mcp.json` (project) or `~/.claude.json` |
| `cursor` | the same JSON | `~/.cursor/mcp.json` |
| `vscode` | `{"servers":{"webtuidb":{"type":"stdio","command":…,"args":["--mcp"]}}}` | `.vscode/mcp.json` |
| `codex` | `[mcp_servers.webtuidb]` with `command = …` and `args = ["--mcp"]` (TOML) | `~/.codex/config.toml` |

The `# put this in …` line goes to standard error, so `webtuidb -mcp-config vscode > .vscode/mcp.json` writes a
valid file (merge it by hand when the file already has servers). `command` is the absolute path of the running
binary. When you also pass `-config <file>`, the entry gets `"--config", "<absolute path>"` after `--mcp`. Any
other client name fails: `unknown MCP client "x" (claude-code | codex | cursor | vscode)`. For Claude Code you can
also run `claude mcp add webtuidb -- webtuidb --mcp`.

How the stdio server behaves:

- **Scope.** It uses the first `[[mcp.token]]` of the config (its scope and connection list, no token value
  needed). With no token configured it has the `full` scope over every connection, since stdio is your own user.
- **Writes.** There is no UI to ask, so with `approval_for_unsafe = true` (the default) `execute_query` refuses
  every statement that is not read-only:
  `needs approval: "UPDATE orders SET status = 'shipped' WHERE id = 1042" is not read-only and approval_for_unsafe is on`.
  Set `approval_for_unsafe = false` to let a `read-write` or `full` scope write over stdio without asking.
- **Input.** A line that is not JSON gets a JSON-RPC `Parse error` (id `null`) and the session goes on. Requests
  are answered as they finish, not in order: wait for `open_connection` to answer before you query it.
- **Data.** Connections, saved queries, history and boards come from the same files as the UIs. The connections are
  read once at start.

## HTTP: `[mcp]` in `config.toml`

```toml
[mcp]
enabled = true
# port = 51003                   # listens on 127.0.0.1:<port>
# listen = "0.0.0.0:51003"        # another address; use TLS then
# tls_cert = "/path/cert.pem"     # both or neither
# tls_key  = "/path/key.pem"
# approval_for_unsafe = true      # ask in the UI before a non-read-only statement

[[mcp.token]]
name = "claude-code"              # shown in the log and on the approval sheet
value = "a-long-random-string"
scope = "read-write"              # read-only | read-write | full
connections = ["Shop (Postgres)"] # empty or missing = all connections

[[mcp.token]]
name = "reporting"
value = "another-long-random-string"
scope = "read-only"
```

The terminal UI starts the listener when it starts; `webtuidb --frontend` starts it too. It speaks MCP
"streamable HTTP" on any path of the address, so the client URL is `http://127.0.0.1:51003/`. Configure the
client with the header `Authorization: Bearer <value>`, for example
`claude mcp add --transport http webtuidb http://127.0.0.1:51003/ --header "Authorization: Bearer a-long-random-string"`.

| Rule | Detail |
| --- | --- |
| Authentication | every request needs `Authorization: Bearer <value>` matching a token; otherwise `401 invalid token`. With no `[[mcp.token]]` no client can connect |
| Sessions | an idle session closes after 30 minutes; one token can hold at most 20 sessions at once |
| Address | `listen` when set, else `127.0.0.1:<port>` (`port` defaults to 51003) |
| TLS | `tls_cert` and `tls_key` must be set together (`mcp.tls_cert and mcp.tls_key must be set together` at start); then the listener is HTTPS |
| Plain HTTP off loopback | the web server prints `webtuidb: WARNING: MCP is served as plain HTTP on …; bearer tokens cross the network unencrypted (set [mcp] tls_cert and tls_key)`; the terminal does not warn |
| One listener per port | when the port is taken (say the terminal UI already serves it), the terminal's **MCP activity** shows `✗ could not listen: listen tcp 127.0.0.1:51003: bind: address already in use`; the web server prints `webtuidb: mcp: …` and serves the UI without MCP |
| Errors after the bind | a certificate that does not load stops the listener just after it binds: the terminal shows `MCP server stopped: …` in the status line and in **MCP activity**; the web server prints `webtuidb: mcp: …` |
| Connections | the server sees the connections saved when it started: restart to pick up new ones |

A config with a token value is chmodded to 0600, like one with a password. Token checks: a duplicate name
(`mcp.token "x": duplicate name`) and a scope outside the three
(`mcp.token "x": scope "admin" is not one of read-only|read-write|full`) stop the app at start.

## Tools

| Tool | Parameters | Returns | Scope rules |
| --- | --- | --- | --- |
| `list_connections` | — | `[{"name","driver"}]` of the connections the token may use | all |
| `list_open_connections` | — | the names of the connections this server has open | all |
| `open_connection` | `connection`: saved connection name | `"opened PostgreSQL 16.15"`, or `"already open"` | the token's `connections` |
| `disconnect` | `connection` | `"closed"`; waits for tools still running on it | not `read-only` |
| `execute_read_query` | `connection`: an open connection; `sql`: one or more statements | the last statement's result | all, with the read-only checks below |
| `execute_query` | `connection`, `sql` | the last statement's result | `read-write` (no DDL) or `full`; approval below |
| `list_saved_queries` | — | `[{"path","name","keywords"}]` | all |
| `get_saved_query` | `name`: a `path` from `list_saved_queries` (`reports/monthly-revenue.sql`) | the SQL text | all |
| `list_history` | `connection`; `limit` (default 50) | the newest history entries, string literals hidden | not `read-only` (history holds literals and row pre-images) |
| `list_boards` | — | `[{"name","connection","panels"}]`, without boards bound to a connection the token may not use | all |
| `run_board` | `name`; `connection` (default: the board's own); `vars`: `{"status":"paid"}` | one entry per panel: `title`, `kind`, and `number`, `columns`/`rows` or `error`. Button and import panels are skipped | all; runs read-only like `execute_read_query` |

A query result is JSON: `{"columns":["status","n"],"rows":[["delivered",450],…],"affected":6,"message":"","truncated":false}`.
Rows stop at `[general] row_limit` (`truncated` is then true) and each call stops after `[general] query_timeout_secs`.
Open connections belong to the server process, not to a client: one client's `open_connection` is visible to the others.

### Scopes and the checks on every statement

`sql` is split into statements with the connection's dialect, and every statement is checked before any runs:

| Check | Applies to | Refusal |
| --- | --- | --- |
| The token may use this connection | every tool with a connection | `token "claude-code" may not open "Shop (MySQL)"` / `may not use` |
| The connection is open | the query and board tools | `"Shop (Postgres)" is not open; call open_connection first` |
| `read-only` scope | `execute_query`, `disconnect`, `list_history` | `token "reporting" is read-only` (history: `…; history is not available to it`) |
| `read-write` scope: no DDL | `execute_query` | `token "claude-code" is read-write: DROP is not allowed`. Refused leading keywords: `DROP`, `ALTER`, `CREATE`, `TRUNCATE`, `GRANT`, `REVOKE`, `RENAME`, `DO`, `CALL`, `EXECUTE`, `EXEC`, `PREPARE`, and a statement whose first word cannot be read (`an unclassifiable statement is not allowed`). `EXPLAIN ANALYZE …` is judged by the statement it explains |
| Read-only statement | `execute_read_query` | `not a read-only statement; use execute_query` |
| Side-effect functions | `execute_read_query`, `run_board` | `nextval is not allowed in a read-only query; use execute_query` (below) |
| Approval | `execute_query`, each statement that is not read-only, when `approval_for_unsafe` is on | `denied by the user: "UPDATE orders SET …"` |

The first word is found after spaces and comments (`--`, `/* */`, and `#` on MySQL, MariaDB and ClickHouse). A
MySQL executable comment `/*!50000 DROP TABLE x */` is read as the statement inside it.

### How `execute_read_query` stays read-only

Three layers, all of which must pass:

1. **Keyword check**: every statement must be read-only for the dialect (the same check the editor uses).
2. **Side-effect check**: functions that change state or reach outside the database are refused even inside a
   `SELECT`. The check runs on a copy of the statement with comments and string literals removed and quoted
   identifiers unquoted, so `SELECT 'copy(this)'` passes while `SELECT "dblink_exec"(…)` does not. Refused:
   - every dialect: `dblink*` (`dblink_exec`, `dblink_connect` …), `lo_*`, `pg_terminate_backend`, `pg_cancel_backend`,
     `pg_read_file`, `pg_read_binary_file`, `pg_ls_dir`, `pg_switch_wal`, `pg_create_restore_point`, `pg_reload_conf`,
     `pg_rotate_logfile`, `pg_advisory_*`, `query_to_xml*`, `cursor_to_xml*`, `setval`, `nextval`, `copy(…)`, a
     statement starting with `COPY`, and a Postgres `U&"…"` identifier (`a unicode-escaped identifier`);
   - MySQL and MariaDB: `LOAD_FILE`, `GET_LOCK`, `RELEASE_LOCK`, `RELEASE_ALL_LOCKS`, `SLEEP`, `BENCHMARK`,
     `SYS_EXEC`, `SYS_EVAL`, `INTO OUTFILE`, `INTO DUMPFILE`, `LOAD DATA`;
   - ClickHouse table functions: `file`, `url`, `urlCluster`, `s3`, `s3Cluster`, `remote`, `remoteSecure`, `hdfs`,
     `hdfsCluster`, `executable`, `azureBlobStorage`, `gcs`, `mysql`, `postgresql`, `mongodb`, `odbc`, `jdbc`,
     `sqlite`, `input`, `cluster`, `clusterAllReplicas`, `deltaLake`, `iceberg`, `hudi`.
3. **Read-only transaction**: the statements run on one pinned connection inside `BEGIN READ ONLY` (PostgreSQL) or
   `START TRANSACTION READ ONLY` (MySQL, MariaDB), rolled back afterwards. A connection saved with `read_only = true`
   on PostgreSQL, MySQL or MariaDB is already read-only on the server and runs without the extra transaction.

{{< callout type="info" >}}
Engines without a server-side read-only transaction cannot pass layer 3, so `execute_read_query` and
`run_board` refuse them: `read-only queries cannot be enforced read-only on this redis connection (no server-side read-only mode or transaction)`.
That covers Redis, MongoDB, ClickHouse, Cassandra, DynamoDB and Vertica. Use `execute_query` with a
`read-write` or `full` token there (non-read-only commands then go through the approval sheet).
{{< /callout >}}

`execute_query` runs on the connection directly, without the read-only transaction. It does not use the
connection's [safe mode](../../safety/safe-mode/); the token scope and the approval sheet are its guards.

## The approval sheet

With `approval_for_unsafe = true` (the default), an HTTP client's `execute_query` stops at each statement that is
not read-only and waits for you in the UI that runs the server, before any statement of the call runs.

{{< ui tui="automation-mcp-approval" web="automation-mcp-approval" >}}A client asks to run an UPDATE. Terminal: token `agent` on "Shop (MySQL)"; browser: token `claude-code` on "Shop (Postgres)". Both were denied.{{< /ui >}}

| | Terminal | Web |
| --- | --- | --- |
| Title | **⚠ MCP wants to run a statement** | **MCP client wants to run a statement** |
| Shows | `Token: <name>`, `Connection: <name>`, the statement | `Token <name> on connection <name>`, the statement |
| Approve | {{< key "y" >}} (**Approve**) | **Approve** |
| Deny | {{< key "Enter" >}}, {{< key "Esc" >}} or {{< key "n" >}} (**Cancel**) | **Deny** (has the focus), **✕** or {{< key "Esc" >}} |
| Stray keys | keys in the first 700 ms are ignored | clicks and keys on **Approve** in the first 700 ms are ignored |
| Waits | until you answer or the client gives up | up to 5 minutes, then `approval timed out after 5 minutes` |
| Several waiting | one sheet at a time | one sheet at a time, oldest first; every open browser tab of the server shows it and any of them can answer |

Denying returns `denied by the user: "<first 60 characters>"` to the client and runs nothing of that call. A
statement you approve runs as sent. In a multi-statement call each unsafe statement gets its own sheet.

## MCP activity and clients

{{< ui tui="automation-mcp-activity" web="automation-mcp-activity" >}}The activity of two tokens: reads, refusals by scope and by the side-effect check, and (in the browser) the denied UPDATE at the top.{{< /ui >}}

| | Terminal | Web |
| --- | --- | --- |
| Open | {{< key "C-d p" >}} → **MCP activity** (`mcp_log` has no key) | **Open anything** → **MCP activity**, or the connection switcher menu (the connection name in the top bar) → **MCP activity** |
| Server off | `the MCP server is off: set enabled = true under [mcp]` | `The MCP server is off. Set enabled = true in the [mcp] section of the config and restart.` |
| Header | `listening on 127.0.0.1:51003 over http` (or `https (TLS)`), then the tool list | `Listening on 127.0.0.1:51003 over plain HTTP` (or `HTTPS (TLS)`), **Export log as CSV** |
| Entries | newest first: time, token, tool, connection, SQL, `✗ error` | a table **Time / Token / Tool / Connection / SQL / error**, newest first, refreshed every 3 s |
| Export | {{< key "e" >}}: form **Export the MCP log as CSV** with **File** (`mcp-log.csv`), then `saved 20 requests to mcp-log.csv` | **Export log as CSV** downloads `mcp-log.csv` |
| Clients | {{< key "c" >}}: **MCP clients**, every token in the log with its request count, last time and connection | **Connected clients**: each token that called a tool in the last 10 minutes, with **Requests** and **Last seen** |

The log keeps the last 200 tool calls in memory, for the running process only; it is not written to disk. String
literals in the SQL and in errors are shown as `'…'` (so a password in `ALTER ROLE … PASSWORD '…'` is not
logged); backslash-escaped and `$tag$` strings are not recognised. The CSV has the columns
`time,token,tool,connection,sql,error`, with the time in RFC 3339. A stdio client has no log in any UI, and its
token column is empty when no token is configured.

You cannot disconnect a client from either UI: to cut one off, remove its `[[mcp.token]]` (or change its `value`)
and restart.

## Settings

| Key | Default | Meaning |
| --- | --- | --- |
| `[mcp] enabled` | `false` | serve MCP over HTTP from the terminal UI and `--frontend` |
| `port` | `51003` | the port on 127.0.0.1 when `listen` is empty |
| `listen` | — | `host:port` to listen on instead |
| `tls_cert`, `tls_key` | — | PEM files; set both for HTTPS |
| `approval_for_unsafe` | `true` | ask before each non-read-only statement (HTTP), refuse it (stdio) |
| `[[mcp.token]] name` | — | the label in the log and on the sheet; must be unique |
| `value` | — | the bearer secret |
| `scope` | — | `read-only`, `read-write` or `full` (required) |
| `connections` | all | the saved connection names the token may use |

`[general] row_limit` and `query_timeout_secs` also apply to every MCP query. See the
[settings reference](../../configuration/settings-reference/).
