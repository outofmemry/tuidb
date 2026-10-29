# Super test — terminal UI (TUI)

- Date: 2026-10-29
- Build: `051da0e` plus uncommitted changes present (binary built from the working tree, `scratchpad/devhome/webtuidb`)
- Terminal: tmux private server (`tmux -L wt -f /dev/null`), TERM=xterm-256color, 180x50 (small-terminal checks at 80x24)
- Config home: scratchpad `tuihome/cfg`, data home `tuihome/data`; PostgreSQL 16 / MySQL / MariaDB / Redis / Mongo containers

Severity: P1 = data loss/corruption, security, crash/panic/hang, or feature broken for everyone. P2 = edge case,
misleading feedback, parity gap, layout break, performance. P3 = UX friction.

## Findings

Totals: **4 P1**, **13 P2**, **22 P3** (UX friction). Test objects created during the run (`shop.tui_t1`,
clones, MariaDB `tui_r`/`tui_imp`) were dropped; rows edited in `shop.customers`/`shop.no_pk` were restored;
the TUI config home was reset. P1s are listed after the first two P2s because the file was written as testing went.

### T-P2-01 Table browse pages with LIMIT/OFFSET and no ORDER BY: an edited row jumps to another page
`internal/db/filter.go:232-266` (`BuildSelect` adds `ORDER BY` only when the user sorted)

Steps: open `shop.customers` (340 rows, PK `id`), edit `notes` of row id 11, `C-d s`. The grid reloads.
Expected: rows stay where they were; paging is stable (TablePlus pages a table in key order; the plan's grid is
"virtualised rows … paging 300").
Actual: id 11 disappears from page 1 and shows up as the last row of page 2, because PostgreSQL returns the updated
tuple at the end of the heap:
```
│ 340 │  11│Customer 11 │c11@example.com │Lagos    │false│147.07   │multi⏎line→note …
```
Page 1 now ends at id 301. With LIMIT/OFFSET over an unordered scan, a row updated by anyone between two page loads
can be shown twice or skipped. Fix: when no sort is chosen, order by the primary key (or ctid / all columns as a
last resort) so pages are deterministic. Reproduced twice.

### T-P2-02 `webtuidb --mcp` exits on one malformed JSON line and drops the answers still in flight
`main.go` (`--mcp` → `internal/mcp` stdio transport)

Steps: pipe `initialize`, `notifications/initialized`, `tools/list`, `tools/call list_connections`, then the line
`not json` into `webtuidb --mcp`.
Expected: JSON-RPC 2.0 answers a parse error (`-32700`) and keeps serving.
Actual: `webtuidb: invalid character 'o' in literal null (expecting 'u')`, exit 1, and **none** of the four valid
requests got an answer (without the bad line all four are answered). Same with a truncated object
(`{"jsonrpc":"2.0","id":9,"method":"tools/list"` + newline): `invalid character '{' after object key:value pair`,
exit 1. One buggy client message kills the server for the session. Reproduced twice.

### T-P1-01 Follow foreign key opens the referenced table unfiltered, while the filter bar claims `id = N`
`internal/ui/filterbar.go:395-399` (`followFK` sets `nt.filters` but never marks them applied; `load()` reads
`t.applied()`, `tab_table.go:298`)

Steps: open `shop.orders`, put the cursor on `customer_id` of the row with value 184, `C-d o`.
Expected (M1): "FK follow" opens the row the key points to (`follow_fk` = "open the row a foreign key points to").
Actual: a new `customers` tab opens with the filter bar showing the filter, but the grid holds every row:
```
│    ✓   id ▾   = ▾  184
│     │⚷id │name        │email  …
│   1 │   1│Customer 1  │c1@example.com …
 rows 1–300 of 340  ·  1 filter(s)
```
The cursor sits on customer 1, so a user who edits "the referenced row" edits the wrong one. Pressing Enter in the
bar applies it. Reproduced twice (values 182 and 184). Fix: `nt.markApplied()` (or set `appliedSet`) before
`addTab`, as the quick filter path does (quick filter `C-d x` applies correctly).

