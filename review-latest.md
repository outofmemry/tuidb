# Review — uncommitted changes (2026-10-28)

Scope: `git diff` (118 modified files, +7394/−2820) plus every untracked file (`git ls-files --others`, ~12.5k lines):
M9–M15 work across `internal/{db,web,ui,mcp,plugin,config,store,backup,board,diagram,import,export}`, `m13_cli.go`
and `web/src`. `go build`, `go vet`, `make parity` and `go test ./...` pass; the issues below are what the tests do
not cover.

**Severity**

- **P1** — data loss or corruption, security hole, app will not start, or a feature that is broken for every user.
- **P2** — edge case, robustness, misleading feedback, parity gap, or a design shortcut that will bite later.

Every P1 was reproduced by reading the code (three by a throwaway probe test or a traced call path, since removed).
P2s come from the same pass; each states the trigger, and none is a style opinion.

**On the "100 P1 / 200 P2" ask:** I am not going to pad this list. This diff has **10 real P1s and 80 real P2s**;
the other ~210 items that would be needed to hit the quota would be invented, and an invented bug costs more time
than a missing one. If you want more volume from this diff, the honest lever is more reviewers on the same areas,
not more claims from one pass.

---

## P1

### P1-01 Assistant chats are written to `chats/<conn>/undefined.json`; history is lost
`internal/store/m13_chats.go:16-35`, `internal/web/m13.go:273-305`, `web/src/utils/api.ts:225-226`

`Chat`/`ChatMessage`/`ChatSummary` carry lowercase JSON tags (`id`, `title`, `messages`, `count`, `updated`), but the
client types and every use site read Go names: `Chat = { ID; Title; Messages }`, `c.ID`, `c.Messages`, `c.Count`
(`Views.tsx:336,346,444,460`).

Trigger: open Assistant, send a message. `POST /chats` answers `{"id":"…"}`, so `(await api<Chat>(…)).ID` is
`undefined`, the client `PUT`s to `/sessions/{sid}/chats/undefined` — and `chatIDRe` (`^[A-Za-z0-9_-]{1,64}$`)
accepts the literal string — so every conversation overwrites that one file and older chats are gone. The history
drawer shows "untitled", "undefined messages", "Invalid Date"; opening a chat renders it empty.

Fix: name the JSON tags `ID`, `Title`, `Messages`, `Role`, `Content`, `At`, `Count`, `Updated` (case-insensitive
decode keeps old files readable), or return Go-named DTOs from `chatNew`/`chatGet`/`chatList`.

### P1-02 Tab in the cell editor writes the previous cell's text into the next cell
`web/src/components/DataTab.tsx:530`, `web/src/components/QueryTab.tsx:644`, `web/src/components/CellEditor.tsx:31-42`

`CellEditor` seeds `text`/`hex`/`mode` from props with `useState`, and `onDone` does `setCell(null)` then
`setCell(next)` in one handler. React batches those, so the dialog is never unmounted — the same instance is
re-rendered with the new `cell` while `text` still holds the old cell. `start` is recomputed from the new `value`,
so `text !== start` and the next Tab/Save writes the **old** cell's content into the neighbouring cell (same for
hex mode). Edits are staged, so this lands on commit.

Fix: `<CellEditor key={cell.row + ':' + cell.col} … />` in both tabs.

### P1-03 MCP read-only bypass via a dollar-quote tag containing a digit
`internal/mcp/mcp.go:644`, `:713-721`

`dollarTagRe` is `^\$[A-Za-z_\x80-\xff]*\$` — no digits — but Postgres allows `$a1$`. On the sequence
`$a1$ x $$ $a1$` the tag is not recognised, the inner `$$` is, and `strings.Index` for the closing `$$` fails, so the
`end < 0` branch sets `i = len(sql)`: **the rest of the statement is blanked** in the copy the deny-list sees. The
server parses the literal correctly and runs what follows.

Trigger (verified against the real `db.Split`/`db.IsReadOnly` and `stripLiterals`):

```sql
SELECT $a1$ x $$ $a1$ , pg_read_file('/etc/passwd')
SELECT $a1$ x $$ $a1$ , dblink_exec('host=… dbname=…','DELETE FROM users')
```

The first passes the keyword check and `sideEffectFunc` (the function name was blanked) and is a read for the
read-only transaction; `dblink_exec` writes through a new outbound connection.

Fix: parse the tag with the same rule as `internal/db` (`\$[A-Za-z_\x80-\xff][A-Za-z0-9_\x80-\xff]*\$`), or reuse
`db.Split`'s lexer instead of a second implementation.

### P1-04 `read_only = true` is not enforced on ClickHouse, and the error message recommends it
`internal/mcp/mcp.go:567-569`, `internal/mcp/readonly_host.go:19-21`, `internal/db/clickhouse/clickhouse.go:490`

`readOnlyRunner` returns the raw session when the connection is marked `read_only`, on the assumption that "the
driver then enforces it on the server". Only postgres/mysql profiles do; ClickHouse has no `s.conn.ReadOnly` gate
(`clickhouse.go:490` checks the *keyword* only) and no `readOnlyBegin` entry. So when a read-only query is refused
with "mark the connection read_only in its settings", following that advice strips the read-only transaction and
leaves only a keyword filter.

Trigger: mark a ClickHouse connection `read_only`, then `execute_read_query` with
`SELECT * FROM file('/etc/passwd')` or `SELECT * FROM url('http://169.254.169.254/latest/meta-data/…')` — file read
and SSRF from the DB host, if the account may call those table functions.

Fix: trust `ReadOnly` only for dialects whose driver checks it (or list them explicitly); otherwise refuse unless a
read-only transaction is available.

### P1-05 Binding an installed plugin's shortcut makes the app refuse to start
`internal/keymap/prefix.go:107`, `internal/config/config.go:450-458`

`KeymapChecked` keeps `[keys] plugin:<name>` overrides whose plugin is installed, then calls
`BuildChecked(prefix, over, extra…)`, which first runs `Build(prefix, overrides)` **without** `extra`. In that call
`known` has only the defaults, so the plugin action is rejected as unknown.

Reproduced with a probe:

