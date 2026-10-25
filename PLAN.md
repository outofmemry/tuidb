# Status (milestone 1 in progress)

**Built and tested** (`make test`; `make smoke` runs the same suite plus driver and end-to-end tests against live databases):
config + keymap with tmux-style prefix, all five drivers, store (history, saved queries, state), welcome screen with
connection form, workspace with sidebar / tabs / console / row detail / Open Anything, data grid (browse, page, sort,
inline edit, add / duplicate / delete rows, Set Value, undo / redo, copy-as formats, commit / preview / discard),
filter bar, quick filter, visible-columns picker, foreign-key follow, structure editor (columns, primary key,
foreign keys, indexes, new table), database new / rename / drop, SQL editor (highlighting, autocomplete with saved-query
keywords, run current / all, cancel, row limit, per-statement result tabs), safe mode (all five levels),
Json / Tree views for MongoDB and Redis, `webtuidb -keys`.

**Milestone 1 is complete.** Added since the first pass: trigger, view and function editing (opened as runnable SQL in a
query tab, so safe mode applies), Redis key folders by `:`, paste rows / cells, find in a grid or result (`/`, `n`, `N`),
pinnable result tabs, editable query results (single-table `SELECT` with the whole primary key in the select list),
a JSON tree in quick look, exact count (`#`) and a limit / offset box (`L`), a Ctrl+t connection test in the connection
form, reopening workspaces at start (`reopen_workspaces`), keep-alive pings and a "taking too long: wait or cancel" prompt.
Beautify / uglify, find in the editor, import / export, the process list and everything in M2 / M3 still answer with a
"later milestone" message.

**Reviewed:** an independent read of the UI and of the db layer found and I fixed: NULL cells becoming `''` on an untouched edit,
edits against a hidden primary key, pending edits dropped by sort / paging / filters / columns / database switch, double commits,
reconnecting to the wrong database, several panics, stale editor selections, a data race on the session, and a set of
safe-mode bypasses in the read-only classifier (MongoDB shell methods, PostgreSQL `#` / `E''` / `$$`, MySQL `/*! */`),
plus identifier escaping for ClickHouse, Redis list deletes by index, TLS URL options being ignored. Each has a regression test.

**Second review (review.md, 78 P1 + 16 P2/P3): all P1 items fixed.** Safety: safe_mode / ssl_mode / unknown keys in
connections.toml are rejected, default_safe_mode applies, unknown levels fail closed, Enter never confirms a destructive
dialog, drop and truncate need the name typed, an empty stored password cannot pass Safe Mode, Copy-URL leaves the password
out unless asked (`U`), password_cmd runs without a shell, the connections file is forced to 0600, the read-only classifier
handles `a$$b$$` identifiers, JavaScript comments and template literals, EXECUTE/CALL/DO, and the MongoDB raw filter is
parsed as a pure document in a helper-only runtime. Data: atomic fsynced writes for every file, a bounded history, a
corrupt state.json is moved aside, committed rows are cleared before the reload, one commit at a time, query results
refresh in place, loads are cancellable and read-only while in flight, a workspace generation drops results from a
previous database or session, MySQL DDL runs outside transactions and keeps comments/collations/generated columns,
MySQL routines are created under a new name before the old one is dropped (with DELIMITER support in the splitter),
PostgreSQL serial columns copy as identity columns and timestamptz keeps its offset, Redis pages streams by id and
checks key types before MULTI, ClickHouse `USE` switches the session. Memory: copy-on-write grid snapshots, cached
editor text and lexing, cached document views, capped quick look, bounded Redis replies. Compose binds to loopback with
a Redis password; CI runs the full suite. Settings nothing read were removed from config.toml (they now error).

**Known limits (documented, not fixed):** each editor statement runs on its own pooled connection, so `BEGIN` / `SET` do not
carry over to the next statement; MySQL DDL commits implicitly, so a failed multi-statement structure change can be partly applied;
`ssl_mode = prefer` means plaintext on engines without opportunistic TLS (ClickHouse, Redis, MongoDB); the editor and text fields
window by rune count, so wide (CJK) text can scroll the cursor out of view; a PostgreSQL connection the server dropped fails the
first query before recovering.