### T-P2-03 The collapsed filter summary shows the edited filter, not the one the rows come from
`internal/ui/filterbar.go` (summary line / `saveState` on Esc)

Steps: customers, apply `notes Contains 'apos` (24 rows), `C-d f`, change the value to `zzz` (no Enter), `Esc`.
Expected: the summary describes the filter that produced the rows, or marks the bar as "not applied".
Actual: `Filters   notes = zzz     C-d f  edit` above 24 rows that match `Contains 'apos` (the real count for
`notes = 'zzz'` is 0); the status line says `rows 1–24 of 24 · 1 filter(s)`. The same mismatch exists in the
Filter SQL popover (`M-s`), which prints the unapplied WHERE. `filter_export` (`M-e`) deliberately exports the bar's
filters, so it exports a different row set from the one on screen. Reproduced twice.

### T-P1-02 Accepting a completion after `schema.` or `alias.` deletes the qualifier
`internal/ui/editor.go:528-549` (`acceptComplete` replaces from the start of `prefixAtCursor`, which includes
`qual.`) with the bare candidate from `internal/ui/tab_query.go:121-140`

Steps: SQL editor on "shop (postgres)" (sidebar on schema `shop`), type `select count(*) from shop.cus`, the popup
offers `customers  table`, press Tab (or Enter). Same with `select c.id from shop.customers c where c.na` → Tab.
Expected (M11/M1): `schema.table` and `alias.column` completion after a qualifier.
Actual: the qualifier is removed:
```
before: select count(*) from shop.cus           after Tab: select count(*) from customers
before: … shop.customers c where c.na           after Tab: … shop.customers c where name
```
The unqualified name then resolves through the session `search_path`, which is still `public` (see T-P2-04): the
statement `select count(*) from orders` produced by completing `shop.orders` returned **3** (public.orders, the e2e
table) instead of 1500. For an UPDATE/DELETE typed the same way this edits a different table than the one the user
typed. With joins the dropped alias makes the column ambiguous. Reproduced three times (schema, schema, alias).

### T-P2-04 Switching the sidebar schema does not change the SQL editor's schema
`internal/ui/sidebar.go` (schema switch `s`), no `search_path` handling outside `postgres.go:381`

Steps: connect "shop (postgres)" (opens `webtuidb.public`), `s` → `shop`. Status line shows `webtuidb.shop`.
`C-d e`, run `select count(*) from orders;`.
Expected: the editor works in the schema the workspace shows (TablePlus sets the search path when you switch
schema), or the status line says which schema unqualified names use.
Actual: `count 3` — the query ran against `public.orders`, while every visible cue says `shop`. Reproduced twice.

### T-P1-03 `BEGIN` in an unpinned editor tab succeeds silently; the next UPDATE autocommits and ROLLBACK cannot undo it
`internal/ui/tab_query.go` / `m5_query.go:171` (pinning exists but is opt-in; nothing detects transaction verbs)