```
KeymapChecked(overrides={"plugin:x":["M-Q"]}, extra=[plugin:x]) →
  err = `[keys] unknown action "plugin:x"`      // main.go:174 → run() returns → no TUI, no web
```

This is the documented M13 flow (bind a plugin key in Settings → Keys, or write the line by hand). Fix: build the
base map with the plugin actions' *keys* removed but their actions known, e.g. pass `extra` with `Key:""` to the
first `Build`, or reconcile overrides against `extra` before validating.

### P1-06 `[keys] plugin:<x> = "-n M-q"` fails validation for an uninstalled plugin
`internal/config/config.go:476`, `internal/keymap/prefix.go:32`

The lenient placeholder created in `keymapWith(…, lenient=true)` uses `specs[0]` verbatim as `Key`. `Build` strips
`-n ` from *overrides* but parses *extra* keys as-is, so `Parse("-n M-q")` fails and `Config.validate` returns an
error — `Load` fails, the app does not start, and `WriteKeys` cannot save it either. The `-n ` spelling is the
documented way to make a binding prefix-free (`prefix.go:17`), and `config.go:442` promises that a removed plugin's
override never breaks startup.

Reproduced: `validate()` → `plugin:x: bad key "-n M-q": unknown key name "-n M-q"`.

Fix: strip `-n ` before using the placeholder, and skip (report) a placeholder that cannot be parsed instead of
failing the whole config.

### P1-07 Pinned result-tab commits are not atomic, but the error says nothing was committed
`internal/db/postgres/pin.go:77-97`, `internal/ui/workspace.go:344-352`, `:510-517`

`pinned.Apply` runs statements with `Conn.Exec` and never opens a transaction (the doc comment says the caller owns
it; the result-edit path does not — only the importer opens one). `executeOn` still computes
`atomic = caps.Transactions && !hasDDL(stmts)`, so on a pin the failure dialog says **"Statement failed (nothing was
committed)"** while the earlier statements have already autocommitted.

Trigger: pin a query tab, edit three result rows, commit, the second UPDATE violates a constraint → row 1 is
applied, the dialog claims it was not, and a retry applies it twice.

Fix: in `pinned.Apply`, wrap the loop in `BEGIN`/`COMMIT`/`ROLLBACK` when `wantTx(caps.Transactions, stmts)` and
skip it only when the pin already holds a transaction.

### P1-08 A stray `"` silently swallows the rest of an import file
`internal/importer/importer.go:105` (`cr.LazyQuotes = q == '"'`), `:146-148`

With the default quote, `encoding/csv` runs with `LazyQuotes` and `FieldsPerRecord = -1`. An unterminated quoted
field consumes to EOF and returns one field, no error, and the row is committed inside the new whole-file
transaction.

Trigger: `a,b\n1,"unclosed\n2,x\n3,y\n` → one row `["1", "unclosed\n2,x\n3,y\n"]`, `err == nil`; rows 2–3 are gone.

Fix: `cr.LazyQuotes = false` for the default quote too, so it returns `ErrQuote` with a line number; add the row's
line to the error.

### P1-09 Expression indexes block every structure edit on the table
`web/src/utils/tabledef.ts:66`, `internal/db/postgres/meta.go:124-126`

`newDef` validates **all** index rows against column names, and Postgres reports expression index keys as their
text (`pg_get_indexdef` → `lower(email)`). Any table with `CREATE UNIQUE INDEX … ON users (lower(email))` therefore
throws `Index users_email_lower names a column that is not in the table` from Preview, for any change, including
one that does not touch the index; the only offered escape is deleting the index row (which plans `DROP INDEX`).
Also blocks the FK form and `StructureEditor` (`tabledef.ts` is shared); the TUI has no such check.

Fix: only validate index rows the user changed, and accept entries that do not match `/^\w+$/` as expressions.

### P1-10 The next backup overwrites the previous one, because the expanded file name is remembered
`internal/ui/m7.go:360-401`, `internal/ui/m13_backup.go:18-25`, `internal/backup/backup.go:261`

The File field's default is already expanded (`export.Filename(…, "{table}-{date}") + ".sql"`, e.g.
`mydb-2026-10-27.sql`), and the dialog stores whatever was submitted back into `State.BackupOptions`
(`cur[pre+"file"] = v["file"]`). `val("file", …)` prefers that stored literal, so from the second day on the dialog
is prefilled with **yesterday's** path. Unlike every other file-writing flow (`m2.go:229`, `m4.go:159`, `m5.go:55`,
`m13_board.go:127`, all through `a.ifFree`), the backup path calls `w.runBackup` directly, and `backup.Run` finishes
with `os.Rename(part, file)` — which silently replaces the old dump. The web refuses an existing backup file until
confirmed (`internal/web/m3.go:224`).

Trigger: run a daily backup twice; the second run deletes the first file with no prompt.

Fix: wrap the backup start in `w.a.ifFree(run, backup.OutputPath(o))`, and don't remember the expanded `file` value
(remember the pattern instead).

---

## P2 — data layer

### P2-01 `=` search on an any-column filter compares natively on MySQL/MariaDB
`internal/db/filter.go:135`, `internal/db/search.go:37`, `internal/db/dialect.go:39`

`=` used to be rendered as an escaped `LIKE` everywhere; now the cast is applied only when `d.LikeCast`, which is
false for MySQL. `WHERE (`i` = ? OR `name` = ?)` with `'bob'` makes MySQL coerce `'bob'` to 0, so every row whose
int column is 0 is reported as a hit and opens a table showing unrelated rows.

Fix: cast `=` as well when `d.CastText` exists (`MySQL.CastText` is already defined).

### P2-02 Vertica refuses `CREATE TABLE` as "catalog browsing", though it needs no catalog
`internal/db/vertica/profile.go:39-41`, `internal/db/postgres/plan.go:248-249`

`postgres.PlanTable(old=nil, new)` goes straight to `planCreate`, which builds DDL from the `TableDef`; only the
alter path reads `pg_catalog`. Imports with "Create the table" and the web structure editor call this, so a
supported operation now fails.

Fix: `if old == nil { return s.Session.PlanTable(nil, new) }`.

### P2-03 ClickHouse `Objects` drops the schema, so an all-schemas search counts the current database
`internal/db/clickhouse/clickhouse.go:250-268`, `internal/db/search.go:50`