**Deviations from the design below:** focus keys are `M-h/j/k/l` (plain `h/j/k/l` clash with open-database);
`C-[` `C-i` `C-d` cannot be bound, so data / structure / index / DDL are `(` `)` `M-i` `M-d` (global);
rebinding a key replaces its default (tmux `bind`), a context default never shadows a user binding;
the Postgres driver has its own small pool (pgxpool needs `puddle`, which was not vendored when it was written).

---

# webtuidb — a TablePlus clone for the terminal

## Context

TablePlus is a native GUI database client (connections, data grid with pending edits, SQL editor, filters, import/export, safe mode, history, process list, users, backup/restore, ER diagram, metrics board, AI assistant, MCP server). The user wants the same feature set as a TUI called **webtuidb**, for **PostgreSQL, MySQL, ClickHouse, Redis and MongoDB**, driven tmux-style: a **prefix key (Ctrl+d) + key** for every action, all rebindable from `~/.config/webtuidb/config.toml`.

Decisions already made with the user:
- **Go + Bubble Tea** (Go 1.27 installed). Single static binary.
- **Prefix = `C-d`** by default.
- **Passwords in plain text** in `~/.config/webtuidb/connections.toml` (0600), with an optional `password_cmd` whose stdout is the password.
- **Milestones, core first** (M1 → M2 → M3). Each milestone is shippable.

The project folder is empty (only `.claude/settings.json`). Not a git repo yet; `git init` happens in M1.

The complete TablePlus docs inventory (≈60 pages incl. hidden sub-pages) was crawled; the feature list below is derived from it. Where a GUI concept has no terminal equivalent (mouse drag canvas, Touch ID, Keychain, native menus, licensing, crash reports, themes-from-git, DBngin) it is dropped or replaced with a text equivalent, noted explicitly.

## Stack / dependencies

| Need | Library |
|---|---|
| TUI | `charmbracelet/bubbletea` v2, `bubbles` v2 (textarea, textinput, viewport, list, table), `lipgloss` v2 |
| Charts (results Chart view, metrics board) | `NimbleMarkets/ntcharts` (bar/line/sparkline for Bubble Tea) — M2/M3 only |
| PostgreSQL | `jackc/pgx/v5` (native: query cancel, `RAISE NOTICE` notices, COPY) |
| MySQL | `go-sql-driver/mysql` |
| ClickHouse | `ClickHouse/clickhouse-go/v2` (native protocol) |
| Redis | `redis/go-redis/v9` |
| MongoDB | `go.mongodb.org/mongo-driver/v2` + `dop251/goja` for Mongo-shell JS syntax |
| SSH tunnel | `golang.org/x/crypto/ssh` (all 5 drivers accept a custom dialer) |
| Config | `BurntSushi/toml` |
| Excel export/import | `xuri/excelize/v2` (M2) |
| MCP server | `modelcontextprotocol/go-sdk` (M3) |
| Everything else | stdlib (`encoding/csv`, `encoding/json`, `net/http` for LLM providers, `os/exec` for backup tools) |

No SQL formatter library exists in Go that is good enough; write a small keyword-based beautifier/uglifier (`internal/sqlfmt`, ~150 lines, `shortcut:` comment).

## Package layout

