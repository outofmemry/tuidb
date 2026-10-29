---
title: Architecture
description: The package map, how the terminal UI and the web server sit on one engine, and what happens, step by step, when you commit a change in either front end.
weight: 40
---

```
                       main.go (flags, config, wiring)
                     /                              \
        terminal UI (internal/ui)            web server (internal/web) ── web/src (React)
        Bubble Tea model, key presses        HTTP + JSON API, SSE, embedded app
                     \                              /
            ┌──────────────── shared engine ────────────────┐
            │ internal/db        Session, Capabilities, drivers, planner, splitter │
            │ internal/store     history, saved queries, state, chats, console     │
            │ internal/config    config.toml, connections.toml, settings, themes   │
            │ internal/keymap · menus · safemode                                   │
            │ importer · export · backup · diagram · board · tools · sqlfmt · …    │
            └──────────────────────────────────────────────┘
                                   │
          postgres · mysql · mariadb · clickhouse · cassandra · dynamodb · redis · mongo
          (+ redshift, greenplum, vertica, singlestore as capability profiles)
```

One Go module, `webtuidb`, with a single `main` package at the root. Both front ends call the same engine; neither contains database logic of its own. That is the reason the [parity rules]({{< relref "parity" >}}) are enforceable.

## The root package

- `main.go`: parses flags (`-config`, `-keys`, `--frontend`, `--mcp`, `-bugreport`, …), checks that at most one mode flag is given (`checkModes`), loads the config and connections, builds the keymap, and then starts one of: the Bubble Tea program (`ui.NewApp`), the web server (`web.Run`), the MCP server on stdio (`mcp.New(...).ServeStdio`) or one of the print-and-exit modes. `-version`, `-man`, `-bugreport`, `-check-update` and the theme and passcode commands run before the config is loaded, so they work even when it is broken. `defer crashlog.Recover` turns a panic into a crash log.
- `commands.go`: the helpers behind the small flags: `-bugreport` (with secret masking), `-check-update`, `-changelog`, `-set-passcode`, `-theme-install`, `-theme-update`.

## `internal/db`: the engine contract

The contract between a front end and a database. `Session` is an open connection (catalog calls, `Rows`, `Count`, `Exec`, and the three planners `Plan`, `PlanAdmin`, `PlanTable` plus `Apply`); `Driver` opens sessions; `db.Register` is called from each driver's `init()`, and `main.go` blank-imports the driver packages (`internal/db/all` registers the profile drivers).

- **`Capabilities`.** A struct of booleans and strings that each session reports: `Schemas`, `Indexes`, `Triggers`, `ForeignKeys`, `Views`, `MatViews`, `Functions`, `Sequences`, `ReorderColumns`, `RenameDB`, `Transactions` (is `Apply` atomic), `ProcessList`, `Users`, `Document` (Json and Tree views), `Editor` (`sql`, `redis` or `mongo`), `Dialect`, `DatabaseNoun`, `Types` (column types the structure editor offers) and `Explain`. Front ends hide or refuse what an engine lacks by reading these, never by testing the driver name. The parity tests check that both do.
- **Drivers**, one package per engine under `internal/db/<engine>`: `postgres`, `mysql`, `mariadb`, `clickhouse`, `cassandra`, `dynamodb`, `redis`, `mongo`. Each implements `Session` with its own client library (`pgx`, `go-sql-driver/mysql`, `clickhouse-go`, `gocql`, the AWS SDK, `go-redis`, the MongoDB driver) and its own planners: the SQL engines build parameterised statements from `db.Change` values, mostly with the shared `PlanSQL` and a `Dialect` (ClickHouse, Cassandra and DynamoDB have their own `plan.go`); Redis and MongoDB produce commands and scripts as `Statement.Text`.
- **Profiles**: `redshift`, `greenplum`, `vertica` and `singlestore` are not new drivers. Each registers the PostgreSQL (or MySQL) driver under another name with an `adjust` function that switches capabilities off (for example Redshift: no triggers, materialized views, indexes or process list, and plain-text `EXPLAIN`). `internal/db/iam` builds AWS RDS IAM authentication tokens used as passwords.
- **`Pinner`**: an optional interface (`Pin(ctx)`) for sessions that can lend one dedicated connection. A SQL editor tab pins one so that `BEGIN`, `SET` and temporary tables survive between statements; `TxReporter` reports whether a transaction is open, `TxVerb` classifies a statement as starting or ending one, and `SchemaSetter` moves PostgreSQL's `search_path`.
- **Statement tools.** `Split` cuts an editor buffer into statements for a dialect (dollar quotes, MySQL `DELIMITER`, Redis by line, MongoDB `//` comments); `IsReadOnly` and the classifiers decide whether a batch is a read, which safe mode uses; `scan` is the shared tokenizer, written so that a string or comment can never hide a write (both are fuzzed, see [Testing]({{< relref "testing" >}})). `classify_cql.go` and `classify_partiql.go` are allow-lists for Cassandra and DynamoDB.
- **Everything else shared**: `FilterSet` and `Operators` (the filter bar and its SQL), `ParamFormat` and query parameters, `ParseChart` (`-- barchart x: a, y: b`), `Explain*` (plan trees), `Search` (search in database), `Preimage` (the "original values" CSV), `CheckExpr` (guards the Set Value advanced mode), `FormatValue`, `DataTypesOf`, `SniffImage`, `WKT` (geometry decoding) and `Tunnel` (SSH).

