# webtuidb — review of uncommitted changes

**Scope:** every unstaged, staged and untracked change in the working tree (`git status --short`: 78 modified/deleted tracked files + 127 new files, ~29,000 added lines). `review-latest.md` itself was empty before this run; this document replaces it.

**Build state at review time:** `go build ./...`, `go vet ./...` and `go test ./...` are green on go1.27.3 darwin/arm64 (all six release targets also cross-compile; `go mod tidy -diff` is empty). The environment-gated integration tests are the exception: `TestIntegrationRetryAfterDroppedConnection` (P2-003) and `TestStreamPaging` (P2-008) fail when `WEBTUIDB_INTEGRATION=1` is set.

**How this was produced:** 19 parallel review passes over 17 file groups (db core, drivers, new db features, config/store, ui core, ui m2–m3, ui m4–m5, ui m6–m8, web server/auth, web handlers, web m2–m3, web m4–m7, mcp/ai/tools/plugin, export/importer/backup, frontend components, frontend routes/utils, keymap/docs, infra). The highest-severity claims were re-checked by hand against the source before being included; one reported P1 (a `CheckExpr` backslash bypass) was **disproved** on inspection — `expr.go:40` rejects `\` outright — and is excluded. Findings that could not be reproduced are marked `confidence: medium`.

**A note on the counts you asked for (100/100/100):** the review produced **18 genuine P1s, ~155 P2 candidates and ~300 P3 candidates**. Filling the P1 list to 100 would mean inventing 82 blockers, which would make the list useless for triage. So this document contains all 18 real P1s, the **100 highest-impact P2s**, and the **100 most actionable P3s**. The P2/P3 items that did not make the cut are all of the same kind as the ones listed (test gaps, hardening); nothing was dropped that would change a release decision.

**Severity definitions used**

| Tier | Meaning |
|---|---|
| **P1** | Data loss, silent data corruption, credential/secret exposure, remote code execution, or a crash that kills the TUI/web session. Fix before release. |
| **P2** | A real bug reachable by a normal user action or a documented feature, but with a workaround or a narrower trigger. Fix in the same release. |
| **P3** | Hardening, error-message quality, race windows requiring unusual timing, and test gaps that let P1/P2 classes regress. |

---

## P1 — fix before release (18)

### P1-01 · Result pane panics on the first repaint after a run
- **Where:** `internal/ui/tab_query.go:1042` (also `:567`), set at `:351`
- **Why:** `start()` sets `t.res, t.rc = keep, len(keep)`, so while a statement is in flight `t.rc == len(t.res)`. `resultsView` does an unguarded `rv := t.res[t.rc]`; `curResult()` has the bounds check, these two readers do not.
- **Trigger:** SQL tab → run `select 1` → focus results → `p` (pin) → `esc` → `enter` to run again. Panic `index out of range [1] with length 1`. Same with `[editor] result_mode = "append"` after two runs.
- **Fix:** clamp before indexing (`if t.rc >= len(t.res) { t.rc = len(t.res)-1 }`) or route both readers through `curResult()`; keep `t.rc = max(0, len(keep)-1)` in `start()`.
- **Confidence:** high (confirmed by reading the code paths).

### P1-02 · Folding a statement taller than the editor pane panics
- **Where:** `internal/ui/editor.go:1025`, cache sized at `:926`
- **Why:** with folds, the render loop jumps `ri = vis[first+k]` past `e.top+e.H`, but `kinds` is only lexed to `need := min(len(e.lines), e.top+e.H)`. The new guard checks the column (`ci < len(kinds[ri])`) but not the row.
- **Trigger:** paste two statements where the first spans more lines than the editor pane, put the cursor on it, press `alt+z` (fold). Panic `index out of range [31] with length 10`.
- **Fix:** guard the row (`var rowKinds []tokKind; if ri < len(kinds) { rowKinds = kinds[ri] }`) and size the cache from the last visible row.
- **Confidence:** high.

### P1-03 · `upper_case` / `lower_case` / `capitalize` on the word under the cursor wipes the buffer and auto-saves an empty file
- **Where:** `internal/ui/m5_query.go:23-24` (body at `:44`), `editor.go:1134-1141`
- **Why:** the guard accepts "word under cursor", but the body uses `s := e.Selection()`, which is `""` without a selection. `Replace("")` → `deleteSelection()` fails → `e.lines = []string{""}`; then `afterEdit()` writes the empty text through `Queries.Update(t.savedAs, "")`. The `.sql` file on disk is truncated; undo only restores the buffer.
- **Trigger:** open a saved query with `auto_save_queries` on, cursor on a word, no selection, run `upper_case`.
- **Fix:** `s := e.Selection(); if s == "" { s = e.wordAtCursor(); /* select its range */ }` and set the selection before `Replace`; add a case to the transform test (it only covers the selection path today).
- **Confidence:** high (verified `Replace` and `afterEdit`).

### P1-04 · Editing a connection in the welcome form silently drops `tools`, `options`, `bootstrap` and `source`
- **Where:** `internal/ui/welcome.go:553-566`
- **Why:** `build()` constructs a fresh `config.Connection` from form values and carries over only `DriverVersion`/`LazyLoad` (and `SSH.ProxyJump`). `save()` then replaces the stored entry. Any field the form has no widget for is lost with no warning.
- **Trigger:** set `tools.pg_dump = /opt/pg_dump` via the backup form, then press `e` on that connection, change only the group, Enter. `tools` is gone from both memory and `connections.toml`; the next backup silently falls back to `PATH`.
- **Fix:** start from `old` and overwrite only the fields the form owns (`c := old; c.Name = …`), or explicitly carry `Tools`, `Options`, `Bootstrap`, `Source`.
- **Confidence:** high (verified `build`).

### P1-05 · Assistant panics when the in-flight question is deleted from the message list
- **Where:** `internal/ui/assistant.go:154` (delete path at `m5.go:416-425`)
- **Why:** the failure branch does `w.m3.chat = w.m3.chat[:len(w.m3.chat)-1]` with no length guard. The messages dialog allows `d` on any entry, including the only (user) message, while a request is running.
- **Trigger:** type "hi" + Enter, `ctrl+t`, `d` on the only message, `esc`. Panic `slice bounds out of range [:-1]` inside `Update` — the TUI exits. On success the reply is appended to a history that no longer holds the question.
- **Fix:** remember the index/id of the appended question and remove only that entry if still present; refuse `d`/resend on the last message while `t.running`.
- **Confidence:** high (verified both sites).

### P1-06 · `column=%00raw` in a launch URL injects SQL into the table-browser WHERE clause
- **Where:** `internal/ui/m5.go:111-128`, `internal/db/filter.go:12,100-107`
- **Why:** `q.Get` decodes `%00`, so `Column` becomes `db.RawColumn`; `openTarget` copies it into a filter without validation, and `FilterSet.Where` pastes `Value` verbatim for `RawColumn`. Every other value is a bound parameter; a pasted/shared URL is the one path where the filter value becomes SQL.
- **Trigger:** `webtuidb 'postgres://u@h/db?table=customers&column=%00raw&value=1%3D1%20OR%20(select%20pg_sleep(30))is%20null'`.
- **Fix:** reject `db.RawColumn` (and control characters) in `openTarget`, and look the column up in the table's columns before building the filter.
- **Confidence:** high.

### P1-07 · Legacy history-file adoption steals another connection's history
- **Where:** `internal/store/history.go:41-61`
- **Why:** `path()` renames `<unsafeName(conn)>.jsonl` to the encoded name whenever the encoded file is missing, with no check that `<mangled>` belongs to a different, existing connection. It runs from read paths too.
- **Trigger:** connection `prod_db` has history; the user creates `prod db`. `path("prod db")` renames `prod_db.jsonl` → `prod%20db.jsonl`. Verified: `List("prod_db")` returns 0, and `prod db` inherits the whole history. Reachable from the web via `GET /api/history?conn=a%20b`.
- **Fix:** migrate once at startup with the connection list in hand (skip any legacy name that matches a live connection), and make `path()` a pure function with no `Stat`/`Rename`.
- **Confidence:** high (reproduced).

### P1-08 · Setting `encoding` on a Postgres connection silently double-encodes writes
- **Where:** `internal/db/postgres/postgres.go:97-99`
- **Why:** pgx does no transcoding between `client_encoding` and UTF-8; passing `client_encoding=LATIN1` makes the server transcode while the client still treats bytes as UTF-8, so a round trip through a non-ASCII value corrupts it. The connection form offers `encoding` for every server driver.
- **Trigger:** `encoding = "latin1"`, then `INSERT INTO t VALUES ('café')`; it reads back as `cafÃ©`. Reads of existing Latin-1 rows return raw bytes in a Go string.
- **Fix:** reject non-UTF-8 encodings in the postgres driver (or hide the field for postgres); leave `Encoding` to MySQL/ClickHouse, which honour it.
- **Confidence:** high (measured by the reviewer against PG 16).

### P1-09 · CSV export emits live spreadsheet formulas — `SafeFormulas` is dead code
- **Where:** `internal/export/export.go:206` (option declared at `:35`)
- **Why:** `CSVField` quotes only for delimiter/quote/CR/LF, so cells starting with `=`, `+`, `-`, `@` are written verbatim. `Options.SafeFormulas` is documented as "would prefix…" and is never read anywhere (`grep` confirms).
- **Trigger:** export a table containing `=cmd|' /C calc'!A0` (untrusted DB content) and open the CSV in Excel/LibreOffice — formula/DDE execution.
- **Fix:** prefix `= + - @ \t \r` with `'` before quoting, implement `SafeFormulas`, default it on for CSV. (XLSX already writes strings, not formulas.)
- **Confidence:** high (verified option is unused).

### P1-10 · Export leaves an unclosed gzip stream / truncated JSON on any error
- **Where:** `internal/export/export.go:54-94` (also `:182`)
- **Why:** `Write`/`Table` return before `s.finish()`, so the gzip writer is never flushed and buffered formats never get their terminator. The caller cannot tell a good file from a partial one.
- **Trigger:** a table export with `gzip:true` where page 2 fails; the destination holds a bare 10-byte gzip header and `gzip.NewReader` fails. JSON ends without `]`.
- **Fix:** `defer` the finish path, return a joined error, and/or write to a `.part` name that the caller renames on success (as `internal/backup` does).
- **Confidence:** high.

### P1-11 · `sqlfmt` panics on an empty statement in tabular mode
- **Where:** `internal/sqlfmt/sqlfmt.go:344` (guard at `:339`, `tabular` at `tabular.go:26`)
- **Why:** in tabular mode `s` is replaced by `tabular(s)`, which returns a whitespace-only string for an empty statement, so the `s == ""` guard no longer fires; the next line indexes `cur[len(cur)-1]` with `cur` empty.
- **Trigger:** `Beautify(";", {IndentType:"tabular_left"})`, or formatting a selection with `;;` / a leading `;` — reachable from the editor's format action.
- **Fix:** `if len(cur) == 0 || s == ""` before indexing, and make `tabular("")` return `""`.
- **Confidence:** high.