```
webtuidb/
  main.go                       flags: webtuidb [url|connection-name] [--config path]
  internal/config/              config.go (Config + defaults + Load/merge), connections.go (Connection, groups, tags, load/save 0600, URL import/export), paths.go (XDG dirs)
  internal/keymap/              key.go (parse "C-d", "M-x", "S-Enter", "f5"...), action.go (Action enum + default bindings table), prefix.go (prefix state machine, which-key listing)
  internal/db/                  driver.go (interfaces + value types), registry.go, split.go (statement splitter), classify.go (read-only detection per dialect), change.go (pending change model → statements), filter.go (filter rows → WHERE), ssh.go (tunnel dialer)
  internal/db/postgres/ mysql/ clickhouse/ redis/ mongo/    one package each implementing db.Driver
  internal/store/               history.go (JSONL per connection), queries.go (favorites = .sql files + keywords), state.go (per-table column widths/filters/sort, last session, pinned items)
  internal/safemode/            level enum, confirm/password gate
  internal/sqlfmt/              beautify/uglify
  internal/export/ internal/importer/   CSV/JSON/SQL/Excel writers and wizards (M2)
  internal/ui/                  Bubble Tea models (see UI section)
  internal/ui/dialog/           confirm, password, text input, picker, which-key overlay
  testdata/ + docker-compose.yml (5 DBs with seed data)
```

## Core design

### Driver interface (`internal/db/driver.go`)

One interface, with a `Capabilities` struct so the UI hides what an engine lacks instead of per-engine `if`s in the UI.

```go
type Driver interface {
    Open(ctx, cfg config.Connection, dial DialFunc) (Session, error)
}
type Session interface {
    Caps() Capabilities                                   // HasSchemas, HasIndexes, HasTriggers, HasFunctions, HasProcessList, HasUsers, Document (Mongo/Redis), CanReorderColumns, CanRenameDB, HasMaterializedViews
    Databases(ctx) ([]string, error); Use(ctx, db) error
    Schemas(ctx) ([]string, error)
    Objects(ctx, schema) ([]Object, error)                // tables, views, matviews, functions, procedures, sequences, types
    Columns/Indexes/ForeignKeys/Triggers(ctx, table) ...
    DDL(ctx, obj) (string, error)
    Rows(ctx, q RowQuery) (*Stream, error)                 // table browse: schema, table, filter WHERE, sort, limit, offset
    Count(ctx, table, filter, estimate bool) (int64, error)
    Query(ctx, text string, limit int) (*Stream, error)    // free-form; Stream delivers column meta, then row batches, then Message (affected rows, notices), and supports Cancel()
    Plan(changes []Change) ([]Statement, error)            // pending edits → statements for Preview
    Apply(ctx, stmts []Statement) error                    // in one transaction where supported
    Processes(ctx) ([]Process, error); Kill(ctx, id) error
    Users(ctx) ...; ApplyUser(ctx, UserChange) error       // PG + MySQL only
    Ping(ctx) error; Close() error
}
```

Engine mapping of the TablePlus "table" concept:
- **PostgreSQL / MySQL / ClickHouse**: as-is. ClickHouse: no indexes/triggers/FKs, column reorder allowed, process list via `system.processes` + `KILL QUERY`.
- **MongoDB**: databases → collections ("tables"); Data view is a grid of flattened top-level fields, plus **Json** and **Tree** views (same as TablePlus). Query editor has a syntax toggle: **Mongo Shell (JS)** via goja with a `db.<coll>.<method>()` shim (find/findOne/aggregate/count/insert*/update*/delete*, cursor `.sort/.limit/.skip/.project`) and **Mongo Command** (extended-JSON `runCommand`). Pending edits → `updateOne` by `_id`.
- **Redis**: databases 0..N (`CONFIG GET databases`); "tables" = key namespaces split on `:` (TablePlus groups keys this way), items = keys via `SCAN`. Opening a key shows its value as a grid (hash/list/set/zset/stream rows; string as one cell) with Json/Tree views. Query editor runs raw commands, one per line, results as a table. Pending edits → `HSET`/`LSET`/`SADD`/`ZADD`/`SET`/`DEL`.

### Keymap (`internal/keymap`) — tmux-style