`Objects(ctx, schema)` queries `system.tables WHERE database = ?` but returns refs with `Schema` empty; `dbOf`
falls back to `CurrentDatabase()`, so `Count`/`Rows` hit the current database's same-named table. The TUI hides
all-schemas (no `caps.Schemas`), but `POST /search` with `"all_schemas": true` is live.

Fix: set `Schema` on each ref, as the new MySQL `Objects` does.

### P2-04 All-schemas search duplicates the current database on Redis/Mongo
`internal/db/search.go:84-88`, `internal/db/redis/redis.go:218-229`

`SearchSchemas` enumerates `Databases()` when `caps.Schemas` is false, but Redis/Mongo `Objects` ignore the schema
argument (Redis even returns `"0".."15"` from `Databases`), so the current database is scanned once per database
name: N identical hits per key and N× the latency.

Fix: return `[""]` from `SearchSchemas` for drivers whose `Objects` cannot honour a schema, or refuse all-schemas.

### P2-05 MySQL: a failed limits query is shown as 0 and then written back
`internal/db/mysql/users.go:227-250`, `internal/ui/m13_users.go:22-32`

`fillLimits` swallows query/scan errors and leaves `Limits == nil`; the dialog renders `u.Limits[n]` (nil map → 0)
with the hint "0 = unlimited" and submits all four keys, so saving clears the account's quotas.

Trigger: an account that can read `User, Host` but not the `max_*` columns → open Resource limits, Save.

Fix: return the error (or a "not readable" marker) instead of presenting zeros.

### P2-06 `EditableSelect` ignores its dialect, so `select "id" from t` is treated as editable on MySQL
`internal/db/m9.go:44`, `:215-222`, `internal/ui/resultedit.go:62`

`lexSelect` treats `"…"` as a quoted identifier (right for Postgres, wrong for MySQL with default `sql_mode`, where
it is a string literal). A result column named `id` then looks like the PK, and an edit plans
`UPDATE t SET id = … WHERE id = 'id'`, rewriting whichever row has PK `'id'` rather than the displayed row. The web
now relies on this function alone (`internal/web/m9.go:34`).

Fix: thread `dialect` into `lexSelect` and treat `"` as a string for mysql/mariadb.

### P2-07 The WKB parser rejects valid deeply-nested geometry collections
`internal/db/m15_geo.go:47`

`depth > 8` returns `errWKB`; PostGIS allows any nesting, so a 10-deep `GEOMETRYCOLLECTION` silently falls back to
a hex dump in the grid and quick look. Each level consumes ≥ 5 bytes, so the cap adds nothing.

Fix: raise it (64) or bound by remaining length.

---

## P2 — config, keymap, store, backup

### P2-08 Backup "extra options" are keyed on driver+direction, not on the tool that runs
`internal/backup/backup.go:114-146`, `:79-84`

A Postgres plain-format restore runs `psql`, but `CommonOptions(Postgres, true)` and `safeExtra` still offer
`--clean`, `--if-exists`, `--no-owner`, `-j`, `--column-inserts`, `--no-tablespaces`; ticking one produces
`psql: unrecognized option`. Same for MySQL restores, which run the `mysql` client while the list is `mysqldump`'s.

Fix: derive the list from the resolved tool, and hide the checkboxes for psql/mysql-client restores.

### P2-09 `--oplog` is offered for mongorestore, which has no such flag
`internal/backup/backup.go:69-73`

`mongorestore` accepts `--oplogReplay`/`--oplogFile`; `--oplog` is a `mongodump` flag. Ticking "replay the oplog"
fails the restore.

Fix: `--oplogReplay` on the restore side, allow-list it.

### P2-10 Dumps written by the tool itself are created with the umask
`internal/backup/backup.go:233-268`, `:379`, `:431-436`

Only the pipes this package opens are 0600. When the tool writes the `.part` (`pg_dump -f` — every Postgres backup,
`mongodump --archive`, `redis-cli --rdb`) the file is typically 0644, so a full dump is world-readable while a
gzipped one is 0600.

Fix: `os.Chmod(file, 0o600)` on the finished file, and say so where the part file is created.

### P2-11 `config.toml` holding only the passcode hash is not chmodded
`internal/config/config.go:277-284`

`secureConfigFile` lists the web password, AI keys and MCP tokens but not `[security] passcode`, so a config whose
only secret is the bcrypt hash stays 0644 — offline brute force of short numeric passcodes by any local user.

Fix: `secret = secret || c.Security.Passcode != ""`.

### P2-12 `theme.file` is not `~`-expanded, unlike every other path setting
`internal/ui/theme.go:142-151`, `internal/config/m13_themes.go`

`[[source]] path` and `queries.shared_dirs` expand `~`/env; `theme.file` is stat'ed literally, so
`theme.file = "~/mine.toml"` silently loads nothing (TUI: a startup error string; web: nothing).

Fix: expand in `Load` next to the other path fields.

### P2-13 `general.hints` / `web.toolbar` accept unknown action names
`internal/config/config.go:368-382`, `internal/ui/m15_app.go:35-55`, `web/src/routes/Workspace.tsx:450`

Only empty/duplicate entries are rejected; a typo (`"commmit"`) is dropped by both UIs with no message, so a valid
looking setting does nothing.

Fix: validate against `keymap.DefaultBindings()` plus the web-only actions in `validate`.

### P2-14 Menus offer actions the web result pane does not implement
`internal/menus/menus.go:198,200`, `web/src/components/QueryTab.tsx:433-438`

"Pin result" (`pin`) and "Add to metrics board…" (`add_to_board`) have `UI:""`, but the web result menu only
dispatches `cellActs()`, which has neither, so both answer "…: not available here" while the TUI implements them.

Fix: handle both in `cellActs`, or mark them `UI:"tui"` with a `Why`.

### P2-15 "Rename database…/Drop database…" ignores `Capabilities.RenameDB`
`internal/menus/menus.go:154-155`

The items have no predicate and nothing reads `RenameDB` (only assignments exist). Vertica sets `RenameDB=false`
yet still gets "Rename…", which plans `ALTER DATABASE … RENAME TO` and fails server-side.

Fix: gate on the capability (and honour it in the UI), or delete the dead field.