## `internal/store`: what is remembered

Everything lives under `config.DataDir()` (`~/.local/share/webtuidb`):

- `History`: one JSONL file per connection under `history/`, newest 5000 entries, statements cut at 64 KB, trimmed once a file passes 16 MB. Each entry records the database, the SQL, the duration, `Source` (`editor` or `gui`) and, with `[general] log_original_values`, the CSV pre-image of an edit.
- `Queries`: saved queries as `.sql` files in folders, with a header comment for the name and keywords; `[queries] shared_dirs` adds read-only folders.
- `State` (`state.json`): per-table column widths, hidden columns, filters and sort; pins and recent items; the last session's workspaces; row limits; folds; open tabs to restore; backup dialog options; recent files; connection sort; item groups; last-connected times; diagram colours.
- `Chats`: assistant conversations, one file per chat per connection.
- `Console`: the last 1000 statements of a workspace (in memory).
- `reveal.go` opens a file manager on a path.

Writers take `config.LockFile` so the terminal UI and a web process running at the same time do not corrupt each other's files.

## `internal/config`, `keymap`, `menus`, `safemode`

- **`config`**: `config.toml` (`Config` and its section structs, `Defaults()`, `Load`, `validate`), `connections.toml` (`Connection`, `LoadAllConnections`, `SaveConnections`, URL parsing, `[[source]]` folders in `sources.go`), tags and colours, themes (built in, files, installed from git), the schema and writer behind the settings editors (`Schema`, `ApplyValues`, `setting_docs.go`), `Parity` (see the parity page), portable import and export of connections, paths (`ConfigDir`, `DataDir`, the legacy `tuidb` migration), the passcode, the AI settings and `LockFile`.
- **`keymap`**: parsing of tmux-style key names (`C-d`, `M-f`), the `defaults` and `noPrefixDefaults` tables, the prefix state machine (`Machine.Feed`), user overrides with conflict reporting, and `Listing` (`webtuidb -keys`). The web has a TypeScript twin of the machine in `web/src/utils/keys.ts`.
- **`menus`**: the one list of context-menu entries both UIs build from, and the list of dialog pairs. `GET /api/meta` exports them.
- **`safemode`**: the five levels (`silent`, `alert1`, `alert2`, `safe1`, `safe2`) and `Level.Gate(readOnly)`, which says whether a batch needs a confirmation and/or the database password. Both front ends call it; the web server enforces it again on `apply` and `exec`, so the browser cannot skip it.

## `internal/ui`: the terminal front end

A Bubble Tea v2 program. The model is `App` (`app.go`), which owns the welcome screen, a slice of `Workspace` values (one per open connection), an overlay stack and the key machine.