- **Every action is `prefix` then a key.** After the prefix the status bar shows `PREFIX` and a **which-key overlay** lists the bindings valid for the focused pane; `Esc` cancels. The prefix pressed twice sends the raw key to the focused widget (like tmux `C-b C-b`).
- Key syntax in TOML: `"C-x"`, `"M-x"`, `"S-Enter"`, `"C-M-x"`, `"F5"`, `"Enter"`, `"Tab"`, `"Space"`, `"Up"`, `"]"`. Shift+Enter and Ctrl+Enter need the kitty keyboard protocol (Bubble Tea v2 enables it; works in Ghostty/kitty/WezTerm/iTerm), so defaults avoid them.
- `-n ` prefix on a key means **no prefix needed** (tmux `bind -n`). Values can be a list.
- Only *actions* go through the prefix. Plain editing/navigation inside a widget (typing, arrows, `j/k`, `Enter` to edit a cell, `Esc`) is never prefixed.
- Unknown action names or unparsable keys in config → error at startup naming the line; duplicates within the same context → error.

Default bindings (derived from TablePlus: `⌘X → prefix x`, `⌘⇧X → prefix X`, `⌥⌘X → prefix M-x`, `⌃⌘X → prefix C-x`). Context-overloaded keys stay overloaded like TablePlus.

| Action (config name) | Default | Context |
|---|---|---|
| `help` (keymap list) | `?` | global |
| `open_anything` | `p` | global |
| `open_connection` / `open_database` | `K` / `k` | global |
| `new_query_tab` / `new_tab` / `close_tab` | `e` / `t` / `w` | global |
| `next_tab` / `prev_tab` / `tab_1..9` | `]` / `[` / `1`-`9` | global |
| `next_workspace` / `prev_workspace` / `workspaces` | `}` / `{` / `\` | global |
| `focus_left/down/up/right` | `h`/`j`/`k`/`l` (also arrows) | global |
| `toggle_sidebar` / `toggle_row_detail` / `toggle_console` / `toggle_results` | `0` / `Space` / `C-c` / `` ` `` | global |
| `commit` / `preview` / `discard` | `s` / `P` / `Backspace` | global |
| `undo` / `redo` | `z` / `Z` | global |
| `reload` / `reload_tab` / `reconnect` / `disconnect` | `r` / `M-r` / `R` / `M-.` | global |
| `process_list` / `safe_mode` / `search_in_database` / `users` / `diagram` / `metrics` / `backup` / `restore` | `.` / `S` / `/` / `u` / `d` / `m` / `b` / `B` | global |
| `export` / `import` | `E` / `I` | global |
| `run_current` / `run_all` / `cancel_query` | `Enter` / `M-Enter` / `.` | editor |
| `beautify` / `uglify` / `toggle_comment` / `block_comment` | `i` / `I` / `/` / `M-/` | editor |
| `save_sql_as` / `add_to_queries` | `W` / `a` | editor |
| `find` / `find_replace` | `f` / `M-f` | editor, results |
| `select_next_occurrence` / `select_all_occurrences` | `D` / `C-g` | editor (M3) |
| `add_row` / `duplicate_rows` / `delete_rows` | `i` / `D` / `Delete` | grid |
| `copy_rows` / `copy_rows_as` / `copy_cells` / `paste_rows` / `paste_cells` | `c` / `C-c` / `C` / `v` / `V` | grid |
| `quick_look` / `set_value` | `Enter` / `M-Enter` | grid |
| `filters` / `columns` / `apply_filters` | `f` / `M-f` / `Enter` (filter bar) | grid |
| `first_row` / `last_row` / `next_page` / `prev_page` | `Up`/`Down`/`Right`/`Left` | grid |
| `show_data` / `show_structure` / `show_index` / `show_ddl` | `C-[` / `C-]` / `C-i` / `C-d` | table tab |
| `sidebar_search` / `truncate` / `delete_item` | `F` / `M-Delete` / `Delete` | sidebar |
| `split_right` / `next_pane` / `prev_pane` | `"` / `M-]` / `M-[` | global (M3) |
| `quit` | `q` | global |

Un-prefixed widget keys (not configurable in M1): grid `hjkl`/arrows, `g`/`G`, `Enter` edit cell, `Esc`, `/` quick search in lists, `Tab`/`S-Tab` move between form fields, `y`/`n` in confirms.

### Config files