### P2-16 The crash log grows without bound and is written from every web request
`internal/crashlog/crashlog.go:63-80`, `internal/web/server.go:315`, `internal/web/m2.go:366`

`write` always appends; `Catch` is deferred on every request and job. A panic that reproduces on a polled endpoint
writes a full stack per poll and can fill the disk.

Fix: truncate/rotate at a few MB before appending.

### P2-17 Chat size is capped by message count, not bytes, and `List`/`Search` decode every file
`internal/store/m13_chats.go:96-160`, `internal/web/m13.go:308`

One pasted 50 MB message makes the history picker (and every search keystroke, which re-reads and unmarshals all
files for the connection) hang or OOM.

Fix: cap bytes on save, and skip oversized files in `scan`.

### P2-18 A connection named `..` moves the chat store out of `chats/`
`internal/store/m13_chats.go:44-46`

`dir` reuses `History.path` and strips `.jsonl`; `"."`/`".."` are allowed by `path`, so `chats/..` is the data
directory itself and `Ask`/`Search` scan unrelated files.

Fix: encode the connection name for chats independently, or reject `.`/`..`.

### P2-19 History import dedupes on unclipped SQL
`internal/store/history.go:186-196`

The key is computed before `clip`, but existing entries are matched by their clipped stored text, so a source with
a >64 KB statement re-imports a duplicate on every import.

Fix: clip before computing the key when the entry has no ID.

---

## P2 — MCP and plugins

### P2-20 The read-only deny-list is Postgres-only, so MySQL `LOAD_FILE` passes
`internal/mcp/mcp.go:636`

`sideEffectRe` has `lo_\w+`, not `load_file`; `START TRANSACTION READ ONLY` allows it. A read-only token on a MySQL
connection can return `/etc/passwd` from the DB host whenever the DB user holds `FILE`.

Fix: per-dialect lists (MySQL `load_file`, `into outfile|dumpfile`, `get_lock`; ClickHouse
`file/url/s3/remote/hdfs/executable`).

### P2-21 Read-only queries on Vertica fail: `BEGIN READ ONLY` on a dialect with no transactions
`internal/mcp/mcp.go:570-573`, `internal/db/vertica/profile.go:15`

`readOnlyBegin` is keyed on the dialect name, and Vertica reports `postgres` with `Transactions: false` while
still exposing `Pin` (embedded `db.Session`), so every `execute_read_query` and plugin `query()` dies on the
server's `BEGIN` error instead of running.

Fix: require `begin != "" && sess.Caps().Transactions`.

### P2-22 `EXPLAIN ANALYZE CREATE …` bypasses the read-write DDL gate
`internal/mcp/mcp.go:326-331`, `:605-620`

The gate uses the first keyword; `EXPLAIN ANALYZE CREATE TABLE x AS SELECT …` starts with `EXPLAIN`, which is not
in `ddl`, but the statement executes the DDL. A read-write token that is not supposed to run DDL can create tables.

(For the record: the agent's other example, `COPY … TO PROGRAM`, *is* caught by `copyStmtRe`.)

Fix: take the first keyword after `EXPLAIN [ANALYZE]`, or refuse `EXPLAIN ANALYZE` on non-full tokens.

### P2-23 The goja host has a timeout but no memory budget
`internal/plugin/m13_script.go:78-125`

`for(;;) s = s + s` allocates tens of GB well inside `jsTimeout`; the process dies. The web path is caught by
`crashlog.Catch`, the TUI's `liveCmd` goroutine is not.

Fix: sample `runtime.MemStats` from the existing watchdog and `vm.Interrupt` past a per-run cap.

### P2-24 Plugin output is not truncated
`internal/plugin/m13_script.go:127-129`

`Output.Text` and every `Effect.Text` are unbounded, unlike command plugins (`maxOut = 1<<20`, `plugin.go:120`).
`"x".repeat(300_000_000)` as the final expression returns 300 MB, which is then split by newlines for the dialog.

Fix: truncate to `maxOut` in both the TUI and web paths.

### P2-25 `runJS` re-reads the script and ignores `maxScript`/`p.Err`
`internal/plugin/m13_script.go:72-75`, `:32-35`, `plugin.go:94`

`loadJS` enforces 1 MiB and records errors on `p.Err`, but `Run` neither checks `p.Err` nor re-checks the size, and
re-reads the file each run — an edited or oversized file executes despite the load-time verdict.

Fix: `if p.Err != nil { return …, p.Err }`, re-check the size, or run the bytes captured at load.

---

## P2 — web API

### P2-26 The UI calls `GET /sessions/{id}/backup/options`, which does not exist
`web/src/components/Admin.tsx:146`, `internal/web/server.go:434-435`

Only `POST …/backup`, `POST …/restore` and `GET/PUT /api/state/backup-options` are registered, so every dialog open
logs a 404 and silently uses the hard-coded copy in `web/src/utils/m13.ts:59`, which will drift from
`backup.CommonOptions` (and never offers the extras `safeExtra` accepts).

Fix: register the route, or drop the request.

### P2-27 Finished backup/restore jobs advertise a Download that always 404s
`internal/web/m13.go:113-117`, `internal/web/m3.go:230-233`, `internal/web/m4.go:157-167`

`summary`/`snapshot` set `"file": j.fileName` whenever `fileName` is set, but `jobFile` requires the temp `j.file`
(backups only have `fileName`+`path`). `Admin.tsx:233` and `Jobs.tsx:35` therefore show Download on every finished
backup, and clicking it returns 404.

Fix: only include `"file"` when `j.file != ""`; keep `download` as the capability flag.

---

## P2 — web UI

### P2-28 The lock screen does not stop shortcuts
`web/src/components/Lock.tsx:29-36`, `web/src/utils/keys.ts:18`

The comment says the lock's capture listener "is registered first", but child effects (`<Outlet/>` → Workspace's
`useKeymap`) run before the parent's, so the workspace listener is registered first and runs first;
`stopImmediatePropagation` cannot undo it. With the default prefix, `ctrl+d` then `5` on the lock screen switches
tabs, and prefix-free keys (`ctrl+h/j/k/l`) work immediately. Confirmation dialogs render in `Prompts`, outside the
lock.