Steps: SQL tab (not pinned). Run `begin;` (`C-d Enter`) → `✓ BEGIN`. Run
`update shop.no_pk set b='T' where a=2;` → `✓ 1 row affected`. Run `rollback;`.
Expected: either the tab keeps the connection for the transaction (M5 "a session-pinned connection per query tab
so BEGIN / SET … carry across statements and a transaction can be rolled back"), or BEGIN is refused / answered with
"pin the connection first (Open anything → Pin connection)".
Actual: `✓ ROLLBACK · 1.9ms  WARNING: there is no transaction in progress`, and the row stays `2|T` in the
database. A user who typed BEGIN to be safe gets an autocommitted change. Reproduced twice (the same pattern with
`begin; update …;` run together with `C-d M-Enter` does keep the transaction because it runs on one connection).

### T-P2-05 Closing a pinned tab with an open transaction rolls it back without asking
`internal/ui/m5_query.go:171-215` (pin), tab close path in `workspace.go`

Steps: `C-d e`, Open anything → "Pin connection to this tab", run `begin; update shop.no_pk set b='T' where a=2;`
(`C-d M-Enter`, `✓ 1 row affected`), then `C-d w`.
Expected: the same "unsaved changes" confirmation the grid gives (`⚠ Close … with unsaved changes?`), since the
transaction holds uncommitted work; the tab title or status line should also show "in transaction".
Actual: the tab closes at once, the transaction is rolled back (`2|b` in the database), no message. Nothing on
screen indicated an open transaction except `[pinned]`. Reproduced twice.

### T-P1-04 "Taking too long" prompt outlives the query; choosing Cancel reports "cancelled" for a statement that already committed
`internal/ui/tab_query.go:413-426` (prompt), `:397-410` (`Cancel` bumps `t.batch`, so the finished result is dropped)

Steps: `[general] query_timeout_secs = 2`. In a SQL tab run
`update shop.tui_t1 set state = 'S1' where id = 1 and pg_sleep(3) is not null;`. The prompt
`The server is taking too long (over 2s)` appears. Wait 6 s (the statement has finished), pick "Cancel the query".
Expected: the prompt closes by itself when the statement finishes and the result is shown; a Cancel after the fact
must not claim anything was cancelled.
Actual: while the prompt is open the run bar keeps counting (`⏳ running 7.64s` for a 3 s statement) and no result
is shown. Picking Cancel shows `⚠ cancelled` and `✓ query cancelled`, but the database has `1|S1`: the UPDATE was
committed. The user is told their change did not happen. Picking "Keep waiting" shows the result with a total
time that includes the time the prompt was open (`✓ 1 statement · 10.55s`, `1 row · 6.01s`). Reproduced twice
(SELECT and UPDATE).

### T-P2-06 Rebinding a key in `[keys]` silently removes another action, and the frame hints keep the old key
`internal/ui/tab_table.go:1285-1293` (hard-coded `pre+" i"`, `pre+" s"`, `pre+" f"`), keymap build for config overrides

Steps: `config.toml`: `[keys] commit = "x"` (the plan's own verification step). Start, open a table, edit a cell.
Expected (Verification): "rebind `commit = "x"` … confirm `C-d x` commits and `C-d ?` shows the new key"; an action
that loses its key is reported (the settings dialog refuses a steal, `keymap.TakenFrom`).
Actual: `C-d x` commits and `--keys`/help/which-key show `commit x`, but:
1. `quick_filter` (grid, `x`) is gone from `--keys`, from the help (`/ quick filter` finds nothing) and from the
   grid; no warning at start or in `--keys`. The settings dialog would have refused the same edit.
2. The table frame still says `C-d s  commit` (`… C-d i  add row   C-d s  commit   C-d f  filter`) while the status
   line says `C-d x  commit`. The structure view hint and the welcome "Quick start" panel are hard-coded the same
   way. Reproduced twice.

### T-P2-07 `webtuidb --keys` omits the six console bindings that `/api/meta` and the help carry
`internal/keymap/listing.go:13` (context list has no `Console`)

Steps: `webtuidb --keys` vs `GET /api/meta` (`keymap.bindings`).
Expected (M14 check 2, CLAUDE.md): same action names, descriptions and order.
Actual: the web lists 183 bindings, `--keys` 177; missing `console_find`, `console_next`, `console_prev`,
`console_copy`, `console_errors`, `console_menu` (the TUI help shows them only while the console has focus).
Everything else matches in names, descriptions and order (checked by script).

### T-P2-08 SQL export "Include structure" turns serial ids into IDENTITY and never advances it; the restored table rejects the next insert
`internal/db/postgres/plan.go:163-178` (`columnSQL`: serial → `GENERATED BY DEFAULT AS IDENTITY`), SQL exporter (no
`setval` / `ALTER … RESTART` after the data)

Steps: `shop.orders` (`id` is `serial`: `nextval('shop.orders_id_seq')`), `C-d E`, Format `sql`, Include structure,
Drop table first, File `exp2.sql`. Restore the file into a scratch schema with psql, then
`insert into tui_rs.orders (customer_id) values (1)`.
Expected: an export that can be loaded and used (pg_dump emits `setval`; TablePlus's SQL export keeps the default).
Actual: the file has `"id" integer GENERATED BY DEFAULT AS IDENTITY NOT NULL` followed by 1500 INSERTs with explicit
ids and no `setval`/`RESTART`, so the restored identity starts at 1:
`ERROR: duplicate key value violates unique constraint "orders_pkey" DETAIL: Key (id)=(1) already exists.`
The DDL view (`C-d M-d`) and "Copy script: CREATE" show the same IDENTITY clause for a serial column, so the DDL
shown is not the table's definition. (Clone with content is fine: the clone's next id was 3.) Reproduced once on
`orders`, and the DDL view/script output twice (`wide`, `orders`).

### T-P2-09 Search in database searches the session's schema, not the one the sidebar shows; hits open under the wrong label
`internal/db/search.go:42-45` (`schema = sess.CurrentSchema()`), `internal/ui/m2.go:497-503` (the total comes from
the sidebar's schema)

Steps: "shop (postgres)", sidebar switched to `shop` (`s` → shop). `C-d /`, Value `Paris` (42 customers in
`shop.customers` have city Paris; `北京` likewise), Enter.
Expected: the tables of the schema on screen are searched (the dialog even counts them: "/9").
Actual: `Search "Paris": 4/9 tables · 0 with hits · done` in 12 ms: only the 4 tables of `public` were searched.
Searching `Ann Lee` (a row of `public.customers`) reports `customers 1 rows`; Enter opens `public.customers` while the
status line reads `webtuidb.shop ▸ customers`. Same root cause as T-P2-04 (the schema switch is a sidebar-only
state). Reproduced three times.

### T-P2-10 The Variables dialog says "Enter run" but only saves; with the default config the statement is sent with `:name` unbound
`internal/ui/m2_query.go:89` (`f.submit = "Enter: run"`), `:110` (`editVars` → "variables saved"),
`tab_query.go:305` (binding only when `[editor] query_params = true`, default false)

Steps: SQL tab, `select id, name, city from shop.customers where city = :city and id < :maxid;`, `C-d M-v`.
The dialog lists `city` and `maxid` (Format ‹ colon ›) with the footer `Enter  run`. Type values, Enter.
Expected: the statement runs with the values (M2 "Query parameters … Variables dialog, remembered values"), or the
dialog says parameters are disabled and how to enable them.
Actual: toast `✓ variables saved`; nothing runs (the result pane still shows the previous error, which looks like
the run failed). `C-d Enter` sends the text unchanged: `ERROR: syntax error at or near ":"`. No hint anywhere that
`[editor] query_params` must be turned on. Reproduced three times.

### T-P2-11 A tab opened from a `.sql` file shows no unsaved mark and closes / quits without asking; the edit is lost
`internal/ui/tab_query.go` (no dirty state for file-backed editor tabs), quit path in `app.go`

Steps: `C-d O`, open `q1.sql`, append ` -- edited` to line 2. Then `C-d w` (or `C-d q`).
Expected: a file tab with changes is marked (●) and closing/quitting asks to save, like a grid with pending edits
(`⚠ Close … with unsaved changes?`, `⚠ Quit with uncommitted changes?`), or the edit is written back.
Actual: the tab title stays `≡ q1.sql`; `C-d w` closes it at once and `C-d q` exits (`EXIT=0`) without a prompt;
`q1.sql` on disk is unchanged. Vim `:q` likewise closes a modified SQL tab (no `:q!` distinction). Reproduced twice
(close and quit).

### T-P2-12 "Copied" toasts are shown even where nothing reaches the clipboard
`internal/ui/m5.go:336-347` (`setClip` falls back to `tea.SetClipboard`, i.e. OSC 52, and `copyText` always says
"copied")

Steps: run the TUI in a terminal without OSC 52 (macOS Terminal.app, or tmux with `set-clipboard off`, the tmux
default for a detached server), `C-d c` on a grid row, or `u` on the welcome list.
Expected: the clipboard holds the text, or the app says it could not copy (TablePlus uses the system clipboard).
Actual: `✓ copied rows` / `✓ copied shop (postgres) as URL…`, while `pbpaste` still returns the previous
clipboard. With `set-clipboard on` the same keys did fill the tmux buffer, so the data is right; only local
non-OSC-52 terminals get nothing. There is no `pbcopy`/`wl-copy`/`xclip` fallback. Reproduced twice.

### T-P2-13 Schema diff keeps foreign keys pointing at the TO schema
`internal/…` schema diff generator (Open anything → Schema diff)

Steps: Schema diff, FROM `public`, TO `shop` (this connection).
Expected: a migration that makes FROM look like TO, with references rewritten to FROM (`public.orders`).
Actual: `CREATE TABLE "public"."order_items" (… CONSTRAINT "order_items_order_id_fkey" FOREIGN KEY ("order_id")
REFERENCES "shop"."orders" ("id") …)`: the new table in `public` references `shop.orders` and `shop.products`.
Serial columns also come out as `GENERATED BY DEFAULT AS IDENTITY` (see T-P2-08). Seen once (output read, not run).

## UX friction (P3)

- T-P3-01 Refusals use the success icon: `✓ the assistant needs an API key: set api_key …`, `✓ no passcode is set:
  run webtuidb -set-passcode …`, `✓ filter the rows first: a bulk update changes every row the filter matches`
  (`a.say` used where `a.fail` / `✗` is meant; other refusals such as `✗ a saved query needs a name` are right).
- T-P3-02 Statements in error/confirm dialogs are printed with Go `%q` escaping:
  `apply "INSERT INTO \"shop\".\"tui_t1\" (\"id\") VALUES ('1')": ERROR …`, and the drop-table confirm says
  `type "wide \"clone\" ü" to confirm` — a user may type the backslashes.
- T-P3-03 Dialog text is hard-wrapped mid-word/mid-number: `every row identical to an edi|ted`, `Error 10|45`,
  `SQLSTATE 2351|4`.
- T-P3-04 Several dialogs close on a validation error and drop what was typed: page settings (`limit must be 1 or
  more`), Add to saved queries (`a saved query needs a name` loses keywords/folder), export connections
  (`include passwords needs a passphrase`), import (`processed`/failed import forgets file, NULL-as and options).
  Keys typed next go to the pane below (on the welcome list `s`, `e`, `c` are commands).
- T-P3-05 Cancel/Esc on the connection form (new and edit) discards all edits without asking.
- T-P3-06 Long connection names: the list truncates `maria ü 北京 a-very-long-…mariadb` with no gap before the
  driver column; a duplicate (`… copy`) is indistinguishable from the original, also in the delete confirm title;
  at 80 columns the host loses its port (`127.0.0.1:533…`).
- T-P3-07 Only the first sort column shows ▲/▼ after "Sort by several columns"; nothing marks the others or their
  order (`grid.go:766`).
- T-P3-08 The preview popover drops tab characters (`multi⏎line→note` previews as `linenote`), so a correct UPDATE
  looks wrong.
- T-P3-09 The console logs `-- browse shop.customers limit 300 offset 0` and `-- search 北京` instead of the SQL that
  ran, so it cannot be copied to reproduce a page.
- T-P3-10 Query errors show no position (PostgreSQL returns one): `ERROR: relation "nope" does not exist` in a
  multi-line script gives no line/column.
- T-P3-11 `Ctrl+C` and `Ctrl+Z` are silently ignored and the lock screen offers no way to quit; a new user gets no
  hint that quitting is `C-d q`.
- T-P3-12 Sidebar tabs (Objects/Queries/History) switch with `1`/`2`/`3`, which the header does not show; `Tab` does
  nothing there.
- T-P3-13 Pickers differ: some filter as you type (operator, Open anything), others ignore typing (copy-as, set
  value, column menu) and `j`/`k` do not move in any of them.
- T-P3-14 Status/frame hints are generic: a read-only view says `↵ edit … C-d s commit`; the assistant tab shows the
  editor's `C-d Enter run`; the view's tab uses the table icon `▦`.
- T-P3-15 Filter bar: `Enter` on the column/operator field opens the picker again instead of applying (the hint
  says `↵ apply`); Raw SQL keeps showing an irrelevant operator (`Raw SQL ▾ > ▾ …`); the focused field is not
  visible without colour.
- T-P3-16 Settings accept a negative `query_timeout_secs` (`-5` is saved and silently disables the prompt);
  `[theme.colors]` accepts unknown keys and non-colours (`accent = "notacolor"`) with no warning.
- T-P3-17 The backup form is filled in before `✗ pg_dump not found (on PATH or at its configured path)`; checking
  the tool first would save the typing. Hex edit errors are raw Go text (`encoding/hex: invalid byte: U+007A 'z'`).
- T-P3-18 `--check-update` with no published releases prints `webtuidb: check-update: 404 Not Found` (exit 1).
- T-P3-19 The JSON copy format sorts keys alphabetically (`"city"` before `"name"`), and INSERT copy from a
  single-table query names the table `"result"`.
- T-P3-20 The landing page of a workspace whose object list failed (Mongo without auth) says `test · 0 objects`
  while the sidebar says `✗ Could not load`.
- T-P3-21 Opening a table from a URL with `schema=shop&table=customers` shows `webtuidb.public` in the sidebar and
  status line while the grid is `shop.customers`.
- T-P3-22 The bulk update preview applies with `a` while every other preview commits with `Enter`; it does not say
  how many rows match.

## Verified OK

- CLI: `--help` (exit 0), `--version`, `--list-connections`, `--completion zsh` (`foo` → exit 1 with message),
  `--changelog` (prints URL), `--mcp-config claude-code` (`bogus` → exit 1), `--bugreport` (secrets masked, TERM
  empty when not in a terminal), `--man`, mutually exclusive flags refused (exit 1), unknown connection name
  (`no saved connection named "unknown-name" (and it is not a URL)`, exit 1), connect by name, connect by URL with
  `schema/table/column/operation/value` (opens the filtered table), `--config /nonexistent` falls back to defaults,
  `[keys]` conflict refuses start with `[keys] commit and undo are both bound to x in global` (exit 1), MCP
  `initialize`/`tools/list`/`list_connections`/unknown method (-32601).
- M1 welcome/workspace: list with groups/colours/tags/LAST column, new/edit form with required name, Test (wrong
  and right password, MariaDB 11.4), `password = "ask"` prompt (wrong → failure dialog, right → connects), URL import
  (bad scheme refused; `%40`/`%3A` decoded, sslmode picked up), duplicate (`… copy`), delete confirm, copy URL
  without / with password (U asks first), group menu (rename, delete with typed name listing the connections),
  which-key after the prefix (gone on Esc), help with search, Mongo without auth (clear `✗ Could not load …
  requires authentication`, app usable), Redis requiring auth (`NOAUTH Authentication required`, clean dialog).
- M1 grid: paging (0.10–0.18 s per 300-row page on 1500 rows), limit/offset popover incl. odd offsets and invalid
  values, exact count, single sort and multi-sort dialog, hide/show columns, row detail with column header line,
  cell editor (multi-line, tab preserved in the database, emoji, 20 KB text responsive), Tab to the next cell, Set
  Value menu (NULL/DEFAULT/EMPTY/NOW()/date/file/advanced), add row / pending marks / discard / undo, preview SQL,
  commit on silent (immediate) and Alert Mode 2 (confirm dialog with the statement), Safe Mode 2 password dialog
  (masked, `✗ wrong password` inline), lowering from Safe Mode 2 asks for the password (M6 item 1), no-PK table
  warns before commit and NULL rows use `IS NULL`, constraint errors (check, PK duplicate, NOT NULL) shown with
  "nothing was committed" and edits kept, quick filter, filter operators (=, BETWEEN, IN with unicode, Contains with
  `'`, IS NULL, Raw SQL, date literal), AND/OR, Filter SQL popover, clear all, unicode widths (北京, emoji) aligned in
  grid and sidebar, frame tinted with the production colour (M15 item 46), JSON tree quick look, blob hex dump and
  hex edit with validation, views read-only (Enter = quick look).
- M1 sidebar/objects: schema switch, Recent, Open anything (`^`/`$` anchors, no matches, features, databases, Alt+Enter
  new tab), object menu: Copy CREATE, Clone with content (sequence advanced), Rename to `wide "clone" ü`, Delete with
  typed name (wrong name refused inline), New table designer (create, rename column, add/drop column, type menu,
  undo, commit), MariaDB column reorder (`MODIFY … AFTER`), structure/DDL views.
- M1 editor: run current / run all with an error in the middle (continues, failed tab marked), cancel
  `pg_sleep(30)` (opens the process list, M4 item 11), explain (tree, `ANALYZE false`), beautify/uglify, toggle
  comment, upper case, undo/redo, find & replace, vim mode (`dd`, `u`, `:q`), editable single-table results with
  PK (preview, Alert Mode 2 on MySQL), non-PK results read-only, chart view, copy rows / copy as (7 formats), saved
  queries (name required, `../../escape` folder refused, folder + keyword written), history list and menu, pinned
  connection keeps `BEGIN` across runs.
- M2: CSV import with header, quotes, embedded comma, `NULL as`, `Empty is NULL`, bad row rolls the whole file back
  with the failing statement; MariaDB import with "Create the table" now works (review NEW-1 not reproduced); JSON
  gzip export, SQL export with structure/drop, jobs list; search in database runs and opens a hit filtered.
- M3/M13: ER diagram of the sidebar schema, metrics board new (validation) / open / bar panel, users list,
  assistant without key explained, process list kill with confirm (`canceling statement due to user request`),
  passcode lock at start and after `lock_after_mins`, 3 wrong tries → 10 s back-off.
- M4–M15: split right / close pane, move tab left/right, numbered tabs, close other tabs, rename via menu, workspace
  overview (connection list, close other connections with confirm), reconnect after `docker pause` of MySQL
  (`✗ lost connection … press C-d R to reconnect`, reconnect keeps tabs), cancel a stuck load, slow-query prompt
  appears after `query_timeout_secs`, settings dialog (bool toggle, int validation `"abc" is not a whole number`,
  bad prefix refused on save with the reason, revert), open a `.sql` file, fake data preview, schema diff output,
  80x24 and 60x15 layouts (no overlap; forms scroll), resize with a dialog open, `go test ./internal/keymap/` and
  `go test ./internal/ui/ -run 'TestFlow|TestM'` pass.
- Parity: `--keys` vs `/api/meta` identical in names, descriptions and order except the console context
  (T-P2-07); the TUI help lists the console actions when the console has focus.

## Could not test

- Redis: the saved "redis cache" connection has no password and the container requires one (`NOAUTH`); read-only
  editing on Redis was not exercised.
- MongoDB browsing: the server requires auth and the saved connection has none (expected failure, checked only).
- Backup/restore runs: `pg_dump`, `mysqldump`, `psql`, `mysql` are not on PATH; only the "not found" path was seen.
- Assistant replies and MCP over HTTP: no API key / no client configured.
- Mouse support (click, drag header edge): tmux `send-keys` cannot produce mouse events reliably here.
- Real clipboard on a local OSC 52 terminal (only the tmux buffer was checked).
- Plugins: none installed in the test config home (dialog shows the empty state).