`~/.config/webtuidb/config.toml` (all optional; defaults mirror TablePlus):
```toml
[general]
prefix = "C-d"
reopen_workspaces = true
new_tab_opens = "query"          # or "empty"
query_timeout_secs = 300
keep_alive = true
row_limit = 300                  # page size for table browse
estimate_count_over = 1000000
default_safe_mode = "silent"     # silent | alert1 | alert2 | safe1 | safe2
log_original_values = false      # history logs pre-image CSV on UPDATE/DELETE

[editor]
autocomplete = true; add_space = true; prefix_schema = false; uppercase_keywords = true; close_braces = true
complete_key = "enter_or_tab"    # enter | tab
indent = "spaces"; indent_width = 4; indent_type = "standard"  # tabular_left | tabular_right
result_mode = "tabs"             # tabs | append
return_on_error = false
auto_save_tabs = "first"         # none | first | all
auto_save_queries = true
query_params = false; param_format = "colon"; params_in_strings = false
show_invisibles = false; wrap = true; highlight_current = true

[table]
alternating_rows = true
filter_default_column = "pk"     # any | raw
filter_default_operator = "="    # contains
filter_state = "remember"        # show | hide
default_sort = "none"            # pk_asc | pk_desc

[csv]  delimiter = ","; quote = "\""; line_break = "\n"; decimal = "."

[sidebar] show_recent = true; show_functions = true; show_sequences = true; show_types = true; show_columns = false; load_system_schemas = false; lazy_load = true; matching = "fuzzy"

[theme] name = "dark"          # dark | light; plus [theme.colors] overrides (keyword, string, comment, number, modified_row, new_row, deleted_row, selection, error, tag.production...)

[keys]   # action = key(s); see table above
commit = "s"
run_all = ["M-Enter", "-n F5"]

[ai]     provider = "anthropic"; model = "claude-sonnet-5-5"; api_key_cmd = "op read ..."; [ai.providers.local] base_url = "http://localhost:11434/v1"   # M3
[mcp]    enabled = false; port = 51003; approval_for_unsafe = true                                                                              # M3
```

`~/.config/webtuidb/connections.toml` (0600; created on first save):
```toml
[[connection]]
name = "Acme Prod"; driver = "postgres"; host = "..."; port = 5432; user = "..."; password = "..."  # or password_cmd = "..."
database = "acme"; tag = "production"; color = "#c0392b"; safe_mode = "alert2"; group = "Acme"
ssl_mode = "prefer"; ssl_cert = ""; ssl_key = ""; ssl_ca = ""
bootstrap = ["SET search_path TO app"]
[connection.ssh] host = ""; port = 22; user = ""; key = "~/.ssh/id_ed25519"; password = ""   # empty key → ssh agent / ~/.ssh/config
```
Groups nest via `group = "A/B"`. Tags default: local, testing, development, staging, production (colored) + custom via `[[tag]]`.

State and data under `~/.local/share/webtuidb/`: `history/<conn>.jsonl`, `queries/**/*.sql` (favorites; first line `-- name: X keywords: a, b`), `state.json` (column widths, per-table filter/sort, pinned items, last session, recent items), `boards/*.toml` (M3).

### UI (`internal/ui`)

Root `App` model: owns connections, open **workspaces** (one per open database; stack shown as a narrow strip on the far left when >1), the prefix state machine, overlays (dialogs, which-key, palette), the status line (connection · database · TAG in its color · safe-mode icon · PREFIX indicator · progress). All DB work runs in `tea.Cmd`s with a cancellable context; the UI never blocks.

Workspace layout (all panes toggleable; sizes from terminal):
```
┌ sidebar (Items/Queries/History tabs, search, schema picker) ┬ tab bar: [SQL 1] [customers] [orders*]        ┐
│                                                              │ content: data grid | structure | index | ddl   │
│                                                              │         or editor + run bar + results          │
│                                                              ├ row detail (right pane, toggled)               │
├──────────────────────────────────────────────────────────────┴ console log (bottom, toggled)                  ┤
└ status line                                                                                                   ┘
```