Fix: have the workspace/keymap handler bail when `document.documentElement.dataset.locked` is set (Lock already sets
it), rather than relying on listener order.

### P2-29 Corrupt-but-valid JSON in localStorage throws and blanks the app
`web/src/utils/workspaces.ts:12-33`, `web/src/routes/Workspace.tsx:126`

Only `JSON.parse` throwing is guarded. `webtuidb-open` holding `"5"` or `[]` makes `o[name] = sid` a TypeError
(strict modules), and `webtuidb-tabs-<sid> = {}` makes `sv.tabs.length` throw on load.

Fix: validate shape in the helpers (`typeof v === 'object' && !Array.isArray(v)`, `Array.isArray(v?.tabs)`).

### P2-30 Dragging the active tab into the other pane leaves a blank pane
`web/src/routes/Workspace.tsx:349-356`

`reorder` only sets the destination pane's active tab; the source pane's `actives` still points at the tab that
moved, no remaining tab matches it, and the pane renders empty. `close_tab` in that pane then closes the tab that is
now on the other side.

Fix: when the moved tab was the source pane's active one, activate the last remaining tab there.

### P2-31 The `reconnect` shortcut skips the "unsent changes are lost" confirmation
`web/src/routes/Workspace.tsx:474`, `:660`

The status-bar button confirms; the keymap action calls `reconnect()` directly, closing the session and discarding
staged cell edits.

Fix: route the action through the same confirm.

### P2-32 "Copy link" puts the server bearer token in a link meant for sharing
`web/src/routes/Workspace.tsx:396-401`, `web/src/utils/api.ts:19-22`

`copy_link` copies `location.origin + withToken(rel)`, and the token in the fragment grants full API access,
independent of `[web] password`. Pasting that link into a chat or an issue hands over read/write access and leaves
the token in the URL history.

Fix: keep the fragment trick for `window.open` of same-origin tabs; copy a plain link.

### P2-33 The shift-selection anchor survives a sort
`web/src/components/Grid.tsx:133-137`

`anchor` is only cleared on Esc/plain click or a change in row/column count; sorting re-orders the same rows and
`curRow` is recomputed from the row key, so a range materialises over unrelated rows. Row detail then offers to
apply one edit to all of them, and `copy_cells` copies them.

Fix: clear the anchor when row order changes (sort, filter, refresh).

### P2-34 `[table] default_sort` is persisted as if the user chose it
`web/src/components/DataTab.tsx:85-101`

The first load with nothing saved sets `sort` from the PK default, and the debounced state PUT always sends `sort`.
The table is then sorted on every open regardless of the setting, and "un-sorting" is undone next time.

Fix: send `sort` only after a user change (or persist `"none"`).

### P2-35 Leaving the Structure view drops the draft but leaves the tab dirty
`web/src/components/StructureEditor.tsx:53`, `web/src/components/DataTab.tsx:135,537`

`onDirty` is reported from an effect with no unmount cleanup, so switching back to Data discards the draft
silently and leaves `structDirty` true: the ● dot, "Discard pending changes?" on close and `beforeunload` all warn
about nothing.

Fix: `useEffect(() => () => onDirty?.(false), [])`, ideally with a confirm before leaving a changed draft.

### P2-36 The filter-column event is global, so hidden tabs also gain a filter row
`web/src/components/FilterBar.tsx:33-45`, `web/src/components/DataTab.tsx:317`

The handler never checks which tab dispatched it, and every mounted `DataTab` renders a `FilterBar` when the bar is
shown. With two table tabs open, "Filter with this column" adds a row to both (and the delayed `focus()` can move
the caret into a hidden tab).

Fix: include the table/tab in the event detail and ignore mismatches.

### P2-37 The jobs list cannot show an error
`web/src/components/Jobs.tsx:18`

`q.data?.jobs ?? []` drops `q.error`, so a failed `/jobs` renders the empty state and a running count of 0.

Fix: pass the error through and render it.

### P2-38 Any unrecognised job menu action deletes the job
`web/src/components/Jobs.tsx:36`

`run()` falls through to `dismiss(j)`, and `show()` returns true for unknown actions. Only `reveal` reaches it
today, and it is disabled — but the menu is server-driven metadata, so adding "Logs" would silently dismiss the job.

Fix: make the fallback a no-op + notify.

### P2-39 Settings offers read-only path settings, and one of them fails the whole save
`web/src/components/Settings.tsx:42,65`, `internal/web/m13.go` (`pathSettings`)

`config.Schema` excludes only `web.password`/`security.passcode`, so `theme.file`, `web.tls_cert`, `web.tls_key`
and `queries.shared_dirs` are editable, while `putSettings` rejects them. Editing one of those plus any other field
returns 400 and **neither** value is written.

Fix: filter `pathSettings` out of the editable fields (show them read-only).

### P2-40 The web fuzzy search omits the TUI's length penalty, so rankings differ
`web/src/utils/matchname.ts:15` vs `internal/ui/util.go:88`

The TUI returns `score - len(t)/8`; the port returns `score`, and `Sidebar.tsx:72` sorts on it. Searching `usr`
ranks `user_sessions_archive` above `users` in the web but not the TUI. `web/test/grid.test.mjs:39-43` only
asserts booleans, so it passes either way.

Fix: subtract `Math.floor(len/8)` as the TUI does.

### P2-41 The MySQL/MariaDB "New function" template can never run
`web/src/utils/templates.ts:13`, `internal/db/split.go:101`

The template emits `BEGIN … RETURN 0; END;`, but `db.Split` only stops at `;` when the script uses `DELIMITER`, so
Run sends `CREATE FUNCTION … BEGIN RETURN 0;` and then `END;` → syntax error every time. The TUI template is a
single statement.

Fix: emit `… DETERMINISTIC RETURN 0;`, or wrap in `DELIMITER // … // DELIMITER ;`.

### P2-42 The MySQL/ClickHouse create templates drop the schema
`web/src/utils/templates.ts:7` vs `internal/ui/m15_menus.go:318`

`name` skips the schema for mysql/mariadb/clickhouse, while the TUI prefixes it for every dialect. The object is
created in the connection's default database instead of the schema being browsed; `web/test/m13_menus.test.mjs:31`
bakes in the divergence.