```
key press ──► App.key ──► overlays? ──yes──► the top overlay takes it
                 │ no
                 ▼
          keymap.Machine.Feed(context, key)  ──► action name
                 ▼
          App.run(action) ──► runAppAction / app-level cases
                 ▼
          Workspace.action(name) ──► workspace-wide cases
                 ▼
          the focused tab's Action(name)  or  sidebar.action(name)
```

Where each part lives:

| Area | Files |
|---|---|
| App, frame, layout | `app.go`, `frame.go`, `layout.go` (zen mode, hints, frame), `theme.go`, `mouse.go`, `help.go` (the `?` dialog), `lock.go` (passcode, idle lock), `settings.go` |
| Welcome and connections | `welcome.go`, `welcome_groups.go`, `connect.go` (`DefaultConnect`, `Opened`), `connections*.go`, `connection_details.go` |
| Workspace, panes, tabs | `workspace.go` (the `tabber` interface, gate, commit plumbing), `panes.go` (splits, history), `tabs.go`, `tab_close.go`, `open_anything.go`, `console.go`, `jobs.go`, `background.go` (the pump that feeds async results back into the loop) |
| Sidebar | `sidebar.go`, `sidebar_groups.go`, `sidebar_marks.go`, `schemas.go`, `pins.go` |
| Data tab | `tab_table.go`, `grid*.go`, `filterbar.go`, `quicklook.go`, `celledit.go`, `setvalue.go`, `json_view.go`, `keytree.go`, `doc_views.go` |
| Structure | `struct_view.go`, `structure_actions.go`, `definitions.go` |
| SQL editor | `tab_query.go`, `editor*.go`, `vim.go`, `lexer.go`, `completion.go`, `query_params.go`, `explain.go`, `results.go`, `resultedit.go`, `chart.go` |
| Tools and admin | `import.go`, `export.go`, `backup.go`, `search.go`, `processes.go`, `users.go`, `diagram.go`, `boards.go`, `board_panels.go`, `data_tools.go`, `assistant.go`, `chats.go`, `mcp.go`, `plugins.go`, `files.go` |
| Menus and dialogs | `menus.go` (built from `internal/menus`), `dialog.go` (`Overlay`, `confirmDlg`, `formDlg`, `pickerDlg`) |

A `Workspace` holds the session (`db.Session`), its capabilities, the safe-mode level, the sidebar, the tabs, the console and the jobs. Tabs implement `tabber` (`Input`, `Action`, `Handle`, `View`, `Context`…): `tableTab` and `queryTab` are the main ones. Results of database calls come back as messages addressed to a tab, stamped with the workspace's generation counter so that a result that belongs to the previous database is dropped.

## `internal/web`: the web server

`Run` listens (loopback only unless `--insecure-listen`), prints the URL with the token in the fragment, serves the embedded app and the `/api` routes, and closes every session on exit. It wraps `db.Session`, `store` and `config`; it also imports `internal/ui` for `ui.Opened` and `ui.DefaultConnect`, so a connection is opened exactly the way the terminal opens it.