Models: `welcome` (connection list with groups/tags/search; create/edit form; import URL; duplicate; export/import `.toml` with optional passwords), `workspace`, `sidebar`, `tabs`, `grid` (virtualised rows, cell selection rectangle, pending-edit highlighting, inline cell editor, sort cycle on header, FK follow), `structure` (columns/PK/FK/default editors as a grid with Set-Value menus), `indexes`, `triggers`, `ddl`, `editor` (textarea + highlighting + autocomplete popup + run bar with limit dropdown / Mongo syntax toggle + fold-by-statement), `results` (tabbed per statement, pinned tabs, Data/Message/Chart, search/replace, export), `console`, `rowdetail` (field list with quick values, bulk edit for multi-select, PRETTY/MINIFY, Assistant tab in M3), `filterbar`, `palette` (Open Anything: fuzzy with `^`/`$` anchors, lists databases/schemas/objects/saved queries/app features), `quicklook` (text / JSON Raw+Tree / hex for binary; save-as / load-file), `dialog/*`.

Tab reuse follows TablePlus exactly: opening an object replaces the current tab unless it has pending changes or is an editor; `Enter` on a sidebar item = open (reuse), `o` = open in new tab.

## Milestones

### M1 — Core (connect, browse, edit, query) for all 5 engines
1. `go mod init webtuidb`, `git init`, layout above, `main.go`.
2. `config` + `keymap` with full default table, TOML override, validation, which-key overlay, prefix state machine. **Unit tests**: key parsing, config merge, conflict detection.
3. `db` core: interfaces, statement splitter (handles `;` in strings/comments/`$$` bodies), read-only classifier per dialect, SSH tunnel dialer, pending-change → statement planner, filter → WHERE builder (operators: `= <> < > <= >= IN NOT IN IS NULL IS NOT NULL BETWEEN NOT BETWEEN LIKE ILIKE(pg) Contains/Not contains/Has prefix/Has suffix`, Any column, Raw SQL; AND/OR combine). **Unit tests** for splitter, classifier, planner, filter builder.
4. Drivers: postgres, mysql, clickhouse, redis, mongo — `Databases/Schemas/Objects/Columns/Indexes/FKs/Triggers/DDL/Rows/Count/Query(stream+cancel)/Plan/Apply/Ping`. Multi-db-version compatibility via information_schema / `system.*` / `listCollections` / `SCAN`.
5. UI: welcome (list, groups, tags, search, create/edit/test/duplicate/delete, import from URL, `webtuidb <url>` CLI), workspace, sidebar (Items with recent/pinned/functions/sequences/types sections, fuzzy/contains search, context actions: open, open in new tab, structure, copy name, copy script as create/drop/truncate/select-100, clone, truncate, delete, pin), tabs, data grid (paging 300, limit/offset popover, estimated count + exact on demand, sort, cell select/copy/paste, add/duplicate/delete rows, inline edit, Set Value menu: EMPTY/NULL/DEFAULT/NOW()/date, FK follow, quick filter), filter bar + column picker, structure/index/trigger/DDL tabs with create/rename/alter/drop for tables, columns, PK/FK/defaults, indexes, triggers, views/matviews/functions/procedures (DDL editor), database open/new/rename/drop, commit/preview/discard with undo/redo, **safe mode** (5 levels, confirm dialog listing statements, password dialog), editor (autocomplete from schema + keywords, run current/all, cancel, row-limit, result tabs with `-- name:` labels, pinning, Data/Message views, result editing + commit, copy-as formats, result search), console log (last 1000, timestamps), row detail pane, Open Anything, quick look (text/JSON tree/hex), status line with tag colour and reconnect-on-click-equivalent (`reconnect` action), keep-alive + timeout dialog.
6. History (per connection, incl. GUI-generated statements, search, run/copy/insert/save-as/add-to-queries/delete/clear) and Queries/favorites (folders, keywords → autocomplete expansion, auto-save, run/edit/rename/delete, shared folder path).
7. `docker-compose.yml` with the 5 engines + seed scripts; `make smoke` runs an integration test per driver against them.