Fix: prefix the schema whenever one is given (MySQL accepts `` `db`.`obj` ``) and update the test.

### P2-43 Settings enum rows have an orphaned `<label>`
`web/src/components/Settings.tsx:85-88`

`htmlFor` points at `set-<key>`, but the enum branch renders `<Select>` without an `id`, so clicking the label does
nothing and the control is unnamed for AT.

Fix: add an `id` prop to `Select` and pass it.

---

## P2 — import/export/board/diagram

### P2-44 CSV export prints a binary value as a number when its bytes look numeric
`internal/export/export.go:246-251`, test `export_test.go:62`

`NeutralizeValue` returns `string(b)` for a `[]byte` that parses as a float, but `[]byte` is only produced for
binary columns (`bytea`, BLOB, Cassandra blob). A `bytea` of `31` exports as `1` instead of `\x31`, and re-import
puts text in the binary column. The test asserts `[]byte("-7") → -7`.

Fix: delete the `[]byte` branch and let `FormatValue` hex it.

### P2-45 Short rows become NULL instead of an error
`internal/importer/importer.go:146-155`

Only `len(rec) > len(cols)` is rejected; shorter rows are padded with `nil`. A truncated file or an unescaped
separator silently imports NULLs and reports success.

Fix: `len(rec) != len(cols)` → error naming the row.

### P2-46 A failed import leaves the created table behind
`internal/importer/importer.go:428-437` vs `:462-483`

`PlanTable`/`CREATE TABLE` runs before `Pin`+`BEGIN`, so a failure in the data (e.g. a NUL byte on Postgres) rolls
back the rows, leaves the new table, and reports "(rolled back, no rows were imported)".

Fix: open the pin/transaction before `PlanTable` and run the CREATE through it.

### P2-47 Import progress counts attempted rows, not inserted rows
`internal/importer/importer.go:372`, `:497`

`p.Rows += len(changes)`; the driver's affected count is never used, so `ON CONFLICT DO NOTHING` over 100
duplicates reports "imported 100 rows".

Fix: use the affected count, or word it "processed N rows".

### P2-48 Formula unquoting cannot be turned off, and the preview disagrees with the import
`internal/importer/importer.go:309-317`, `:129-131`

`KeepFormulaQuote` is set by neither caller (`ui/m2.go:312`, `web/m2.go:514`), so a genuine `'=A1` cell loses its
apostrophe; `Preview` does not apply `cell()`, so it shows `'=1+1` while the DB gets `=1+1`.

Fix: expose the flag, and apply the same transform in `Preview`.

### P2-49 A wrong import encoding imports mojibake without an error
`internal/importer/importer.go:523-533`

`x/text` decoders substitute U+FFFD and return nil, so "utf-16le" on a UTF-8 file silently writes CJK-looking
rows.

Fix: use a transformer that errors on replacement characters the source did not contain.

### P2-50 A large `.sql` import is read into memory and cannot be cancelled
`internal/importer/importer.go:381`

`io.ReadAll` with no `ctx.Err()` in the loop: a 2 GB dump blocks until read, ignoring cancel.

Fix: stream over a `bufio.Reader` and check the context per statement.

### P2-51 `on_conflict` is gated on the dialect name, which Redshift/Greenplum/Vertica share
`internal/importer/importer.go:344`

They all report `Dialect: "postgres"`; Redshift has no `ON CONFLICT`, so every row of an upsert import fails and
the whole import rolls back. Vertica instead reports the unrelated "catalog browsing" error.

Fix: gate on the driver, not `caps.Dialect`.

### P2-52 `on_conflict=update` silently inserts duplicates when the PK is not in the file
`internal/importer/importer.go:445-459`

`ON CONFLICT (pk)` only fires when the inserted row carries the PK column. Importing emails into
`(id serial pk, email unique)` never conflicts.

Fix: refuse `update`/`replace` when no mapped column covers the PK.

### P2-53 File-name patterns mis-handle empty tokens
`internal/export/export.go:561`

`strings.Trim(out, " .")` trims leading dots too: `{database}.csv` with an empty database becomes `export`, and
`{tag}-{table}` becomes `-t`. Callers append the extension again or not, so you get `csv.csv`/`-t`, and the TUI can
overwrite a previous export.

Fix: `TrimRight` only, and drop an empty token with its separator.

### P2-54 Windows reserved device names are not neutralised
`internal/export/export.go:544-560`

`{table}` = `CON`/`NUL`/`COM1` produces a file Windows cannot create; the export fails with an OS error and no
hint.

Fix: prefix `_` for `(?i)^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])$`.

### P2-55 A NULL dropdown value becomes the literal option `<nil>`
`internal/board/board.go:154-158`

`fmt.Sprint(row[0])` renders NULL as `<nil>` (and a bytea as `[104 101]`), and selecting it binds that string into
the next query.

Fix: skip NULLs, use `db.FormatValue` for the rest.

### P2-56 Dropdown options are silently capped at 500
`internal/board/board.go:146-149`

Values past 500 are unreachable with no indication.

Fix: raise the cap and surface truncation.

### P2-57 `diagram.Schemas` discards the error
`internal/diagram/diagram.go:49-55`, `internal/ui/m13_diagram.go:264`

A permissions/timeout failure is reported as "has no schemas".

Fix: return `([]string, error)` and show it.

### P2-58 Adding an existing FK from the diagram duplicates the constraint
`internal/diagram/diagram.go:57-61`

`WithFK` appends blindly; `loadDef` already loaded the table's FKs and `PlanTable` diffs by name, so the same key
again under a new name is planned as a second `ADD FOREIGN KEY`, which Postgres accepts, and the diagram draws two
edges.

Fix: skip when the same name or the same column/ref-column set exists.

### P2-59 Export `null_as` has no import counterpart, and `EmptyIsNull` is dead
`internal/export/export.go`, `internal/importer/importer.go`, `internal/ui/m2.go:302-312`, `internal/web/m2.go:421-435`

NULL exports as an empty cell by default, but neither import dialog sets `EmptyIsNull`/`null_as`, so the product's
own round trip turns every NULL into `''` in a created TEXT column, and a custom `null_as` can never be read back.

Fix: add the field to both import dialogs and thread it through.

---

## P2 — CLI and packaging