- `server.go`: `Options`, `Server`, `Handler()` with every route, the `api()` wrapper (origin check, `Sec-Fetch-Site`, bearer token or login cookie, panic recovery into the crash log), JSON decoding limits.
- `auth.go`: `[web] password` login: random cookie values with server-side expiry, five failures in ten minutes lock logins for a minute. `passcode.go` checks the lock passcode.
- `sessions.go`: `session` (one open connection: its result cache, stored plans, jobs, pinned tab connections, a second connection for the process list), the safe-mode `gate` and `check`, the idle reaper (an hour).
- Handlers per feature: `handlers.go` (meta, connections, rows, exec, plan, apply), `changes.go` (edit requests to `db.Change`), `table_edit.go`, `structure.go`, `filters.go`, `query_page.go`, `results.go`, `explain.go`, `search.go`, `export.go`, `import.go`, `backup.go`, `jobs.go`, `processes.go`, `users.go`, `diagram.go`, `boards.go`, `board_panels.go`, `data_tools.go`, `chats.go`, `assistant.go`, `plugins.go`, `mcp.go`, `pins.go`, `folds.go`, `tabs.go`, `settings.go`, `safemode.go`, `connections_*.go`, `queries_io.go`, `history_io.go`, `shared_state.go`, `notices.go`, `console.go`, `reveal.go`.
- Streaming: `stream.go` has server-sent events (`newSSE`) for job progress (`/jobs/{id}/events`) and for `exec/stream` (a query's results as `columns`, row batches, `done` and `end` events), and `sendFile` for downloads that are produced while they are sent.
- `meta.go`, `config_live.go`: `GET /api/meta` (drivers, operators, config sections, theme, keymap, menus, version, security flags). The config is re-read when `config.toml` is saved from the settings dialog.
- `parity.go`: the `SUPPORTED` parser the parity tests use.

## `web/src`: the browser app

React 19, TypeScript, Vite, Tailwind v4, TanStack Query and Router, CodeMirror 6 for the editor. `main.tsx` reads the token from the URL fragment (and then removes it from the address bar), fetches `/api/meta`, and mounts the routes: the connection list (`routes/Connections.tsx`) and `/s/<session>` workspaces (`routes/Workspace.tsx`, which also holds `SUPPORTED`, the action switch and the help dialog), with deep links for tables and saved queries.

| Folder | Contents |
|---|---|
| `components/shell` | frame pieces and shared dialogs: `Layer` (every floating menu), `Palette` (Open Anything), `Dialogs`, `ConsolePane`, `Lock`, `SafeModeDialog`, `TabMenu`, `DetailsDialog`, `RecentDialog`, `Select`, `Logo` |
| `components/sidebar` | `Sidebar.tsx`: the object tree, groups, marks, the `+` menu |
| `components/grid` | `DataTab`, `Grid`, `FilterBar`, `GridFind`, `StructureEditor`, `RowDetail`, `CellEditor`, `SetValueDialog`, `PageDialog`, `SortDialog`, `ItemMenu`, `Tree` |
| `components/query` | `QueryTab`, `Completion`, `Chart`, `ExplainDialog`, `VariablesDialog`, `CopyAsDialog`, `SqlViewer`, `cmtheme` |
| `components/tools` | import, export, multi-export, process list, search, bulk update, column dialogs, schema diff, data diff, fake data |
| `components/admin` | settings, users, backup and restore, jobs drawer, plugins, MCP |
| `components/diagram`, `boards`, `assistant` | ER diagram, metrics boards, the assistant tab |
| `routes` | the two pages |
| `utils` | the typed API client (`api.ts`), the key machine (`keys.ts`), and pure modules such as `pending.ts`, `tablestate.ts`, `tabledef.ts`, `menus.ts`, `links.ts`, `results.ts`, `selection.ts`, `copy.ts`, `config.ts`, `theme.ts`; most of the pure ones have a test in `web/tests` |
| `hooks` | `useKeymap` (the key machine for pages that have no workspace) |

Server state goes through TanStack Query; the browser stores only per-viewer conveniences, and anything shared with the terminal (table state, pins, groups, recent files, open tabs) goes through `/api/state/*`.

## The small packages

| Package | What it does |
|---|---|
| `internal/importer` | CSV, JSON and SQL file import: encoding detection, preview without touching the database, column mapping, upsert modes, and a transaction on a pinned connection where the engine allows it |
| `internal/export` | CSV, JSON, SQL `INSERT` and XLSX writers for results and whole tables, file-name patterns, gzip, the `[csv]` settings |
| `internal/backup` | builds and runs `pg_dump`, `mysqldump` and friends; secrets go through the environment or a redacted URI |
| `internal/diagram` | the ER diagram model: tables, foreign-key clusters, a text rendering for the terminal |
| `internal/board` | metrics boards: TOML files of parameterised query panels |
| `internal/tools` | pure data tools: schema diff, data diff, bulk update, fake data. They return statements; the UIs run them through `Apply` |
| `internal/ai` | the assistant: Anthropic through the official SDK, or any OpenAI-compatible endpoint |
| `internal/mcp` | the MCP server (stdio or streamable HTTP): tools over your connections, per-token scopes (`read-only`, `read-write`, `full`), approval for unsafe statements |
| `internal/plugin` | plugins from `<config dir>/plugins`: `*.toml` (an external command) and `*.js` (a goja script) |
| `internal/sqlfmt` | the keyword-level SQL beautifier and minifier |
| `internal/clip` | OSC 52 and system clipboard |
| `internal/crashlog` | appends a panic, its stack and the last console lines to `~/.local/share/webtuidb/crash.log`; nothing is sent anywhere |

## Request flow: a commit from the web

You edit two cells, delete a row and press {{< key "C-d s" >}} (`commit`).

```
Grid edit ──► pending state (edits by row, deletes, new rows)         web/src/utils/pending.ts, DataTab.tsx
     │
     ▼ commit
POST /api/sessions/{id}/plan  {resultId, updates[{row,set,key}], deletes, deleteKeys, inserts}
     │   buildChanges (changes.go): primary key values come from the server's cached copy of the rows,
     │   and the "key" the browser saw must match it, or the request is rejected as stale
     ▼
Session.Plan(changes) ──► []db.Statement   (parameterised, with a Display form for the preview)
     │   readOnlyViolation: a read-only connection refuses writes here
     ▼
{planId, statements, gate{confirm,password}, transactions, levelLabel}     the plan is kept on the server
     │
     ├─ gate says nothing needed ──────────────────────────────┐
     └─ gate asks ──► PlanDialog shows the SQL, asks for        │
                      confirmation / the database password       │
                                                                 ▼
POST /api/sessions/{id}/apply  {planId, confirmed, password}
     │   re-checks readOnlyViolation and the safe-mode gate (safemode.Level.Gate): the browser cannot skip it;
     │   a failed gate puts the plan back
     ▼
Session.Apply(ctx, stmts)   one transaction when Capabilities.Transactions, detached from the request's cancellation
     │   error ─► 400 {error, partial}   (partial: DDL or no transactions, so some statements may have run)
     ▼
ss.record(sql, "gui", duration, original)  ──► store.History (JSONL) and the console ring
     ▼
column cache cleared, the result snapshot dropped, the grid reloads
```

Two details matter. The server only ever runs statements it planned itself: `apply` takes a `planId`, never SQL from the client, so the safe-mode gate and the read-only check cannot be bypassed from the browser; ad-hoc SQL goes through `exec`, which has the same gate. And `Preimage` records the old values (one pre-image per commit, on the first statement's history entry) when `[general] log_original_values` is on.

## Request flow: a commit in the terminal

```
Grid edits ─► pending cells in the grid (marked amber, deleted rows struck through)       internal/ui/grid.go
     │
     ▼ commit (C-d s): App.key ─► Machine.Feed ─► App.run("commit") ─► Workspace.commit ─► tableTab.Commit
tableTab.Commit
     │   no primary key and an update or delete pending? ─► confirmation: rows are matched by all their values
     │   Statements() ─► Grid.Changes(ref) ─► Session.Plan(changes)
     ▼
Workspace.executeWith(preimage, stmts, then) ─► Workspace.gate(stmts, run)
     │   safemode.Level.Gate(readOnly): confirmation dialog, or a password form checked against the stored password
     ▼
run: sess.Apply(context.Background(), stmts)       no deadline: a commit is never cut off half way
     ▼
applyDoneMsg ─► Workspace.recordOrig ─► store.History.Append(..., Source "gui") and the console
     ▼
then(err): grid.DiscardPending(), reload the tab, "committed N statement(s)"
```

The two flows share everything below `Session.Plan`; the differences are above it. In the terminal the grid builds `db.Change` values itself, and a table without a primary key is edited by matching every original value. The web sends row indexes and the key values it saw, and the server builds the changes from its cached copy and refuses to update or delete rows on a table with no primary key.
