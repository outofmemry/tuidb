# Status (milestones 1-15 complete)

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
**Milestone 2 is complete (terminal and web).** Import (CSV / JSON / SQL dump with preview, mapping, create-table, background
progress), export (CSV / JSON / SQL with structure + drop / Excel, gzip, whole table with filters or current page or a
result), results chart (bar / line / pie, `-- barchart x: a, y: b`), query parameters (six formats, Variables dialog with
remembered values), beautify / uglify, editor find / replace, result mode append + return-on-error, load more, search in
database (opens the hit as a filtered table), process list with kill, copy-as formats on grids and results with the `[csv]`
settings, `log_original_values` (key pre-image in history), themes with `[theme.colors]` overrides and `webtuidb -theme` export.
Shortcuts taken (each marked `shortcut:` in code): load more re-runs with a doubled limit; the pre-image logs key values only;
the web import travels inside a JSON body and the web export is built in memory; the web search does not stream progress.

**Milestone 3 is complete (terminal and web).** User management (PostgreSQL roles, MySQL users; every change goes through
Preview and the safe-mode gate), backup / restore by shelling out to `pg_dump` / `pg_restore` / `psql`, `mysqldump` / `mysql`,
`mongodump` / `mongorestore`, `redis-cli --rdb` and ClickHouse server-side `BACKUP` (passwords via env or a 0600 config file,
dumps to `.part` then renamed), ER diagram (text boxes in the terminal, tinted boxes in the web, coloured by FK cluster,
sort / filter, opens the structure editor), metrics boards (`~/.local/share/webtuidb/boards/*.toml`, `$var` inputs,
table / number / bar / line panels, auto refresh, "Add to Metrics Board" from a results chart), split pane right, editor
extras (next / all occurrences, Vim keys, invisibles), AI assistant (Anthropic via the official SDK, OpenAI-compatible
incl. Ollama; `@table` pulls the DDL in; ```sql blocks copy / run through the normal run path), MCP server (`webtuidb --mcp`
stdio and `[mcp] enabled` HTTP with scoped tokens and an activity log), plugins (`~/.config/webtuidb/plugins/*.toml`
executables fed JSON on stdin), bootstrap statements, go back / forward.
Also: an MCP approval sheet (an unsafe statement waits, up to five minutes, for Approve / Deny in whichever UI is running),
fold by statement (`M-z`, remembered per saved query or tab in state.json) and a per-connection `encoding` override
(PostgreSQL client_encoding, MySQL charset).
Shortcuts taken (each marked `shortcut:` in code): the read-only MCP scope relies on the statement classifier, not a
database-enforced read-only transaction; no AI streaming; the terminal has one cursor, so "select next occurrence" jumps
instead of adding a caret; Vim mode is normal / insert only; folds are tracked by statement index, so an edit unfolds. **M3 is in progress:** the engines for the ER diagram (`internal/diagram`), metrics board (`internal/board`), assistant (`internal/ai`), plugins (`internal/plugin`) and MCP (`internal/mcp`, `--mcp` serves stdio) exist with tests, but neither UI is wired to them yet. What remains after M3 is listed under M4 (TablePlus parity), M5 (debt, limits, beyond TablePlus), M6 to M8 (second- and third-pass parity) M9 (depth of existing features), M10 (web depth), M11 (editor completion, last depth items), M12 (web editing parity), M13 (milestone 3 depth), M14 (terminal / web parity, verified by tests) and M15 (everything else from TablePlus, final) below.

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

**Milestones 4 and 5 are complete (terminal and web).** M4: the web now supports every bound action the terminal has;
tab menu (close others / right / all, rename), cancel running queries; editor `auto_save_tabs`, wrap, `indent_type`,
autocomplete toggles, cleanup of remembered variables, statement folding, case transforms, use selection for find, jump
to selection, complete on demand; PostgreSQL notices in the Message view, Match Case, export all result tabs, export the
Message view, chart as PNG (web); multi-column sort, Date Picker and Add a file in Set Value, Advanced expressions and
sequence defaults, hex editing and View Using Encoding in quick look, item overview, hide / show columns,
`filter_column_sort`; `[sidebar]` show_columns / sort_columns / load_system_schemas / lazy_load, query sort, `[queries]
shared_dirs`, import and open `.sql` files, history Save As / Import, console clear + highlighting; connection export /
import to a passphrase-protected file (commands stripped on import), `[[source]]` folders, `[[tag]]` colours,
`password = "ask"`, URL open targets; Redshift / Greenplum / Vertica / SingleStore profile drivers (sqlite, duckdb,
cockroach and d1 stubs removed); named themes, `auto`, `theme.file`; MCP listen address + TLS, CSV log export, clients;
process list sort / copy / export / quick look, assistant model list and message actions, board connection + duplicate,
database stack labels, `--ssh-log`, structure column reorder; web ER canvas (pan, zoom, drag, FK by drag) and board
drag / resize. M5: streaming export, multipart import, SSE progress and streamed results, connections reload on file
change, SSH key passphrase, ProxyJump, SOCKS, `read_only` (server-enforced on PostgreSQL / MySQL), RDS IAM auth, DELIMITER
(already there), formatter CASE / window / ON CONFLICT / FOR UPDATE + tabular indent, fuzz tests, Redis stream marks,
backups via Go gzip and `.part` + rename, Windows config lock, MCP approval sheet, session-pinned query tabs
(`db.Pinner`), PostgreSQL retry only when safe, TLS + login password for the web, mobile layout, EXPLAIN tree,
alias-aware completion, snippets, schema diff, data diff, bulk update, fake data, OSC 52, mouse clicks, 16-colour
fallback, goreleaser, Dockerfile, completions, `--version`, man page, `-mcp-config`.
Shortcuts and skips (each marked `shortcut:` in code or named here): no new engines beyond the four profiles (SQL Server,
BigQuery, etc. not started); ACP agents / Copilot / Codex sign-in skipped; no interactive per-user safe mode; the
read-only classifier is still regex-based; "load more" pages by re-running inside a subquery, not a server cursor;
result streaming batches whole results; web board layout and diagram positions live in localStorage; no Playwright
suite (offline install); the MCP clients view is derived from the log; a global login lockout can be tripped by an
attacker; the assistant reply is not streamed; terminal "select next occurrence" jumps rather than adding a caret.
Three independent reviews (one per wave) found and fixed: bash completion executing connection names, imported
connection files able to run commands, a loopback-port cookie scope hole, a double-execution retry, Mongo passwords in
`ps`, backups truncating a previous dump on failure, an MCP approval sheet that a stray keypress could approve, backup
paths outside the data dir, tab closes losing unsaved edits, and INSERT copy-as injection on MySQL.

**Milestones 6, 7 and 8 are complete (terminal and web).** M6: the safe-mode password gate when lowering the level,
`+ssh` connection URLs with `tLSMode` / `usePrivateKey`, welcome-group edit / delete and "close other connections",
split pane down, rectangular block selection, find next / previous, Mongo shell method completion, a standalone SQL
viewer with Open file / Open recent (`[general] recent_files`), charts with several Y columns, create / drop schema from
the sidebar, custom item groups, create database with encoding / collation, multi-schema export, the filter bar's SQL
popover, binary "Save as…", CSV `quote_mode` and the delimiter / decimal warning. M7: the process list on its own
connection, per-connection dump tool paths (`tools = { pg_dump = "..." }`) and remembered backup options, restore into a
new database, column-level privileges, board button / import panels with `refresh_on` wiring, MCP tools for saved
queries, history and boards, per-chat assistant providers (`[ai.providers.<name>]`), a Settings entry. M8: connection
Sort By (`[general] connection_sort`), a connection details popover with a colour picker, sidebar multi-select for
delete / truncate / export, `[table] auto_pretty_json`, `label:` / `tab:` result titles and a summary tab for runs of
more than 100 statements, a format picker in the Variables dialog, board new / rename / delete, "Diagram" from a table's
menu, and the structure-row form.
Shortcuts (each marked `shortcut:` in code or named here): the terminal's second split-down pane is not saved with the
tab; block selection counts characters, not cells; the web Settings view is read-only (the terminal opens `$EDITOR`);
restore into a new database does not drop it automatically on failure (the terminal offers to, the web reports it);
`export.Options.SafeFormulas` exists but is not wired; a dotted PostgreSQL schema name confuses the column-grant parser.
A fourth independent review found and fixed: restore bypassing `read_only`, split-down overwriting a saved query,
tool-path overrides via connection import, MCP history leaking statements and row pre-images to read-only tokens,
stale sidebar marks dropping the wrong tables after a database switch, and web drop-schema always cascading.

---

**Review fixes (2026-10-28).** All 18 P1 and 100 P2 findings from `review-latest.md` are fixed in both UIs, with
tests, except these partials: Vertica still reads `pg_catalog` for sidebar / structure (P2-010); `pinned.PlanTable` takes
no context so the diff jobs cancel between tables, not inside one (P2-055); history import still appends one entry at a
time, off the UI loop (P2-057); `$expr` is an allow-list plus an explicit "advanced" opt-in (P2-083); background jobs
are capped at 4 running and 3 finished exports per session (P2-084); `execute_read_query` is wrapped in a READ ONLY
transaction only on Postgres / MySQL (P2-094). Key changes: `close_all_tabs` moved to `alt+A` (browser owns `ctrl+w`),
`alt+shift+letter` now reaches the `alt+K`-style bindings, web punctuation chords (`alt+"`, `alt+.`) work on macOS.

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

### M4 — Parity (what the TablePlus docs still have that we do not, after M3)

Found by checking every page under tableplus.com/docs (67 pages, 2026-10-28) against the code. Same rule as before:
each item lands in the terminal UI and the web UI in one pass, with the same action name and key.

1. **Web parity.** `SUPPORTED` in `web/src/routes/Workspace.tsx` omits 43 bound actions. The ones the TUI already
   has go first: `redo`, `reconnect`, `safe_mode`, `toggle_console`, `toggle_results`, `set_value`, `paste_rows`,
   `paste_cells`, `quick_filter`, `follow_fk`, `page_settings`, `show_index`, `show_ddl`, `show_tree`, `triggers`,
   `truncate`, `delete_item`, `open_anything`, `open_database`, `next_workspace` / `prev_workspace` / `workspaces`,
   `toggle_comment`, `block_comment`, `save_sql_as`, `focus_*`. Then the M3 actions as they are wired.
2. **Navigation.** Workspace overview, "Cancel running queries" in Open Anything. Tab menu: close other tabs, close tabs to the right, close all, rename tab.
3. **Editor.** Statement folding with folds remembered per saved query; `auto_save_tabs = none | first | all`
   (restore editor tabs on reconnect); wrap-lines toggle; `indent_type = standard | tabular_left | tabular_right`;
   autocomplete toggles (tables / functions / keywords, `add_space`, `prefix_schema`); `params_in_strings`; a
   Cleanup action for remembered variables.
4. **Results.** PostgreSQL `RAISE NOTICE` shown in the Message view (the driver already collects notices),
   Match Case in the result search, export results from all tabs, export the Message view as text. Web only: export a
   chart as PNG.
5. **Grid and structure.** Sort by several columns, Date Picker and "Add a file…" in Set Value, hex editing in
   quick look, item overview, `View Using Encoding`, index comment, header actions
   "Hide this column" / "Show all columns", `filter_column_sort = name | ordinal`.
6. **Sidebar, queries, history, console.** `[sidebar] show_columns`, `sort_columns`, `load_system_schemas`,
   `lazy_load` (dropped in M1; put back). Queries: sort by name / created / modified, `[queries] shared_dirs`,
   import a `.sql` file, open file in tab. History: Save As…, Import…. Console: clear, syntax highlighting, replace
   queries over N KB with a note.
7. **Connections.** Export / import a group or all connections to a passphrase-protected file (passwords optional);
   `[[source]]` folders of connection files (Connection Sources); `[[tag]]` custom tags with colours; `password =
   "ask"` (prompt every connect); URL options `driverVersion`, `lazyload`, and the table-opening options `schema`,
   `table`, `column`, `operation`, `value`, `condition` (open a filtered table straight from the URL).
8. **Drivers.** `sqlite`, `duckdb`, `cockroach`, `d1` are `doc.go` stubs that still appear in `config.Drivers`:
   implement or remove them from the list. Not started: SQL Server, Redshift, BigQuery, Elasticsearch, libSQL,
   Snowflake, Oracle, Greenplum, Vertica, SingleStore. Redshift, Greenplum and Vertica are the PostgreSQL driver with
   a capability profile; SingleStore is MySQL's. The README still says five engines: update it with each driver.
9. **Themes.** Named built-in themes (Silver Dark, Jade Dark, Polaris Dark, Sunset Dark, Sunset Light), `theme.name =
   "auto"` following the terminal / `prefers-color-scheme`, import a theme TOML (export exists).
10. **MCP.** Confirm against the docs once M3 lands: streamable HTTP with bearer tokens, per-token connection scope,
    approval sheet for unsafe queries on Silent Mode, activity log with CSV export, remote TLS.

11. **Small items** (an hour or less each): row detail "Search for field…" with `matching = fuzzy | contains`; Set Value
    "Advanced…" default expression and a sequence default on PostgreSQL; CSV export options for NULL handling and
    line-break conversion (verify file-name patterns while there); run-button default action, and Cancel opening the
    process list on PostgreSQL / MySQL; process list sort by column, copy rows, export the page, quick look on a query;
    editor transformations (upper case, lower case, capitalize), Use Selection for Find, Jump to Selection; assistant
    extras (ACP custom agents, GitHub Copilot and Codex CLI sign-in, model list from the provider, message copy /
    resend / delete); MCP connected-clients view with disconnect and a launcher that starts the app in the background;
    metrics board Connection object (alternate connection) and board duplicate; database stack `show_database_name` /
    `show_connection_name`; an SSH tunnel debug log (`--ssh-log`); column reorder in the structure grid where
    `CanReorderColumns` is set (the capability exists, the grid handler is unverified). Web only: font size and line
    height settings, desktop notifications when a backup or import finishes, reveal-path for saved queries and exports.
12. **Web canvases.** The TUI draws the ER diagram and the metrics board as text (M3). The web can have the real thing:
    a diagram canvas with drag, zoom, colour by FK cluster and FK-by-drag, and a board with drag / resize panels.

Still dropped (see the list below): image / PDF / map preview, PostGIS to WKT, Keychain, Touch ID, passcode, crash
reports, auto-update, language, themes from git, DBngin, multiple OS windows, font settings in the terminal.

### M5 — Debt, limits and beyond TablePlus

Not in the TablePlus docs. Three buckets, in the order they bite.

**Debt the code admits** (`grep -rn 'shortcut:'` lists all 30; these are the ones users hit):
- Web export buffers the whole table before sending and web import travels inside a JSON body (32 MB cap): stream the
  export, multipart upload for the import (`internal/web/m2.go`).
- "Load more" re-runs the statement with a doubled limit in both UIs: keep a server cursor per result instead.
- `--frontend` only sees connections saved at startup; reload `connections.toml` on change (`internal/web/server.go`).
- Encrypted SSH keys have no passphrase prompt (`internal/db/ssh.go`).
- No DELIMITER support: MySQL routine bodies with `;` are split wrongly (`internal/db/split.go`, `internal/ui/definitions.go`).
- The read-only classifier is regex-based (`internal/db/classify.go`); a real parser per dialect closes the bypass class.
- `internal/sqlfmt` is token-level: CASE / WHEN, window clauses, ON CONFLICT and FOR UPDATE come out flat.
- Redis stream row marks shift after XDEL / XTRIM (`internal/db/redis/rows.go`).
- Backup piped into gzip can exit 0 on a failed dump without pipefail; only the direct child is killed on cancel (`internal/backup`).
- Windows has no cross-process config lock (`internal/config/lock_windows.go`).
- MCP refuses unsafe statements instead of asking; the assistant reply is not streamed.

**Known limits** (from the status header): a session-pinned connection per query tab so `BEGIN` / `SET` / temp tables carry
across statements and a transaction can be rolled back; MySQL DDL partial apply reported per statement; `ssl_mode = prefer`
warning on engines without opportunistic TLS; width-aware (CJK) cursor windowing in the editor and text fields; retry the
first query after PostgreSQL drops a pooled connection.

**Beyond TablePlus:**
- Web: TLS and a login beyond the single bearer token so `--insecure-listen` is usable on a LAN; per-user safe mode; a
  mobile layout.
- Streaming over the web: result rows as they arrive, and progress for search / import / export (SSE), instead of
  buffered responses.
- Editor: EXPLAIN / EXPLAIN ANALYZE as a tree, alias-aware column autocomplete, snippets.
- Data tools: table data diff, schema diff with generated migration SQL, bulk update by filter, fake data generator.
- Connections: AWS RDS IAM and Cloud SQL token auth, `ProxyJump` from `~/.ssh/config`, SOCKS proxy, a per-connection
  `read_only = true` flag.
- Terminal: OSC 52 clipboard so copy works over SSH, mouse clicks in the grid and sidebar, 16-colour fallback.
- Shipping: goreleaser binaries, Docker image, shell completions, `--version`, man page, fuzz tests for the splitter and
  classifier, Playwright tests for the web.

### M6 — Remaining TablePlus parity: connections, editor, sidebar

Found by a second pass over all 67 docs pages against M1–M5 (2026-10-28). Same rule: both UIs in one pass.

1. **Safe mode gate.** Lowering the level while the connection is in Safe Mode 1 or 2 asks for the database password
   (TablePlus rule). `pickSafeMode` in `internal/ui/workspace.go` applies the level directly today; add the gate there
   and in the web `safe_mode` handler, with a test.
2. **Connection URLs.** The `+ssh` scheme (`postgresql+ssh://…` carrying the tunnel host, user and key), and the
   `tLSMode` / `usePrivateKey` options, which today land in the generic options map (`internal/config/connections.go`).
3. **Welcome screen groups.** Edit and delete a group; deleting a group deletes its connections after a typed confirm.
   "Close other connections" (close every other workspace) from the workspace switcher.
4. **Editor.** Split pane down (editors stacked in one tab; M3 has split right only). Rectangular block selection as a
   keyboard mode beside the carets. Find next / find previous inside the editor (the grid already has `n` / `N`).
   Mongo shell method suggestions in autocomplete. A standalone SQL viewer tab with no connection, plus Open file and
   Open Recent (`[general] recent_files`).
5. **Results.** Charts with several y-axis columns (`-- barchart x: a, y: b, c`); `ChartSpec` in `internal/db/chart.go`
   holds one Y today.
6. **Sidebar and objects.** Create / drop a schema from the sidebar `+` menu. Custom item groups (user-defined folders
   of tables, stored in `state.json`). Create database with encoding and collation where the engine supports it.
   Export with table selection across several schemas in one run.
7. **Filters and cells.** A "SQL" popover on the filter bar showing the generated WHERE clause. "Save as…" for a
   binary cell from quick look (M1 names it; confirm it exists in both UIs). CSV quote mode "quote if needed" and a
   warning when delimiter and decimal are both `,`.

### M7 — Remaining TablePlus parity: admin tools

1. **Process list** on its own server connection, so it opens while the workspace is busy (needed for "Cancel opens
   the process list" in M4 item 11).
2. **Backup / restore.** Dump tool path or version selection (`pg_dump_path` etc. per connection), per-connection
   memory of the chosen options, restore into a new database created on the spot.
3. **User management.** Column-level privileges for PostgreSQL and MySQL (the MySQL `SHOW GRANTS` parser skips them
   today, see the `shortcut:` in `internal/db/mysql/users.go`).
4. **Metrics board.** Importer panel; "Refresh on Event" and submit-on-event wiring between panels (button → query).
5. **MCP.** Expose saved queries, per-connection history and metrics boards to clients as resources or tools, as
   TablePlus does; today only the six connection / query tools exist (`internal/mcp/mcp.go`).
6. **Assistant.** A default vendor plus per-chat provider switching (`[ai]` holds one provider today).
7. **Settings.** An Open Anything "Settings" entry that opens `config.toml` in `$EDITOR` (TUI) or a settings page (web).

### M8 — Last TablePlus conveniences

Third pass over the 67 docs pages against M1–M7 (2026-10-28). All small; after these, every documented TablePlus
feature is either built, in a milestone above, or in the dropped list below.

1. Welcome list **Sort By** tag / name / driver (`[general] connection_sort`, remembered in `state.json`).
2. **Connection details** popover from the status line: server version (the drivers already fetch it), host,
   database, user, TLS state; change the status colour from there.
3. **Sidebar multi-select** (`space` marks, as in the pickers) so delete, export and truncate act on several tables.
4. **Auto Pretty JSON** display setting (`[table] auto_pretty_json`) for the grid and row detail.
5. Editor: a key to open autocomplete suggestions on demand; `label:` and `tab:` as aliases of `-- name:` on result
   tabs; one result tab for a run of more than 100 statements.
6. A format picker inside the Variables dialog (today only `[editor] param_format`).
7. Metrics board **new / rename / delete** from the UI (boards are TOML files edited by hand today).
8. **Diagram** entry in a table's sidebar menu, opening the ER diagram focused on that table.
9. `webtuidb -mcp-config <client>` printing the client snippet for Claude Code, Codex, Cursor, VS Code (TablePlus's
   one-click setup).
10. Row detail (`space`) on a structure row to edit all of a column's attributes as a form; confirm it works in both UIs.

### M9 — Depth of existing features

Features both tools have, where TablePlus does more (checked in the terminal UI code, 2026-10-28; the web UI was not
compared at this depth and gets the same change in each item). Two of these are claimed as done in the M1 / M2 status
text above and are not in the code: row detail bulk edit and export file-name patterns.

1. **Result search.** `Grid.Find` is a plain text find. Add column choice (incl. Any column), operators Contains /
   Not contains / Has prefix / Has suffix, Match Case (M4 item 4), and Replace one / all inside an editable result.
2. **Row detail multi-row edit.** One field across every selected row, committed as one pending change per row.
3. **Structure editor data types.** A menu of the engine's types (`Session.DataTypes()` per driver; free text stays
   possible); keep the existing default-value menu.
4. **Export.** File-name patterns (`{connection}`, `{database}`, `{table}`, `{tag}`, `{date}`, `{time}`) with a
   `[export] filename` default, a CSV header-row toggle, and NULL handling (M4 item 11).
5. **Import.** A file-encoding option in the wizard, and "Import into this table…" in the table context menu (sidebar
   and grid), pre-selecting the destination.
6. **Beautify / uglify** the statement under the cursor when nothing is selected (`reformat` in
   `internal/ui/m2_query.go` uses the whole buffer today).
7. **History menu.** Insert into SQL editor, Add to Queries, Clear all history (Save As… and Import… are M4 item 6).
8. **Saved queries menu.** Edit / Rename…, Insert into SQL editor (Open file in tab is M4 item 6).
9. **Search in Database** across all schemas in one run, results grouped by schema (`search.Options.Schema` is one
   schema today).
10. **Editable results.** Replace the "any `(` or keyword anywhere disables editing" rule (`internal/ui/resultedit.go`)
    with a check on the statement's FROM clause: single table, no join / group / distinct, primary key in the select
    list. Keep the strict path as the fallback when parsing fails.
11. **Filter bar.** Clear all filters and Export filtered rows as actions on the bar (add-as-copy and remove exist;
    the two are confirmed absent).

### M10 — Web depth on shared features

Features the web already has, done with less than the terminal UI and TablePlus (read from `web/src`, 2026-10-28).
Separate from M4 item 1, which is about actions the web lacks entirely. Each item is the web catching up to the TUI;
where the TUI is also short, the item says so.

1. **Set Value** menu: add EMPTY and NOW() beside DEFAULT and NULL (`web/src/components/DataTab.tsx`); the date
   picker and "Add a file…" arrive with M4 item 5 in both UIs.
2. **Sidebar context menu**: Open in new tab, Open structure, Copy name, Script: TRUNCATE, Edit definition
   (`web/src/components/Sidebar.tsx` already has Open, View definition, Export, Import, Script CREATE / DROP / SELECT
   top 100, Clone, Rename, Truncate, Drop; Triggers is M4 item 1).
3. **Sidebar search** honouring `[sidebar] matching = fuzzy | contains` and the `^` / `$` anchors, as `matchName` does
   in the TUI.
4. **Quick look** (`web/src/components/CellEditor.tsx`): Raw / Tree toggle for JSON, PRETTY / MINIFY buttons, hex view
   for binary, save to file and load from file. TUI: add the Raw / Tree toggle and MINIFY inside the popover too
   (it pretty-prints on open and shows hex, nothing else).
5. **Result tabs**: `-- name:` labels and a pin button, as the TUI has (`web/src/components/QueryTab.tsx`).
6. **Console log pane** on the web: the server already keeps the log; show it with timestamps, the 1000-line cap and
   clear, behind M4's `toggle_console`.
7. **Grid cell range selection** (shift-click / shift-arrows rectangle) so copy cells and paste cells (M4 item 1) have
   something to act on; row detail follows the cursor cell.
8. **Connections page**: Duplicate, Copy as URL (without the password unless asked, as the TUI's `u` / `U`), and the
   form fields the TUI has that the web may lack: tag, colour, group, `password_cmd`, bootstrap (confirm against
   `web/src/routes/Connections.tsx` first).
9. **History and saved queries drawers**: per-item Run, Copy, Insert into editor, Add to Queries, Delete for history;
   Open, Run, Copy, Rename, Delete for saved queries (Clear history exists).
10. **Copy as** formats: the seven the TUI has (plain, JSON, HTML, Markdown, CSV, CSV with header, INSERT) if
    `COPY_FORMATS` in `web/src/utils/copy.ts` has fewer; confirm first.

### M11 — Editor completion and the last depth items

Found on the fifth pass (2026-10-28): the web editor's completion, and the last places where one UI does less than the
other or than TablePlus.

1. **Web SQL autocomplete.** `web/src/components/QueryTab.tsx` mounts CodeMirror's `sql()` with no schema, so only
   keywords complete. Add a completion source fed by the session's objects and columns (the sidebar already loads
   them), `schema.table` and `alias.column` after a qualifier, saved-query keywords from `GET /api/queries`, and the
   Mongo shell methods when `caps.Editor` is mongo; honour `[editor] complete_key`, `uppercase_keywords`,
   `close_braces` and the M4 item 3 toggles. Same candidate ranking as `queryTab.complete` in the TUI.
2. **Web import wizard**: all-text columns, matching strategy (name and order / name / order, case-insensitive),
   insert option (stop on error / update on duplicate / replace, per engine), quote character, per-column "do not
   import" and type choice when creating the table. Encoding is M9 item 5.
3. **Web Mongo syntax toggle** (shell JavaScript / command document) on the run bar, as the TUI has.
4. **Web Search in Database**: pause and resume on the running job (the job already reports progress).
5. **Web row detail**: inline edit of a field, quick values (EMPTY / NULL / DEFAULT / NOW()), PRETTY / MINIFY for
   JSON, and multi-row edit together with M9 item 2.
6. **Web filter bar**: add a filter as a copy of the current one (TUI `M-n`).
7. **TUI trigger editor**: a grid of name / timing / event beside the body editor, instead of only the CREATE TRIGGER
   script (`internal/ui/definitions.go`); views and functions stay as scripts.
8. **TUI row detail**: PRETTY / MINIFY on a JSON field (quick look pretty-prints on open; the pane shows the raw value).

### M12 — Web editing parity: structure, results, tabs

Sixth pass (2026-10-28). The terminal UI is level with TablePlus on all of these; the web is not. Items 1 and 2 are the
largest remaining gaps in the whole plan.

1. **Structure editing on the web.** `Structure` in `web/src/components/DataTab.tsx` renders columns, indexes and
   triggers as read-only tables. Make it the editor the TUI has: add / rename / delete columns, type, nullability,
   default (menu + free text), primary key, foreign keys with referenced table / columns and ON UPDATE / ON DELETE,
   indexes (name, unique, columns, algorithm, condition), new table, rename table; all as pending changes through the
   same `POST /sessions/{sid}/plan` + commit / preview / discard path as data edits, so safe mode applies.
2. **Editable query results on the web.** A single-table `SELECT` whose select list includes the whole primary key is
   editable (the server already decides this for the TUI, `internal/ui/resultedit.go` rule, relaxed by M9 item 10):
   inline edit, add / duplicate / delete rows, Set Value, commit / preview / discard with undo, on the results grid.
3. **Tab reuse rule.** `openObj` in `web/src/routes/Workspace.tsx` adds a tab unless the same object is open. Follow the
   TUI / TablePlus rule: replace the current table tab when it has no pending changes; keep tabs with changes and
   editors; sidebar "Open in new tab" (M10 item 2) always adds.
4. **Resizable columns** with widths saved to the per-table `widths` the TUI already keeps in `state.json`.
5. **Restore open tabs on reload** (per session, in `sessionStorage`; editor text included), and reopen the last
   workspaces on start when `[general] reopen_workspaces` is set, as the TUI does.
6. **"Taking too long" prompt** after `query_timeout_secs`: keep waiting or cancel, as the TUI's picker.
7. **Redis key folders** by `:` and **saved-query folders** in the web sidebar and queries drawer.
8. **Help order.** The TUI help sorts bindings by description within a context; the web lists them in table order.
   Pick one (table order, since `webtuidb -keys` prints it) and apply it to both so the two listings are identical.

### M13 — Milestone 3 depth

Seventh pass (2026-10-28), after M3 landed: where the M3 engines do less than the M3 text above claims or than the
docs describe. Both UIs in one pass, as always.

1. **ER diagram.** Add a foreign key (pick table, columns, ON UPDATE / ON DELETE; a form in the TUI, drag in the web
   per M4 item 12), delete a table, switch schema, copy a column name. `internal/diagram` + `internal/ui/m3.go`
   have scroll / sort / filter / edit / new table only.
2. **Metrics board panels.** Add `exporter` (CSV / JSON / SQL / Excel of a panel's query), `input`, `dropdown`
   (fixed list or query-loaded) and `button` kinds to `board.Panel`, so `$var` values can come from a panel and a
   button re-runs the panels wired to it (event wiring itself is M7 item 4). `Kind` today: number, bar, line, table.
3. **User management.** MySQL resource limits (`MAX_QUERIES_PER_HOUR`, `MAX_UPDATES_PER_HOUR`,
   `MAX_CONNECTIONS_PER_HOUR`, `MAX_USER_CONNECTIONS`) on `db.User`; an attribute toggle entry in the TUI menu
   (SUPERUSER / CREATEDB / CREATEROLE / LOGIN / REPLICATION / BYPASSRLS; the `attrs` change kind exists); check all /
   uncheck all on privilege lists.
4. **Backup / restore.** Free-form extra tool options (typed, plus a picker of the common ones per tool), file-name
   patterns shared with M9 item 4, and live tool output in the dialog with cancel (confirm what `internal/backup`
   streams today). Tool path, remembered options and restore into a new database are M7 item 2.
5. **Assistant.** New chat, Clear chat, and a chat history list with search (stored under
   `~/.local/share/webtuidb/chats/<connection>/`), in both UIs; a Stop key in the TUI if `running` cannot be cancelled
   today. Message copy / resend / delete are M4 item 11.
6. **Plugins** may declare `key = "…"` in their TOML, bound through the keymap like any action (and listed in help).
7. **Split panes**: a close-pane action (`close_pane`, prefix `alt+w`); split down is M6 item 4.
8. **Vim mode**: visual mode, counts and the `:w` / `:q` family; folds keyed by statement text rather than index so an
   edit above a fold keeps it.
9. **Carets.** The terminal has one cursor, so `select_next_occurrence` jumps; the web adds a caret. Say so in the
   help text of both so the difference is documented, not discovered.

### M14 — Terminal / web parity: verify and fix

Runs after M13. The goal is one feature set in both UIs, checked by tests that keep it that way, not by reading.
The matrix below is what the first pass (2026-10-28) found; each later pass re-runs the same checks until every row is
empty.

**Checks (make them `make parity`, run in CI):**
1. **Actions.** Every action in `internal/keymap/action.go` is in `SUPPORTED` in `web/src/routes/Workspace.tsx` and
   has a handler in both UIs. A Go test parses `SUPPORTED` out of `Workspace.tsx` and fails on any keymap action that
   is missing, and on any `SUPPORTED` entry that is not a keymap action. Found: 28 missing (M4 item 1) and the four
   `focus_*` moves handled but not listed, so the web help greys them out.
2. **Help listings identical.** `webtuidb -keys` output and `GET /api/meta` bindings carry the same action names,
   descriptions and order; the TUI help and the web help dialog render that list without re-sorting (M12 item 8).
   Test: render both to text and diff.
3. **Config keys.** Every key in `internal/config/config.go` that changes UI behaviour is honoured by both UIs (the
   server applies what it can, the web reads the rest from `/api/meta`). Found applied only in the terminal:
   `[table] filter_default_column`, `filter_default_operator`, `filter_state`, `default_sort`, `alternating_rows`;
   `[general] new_tab_opens`, `estimate_count_over`, `reopen_workspaces`; `[editor] highlight_current`,
   `close_braces`, `complete_key`, `autocomplete`, `auto_save_queries`; `[sidebar] matching`. Test: a table of key →
   {tui, web} in `internal/config` that each UI's test asserts against.
4. **Context menus.** The sidebar, grid, result and filter-bar menus list the same entries in both UIs, in the same
   order, with the same labels. Found: the web lacks Open in new tab, Open structure, Show DDL, Copy name, Script:
   TRUNCATE, Edit definition, Triggers, Pin / unpin, Delete on non-table objects (M10 item 2, M4 item 1); the terminal
   lacks Export… and Import… on an object; the terminal's New table dialog has engine options (collation, encoding,
   replication) that the web's New prompt does not. Test: both UIs build menus from one list in `internal/ui/menus.go`
   exported through `/api/meta`.
5. **Welcome screen.** Same actions: new, edit, duplicate, delete, test, import URL, copy URL with and without the
   password, sort (M8 item 1), export / import file (M4 item 7). Found: the web lacks duplicate and copy URL (M10 item 8).
6. **Dialogs and settings.** Every dialog the terminal opens for an action (Set Value entries, quick filter modes,
   page settings, count, clone with content, truncate options, drop with typed name, safe-mode gate with statements
   and password, "taking too long", bootstrap failure) exists on the web with the same options, and the other way
   round. Found so far in M10–M13; re-check after they land.
7. **Engine capability gating.** Both UIs hide the same features for the same `Caps` (indexes, triggers, functions,
   process list, users, schemas, document views). Test: a table-driven test over the fake session with each cap off.

**Fixes this milestone owns** (everything not already in M4–M13): the four `focus_*` entries in `SUPPORTED`; the nine
config keys above that no earlier milestone covers; Export… / Import… on an object in the terminal sidebar menu;
engine options on the web's New table; and whatever the checks above turn up after M10–M13 land.

### M15 — Everything else from TablePlus, final

A last full walk over all 67 docs pages, bullet by bullet, against M1–M14 (2026-10-28), plus the behaviours TablePlus
has that its docs only imply, plus the dropped list re-read for what a terminal or a browser can in fact do. After M15
the only TablePlus items not in this plan are licensing / MDM, iOS, Touch ID, speech and crash telemetry. Both UIs in
one pass, as always; a web-only or TUI-only item says so.

**A. Leftover documented items (small, each one sitting or less)**
1. Copy Selected Cells As… with the seven copy formats (`copy_cells_as`, prefix `alt+C`); `copy_cells` is plain text only.
2. Copy All Column Values on the web (the TUI has `Grid.CopyColumn`).
3. Copy-as on the structure, index and process-list grids (TablePlus copies column definitions and processes as CSV / JSON / Markdown).
4. Select Word (editor, `select_word`) and Select All in both editors; Use Selection for Find and Jump to Selection (M4 item 11) ride along.
5. Column header menu: Sort ascending / descending / Remove sort, Sort multiple columns (M4 item 5), Filter with column, Hide this column, Show all columns — one list for the TUI header picker and the web right-click menu.
6. Header tooltip / detail line with type, nullable, default and key for a column (web `title=`; TUI in the row detail header).
7. Sidebar `+` menu creates view, materialized view, function / procedure, schema (M6 item 6) and item group (M6 item 6) on both UIs; the web's New makes a table only.
8. Database picker: Open in current tab vs Open in new workspace, and Copy database name, on both UIs (the TUI has the open choice, the web neither).
9. Open Anything: open the hit in a new tab (`alt+enter`), Tab completes the keyword with the selected name.
10. Double-click behaviours on the web: a sidebar item opens in a new tab, below the last row adds a row, below the last column adds a column, the empty tab-bar area opens a new tab; middle-click or `ctrl+click` a cell opens quick look.
11. Tab to move to the next cell while editing, Esc deselects all rows (both UIs).
12. Process list: multi-select kill, "Dismiss" closes, sort by header (M4 item 11).
13. Metrics board Status (last refresh time and per-panel errors in the toolbar).
14. ER diagram: custom table colour override on top of the FK-cluster colour (web canvas, M4 item 12; TUI as a box style).
15. Console log: find in console, copy a line, filter errors only.
16. Notification / jobs centre in both UIs: one list of background jobs (import, export, backup, restore, search) with progress, stop, and open-folder (TUI: reveal path via `open` / `xdg-open`; web: download link). TablePlus's Notifications popover.
17. Finish signals: terminal bell plus OSC 9 / OSC 777 desktop notification when a job ends while the TUI is unfocused; web `Notification` API (M4 item 11).
18. Reveal in file manager for saved queries, exports and backups (`open` / `xdg-open` / `explorer`), the "Show in Finder" items.
19. Web deep links: `/s/<session>/t/<schema>/<table>?filter=…` and `/s/<session>/q/<saved query>`, the browser form of TablePlus's table-opening URL options (M4 item 7 covers the CLI URL).
20. Opening a second browser tab keeps the token (carry the fragment into every in-app link) so "Move tab to new window" becomes "Open workspace in a new browser tab".
21. Reconnect by clicking the status bar (web; TUI with mouse clicks in M5), and the connection details popover there (M8 item 2).
22. Click a tab to activate, drag to reorder, close button on hover (web); TUI tab reorder with `alt+shift+[` / `]`.
23. Drag and drop on the web: a CSV / JSON / SQL file onto the grid or sidebar opens the import wizard on that table; drag queries between folders; drag connections between groups; drag columns to reorder where `CanReorderColumns`.
24. "Zen" toggle: hide sidebar, console, row detail and status in one key (`toggle_panels`), the terminal and browser form of Enter Full Screen.
25. Binary cells: hex / base64 / text display toggle in quick look, in addition to hex edit (M4 item 5) and save-as (M6 item 7).
26. NULL / EMPTY / DEFAULT rendering identical in both grids (dim italic `NULL`, `''` for empty), and the same marks for new / modified / deleted rows (the theme keys exist; confirm the web uses all of them).
27. Theme keys parity with TablePlus's list: editor keyword / string / comment / number / query background / selection / error; data table new / modified / deleted / selected / grid / cell selection; application window / sidebar / tabs / notifications; tag colours. Any key TablePlus has and `[theme.colors]` lacks is added, in both UIs.
28. Auto-hide scrollbars on the web (`[web] auto_hide_scrollbars`), TablePlus's Table setting.
29. Connection list: last-connected time per connection, "Recent" ordering as a sort choice beside tag / name / driver (M8 item 1).
30. Settings entry in Open Anything (M8 item 7) becomes an in-app settings editor: a TUI form and a web settings page over every `config.toml` key with validation messages, including a keymap editor that writes `[keys]` with conflict detection (TablePlus's Settings and Keymap panes).
31. `webtuidb -version`, `-changelog` (opens the GitHub release notes), `-bugreport` (prints version, OS, terminal, drivers in use, config with secrets masked, for an issue), opt-in `-check-update` against GitHub releases. TablePlus's About / Software Update / Report bug, without telemetry.
32. Local crash log: panics written to `~/.local/share/webtuidb/crash.log` with the last 20 console lines, never sent anywhere. TablePlus's crash reports, kept local.
33. Help menu equivalents: `?` lists bindings (exists); add "Open docs" (README / GitHub) and "Enable SSH log" (M4 item 11) to Open Anything.

**B. Previously dropped, now feasible (optional, each behind a setting)**
34. Image preview for binary cells whose bytes carry an image signature (web: `<img>` from a blob; TUI: dimensions and type only). PDF: web opens the blob in the browser viewer.
35. Spatial values: PostGIS / MySQL geometry and GeoJSON shown as WKT text in both grids (small EWKB decoder in `internal/db`), and drawn on an SVG canvas on the web (`Map` view in quick look). TablePlus's Map view and "convert PostGIS geometry to WKT".
36. Keychain-style password storage: `password_cmd` presets for macOS Keychain (`security find-generic-password`), Linux `secret-tool`, `pass`, 1Password `op`, offered as choices in the connection form so nobody types the command.
37. Passcode lock: `[security] passcode` (bcrypt hash) with `lock_after_mins`; the TUI and the web ask for it at start and after idle. TablePlus's Passcode without Touch ID.
38. TablePlus-style JavaScript plugins: `goja` is already vendored for the Mongo shell; expose a small API (`context`, `query`, `copy`, `open`, `notify`) so `.js` plugins work beside the TOML command plugins (M3), with the same context menus and `key = …` shortcuts (M13 item 6).
39. Themes from git: `webtuidb -theme-install <git url>[@ref]` clones into `~/.config/webtuidb/themes/` and `-theme-update` pulls; themes there show in the picker (M4 item 9).
40. Toolbar / status hints customisation: `[web] toolbar = [...]` picks and orders the web toolbar buttons; `[general] hints = [...]` picks the TUI status-line hints. TablePlus's Customize Toolbar.
41. Open in Window: web quick look and SQL preview open in a separate browser window (`window.open`) when asked.
42. DBngin: a docs section on local servers (DBngin, Docker, Homebrew) with import URLs, nothing in code.

**C. Interaction parity the docs imply**
43. Mouse in the TUI (M5): click to focus a pane, select a tab, pick a sidebar item, place the grid cursor; click a header to sort, drag a header edge to resize (saved to `widths`); double-click a cell to edit.
44. Keyboard on the web: every grid, dialog and picker usable without the mouse (Tab order, Enter / Esc, arrows), focus rings, `aria-` labels on toolbars and menus, `prefers-reduced-motion` honoured.
45. Context menus on the web mirror the TUI pickers one for one (M14 check 4) and open on right-click, `shift+F10` and the menu key.
46. Production safety cue: tag colour on the tab bar / frame (TUI) and page accent plus a thin top bar (web) so a production workspace is obvious, as TablePlus colours the window. The web has the accent; the TUI colours the status line only.

**D. Done means**
- Every row of the M14 checks is empty and `make parity` passes in CI.
- Each A/B/C item is either implemented in both UIs or marked "web only" / "TUI only" in the help text with a reason.
- The README feature list, the `-keys` output and the two help screens agree, and the status header above lists no "claimed but missing" item.

Dropped (no terminal equivalent / not applicable): licensing and MDM, iOS, Touch ID, speech, crash telemetry (a local crash log is M15 item 32), native menu bar, Windows .NET notes. Everything else once listed here (Keychain, passcode, toolbar customisation, themes from git, DBngin, map / image / PDF preview) moved to M15 section B as optional items. Excel import stays out (CSV / JSON / SQL only; Excel export is supported).

## Verification

- `go build ./... && go vet ./... && go test ./...` — unit tests for keymap, config, splitter, classifier, change planner, filter builder, sqlfmt, CSV mapping.
- `docker compose up -d` (Docker Desktop must be running; it is installed but the daemon is currently stopped) then `make smoke`: integration test per driver (connect, list objects, browse, plan+apply an edit, run a query with cancel, process list).
- Manual TUI check per milestone in the app's terminal: `webtuidb` → create the 5 seeded connections → browse/edit/commit a row in each → run multi-statement query → safe-mode dialog appears on `alert2` with an UPDATE → rebind `commit = "x"` in config.toml and confirm `C-d x` commits and `C-d ?` shows the new key.
- Record a `vhs`/`asciinema`-free check: drive the binary in `tmux` and capture panes for the autonomous smoke of the TUI itself.