### M2 — Data tooling
- Import wizards: CSV (delimiter/quote/encoding/header/create-table/all-text/matching strategy/insert option/per-column mapping/preview 100), JSON, SQL dump; background job with progress in a notifications list.
- Export: tables (CSV/JSON/SQL with structure+drop+data+gzip/Excel), result export incl. all tabs, current page, column selection, filename patterns.
- Results **Chart** view (bar/line; pie rendered as a sorted bar list) + `-- barchart x: a, y: b` comment syntax.
- Query parameters (6 formats, Variables dialog, remembered values), reformat (beautify/uglify, indent settings), split results modes, return-on-error, streaming "load more", `log_original_values`.
- Search in Database (contains/not/=/prefix/suffix, all or selected items, pause/resume, click → filtered table).
- Process list (PG `pg_stat_activity`, MySQL `processlist`, ClickHouse `system.processes`, Mongo `currentOp`, Redis `CLIENT LIST`) with kill.
- Copy-as formats everywhere (plain/JSON/HTML/Markdown/CSV/CSV+header/INSERT), CSV copy/paste settings.
- Themes: dark/light built-ins + colour overrides; export/import theme TOML.

### M3 — Admin and extras
- User management (PG roles/attributes/table privileges; MySQL users@host, global/db privileges, resource limits).
- Backup/restore by shelling out to `pg_dump/pg_restore`, `mysqldump/mysql`, `clickhouse-client`, `mongodump/mongorestore`, `redis-cli --rdb` if on `PATH` (TablePlus bundles these; we do not), with option picker, gzip, live output, cancel, safe-mode gate.
- ER diagram: text layout (boxes with columns, FK list per box, colour by FK cluster, search, sort by name/column count, add table/FK, delete) — no 2D drag canvas.
- Metrics board: TOML-defined boards of panels (data table, scoreboard, bar/line chart, input, dropdown, button, exporter) with `$var` wiring and refresh intervals; "Add to Metrics Board" from a result chart. No free-form drag layout; panels tile in a grid.
- Split pane right (two tab groups side by side), next/prev pane.
- Editor extras: multiple carets (select next/all occurrences, add caret above/below), Vim keybinding mode (basic normal/insert/visual motions), invisible chars, fold state persistence.
- AI Assistant tab (Anthropic, OpenAI-compatible incl. Ollama; `@` table context; SQL blocks with copy/run; approval for non-read-only; stop; chat history).
- MCP server (stdio + HTTP, tokens with read-only/read-write/full and per-connection scope, tools `list_connections/list_open_connections/open_connection/disconnect/execute_read_query/execute_query`, activity log, approval sheet for unsafe queries).
- Plugins: `~/.config/webtuidb/plugins/*.toml` declaring a command (any executable) that receives JSON context (connection, selection, cell) on stdin and returns text/SQL; shown in the palette and context menus. Not TablePlus JS plugins.
- Bootstrap commands, encoding override, workspace overview, go back/forward, reopen workspaces at startup.

Dropped (no terminal equivalent / not applicable): licensing, crash reports, Keychain, Touch ID, passcode lock, native menu bar/toolbar customisation, themes from git, DBngin, iOS, Windows .NET notes, spatial map preview, image/PDF preview (hex + save-as instead), Excel import (CSV only; export supported).

## Verification

- `go build ./... && go vet ./... && go test ./...` — unit tests for keymap, config, splitter, classifier, change planner, filter builder, sqlfmt, CSV mapping.
- `docker compose up -d` (Docker Desktop must be running; it is installed but the daemon is currently stopped) then `make smoke`: integration test per driver (connect, list objects, browse, plan+apply an edit, run a query with cancel, process list).
- Manual TUI check per milestone in the app's terminal: `webtuidb` → create the 5 seeded connections → browse/edit/commit a row in each → run multi-statement query → safe-mode dialog appears on `alert2` with an UPDATE → rebind `commit = "x"` in config.toml and confirm `C-d x` commits and `C-d ?` shows the new key.
- Record a `vhs`/`asciinema`-free check: drive the binary in `tmux` and capture panes for the autonomous smoke of the TUI itself.