### P2-60 `--bugreport` leaks a multi-line secret when the config does not decode
`m13_cli.go:101-125`

`maskSecrets` decodes TOML and masks by key; when decoding fails it falls back to the line pass, which replaces
`password = """` with `"***"` but leaves the body lines of the multi-line string. A config with a syntax error is
exactly the case where `--bugreport` is used, and the output is meant to be pasted publicly.

Fix: in the fallback, also drop the body of multi-line strings (or refuse to print the config and say why).

### P2-61 Theme install/update and `-changelog` can hang or leak a process
`m13_cli.go:249-260`, `:26-38`

`git clone`/`git pull` use `CombinedOutput` with no timeout/context, so a stalled remote hangs the CLI;
`openURL` calls `Start()` without `Wait()`, leaving the child unreaped until exit (and `rundll32` mangles URLs with
query strings on Windows).

Fix: `exec.CommandContext` with a timeout; `go cmd.Wait()` or use the platform browser opener already in the tree.

---

## P2 — terminal UI (modified files)

### P2-62 `[export] filename = "{table}.csv"` produces `users.csv.csv` in the TUI
`internal/ui/m5.go:268-270` vs `internal/web/m6web.go:82-88`

`exportName` always appends `"." + ext`; the web returns the name unchanged when the expanded pattern already ends
with the extension. Affects the export dialog, multi-table export and board export.

Fix: same suffix check as `m6web.go`.

### P2-63 Find & replace is missing on editable query results
`internal/ui/tab_query.go:683`, `internal/ui/resultedit.go:112-118`

The results find form is opened with `canReplace=false`, so Replace / Alt+r / Alt+a are never offered, although
query results become editable and the web results pane passes `editable={canEdit}` to `FindBar`; `Grid.ReplaceSkip`
already handles the grid case.

Fix: pass `true`; `findForm` hides the fields when `!g.Editable`.

### P2-64 `alt+e` (export filtered) does nothing on an unfiltered table, and says otherwise
`internal/ui/filterbar.go:226-231` vs `web/src/components/FilterBar.tsx` (`exportRows`)

`exportFiltered` refuses unless a filter is *applied* and the toast reads like a report ("no filter is applied; this
exports the whole table"), while the web always opens the export dialog with the current filters (empty = whole
table). It also exports the applied filters while the bar shows drafts.

Fix: drop the guard and call `exportAction()` with the current filter set.

### P2-65 `alt+c` clears the filter model before the unsaved-changes guard
`internal/ui/filterbar.go:220-224`

`clearFilters` sets `Filters = nil` and closes the bar, then `applyFilters()` goes through `t.guard`. Cancelling the
"Discard pending changes?" prompt leaves the filters cleared and the bar closed while the loaded rows are still
filtered, and the persisted state was never written.

Fix: move the mutations inside the `guard` closure.

### P2-66 A failed chat save in the error path is silent
`internal/ui/assistant.go:186-192`

`sendFailed` calls `persistChat()` as a statement and drops the returned `fail` cmd, unlike `send`/`assistantNew`
which `tea.Batch` it. With an unwritable chats directory the question stays in the file and nothing is reported.

Fix: `return tea.Batch(w.persistChat(), msgCmd)`.

### P2-67 Row-detail edit toast prints the column name and the wrong count
`internal/ui/celledit.go:144-153`

`say("staged %q for %d row(s)", g.Cols[g.col].Name, rows)` quotes the **column name**, not the staged text, and
`rows` is `len(g.selectedRows())` — every selected row, including deleted ones and rows whose value did not change —
rather than the count `SetColumn` returns. Enter on an unchanged value still reports a staged change.

Fix: use `n := g.SetColumn(text)` for the count and return without a toast when `n == 0`.

### P2-68 The assistant's "Esc Stop" rule can be wider than the pane
`internal/ui/assistant.go:302-306`

The rule is `2 + width(stop) + max(0, W-2-width(stop))`; when `W < 2+width(stop)` (≈11 cells) the filler is clamped
but the prefix is not, so the line overflows and the frame below is misaligned.

Fix: truncate `stop` to `max(0, W-2)`, or draw the plain rule when the pane is too small.

---

## P2 — terminal UI (new files, M13–M15)

### P2-69 "0 = unlimited" is wrong for MySQL `MAX_USER_CONNECTIONS`
`internal/ui/m13_users.go:24`

One hint is used for all four limits. For `MAX_QUERIES_PER_HOUR`/`MAX_UPDATES_PER_HOUR`/`MAX_CONNECTIONS_PER_HOUR`
0 means no limit; for `MAX_USER_CONNECTIONS` 0 means "use the server's `max_user_connections`". Setting 0 to remove a
cap can therefore give the account whatever the global limit is.

Fix: per-key hints, `"0 = the server default"` for `MAX_USER_CONNECTIONS`.

### P2-70 Turning vim off leaves visual mode and `:` mode behind
`internal/ui/m3_editor.go:14-18`, `internal/ui/m15_vim.go:13-24`

The toggle resets only `on`, `insert`, `pend`. In visual mode `vmode`/`va` stay set, so the editor reports a
char-wise selection and the next character replaces the selected block; with the `:` line open, `cmdOn` stays true,
so re-enabling vim resumes command mode with a hidden command line.

Fix: reset the whole `vimState` (or call `vimLeaveVisual()`, clear `cmd`, `cmdOn`).

### P2-71 Backup/restore tool output and job errors are drawn without stripping control sequences
`internal/ui/m13_backup.go:105-109`, `:157-158`, `internal/ui/m15_jobs.go:95-107`

Raw tool lines go through `wrapText` → `frameEdge` → `fit`; short lines are emitted verbatim, and `e.err` is shown
as-is in the jobs list. A database object name echoed in an error can carry ANSI/OSC sequences (clipboard write,
window title). The assistant path already sanitises (`assistant.go:199 cleanReply`).

Fix: strip ANSI on every tool line/error before `d.add`/`jobEntry.note`.

### P2-72 Console pane keys are hard-coded, so help cannot list them and they cannot be rebound
`internal/ui/m15_console.go:99-135`, `internal/keymap/action.go` (no `Console` context)