### P1-12 · `PUT /api/queries/{path}` with an empty body erases the saved SQL
- **Where:** `internal/web/handlers_store.go:71` (via `store/queries.go:190-200`)
- **Why:** `decode` succeeds with an empty body, `req.SQL == ""`, and `Queries.Update` rewrites the file as just the `-- name:` header. No `If-Match`/mtime check either, so two tabs editing the same saved query also clobber each other.
- **Trigger:** `curl -X PUT .../api/queries/reports/daily.sql -d ''` (or a client with a typo'd field name — unknown JSON fields are ignored).
- **Fix:** reject a zero-length body and require `SQL` to be present (pointer field); add an ETag/mtime precondition returning 409.
- **Confidence:** high.

### P1-13 · A read-only connection is still writable through the web UI on drivers that do not self-guard
- **Where:** `internal/web/handlers.go:520-720` (`plan`, `apply`, `bulkUpdate`, `fakeRows`, `rows`); guards exist only in `internal/db/postgres/exec.go:325` and `internal/db/mysql/mysql.go:711`
- **Why:** the web layer never consults `ss.conn.ReadOnly`. For Redis/Mongo/Cassandra/ClickHouse the plan is executed as-is, while the UI badge promises every data-changing statement is refused.
- **Trigger:** mark a Mongo or Redis connection `read_only = true`, edit a cell in the web grid, apply. The write goes through.
- **Fix:** gate at the web layer (where the flag lives): in `execPrep`, `plan`, `planAdmin`, `apply`, `bulkUpdate`, `fakeRows`, return 403 when `conn.ReadOnly` and any statement is not `db.IsReadOnly`; set `editable:false` in `rows`.
- **Confidence:** high for the missing check; medium only on how many drivers are affected (postgres/mysql do refuse).

### P1-14 · Import reloads keep index-keyed edits, so a later commit writes the wrong row
- **Where:** `web/src/components/DataTab.tsx:397` (state at `:96-101`, keys at `:175-181`)
- **Why:** `edits`/`deletes` are keyed by row index and survive any refetch that does not go through `guard()`/`clearPending()`; `ImportDialog onDone` calls `setNonce` directly. `keyOfRow()` then rebuilds the WHERE key from the *new* snapshot, so the server's stale-row check passes.
- **Trigger:** edit `r3.status`, mark `r5` for deletion, run an import into the same table, then "Send changes": the UPDATE/DELETE land on whatever rows now sit at indices 3 and 5.
- **Fix:** route every out-of-band reload through `guard()`/`clearPending()`, and key edits by primary-key value instead of array index.
- **Confidence:** high.

### P1-15 · Applying an unrelated plan silently discards staged cell edits
- **Where:** `web/src/components/DataTab.tsx:185`
- **Why:** `applied()` is the `onApplied` for every `PlanDialog` opened from the tab (column edit, bulk update, move column), not just the grid's own commit, and it calls `clearPending()`.
- **Trigger:** stage three cell edits, open Bulk update, apply the previewed UPDATE — the staged edits vanish with only "Changes sent".
- **Fix:** pass the plan source (`applied(kind)`), clear only for `commit`/`send`, otherwise warn that pending changes were kept/discarded; or block tools while `pending > 0`.
- **Confidence:** high.

### P1-16 · Every `alt+Shift+letter` binding is unreachable in both UIs
- **Where:** `internal/keymap/action.go:83,87,93,95,120`; `internal/keymap/msg.go:10,22-28`; `web/src/utils/keys.ts:19-25`
- **Why:** `Canonical` only maps `^shift\+([a-z])$`. ultraviolet reports `Option+Shift+K` as `alt+shift+k`, and the web's `canon` keeps `shiftKey` with the alt branch, producing `alt+shift+k`. The tables bind `alt+K`, so `Lookup` misses.
- **Trigger:** press `Ctrl+d` `Option+Shift+K` (`connection_details`), `alt+W` (close other connections), `alt+Q` (open recent), `alt+O`, `alt+V` (cleanup variables) — help lists them, nothing happens; the status line says "no binding for that key after the prefix".
- **Fix:** fold shift into the letter for alt/ctrl combos (`alt+shift+k` → `alt+K`) in both `Canonical` and `canon`, and add a `FromMsg` test for that form.
- **Confidence:** high (verified `Canonical`).

### P1-17 · `close_all_tabs` is bound to `ctrl+w`, which the browser cannot intercept
- **Where:** `internal/keymap/action.go:84`; `web/src/routes/Workspace.tsx:294`
- **Why:** Ctrl+W / Ctrl+Shift+W are reserved; `preventDefault` is ignored. Instead of closing tabs the browser window closes, taking every web tab and its pending grid edits with it (only `beforeunload` stands in the way).
- **Trigger:** web UI, `Ctrl+d` then `Ctrl+W` with three tabs of unsent edits.
- **Fix:** pick a different web key for `close_all_tabs` (e.g. `alt+A`) and say so in the web help, per CLAUDE.md's "a terminal key the browser swallows is not skipped silently".
- **Confidence:** high.

### P1-18 · `split_down` (`alt+"`) can never fire in the web UI
- **Where:** `internal/keymap/action.go:92`; `web/src/utils/keys.ts:19-25`
- **Why:** `canon` only uses `e.code` when it matches `^(Key|Digit)`; `Quote` falls through to the composed character, which on a US Mac layout is `Æ` for `Option+Shift+'`, giving `alt+Æ`. The action is advertised in `SUPPORTED` and implemented in `QueryTab.tsx:206`.
- **Trigger:** web query tab, `Ctrl+d` `Option+Shift+'` — nothing happens (the same root cause already breaks `alt+.` = `≥` on macOS).
- **Fix:** extend the `e.code` branch to punctuation with a code→char table, or bind split-down to a letter.
- **Confidence:** high on macOS, medium on Windows layouts.

---

## P2 — the 100 highest-impact (fix in this release)

### P2-001 · Paged "load more" can repeat or skip rows because ORDER BY is buried in a derived table
- **Where:** `internal/db/page.go:35` (used by `exec/page`)
- **Why/trigger:** the wrapper is `SELECT * FROM (<stmt>) AS x LIMIT n OFFSET m`. Postgres may discard an ORDER BY the outer query does not need and MySQL merges it away, so `SELECT … ORDER BY created_at DESC` + "load more" returns overlapping/missing rows. The guard rejects LIMIT/OFFSET/FOR/INTO but not ORDER BY.
- **Fix:** hoist a trailing top-level `ORDER BY` outside the derived table, or decline to page statements that have one.
- **Confidence:** medium.

### P2-002 · A ProxyJump bastion is dialled with the target's user/key and never gets its own ssh_config block
- **Where:** `internal/db/ssh.go:230-244`
- **Why/trigger:** `hop := *cfg` copies the target's `User`/`Key`, `Port` comes only from the `user@host:port` spec, and `ProxyJump = "none"` suppresses the ssh_config lookup. `~/.ssh/config` with `Host bastion / User ubuntu / Port 2222 / IdentityFile …` is the common setup; the tunnel fails with an opaque auth/DNS error.
- **Fix:** resolve each hop through the ssh_config reader, default the hop's user to the local user, and let the spec override.
- **Confidence:** medium.

### P2-003 · Retry-after-dropped-connection never fires; the new integration test cannot pass
- **Where:** `internal/db/postgres/postgres.go:386-418`
- **Why/trigger:** `retryable` requires `pgconn.SafeToRetry`, which is false for a terminated backend's `57P01` ErrorResponse and for a bare EOF (which `dropped` treats as not retryable). `WEBTUIDB_INTEGRATION=1 go test -run TestIntegrationRetryAfterDroppedConnection` fails on the first query after the kill. The EOF/EPIPE/ECONNRESET branches are dead code.
- **Fix:** treat connection-fatal `57P0x` server errors (and idle-connection EOF) as retryable for one attempt, or ping idle connections before reuse; then align the unit test with the integration test.
- **Confidence:** high.

### P2-004 · `Users()` blocks ~11 s on a stock Postgres cluster
- **Where:** `internal/db/postgres/users.go:81-95`
- **Why/trigger:** the correlated `NOT EXISTS` over `information_schema.role_table_grants` is replanned per row of `role_column_grants`; the integration test takes 11 s and yields zero usable rows after filtering.
- **Fix:** read ACLs from `pg_class`/`aclexplode` directly and exclude system schemas.
- **Confidence:** high.

### P2-005 · A pinned Postgres Exec still cancels the statement, aborting the transaction
- **Where:** `internal/db/postgres/pin.go:44-58`
- **Why/trigger:** the limit+1 trick only applies when `PageStatement` wraps the SQL. For `SELECT … FOR UPDATE` or a statement with its own LIMIT, `fetch` cancels the query context; the next statement fails with `25P02 current transaction is aborted`.
- **Fix:** never cancel on a pinned connection — read `limit` rows and `rows.Close()`, or always wrap read-only statements (nested LIMIT is legal).
- **Confidence:** high (measured).

### P2-006 · MySQL `Pin` builds its pool on the parent, so the single-connection guard is never installed
- **Where:** `internal/db/mysql/pin.go:22` vs `mysql.go:206`
- **Why/trigger:** `pool, err := s.openPool(ctx, name)` runs on the parent whose `pinned` flag is false, so `onceConnector` is skipped. If the pinned physical connection drops, the next statement silently runs on a new connection outside the lost transaction, and a later `ROLLBACK` "succeeds" while the INSERT stays committed.
- **Fix:** `p.openPool(...)`; add an integration test that kills the pinned connection and asserts the next Exec errors.
- **Confidence:** high.

### P2-007 · `pinned.Apply` runs DDL inside the user's transaction, which MySQL implicitly commits
- **Where:** `internal/db/mysql/pin.go:61-72`
- **Why/trigger:** `session.Apply` normally routes DDL to `applyDDL` (no transaction); the pinned override executes everything on the connection holding the user's BEGIN, so MySQL commits the open transaction around the DDL — a `ROLLBACK` afterwards undoes nothing.
- **Fix:** detect DDL in `pinned.Apply` and refuse ("release the pin; it would commit your transaction") or route it through `applyDDL` with a warning.
- **Confidence:** high.

### P2-008 · The new Redis paging assertion always fails (operator precedence)
- **Where:** `internal/db/redis/redis_integration_test.go:572`
- **Why/trigger:** `off == 10 && … || marks[key][1].from != "(ids[19]"` — the second comparison runs on every iteration against the final page's mark, so `-run TestStreamPaging` reports failure at `off == 0`.
- **Fix:** wrap the `||` in the `off == 10 && ( … )` guard.
- **Confidence:** high.

### P2-009 · EXPLAIN is chosen from `Dialect`, so Redshift, Vertica and SingleStore get syntax they reject
- **Where:** `internal/db/explain.go:11-21` (callers `ui/m5_query.go:122`, `web/m4_tools.go:38`)
- **Why/trigger:** the new profiles report `Dialect: "postgres"`/`"mysql"`, so Redshift receives `EXPLAIN (FORMAT JSON, ANALYZE false)` and SingleStore `EXPLAIN FORMAT=JSON`; both are syntax errors, so Explain never works on those drivers.
- **Fix:** key on the driver name or add a `Capabilities.Explain` mode per profile, empty where unsupported.
- **Confidence:** high.

### P2-010 · Vertica is registered as a full PostgreSQL session but its catalogs are `v_catalog`/`v_monitor`
- **Where:** `internal/db/vertica/profile.go:11`
- **Why/trigger:** the reused session queries `pg_namespace`, `pg_class`, `pg_attribute`, `pg_get_viewdef`; none exist on Vertica, so the sidebar, structure and DDL tabs error on every object although the driver is offered in the connection form.
- **Fix:** implement a Vertica session against `v_catalog`, or remove the profile until it exists.
- **Confidence:** high.

### P2-011 · The Vertica profile keeps `Transactions: true`, but Vertica has no BEGIN
- **Where:** `internal/db/vertica/profile.go:13`, `postgres/exec.go:335-348`
- **Why/trigger:** `postgres.Apply` opens a transaction unconditionally and never reads the capability, so every structural/data edit fails with "begin: syntax error"; the UI also reports the edits as atomic.
- **Fix:** set `Transactions = false` in the adjust **and** make `Apply` honour the flag.
- **Confidence:** high.

### P2-012 · `DISCARD ALL` on the new Postgres-family profiles makes every statement reconnect
- **Where:** `internal/db/postgres/postgres.go:364-378`
- **Why/trigger:** `put` always resets a dirty connection; Vertica (and probably Redshift) reject `DISCARD ALL`, so `reset` returns false and the connection is closed instead of pooled. Result: a full handshake (and an IAM token) per statement, silently.
- **Fix:** make the reset statement per-driver (empty = skip) and log when a reset fails.
- **Confidence:** high.

### P2-013 · The Redshift profile leaves `Indexes: true` on an engine with no indexes
- **Where:** `internal/db/redshift/profile.go:13`
- **Why/trigger:** the structure editor offers index editing and the planner emits `CREATE INDEX`, which Redshift rejects; schema diff queries `pg_am`/`pg_index`.
- **Fix:** clear `Indexes` (and gate schema diff on the capability).
- **Confidence:** high.

### P2-014 · The Redshift profile keeps `ProcessList: true`, but `pg_stat_activity` does not exist there
- **Where:** `internal/db/redshift/profile.go:13`, `postgres/exec.go:377`
- **Why/trigger:** after a cancel the UI opens the process list and gets a second error toast about a missing relation; the feature is permanently broken on Redshift.
- **Fix:** clear `ProcessList` until a per-engine query exists.
- **Confidence:** high.

### P2-015 · SingleStore is registered with no capability adjust, so it advertises triggers and foreign keys
- **Where:** `internal/db/singlestore/profile.go:11`
- **Why/trigger:** SingleStore documents both as unsupported. The trigger menu is dead and an FK edit plans `ADD CONSTRAINT … FOREIGN KEY`, which fails unless `ignore_foreign_key` is set; existing FKs are not enforced, so the diagram shows relationships that do not hold.
- **Fix:** clear `Triggers` and `ForeignKeys` in an adjust function.
- **Confidence:** high.

### P2-016 · `iam_auth` is offered for every server driver but always mints an RDS token
- **Where:** `internal/db/iam/iam.go:18-35`; checkbox shown for `Show: server` at `ui/welcome.go:464`
- **Why/trigger:** `auth.BuildAuthToken` is the RDS signer. Ticking it on Redshift, Greenplum, Vertica or SingleStore produces a token that cannot authenticate, and the error comes from the server rather than from the app.
- **Fix:** show the field only for `postgres`/`mysql` (RDS) and reject it elsewhere with a clear message.
- **Confidence:** medium-high.

### P2-017 · Charts never render DynamoDB decimal metrics (`json.Number` not handled)
- **Where:** `internal/db/chart.go:63-78`
- **Why/trigger:** DynamoDB keeps non-integer numbers as `json.Number`; `cellNum`'s type switch has no case for it, so an inferred numeric column is "not numeric" and `Chart` fails with "need one label column and one numeric column".
- **Fix:** add `case json.Number` (and `float32`/`int`/`int32`) to `cellNum`.
- **Confidence:** high.

### P2-018 · `Chart` prefers a stale `Ys` over an explicit `Y`, so the board's Y override is ignored
- **Where:** `internal/db/chart.go:132-135`; caller `internal/web/m3.go:315-323`
- **Why/trigger:** `ParseChart` sets `Ys` and `Y`; `boardSave` overwrites only `Y`, and `Chart` uses `Ys` when non-empty. Saving `y: b` over a query comment `y: a` still charts `a`.
- **Fix:** make an explicit `Y` authoritative, or set `Ys` in the handler.
- **Confidence:** high.

### P2-019 · `board.FromChart` drops every Y series after the first
- **Where:** `internal/board/board.go:186-203`
- **Why/trigger:** it serializes only `spec.X`/`spec.Y`, never `Ys`, so a multi-series chart added to the board re-parses as single-series and the extra columns disappear on save/load.
- **Fix:** serialize `strings.Join(spec.Ys, ", ")` and add a round-trip test with two Y columns.
- **Confidence:** high.

### P2-020 · `board.Save` takes a value receiver, so it cannot record the path it chose
- **Where:** `internal/board/board.go:76-90`
- **Why/trigger:** `b.Path = …` mutates the copy; the caller's board keeps `Path == ""`, so the next save re-enters the naming branch and fails with `board "Sales" already exists`. The doc comment claims the opposite.
- **Fix:** `func Save(b *Board) error`.
- **Confidence:** high.

### P2-021 · Mongo boards silently run with unbound `$vars`
- **Where:** `internal/board/board.go:160`
- **Why/trigger:** binding (and the unknown-variable check) is skipped for mongo, so a panel with `find({region: "$region"})` executes literally, returns nothing, and the UI shows the selected value as if it had been applied.
- **Fix:** bind for mongo too, or return an explicit error when a declared variable is not substituted.
- **Confidence:** high.

### P2-022 · Diagram boxes misalign for wide runes (CJK, emoji)
- **Where:** `internal/diagram/diagram.go:110`
- **Why/trigger:** width and padding use `utf8.RuneCountInString`, so each CJK rune counts 1 while the terminal spends 2 columns; every box containing a wide character has a ragged right border and the row layout after it shifts.
- **Fix:** use a display-width helper (go-runewidth or an east-asian-width table) for width and padding.
- **Confidence:** high.

### P2-023 · A relative `[queries] shared_dirs` entry makes Read/Update miss the file and bypasses the read-only flag
- **Where:** `internal/store/queries.go:39-46,168-187`
- **Why/trigger:** `AddDir` accepts a relative path and `List` stores it, but `sharedAbs` requires an absolute path. `Read("team/x.sql")` falls back to the store root (ENOENT), or, if `<Root>/team` exists, reads the store copy with `Shared:false` — so the UI's read-only guard is skipped and writes land in the data dir.
- **Fix:** `AddDir` should expand `~` and `filepath.Abs`, or return an error; `List` should store absolute paths.
- **Confidence:** high (verified).

### P2-024 · `Queries.Update` throws away the `abs()` error and writes its temp file into the process CWD
- **Where:** `internal/store/queries.go:190-197`
- **Why/trigger:** `p, _ := s.abs(path)` yields `""`, then `WriteFileAtomic("")` creates a temp file in the current directory containing user SQL and fails at rename with "no such file or directory". Reachable for any shared path.
- **Fix:** propagate the error and return "shared queries are read-only" before touching the filesystem; make `WriteFileAtomic` reject an empty path.
- **Confidence:** high (reproduced).

### P2-025 · `Queries.Update` is an unlocked read-modify-write, so TUI and web edits lose each other
- **Where:** `internal/store/queries.go:190-197`
- **Why/trigger:** both UIs read the file to keep the `-- name:` header, then atomically rewrite the whole file. Last writer wins, including a stale header. Every other shared file in the store takes `config.LockFile`; query files take none.
- **Fix:** lock around the read-modify-write and/or add an mtime/ETag precondition.
- **Confidence:** high.

### P2-026 · `SaveIfAbsent` uses stat-then-write, so its 409 can be missed
- **Where:** `internal/store/queries.go:154-156`
- **Why/trigger:** two tabs POST the same new query name; both `Stat` miss, both write, one silently overwrites the other and both return 200.
- **Fix:** create with `O_CREATE|O_EXCL` and map `EEXIST` to `ErrExists`.
- **Confidence:** medium.

### P2-027 · `State.do` re-marshals a struct that cannot preserve unknown keys, so an older binary wipes the newer state
- **Where:** `internal/store/state.go:91-98`
- **Why/trigger:** a new build writes `open_tabs`, `folds`, `groups`, `backup_options`; a web/TUI process from the previous build then saves anything and rewrites the file from its own struct, dropping everything it does not know. Same on downgrade.
- **Fix:** merge at the JSON level (`map[string]json.RawMessage`, replace only owned keys) or add a `version` field and refuse to write a newer file.
- **Confidence:** medium.

### P2-028 · `[ai.providers.<name>].provider` is never validated, so a typo silently selects the other vendor
- **Where:** `internal/config/ai.go:20-36` (validation only at `config.go:277`)
- **Why/trigger:** `provider = "OpenAI"` or `"opneai"` falls through `cfg.Provider == "openai"` and is sent to `api.anthropic.com` with that provider's key. A provider that sets only `base_url` keeps `Provider:"anthropic"`, so an Ollama endpoint receives Anthropic wire protocol.
- **Fix:** validate every provider name against `{anthropic, openai}` in `validate()` and `AIFor`.
- **Confidence:** high.

### P2-029 · `config.toml` now holds several plaintext secrets but is never permission-checked
- **Where:** `internal/config/config.go:119-127,148-156,166-181`
- **Why/trigger:** `[web] password`, `[ai] api_key` and `[[mcp.token]] value` live in a file created 0644 under a default umask, while `connections.toml` is chmod'ed to 0600 or refused.
- **Fix:** if any of those fields is set and the file is group/world-readable, chmod 0600 (or return an actionable error naming the file).
- **Confidence:** high.

### P2-030 · `WriteFileAtomic` leaks the temp file handle on every error path
- **Where:** `internal/config/connections.go:310-337`
- **Why/trigger:** on a write/sync/chmod error the function returns without `tmp.Close()`; only the `os.Remove` is deferred. `state.json` is written on nearly every UI action, so a full disk leaks an fd per save; on Windows the remove of an open handle fails, leaving temp files that may contain SQL/passwords.
- **Fix:** `defer tmp.Close()` (idempotent, ignore the double-close) before the writes.
- **Confidence:** high.

### P2-031 · Portable import keeps `tools` paths, so a shared connections file can point the app at an arbitrary executable
- **Where:** `internal/config/portable.go:131-137,153-163`
- **Why/trigger:** `ToolPathOK` only checks "absolute, no `..`, basename is `pg_dump`/`pg_dump-<ver>`". A file with `tools.pg_dump = /tmp/evil/pg_dump` is imported even with `allowCommands=false` (which does drop `password_cmd`/`bootstrap`), and the next backup runs it.
- **Fix:** drop `Tools` on import unless `allowCommands`, or require the path to be inside a trusted, non-writable-by-others prefix.
- **Confidence:** high.

### P2-032 · Removing the SQLite/DuckDB/Cockroach/D1 drivers makes an existing `connections.toml` unloadable at startup
- **Where:** `internal/config/connections.go:46-48,116-122`; `main.go:142`
- **Why/trigger:** an upgrading user with `driver="sqlite"` gets `unknown driver "sqlite"` from `Validate`; `LoadAllConnections` fails, `main` exits 1 before any UI, and every other connection is unreachable — including `--frontend` and `--mcp`. The test fixture was changed from SQLite to Postgres, so the upgrade path is untested.
- **Fix:** skip unsupported-driver entries with a collected warning instead of failing the file, or keep them valid-but-unconnectable; add a legacy-entry test.
- **Confidence:** high.

### P2-033 · History trimming is keyed on the last line's size, so the cap is entries-based and the file can reach hundreds of MB
- **Where:** `internal/store/history.go:99-104`
- **Why/trigger:** `st.Size() > int64(n)*maxEntries*2` where `n` is the just-written entry. A 50 KB INSERT means the trim only triggers above ~500 MB; `List` then parses all of it, and each trim buffers the whole file.
- **Fix:** trim on a byte cap as well as an entry count, and truncate very large `SQL` (as `console.go` already does at 64 KB).
- **Confidence:** high.

### P2-034 · `SaveConnections` silently skips the merge when the on-disk file does not parse
- **Where:** `internal/config/connections.go:277-290`
- **Why/trigger:** if `connections.toml` was hand-edited into invalid TOML, the `if err == nil` guard skips the merge and the caller's list overwrites the file — every connection another process added is gone.
- **Fix:** on a decode error, refuse to write and return the error.
- **Confidence:** high.

### P2-035 · A cancelled "load more" leaves the doubled row limit sticky for the whole tab
- **Where:** `internal/ui/tab_query.go:371`; set at `m2_query.go:194`, reset only at `:450`
- **Why/trigger:** `start()` resets `t.replace` but not `t.override`, and `Cancel()` resets neither. After a cancelled load-more, every later statement in that tab runs with the doubled limit.
- **Fix:** clear `t.override` in `start()` and `Cancel()`.
- **Confidence:** medium-high.

### P2-036 · Commits from a pinned tab run on the shared session, not the pin
- **Where:** `internal/ui/resultedit.go:170,194`; `workspace.go:333`
- **Why/trigger:** `Statements()`/`Commit()` use `w.sess`, while the tab's reads use `t.sess()`. An open BEGIN, temp tables or session GUCs on the pin do not apply to the write, and the pinned transaction never sees it.
- **Fix:** give the commit path an explicit `db.Session` and pass `t.sess()`.
- **Confidence:** medium-high.

### P2-037 · MCP server failures after the listener binds are silently ignored
- **Where:** `internal/ui/app.go:80-82`
- **Why/trigger:** the `go http.Serve/ServeTLS` return values are discarded. A bad `tls_cert` surfaces at `ServeTLS` time, so the app starts, no error is shown, and the activity pane claims MCP is listening while every client fails.
- **Fix:** capture the error and route it to the UI (`a.mcpErr`, toast/log).
- **Confidence:** medium-high.

### P2-038 · "Load more" discards pending result edits without asking
- **Where:** `internal/ui/m2_query.go:184-197`
- **Why/trigger:** `loadMore` sets `t.replace` and re-runs; `Handle` then replaces the grid, including uncommitted cell edits. Every other path that replaces results pushes "Discard pending result edits?" first.
- **Fix:** reuse the same guard before setting `t.override`.
- **Confidence:** high.

### P2-039 · Export uses filter values that were typed but never applied
- **Where:** `internal/ui/m2.go:112-125`; `filterbar.go:190`
- **Why/trigger:** the filter bar writes every keystroke into `t.filters`, and `exportAction` snapshots it. Type `Ann` into the value field without pressing Enter, export the table: the file is filtered by `Ann` while the grid still shows all rows.
- **Fix:** keep in-progress edits in `fb.edit` only and copy them into `t.filters` on Enter (or export a "last applied" snapshot).
- **Confidence:** high.

### P2-040 · Query variable values are written to history in cleartext
- **Where:** `internal/ui/m2_query.go:40-47`
- **Why/trigger:** `bindAs` substitutes literals into the statement (`db.Bind`), and that text is what `w.record` writes to the console and `<conn>.jsonl`. A password typed into the Variables dialog is persisted forever and shown on screen.
- **Fix:** record the unbound text (plus variable names, values masked) and pass only the bound text to `Exec`.
- **Confidence:** high.

### P2-041 · `liveCmd` results are applied without a generation or closed-workspace check
- **Where:** `internal/ui/m3.go:647-663,271-292,824-844`; `m2.go:294-304`
- **Why/trigger:** press `d` (diagram) then switch database or disconnect; the callback runs later and pushes a dialog built from the previous database, on top of the picker or the welcome screen. `wsMsg` results are generation-checked; these are not.
- **Fix:** capture `gen := w.gen` and bail in the callback when `w.closed || gen != w.gen`.
- **Confidence:** high.

### P2-042 · Vim NORMAL mode still edits the buffer
- **Where:** `internal/ui/m3_editor.go:148-164`
- **Why/trigger:** `vimKey` returns false for keys with empty `Text` (Enter, Tab, Backspace, Delete), so `handleKey` falls through and inserts a newline / deletes a character / indents while the `[N]` badge is shown. The same fall-through accepts the completion popup in normal mode.
- **Fix:** handle the text-less editing keys in normal mode (or return true and ignore), and close `e.ac` on mode change.
- **Confidence:** high.

### P2-043 · `api_key_cmd` runs on the UI goroutine and freezes the TUI for up to 30 s
- **Where:** `internal/ui/assistant.go:115`
- **Why/trigger:** `send()` calls `config.ResolveAIKey` synchronously; that runs the command with a 30 s timeout, so with `op read`/`aws ssm` waiting on unlock the spinner stops and Esc does nothing.
- **Fix:** resolve the key inside the existing `liveCmd` goroutine, or cache it per provider when the tab opens.
- **Confidence:** high.

### P2-044 · Model output is rendered to the terminal without stripping control sequences
- **Where:** `internal/ui/assistant.go:216-242`
- **Why/trigger:** a table comment or model reply containing CSI/OSC sequences (e.g. `\x1b]52;c;…\x07`) is printed verbatim, allowing clipboard writes and title/cursor manipulation. The prompt labels DB content as untrusted but the output side never sanitizes.
- **Fix:** `ansi.Strip` and drop control bytes before appending the reply to the transcript.
- **Confidence:** medium.

### P2-045 · A panic in a background job wedges the workspace until restart
- **Where:** `internal/ui/m2.go:48-65`
- **Why/trigger:** `w.jobCancel` is cleared and `close(ch)` happens only in the final callback inside the goroutine. If `work` panics, `w.jobCancel` stays non-nil and `busy()` stays true: every later export/import/search says "another … is still running" and the spinner never stops.
- **Fix:** `defer func(){ recover(); close(ch) }()` plus a final callback that clears the busy state.
- **Confidence:** medium-high.

### P2-046 · A finished job can be reported as "stopped" and skip its follow-up work
- **Where:** `internal/ui/m2.go:50-63`
- **Why/trigger:** `stopped := ctx.Err() != nil` is evaluated when the callback runs, not when the work finished, so cancelling in that window marks a complete export as stopped and skips `then()` — e.g. the sidebar is never reloaded after an import created a table.
- **Fix:** capture `done := ctx.Err()` in the goroutine before sending.
- **Confidence:** medium.

### P2-047 · "All result tabs" export confirms the overwrite of only the first file
- **Where:** `internal/ui/m2.go:196-198`, `m4.go:182`
- **Why/trigger:** the caller wraps the job in `ifFree(run, numberedPath(path, 1))`, but `runExportAll` writes `out-1…out-N`. `out-2.csv` and later are replaced with no prompt; if `out-1` was deleted, nothing is asked at all.
- **Fix:** compute all target paths and pass the whole list to `ifFree`.
- **Confidence:** high.

### P2-048 · Import mapping allows two source columns to target one table column
- **Where:** `internal/ui/m2.go:345-380`
- **Why/trigger:** `mapColumns` never checks for duplicates. Mapping `email` → `name` (with `name` already mapped) produces `INSERT … ("name","name")` or `CREATE TABLE (name TEXT, name TEXT)`, and the job fails after several committed batches.
- **Fix:** reject duplicate targets in the submit handler with a message naming both source columns.
- **Confidence:** high.

### P2-049 · Two assistant tabs share one chat and can run two requests at once
- **Where:** `internal/ui/assistant.go:29-37`
- **Why/trigger:** `assistantAction` reuses a tab only in the current pane; opening one per pane gives two tabs over the same `w.m3.chat` with per-tab `running`/`cancel`. Replies interleave and one pane's Esc cancels only its own request.
- **Fix:** keep one assistant tab per workspace, or move `running`/`cancel` to workspace state and refuse a second send.
- **Confidence:** medium.

### P2-050 · An unresolved `@mention` is dropped without telling the user
- **Where:** `internal/ui/assistant.go:122-130`
- **Why/trigger:** refs come only from `w.sb.objs`; a mention of a table in another schema, or before the sidebar loads, matches nothing and the request goes out with no DDL — the model then invents columns while the user believes the table was included.
- **Fix:** collect unmatched names and report "no table x in this schema: mention skipped" before sending.

### P2-051 · "Sort by" cannot express the priority order it promises
- **Where:** `internal/ui/m4_grid.go:37-43`
- **Why/trigger:** `picked` is built from the checklist's item order, and `checklistDlg` only toggles in place — there is no move up/down. Ticking `email`, then `name`, then Enter sorts by table column order, not by the order ticked, contradicting the title. The test happens to tick in column order.
- **Fix:** add reorder keys to the checklist, or derive order from tick sequence; also drop sort entries whose column no longer exists.
- **Confidence:** high.

### P2-052 · Schema diff emits `DROP TABLE` for everything missing on the TO side, with no warning
- **Where:** `internal/ui/m5_tools.go:123-135`, `internal/tools/tools.go:55-71`
- **Why/trigger:** pick an empty target schema and run `schema_diff`: the new editor tab contains `DROP TABLE` for every source table, and the default safe mode is `silent`, so running the tab executes it with no confirmation. Nothing marks the drops or counts them.
- **Fix:** lead the tab with a count and a warning comment, and make drops opt-in (checkbox default off, or typed confirmation).
- **Confidence:** high.

### P2-053 · Generated migration DDL is in name order, not dependency order
- **Where:** `internal/ui/m5_tools.go:123`, `internal/tools/tools.go:37-71`
- **Why/trigger:** `child` sorts before `parent`, and `PlanTable` inlines `REFERENCES` into `CREATE TABLE`; the batch fails on the FK. Drops are emitted in the same order, so a parent can be dropped before its child.
- **Fix:** topologically sort creates (or emit FKs in a later ALTER pass) and reverse-sort drops.
- **Confidence:** high.

### P2-054 · Applying a data diff runs thousands of DELETEs from one keystroke
- **Where:** `internal/ui/m5_tools.go:268`
- **Why/trigger:** the only gate is the `a` key, and `w.gate` is a no-op at the default `silent` level. A diff of a 10 000-row table against an empty one previews 10 000 DELETEs; nothing distinguishes that from three inserts.
- **Fix:** require a typed confirmation when the batch contains DELETE/DROP/TRUNCATE or exceeds a threshold, showing the row count.
- **Confidence:** high.

### P2-055 · `schema_diff`/`data_diff` run a 2-minute uncancellable silent walk
- **Where:** `internal/ui/m5_tools.go:92,184`
- **Why/trigger:** both use `liveCmd`, not `w.job`, so there is no progress, no cancel and no "already running" guard. A 400-table schema does 1–3 round trips per table and then fails with a bare `context deadline exceeded`; pressing the action again starts a second walk.
- **Fix:** run through `w.job`, pass `ctx` into `PlanTable`, and show per-table progress.
- **Confidence:** high.

### P2-056 · Restored auto-saved tabs lose their saved-query binding, so later edits are never written
- **Where:** `internal/ui/m4.go:112,130`; `store/state.go:44-48`
- **Why/trigger:** `store.OpenTab` carries only Kind/Name/Text, so `restoreTabs` recreates the tab with `savedAs == ""` and `afterEdit()` stops calling `Queries.Update`. The tab looks like `report.sql` but edits are lost on the next restart.
- **Fix:** persist `Path`/`Shared` in `OpenTab` and restore `savedAs` (refuse shared ones), and stop swallowing `SetOpenTabs` errors.
- **Confidence:** high.

### P2-057 · History import appends one entry at a time on the UI loop, with partial imports
- **Where:** `internal/ui/m4.go:261-265`
- **Why/trigger:** each `Hist.Append` takes a cross-process lock, opens/appends/closes; a 20 000-entry import freezes the TUI, and a failure halfway leaves the earlier entries imported while the message gives no count — retrying duplicates them.
- **Fix:** add a bulk `AppendAll` (one lock, one handle) and run it off the loop, reporting `k of n`.
- **Confidence:** high.

### P2-058 · A persisted hidden column that no longer exists makes the grid read-only
- **Where:** `internal/ui/m4_grid.go:80`; `tab_table.go:101,483`
- **Why/trigger:** `t.hidden` is restored from state and never intersected with the loaded columns. After a rename, `len(t.cols)-len(t.hidden)` miscounts and `editablePK` (`hasPK || len(t.hidden)==0`) keeps a keyless table's grid read-only, with no explanation.
- **Fix:** prune `t.hidden` against `t.cols` when columns load, and count visible columns directly.
- **Confidence:** high.

### P2-059 · Pinning/unpinning a connection does blocking I/O inside `Update`
- **Where:** `internal/ui/m5_query.go:173,184`
- **Why/trigger:** `pinned.Close()` waits on the pool mutex a running Exec holds; `p.Pin(context.Background())` can block on the semaphore or dial an unreachable host with no deadline. The TUI freezes with no Esc.
- **Fix:** do both in a `tea.Cmd`, use the configured query timeout, and refuse to unpin while a query is running.
- **Confidence:** high.

### P2-060 · The model picker runs `ai.api_key_cmd` synchronously on the UI loop
- **Where:** `internal/ui/m5.go:367`
- **Why/trigger:** `config.ResolveAIKey` executes the command with a 30 s timeout from the key handler, so the whole TUI freezes; the Anthropic branch avoids it, which makes it easy to miss.
- **Fix:** resolve the key inside the existing `liveCmd` closure and report from the callback.
- **Confidence:** high.

### P2-061 · Schema diff offers every connection, including non-SQL engines
- **Where:** `internal/ui/m5_tools.go:74-79`
- **Why/trigger:** FROM = Postgres `public`, TO = Mongo. Mongo objects are `KindTable` with sampled columns, so the diff succeeds and plans `ALTER TABLE … DROP COLUMN` for every PG column Mongo did not sample.
- **Fix:** only offer connections with `Caps().Editor == "sql"` and the same dialect.
- **Confidence:** high.

### P2-062 · Cancelling a filter/search keeps the positional cursor, so Enter acts on a different row
- **Where:** `internal/ui/welcome.go:236-241`; `internal/ui/sidebar.go:502-506`
- **Why/trigger:** `sel` indexes the filtered list. Filter to `b`, move down to `b2`, press Esc: the full list returns and Enter connects to `a2` (the welcome screen) or acts on an unrelated table (sidebar). Reproduced for both.
- **Fix:** record the selected identity before clearing the filter and restore its index (or reset to 0 and re-run `step(0)`).
- **Confidence:** high (reproduced).

### P2-063 · Sidebar selection is a positional index, so a refresh silently moves the "current" table
- **Where:** `internal/ui/sidebar.go:156,453-459`
- **Why/trigger:** another session creates `aaa`; press `r`; `objs` is re-sorted and `sel` keeps its position. `current()` changes from `orders` to `customers`, and Enter/truncate/drop act on the wrong table (the confirm names it, but the user was looking at `orders`).
- **Fix:** keep the selected object key across reloads and re-select it after sorting.
- **Confidence:** high (reproduced).

### P2-064 · Expanded columns stay on "loading columns…" forever after meta is invalidated
- **Where:** `internal/ui/sidebar.go:400-407,434-441`
- **Why/trigger:** committing an edit or a global reload clears `w.meta` but leaves `s.cols[key] = true`; `columnRows` keeps appending the header and nothing re-requests the columns. A failed `fetchColumns` lands in the same dead state with no error.
- **Fix:** trigger a fetch when the flag is set but meta is missing (track in-flight refs), or clear `s.cols` in `forget`.
- **Confidence:** high (reproduced).

### P2-065 · A board refresh tick that overlaps a manual run stops auto-refresh for good
- **Where:** `internal/ui/m7_board.go:230-258,312-342`
- **Why/trigger:** the periodic chain is re-armed only from `run`'s completion callback. A tick that finds `v.running` returns nil and is never re-scheduled, and `runPanels` never arms a tick — so `refresh_secs` silently stops.
- **Fix:** keep the timer independent of runs (always reschedule, skip work while busy) or set a rerun flag.
- **Confidence:** high.

### P2-066 · A variables change or Enter during a board run is silently discarded
- **Where:** `internal/ui/m7_board.go:196-199,231-234,313-315`
- **Why/trigger:** the form callback sets `v.vals` then returns `v.run()`, which no-ops while `v.running`. With `refresh_secs = 0` the header shows the new values forever while the tiles keep the old run's results.
- **Fix:** queue a pending run with the newest values and start it from the completion callback.
- **Confidence:** high.

### P2-067 · A board's import panel ignores `Board.Connection` and imports into the workspace's connection
- **Where:** `internal/ui/m7_board.go:291-300`
- **Why/trigger:** queries use `v.session()` (which honours the pin) but `activate` builds the ref from `v.w.sess.CurrentSchema()` and runs `importWizard` on `w.sess`. A board pinned to `prod`, opened from a `staging` workspace, writes into `staging`.
- **Fix:** resolve the target session and refuse an import panel when the board's connection differs from the workspace's.
- **Confidence:** high.

### P2-068 · Connecting from the welcome screen has no in-flight guard and no cancel
- **Where:** `internal/ui/welcome.go:272-275`
- **Why/trigger:** with an unreachable server, each Enter starts another connect (`context.Background()`, no cancel). Several successes append several workspaces for the same connection (duplicate pools and keep-alive pings), and late failures pop modals over unrelated work.
- **Fix:** track a pending connection, ignore further Enters, and make it cancelable with a timeout.
- **Confidence:** high.

### P2-069 · Deleting a group closes its workspaces without warning about unsaved edits
- **Where:** `internal/ui/m6.go:99-123`
- **Why/trigger:** the typed confirmation says only "This removes N connection(s)". The callback calls `ws.close()` on every workspace of those connections, discarding uncommitted grid edits and unsaved tab text; `closeOthers` at least counts `w.dirty()`.
- **Fix:** include dirty/applying workspaces in the dialog, or refuse and ask the user to close them first.
- **Confidence:** high.

### P2-070 · "Close other connections" ignores a commit in flight
- **Where:** `internal/ui/m6.go:187-214`
- **Why/trigger:** the only guard is `dirty()`. If workspace A is applying a multi-statement batch (`w.applying`), closing it cancels the statements mid-batch with no warning, whereas `confirmQuit` explicitly refuses that case.
- **Fix:** include `w.applying` in the pre-check and in the confirmation text.
- **Confidence:** medium.

### P2-071 · Board pane panics on a very narrow terminal
- **Where:** `internal/ui/m7_board.go:444`
- **Why/trigger:** `tile` computes `iw = tw-2` and calls `strings.Repeat("─", iw)` unguarded; a pane content width ≤ 2 gives a negative count and a panic. Every other Repeat in the function is `max(0, …)`.
- **Fix:** `if iw < 1 { iw = 1 }`.
- **Confidence:** high (arithmetic), medium (reachability).

### P2-072 · The dev-origin escape hatch disables the only cross-port CSRF barrier
- **Where:** `internal/web/server.go:218-222`
- **Why/trigger:** with `WEBTUIDB_DEV_ORIGIN` set, the port check collapses to "hostname is loopback". A `SameSite=Strict` cookie is sent between ports on the same site, `Sec-Fetch-Site: same-site` passes, and `decode` ignores Content-Type — so a page on `localhost:3000` can POST `text/plain` to `/exec`. The variable is undocumented and no startup warning is printed.
- **Fix:** match an explicit origin allow-list, warn at startup, and require `application/json` for state-changing requests.
- **Confidence:** high.

### P2-073 · Tightening the origin check breaks every other loopback port, and the error is indistinguishable from a bad password
- **Where:** `internal/web/server.go:206-226`
- **Why/trigger:** previously any `localhost|127.0.0.1|::1:<port>` origin was accepted; now `POST/PUT/DELETE` from a non-Vite local front end returns `401 {"error":"unauthorized"}`, which the UI reports as an auth failure. `loopback(u.Host)` is also called with `host:port` (so `Origin: http://localhost` fails) while the neighbouring line uses `Hostname()`.
- **Fix:** return a distinct 403 "origin not allowed", document the env var, and compare on `Hostname()` plus the effective port.
- **Confidence:** high.

### P2-074 · The reaper holds the global session mutex across database connection closes
- **Where:** `internal/web/sessions.go:179-190`
- **Why/trigger:** `sweepPins` calls `p.Close()` (network I/O) inside `s.mu`, and it also takes `pinMu`, which a concurrent `Pin` holds for up to 30 s while dialling. Every session-scoped request blocks behind it.
- **Fix:** collect expired pins under the lock, then close outside it.
- **Confidence:** high.

### P2-075 · The reaper ignores in-flight handlers, so a long job has its pool pulled
- **Where:** `internal/web/sessions.go:181-192`
- **Why/trigger:** `lastUsed` is set at handler entry only. A backup streaming for over `idleSessionTTL` is deleted mid-flight, its pool closed, and every later request on that session 404s.
- **Fix:** refresh `lastUsed` while a session handler is alive, and skip sessions with running jobs.
- **Confidence:** high.

### P2-076 · Safe-mode level is read without the lock it is written under
- **Where:** `internal/web/sessions.go:325-338`
- **Why/trigger:** `setSafeMode` writes `ss.level` under `ss.mu`, but `gate`/`check`/`info`/`storePlan` read it unlocked. Two concurrent requests (one lowering the level) can compute the gate from the old value, and `-race` reports a data race.
- **Fix:** snapshot the level once under the lock at the top of `gate`, and pass it down.
- **Confidence:** high.

### P2-077 · `PUT /api/state/table` silently wipes a table's saved state on an empty or partial body
- **Where:** `internal/web/handlers_store.go:116`
- **Why/trigger:** `State.SetTable` replaces the whole record; an empty body (or a body with only `hidden`) erases `show_filters`, `sort`, `filters` and `widths` that the TUI saved. `putConns` guards exactly this case.
- **Fix:** reject zero-length bodies and merge fields that were actually present.
- **Confidence:** high.

### P2-078 · ETag and If-Match are computed from different values, so an unparseable `connections.toml` blocks saving forever
- **Where:** `internal/web/handlers.go:61,79`; `m4.go:295-308`
- **Why/trigger:** GET publishes `sha256(s.stamp)`, PUT validates `sha256(connStamp())` (live stat). When the file does not parse, `reloadConns` returns early and `s.stamp` is stale, so every PUT answers 409 and "reload and retry" can never succeed.
- **Fix:** publish and compare the same value, and give the parse failure its own error message.
- **Confidence:** high.

### P2-079 · Sending `"vars": {}` skips the Variables prompt and sends raw placeholders to the server
- **Where:** `internal/web/handlers.go:487`
- **Why/trigger:** the 422 prompt is guarded by `req.Vars == nil`; the shipped client sends `{}` when it has no values. `db.Bind` leaves `:id` in place, so the user sees a driver syntax error instead of the dialog.
- **Fix:** 422 whenever a detected parameter is missing from `Vars`.
- **Confidence:** high.

### P2-080 · `limit: -1` lets an API client turn the row limit off, materialising whole tables in memory
- **Where:** `internal/web/handlers.go:506`; `internal/web/m2.go:118`
- **Why/trigger:** `limit < 0` means "no limit" for `/exec` and export `query` mode, but "use the configured limit" for `/rows` and export `page` mode. One request can exhaust server memory and return a multi-GB response despite `row_limit`.
- **Fix:** clamp to `rowLimit()` everywhere, with an explicit opt-in if unlimited is ever wanted.
- **Confidence:** high.

### P2-081 · Clearing a connection's whole history is indistinguishable from deleting one entry
- **Where:** `internal/web/handlers_store.go:30`
- **Why/trigger:** `DELETE /api/history?conn=c1` with `id == ""` escalates to `Clear`, destroying up to 5000 entries. A client that sends an undefined id wipes the history with no confirmation.
- **Fix:** require `&all=1` for the bulk operation and 400 when `id` is missing.
- **Confidence:** high.

### P2-082 · `DELETE /api/queries/{path}` with a trailing slash deletes a whole folder
- **Where:** `internal/web/handlers_store.go:83`
- **Why/trigger:** `queryPath` only rejects the store root, and `Queries.Delete` uses `os.RemoveAll`; `reports/` cleans to `reports` and removes every saved query inside it, answering 200.
- **Fix:** require a `.sql` suffix and a regular file in the web path.
- **Confidence:** high.

### P2-083 · `$expr` inlines an arbitrary SQL expression into cell edits
- **Where:** `internal/web/changes.go:32`
- **Why/trigger:** the old allow-list (`now()`, `gen_random_uuid()`) was replaced by `db.CheckExpr`, which only rejects separators/comments. `(SELECT pg_sleep(300))`, `nextval`, `set_config` and any dialect function are accepted and inlined into `UPDATE … SET col = <expr>`, so one cell edit can read other tables or hold the session (and `ss.applying`) for 5 minutes.
- **Fix:** keep a macro allow-list for the dialog and treat anything else as an explicit advanced mode with its own confirmation; shorten the per-statement timeout.
- **Confidence:** high.

### P2-084 · Background jobs have no concurrency or disk cap, and export files are never pruned
- **Where:** `internal/web/m2.go:153,223`
- **Why/trigger:** each call starts a goroutine and (for table exports) a temp file that grows to the full dump; imports spool up to 512 MB each. Nothing limits jobs per session, so one authenticated tab can fill the temp disk or run many concurrent scans on the same pool.
- **Fix:** per-session job/byte caps with 409/429, and delete the oldest un-downloaded export before creating a new one.
- **Confidence:** high.

### P2-085 · Schema diff with a mistyped/unknown TO schema plans a DROP for every table
- **Where:** `internal/web/m4_tools.go:114-149`
- **Why/trigger:** `Objects(ctx,"publik")` returns an empty list with no error, so every FROM table is "only in FROM" and the stored plan is a list of `DROP TABLE`. A stale schema list or a typo is enough.
- **Fix:** resolve `To` against `sess.Schemas()` and 400 when missing; require opt-in for a drops-only plan.
- **Confidence:** high.

### P2-086 · Schema diff rewrites the table's schema but not its foreign keys'
- **Where:** `internal/web/m4_tools.go:137-144`
- **Why/trigger:** `to[i].Schema = req.From` is applied, but `ForeignKeys[].RefSchema` still points at `req.To`; the generated CREATE references the comparison schema, and for tables present on both sides every FK looks changed and gets `ADD CONSTRAINT` without `DROP`, so the apply fails.
- **Fix:** map `RefSchema` from `To` to `From` as well, and add an FK-in-diff test.
- **Confidence:** high.

### P2-087 · Safe mode can be lowered with no password or confirmation when the connection stores no password
- **Where:** `internal/web/m5web.go:27-31`
- **Why/trigger:** `ss.o.Password == ""` (password_cmd, `ask`, keyring) makes the handler skip the gate entirely, so one click drops Safe1 → Silent. `ss.gate`/`ss.check` and the TUI both fall back to a confirmation in this case.
- **Fix:** return 409 `gate.confirm` unless `confirmed`, and add `Confirmed` to the request plus the dialog.
- **Confidence:** high.

### P2-088 · Changing safe mode or colour overwrites concurrent edits to `connections.toml`
- **Where:** `internal/web/m5web.go:35-48`; `internal/web/m7web.go:275-288`
- **Why/trigger:** both handlers write `s.o.Conns` without `reloadConns()`, so a connection added by the terminal since the last GET is dropped by the full-file replace.
- **Fix:** call `s.reloadConns()` under `connMu` before mutating.
- **Confidence:** medium-high.

### P2-089 · `/exec/page` ignores the connection's row limit
- **Where:** `internal/web/m6web.go:184-210`
- **Why/trigger:** `/exec` and `/export` clamp `limit` to `rowLimit()`; `execPage` passes the client's `1e9` straight into `LIMIT`, and the driver materialises every row before `toJSON` copies them again.
- **Fix:** apply the same clamp and add a test for a huge limit.
- **Confidence:** high.

### P2-090 · Saving a board from the web erases its `connection` pin
- **Where:** `internal/web/m6web.go:392-449`; `Views.tsx:128`
- **Why/trigger:** `boardJSON` never exposes `Connection` and `boardPut` rebuilds the board from the request, so a TUI-created board pinned to `prod` loses the pin on any web save and then runs on whatever session the client names.
- **Fix:** echo `connection` in the JSON and keep the old value when the request omits it.
- **Confidence:** high.

### P2-091 · Restore's "create the new database" step runs on the request context
- **Where:** `internal/web/m3.go:181`
- **Why/trigger:** closing the tab cancels `Apply`, so a `CREATE DATABASE` can report "context canceled" while it actually committed; the "the database still exists" note only exists on the job path. `/apply` already detaches with `context.WithoutCancel`.
- **Fix:** use the same detached, bounded context for the create step.
- **Confidence:** medium.

### P2-092 · ClickHouse backups cannot be restored through the web handler
- **Where:** `internal/web/m3.go:129`
- **Why/trigger:** for ClickHouse, `File` is a name on the server's `backups` disk, not a local path. `backupFile` rejects `/` and rewrites the name under `DataDir/backups`, and restore then demands a local file — so the round trip the feature promises always fails, and a legitimate `2026/10/dump` is rejected.
- **Fix:** skip path rewriting and the local-existence check for ClickHouse (still rejecting a leading `-`).
- **Confidence:** medium-high.

### P2-093 · Read-write MCP tokens can run DDL: the gate only looks at the first keyword
- **Where:** `internal/mcp/mcp.go:285,487-503`
- **Why/trigger:** `ddl[firstWord(st.Text)]` is the only barrier. `DO $$ BEGIN EXECUTE 'DROP TABLE orders'; END $$`, `CALL …`, or (on MySQL/ClickHouse) a leading `# x\nDROP TABLE orders` all return `DO`/`CALL`/`""`, so the statement is executed; `skipLead` also knows only `--`/`/* */`, not `#`.
- **Fix:** decide on the token stream (a `ContainsDDL` helper reusing the dialect lexer), and reject `DO`/`CALL`/`EXECUTE`/anonymous blocks for read-write tokens.
- **Confidence:** high.

### P2-094 · Read-only MCP tokens can run data-changing "read-only" statements
- **Where:** `internal/mcp/mcp.go:282-290`
- **Why/trigger:** `db.IsReadOnly` is a keyword pre-filter, so `SELECT setval('s',0)`, `SELECT pg_read_file(...)`, `SELECT pg_terminate_backend(pid)` and `SELECT … FOR UPDATE` all pass as read-only.
- **Fix:** run `execute_read_query` inside a real read-only transaction (`BEGIN READ ONLY`, MySQL `START TRANSACTION READ ONLY`) and roll it back; refuse where the driver cannot enforce it.
- **Confidence:** high.

### P2-095 · HTTP MCP sessions are never expired and never capped
- **Where:** `internal/mcp/mcp.go:124-136`
- **Why/trigger:** `StreamableHTTPOptions` is nil, so `SessionTimeout` is zero ("idle sessions are never closed"). A token holder can create unlimited sessions, each holding a full tool set and transports, and keep them forever.
- **Fix:** set a session timeout (e.g. 30 min) and cap live sessions per token.
- **Confidence:** high.

### P2-096 · An MCP session object is used after unlock, and `disconnect` can close it mid-query
- **Where:** `internal/mcp/mcp.go:252-278`
- **Why/trigger:** the session is copied out of the map, then `Exec` runs unlocked; a concurrent `disconnect` deletes the entry and calls `Close()`, producing use-after-close (panic or driver corruption) and failing pending statements.
- **Fix:** refcount or RWMutex per connection; make `disconnect` wait for in-flight users.
- **Confidence:** high.

### P2-097 · Import button can start two jobs before `running` becomes true
- **Where:** `web/src/components/Tools.tsx:123-131,177`
- **Why/trigger:** `disabled={!!job && !job.done}` and `job` is set only after `/import` responds; a double click starts two imports, inserting the same rows twice, and the first job cannot be cancelled or watched.
- **Fix:** set a `starting` flag before the POST and include it in `disabled`.
- **Confidence:** high.

### P2-098 · "Load more" has no in-flight guard and uses a stale `results` snapshot
- **Where:** `web/src/components/QueryTab.tsx:291-304`
- **Why/trigger:** pressing `alt+m` twice sends the same `offset` twice and both callbacks close over the same `results`; the same block is appended twice or one append is dropped.
- **Fix:** a `loadingMore` ref plus a functional `setResults` update.
- **Confidence:** high.

### P2-099 · Enter on the grid after hiding the cursor column crashes the tab
- **Where:** `web/src/components/Grid.tsx:66`; state at `:55-56`; action at `DataTab.tsx:269`
- **Why/trigger:** `hide_column` hides the cursor column, leaving `col === columns.length`; `open()` does `columns[c].name`. Any action that does not first move the cursor (Enter, `copy_cells`, quick filter) throws and blanks the workspace.
- **Fix:** clamp `col` in an effect on `columns.length`, and guard the reads.
- **Confidence:** high.

### P2-100 · The filter bar answers key actions for every open table tab
- **Where:** `web/src/components/FilterBar.tsx:18-25`
- **Why/trigger:** hidden tabs stay mounted and their listeners do not check `active` or `e.defaultPrevented`, so `s`/`enter` POST `/filters/sql` in all of them, and `apply_filters` can raise "Discard pending changes?" from a hidden tab and discard its edits.
- **Fix:** pass `active` from `DataTab`, return early when inactive, and honor `e.defaultPrevented`.

---

## Appendix A — further P2 findings that did not fit the top 100

These are the same class as the P2 list (real bugs, normal-user reachable) but lower blast radius. Full details are in the per-area notes; the fix is stated inline.

- **`internal/tools/tools.go:93`** — a key column missing on one side falls back to *all* `to` columns, so `DataDiff` plans a wipe-and-reload (every source row DELETE, every target row INSERT) instead of erroring. *Fix:* error when a requested key column is absent on either side.
- **`internal/tools/tools.go:108`** — row keys are `strings.Join(parts, "\x01")` of `fmt.Sprint` values, so distinct rows collide (`("x\u0001y","z")` vs `("x","y\u0001z")`, `int64(1)` vs `"1"`). A collision updates the wrong row. *Fix:* length-prefix and type-tag each part.
- **`internal/tools/tools.go:243`** — `FakeRows` emits a full timestamp for `time` columns, which Postgres/MySQL reject. *Fix:* emit a clock string for `time*`.
- **`internal/tools/tools.go:236`** — `FakeRows` has no uniqueness tracking for text/numeric PKs; 5000 rows over 20 000 possible slugs produce duplicate keys. *Fix:* derive PKs from the row index / honour length.
- **`internal/mcp/mcp.go:291`** — the approval prompt is skipped for statements classified read-only, so `SELECT setval(…)`/`pg_terminate_backend` bypass `approval_for_unsafe`. *Fix:* make the approver the default for anything not provably side-effect-free.
- **`internal/mcp/mcp.go:309`** — a multi-statement `execute_query` batch is autocommitted statement by statement; a failure halfway leaves partial writes after one approval. *Fix:* wrap in a transaction or refuse multi-statement writes.
- **`internal/mcp/mcp.go:505`** — `redact` only masks single-quoted literals, so `IDENTIFIED BY "…"` and `$q$…$q$` passwords land in the activity log and `mcp-log.csv`. *Fix:* mask every literal token with the dialect lexer.
- **`internal/mcp/mcp.go:180`** — `[]byte` cells are base64-encoded by `json.Marshal` with no marker, so binary columns reach the model corrupted. *Fix:* format via `db.FormatValue` or wrap as `{"$hex":…}`.
- **`internal/plugin/plugin.go:71`** — a relative `command` resolves against the process CWD, so starting webtuidb inside a cloned repo can execute that repo's script from a config plugin. *Fix:* require an absolute path or resolve against the plugin dir; mark it broken in `List`.
- **`internal/plugin/plugin.go:65`** — no internal timeout; with `query_timeout_secs = 0` a hung plugin pins the request/`liveCmd` forever. *Fix:* hard cap (30–60 s) inside `Run`.
- **`internal/ai/ai.go:35`** — `base_url` is trusted verbatim: the API key can be sent over plain HTTP or to an arbitrary host (SSRF/metadata read). *Fix:* require https (or loopback http), reject credentials in the URL and link-local ranges.
- **`internal/export/export.go:147`** — duplicate result column names keep only the last index, so `select a.id, b.id` exports `2,2`. *Fix:* disambiguate names and keep a source-index slice.
- **`internal/export/export.go:241`** — NULL and `''` both export as an empty cell, and `csv.null_as` is never read (dead setting). *Fix:* honour `NullAs` on write and read.
- **`internal/export/export.go:389`** — SQL export formats times as RFC3339Nano; MySQL/ClickHouse reject the offset. *Fix:* use `Dialect.Literal` for those engines.
- **`internal/export/export.go:244`** — CSV/JSON/XLSX drop the timezone of `timestamptz`, so round trips shift the instant. *Fix:* format with offset.
- **`internal/export/export.go:416`** — a table name with a leading/trailing `'` (or `History`) makes `SetSheetName` fail, aborting the whole XLSX export after all rows are fetched. *Fix:* sanitize those cases and fall back to the default sheet name.
- **`internal/importer/importer.go:300`** — duplicate or blank CSV headers produce duplicate INSERT columns and silently dropped values. *Fix:* validate the header before planning.
- **`internal/importer/importer.go:338`** — imports are not transactional; a parse error leaves earlier batches committed and `CreateTable` already run, so a retry fails. *Fix:* wrap in a transaction where supported, or report rows committed.
- **`internal/importer/importer.go:53`** — `encoding/csv` normalises CRLF inside quoted fields, so `"a\r\nb"` round-trips as `"a\nb"`. *Fix:* pin the behaviour in a test and document it.
- **`internal/importer/importer.go:54`** — `csv.quote` is honoured on export but not import; a custom quote makes the file unreadable. *Fix:* reject non-`"` quotes or implement the parser.
- **`internal/backup/backup.go:181`** — `append(os.Environ(), env...)` puts the shell's `PGPASSWORD`/`MYSQL_PWD` first, and libpq uses the first — backups authenticate with a stale credential. *Fix:* filter the keys being overridden.
- **`internal/backup/backup.go:228`** — pg_dump creates the file, so the dump is `0644` (other drivers use `0600`). *Fix:* chmod the `.part` file to 0600.
- **`internal/backup/backup.go:413`** — the display command appends the output path unquoted, so a crafted filename renders as extra shell commands in the confirm dialog. *Fix:* quote it with the same helper.
- **`internal/backup/backup.go:384`** — a single >1 MB output line stops the scanner; `sc.Err()` is ignored and the remainder is drained, so a failed restore reports success. *Fix:* check `sc.Err()` and surface truncation.
- **`internal/backup/backup.go:271`** — Mongo restore ignores `NewDatabase` and restores into the source database (the exact scenario the option exists to prevent). *Fix:* pass `--nsFrom/--nsTo` or refuse.
- **`internal/sqlfmt/tabular.go:44`** — a trailing `;` produces a phantom whitespace statement and CTE layout collapses. *Fix:* return `""` for empty input; treat `WITH` as a clause.
- **`internal/web/m4.go:144`** — `GET /api/connections/export?passphrase=…` puts the export passphrase (which protects plaintext DB passwords) in the URL, so it lands in logs and history; the shipped client already POSTs. *Fix:* delete the GET route.
- **`internal/web/handlers.go:229`** — `DELETE /api/sessions/{id}` has no ownership check; any client with the token can close another browser's session and kill its transaction. *Fix:* bind sessions to the issuing token.
- **`internal/ui/m2.go:48`** — `liveMsg`/job state aside, a backup dialog is pushed before `w.job` accepts it (`m3.go:515`), so an unrelated job can be cancelled from the dialog's `x`. *Fix:* push only after acceptance and scope the cancel.
- **`web/src/components/Dialogs.tsx:…` / `DataTab.tsx:402`** — the Set Value dialog dereferences `res.columns[colRef.current]` after the cursor column was hidden; `colRef` is never clamped. *Fix:* clamp once per result and reuse.
- **`web/src/components/QueryTab.tsx:264`** — Beautify/Uglify dispatch a change with offsets captured before the `/format` await, so keystrokes typed meanwhile are replaced. *Fix:* re-validate `sliceDoc(from,to)` before applying.
- **`web/src/components/Tools.tsx:106`** — `/import/preview` responses are not sequenced; a late response resets the user's column mapping. *Fix:* drop stale responses.
- **`web/src/components/Tools.tsx:200`** — Search can start twice, and Stop cancels only the newest job while the first keeps scanning. *Fix:* in-flight guard.
- **`web/src/routes/Workspace.tsx:97`** — tab auto-restore runs twice (StrictMode and session invalidation), duplicating tabs and persisting the duplicates. *Fix:* synchronous started-flag plus abort.
- **`web/src/routes/Workspace.tsx:257`** — `disconnect`/`quit`/`reconnect` ignore `dirty` and throw away uncommitted edits, unlike the TUI. *Fix:* reuse `okToDrop`.
- **`web/src/routes/Workspace.tsx:135`** — the keymap listener intercepts keys inside text inputs, so `ctrl+d` never deletes a character and the next key runs a command. *Fix:* skip dispatch in input/textarea/CodeMirror unless a prefix is pending.
- **`web/src/routes/Workspace.tsx:132`** — the prefix survives clicks and open dialogs, so the next keystroke is consumed as a command in a different context. *Fix:* reset on `pointerdown` and in `push()`-equivalent.
- **`web/src/utils/keys.ts:67`** — pressing the prefix twice reaches the browser (Ctrl+D opens "Add bookmark") instead of delivering the literal key. *Fix:* `preventDefault` on the passthrough.
- **`web/src/utils/copy.ts:38`** — `navigator.clipboard` throws synchronously outside a secure context, so copy silently fails when the server is reached over plain HTTP on a LAN address (a supported mode). *Fix:* textarea + `execCommand` fallback.
- **`web/src/utils/api.ts:51`** — a 401 after cookie expiry leaves the UI stuck on "unauthorized" because the `['meta']` query never refetches. *Fix:* invalidate meta on 401.
- **`web/src/routes/Connections.tsx:206`** — Test runs `clean()` before the async `parseUrl()` resolves, so it tests the previous form; two clicks race. *Fix:* await the parse, snapshot the values tested, disable while in flight.
- **`Dockerfile:26`** — the image's default entrypoint is `--insecure-listen` on `0.0.0.0`; see Appendix B (B-1) for the full finding.

---

## P3 — 100 highest-value hardening, clarity and test items

Ordered by area; numbering is continuous. Each line names the file and line, the problem, and the fix.

### db core, params and search

1. **`internal/db/expr.go:40`** — the scanner rejects *any* backslash, so valid MySQL/ClickHouse `E'…'`-style literals cannot be entered at all. *Fix:* make the check dialect-aware (or document the restriction in the dialog hint).
2. **`internal/db/params.go:30`** — one generic comment model (`--`, `/* */`) while `scan` in `classify.go` is per-dialect: MySQL `#` comments and `5--3` produce wrong parameter lists (both directions verified). *Fix:* drive both from the same lexer.
3. **`internal/db/params.go:40`** — backslash escaping is disabled inside ClickHouse backticks, so a `\`` in an identifier can invent a parameter. *Fix:* disable it for backticks only on mysql/mariadb, mirroring `split.go:74`.
4. **`internal/db/params.go:109`** — `Bind` leaves an unbound `:b` in the SQL instead of reporting it, so the user gets a server syntax error. *Fix:* collect missing names and return an error.
5. **`internal/db/params.go:113`** — any casing of `null` becomes SQL NULL, so the string "Null" can never be searched for. *Fix:* make NULL an explicit opt-in or provide an escape.
6. **`internal/db/params.go:62`** — in `question` format every `?` is a placeholder, including Postgres jsonb `?`/`?|`/`?&`, silently rewriting valid queries. *Fix:* skip operator positions for dialects that have them.
7. **`internal/db/search.go:31`** — the `=` operator is implemented as LIKE, so exact matching on numeric/date columns misses values and PartiQL connections report "LIKE is not supported" for every table. *Fix:* `CAST(col AS TEXT) = ?`, and validate the operator against `Operators(dialect)` once.
8. **`internal/db/search.go:30`** — an invalid `Op` (wrong case, future op) is not rejected up front, so the walk fails with one identical error per table. *Fix:* validate at the top of `Search`.
9. **`internal/db/search.go:34`** — a mistyped table name in `Tables` is silently skipped; the progress counter still reaches N/N, so "no matches" and "no such table" are indistinguishable. *Fix:* emit a hit with an error for unseen names.
10. **`internal/db/preimage.go:24`** — the audit CSV omits schema (and database), so `public.orders` and `archive.orders` produce identical rows. *Fix:* qualify the name or add columns.
11. **`internal/db/preimage.go:22`** — a real NULL and the four-character string `NULL` render identically in the destructive-change audit trail. *Fix:* use a distinct marker or a type column.
12. **`internal/db/filter.go:290`** — `BulkUpdate`'s "never update every row" guard tests `len(fs.Active())`, but a blank enabled `RawColumn` produces an empty WHERE body, so the statement is emitted malformed rather than refused. *Fix:* also require `where != ""`.
13. **`internal/db/filter.go:299`** — duplicate SET columns are emitted twice (`UPDATE t SET a=?, a=?`): Postgres rejects, MySQL applies only the last. *Fix:* reject duplicates case-insensitively.
14. **`internal/db/filter.go:313`** — the display pass discards its error, so Preview/history can show `" WHERE "` with nothing after it while the executed text is complete. *Fix:* propagate the error as `WhereText` does.
15. **`internal/db/db_test.go:479`** — no test covers `BulkUpdate` at all (empty set, empty filter, placeholder numbering after SET args, ClickHouse `ALTER … UPDATE`, display inlining). *Fix:* table test over PG/MySQL/ClickHouse asserting Text, Args and Display plus both refusal paths.
16. **`internal/db/ssh.go:102`** — the ssh_config parser ignores `Match` blocks, tabs, inline comments, `ssh://` URIs, `Include`, and a global `ProxyJump` before the first `Host`, so a tunnel can go to the wrong place silently. *Fix:* split on `strings.Fields`, strip comments, skip `Match`/`Include` explicitly.
17. **`internal/db/ssh.go:55`** — `Tunnel.Close` drops the parent's error, hiding a failed bastion teardown (and which hop failed in a chain). *Fix:* `errors.Join`.
18. **`internal/db/ssh.go:258`** — a cancelled context surfaces as "use of closed network connection" instead of a timeout, because `AfterFunc` already closed the socket. *Fix:* check `ctx.Err()` before returning the handshake error.
19. **`internal/db/ssh_test.go:16`** — no test builds a ProxyJump chain (hop order, empty password, parent close on each failure) or exercises `DialFor`/SOCKS5. *Fix:* extract hop construction into a pure function and table-test it.
20. **`internal/db/fuzz_test.go:27`** — the `custom` waiver is computed from the whole input, so any corpus entry containing "delimiter" disables the "no lost code" property for that dialect entirely. *Fix:* strip DELIMITER lines instead of waiving the invariant.
21. **`internal/db/fuzz_test.go:46`** — `FuzzIsReadOnly` calls the function and asserts nothing; it only catches panics. *Fix:* assert the documented invariant (read-only ⇒ no write keyword, no interior `;`).
22. **`internal/db/types.go:291`** — `UserChange.Kind` is a bare string whose valid set lives in a comment, so `"Create"` compiles and fails at plan time. *Fix:* a typed `UserChangeKind` with constants.
23. **`internal/db/db_test.go:479`** — `WhereText` covers one Postgres shape; MySQL `?` numbering, `AnyColumn` expansion and `fs.Or` are untested, which is where an off-by-one hides. *Fix:* add a MySQL case and an `AnyColumn`+`Contains` case.

### drivers

24. **`internal/db/postgres/pin.go:15`** — `pinned` only overrides `Exec`/`Close`; `Apply`, `Rows` and `Count` go through the parent pool, so a pinned transaction's writes land elsewhere (MySQL overrides `Apply`, so the two drivers disagree). *Fix:* override `Apply` to loop over the pinned `Exec` and document which methods are pinned.
25. **`internal/db/postgres/pin.go:33`** — DDL on a pin does not clear the Any-column cache that unpinned `Exec` clears, so filters keep searching old columns. *Fix:* mirror the cache reset.
26. **`internal/db/postgres/pin.go:22`** — switching database closes the pool the pin captured, so the pinned tab dies with a generic "conn closed". *Fix:* override `UseDatabase` to return "release the pin first".
27. **`internal/db/postgres/postgres.go:118`** — the new Redshift/Vertica/Greenplum profiles inherit `Users`, `Processes` and `Transactions` from postgres, and `Users()` queries `rolreplication`/`rolbypassrls`, which those engines lack. *Fix:* add capability flags and clear them per profile.
28. **`internal/db/postgres/users.go:141`** — `GRANT ALL ("a") ON TABLE …` is generated for `ALL`/`ALL PRIVILEGES` with a column list (SQLSTATE 42601). *Fix:* reject `ALL` when columns are given.
29. **`internal/db/postgres/users.go:14`** — one allowlist serves role attributes, table privileges and schema/sequence privileges, so `ALTER ROLE … WITH SELECT` type errors reach Apply. *Fix:* split into role-attr, column-priv and object-priv sets, validated in `PlanUser`.
30. **`internal/db/postgres/users.go:141`** — an empty grant object produces `"public".""` and only fails at Apply. *Fix:* require an object.
31. **`internal/db/postgres/exec.go:21`** — read-only connections refuse `BEGIN`/`COMMIT` and `Kill`, so the transaction UI cannot work and a runaway `SELECT` cannot be stopped from the process list. *Fix:* allow transaction control and cancels through the gate.
32. **`internal/db/postgres/postgres.go:411`** — `dropped()`'s EOF/EPIPE/ECONNRESET branches are unreachable because `retryable` rejects them, so the classification reads as a safety net that does nothing. *Fix:* fold into one helper or use it in `put`.
33. **`internal/db/mysql/mysql.go:495`** — a truncated result on a pinned connection drains every remaining row (10 M rows) instead of stopping, and the early return skips `rows.Err()`. *Fix:* wrap with `db.PageStatement` and check the drain error.
34. **`internal/db/mysql/users.go:86`** — `rows.Err()` is ignored for the grant queries and every `SHOW GRANTS` failure is swallowed, so the panel shows empty privileges with no warning. *Fix:* collect and surface per-account errors.
35. **`internal/db/mysql/users.go:115`** — backticks are removed, not unescaped, when parsing `SHOW GRANTS`, so a name containing `` ` `` round-trips to the wrong object. *Fix:* split on unquoted `.` and replace doubled backticks.
36. **`internal/db/mysql/users.go:15`** — `GRANT OPTION` is accepted as a grantable privilege and produces invalid SQL; it belongs in `WITH GRANT OPTION`. *Fix:* special-case or reject it.
37. **`internal/db/mysql/users.go:166`** — `create` silently ignores `User.Attrs`/`Grant`, and there is no way to revoke global privileges. *Fix:* append the grants or reject unconsumed fields.
38. **`internal/db/mysql/users.go:45`** — `acct()` doubles backslashes, which is wrong under `NO_BACKSLASH_ESCAPES`, so grants for such accounts are silently missing. *Fix:* quote per the session's `sql_mode`.
39. **`internal/db/postgres/users_test.go:55`** — `TestSchemaAdmin` indexes `st[0]` before checking `err`, so a regression panics instead of naming the operation. *Fix:* check the error/length first.
40. **`internal/db/mysql/pin_test.go:10`** — no test proves the pin uses one physical connection (the `onceConnector` wiring bug is invisible) and none kills the pinned connection. *Fix:* add a TEMP-table assertion and a kill test.
41. **`internal/db/postgres/pin_test.go:10`** — the only test uses a plain SELECT (the path that works); `… LIMIT n` and `FOR UPDATE` (the paths that abort) are uncovered. *Fix:* add both and assert a following INSERT succeeds.
42. **`internal/db/redshift/profile.go:13`** — `MatViews` is the one flag the profile clears, and nothing in either UI reads it; the flags that are read (`Indexes`, `ProcessList`, `Sequences`) stay on. *Fix:* gate matview rows where objects are filtered, then spend the adjust on real flags.
43. **`internal/db/greenplum/profile.go:11`** — registered with no adjust, so `Triggers` stays true although Greenplum does not support user triggers. *Fix:* clear it.
44. **`internal/db/redshift/profile_test.go:9`** — the test builds its own `Capabilities` and calls `adjust`, so it cannot see what the driver actually reports. *Fix:* expose the caps without a server and assert the full struct per profile.
45. **`internal/db/iam/iam.go:19`** — `awsconfig.LoadDefaultConfig` runs per physical connection (per statement with the DISCARD problem), including IMDS probes and SSO prompts. *Fix:* load once per pool and only rebuild the token.
46. **`internal/db/iam/iam.go:30`** — the package has no tests and no input validation; empty host/user produce `":5432"` and surface only as an auth failure. *Fix:* validate and add region/endpoint tests.
47. **`internal/db/chart.go:103`** — a column whose values are all NULL is classified "not numeric", so a left-join aggregate chart fails with a misleading message. *Fix:* fall back to the column type.
48. **`internal/db/chart.go:164`** — an explicitly named Y column with no numbers returns an empty series and no error, unlike the inferred case. *Fix:* return one consistent outcome.
49. **`internal/db/chart.go:106`** — rows are indexed without a length guard; `Chart(nil, …)` and any unpadded `Result` panic the TUI/web server. *Fix:* validate `res` and each row length.
50. **`internal/db/registry.go:15`** — `Register` is a bare map write, so a copy-pasted profile with the wrong constant silently replaces the real postgres driver. *Fix:* panic/log on duplicate names and add a test that every `config.Drivers` entry resolves.

### store, config and state

51. **`internal/db/all/all.go:1`** — the package is described as registering "the drivers" but only pulls in postgres and mysql (via the profiles); mariadb, clickhouse, redis, mongo, cassandra and dynamodb depend on unrelated blank imports in `main.go`. *Fix:* import them here and delete the duplicates in `main.go`, or rename the package to `profiles`.
52. **`internal/db/explain_test.go:40`** — the ClickHouse depth test is hand-written to match the `/2` divisor, and the `[]byte` JSON path (what the driver actually returns) is never exercised. *Fix:* use a captured real plan and a `[]byte` cell.
53. **`internal/store/history.go:82`** — appends are never `Sync()`ed, while the trim/rewrite path uses a fsyncing atomic write, so the newest entries vanish on power loss. *Fix:* sync before close.
54. **`internal/store/history.go:119`** — one >64 MB line makes `bufio.Scanner` fail, and the error is returned from `readAll`, so `List`/`Delete`/`Clear` all stop working for that connection with no recovery path. *Fix:* cap stored SQL and skip-and-count oversize lines.
55. **`internal/store/history.go:77`** — a `.lock` file is created per connection and never removed, and the legacy rename also orphans the old lock name. *Fix:* use one lock per directory or document/clean them.
56. **`internal/store/history.go:69`** — entry IDs are `Time.UnixNano()` and import preserves `Time`, so importing the same file twice creates duplicate IDs and deleting one entry removes both. *Fix:* transport/de-dupe IDs on import.
57. **`internal/store/history_io.go:35`** — JSON vs CSV is sniffed from the first byte, so a BOM or leading newline (Excel/Windows editors) makes a valid export unreadable. *Fix:* trim BOM/whitespace before deciding, and trim the first CSV header cell.
58. **`internal/store/console.go:36`** — the ring buffer reallocates the whole 1000-entry slice on every statement past the cap, in the query hot path. *Fix:* keep a head index and wrap.
59. **`internal/store/queries.go:196`** — saving over a symlinked `.sql` replaces the link with a regular file, so a queries-in-git setup silently stops updating the repo. *Fix:* `Lstat` + `EvalSymlinks` and write through.
60. **`internal/store/queries.go:252`** — `strings.Trim(p, "./")` strips any leading dot, so `~/.config/queries` is grouped under folder `config`, and two shared dirs with the same basename merge. *Fix:* `TrimPrefix(filepath.ToSlash(p), "./")` and disambiguate folders.
61. **`internal/store/queries.go:244`** — one unreadable file (root-owned or dangling symlink) aborts the whole walk, so the sidebar shows no saved queries at all. *Fix:* skip and collect per-file errors.
62. **`internal/config/sources.go:36`** — source files are read with no permission check, so plaintext DB passwords in a 0644 `team.toml` are used silently. *Fix:* return the offending paths for a warning.
63. **`internal/config/sources.go:33`** — a missing or malformed `[[source]]` glob is indistinguishable from "no connections here"; `Glob`'s error is discarded. *Fix:* `Stat` each expanded dir and propagate the error.
64. **`internal/config/sources.go:27`** — `os.ExpandEnv` silently blanks unset variables, and the same expansion (only `~/` aware) is copy-pasted in `internal/web/m4.go:264` and `internal/ui/m4.go:290`, so the TUI and server can load different connection sets. *Fix:* one exported `ExpandPath` that errors on unset vars.
65. **`internal/config/sources.go:40`** — a duplicate name across source files is shadowed silently (order depends on sorted filenames), while one bad source file aborts startup for *all* connections. *Fix:* warn on shadowing and degrade per file.
66. **`internal/config/connections.go:473`** — a `+ssh` URL with `usePrivateKey=false` drops `ssh_password` silently, producing a connection that looks complete and fails at connect time. *Fix:* return a `dropped` list like the file-import path.
67. **`internal/config/tags.go:19`** — tag colours are unvalidated (unlike connection colours), and names are not trimmed, so `name=" local"` never matches. *Fix:* reuse the colour regex and require a trimmed non-empty name.
68. **`internal/config/config.go:283`** — an MCP token with an empty `value` (or empty `name`) is accepted and can never authenticate, with no warning. *Fix:* require both.
69. **`internal/config/config.go:184`** — `ResolveAIKey` takes no context and runs `api_key_cmd` with `context.Background()`, so a 1Password unlock prompt parks an HTTP request for 30 s with no cancel. *Fix:* accept `ctx` and derive the timeout from it.
70. **`internal/store/state.go:229`** — `OpenTabs` stores unbounded editor text in `state.json`, which `do()` re-reads and rewrites on nearly every UI action; a 12 MB file means a 12 MB write per navigation. *Fix:* cap `Text` or move tab bodies to their own files.
71. **`internal/store/state.go:335`** — `SetGroups` stores the caller's slice while every other setter deep-copies, so the sidebar's later mutations leak into the "saved" state and are then discarded by the next `do()`. *Fix:* deep-copy.
72. **`internal/config/lock_windows.go:13`** — the Windows lock has no test, no timeout, discards the unlock error, and behaves differently from `flock` on re-entrant acquisition. *Fix:* inject a locking interface and test the failure paths.
73. **`internal/config/connections.go:329`** — rename-over-target fails on Windows while another process holds the file open (a common complaint for `state.json`), with no retry. *Fix:* bounded retry on `ERROR_ACCESS_DENIED`.
74. **`internal/config/portable.go:87`** — `ImportConnectionsOpts` reads the whole input with no size limit; only the web transport caps it at 32 MB, so the TUI/CLI path will happily read gigabytes. *Fix:* `io.LimitReader` inside the parser.

### TUI

75. **`internal/ui/app.go:636`** — "close other connections" from the workspace picker always answers "no other connection is open", because `closeOthers()` starts with `a.ws()`, which is nil on the welcome screen. *Fix:* use the picked entry and hide the item when fewer than two workspaces exist.
76. **`internal/ui/theme.go:45`** — the palette rewrite dropped `driver.sqlite`, `driver.duckdb`, `driver.cockroachdb` and `driver.d1`, so those stripes fall back to dim text. *Fix:* restore the four keys in both palettes (or drop them with the drivers).
77. **`internal/ui/frame.go:18`** — the 44-cell wordmark moved the tagline threshold to 94 columns, so a standard 80-column terminal loses the tagline and a 40-column one clips the art mid-glyph. *Fix:* separate `logoMinW` from the tagline threshold.
78. **`internal/ui/util.go:195`** — `ifNoQuery` discards the query-list error, so an unreadable queries dir turns "name already exists" into a silent overwrite. *Fix:* return the error as a toast.
79. **`internal/ui/util.go:174`** — `writeAtomic` leaves the saved file at 0600 and replaces symlinks instead of writing through them (the old `os.WriteFile` did neither). *Fix:* `Stat` + `Chmod` the existing mode and resolve symlinks.
80. **`internal/ui/editor.go:1157`** — `Find`/`FindPrev` set `anchor`/`cur` but leave `blk` set, so with a block selection the highlight, `Selection()` and "run current" keep using the old rectangle. *Fix:* clear `e.blk` when starting a find.
81. **`internal/ui/tab_query.go:600`** — `m` (message view) and `s` (save message) do nothing on a failed statement, because `resultsView` handles `isErr` before `msgView` — exactly where the server message matters. *Fix:* order the cases so the message view wins.
82. **`internal/ui/doc_views.go:543`** — the quick-look encoding switcher is offered for plain text cells (a `string` also fills `rawBytes`), so pressing `v` turns normal UTF-8 into mojibake. *Fix:* gate on binary/`[]byte`.
83. **`internal/ui/app.go:411`** — reconnect and database switch do not release per-tab pins, so a pinned tab keeps a closed session and later writes through it. *Fix:* release pins in `reconnectedMsg`/`dbSwitchedMsg` with a toast.
84. **`internal/ui/workspace.go:463`** — fold state is persisted under `conn/tab<id>` but `restoreTabs` creates tabs with fresh ids, so folds never come back after a restart. *Fix:* key restored tabs by position/content and persist folds in `store.OpenTab`.
85. **`internal/ui/app.go:134`** — `askSecret` uses an unbuffered channel send/receive with no context, so quitting while the password prompt is open parks the connect goroutine forever. *Fix:* `select` on a done channel and close the answer channel once.
86. **`internal/ui/struct_view.go:616`** — `now := t.ws.sess.Caps().Dialect; _ = now` is dead code that also calls `Caps()` on every dialog open. *Fix:* delete.
87. **`internal/ui/dialog.go:545`** — in `pickerDlg`, `p.keys` is consulted before the filter input, so a bound letter cannot be typed into the filter box. *Fix:* only consult `p.keys` when the query is empty.
88. **`internal/ui/app.go:349`** — `liveMsg.f` is called unconditionally; a single `nil` return from any `liveCmd` kills the UI loop and loses unsaved edits. *Fix:* guard `m.f == nil`.
89. **`internal/ui/tab_query.go:486`** — result tabs are numbered `len(t.res)+1`, so keeping a pinned "Query 3" and running again produces "Query 2". *Fix:* a monotonic per-tab counter.
90. **`internal/ui/workspace.go:686`** — new query-only bindings (`split_down`, `wrap_lines`, `variables`, `lower_case`, …) print the raw action id ("`split_down` is not available here") on non-query tabs, while similar actions have friendly messages. *Fix:* add a "needs an SQL editor" branch.
91. **`internal/ui/app.go:153`** — MCP startup failure and a theme-file error both assign the single toast, so only one is ever shown. *Fix:* join them into one dialog.
92. **`internal/ui/m3.go:515`** — the backup dialog is pushed before `w.job` accepts it, so it can end up with no output and its `x` cancels an unrelated job. *Fix:* push after acceptance and scope the cancel.
93. **`internal/ui/m2.go:112`** — export refuses from a table's Structure/DDL view with "open a table or run a query first", although a table is open and exporting it is possible. *Fix:* drop the `view == viewData` condition for the table ref.
94. **`internal/ui/m2.go:122`** — "All result tabs" disappears when the focused result is a message/error tab, even though other tabs have rows. *Fix:* build `all` from `q.res` unconditionally.
95. **`internal/ui/m2.go:278`** — the import CSV preview reads and parses 100 records on the UI loop, so a large/networked file freezes the TUI with no spinner or cancel. *Fix:* move it into a `liveCmd`.
96. **`internal/ui/m2.go:263`** — "Create the table" is allowed for an existing table or a view, so the import fails at `CREATE TABLE` before inserting anything. *Fix:* hide it or validate.
97. **`internal/ui/m3.go:381`** — the grant form defaults Object to `"public"`, producing `GRANT SELECT ON TABLE "public"."public"` when the sidebar is on a function/sequence. *Fix:* leave it empty and validate `schema.table`.
98. **`internal/ui/m2.go:485`** — `openFiltered` assigns `t.filters` before the pending-edits guard, so cancelling "Discard pending changes?" still leaves the new filter pill (and it applies on the next reload). *Fix:* assign inside the guarded continuation.
99. **`internal/ui/assistant.go:134`** — the assistant hardcodes a 5-minute deadline instead of `general.query_timeout_secs` and has no retry for 429/5xx, unlike every query path. *Fix:* derive the deadline from config and offer a retry.
100. **`internal/ui/assistant.go:214`** — `View` re-wraps and re-highlights the whole transcript on every frame, driven by a ~12 fps spinner, so long chats lag while typing. *Fix:* cache per message keyed on width and message count.

---

## Packaging and docs

The infrastructure findings are detailed in Appendix B (Docker exposure, non-root mount, wildcard URL, `--mcp-config`, mode conflicts, Makefile/GoReleaser, man page, version strings). Two documentation items are not covered there:

- **`README.md:28`** — `go install webtuidb@latest` cannot work: `go.mod` declares `module webtuidb`, and the toolchain rejects a module path without a dot in its first element (`verified: malformed module path`). Replace with clone + `make web build`, or rename the module.
- **`PLAN.md:41,373,395,426-439`** — the milestone status contradicts the code in several places: "M3 is in progress … neither UI is wired" under a "milestones 1-8 complete" header, "`SUPPORTED` omits 43 bound actions" (it omits 0 of 130), and five "debt" bullets that no longer exist (DELIMITER support, SSH passphrase prompt, Windows lock, sqlfmt CASE/ON CONFLICT/FOR UPDATE, MCP approval). Stale claims like these are how the next reviewer misses real regressions.

---

## What was checked and looks right

Worth recording so the next review does not re-litigate it:

- **MCP auth:** every web route for the new handlers goes through `s.api(...)`, which enforces the constant-time token, the login cookie and the Origin check; MCP sessions are bound to the token that created them (`TestSessionBoundToToken`).
- **SQL construction:** filters, sorts, board variables, admin statements and bulk updates go through `Dialect.Quote` plus real placeholders; `RawColumn` is the single deliberate raw path (and is the subject of P1-06). `Kill`/`Processes` parse or parameterise their ids.
- **Backup command construction:** argv vectors, no shell, password via env/temp 0600 file, `-`-prefixed names rejected — the browser-supplied database/file names are not a command-injection surface (the display string is: Appendix A).
- **Plugin execution:** no shell, output capped at 1 MiB.
- **`internal/db/ssh.go` credential handling:** the bastion never receives the target's password, host keys are verified per hop, secrets are never logged, and the tunnel is closed on every handshake failure path including the parent.
- **Frontend rendering:** no `dangerouslySetInnerHTML`, no `innerHTML`, no `href`/`src` built from server data. SQL, DDL, plans, plugin output and AI replies are all text nodes — the frontend has correctness bugs but no XSS sink.
- **`go build ./...`** is clean, and the clickhouse/mysql `plan.go` schema-op refusals are correct for those engines.

---

## Suggested fix order

1. **Crashes and data loss first (P1-01…P1-05, P1-11, P1-14, P1-15).** These are cheap, isolated fixes and they are the ones a user hits in the first ten minutes.
2. **Silent corruption and secrets (P1-07…P1-10, P1-12, P1-13, plus P2-029/P2-031/P2-040).** Encoding, CSV formulas, partial exports, history adoption, cleartext variables, world-readable config.
3. **Security gates (P2-072, P2-083, P2-087, P2-093, P2-094, Appendix A origin/read-only items).** The safe-mode and MCP gates are the product's safety story; a bypass there invalidates the feature.
4. **Driver correctness (P2-002…P2-015, P2-024…P2-033).** The new profiles advertise capabilities they cannot deliver; either implement or clear the flags, otherwise onboarding to Redshift/Vertica/SingleStore is broken on first use.
5. **Shortcut reachability (P1-16…P1-18, plus P2-059/P2-060).** Both UIs share one keymap now, so a single encoding fix restores five actions at once.
6. **Test gaps that let the above regress.** Highest value: a test per new keybinding, `BulkUpdate` table test, pinned-connection kill/limit tests, and the tab-restore/assistant-failure tests.
7. **Packaging and CLI (Appendix B).** B-2 (a URL silently replaced by a same-named saved connection) and B-1 (plaintext exposure in the only documented container command) are the two that can surprise a user on day one; the rest is release hygiene.

---

## Method and limits

- Every finding cites a file and line from the working tree at review time; line numbers will drift as fixes land.
- Findings marked `confidence: medium` were not reproduced end-to-end (no live Redshift/Vertica/SingleStore instance, no Windows host, no browser with a non-US layout); they follow from the code and library semantics but should be confirmed before a large refactor.
- The review did not run the full `go test ./...` or `-race` suite; agents ran the packages they touched, and the reported test failures (P2-003, P2-008) were observed directly.
- This document intentionally lists fewer than 100 P1s. The three tiers above are exhaustive for what the review found; the remaining candidates in the per-area notes are the same kind as the P3 items, not additional blockers.

---

## Appendix B — infrastructure, CLI and packaging (late pass, verified by building and running)

This pass built the binary, the Docker image and the GoReleaser snapshot, and probed the CLI. Findings are verified unless noted.

### B-1 (P2) · The documented Docker command serves the DB console over plaintext HTTP on every interface
- **Where:** `Dockerfile:22`, `README.md:23-24`
- **Why/trigger:** the ENTRYPOINT bakes in `--listen 0.0.0.0:7788 --insecure-listen`, so the README's `docker run -p 7788:7788 webtuidb` is the only documented deployment and it is bound to all interfaces without TLS. The random token, the `[web]` login, SQL text and result rows cross the network in cleartext; a sniffer can lift the token and query the user's databases. `web.Run` prints a warning, but the README never mentions TLS, the token or the exposure, and the flag cannot be disabled without overriding `--entrypoint`.
- **Fix:** document (and ideally ship an example `config.toml` with `[web] tls_cert`/`tls_key`) TLS as the supported exposure path; add a warning next to the `docker run` example; gate `--insecure-listen` behind an explicit env var in a tiny entrypoint wrapper.
- **Confidence:** high (image built and run; `GET /` returned 200 with the real bundle).

### B-2 (P2) · A URL argument is silently replaced by a saved connection with the same name
- **Where:** `main.go:325-331`
- **Why/trigger:** saved `Prod` → `prod-db.internal`; run `webtuidb 'postgresql://localhost:5432/sandbox?name=Prod'`. `pick` returns the saved connection and discards the URL entirely (host, port, user, password, database). The URL is documented as "opens that connection directly", so a pasted test URL can run against production with the saved `safe_mode`.
- **Fix:** use the parsed URL and either suffix the generated name (`Prod-2`) or refuse with a clear "that name is taken" error.
- **Confidence:** high.

### B-3 (P2) · `--mcp-config` snippets omit `--config`, so the client loads a different configuration
- **Where:** `main.go:203-213`, test at `main_test.go`
- **Why/trigger:** `webtuidb --config ~/work/conns.toml --mcp-config claude-code` emits `args: ["--mcp"]`; the client re-launches with the default `~/.config/webtuidb/config.toml`, so it sees different connections, tokens and AI settings. The test asserts only the single argument, locking the bug in.
- **Fix:** emit `["--mcp","--config", <abs path>]` when a non-default config is in use, and build the JSON with `encoding/json`.
- **Confidence:** high.

### B-4 (P2) · Conflicting modes and stray arguments are accepted silently
- **Where:** `main.go:106-117,153-159,183-189`
- **Why/trigger:** verified `--mcp --frontend` exits 0 after serving stdio (no HTTP port — a container would restart-loop), `--keys --frontend` prints keys and ignores the frontend, `--theme --keys` prints the theme, `--list-connections --keys` prints keys, `--mcp prod-db` ignores the argument. Only `--frontend` with an argument and the listen/token flags are validated.
- **Fix:** after `flag.Parse`, reject incompatible combinations and any unexpected positional arguments for `--mcp`, with one message each.
- **Confidence:** high.

### B-5 (P2) · The documented bind mount makes the container fail or be unable to save, on Linux
- **Where:** `Dockerfile:1`, `README.md:24`; code at `internal/config/connections.go:232-235,263`
- **Why/trigger:** `loadConnections` chmods a group/world-readable `connections.toml` and returns an error when chmod fails. With `-v ~/.config/webtuidb:/home/nonroot/.config/webtuidb`, a host-owned 0644 file makes uid 65532's chmod fail and the process exits at startup; `SaveConnections` also needs write access to that directory. (Verified on Docker Desktop/macOS the uid mapping hides it, so this is Linux-specific.)
- **Fix:** document `--user $(id -u):$(id -g)` (or ship a compose file that sets it), and downgrade "cannot fix permissions on a file we do not own" to a warning.
- **Confidence:** medium (Linux not exercised).

### B-6 (P3) · Only the config directory is mounted, so history, saved queries, state and boards are lost on recreate
- **Where:** `README.md:24`
- **Why/trigger:** those live under `/home/nonroot/.local/share/webtuidb`; the documented command mounts only `~/.config`. `docker rm` discards them while the UI presents them as persistent.
- **Fix:** add `-v webtuidb-data:/home/nonroot/.local/share/webtuidb` to the documented command and the Dockerfile header.
- **Confidence:** high.

### B-7 (P3) · `--frontend` prints an unusable wildcard URL, and it is the only place the token is shown
- **Where:** `internal/web/server.go:128` (seen via `Dockerfile:22`)
- **Why/trigger:** logs show `webtuidb web UI: http://[::]:7788/#token=…`; `ln.Addr()` is the unspecified address, which users cannot paste or share.
- **Fix:** print `http://localhost:<port>/#token=…` when the listen host is unspecified, optionally followed by the bound address, or add a public-URL override.
- **Confidence:** high (reproduced).

### B-8 (P3) · `--ssh-log` creates the file before validation and before mode dispatch
- **Where:** `main.go:98-105`
- **Why/trigger:** `webtuidb --ssh-log /tmp/x.log --keys` leaves a 0-byte 0600 file; a bad log path makes an informational command fail; `--ssh-log` with a non-frontend `--listen` creates the file then errors.
- **Fix:** open the log after the mode/flag validation, or lazily on the first tunnel with `sync.Once`.
- **Confidence:** high (verified).

### B-9 (P3) · `--mcp` has no signal handling or session draining
- **Where:** `main.go:158`
- **Why/trigger:** `ServeStdio(context.Background())`; SIGINT during an in-flight `execute_query` kills the process with default disposition and leaves open sessions and SSH tunnels unclosed, unlike the web path (`signal.NotifyContext` + drain + `closeAll`).
- **Fix:** use `signal.NotifyContext`, add `Server.Close()` that closes every open session, and `defer` it.
- **Confidence:** high.

### B-10 (P3) · `make build` does not depend on `web`, so it embeds stale or absent assets
- **Where:** `Makefile:3-7`
- **Why/trigger:** edit a React component, `make build`, run `--frontend`: the old UI is embedded with no warning. On a clean clone the binary is API-only (documented), but the failure mode after a web change is silent. `web` is `.PHONY`, so chaining it reruns `npm ci` every time.
- **Fix:** add `build: web` (plus a `build-api` target) or a real file dependency on `internal/web/dist/index.html`, and assert the dist exists in `build`.
- **Confidence:** high.

### B-11 (P3) · `make up` still starts the removed CockroachDB service
- **Where:** `Makefile:11`
- **Why/trigger:** the driver package is deleted and no test uses the service, but `up` still pulls `cockroachdb/cockroach` and waits for it, slowing the documented dev loop and adding a failure surface.
- **Fix:** drop it from the `up` list (keep the compose service for manual use).
- **Confidence:** high.

### B-12 (P3) · GoReleaser builds are not reproducible and cannot run in its official image
- **Where:** `.goreleaser.yaml:4-15`
- **Why/trigger:** the darwin snapshot binary embeds `/src/internal/keymap/key.go` and `/usr/local/go/src/...` (no `-trimpath`), while the Docker build uses it — the two artifacts differ for no reason. `mod_timestamp` is unset. The `before` hook `make web` fails in `goreleaser/goreleaser` with `npm: not found`, so the documented release path cannot run there.
- **Fix:** add `flags: [-trimpath]`, `mod_timestamp: "{{ .CommitTimestamp }}"`, and either a Node step or a `--skip=before` contract; pin the GoReleaser version.
- **Confidence:** high (snapshot built, hook reproduced failing).

### B-13 (P3) · Nothing publishes or signs releases, yet the README points at Releases and Homebrew
- **Where:** `.goreleaser.yaml:25-26`, `.github/workflows/ci.yml`, `README.md:20-31`
- **Why/trigger:** there is no tag-triggered release workflow, `make release` is `--snapshot`, and `checksums.txt` is unsigned with no SBOM, while the README tells users to download from Releases and `brew install --HEAD` (no `brews:` block exists).
- **Fix:** add a tag workflow that installs Node + a pinned GoReleaser, runs a real release, and signs checksums (cosign) with an SBOM; add the `brews:` block or soften the README.
- **Confidence:** high.

### B-14 (P3) · Version strings differ across install paths
- **Where:** `main.go:194-200`, `.goreleaser.yaml:15`, `Makefile:21`, `Dockerfile:16-17`
- **Why/trigger:** verified `make build` → `v0.0.0-2026…+dirty`, `make docker` → `v0.1.0-4-gabc123-dirty`, goreleaser → `0.0.0-SNAPSHOT-799f060`, `go install` → `v0.1.0`. The build-info fallback also fires for every plain `go build` in a checkout, so local builds never say `dev`.
- **Fix:** normalise in one place (trim the `v` prefix, append `-dirty` deliberately) and pass a consistent version from each build path.
- **Confidence:** high.

### B-15 (P3) · The man page's exit status is wrong and it omits `-list-connections`
- **Where:** `docs/webtuidb.1:82-83` (also `:63`, `:69`)
- **Why/trigger:** `webtuidb --nope` exits **2** (Go's flag package), but the page says "0 on success, 1 on any error". `-list-connections` — which the generated completions call — is undocumented, and FILES omits `[csv]`, `[ai]`, `[mcp]`, `[[tag]]` and `[[source]]`.
- **Fix:** "1 on error, 2 on a usage error"; document the flag and the missing sections.
- **Confidence:** high.

### B-16 (P3) · Unpinned base images/toolchain, and an RC dependency in every binary
- **Where:** `go.mod:3,101`, `Dockerfile:2,10,19`
- **Why/trigger:** `go 1.27.3` is a patch-level directive, so older 1.27.x toolchains must download one; `golang:1.27-alpine`, `node:22-alpine` and `gcr.io/distroless/static-debian12:nonroot` float on tags with no digest pin and the image has no `HEALTHCHECK`; `go.yaml.in/yaml/v4 v4.0.0-rc.2` reaches every binary via the Anthropic SDK.
- **Fix:** pin `golang:1.27.3-alpine` (or add `toolchain`) and digest-pin the base images, add a healthcheck (a `/healthz` route, since distroless has no curl), and plan the RC dependency upgrade.
- **Confidence:** medium.

### Verification performed for this appendix

- `go build ./...`, `go vet ./...`, `go test ./...` — all pass; six-target cross-compile (`linux/darwin/windows` × `amd64/arm64`) passes, so deleting the CGO drivers broke no build.
- `go mod tidy -diff` empty, `go mod verify` clean.
- Full `docker build` succeeded (~21 MB, uid 65532, no healthcheck); `docker run -p 7799:7788` served the real `index.html`; container logs printed the wildcard URL.
- `goreleaser check` validates; `goreleaser release --snapshot --skip=before --parallelism=2` produced six archives plus checksums; without `--skip=before` it fails on missing `npm`. `dist/` was removed afterwards.
- CLI probes: unknown flag → exit 2; `--mcp --frontend` → exit 0 with no HTTP; `--mcp extra` → exit 0; `--keys --frontend` and `--theme --keys` → silent precedence; `--ssh-log … --keys` → 0-byte file; a legacy `driver = "sqlite"` connection file → every mode exits 1.
- Not run: GoReleaser on the host (used Docker), `nroff` (not installed), and `-race` (left to CI).