`/ n N y e m` are literal keys in the console's handler, yet the console is a focusable pane that maps to
`keymap.Global` (`workspace.go:207-210`). `?`/`C-d ?` never shows them, and `[keys]` cannot change them, unlike the
sidebar's keys.

Fix: add a `Console` context with `console_find`/`console_copy`/`console_errors`/`console_menu` actions.

### P2-73 Deleting a saved chat has no confirmation
`internal/ui/m13_assistant.go:98-106`

`ctrl+x` in the chat picker calls `Chats.Delete` immediately, while saved-query and history deletes go through
`confirmDlg` (`m15_menus.go:216`, `:270`). The conversation file is gone with one keystroke.

Fix: push a `confirmDlg{danger: true}`.

### P2-74 Sidebar colours and diagram colours use different vocabularies in the same map
`internal/ui/m15_menus.go:158-178`, `internal/ui/m13_diagram.go:23-30`, `internal/ui/m3.go:682`

The sidebar stores hex swatches in `State.DiagramColors` ("so a table has one colour in both places"), but the
diagram resolves only palette names via `paletteCode`, and `m3.go` skips values it does not know. Setting a colour
from the sidebar therefore tints nothing in the ER diagram (the reverse happens to render because hex is a valid
ANSI sequence).

Fix: store one vocabulary (the ANSI code) in both directions.

### P2-75 `askSecret` blocks forever while the app is locked
`internal/ui/m15_lock.go:34-42`, `internal/ui/app.go:161-177`, `:238-240`

While a `lockDlg` is on the stack `a.push` drops every other overlay, but `askSecret` waits on `<-ans` with no
timeout and no dialog to answer it.

Trigger: set a passcode, press `alt+L`, then have an MCP client open a connection that needs a password — the tool
call hangs until the process exits (the TUI keeps working).

Fix: answer/dismiss queued secret forms while locked, or give `askSecret` a context that returns "locked".

### P2-76 A board "exporter" panel silently truncates at 100 000 rows
`internal/ui/m13_board.go:99-100`

`board.ExportPanel(…, 100000)` applies a row limit and the form exports only that result; nothing says the file is
partial. Table exports stream instead.

Fix: label the limit in the form/status, or stream like table exports.

### P2-77 Saving one setting deletes every comment in `config.toml`
`internal/ui/m15_settings.go:235-253`, `internal/config/m13_settings.go:84-93`

`ApplyValues` → `rewrite` re-encodes the TOML and writes a "comments are not kept" header; the dialog only reports
"saved N settings", while the `$EDITOR` path preserves comments.

Trigger: open `,`, toggle a bool, press `s` — hand-written comments are gone.

Fix: warn in the form (or refuse to rewrite a commented file and offer `e`).

### P2-78 Console keeps focus when its pane is not drawn
`internal/ui/m15_console.go:68-70`, `internal/ui/workspace.go:550-553`, `:1112`

`input()` routes keys while `focusConsole && showConsole`, but `layoutOf` hides the console below ~14 body lines and
nothing clears `focusConsole` on resize.

Trigger: focus the console, shrink the terminal: the pane vanishes but `/`, `y`, `e` still go to the invisible
widget until Esc.

Fix: clear `focusConsole` when the console height is 0.

### P2-79 Rebinding a key silently takes it from another action, which then disappears
`internal/ui/m15_settings.go:258-302`, `internal/keymap/prefix.go:78-90`

`Build` lets a user binding win over a default, so rebinding `rename_tab` to `alt+x` removes `cancel_queries`
without an error; the dialog reports success, and because the rows come from `Keys.All()` the vanished action can
no longer be reached to revert.

Fix: in the dry run, detect freed defaults and refuse or say "this takes the key from cancel_queries".

### P2-80 Two new tests cannot fail for the bug they are named after
`internal/ui/m9_tui_a_test.go:120-128`, `internal/ui/m15_app_test.go:523-543`

`TestM11TriggerEditorMakesTheSameScript` asserts `strings.Contains(want, "BEFORE")` on a string built from the test's
own `got`; the tab's text is never compared to `want`, so a `triggerSQL` that ignored `Timing` would pass.
`TestM15SettingsKeyConflictShown` uses `alt+x`, which is not a conflict — it is `cancel_queries`' key, silently
given away by the builder (see P2-79) — and asserts success, so the conflict path is untested. (Same family:
`m13_test.go:240` never exercises `pluginDetail`.)

Fix: compare the new tab's text with `want`; build a real conflict from two `[keys]` entries or a plugin binding.

---

## Checked and clean (so it is not re-reviewed)

- **Auth/routing** (`internal/web`): all 124 `HandleFunc` registrations go through `s.api(...)` except
  `POST /api/login` (own Origin + `Sec-Fetch-Site` check), the `/api/` 404 and static; constant-time token compare;
  `HttpOnly`/`SameSite=Strict`/`Secure`-under-TLS cookie; session map and `ss.live` under `s.mu`; reaper skips busy
  sessions.
- **Traversal/shell**: `queryPath`/`store.Queries.abs`, `backupFile`, `chatIDRe`+`Chats.file`, board lookup by
  enumeration, `export.Filename` sanitising, plugin names resolved against `plugin.List()`, `backup.Extra`
  allow-listed and run without a shell, `Kill` ids parsed, `reveal` argv-only.
- **WKB**: endianness, SRID, ISO Z/M, ring/point counts, all-bytes-consumed guard, WebP/VP8/VP8X sizes.
- **Import**: MySQL `PlanUser` limits whitelist (fixed keys, `%d` values), quote/delimiter/`null_as` plumbing,
  single transaction on pinned Postgres/MySQL.
- **Web**: `pending.ts` index alignment, `links.ts` encoding, `gridfind.ts` regex escaping, `m15_bin.ts` sniffing,
  `menus.ts` predicates, `m15_cfg.ts`/`m15_folds.ts` throwing storage; `/structure/plan`, `/plan/admin`,
  `/settings/keys`, `/passcode/check`, `/object/*`, `/jobs*` shapes all match the TS types; `tsc --noEmit` and the
  ten test files pass.
- **Export**: `Filename` traversal/sanitising (only the empty-token and Windows cases above are wrong).
- `internal/db/postgres/pin.go`'s transaction handling for the *importer* is correct; the problem is only the
  result-edit caller (P1-07).
