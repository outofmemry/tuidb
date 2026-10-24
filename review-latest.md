# Code review — tuidb (full codebase)

Scope: all tracked Go code (41,218 lines), the untracked web frontend (`web/src`, `internal/web`), plus build/CI/docs.
Review method: 18 parallel review agents, one per subsystem, followed by manual verification of every P1 claim against the source (and targeted throwaway tests for the reachable ones).
Baseline checks: `go build ./...`, `go vet ./...`, and `go test ./...` pass on the current tree. The UI-01 panic is not covered by a committed test; it was reproduced with a temporary test (since removed), and the untracked `internal/ui/zz_temp_test.go` that reproduced it during the review has also been cleaned up.

Severity definitions used here:

- **P1** — real bug: wrong user-visible behavior, data loss/corruption, crash, security issue, or silent failure on a realistic path.
- **P2** — edge case, defense-in-depth gap, or a real but low-frequency failure.
- **P3** — minor, cosmetic, or latent/defensive concern.
- **UNSURE** — the reviewer could not confirm reachability or impact; not counted as a defect.

> Note on the "100 P1s" target: the codebase does not contain 100 defensible P1 defects. Inflating P2/P3 items to P1 would make the report unusable. This document reports every real defect found and labels severity accurately: **194 findings — 23 P1, 87 P2, 84 P3** (plus UNSURE items called out inline, not counted).

## Fix order (highest risk first)

1. **Data-destroying HTTP paths:** WEB-01 (`DELETE /api/queries/` wipes all saved queries) and WEB-24 (empty `PUT /api/connections` wipes all connections). Both are one-line guards.
2. **Crashes reachable in normal use:** UI-01 (undo/redo panic, reproduced during this review), UG-02 (preview before structure load), UI-02 (Json view + empty reload), FE-01 (aborted body read → blank app), CC-01 (NULL field in a UDT).
3. **Silent writes to the wrong place:** DB-01 (Mongo regex merges a write into a "read-only" statement and skips Safe Mode), CM-01/OD-01 (`USE` with a comment switches one pooled connection), WEB-14 (staged edits resolve against a replaced row snapshot), UG-01 (Enter after a row action writes into another row).
4. **Silent data corruption:** CAS-01 (batch timestamp makes delete win over insert), CAS-02 (counter overflow → `MaxInt64`), MY-01 (binary default re-emitted as text), WEB-15 (0-row update reported as success), OD-03 (Redis rename overwrites an existing field).
5. **Shared-file clobbering:** STORE-01/STORE-02 (TUI and web overwrite each other's state and connections), STORE-03 (password in URL parse error).
6. **Build:** BUILD-01 (fresh clone cannot compile because `all:dist` has no committed files).

## Index

- UI: UI-01 (undo panic), UI-02–UI-12 (workspace/editor state), UI-13–UI-23 (welcome, dialogs, sidebar, connect), UG-01–UG-09 (grid/result/structure editing).
- HTTP API: WEB-01–WEB-13 (server/sessions), WEB-14–WEB-23 (change mapping), WEB-24–WEB-32 (handlers/store).
- Web frontend: FE-01–FE-12 (api/workspace/query), FE-13–FE-23 (grid, filters, connections, sidebar).
- Cassandra: CAS-01–CAS-12 (plan/admin), CM-01–CM-08 (metadata/classifier), CC-01–CC-08 (core reader/session).
- Store/config: STORE-01–STORE-12.
- Build/CI/docs: BUILD-01–BUILD-11.
- Drivers: DB-01–DB-12 (split/classify/filter), PG-01–PG-11, MY-01–MY-12, DDB-01–DDB-12, OD-01–OD-10 (ClickHouse/Redis/Mongo).

---

## Summary

| Area | P1 | P2 | P3 |
|---|---|---|---|
| UI (editor / workspace / grid) | 5 | 12 | 15 |
| `internal/web` server + handlers | 2 | 12 | 7 |
| `internal/web` change mapping | 2 | 5 | 3 |
| Web frontend (`web/src`) | 3 | 11 | 9 |
| Cassandra driver | 4 | 8 | 16 |
| Store / config / safemode | 3 | 7 | 2 |
| Build / CI / docs | 1 | 5 | 5 |
| DB core (split/classify/filter) | 1 | 6 | 5 |
| DynamoDB driver | 0 | 4 | 8 |
| PostgreSQL driver | 0 | 8 | 3 |
| MySQL / MariaDB | 1 | 4 | 7 |
| ClickHouse / Redis / Mongo | 1 | 5 | 4 |
| **Total** | **23** | **87** | **84** |

Status: all 18 subsystem reviews complete. P1 findings were manually re-verified against the source (and several by targeted tests); the detailed sections follow.

---

# UI-01 — Undo/redo leaves the render cache stale and panics the app

**Severity: P1 (crash)**
**Location:** `internal/ui/editor.go:371-394` (`Undo`, `Redo`), panic at `internal/ui/editor.go:777`

`Undo` and `Redo` replace `e.lines`, `e.cur`, and `e.anchor` but never set `e.cache.dirty`. `markChanged()` is the only cache invalidation path, and neither function calls it:

```go
func (e *Editor) Undo() {
    ...
    e.lines, e.cur, e.anchor, e.lastKind = s.lines, s.cur, nil, ""
    e.ac.open, e.Changed = false, true
    e.scroll()
}
```

`View` only rebuilds the cached line offsets when `cache.dirty` is set (`editor.go:731-733`), then indexes them per visible line:

```go
bytePos = offs[ri] + len(string(r[:min(e.left, len(r))]))
```

**Trigger (reachable from the UI):** edit a buffer so it loses lines, then press Ctrl+Z (`keymap/action.go:63` → `tab_query.go:646` → `Editor.Undo`). If undo restores more lines than the cache was built for, `offs[ri]` is out of range and the process panics. Reproduced by `internal/ui/zz_temp_test.go`:

```
after delete: "select 1\n"
after undo: "select 1\nfrom t\nwhere a = 1"
panic: runtime error: index out of range [2] with length 2
  internal/ui.(*Editor).View ... editor.go:777
```

Even when it does not panic, stale `cache.kinds` and `cache.stmts` produce incorrect syntax highlighting and current-statement highlighting until the next edit. `make test` is red because of this.

**Fix:** call `e.markChanged()` in `Undo`/`Redo` (or a dedicated `invalidateCache()`), and guard the per-line index (`ri < len(offs)`) as defense in depth. Add a regression test that undoes an edit which removed lines.

---

# WEB-01 — `DELETE /api/queries/` deletes every saved query

**Severity: P1 (irrecoverable data loss)**
**Location:** `internal/web/server.go:244`, `internal/web/handlers_store.go:63-65`, `internal/store/queries.go:68-73`, `:121-127`

The route is `DELETE /api/queries/{path...}`. Go's wildcard matches the empty remainder, so `DELETE /api/queries/` calls `Queries.Delete("")`.

```go
func (s *Queries) abs(rel string) (string, error) {
    clean := filepath.Clean(filepath.FromSlash(rel))   // Clean("") == "."
    if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
        return "", fmt.Errorf("invalid query path %q", rel)
    }
    return filepath.Join(s.Root, clean), nil          // Root/.
}

func (s *Queries) Delete(path string) error {
    p, err := s.abs(path)
    ...
    return os.RemoveAll(p) // also removes a folder
}
```

`DELETE /api/queries` (no slash) is redirected by the mux to the trailing-slash form with 307, which preserves the method, so both spellings reach the handler. `DELETE /api/queries/a/..` cleans to the root as well. The response is `200`, and there is no confirmation or trash.

**Trigger:** any client/tool that treats the collection URL as "the queries collection", or a folder-delete UI that sends an empty path. Every saved `.sql` file under the store root is removed.

**Fix:** reject an empty/clean-`.` relative path in `abs` (and `".."` prefixes) before joining; alternatively require a non-empty `{path}` and serve `DELETE /api/queries` as `405`/`400`. Add a store-level unit test for `Delete("")`, `Delete(".")`, `Delete("a/..")`.

**Verified:** manually traced the route and `abs`/`Delete`; the store root is the actual store directory from `config`.

---

# WEB-02 — Saved-query deletion is unauthenticated-by-path but also has no CSRF token (context)

Not filed separately. The server requires `Authorization: Bearer`, and no CORS allow headers are sent, so cross-site requests cannot attach the token. This is context for WEB-08 rather than a defect of its own.

---

# WEB-03 — Sessions never expire; reloading the page leaks a live DB connection

**Severity: P2 (resource exhaustion over time)**
**Location:** `internal/web/sessions.go:63-74`, `internal/web/server.go:111-118`, `internal/web/handlers.go:147-153`

Sessions are removed only by an explicit `DELETE /api/sessions/{id}` or process shutdown (`closeAll`). There is no idle timeout, no cap, and no reaper. The frontend has no `beforeunload`/`sendBeacon` hook; every page reload (or a `POST /sessions` whose response write fails because the browser aborted) leaves the pool, the SSH tunnel, and (with `KeepAlive` default true) a ticker alive until the process exits.

**Trigger:** connect, reload the browser tab, repeat. Each reload leaves one pool. Eventually the database refuses new connections.

**Fix:** add an idle deadline per session (touch on each request), a maximum session count, and abort unpublished sessions when the response write fails. Optionally add a `visibilitychange`/`sendBeacon` disconnect.

---

# WEB-04 — Shutdown closes pools while handlers are still draining

**Severity: P2**
**Location:** `internal/web/server.go:111-118`

`srv.Serve` returns `ErrServerClosed` as soon as the listener closes, so `s.closeAll()` runs immediately, while `srv.Shutdown(ctx)` is still waiting (3 s) for active handlers. `Run` then returns and `main` exits.

**Trigger:** Ctrl-C during a long `/apply`. `apply` deliberately detaches from request cancellation so the commit is not cut in half, but `closeAll` closes its pool and the process exits. The commit can be left half-applied with no error surfaced.

**Fix:** close sessions only after `Shutdown` returns (or after a bounded wait), and let in-flight applies finish or report failure.

---

# WEB-05 — `/apply` has no deadline and wedges the session if the commit hangs

**Severity: P2**
**Location:** `internal/web/handlers.go:507-521`

`ss.o.Sess.Apply(context.WithoutCancel(r.Context()), stmts)` has no timeout, and `ss.applying` is cleared only in a `defer` after `Apply` returns. The plan is deleted at line 512 before the commit.

**Trigger:** a commit blocked on a row/table lock (Postgres lock waits have no default timeout) or a black-holed connection. The handler and connection stay stuck; every later apply on that session returns `409 "a change is still being sent"`; shutdown's drain expires and hits WEB-04.

**Fix:** apply a configurable commit timeout, or track the applying state with a cancellable job and surface a stuck state.

---

# WEB-06 — Client `Limit` bypasses the configured row limit

**Severity: P2 (potential OOM)**
**Location:** `internal/web/handlers.go:300-302`, `internal/web/sessions.go:132-152`, `internal/db/filter.go:218-219`

```go
if req.Limit <= 0 { req.Limit = ss.rowLimit() }
```

A caller-supplied positive limit is passed straight to `db.RowQuery` and rendered as `LIMIT %d`; `limit < 0` means no limit. The server then caches the entire converted result (up to 50 results per session), so a large limit can consume unbounded memory.

**Trigger:** `web/src/DataTab.tsx:229-230` has `min={1}` and no max; a typo such as `3000000` makes the server fetch, convert, and retain millions of cells.

**Fix:** clamp to the configured `row_limit` (or a hard server maximum) in the handler, and reject negative limits unless an explicit override is configured.

---

# WEB-07 — SPA fallback answers missing static assets with `200 text/html`

**Severity: P2 (blank app after rebuild)**
**Location:** `internal/web/server.go:264-276`

Any path not found in `dist` falls through to `http.ServeFileFS(w, r, sub, "index.html")`, including `/assets/<hash>.js`.

**Trigger:** a browser holds an old `index.html` after a rebuild, requests the old hashed bundle, receives HTML with 200, and fails with a MIME-type error. `GET /index.html` also gets a 301 to `./` from `ServeFileFS`.

**Fix:** return 404 for paths under `/assets/` (and anything with a file extension) and only fall back to `index.html` for navigation requests (`Accept: text/html`).

---

# WEB-08 — DELETE session can close a pool under an in-flight handler; `close()` is not idempotent

**Severity: P2**
**Location:** `internal/web/sessions.go:91-98`, `:112-122`, `internal/web/handlers.go:147-153`

`withSession` returns `*session` after unlocking, with no lifetime guarantee. `closeSession` deletes the map entry and calls `ss.close()` immediately; a handler already holding the pointer keeps using the closed driver pool. Two callers can also race `closeAll`, and `ss.o.Close()` (pool, mongo client, SSH tunnel) is not guarded.

**Trigger:** click Disconnect while a query/apply is running. The in-flight work fails with a driver "pool closed" error; for a non-transactional apply the statements already sent stay applied.

**Fix:** reference-count sessions or take a per-session lock for the duration of a request; make `close()` idempotent (`sync.Once`).

---

# WEB-09 — Origin check compares against the client-supplied Host

**Severity: P2 (defense-in-depth only; exploitability UNSURE)**
**Location:** `internal/web/server.go:161-163`

`o != "http://"+r.Host` compares `Origin` to the attacker-controlled `Host`, so a rebinding page (`Origin: http://evil.com`, `Host: evil.com`) passes. Requests with no `Origin` also pass. Plain CSRF is still blocked because the bearer token is required and is not a CORS-safelisted header; no `Access-Control-Allow-*` headers are present.

**Fix:** compare against a configured expected origin (the listening address) and reject requests with `Sec-Fetch-Site: cross-site`.

---

# WEB-10 — Bearer comparison is constant-time only for equal-length inputs

**Severity: P3**
**Location:** `internal/web/server.go:164-165`

`subtle.ConstantTimeCompare` short-circuits on length mismatch, and a header without the exact `"Bearer "` prefix is compared raw, so timing reveals token length. With 128 bits of `crypto/rand` this is not exploitable.

**Fix:** hash both values (SHA-256) and compare fixed-length digests, then `ConstantTimeCompare`.

---

# WEB-11 — 32 MiB body cap silently truncates and reports "invalid JSON"

**Severity: P3**
**Location:** `internal/web/server.go:197-209`

`io.ReadAll(io.LimitReader(r.Body, 32<<20))` truncates without signal, so oversized bodies reach `json.Unmarshal` cut off and return `400 invalid JSON` instead of `413`.

**Fix:** use `http.MaxBytesReader` and map `*http.MaxBytesError` to 413.

---

# WEB-12 — Ignored errors from `fs.Sub` / `f.Stat`

**Severity: P3 (UNSURE reachability)**
**Location:** `internal/web/server.go:259`, `:264-267`

`sub, _ := fs.Sub(distFS, "dist")` and `st, _ := f.Stat()` discard errors; nil results would panic the request. Not reachable with a valid embed.

**Fix:** check and return 500.

---

# WEB-13 — Stored plans grow without bound per session

**Severity: P3**
**Location:** `internal/web/handlers.go:435-439`, `internal/web/sessions.go:195-201`

Every `/plan` preview inserts into `ss.plans`; entries are removed only by a successful apply, `resetCaches`, or close. Results are capped at 50, plans are not.

**Fix:** cap and evict plans like results, or drop a previous plan for the same target.

---

# FE-01 — A failed JSON body read becomes `{}` and crashes the app

**Severity: P1 (crash / blank page)**
**Location:** `web/src/api.ts:27`, `web/src/QueryTab.tsx:30-31`, `:84`, `web/src/main.tsx:20-24`

```ts
const data = await res.json().catch(() => ({}))
if (!res.ok) throw new ApiError(...)
return data as T
```

`res.json()` runs after `fetch` resolved on the headers; an abort during body download raises inside `res.json()` and is swallowed because `res.ok` is still true.

**Trigger:** run a large query, then press Cancel while the body downloads. `api` resolves `{}`; `setResults(r.results)` stores `undefined`; the next render executes `results.map` (`QueryTab.tsx:84`) and throws. There is no error boundary, so the app goes blank. The same pattern affects `DataTab.tsx:59/77` (`res.cells.map`) and `Workspace.tsx:16` (truthy `{}` treated as metadata).

**Fix:** only swallow a parse failure for error responses; for a successful response, propagate the parse/abort error, and use `res.json()` with a meaningful fallback only when the body is expected to be empty (204).

---

# FE-02 — Overlapping runs are not serialized; the older response can win

**Severity: P1 (wrong result shown)**
**Location:** `web/src/QueryTab.tsx:26`, `:30`, `:38`, `:59`

`send` overwrites `ctrl.current` with a new controller and never aborts the previous request. The CodeMirror keybinding calls `runRef.current(false)` directly, bypassing the `disabled={running}` guard on the buttons.

**Trigger:** press Cmd/Ctrl+Enter twice on a slow query. Request A is orphaned and unaborted; B resolves first and is displayed; A then resolves and replaces B. A's `finally` also sets `running=false` while B is live, re-enabling Run and disabling Cancel, so Cancel no longer aborts the request whose result is still pending.

**Fix:** if a request is in flight, abort it before starting another (or ignore stale responses via a request sequence number); guard `run()` on `running` as well.

---

# FE-03 — Query is not aborted on unmount

**Severity: P2**
**Location:** `web/src/QueryTab.tsx:23`, `:26-30`

No `useEffect` cleanup aborts `ctrl.current`; closing the tab or switching database leaves the server-side query running because the Go handler's `r.Context()` is only cancelled by the HTTP abort.

**Fix:** `useEffect(() => () => ctrl.current?.abort(), [])`.

---

# FE-04 — `json` view flag is not reset; `cur.docs!` can be undefined

**Severity: P2 (render crash)**
**Location:** `web/src/QueryTab.tsx:21`, `:89`, `:95`

The JSON toggle appears only when the current result has docs, but `json` is global state and `cur.docs!` is non-null-asserted. Document engines set `HasRows: true` with nil `Docs` for an empty result (`internal/db/mongo/bsonx.go:274`, `:286`).

**Trigger:** on MongoDB/DynamoDB, run a find with rows, click JSON, run a find that returns zero rows. `json` is still true and `cur.docs!.map` throws, blanking the app.

**Fix:** gate on `json && cur.docs` and reset `json` when the result set changes.

---

# FE-05 — History/saved-query load errors render as empty

**Severity: P2**
**Location:** `web/src/Workspace.tsx:117-118`, `:126-131`

`hist.error` / `saved.error` are never used; `(hist.data ?? [])` shows an empty list on failure, indistinguishable from genuinely empty.

**Fix:** render an error state and retry.

---

# FE-06 — "Clear history" failure is unhandled and leaves stale list

**Severity: P2**
**Location:** `web/src/Workspace.tsx:122`

`onClick={async () => { ... await api(...); hist.refetch() }}` has no `catch`, so a failure is an unhandled rejection and `refetch` is skipped; the user sees no change and assumes it worked.

**Fix:** await in a try/catch, surface the error via `notify`, and refetch in `finally`.

---

# FE-07 — Disconnect failure is swallowed and the session leaks

**Severity: P2**
**Location:** `web/src/Workspace.tsx:56`

`await api('DELETE', '/sessions/'+sid).catch(() => {})` clears local state and navigates away even when the server call fails, leaving the server-side pool alive until shutdown (compounds WEB-03).

**Fix:** report the failure and offer retry; keep local state until the delete succeeds.

---

# FE-08 — Every session-query failure is reported as "This session has ended"

**Severity: P3**
**Location:** `web/src/Workspace.tsx:29`, `:15`

`retry: false` plus a shared query client means a transient `Failed to fetch` or a 401 renders a terminal "session ended" screen with no retry.

**Fix:** distinguish 404/410 (gone) from network/5xx and offer retry.

---

# FE-09 — Toast timer is not cleared or keyed

**Severity: P3**
**Location:** `web/src/Workspace.tsx:28`

A second notification within 3.5 s is blanked by the first timer; a pending timer fires after unmount (no-op in React 18).

**Fix:** store the timer in a ref and clear/replace it.

---

# FE-10 — Password persists in state after the gate dialog is dismissed

**Severity: P3**
**Location:** `web/src/QueryTab.tsx:20`, `:101`, `:107`

`setPassword('')` only runs on success. Closing the dialog with Escape/backdrop keeps the password, so the next gate is pre-filled and can be confirmed without retyping.

**Fix:** clear the password when the gate is closed or replaced.

---

# FE-11 — Saving a query does not invalidate the `['queries']` cache

**Severity: P3**
**Location:** `web/src/QueryTab.tsx:53`, `web/src/Workspace.tsx:118`

The Saved-queries drawer (which can be open behind the editor) does not show the newly saved query until reopened.

**Fix:** `queryClient.invalidateQueries({ queryKey: ['queries'] })` after save.

---

# FE-12 — No router error/not-found component; non-null root assertion

**Severity: P3**
**Location:** `web/src/main.tsx:20-24`

Unmatched URLs render blank, and any render-time TypeError unmounts the tree with no message. `document.getElementById('root')!` turns a markup regression into `createRoot(null)`.

**Fix:** add `defaultErrorComponent`/`notFoundComponent` and check the root element.

---

# CAS-01 — A batched save shares one timestamp; delete-then-insert of the same key silently loses the insert

**Severity: P1 (silent data loss)**
**Location:** `internal/db/cassandra/plan.go:358-372`, ordered input from `internal/ui/grid.go:471-474` and `internal/web/changes.go:50-51`

`Apply` puts every DML save of two or more statements into one `gocql.LoggedBatch`. Cassandra executes a batch with a single request timestamp, and a tombstone at timestamp T deletes cells with timestamp `<= T`. Both grids order deletes before inserts specifically to allow key reuse.

**Trigger:** in one save, delete a row and insert a new row with the same primary key (the "recreate this row" flow, or the documented PK-change workaround at `plan.go:260`). The batch succeeds, the reload shows no row: the delete tombstone wins the timestamp tie. Two inserts with the same PK in one save have the same issue, resolved by cell-byte comparison rather than order.

Single-statement saves take the non-batch path, where the last statement wins as documented.

**Fix:** do not batch a delete and an insert/update that share a primary key; or set explicit, monotonically increasing timestamps per statement in the batch (`Batch.SetTimestamp` per query is not possible in gocql, so split conflicting statements into sequential executions). Alternatively, dedupe key collisions in `Apply` and run those sequentially.

---

# CAS-02 — Counter delta overflowing int64 is applied as `MaxInt64`

**Severity: P1 (irreversible data corruption)**
**Location:** `internal/db/cassandra/plan.go:321-324`

```go
n, _ := strconv.ParseInt(m[1], 10, 64)
```

`ParseInt` returns `(math.MaxInt64, ErrRange)` on overflow and the error is discarded, so a huge delta is applied as `9223372036854775807` instead of failing.

**Trigger:** type `+99999999999999999999` into a counter cell; Plan succeeds and emits `"n" = "n" + ?` with `MaxInt64`, wrecking the counter. `counterDelta` accepts arbitrarily many digits.

**Fix:** check the error and reject with the existing counter message.

---

# CAS-03 — UPDATE/DELETE validate only that Key is non-empty, not that it is the full primary key

**Severity: P2 (defense in depth; a partial key can delete a whole partition)**
**Location:** `internal/db/cassandra/plan.go:308-310`, `:335-341`, `:236-248`

Cassandra accepts `DELETE FROM t WHERE partition_key = ?` as "delete the partition"; it only rejects missing clustering prefixes for UPDATE. The driver never compares the supplied key against the schema, although `colTypes`/`Columns` have the metadata.

Today both UIs guard this (`internal/ui/resultedit.go:99-105` requires all PK columns; `internal/web/changes.go:77-82` errors on a hidden PK), so this is only reachable through the `db.Session` interface directly.

**Fix:** compare `c.Key` column set with the table's partition+clustering columns and reject subsets/supersets.

---

# CAS-04 — `PlanTable` emits ADD before DROP, so same-name column replacement fails

**Severity: P2**
**Location:** `internal/db/cassandra/admin.go:295` (ADD) vs `:320-327` (DROP)

To change a column type, deleting the `x` row and adding a new `x` row with a different type in the same save produces `ALTER TABLE … ADD "x" …` while `x` still exists; the server rejects it ("Column x already exists"). The drop is appended only after every ADD/RENAME.

**Fix:** order drops before adds, or detect a same-name add+drop and error with the documented "add a new column, copy the data, drop the old one" message.

---

# CAS-05 — Clone always fails on the default the UI offers

**Severity: P2 (feature always errors)**
**Location:** `internal/db/cassandra/admin.go:54-56`, `internal/ui/sidebar.go:833-834`

Cassandra rejects `with_data=true`, but the shared clone dialog defaults that checkbox to `"true"` and has no dialect-specific default or `Show` predicate (unlike the `restart_identity` field two dialogs above).

**Trigger:** right-click a table → Clone → accept the default → the operation is refused every time.

**Fix:** default `with_data` to false for Cassandra (or hide the field).

---

# CAS-06 — "Nothing was applied" is wrong for batch timeouts

**Severity: P2 (misleading error)**
**Location:** `internal/db/cassandra/plan.go:369-371`

A `WriteTimeoutException`/`ReadTimeoutException` from a logged batch means the batchlog may still replay the mutations. The error text claims nothing was applied, inviting a retry that overwrites newer data.

**Fix:** qualify the message for timeouts ("the batch may still be applied; verify before retrying").

---

# CAS-07 — Key roles and clustering order are never diffed

**Severity: P2 (silent no-op on structure edits; UNSURE reachability from the TUI)**
**Location:** `internal/db/cassandra/admin.go:307`, `:328-340`

The per-column check ignores `Column.Extra` (which carries "partition key" / "clustering asc|desc" and is what `createTable` reads), and the table-level check compares only names/count. Moving a column between partition and clustering roles, or flipping clustering order, yields `nil` statements and a "done" message.

**Fix:** compare `Extra` roles and clustering order, and reject unsupported changes explicitly.

---

# CAS-08 — Dropping an aggregate can become `DROP FUNCTION` when the cache is cold

**Severity: P3**
**Location:** `internal/db/cassandra/admin.go:28`, `internal/db/cassandra/meta.go:353-364`

`dropTarget` decides function vs aggregate from `s.aggs`, populated only by `Objects()` and cleared by `UseDatabase`. Dropping before listing objects in a keyspace emits `DROP FUNCTION` for an aggregate (error, or the wrong object if a same-signature function exists).

**Fix:** resolve the kind from `system_schema` at plan time or carry it in `ObjRef`.

---

# CAS-09 — Preview/history timestamps are truncated to milliseconds

**Severity: P3**
**Location:** `internal/db/cassandra/plan.go:225`

`quoteLit` formats `.000+0000` (3 decimals) while `Args` keeps microseconds and the grid prints `.999999`. Preview/history show a different value from the one applied.

**Fix:** format with microsecond precision in the display string.

---

# CAS-10 — `Plan` ignores the caller context and uses a fixed 15 s per change

**Severity: P3**
**Location:** `internal/db/cassandra/plan.go:265-267`, `internal/db/cassandra/admin.go:57-58`

Metadata fetches cannot be cancelled by the user, and N changes mean N sequential timeouts.

**Fix:** accept/derive a context from the request (interface change) or a shorter shared deadline.

---

# CAS-11 — Free-text column types/index expressions are spliced into DDL unvalidated

**Severity: P3 (not multi-statement injection; Apply sends the text as one query)**
**Location:** `internal/db/cassandra/admin.go:187`, `:295`, `:229-231`

Identifiers go through `q` and values through args, but `Column.Type` is concatenated verbatim and an index target containing `(` bypasses quoting. A structure-editor entry like `text) WITH CLUSTERING ORDER BY (…` is emitted as written.

**Fix:** validate types against `system_schema.types`/known CQL types, and quote index targets unless they are known function expressions.

---

# CAS-12 — Missing existence checks and unvalidated admin options

**Severity: P3**
**Location:** `internal/db/cassandra/admin.go:32,44,79,71,110,119-121`

- `DROP TABLE`/`DROP KEYSPACE`/`TRUNCATE` are planned without `IF EXISTS` or a schema check; retries return raw server errors and the sidebar can keep a phantom object.
- `OpClone` discards the keyspace error (`ks, _ := s.ksOf(ref)`), producing `CREATE TABLE ""."name"` when no keyspace is selected.
- `createKeyspace` accepts replication factor `0` for `NetworkTopologyStrategy`, silently drops an unrecognized `durable_writes`, and does not validate a non-empty name.

**Fix:** validate options and use `IF EXISTS`.

---

# BUILD-01 — A fresh checkout cannot compile: `go:embed all:dist` has no matching files

**Severity: P1 (build broken for every new clone/CI run)**
**Location:** `internal/web/server.go:32`, `.gitignore:6-7`, `Makefile:5`

```go
//go:embed all:dist
var distFS embed.FS
```

`internal/web/dist` is generated by Vite and its contents are gitignored. There is no committed placeholder: `git ls-files internal/web/dist` returns 0 entries and `ls -a internal/web/dist` shows only `index.html` and `assets/`. `make web` creates `internal/web/dist/.gitkeep` *after* `npm run build`, but nothing commits it, and `make build` does not depend on `make web`.

**Trigger:** clone the repo (with the web server committed) and run `go build ./...`, `go vet ./...`, or `go test ./...` without Node/build steps. Confirmed in a copy of the tree with `dist` removed:

```
internal/web/server.go:32:12: pattern all:dist: no matching files found
```

`README.md:79` claims "plain `make build` works without it, API only", which is false.

**Fix:** commit `internal/web/dist/.gitkeep` (the `.gitignore` already has `!internal/web/dist/.gitkeep`) and verify `go build` on a clean clone; or move the embed behind a build tag with a fallback stub.

---

# BUILD-02 — CI never builds the web app

**Severity: P2**
**Location:** `.github/workflows/ci.yml:13-16`

CI goes from checkout straight to `go vet`, `go test -race`, `make up`, `make smoke`; there is no Node setup or `make web`. Once BUILD-01 is fixed with a stub, CI will be green while never compiling `web/src`, and any binary built in CI serves "web app not built" (`internal/web/server.go:273`). `plan-frontend.md:151` says CI builds the web app before `go build`; it does not.

**Fix:** add `actions/setup-node` + `npm ci && npm run build` (or `make web`) before the Go steps, and assert the dist assets are non-empty.

---

# BUILD-03 — SIGTERM is not handled; graceful shutdown is skipped

**Severity: P2**
**Location:** `main.go` (`signal.NotifyContext`), `internal/web/server.go:109-118`

Only `os.Interrupt` is captured (`signal.NotifyContext(context.Background(), os.Interrupt)`). SIGTERM — the default for `kill`, `docker stop`, systemd, and IDE stop buttons — terminates the process immediately, so `srv.Shutdown` and `s.closeAll()` never run. Open pools/tunnels are not closed and in-flight queries are dropped. The `srv.Shutdown(sh)` error is also discarded.

**Fix:** notify on `syscall.SIGTERM` as well, and log/handle the shutdown error.

---

# BUILD-04 — The web UI cannot insert the first row into an empty table

**Severity: P2 (core feature unusable on empty tables)**
**Location:** `internal/web/handlers.go:313-314`, `web/src/DataTab.tsx:148,180-191`, `internal/web/handlers.go:455`

```go
if cerr == nil && res.HasRows {
    out.Editable = true
    out.ResultID = ss.put(&cached{ref: req.Ref, cols: cols, res: res})
}
```

An empty table has `HasRows == false`, so it gets no `resultId` and `editable=false`. The UI hides "Add row" behind `res?.editable` while simultaneously telling the user "This table has no rows yet. Add one to start." (`DataTab.tsx:191`). Even if an insert were staged, `body()` sends `resultId: undefined` and `/plan` fails in `ss.result("")` (`handlers.go:455`).

**Fix:** create a cached result entry (with the column metadata) for empty tables and mark them editable, or support inserts that do not reference a cached result.

---

# BUILD-05 — `make up` silently ignores a failed Redis seed

**Severity: P2**
**Location:** `Makefile:11`, `docker-compose.yml:34-35`

`docker compose up redis-seed` without `--exit-code-from`/`--abort-on-container-exit` exits 0 even when the one-shot seed container fails, so a failed seed looks successful and `make smoke` fails later with unrelated-looking Redis errors.

**Fix:** `docker compose run --rm redis-seed` or add `--exit-code-from redis-seed`.

---

# BUILD-06 — `.claude/` and `.agents/` are unignored; 12 MB of worktree copies can be committed

**Severity: P2**
**Location:** `.gitignore:1-7`

The working tree contains untracked `.claude/` (including `.claude/worktrees`, ~12 MB of full repo checkouts) and `.agents/`, neither ignored. `git add -A` would commit duplicate trees, nested `go.mod` files, and a symlink into `.agents/`; CI's `gofmt -l .` would then scan the copies.

**Fix:** add `.claude/`, `.agents/`, and `.claude/worktrees/` to `.gitignore` (or commit only the intended skill/config files explicitly).

---

# BUILD-07 — README development section is stale

**Severity: P3**
**Location:** `README.md:3`, `README.md:101-104`

The README lists five engines/databases while `main.go` registers twelve drivers and `docker-compose.yml` starts nine databases plus the Redis seeder. Missing ports/services include mariadb, cassandra, dynamodb, and cockroach.

**Fix:** update the supported-engine list and development database table.

---

# BUILD-08 — Mutable container image tags make CI non-reproducible

**Severity: P3**
**Location:** `docker-compose.yml:7,13,19,26,37,44,55,64`

`amazon/dynamodb-local:latest` plus floating minor tags (`postgres:16`, `mysql:8.4`, `redis:7`, `mongo:7`, `cassandra:5.0`) can change under CI without a repo change.

**Fix:** pin digests or exact patch versions.

---

# BUILD-09 — `plan-frontend.md` contradicts the implemented API/build story

**Severity: P3**
**Location:** `plan-frontend.md:80-82,121,151`

Three statements are false against the code: a stub `dist/index.html` is "checked in" (none is), `POST /connections/parse` is "not added" (it is routed at `internal/web/server.go:218` and implemented at `handlers.go:73`), and CI builds web before `go build` (it does not).

**Fix:** update or delete the plan document before merging.

---

# BUILD-10 — Flags after a positional argument are silently ignored

**Severity: P3**
**Location:** `main.go:54-66`, `:109`

Go's `flag` parsing stops at the first non-flag argument, so `tuidb pg-local --frontend` leaves `*frontend` false, never runs the "only works with --frontend" guard, and silently discards the remaining arguments; the TUI starts instead of reporting the mistake.

**Fix:** reject `flag.NArg() > 1` with a usage error.

---

# BUILD-11 — CI actions use mutable tags and the workflow has no `permissions:` block

**Severity: P3**
**Location:** `.github/workflows/ci.yml:1-10`

`actions/checkout@v4` and `actions/setup-go@v5` are mutable tags (not SHAs), and the default `GITHUB_TOKEN` permissions are not restricted.

**Fix:** pin action SHAs and add `permissions: contents: read`.

---

# STORE-01 — TUI and web silently overwrite each other's `state.json`

**Severity: P1 (silent loss of user state)**
**Location:** `internal/store/state.go:67-79`, loaded once at `main.go:86`; web writes at `internal/web/handlers_store.go:99-135`, TUI writes at `internal/ui/workspace.go:728`

Every mutation re-marshals the whole in-memory `stateFile` and writes it with `WriteFileAtomic`; there is no read-merge and no inter-process lock.

```go
func (s *State) do(f func(*stateFile)) error {
    s.mu.Lock(); defer s.mu.Unlock()
    f(&s.d)                                   // this process's snapshot only
    b, err := json.MarshalIndent(s.d, "", " ")
    ...
    return config.WriteFileAtomic(s.Path, b)  // replaces the whole document
}
```

**Trigger:** run `tuidb` and `tuidb --frontend` together (README:83 advertises that they share pinned/recent tables, filters, and row limits). Pin a table in the TUI, then open a table in the browser (`POST /api/state/touch`, fired by `web/src/Workspace.tsx:42`); the browser rewrites the file from its startup snapshot and the TUI's pins, recents, filters, row limits, and last-session entry disappear with no error.

**Fix:** lock the file (`flock`/lockfile), re-read before mutate, or merge per key with a version/generation number.

---

# STORE-02 — `connections.toml` writes clobber connections added by the other process

**Severity: P1 (silent loss of a connection + its password)**
**Location:** `internal/config/connections.go:208-217`, callers `internal/ui/app.go:168` and `internal/web/handlers.go:42-66`

`SaveConnections` encodes the caller's full in-memory list and replaces the file; it never re-reads or merges. `PUT /api/connections` sends the list the page fetched at mount, and the TUI saves its own list from `pickSafeMode` (`internal/ui/workspace.go:726-733`).

**Trigger:** TUI open, browser open. Add a connection in the TUI, then change any connection's safe mode in the browser and save: the new TUI connection is deleted from disk (including its stored password). The reverse also happens.

**Fix:** same locking/merge strategy as STORE-01; at minimum scope connection saves to the edited connection rather than the whole list.

---

# STORE-03 — `ParseURL` puts the database password into an error message

**Severity: P1 (credential disclosure to stderr/UI)**
**Location:** `internal/config/connections.go:301-305`

```go
u, err := url.Parse(raw)
if err != nil {
    return Connection{}, fmt.Errorf("bad URL: %w", err)
}
```

`url.Parse` errors embed the full raw URL. A password containing an unescaped `%`, a space, or a bad port is echoed verbatim.

**Trigger:** paste `postgresql://bob:p@ss%word@db/x` into the TUI import dialog (`internal/ui/welcome.go:353-355` → `w.a.fail`) or pass it on the CLI (`main.go:152-155`, printed to stderr): `tuidb: bad URL: parse "postgresql://bob:p@ss%word@db/x": invalid URL escape "%wo"`. The web path already has a `redactURL` (`internal/web/handlers.go:89-117`); the TUI/CLI do not.

**Fix:** reuse the redaction helper in `ParseURL`, or wrap the error with a sanitized URL (scheme/host only).

---

# STORE-04 — A torn history line silently swallows the next appended entry

**Severity: P2**
**Location:** `internal/store/history.go:58-63`, `:90-95`

A crash or partial write can leave a line without a trailing `\n`. The next `Append` writes its JSON directly onto that line, producing one invalid line that `readAll` discards, so the new entry is lost permanently.

**Trigger:** `kill -9`/power loss/ENOSPC during an append, then run one more query. Reproduced: after a truncated trailing line, appending `select AFTER` leaves `List()` returning only the older entries. `TestHistoryTornLine` only asserts the old entry survives.

**Fix:** before appending, seek to EOF and write a leading `\n` if the last byte is not a newline; or repair the file on open.

---

# STORE-05 — History trim/clear from one process loses the other process's appends

**Severity: P2**
**Location:** `internal/store/history.go:58`, `:120-134`, `:143-150`

The mutex is process-local. When one process renames (trim) or unlinks (clear) the JSONL while another holds it open in append mode, the other's writes go to the unlinked inode and are lost.

**Trigger:** TUI running queries while the browser clicks "Clear" (`DELETE /api/history`), or while the size-triggered rewrite fires.

**Fix:** lock the history file across processes, or reopen after detecting a replaced inode; use `O_APPEND` with a per-process lockfile.

---

# STORE-06 — TLS settings from a pasted URL can be silently dropped to plaintext

**Severity: P2 (security fail-open; requires a nonstandard value)**
**Location:** `internal/config/connections.go:264`, `:270-285`

`sslFromQuery` uses case-sensitive truthiness and a strict map for native TLS parameters:

```go
truthy := func(v string) bool { return v == "true" || v == "1" || v == "yes" }
...
return map[string]string{"DISABLED": "disable", "PREFERRED": "prefer", "REQUIRED": "require",
    "VERIFY_CA": "verify-ca", "VERIFY_IDENTITY": "verify-identity"}[strings.ToUpper(v)], []string{"ssl-mode"}
```

An unrecognized value yields `""`, the `if mode != ""` validation is skipped, and the key is marked consumed, so nothing remains in `Options`. For MySQL/MariaDB `SSLMode == ""` means no TLS at all (`mysql.go:161-164`).

**Trigger:** `mysql://u@h/d?ssl-mode=VERIFY-FULL` or `ssl-mode=verify-identity` (hyphen, tuidb's own spelling) → `""` → plaintext. `?tls=TRUE`/`?secure=TRUE` → `truthy` false → explicit `disable`.

**Fix:** accept case-insensitively, normalize `-`/`_`, and return an error for any unknown `ssl-mode`/`tls`/`secure` value instead of `""`/"disable".

---

# STORE-07 — Config paths fall back to the current directory when `HOME` is unset

**Severity: P2 (trusts a CWD-relative connections file, including `password_cmd`)**
**Location:** `internal/config/paths.go:9-24`

```go
h, _ := os.UserHomeDir()
return filepath.Join(h, ".config", "tuidb")   // h == "" -> ".config/tuidb"
```

With `HOME` unset (launchd/systemd unit, cron, container, `env -u HOME tuidb`), the app loads `./.config/tuidb/connections.toml`, chmods it, and executes any `password_cmd` it names. History, state, and saved queries are also written into the CWD.

**Fix:** treat an empty `UserHomeDir` as an error, and require `XDG_*` values to be absolute.

---

# STORE-08 — `validate()` checks only three settings; enum typos silently fall back

**Severity: P2**
**Location:** `internal/config/config.go:126-135`

`editor.indent`, `complete_key`, `result_mode`, `table.filter_default_*`, `filter_state`, `default_sort`, `sidebar.matching`, `new_tab_opens`, and `csv.*` are not validated.

**Trigger:** `indent = "tab"` → tabs silently become spaces (`tab_query.go:62-67`); `complete_key = "ente"` → treated as `enter_or_tab`; `sidebar.matching = "fuzzyy"` → falls through to `contains`. This contradicts `TestUnimplementedSettingsAreRejected` and the package's stated fail-loudly goal.

**Fix:** validate enums in `validate()` and reject unknown values.

---

# STORE-09 — `log_original_values` and the whole `[csv]` block are accepted but never read

**Severity: P2 (documented features silently do nothing)**
**Location:** `internal/config/config.go:36`, `:60-65`; `internal/store/history.go:27`; `internal/ui/grid.go:540`

`LogOriginalValues` has no reader and `HistoryEntry.Original` is never populated by either record function, so the promised UPDATE/DELETE pre-images never exist. `Cfg.CSV` has no readers at all; the CSV writer hardcodes `csv.NewWriter` defaults, so `delimiter = ";"` has no effect.

**Fix:** implement the features or reject the settings, per the package's own unknown-setting policy.

---

# STORE-10 — `Queries.Save` silently overwrites a different query on name collisions

**Severity: P2**
**Location:** `internal/store/queries.go:76-78`, `:81-97`

The on-disk filename is a lossy sanitization, and `Save` never checks for an existing file.

**Trigger:** save "Orders" then "orders" on a case-insensitive filesystem (APFS/NTFS): the second write replaces the first. Names differing only in punctuation (`"a b"` / `"a?b"`) both become `a_b.sql`.

**Fix:** check `os.Stat` before saving and require an explicit overwrite flag; keep a stable id in the header instead of relying on the filename.

---

# STORE-11 — `safemode.Level.Gate` fails open for levels not produced by `Parse`

**Severity: P3 (defense-in-depth; UNSURE reachable today)**
**Location:** `internal/safemode/safemode.go:38-50`

The `switch` has no `default:`, so an unknown/zero `Level` returns `(false, false)` — no confirm, no password. Both current call sites use `Parse` (which fails closed), but the type and `Gate` are exported.

**Fix:** `default: return true, true`.

---

# STORE-12 — Additional issues noted (no separate ID)

**Severity: P3 unless stated**

- `internal/store/history.go:58` — an existing 0644 history file is never re-chmodded (unlike `connections.toml`), and `Append`'s error is discarded by both callers (`ui/workspace.go:255`, `web/sessions.go:404`), so a failed write is completely silent. History holds SQL literals and intended pre-images.
- `internal/store/history.go:38-42` — `unsafeName` maps distinct connection names to one history file (`"Acme Prod/1"` vs `"Acme_Prod_1"`, or `"Prod"`/`"prod"` on a case-insensitive FS), so clearing one connection's history erases the other's.
- `internal/store/state.go:67-79` — `do` mutates `s.d` before the write with no rollback: after a failed save the in-memory state is dirty and a later successful save persists it. `SetRowLimit`'s error is dropped at `ui/tab_query.go:667`.
- `internal/config/connections.go:102-104` — `Validate` range-checks only the SSH port (and accepts `0` while the message says 1-65535); `Connection.Port` is unchecked.
- `internal/config/connections.go:221-248` — saving renames over `connections.toml`, so a dotfiles-style symlink becomes a regular file; a crash before the rename leaves `.<name>-*` temp files.
- `internal/store/queries.go:29-35`, `:93` — `header()` writes the user's name/keywords raw, so a newline in a web-supplied `Name` injects lines into the saved `.sql` and breaks re-parsing of the keyword header.

---

# WEB-14 — Edits are addressed by row position in a replaceable snapshot, so they can land on the wrong row

**Severity: P1 (writes/deletes the wrong record)**
**Location:** `internal/web/changes.go:87-96`, `web/src/DataTab.tsx:58`, `:77`, `:147-152`, `web/src/Workspace.tsx:101`

`/plan` resolves a request's `row` index directly against whatever snapshot `resultId` currently names:

```go
key := func(row int) ([]db.KeyVal, error) {
    if row < 0 || row >= len(c.res.Rows) { ... }
    kv[j] = db.KeyVal{Col: c.res.Columns[ci].Name, Val: c.res.Rows[row][ci]}
```

Nothing binds a staged edit to the snapshot it was made against. The client keeps `edits`/`deletes` keyed by `'r'+index` and never clears them when `res` changes; the rows query key does not include `resultId`, and `Workspace.tsx:101` invalidates all queries with no key after any sidebar plan (drop/truncate/rename/clone/create DB).

**Trigger:**
1. Edit the cell at index 5 (or mark row 5 for deletion).
2. Apply any sidebar plan; the mounted table tab refetches rows and receives a new `resultId`.
3. Because the browse SELECT has no `ORDER BY`, the same rows may come back shifted (and a concurrent TUI writer shifts them too).
4. "Send changes" sends the new `resultId` with the old index; `changes.go` builds the key from the row now at index 5 and updates/deletes that record.

**Fix:** send the PK values (or a snapshot hash) with each staged edit and verify them server-side; invalidate staged edits when the result is replaced, or key edits by PK rather than position. `DataTab.tsx:95` already knows the PK values for display.

---

# WEB-15 — A change that matched zero rows is reported as success

**Severity: P1 (silent lost update)**
**Location:** `internal/web/handlers.go:518-530`, `internal/db/postgres/exec.go:331`, `internal/db/types.go:189-194`

`Apply` discards the `CommandTag` for every statement and `/apply` returns `{"ok": true}` unconditionally. `db.Statement` has no rows-affected field, so the information never reaches the API.

**Trigger:** two writers on one table (two browser tabs are independent sessions, or the TUI is open). Tab A loads rows, edits `name` for `id=7`; the other session changes that row's PK or deletes it. Tab A sends `UPDATE "t" SET "name"=$1 WHERE "id"=$2` → 0 rows → toast "Changes sent" and the staged edit is discarded. The same silent no-op occurs when any key column does not compare equal (NaN float, collation, rounded display value).

**Fix:** return the affected count from `Apply` (change the interface or add an optional interface), and surface "0 rows affected" as an error/confirmation instead of success.

---

# WEB-16 — A successful apply leaves the source result plannable, so retries duplicate inserts

**Severity: P2**
**Location:** `internal/web/handlers.go:497-529`, `internal/web/sessions.go:141-152`

`apply` deletes only the plan and clears `colCache`; `ss.results[resultId]` with its pre-apply originals stays valid, and `/plan` is not blocked while `applying` is true.

**Trigger:** default safe mode is `silent`, so "Send changes" applies immediately. Double-click, or retry after a lost response: `makePlan()` re-plans from the un-updated snapshot and the insert is inserted twice. (The TUI avoids this by re-running the query after commit, `internal/ui/resultedit.go:190-197`.)

**Fix:** mark the cached result consumed/invalid after a successful apply (or drop it), and make plan creation serialize with apply.

---

# WEB-17 — The hidden-PK check also rejects insert-only plans

**Severity: P2**
**Location:** `internal/web/changes.go:71-81` vs `:141-147`, `web/src/DataTab.tsx:240-251`

`buildChanges` errors on a hidden PK before looking at `req.Inserts`, even though an insert needs no key.

**Trigger:** untick `id` in the Columns menu (common). `/rows` returns the projection with `Editable: true`; "Add row" is shown; filling it and sending changes returns `400 primary key column "id" is hidden; show it before editing`. Unhiding goes through `change()` → `clearPending()`, so the typed row is lost.

**Fix:** only require the full PK when a delete/update is present.

---

# WEB-18 — Tables without a PK (and views) are editable with an all-columns key, so one delete can remove many rows

**Severity: P2 (data-loss hazard; deliberate divergence from the TUI)**
**Location:** `internal/web/handlers.go:311-315`, `internal/web/changes.go:82-86`

`/rows` grants `Editable` for any successful column lookup — no PK check, no `Kind` check — and `buildChanges` falls back to keying on every result column when the table has no PK. Duplicate rows all match the same `DELETE`.

**Trigger:** a log/join table with no PK and two identical rows; delete one in the grid → `DELETE FROM t WHERE a=$1 AND b=$2 …` removes both, while the tray showed one removal. Hiding a non-key column narrows the `WHERE` and widens the blast radius. Views/matviews opened in the same tab (`Workspace.tsx:35`) take the same path.

The TUI deliberately refuses this: `internal/ui/resultedit.go:60` only unlocks `db.KindTable`, and `:105` requires all PK columns.

**Fix:** require a PK (or a unique index) for edits, and block non-table kinds, matching `resultedit.go`; or at minimum warn when the key cannot uniquely identify a row.

---

# WEB-19 — An update that omits `"row"` silently updates row 0

**Severity: P2**
**Location:** `internal/web/changes.go:51-59`

`Row int` has no presence marker, so `{"updates":[{"set":{...}}]}` decodes as row 0 and is accepted; the first row of the page is overwritten. Every other required input is validated.

**Fix:** use `*int` and reject a missing value.

---

# WEB-20 — `{"$expr": …}` inlines arbitrary SQL into the planned statement

**Severity: P2 (breaks the "only server-planned statements run" invariant; not a privilege boundary)**
**Location:** `internal/web/changes.go:31-34`, `internal/db/change.go:34-52`

`$expr` accepts an arbitrary string and `db/change.go` writes it raw (`b.raw(string(x))`). The TUI only ever offers fixed `NOW()`/`now()` (`internal/ui/tab_table.go:805-807`); the API accepts free text.

**Trigger:** `{"inserts":[{"name":{"$expr":"1); DROP TABLE customers; --"}}]}` produces `INSERT INTO "public"."t" ("name") VALUES (1); DROP TABLE customers; --)` with zero args. With zero arguments pgx selects the simple protocol (`pgx v5.11.0 conn.go:525-526`), which accepts multiple statements.

The same token could call `/exec`, and the preview shows the injected text, so this is a hardening issue rather than an escalation. The comment at `handlers.go:486-487` ("Only statements the server planned can run through here") is no longer accurate.

**Fix:** restrict `$expr` to a validated allow-list of expressions (as the TUI does), or drop it from the HTTP API.

---

# WEB-21 — Duplicate updates for one row are not rejected

**Severity: P3**
**Location:** `internal/web/changes.go:112-123`

`seen` tracks only deletes, so two updates for the same row are both emitted and run in order. If the first changes the PK, the second matches 0 rows (reported as success per WEB-15) or updates another row that still has the old key.

**Fix:** apply the same `seen` treatment to updates.

---

# WEB-22 — 50-entry result cache can evict the snapshot behind staged edits

**Severity: P3**
**Location:** `internal/web/sessions.go:16`, `:140-152`, `internal/web/handlers.go:414-426`

Every `/rows` and `/exec` result is inserted into a 50-entry FIFO; the client never calls `DELETE /results/{rid}`. A long session silently evicts old snapshots.

**Trigger:** keep a table tab with staged edits open while browsing/querying; sending changes fails with `unknown or evicted result …; reload the table`, and reload clears the staged edits.

**Fix:** pin results referenced by plans/staged edits, or have the client drop results it no longer needs.

---

# WEB-23 — `partial` can be false when earlier statements were committed

**Severity: P3 (latent; UNSURE reachability)**
**Location:** `internal/web/handlers.go:518-523`, `internal/db/postgres/exec.go:302-319`

`partial` is `!Transactions || hasDDL(stmts)`, but the Postgres driver drops the transaction for the whole batch as soon as any statement is `noTx`, so a later failure can leave earlier statements applied while `partial` is false. `hasDDL` is a first-word heuristic and misses a DDL statement with a leading comment. No current endpoint appears to produce such a mixed plan.

**Fix:** have the driver report whether it ran transactionally, and derive `partial` from the actual execution.

---

# FE-13 — Double-clicking "Send changes" duplicates inserts

**Severity: P1 (duplicated rows)**
**Location:** `web/src/DataTab.tsx:158-162`, `:205`, `internal/web/handlers.go:506-513`

`commit` has no in-flight/disabled guard:

```tsx
const commit = async () => {
  const p = await makePlan(); if (!p) return
  if (p.gate.confirm || p.gate.password) { setPlan(p); return }
  try { await api('POST', `${base}/apply`, { planId: p.planId, confirmed: true }); applied() } ...
}
...
<button className="primary" onClick={commit}>Send changes</button>
```

Both clicks call `makePlan()` before `clearPending()` runs, so the server stores two plans from the same `body()`. The server only rejects _overlapping_ applies (`ss.applying`); if the first finishes before the second starts, both run and inserts are duplicated (updates/deletes are idempotent).

**Fix:** a `busy` flag that disables the button and short-circuits `commit`, plus server-side idempotency (see WEB-16).

---

# FE-14 — Saving while the URL is still being parsed persists a stale connection

**Severity: P2 (wrong host/credentials saved)**
**Location:** `web/src/Connections.tsx:89-99`, `:114-115`, `:144`

The URL input parses on `onBlur` into a state update, while Save reads the pre-parse state. Blur (async parse starts) and click (Save runs with old `c`) are not ordered.

**Trigger:** new connection → type a name → paste `postgresql://u:secret@prod:5432/db` → click Save without leaving the field. The server's `Validate` accepts host `127.0.0.1`/empty database, so the connection is saved pointing at localhost; the dialog closes and the pasted URL is lost.

**Fix:** parse synchronously in the change handler, or disable Save while a parse is pending / when the URL text differs from the parsed value.

---

# FE-15 — Connection save errors render behind the modal

**Severity: P2 (failed save looks like nothing happened)**
**Location:** `web/src/Connections.tsx:22`, `:48`, `:69`

`onError` writes to page-level `msg`, but the edit form is a fixed overlay (z-index 50). A rejected save (duplicate name, invalid SSL mode) leaves the dialog open with no visible error.

**Fix:** pass the error into `ConnForm` and render it inside the dialog.

---

# FE-16 — Scrolling the edited cell out of the virtualized window loses the typed value

**Severity: P2**
**Location:** `web/src/Grid.tsx:39`, `:74-78`, `:91-104`

`CellEditor` commits only from `onBlur`/Enter/Escape. Virtualization unmounts the row when it leaves the window (overscan 12); no usable blur is delivered, and there is no commit-on-unmount effect.

**Trigger:** double-click a cell, type a value, wheel-scroll until the row is unmounted. Neither `onEdit` nor cancellation runs; the edit is silently dropped.

**Fix:** commit in a `useEffect` cleanup, or keep the edited row mounted while editing.

---

# FE-17 — Opening the filter bar seeds an enabled blank filter

**Severity: P2 (full-table scan, persisted)**
**Location:** `web/src/FilterBar.tsx:9`, `:12`, `:44`, `internal/db/filter.go:99-116`

`blank()` sets `Enabled: true`, and `apply()` keeps it. Clicking "Apply filters" with nothing typed installs `Any column Contains ""` for every column, producing `(c1 ILIKE '%%' OR c2 ILIKE '%%' …)` — a scan of every column — and persists the useless filter to `/state/table`, so every later visit is slow. The footer also switches to an exact count.

**Fix:** drop blank enabled filters in `apply()` (or start the seeded row disabled).

---

# FE-18 — Pins/recents from another schema disappear and cannot be unpinned

**Severity: P2**
**Location:** `web/src/Sidebar.tsx:22`, `:26`, `:29-31`

`byKey` searches only the currently selected schema's object list, but `keyOf`/`recentKey` include the schema. Pinning `sales.orders` then switching to `public` drops the entry from the list (`.filter(Boolean)`), so there is no star row to unpin it.

**Fix:** resolve pinned/recents keys against a cross-schema lookup (or a server endpoint), not the current list.

---

# FE-19 — Sidebar schema is not re-synced after switching databases

**Severity: P2**
**Location:** `web/src/Sidebar.tsx:17`, `:67-69`, `web/src/Workspace.tsx:52-55`

`schema` is initialized once from `info.schema` and never updated when `info.database` changes. After switching databases, the object query still uses the old schema (empty list, no error) and the select shows no valid option, so the database looks empty.

**Fix:** reset `schema` when the database changes (effect keyed on `info.database`, or `key={info.database}`).

---

# FE-20 — Editing a cell back to its original value still stages an update

**Severity: P3**
**Location:** `web/src/DataTab.tsx:117-119`, `web/src/Grid.tsx:94`

`onEdit` never compares the new value with `res.cells[row][col]`, only with the editor's `initial` (the previously staged value). Edit `abc` → `abd` → `abc`: the update stays staged, the tray shows `abc → abc`, the "Discard pending changes?" prompt fires, and commit sends a no-op `UPDATE` (firing triggers/`updated_at`).

**Fix:** compare against the server original and delete the edit when equal.

---

# FE-21 — Overloaded functions share a React key and pin identity

**Severity: P3**
**Location:** `web/src/Sidebar.tsx:48`, `:29`, `:32`

Keys are `Schema + '.' + Name + Kind`; Postgres returns one row per `pg_proc` overload (`internal/db/postgres/meta.go:37-40`), so two overloads produce duplicate React keys, and starring one stars/unpins both.

**Fix:** include the argument list (`Detail`) in the key and pin identity.

---

# FE-22 — Pagination boundaries are off; offset is not clamped

**Severity: P3**
**Location:** `web/src/DataTab.tsx:225-230`, `:72`, `:156`, `:189`

`Next` is disabled only when `res.cells.length < limit`, so it stays enabled on an exactly-full last page; clicking it shows "This table has no rows yet. Add one to start." `offset` is never clamped or reset on Reload/after apply, so deleting the last page's rows yields the same false empty state. The rows-per-page input is uncontrolled (`defaultValue`), so cancelling the discard confirmation leaves a limit displayed that was never applied.

**Fix:** use the count query to disable Next, clamp offset after apply/reload, and make the limit input controlled.

---

# FE-23 — Lower-confidence frontend notes

**Severity: P3**

- `web/src/Grid.tsx:20` vs `web/src/styles.css:163` — `ROW_H = 26` but `.grid .c` is 28px, so virtual rows overlap by 2px and total scroll height is undercounted.
- `web/src/DataTab.tsx:136-137` — `duplicate()` round-trips special values through `show()`, so a staged `{$:'default'}` or `{$expr:'now()'}` becomes the literal string `"DEFAULT"`/`"now()"` in the duplicated row.
- `web/src/FilterBar.tsx:9` — the draft initializer drops a saved `Or: true` when there are no filters.

---

# WEB-24 — `PUT /api/connections` with an empty body deletes every saved connection

**Severity: P1 (irrecoverable loss of all connections and stored passwords)**
**Location:** `internal/web/handlers.go:42-64`, `internal/web/server.go:197-204`

`decode()` returns `nil` for a zero-length body without touching the target:

```go
func decode(r *http.Request, v any) error {
    body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
    ...
    if len(body) == 0 { return nil }
```

So `conns` stays `nil`, validation passes trivially, and `config.SaveConnections(ConnPath, nil)` rewrites `connections.toml` with no `[[connection]]` entries; `s.o.Conns` becomes nil.

**Trigger:** any `PUT /api/connections` with no body (client bug, proxy stripping the body, a future save path that forgets the payload) or a literal `null`. Response is `200 {"ok":true}`; every saved connection including its stored password is gone.

**Fix:** reject a missing/`null` body for full-replace endpoints (decode into `*[]config.Connection` and require non-nil; allow `[]` explicitly).

---

# WEB-25 — DDL through `/exec` never invalidates `colCache`

**Severity: P2 (broken table tabs until reconnect)**
**Location:** `internal/web/handlers.go:343-406`, `:306`, `:210-211`; contrast `:527-529`

`apply` clears `colCache` after a GUI commit, but `exec` does not. `columns()` is the only producer and is keyed purely by `db.ObjRef`.

**Trigger:** run `ALTER TABLE t DROP COLUMN old` in the query tab, then open `t`. `/object/columns` still returns `old`; if the table has any hidden column the client sends an explicit `columns` list, and every `/rows` builds `SELECT … "old" …` and fails with "column does not exist". The Columns menu and structure view also list the dropped column. Only a database switch or GUI apply clears it.

**Fix:** clear `colCache` (and possibly `results`) after any successful statement that `db.IsReadOnly` classifies as non-read-only, or after DDL detection.

---

# WEB-26 — `columns()` caches results after a cache reset with no generation guard

**Severity: P2 (wrong metadata after a database switch)**
**Location:** `internal/web/sessions.go:164-193`, `:195-201`, `internal/web/handlers.go:178`

A goroutine that missed the cache can finish its `Columns`/`ForeignKeys` round-trips after `useDatabase` replaced `ss.colCache`, then store columns fetched from the previous database under the same `ObjRef` in the new map. `buildChanges` uses those columns to build UPDATE/DELETE keys, so the wrong columns can be used for writes.

**Trigger:** open a table (columns fetch in flight) and switch database. Narrow timing, but the guard is structurally missing; the TUI solves this with a session generation counter (`internal/ui/workspace.go:157-161`).

**Fix:** capture a generation on cache miss and drop the result if the generation changed (mirror the TUI).

---

# WEB-27 — `/exec` caches every row-returning statement, evicting editable results

**Severity: P2**
**Location:** `internal/web/handlers.go:398-401`, `internal/web/sessions.go:16`, `:141-152`, `internal/web/changes.go:64-66`

Exec results are stored with a zero `ref` and can never be edited (`buildChanges` rejects them), but they still consume the 50-slot FIFO and evict table results that have staged edits.

**Trigger:** stage edits in a table tab, run a 50+ statement SELECT script in the query tab (one `put` per statement), then "Send changes" → `unknown or evicted result …; reload the table`; reloading discards the staged edits.

**Fix:** don't cache exec results (the results tab never uses `resultId`), or evict only non-pinned/table entries.

---

# WEB-28 — Plan lookup and the `applying` guard are not atomic

**Severity: P2 (duplicate apply; UNSURE reachability — not reproduced)**
**Location:** `internal/web/handlers.go:497-514`

The handler reads and unlocks the plan (`:497-499`), checks the gate (`:503`), then re-locks to check `applying` and delete the plan (`:506-513`). Two concurrent applies with the same `planId` can both read it; the loser normally sees `applying == true`, but if descheduled across the peer's whole `Apply`, it re-locks after `applying` was reset and applies the same statements again.

**Fix:** delete the plan and set `applying` in the same critical section as the lookup (before the gate check), and restore it on gate failure; or add a per-plan consumed flag.

---

# WEB-29 — Safe Mode 1/2 asks for a password that is not verified on a passwordless connection

**Severity: P2 (misleading security UI)**
**Location:** `internal/web/sessions.go:211-236`

`gate()` returns `Password:true` whenever the level requires it, regardless of `ss.o.Password`. The UI then disables Send until text is entered (`web/src/Dialogs.tsx:19-21,51`), but `check` skips verification when `ss.o.Password == ""` and accepts any `confirmed:true`.

**Trigger:** Safe Mode 1/2 on a trust/peer-auth/socket connection (or a `password_cmd` that prints nothing). The dialog says "enter the database password"; typing `x` applies the change.

**Fix:** report `Password:false` when there is no stored password (like the TUI's effective gate), or require a configured password for those levels.

---

# WEB-30 — `PUT /api/state/table` with an empty body overwrites saved table state

**Severity: P3**
**Location:** `internal/web/handlers_store.go:83-92`, `internal/store/state.go:102-109`

Same empty-body decode pattern as WEB-24: a request with no body stores the zero `TableState`, silently dropping widths, hidden columns, filters, and sort.

**Fix:** require a body for state writes.

---

# WEB-31 — History endpoint reports the wrong cause and treats `limit=0` as "all"

**Severity: P3**
**Location:** `internal/web/handlers_store.go:9-19`, `internal/store/history.go:99-116`

`if s.o.Hist == nil || q.Get("conn") == ""` collapses a nil store into `400 "conn is required"` even when the parameter was supplied. `atoi(q.Get("limit"), 200)` passes `0`/negative through, and `History.List` documents `limit<=0` as "all", so `?limit=0` returns the whole file instead of zero rows.

**Fix:** separate the missing-store error, and clamp/validate `limit`.

---

# WEB-32 — `/rows` swallows a metadata failure and returns 200 with editing disabled

**Severity: P3**
**Location:** `internal/web/handlers.go:306-315`

`cols, cerr := ss.columns(...)`: `cerr` only gates `out.Editable`; the response is still `200` with all `pk` flags false and no error field. When `Columns()` fails but `Rows()` succeeds (catalog permission denied, odd object), the user gets a normal-looking grid that cannot be edited and no explanation.

**Fix:** surface the metadata error in the response or fail the request.

---

# DB-01 — Mongo regex literals are lexed as comments, merging a write into a "read-only" statement (Safe-Mode bypass)

**Severity: P1 (security: write executes without confirmation)**
**Location:** `internal/db/split.go:45-48`, `internal/db/classify.go:33-36`, executed at `internal/db/mongo/shell.go:111` (called from `:543`); gate at `internal/ui/workspace.go:263-272`

The Mongo lexer has no regex-literal state, so `//` (or `/*`) inside a JS regex is treated as a comment and the rest of the line — including a following statement — is swallowed. The merged text is then classified read-only, but `goja`'s `vm.RunString` executes every statement in it.

**Verified:**
```
db.links.find({url: /^https?:\/\//}); db.links.deleteMany({})
→ stmts=1, IsReadOnly("mongo", …)=true
```

The gate therefore asks nothing and `deleteMany` runs. The same gap corrupts normal queries: `db.users.find({name: /a;b/})` splits into `db.users.find({name: /a` and `b/})`.

**Fix:** add regex-literal state to the Mongo scanner (distinguish `/` from division by the previous token), and make the classifier reject an unterminated/merged statement instead of defaulting to read-only. Also consider gating Mongo writes on the parsed AST rather than text.

---

# DB-02 — `IN`/`NOT IN` splits values on every comma, including commas inside quotes

**Severity: P2 (silently wrong results)**
**Location:** `internal/db/filter.go:55-65`, `:161-167`

`splitList` splits on `,` before considering quotes, and the quote-stripping heuristic leaves doubled quotes intact.

**Trigger:** filter `a IN` with value `'x,y', 2` produces `"a" IN ($1, $2, $3)` with args `['x`, `y'`, `2]`. `IN ('it''s')` passes the literal `it''s`. The result set is empty or wrong with no error.

**Fix:** parse the list with the dialect's quoting rules (a small tokenizer), and support doubled quotes.

---

# DB-03 — MySQL backtick identifiers are lexed with backslash escapes, merging statements

**Severity: P2**
**Location:** `internal/db/split.go:70-75`, `:134-151`; same in `internal/db/classify.go:75-83`

A backslash is not an escape inside MySQL backticks (only doubling is), but one `backslash` flag is used for `'`, `"`, and `` ` ``. An identifier ending in `\` makes the scanner swallow the closing backtick and run past real `;` boundaries.

**Verified:** `SELECT `x\` FROM t; DELETE FROM t;` → one Stmt, `IsReadOnly("mysql", …) = true`. With `multiStatements` off the server rejects the packet, so the practical impact is a confusing error plus the same classification blind spot as DB-01.

**Fix:** disable backslash escaping while inside a backtick identifier for MySQL/MariaDB (ClickHouse genuinely escapes and is handled by `chQuote`).

---

# DB-04 — The generic classifier answers "read-only" for text containing several statements

**Severity: P2 (amplifier for DB-01/DB-03)**
**Location:** `internal/db/classify.go:252-277`

`IsReadOnly` only inspects the first token, so `SELECT 1; DELETE FROM t` is read-only. The new per-dialect classifiers guard this (`classify_partiql.go:20-24` rejects an interior `;`; `classify_cql.go:17-26` is an allow-list), but the generic one is the outlier.

**Trigger:** verified `IsReadOnly("postgres", "SELECT 1; DELETE FROM t") == true`. Today callers split first, so it matters only when a lexer merges statements, but it turns lexer divergence into a silent gate bypass instead of an error.

**Fix:** reject an interior top-level `;` in the SELECT/VALUES/TABLE arm, mirroring `classify_partiql.go`.

---

# DB-05 — "Filter SQL" inlines parameters in reverse for every `?`-style dialect

**Severity: P2 (displayed SQL does not match the grid)**
**Location:** `internal/ui/filterbar.go:211-212`, `internal/db/dialect.go:39-40`, `:50`, `:6`

The loop walks args from last to first and replaces the *first* remaining placeholder each time. That works only when placeholders are distinct (`$1`, `$2`); for MySQL/ClickHouse/CQL/PartiQL/SQLite every placeholder is `?`, so values come out mirrored.

**Trigger:** MySQL filters `a = 1`, `b = 2` → dialog shows `` `a` = '2' AND `b` = '1' ``. Copying that SQL yields a different result set from the grid.

**Fix:** substitute placeholders left-to-right with args in order, or render the args list explicitly.

---

# DB-06 — MySQL `DELIMITER` is only recognized at column 0

**Severity: P2**
**Location:** `internal/db/split.go:96-105` (`i == 0 || text[i-1] == '\n'`)

One leading space or tab defeats the delimiter switch, and the `DELIMITER` line is left in the statement buffer.

**Verified:** `  DELIMITER $$\nCREATE PROCEDURE …; END$$\nDELIMITER ;\nSELECT 2;` splits into `["DELIMITER $$\nCREATE PROCEDURE p() BEGIN SELECT 1", "END$$", "SELECT 2"]` — the routine body is cut in half and `DELIMITER` is sent to the server.

**Fix:** allow leading whitespace before `DELIMITER`.

---

# DB-07 — `Split` and `scan` disagree about MySQL executable comments

**Severity: P3**
**Location:** `internal/db/split.go:55-69` vs `internal/db/classify.go:49-57`

The splitter treats `/*! … */` as a plain comment (a `;` inside cannot split), while the classifier treats the content as code (`execComment`). The file header states the two must agree.

**Trigger:** `SELECT 1 /*! ; DELETE FROM t */` → one statement, classified read-only; with `multiStatements=true` it executes two statements.

**Fix:** lex executable comments identically in both passes.

---

# DB-08 — Empty insert becomes `INSERT INTO t () VALUES ()` for non-Postgres dialects

**Severity: P2 (ClickHouse rejects it on every empty add-row)**
**Location:** `internal/db/change.go:88-92`

Only Postgres gets `DEFAULT VALUES`; ClickHouse and the stubbed SQLite/DuckDB dialects receive `()`. Confirmed against ClickHouse 24.8: `INSERT INTO \`t\` () VALUES ()` → `Code: 62 … Syntax error`.

**Trigger:** add a row in the TUI (`i`) or web `inserts:[{}]` and commit without typing anything. The grid fills new rows with `db.Default`, and `change.go:83-85` drops those cells, so the same text is produced for an all-default row.

**Fix:** emit `INSERT INTO t VALUES (DEFAULT)`/explicit columns for engines that need it.

---

# DB-09 — `FormatValue(time.Time)` truncates to microseconds and drops the zone; dirty-tracking compares that text

**Severity: P3 (display/precision only)**
**Location:** `internal/db/format.go:27-28`, `internal/ui/grid.go:279-284`

The layout `2006-01-02 15:04:05.999999` hides nanoseconds and any offset, and `sameValue` compares formatted strings, so values differing below a microsecond are treated as unchanged.

**Trigger:** a Postgres `timestamp(9)` value `…12.123456789` displays `…12.123456`; editing that cell and saving writes back the truncated value.

**Fix:** preserve precision/offset in the display, or compare typed values for dirty tracking.

---

# DB-10 — `splitBetween` upper-cases the value, then indexes the original string

**Severity: P3**
**Location:** `internal/db/filter.go:67-73`

`strings.Index(strings.ToUpper(v), " AND ")` can return an offset that does not exist in `v` when uppercasing changes byte length (`ı`→`I`), slicing mid-rune and passing invalid UTF-8.

**Verified:** `BETWEEN 'x ı AND 5'` yields the low bound `x \xc4`. It also cannot handle ` AND ` inside quoted bounds.

**Fix:** scan for the keyword with a quote-aware tokenizer instead of uppercasing and indexing.

---

# DB-11 — Registry hygiene: silent duplicate overwrite; dialect maps are unlocked

**Severity: P3 (UNSURE whether registration happens after `init`)**
**Location:** `internal/db/registry.go:15-19`, `internal/db/dialect.go:44-46`, `internal/db/classify.go:303-306`

`Register` silently replaces a driver with the same name, and `extraDialects`/`readOnlyExtra` are plain maps without the `sync.RWMutex` used by the sibling driver map. Any runtime registration would race under `-race`.

**Fix:** reject duplicate registrations and guard all registries with the same mutex.

---

# DB-12 — CQL raw filters are emitted without parentheses, so `OR` leaks across filters

**Severity: P3 (UNSURE whether parentheses are legal in CQL `WHERE`)**
**Location:** `internal/db/filter.go:86-92`

The `d.Name != "cql"` exception means a raw filter's internal `OR` binds looser than the `AND` joining filters.

**Trigger:** CQL filters `raw "a = 1 OR b = 2"` + `"c = 3"` with `Or=false` → `WHERE a = 1 OR b = 2 AND c = 3`, matching rows the user excluded. Postgres produces `(a = 1 OR b = 2) AND c = 3`.

**Fix:** parenthesize raw filters where the dialect allows it.

---

# DDB-01 — DynamoDB `IN`/`BETWEEN` filters are not quote-aware

**Severity: P2 (silently wrong results; same class as DB-02, separate implementation)**
**Location:** `internal/db/dynamodb/rows.go:72-90`, used at `:136`, `:149`

**Trigger:** a text column holding `x,y` with filter `txt IN 'x,y'` → split into `'x` and `y'`, returning zero rows with no error (verified against DynamoDB Local: `txt IN "'x,y'"` → `[]` while `Contains ","` matches 4). `BETWEEN 'A AND B' AND 'Z'` splits inside the quoted bound.

**Fix:** share the quote-aware list parser from DB-02.

---

# DDB-02 — Non-scalar column types have their filter value re-typed by JSON guessing

**Severity: P2 (filters silently match nothing)**
**Location:** `internal/db/dynamodb/rows.go:105-116`, `plan.go:53`, `exec.go:199`

Any column whose type is not `S/N/B/BOOL` (sets, lists, maps, and *unknown* columns) has `typ` reset to `""`, then `coerceText` guesses a scalar, producing operators like `begins_with(SS, S)`.

**Trigger (verified):** `tags IN "p, q"` (an `SS`) → 0 rows; `tags Has prefix p` → 0 rows. The unknown-column path is worse: `Apply` resets metadata to a 100-item sample, so a text value like `123`/`true` becomes `N`/`BOOL` and silently mismatches.

**Fix:** keep the declared type and emit the correct set/list comparison, or reject unsupported operators with an error instead of guessing.

---

# DDB-03 — `$binary` with a non-string payload silently stores empty bytes

**Severity: P2**
**Location:** `internal/db/dynamodb/value.go:196`

```go
s, _ := e.(string)          // non-string -> ""
DecodeString("")            // succeeds
AttributeValueMemberB{Value: []byte{}}  // empty blob
```

**Trigger (verified):** `{"$binary": 123}` (or nested in a map) stores a zero-length binary with `err=nil`. The existing test only covers an invalid base64 string.

**Fix:** type-assert with `ok` and return an error.

---

# DDB-04 — Any credential-chain failure is silently replaced by `local`/`local` when an endpoint is set

**Severity: P2 (misconfiguration masked; VPC-endpoint users see signature errors)**
**Location:** `internal/db/dynamodb/dynamodb.go:225-230`

When `cfg.Credentials.Retrieve` fails and `endpoint != ""`, the driver swaps in static `local`/`local`. The actionable "no AWS credentials: …" error at line 227 becomes unreachable for the documented VPC-endpoint use case (package comment, lines 11-12).

**Trigger:** `?endpoint=https://vpce-xxx.dynamodb…` with expired SSO/instance-role credentials → later `UnrecognizedClientException`/`InvalidSignatureException` instead of a credentials error.

**Fix:** only use the local fallback for `localhost`/`127.0.0.1` endpoints, and propagate the credential error otherwise.

---

# DDB-05 — Plan-time I/O ignores the request context

**Severity: P3**
**Location:** `internal/db/dynamodb/plan.go:53-55`, `:572-574`

`planMeta` and `tableExists` create `context.WithTimeout(context.Background(), 15*time.Second)`, and `Plan`/`PlanTable` take no context. Cancelling/closing a grid still blocks up to 15 s on `DescribeTable`/`Scan`.

**Fix:** thread a context through planning.

---

# DDB-06 — Raw filters reject signed scientific notation

**Severity: P3**
**Location:** `internal/db/dynamodb/rows.go:224-233`

The number scanner accepts `0-9 . e E` but not `+`/`-`, so `num = 1e-2` reads `1e` and fails ("`1e` is not a number"), even though `numRe` accepts it.

**Fix:** accept a sign in the exponent.

---

# DDB-07 — Raw filters are always wrapped, so already-parenthesized input is rejected

**Severity: P3**
**Location:** `internal/db/dynamodb/rows.go:267`

`(num > 1)` becomes `((#n0 > :v0))`; DynamoDB returns `ValidationException: … redundant parentheses` (verified live).

**Fix:** don't wrap when the expression is already a single parenthesized group.

---

# DDB-08 — `Columns` bypasses the metadata cache and resets learned types

**Severity: P3**
**Location:** `internal/db/dynamodb/dynamodb.go:551-553`, `:454-468`

`Columns` always calls `loadMeta` (no cache lookup), and `loadMeta` overwrites `s.metas[table]` with a fresh 100-item sample; a concurrent `Rows` goroutine keeps mutating the orphaned pointer. No data race (all `learn` calls hold `s.mu`), but type knowledge is repeatedly lost.

**Fix:** look up the cache in `Columns`, and merge rather than replace on `loadMeta`.

---

# DDB-09 — PartiQL reader always evaluates at least 100 items per page

**Severity: P3 (extra read capacity)**
**Location:** `internal/db/dynamodb/exec.go:56-64`

`in.Limit = max(limit+1-len(items), 100)` together with `len(items) > limit` means a `LIMIT 1` query evaluates 100 items, and exact multiples cost an extra round trip.

**Fix:** use `min(limit+1, 100)` (or the remaining count) and accept DynamoDB's minimum page semantics explicitly.

---

# DDB-10 — The `DescribeTable` used to order PartiQL result columns is swallowed

**Severity: P3**
**Location:** `internal/db/dynamodb/exec.go:83`

`if d, err := s.describe(ctx, table); err == nil { m = metaOf(d) }` — on throttling, a missing table, or a cancelled context the result is still returned successfully with alphabetical columns, so a read cancelled after its last page reports success.

**Fix:** at least propagate `context.Canceled`; otherwise decorate the result with a warning.

---

# DDB-11 — User maps whose only key is `$binary`/`$ss`/`$ns`/`$bs` cannot round-trip

**Severity: P3**
**Location:** `internal/db/dynamodb/value.go:124-129`, `:191-209`

`toJSON` emits the same wrapper shape that `fromJSON` interprets as a set/binary, so a real attribute `{"$ss": "x"}` is ambiguous: editing it as `M` fails with `got SS`, and with an unknown type it silently becomes an `SS`.

**Fix:** escape user maps that collide with wrapper keys, or use a tagged envelope.

---

# DDB-12 — An index wait failure can be reported as success

**Severity: P3**
**Location:** `internal/db/dynamodb/exec.go:339-344`, `:253`

When `more == false`, any `waitActive` error other than cancellation becomes `"… (still being built in the background)"`, including permission/throttling errors; the message says "table %s is still %s" even when the table is `ACTIVE` and a GSI is what is still `CREATING`.

**Fix:** only downgrade genuine timeout errors and name the index in the message.

---

# UI-02 — Json view panics when a reload returns zero documents

**Severity: P1 (crash)**
**Location:** `internal/ui/doc_views.go:358`

```go
if t.jsonCache.docs == nil || &t.jsonCache.docs[0] != &t.docs[0] || len(t.jsonCache.docs) != len(t.docs) {
```

Once the cache has documents, a reload that yields none leaves `t.docs == nil` while `t.jsonCache.docs != nil`. The first condition is false, so `&t.docs[0]` is evaluated on a nil slice and panics. The `len(lines)==0` "no documents" guard at `:367` runs too late.

**Trigger:** MongoDB/Redis/DynamoDB collection, press prefix `J`, then any reload that returns no rows (filter to nothing, delete the last row, `alt+r`). Independent reproduction:

```
panic: runtime error: index out of range [0] with length 0
  ui.(*tableTab).jsonView(...) internal/ui/doc_views.go:358
```

**Fix:** compare lengths/slices before indexing, e.g. `len(a) != len(b) || (len(a) > 0 && &a[0] != &b[0])`, and return the empty state before dereferencing.

---

# UI-03 — Reload/reconnect during a running query drops its completion and wedges the tab in "running"

**Severity: P1 (tab permanently unusable until cancel)**
**Location:** `internal/ui/workspace.go:226-228`, `:598-600`, `internal/ui/app.go:285-286`, `internal/ui/tab_query.go:289-315`

`Workspace.handle` discards any message whose `gen` is stale *before* the tab can clear its state:

```go
if _, apply := m.msg.(applyDoneMsg); !apply && m.gen != w.gen {
    return nil
}
```

`queryTab.running`/`queue` are cleared only by `queryDoneMsg` (`tab_query.go:299-306`), `Cancel`, or `Close`.

**Trigger:** run a long query, then prefix `r` (reload) or `R` (reconnect). The in-flight `queryDoneMsg` is dropped; the tab keeps `running=true` forever: the spinner never stops, `busy()` stays true (prefix `k` / open-database refused), and every further run answers "a query is already running; cancel it first". The only escape is prefix `.`/cancel. A non-current table tab can likewise stay `loading=true` because `reload` only reloads the focused tab.

**Fix:** route stale messages through a cancellation path that clears the originating tab's state (or have the tab reset `running` on generation change), rather than silently dropping them.

---

# UI-04 — Paste into an editable query-result cell is silently swallowed

**Severity: P2**
**Location:** `internal/ui/tab_query.go:489-497`; dead handler at `internal/ui/resultedit.go:126-135`

`resultsInput` returns immediately for anything that is not a `tea.KeyPressMsg`, so the `tea.PasteMsg` branch in `resultEditKeys` is unreachable and `ctrl+v` in the Results context never issues `tea.ReadClipboard`.

**Trigger:** edit a `SELECT *` result cell and paste — nothing happens (verified for both `tea.PasteMsg` and `tea.ClipboardMsg`). The table grid handles paste correctly (`tab_table.go:508-510`).

**Fix:** forward paste/clipboard messages to the result editor, as the table tab does.

---

# UI-05 — Safe mode can be downgraded with an empty password when the connection has no stored password

**Severity: P2 (removes write protection without a secret)**
**Location:** `internal/ui/workspace.go:736-753` vs `:272-277`

`gate` deliberately refuses to treat a blank field as the database password when `w.o.Password == ""` (`case password && w.o.Password == "":`). `pickSafeMode` instead validates `v["pw"] != w.o.Password`; with an empty stored password, an empty field passes.

**Trigger (verified):** a connection in `safe2` with no stored password (passwordless/trust/SSH-tunneled/IAM). Prefix `S` → pick "Silent Mode" → press Enter on the empty password form → `level now silent`.

**Fix:** use the same rule as `gate`: when there is no stored password, require an explicit confirmation dialog, not a password comparison.

---

# UI-06 — Auto-save of saved queries is dead code

**Severity: P2 (edits silently never persisted)**
**Location:** `internal/ui/tab_query.go:54`, `:478-485`

`t.savedAs` is declared and read but never assigned anywhere (`grep -rn savedAs internal/ui` returns only those two lines). `newQueryTab(w,id)` does not set it, and opening a saved query uses `newQueryTab(text)` with only the SQL. With the default `editor.auto_save_queries = true`, editing a saved query never calls `Queries.Update`; the tab is not `Dirty()`, so closing/quitting discards the edit without warning. `addToQueries` (`:673-701`) saves a new file but never adopts it.

**Fix:** pass the saved query's path into the tab and set `savedAs` on open/save.

---

# UI-07 — Quitting is not gated on an in-flight commit

**Severity: P2 (commit cut off mid-batch)**
**Location:** `internal/ui/app.go:440-456`, `:304-313`, `internal/ui/workspace.go:313-319`, `:501-503`

`quit` is not in the `mutating` set, and `confirmQuit` only checks `dirty()`. `execute` runs `sess.Apply(context.Background(), …)` with no deadline specifically so a commit is not cut off, but:

- During a DDL/admin batch (truncate/drop/rename/routine swap) no tab is dirty, so prefix `q` quits instantly with no prompt while `w.applying` is true; `w.close()` then closes the session under the running `Apply`.
- During a grid commit the dialog says "Pending edits will be lost", which is misleading: the edits were already sent, and `y` kills the in-flight apply.
- `tea.InterruptMsg` skips even the dirty check and tears down with `applying` still true.

**Fix:** block/interrupt quit while `applying`, with an explicit "a commit is still running" dialog.

---

# UI-08 — Cancelling the reload after a commit wedges the tab at "still loading"

**Severity: P2**
**Location:** `internal/ui/tab_table.go:886-895`, `:880-882`, `:336`, `:286-293`

`committing` is set in the commit callback and cleared only by a `tableLoadedMsg` with the matching `seq`. Pressing prefix `.` while that reload is in flight calls `CancelWork`, which bumps `seq` and clears `loading` but not `committing`; the reload's message is now stale and never clears it. Every later commit answers "still loading; wait before committing again" until another load (alt+r, sort, page) lands.

**Fix:** clear `committing` in `CancelWork` (or when the seq is invalidated).

---

# UI-09 — `openObject` replaces a clean table tab without `Close()`

**Severity: P3 (leaked query + cancel functions)**
**Location:** `internal/ui/workspace.go:400-406`

```go
if c, ok := w.tab().(*tableTab); ok && !newTab && !c.Dirty() {
    w.tabs[w.cur] = nt
```

The replaced tab may still be loading; its `cancels` are never called and `closed` is never set (`tab_table.go:277-281`), so its queries keep running and their results are dropped by tab-ID lookup.

**Fix:** call the replaced tab's `Close()` before substituting.

---

# UI-10 — Data race: `makeEditable`'s cached branch reads `t.ws.gen` off the UI goroutine

**Severity: P3**
**Location:** `internal/ui/resultedit.go:69-74`

Every other async message captures `gen` on the UI loop (`workspace.go:214`, `tab_query.go:311/221`). This closure reads `t.ws.gen` when Bubble Tea runs it, while the UI loop writes `w.gen` in `forget(nil)` — an unsynchronized read/write that `-race` would flag when a reload coincides with a result committing.

**Fix:** capture `gen` before returning the closure.

---

# UI-11 — Cancel with nothing running produces no feedback

**Severity: P3**
**Location:** `internal/ui/workspace.go:519-532`

`CancelWork` returns `nil` for idle tabs but still appends a nil command, so `len(cmds) == 0` is false whenever any tab exists. Prefix `.` with nothing running yields `tea.Batch(nil…)` and no message instead of "nothing is running".

**Fix:** only append non-nil commands.

---

# UI-12 — Quick-look truncation hint promises more than it does

**Severity: P3**
**Location:** `internal/ui/doc_views.go:443-449`, `:471-472`

For a value over `quickLookMax`, `newQuickLook` sets `raw: true` and the text ends "c copies everything", but `Update`'s `case "c"` copies the truncated `q.text`; the full value is unavailable in the dialog.

**Fix:** keep the full value separately for the copy action, or correct the hint.

---

# PG-01 — Changing a primary key fails on any table whose PK constraint is not named `<table>_pkey`

**Severity: P2 (operation impossible through the UI; loud error, no data loss)**
**Location:** `internal/db/postgres/plan.go:330-338`

The diff drops the old PK by synthesizing `old.Name + "_pkey"` unless `old.Indexes` contains the primary index:

```go
if pkChanged && len(oldPK) > 0 {
    name := old.Name + "_pkey"
    for _, ix := range old.Indexes {
        if ix.Primary { name = ix.Name }
    }
    alter("DROP CONSTRAINT " + ident(name))
}
```

The structure editor filters primary indexes out of the definition it hands to `PlanTable` (`internal/ui/struct_view.go:219-221`), so the loop is dead code on the UI path. `Indexes()` already knows the real name (`internal/db/postgres/meta.go:123`).

**Trigger (verified):** a table created with `CONSTRAINT my_pk PRIMARY KEY (…)`, or with the PK added/renamed later. Toggle `primary_key` in the structure editor and commit:
```
ALTER TABLE "public"."zz_pk" DROP CONSTRAINT "zz_pk_pkey"
→ ERROR: constraint "zz_pk_pkey" of relation "zz_pk" does not exist
```

**Fix:** carry the primary index in the definition, or look up the constraint name before dropping.

---

# PG-02 — Unique/exclusion constraint indexes are dropped with `DROP INDEX`, which PostgreSQL always rejects

**Severity: P2 (structure edit impossible; loud error)**
**Location:** `internal/db/postgres/plan.go:345-349`

`Indexes()` returns every `pg_index` row, including the index behind a `UNIQUE`/`EXCLUDE` constraint; `Index.Primary` excludes only the PK. The diff then emits `DROP INDEX` for those.

**Trigger (verified):** delete or modify the `email` unique-constraint row in the index grid:
```
DROP INDEX "public"."zz_u_a_key"
→ ERROR: cannot drop index … because constraint … requires it
```

**Fix:** use `ALTER TABLE … DROP CONSTRAINT` for constraint-backed indexes (the identity is known from `pg_constraint`).

---

# PG-03 — "Clone with data" produces a clone that cannot accept inserts

**Severity: P2 (clone reports success but is broken)**
**Location:** `internal/db/postgres/plan.go:100-103`

`CREATE TABLE … (LIKE src INCLUDING ALL)` + `INSERT INTO clone SELECT *` has three verified failure modes:

1. `GENERATED ALWAYS AS IDENTITY` source → the copy is rejected (`cannot insert a non-DEFAULT value into column "id"`); needs `OVERRIDING SYSTEM VALUE`.
2. `GENERATED BY DEFAULT AS IDENTITY` → the clone gets its own sequence that is never advanced, so the first natural insert hits a duplicate key.
3. `serial` source → the copy keeps `DEFAULT nextval('src_id_seq')`, so inserts into the clone advance the source's sequence and break if the source is dropped.

**Fix:** emit `OVERRIDING SYSTEM VALUE` and follow the copy with `setval(pg_get_serial_sequence(…), max(pk))`, matching the driver's serial→identity policy elsewhere.

---

# PG-04 — Foreign tables and partition children are rendered as plain `CREATE TABLE`

**Severity: P2 (silently wrong DDL)**
**Location:** `internal/db/postgres/meta.go:28-34` (classification), `:267-303` (`tableDDL`)

`Objects` maps `relkind 'f'` to `"table"`, and `tableDDL` has no branch for foreign tables or `PARTITION OF`.

**Trigger (verified):** *Copy Create Script* on a foreign table emits a plain `CREATE TABLE` with no `SERVER`/`OPTIONS`; on a partition child it emits no `PARTITION OF`/bound. Neither errors, so the script silently creates a different object.

**Fix:** classify `f`/partition children separately and generate `CREATE FOREIGN TABLE` / `PARTITION OF`, or refuse with a clear message.

---

# PG-05 — The Any-column filter cache is only invalidated by `Apply`, so DDL in the SQL editor leaves it stale

**Severity: P2 (silent wrong results)**
**Location:** `internal/db/postgres/exec.go:206-233`, `:19-21`, `:313-315`

Only `Apply` clears `s.cols`; anything run through `Exec` leaves it untouched, and the cache is per session.

**Trigger (verified):** filter *Any column contains "first"* (populates the cache), then `ALTER TABLE … ADD COLUMN b` + `UPDATE … SET b='second'` in the editor, then filter *Any column contains "second"* → 0 rows instead of 1. A dropped column makes the filter query fail with "column does not exist".

**Fix:** clear the column cache after any successful non-read-only statement, or include the relation's schema version.

---

# PG-06 — Session state set in the editor leaks into one pooled connection

**Severity: P2 (nondeterministic results)**
**Location:** `internal/db/postgres/postgres.go:296-307`

`pool.put` returns a connection to the idle list whenever `TxStatus == 'I'`; it never runs `DISCARD ALL` or resets session state. `SET search_path`, `SET ROLE`, `SET TIME ZONE`, `SET statement_timeout`, temp tables, and `LISTEN` persist on whichever of the up-to-8 connections served the statement.

**Measured:** after `SET search_path TO app`, 16 concurrent `SHOW search_path` returned `public` 13 times and `app` 3 times.

**Trigger:** `SET search_path TO app` (or `SET TIME ZONE 'UTC'`) in the editor, then use the grid/editor: unqualified names resolve differently between runs and `timestamptz` text changes offset between refreshes.

**Fix:** run `DISCARD ALL` before returning a connection to the pool when session state changed, or pin a connection for the editor.

---

# PG-07 — `UseDatabase` publishes the new identity before the pool is swapped

**Severity: P2 (race; wrong database/schema on concurrent loads)**
**Location:** `internal/db/postgres/postgres.go:131-133`, `:193-204`

`connect` writes `s.dbName`, `s.schema`, and `s.version` as soon as the new pool's probe query returns, but `s.pool` is replaced later. Between those windows `CurrentDatabase()`/`CurrentSchema()` report the new database while `s.p()` still returns the old pool. A concurrent metadata/rows load (the TUI loads in background goroutines) then queries `newSchema.table` in the old database.

**Fix:** return the identity from `connect` and commit all four fields under one lock after the swap.

---

# PG-08 — Dropping/renaming an overloaded function always fails

**Severity: P2 (operation impossible; loud error)**
**Location:** `internal/db/postgres/meta.go:352-361`, `internal/db/postgres/plan.go:77-109`

`Objects` returns one row per overload but puts the identity arguments only in `Detail`; the `ObjRef` carries just the name.

**Trigger (verified):** `Objects("public")` returned two rows named `zz_f`; `DROP FUNCTION "public"."zz_f"` → `function name "public.zz_f" is not unique` (the same applies to `ALTER FUNCTION … RENAME`).

**Fix:** carry identity arguments (or the OID) in `ObjRef`/`Object` and emit `DROP FUNCTION "s"."f"(int)`.

---

# PG-09 — `date` values render with a fabricated midnight time

**Severity: P3 (display only)**
**Location:** `internal/db/postgres/exec.go:157-161`, `internal/db/format.go:27-28`, `internal/db/dialect.go:106-107`

`DateOID` is scanned into `time.Time`, then formatted with `2006-01-02 15:04:05.999999`, so a `date` cell shows `2024-01-02 00:00:00`; CSV/INSERT exports and commit previews inherit it. It round-trips (Postgres `date_in` ignores the time), but it is inconsistent with other date displays.

**Fix:** return the server text for `DateOID`, as `timestamptz` already does.

---

# PG-10 — Recreating an edited index silently drops `INCLUDE`, opclass, ordering, and collation

**Severity: P3 (behaviour/performance only)**
**Location:** `internal/db/postgres/plan.go:157-185`, `internal/db/postgres/meta.go:123-126`, `:276-279`

`indexSQL` rebuilds from key columns only; `Indexes()` reports only `1..indnkeyatts`, and `indexEqual` ignores opclass/`indoption`/collation.

**Trigger:** rename or change the column list of `CREATE INDEX i ON t (a) INCLUDE (b)` or `(a DESC, b text_pattern_ops)`; the recreated index is just `(a)` / `("a","b")`, with no error. *Copy Create Script* (which uses `pg_get_indexdef`) disagrees with the structure editor.

**Fix:** preserve the full definition (or regenerate via `pg_get_indexdef`).

---

# PG-11 — A genuine stream failure on the truncated-result path is reported as success

**Severity: P3 (only at the row limit, default 300)**
**Location:** `internal/db/postgres/exec.go:54-72`

When the limit trips, `stop()` closes the result and `rows.Err()` is intentionally ignored. That also swallows connection loss, a server cancel, or an abort mid-stream, and `Affected` comes from a zero `CommandTag`. Measured: the same shape of query sometimes stops server-side (2 rows, 103 ms), sometimes drains the full result (40 rows, 4.2 s), so contents and `Affected` are timing-dependent.

**Fix:** ignore only the cancellation error caused by `stop()`, and return any other `rows.Err()`.

---

# CM-01 — A `USE` preceded by a comment bypasses the intercept and switches one pooled connection

**Severity: P1 (silent keyspace mismatch; could read/write the wrong keyspace)**
**Location:** `internal/db/cassandra/cassandra.go:341`, regex at `:272`; `internal/db/split.go:12`, `:30-40`

`Exec` recognizes `USE` with `useRe`, anchored at `^\s*use` (case-insensitive). `db.Split` deliberately keeps leading comments in `Stmt.Text` (`split.go:12`: "trimmed, includes leading comments"), so a comment before `USE` defeats the regex. The statement is then handed to gocql, whose own guard (`isUseStatement`, which checks `stmt[0:3]`) also misses it, and the server executes `USE` on **one pooled connection** while `s.ks`, the sidebar, `resetCaches`, and history still report the old keyspace.

**Trigger (verified live against Cassandra 5.0):**
```
-- switch
USE reporting;
DELETE FROM orders WHERE id = 1;   -- unqualified: may now run in either keyspace
```
`Exec("/* c */ USE system_schema")` → `err=nil`, `CurrentDatabase()=="tuidb"`, and later unqualified queries ran in `system_schema` (40/40 in the probe). App-generated SQL is keyspace-qualified, so the damage is confined to user-authored unqualified statements — which then run in an unpredictable keyspace.

**Fix:** classify on `scan("cql", stmt)` tokens (which already yields `USE` as token 0 for all these forms), as `cqlReadOnly` does, and strip leading comments before the regex.

---

# CM-02 — `DDL` swallows every `DESCRIBE` error and silently returns a lossy rebuilt `CREATE TABLE`

**Severity: P2 (misleading DDL)**
**Location:** `internal/db/cassandra/meta.go:223-229`, `:248-259`

```go
rows, err := s.maps(ctx, "DESCRIBE "+kw+" "+q(ks)+"."+q(ref.Name))
if err == nil && len(rows) > 0 { return … }
if ref.Kind == db.KindTable { return s.rebuiltDDL(ctx, ref) }
```

Any DESCRIBE failure — old server, permission error, timeout — silently yields a statement built only from `system_schema.columns`: no `WITH` options (compaction, compression, TTL, gc_grace, comment), no indexes, no `COMPACT STORAGE`. It is presented as "the table's DDL" and the user will copy/re-run it.

**Trigger:** a 3.x server (the documented case) or any transient/authorization failure on 4/5, with no warning. Contrast `KindMatView`, which at least surfaces the error.

**Fix:** if DESCRIBE fails for a reason other than "not supported", return the error; if falling back, label the result as lossy.

---

# CM-03 — `Indexes` reports an arbitrary Java class as `Algorithm`, and `createIndex` silently degrades it

**Severity: P3**
**Location:** `internal/db/cassandra/meta.go:176-183`, `internal/db/cassandra/admin.go:240-245`

`case class != "": ix.Algorithm = class` puts raw `class_name` into the UI. `createIndex` only honors `"SAI"`/`"SASI"`; anything else silently becomes a plain `CREATE INDEX`, so a third-party/legacy custom index re-created from a loaded `TableDef` loses its class.

**Fix:** map known classes, and reject unknown ones instead of silently downgrading.

---

# CM-04 — `Objects` ignores the keyspace argument while every other metadata call honors `ref.Schema`

**Severity: P3 (latent API asymmetry)**
**Location:** `internal/db/cassandra/meta.go:65`, `:288-294`, `internal/web/handlers.go:192`

`Objects(ctx, _ string)` always returns the current keyspace and never errors, while `Columns`/`Indexes`/`DDL`/`Rows` accept a qualified ref. A caller asking for keyspace B silently gets A's list. Harmless with today's UI (the sidebar passes `CurrentSchema()` = ""), but a future caller will trip over it.

**Fix:** honor the argument or document that it must be empty.

---

# CM-05 — User-facing `<nil>` error text when the metadata row is missing

**Severity: P3**
**Location:** `internal/db/cassandra/meta.go:230`, `:234`

`fmt.Errorf("describe %s: %v", ref.Name, err)` and `fmt.Errorf("type %s.%s not found: %v", …, err)` print `<nil>` when the query succeeded with zero rows (stale ref, dropped view), e.g. `describe x: <nil>`.

**Fix:** return a "not found" error when `err == nil && len(rows) == 0`.

---

# CM-06 — `inCQLBatch` re-tokenizes the growing batch prefix at every `;`

**Severity: P3 (quadratic on large batch scripts)**
**Location:** `internal/db/classify_cql.go:32`, `internal/db/split.go:107`

`Split` evaluates `inCQLBatch(text[start:i])` for every candidate terminator, and `inCQLBatch` runs `upper(scan("cql", seg))` over the whole segment from `start`. A large single `BEGIN BATCH …; …; APPLY BATCH` paste re-tokenizes the entire prefix at each `;` — O(n²) time and allocations. Ordinary multi-statement scripts are unaffected.

**Fix:** keep incremental lexer state for the batch region instead of rescanning.

---

# CM-07 — Keyspaces that `Databases` lists can never have objects

**Severity: P3**
**Location:** `internal/db/cassandra/meta.go:82-95`

`Databases` returns `system_views` and `system_virtual_schema`, but their tables live in `system_virtual_schema.tables`, not `system_schema.tables` (verified: 0 rows vs 43). Browsing either keyspace shows an empty sidebar, and `Columns`/`DDL` on a virtual table return "table not found".

**Fix:** hide those keyspaces, or query `system_virtual_schema` for them.

---

# CM-08 — The CQL filter bar offers `LIKE`, which this driver can never make work

**Severity: P3**
**Location:** `internal/db/filter.go:41-43`, `internal/db/cassandra/admin.go:243-245`

`Operators("cql")` includes `LIKE` ("LIKE needs a SASI index"), but `createIndex` refuses SASI ("use SAI") and SAI does not implement `LIKE`. Selecting it produces a server error with no path to success.

**Fix:** drop `LIKE` from the CQL operator list.

---

# MY-01 — MySQL binary-column defaults are re-emitted as quoted hex text

**Severity: P1 (silent data corruption of the column default)**
**Location:** `internal/db/mysql/meta.go:189-197`, `internal/db/mysql/plan.go:134-136`

MySQL 8 reports `COLUMN_DEFAULT` for `BINARY`/`VARBINARY` as an uppercase hex literal (`0x01FF`), which is not in `rawDefault` and has no `DEFAULT_GENERATED` flag. `columnDefault` therefore runs `quote()`, and the planner emits `DEFAULT '0x01FF'` — the 6-character string, not the bytes.

```go
if strings.Contains(extra, "DEFAULT_GENERATED") || rawDefault[strings.ToLower(dataType)] {
    return def.String, true
}
return quote(def.String), true
```

**Trigger (verified end-to-end through `PlanTable`+`Apply` on MySQL 8.4):** edit any column on a table with a binary default (changing length/type/nullability rebuilds the whole column):
```
ALTER TABLE `t` MODIFY COLUMN `v` varbinary(20) NULL DEFAULT '0x01FF'   -- succeeds
v default now: 0x307830314646   -- ASCII "0x01FF"
```
For `varbinary(4)` it fails loudly (`Invalid default value`). Every later `INSERT` that relies on the default stores the wrong bytes.

**Fix:** treat `0x…`/`b'…'`/bit-literal defaults as raw expressions, and keep binary defaults typed end-to-end.

---

# MY-02 — MySQL 8 expression defaults are re-emitted with `information_schema` escaping

**Severity: P2 (structure edit produces invalid DDL)**
**Location:** `internal/db/mysql/meta.go:189-192`; `unescapeExpr` at `:168-172` is applied to generation expressions and index expressions but not `COLUMN_DEFAULT`

For `DEFAULT_GENERATED` columns MySQL returns the default with escaped quotes (`_utf8mb4\'x\'`), and the code returns it verbatim, so `defaultSQL` emits `DEFAULT (_utf8mb4\'x\')` — a syntax error.

**Trigger (verified):** touch any `TEXT`/`JSON`/`VARCHAR` column with a literal expression default (`DEFAULT ('{}')` — the required paren form for TEXT/BLOB/JSON). `Apply` → `Error 1064 near '\'x\')'`. The no-op diff is clean; only an otherwise-touched column breaks.

**Fix:** run `COLUMN_DEFAULT` through `unescapeExpr` for `DEFAULT_GENERATED` values.

---

# MY-03 — MariaDB binary defaults lose non-UTF-8 bytes

**Severity: P2 (silent corruption of the default)**
**Location:** `internal/db/mysql/meta.go:183-188`, `:94-105`

`information_schema.COLUMN_DEFAULT` is a character column, so MariaDB renders a binary default through the connection charset (`0xFF` → `?`); the MariaDB branch returns that text verbatim as SQL.

**Trigger (verified via MariaDB 11.4):** edit a column with `DEFAULT 0x01FF`; apply succeeds and `HEX(COLUMN_DEFAULT)` becomes `27013F27` (`'\x01?'`).

**Fix:** read binary defaults via `SHOW CREATE TABLE`/`HEX(COLUMN_DEFAULT)`, or detect and preserve the hex form.

---

# MY-04 — Index key attributes are not read, so re-created indexes are widened or fail

**Severity: P2**
**Location:** `internal/db/mysql/meta.go:204-223`, `internal/db/mysql/plan.go:169-175`

`Indexes` selects only `INDEX_NAME, NON_UNIQUE, COLUMN_NAME, INDEX_TYPE`; `SUB_PART` (prefix length), `COLLATION` (`D` for descending), `IS_VISIBLE`, and `INDEX_COMMENT` are dropped, so `indexSQL` rebuilds every key part as plain, ascending, full length.

**Trigger (verified):** toggle Unique on a prefixed or descending index (`DROP INDEX x, ADD UNIQUE INDEX x (...)`) → the `SUB_PART` is lost; on a `TEXT`/`BLOB` column it fails with `Error 1170` (no key length). Descending parts come back ascending. Copying a table (`PlanTable(nil, loadedDef)`) fails outright on any prefix index over TEXT/BLOB.

**Fix:** select and round-trip `SUB_PART`, `COLLATION`, visibility, and comment.

---

# MY-05 — "Clone with data" fails on tables with generated columns and leaves the empty clone

**Severity: P2**
**Location:** `internal/db/mysql/plan.go:65-68`

`INSERT INTO dst SELECT * FROM src` includes generated columns in both the select list and the implicit target list, which the server refuses.

**Trigger (verified):** clone a table with `GENERATED ALWAYS AS … [VIRTUAL|STORED]` → `Error 3105: The value specified for generated column 'g' … is not allowed. (1 of 2 statements were already applied …)` — the `CREATE TABLE … LIKE` stays applied.

**Fix:** build an explicit column list excluding generated columns.

---

# MY-06 — `sameIndex` ignores the Algorithm and Condition cells the UI lets users edit

**Severity: P3 (edits silently discarded)**
**Location:** `internal/db/mysql/plan.go:440-448`, `internal/ui/struct_view.go:171-176`

For an index whose algorithm is neither FULLTEXT nor SPATIAL, changing Algorithm (BTREE→HASH) or Condition returns `sameIndex == true`, producing no statement and no error.

**Fix:** include algorithm/condition in the comparison.

---

# MY-07 — The Any-column column list is never invalidated by DDL run through `Exec`

**Severity: P3**
**Location:** `internal/db/mysql/mysql.go:522-543`, `:324-340`, `:240`

`s.cols` survives `ALTER TABLE` in the editor, so an Any-column filter silently stops searching columns added/renamed that way until reconnect.

**Fix:** clear the cache after non-read-only statements (same issue as PG-05).

---

# MY-08 — `searchable` misses spatial type names, so they are cast to CHAR

**Severity: P3**
**Location:** `internal/db/mysql/mysql.go:512-520`

The prefix list omits `multipoint`, `multilinestring`, `multipolygon`, and `geomcollection` (MySQL's actual `DATA_TYPE` is `geomcollection`), so these columns are treated as text and the filter generates `CAST(mp AS CHAR) LIKE ?` over WKB bytes — a guaranteed miss plus a full scan.

**Fix:** add the missing prefixes and test with `GEOMETRYCOLLECTION`.

---

# MY-09 — `UseDatabase` closes the old pool while other goroutines may still use it

**Severity: P3 (transient "database is closed" errors)**
**Location:** `internal/db/mysql/mysql.go:217-221`, `:233-244`

`db()` hands out `s.pool` after releasing the lock; `UseDatabase` swaps and closes it, so a `Rows`/`Count`/`Apply` that read the old pointer gets `sql: database is closed` instead of retrying. Triggered by `USE otherdb` or a sidebar switch during a table load/count.

**Fix:** reference-count pools or retry once on a closed pool.

---

# MY-10 — `quote()` assumes backslash escapes under `NO_BACKSLASH_ESCAPES`

**Severity: P3**
**Location:** `internal/db/mysql/meta.go:195-197`, `internal/db/mysql/mysql.go:138-140`

Metadata literals always double backslashes, while the parameter path is mode-aware. With `NO_BACKSLASH_ESCAPES`, a comment or string default containing `\` is re-emitted as `\\`.

**Fix:** thread the connection's `sql_mode` into metadata quoting.

---

# MY-11 — Partial-zero and invalid stored dates are normalized to a different day

**Severity: P3 (legacy data only)**
**Location:** `internal/db/mysql/mysql.go:444-455`

Only the exact all-zero date is special-cased; `parseDateTime` builds `time.Date(year, month, day, …)`, and Go normalizes out-of-range fields, so `'2024-00-00'` becomes `2023-11-30` and `'2024-02-31'` becomes `2024-03-02`. The grid shows the wrong date, and using the cell as a row key can update/delete 0 rows.

**Fix:** preserve invalid dates as text.

---

# MY-12 — `Connection.Options` is never read by the MySQL driver

**Severity: P3**
**Location:** `internal/db/mysql/mysql.go:129-142`

`ParseURL` puts every non-consumed query parameter into `Options`, and the connection editor exposes an Options field, but the driver ignores them (`charset`, `sql_mode`, `loc`, …). A pasted TablePlus/CLI URL silently runs with driver/server defaults.

**Fix:** apply recognized DSN parameters (or reject unsupported ones).

---

# UI-13 — The engine strip pushes the connection list off-screen on short terminals

**Severity: P2 (new-layout regression; list invisible and still interactive)**
**Location:** `internal/ui/welcome.go:669`, `:775-782`

The engine header is 11 lines (15 with the logo), and `bodyH := max(6, H-len(head)-2)` imposes a 6-line floor. The body therefore always overflows `H`, and `lines[:H]` clips the list away.

**Trigger (verified at 100 columns):** terminal height 15 → only the list frame's top border and column header are visible; no connection row, yet Enter/`e`/`d` still act on the invisible selection. Height 16-18 → one row and no bottom border. Height ≤ 14 → nothing. Before the engine strip, the list was visible at 15 rows.

**Fix:** make the strip shrink or scroll (drop art/tiles first), and never floor the body above the available height.

---

# UI-14 — The production/staging TAG badge disappears at 118-137 columns

**Severity: P2 (safety signal lost in the new layout)**
**Location:** `internal/ui/welcome.go:670-677`, `:738-741`

When the Quick-start panel switches on at `W >= 118`, `sideW = 54` leaves `listW ≈ 63`, but a connection row is a fixed `1+2+26+12+26+tag` ≈ 79 cells; the tag is the part that gets truncated.

**Trigger (verified):** a connection tagged `production`: visible at width 100, absent at 118/120, visible again at 140.

**Fix:** truncate the host/name columns first, or reserve space for the tag.

---

# UI-15 — Tabbing through the Driver field silently rewrites a cross-engine port

**Severity: P2 (unintended change persisted on save)**
**Location:** `internal/ui/dialog.go:307`, `:359-364`, `internal/ui/welcome.go:450-463`

`fl` is captured before the key is handled, so Tab/↓ moves focus first and then calls `onChange(fl.Key, …)` for the field you left. `welcome`'s driver handler rewrites the port whenever the current port is any engine's default.

**Trigger (verified):** edit a connection that uses Postgres over port 3306 (proxy), press Tab on the Driver field without changing it → `isDefaultPort("3306")` is true → the port becomes 5432 and saving persists it.

**Fix:** call `onChange` with the new field after the move, and only rewrite the port when the driver value actually changed.

---

# UI-16 — Engine tile labels overflow their 11-cell inner width

**Severity: P3**
**Location:** `internal/ui/welcome.go:647`, `center` at `:117-120`

`center()` only pads, never truncates, so a label longer than `tileW-2 = 11` widens that row by 1-2 cells while the border rows stay 13, shifting every tile to the right.

**Trigger (verified):** one ClickHouse connection → `│ClickHouse ·1│`; also `Cockroach ·1`, `Cassandra ·1`, `DynamoDB ·1`, or any engine with ≥10 saved connections.

**Fix:** truncate labels to `tileW-2`.

---

# UI-17 — An object-list error hides the local Queries/History tabs

**Severity: P3**
**Location:** `internal/ui/sidebar.go:921-939`, `:377-389`

The `s.loading`/`s.err` cases are evaluated before `s.tab == 1/2`, and `switchTab` never clears `s.err`, so a failed `Objects` call replaces saved queries and run history (which need no DB) with "✗ Could not load … r reload"; `r` reloads objects, not queries.

**Trigger (verified):** make `Objects` fail (e.g. no catalog permission), then press `2`/`3`.

**Fix:** handle the local tabs before the remote error/loading states.

---

# UI-18 — `fChoice` with empty `Options` divides by zero

**Severity: P3 (latent crash; UNSURE reachable)**
**Location:** `internal/ui/dialog.go:355-364`

`fl.Options[(cur+1)%n]` panics (`integer divide by zero`) when `len(fl.Options) == 0`. Today all call sites pass non-empty lists, but `newForm` does not guard.

**Fix:** no-op or return an error when `len(Options) == 0`.

---

# UI-19 — Empty `checklistDlg` indexes `items[-1]`

**Severity: P3 (latent crash; unreachable today)**
**Location:** `internal/ui/dialog.go:705`, `:727-743`

With zero items, Down/`j` sets `c.sel = min(0, -1) = -1`, `View` sets `c.top = -1`, and the loop reads `c.items[-1]` (verified panic). The only caller returns early when `len(t.cols)==0`.

**Fix:** guard `len(items) == 0` in `Update`/`View`.

---

# UI-20 — MySQL trigger round-trip is split at the body's semicolons

**Severity: P3**
**Location:** `internal/ui/definitions.go:81`, contrast `:55-59`

The generated trigger script has no `DELIMITER`, unlike the routine path, so a `BEGIN … END` trigger body is cut into fragments by `db.Split` on "run all" and the `CREATE TRIGGER` fails (or a valid fragment executes alone).

**Fix:** emit the `DELIMITER $$ … DELIMITER ;` wrapper for triggers, as the procedure path does.

---

# UI-21 — `segmented` drops the key hint of the active tab

**Severity: P3**
**Location:** `internal/ui/frame.go:118-121`

The active branch ignores `label` (which carries `keys[i]`), so the selected result tab loses its shortcut and the strip width changes when switching tabs.

**Fix:** render the shortcut for both states (or reserve its width).

---

# UI-22 — Precedence makes the `hintChips` space test dead

**Severity: P3**
**Location:** `internal/ui/frame.go:87`

`ok && !strings.Contains(k, " ") || ok && len(k) <= 12` parses as `ok && (!contains || len(k) <= 12)`, so any prose fragment containing `": "` with a ≤12-char prefix renders as a key badge (e.g. `"copy as URL: without the password"`).

**Fix:** parenthesize the intended condition.

---

# UI-23 — `password_cmd` failures discard stderr

**Severity: P3 (opaque error)**
**Location:** `internal/ui/connect.go:35`, `internal/config/connections.go:127-132`

Only stdout is captured, so a failing resolver (`op read`, `pass`, `gcloud`, …) reports just `exit status 1`; its diagnostic is discarded. The 30 s budget is also shared with the tunnel and driver open, so a slow resolver can consume the connect timeout.

**Fix:** capture stderr into the error (redacting the secret) and give resolution its own budget.

---

# CC-01 — NULL fields inside a UDT become typed zero values; a NULL `varint` panics the process

**Severity: P1 (crash; silent NULL loss on write-back)**
**Location:** `internal/db/cassandra/normalize.go:166-177`, `:90-96`, `:60`

gocql's UDT-into-`map[string]any` path returns *typed* zero values for NULL fields (`(*big.Int)(nil)`, `(*inf.Dec)(nil)`, `[]string(nil)`, zero UUID, empty blob). `jsonValue`'s `fv != nil` test does not catch typed nils, and `normalize` then dereferences them:

```go
if fv, ok := mv[f.Name]; ok && fv != nil {   // typed nil is not == nil
    m[f.Name] = jsonValue(f.Type, fv)
}
...
case *big.Int:
    if x.IsInt64() { ... }                    // panics on (*big.Int)(nil)
```

**Trigger (reproduced):**
```cql
CREATE TYPE u (v varint, d decimal, ip inet, b blob, u uuid);
CREATE TABLE t (id int PRIMARY KEY, u frozen<u>);
INSERT INTO t (id, u) VALUES (1, {v: null, d: null, ip: null, b: null, u: null});
```
Opening the table:
```
panic: runtime error: invalid memory address or nil pointer dereference
  tuidb/internal/db/cassandra.normalize(...) normalize.go:93
```

Without the `varint`, the same row renders `{"b":"0x","d":"\u003cnil\u003e","ip":"\u003cnil\u003e","u":"00000000-…"}`, and null text/int/set/timestamp fields show `""`/`0`/`[]`/`0001-01-01 …` instead of `null`. Writing the cell back then fails on the server (`Value '<nil>' is not a valid representation of a decimal`) or silently stores the zero value.

The top-level path is correct (`normalize.go:60` checks `!p.Elem().IsNil()`); the map path has no way to distinguish a NULL field from a zero one. `TestIntegrationNormalize` misses it because the seed UDT is fully populated and the unit test uses a missing key, not a typed zero.

**Fix:** decode UDT fields into per-field pointer destinations (or raw UDT bytes with a `UDTUnmarshaler`) so presence is observable, then emit `nil` for absent fields. At minimum nil-guard `*big.Int`/`*inf.Dec` and treat typed nils as NULL.

---

# CC-02 — Unsynchronized read of `s.cons` is a data race

**Severity: P2**
**Location:** `internal/db/cassandra/cassandra.go:145`, `:353`

`openSession` reads `s.cons` without `s.mu`, while `Exec`'s `CONSISTENCY` handler writes it under the lock. This breaks the documented "safe to call from different goroutines" contract.

**Trigger (reproduced with `-race`):** one goroutine runs `USE`/`UseDatabase` while another runs `CONSISTENCY one`.

**Fix:** read `s.cons` under `s.mu` (or pass it as an argument).

---

# CC-03 — `Close()` racing with `UseDatabase` leaks the newly installed session

**Severity: P2**
**Location:** `internal/db/cassandra/cassandra.go:225`, `:258-269`

`Close` reads `s.ses` without the mutex and closes only that session. If `UseDatabase` installs a fresh session after the read, the new session is never closed.

**Trigger (reproduced):** call `UseDatabase` and `Close` concurrently; after `Close()` returns, `Ping` still succeeds and the new gocql session (pool, control connection, goroutines) is unreachable. `Close` also drops the gocql error and would panic if called before `Open` finished.

**Fix:** take `s.mu`, set `s.ses = nil`, close the captured session, and make `Close` idempotent.

---

# CC-04 — `Rows` can never set `Truncated`

**Severity: P3 (flag/message only)**
**Location:** `internal/db/cassandra/rows.go:82-95`, `internal/db/cassandra/cassandra.go:373-374`, `:396-399`

`Rows` puts `LIMIT offset+limit` into the CQL, so the server never returns the extra row that `PageSize(limit+skip+1)` was added to observe; `run`'s `len(res.Rows) == limit` is unreachable for the table browser.

**Trigger (reproduced, 4 rows):** `Rows{Limit: 2}` → 2 rows, `truncated=false`, although 2 more exist. `Exec` with limit 2 correctly reports `truncated=true`.

**Fix:** request `limit+1` when building the browse query (and drop the extra row), or set the flag from the `LIMIT` comparison explicitly.

---

# CC-05 — `ssl_mode=prefer` silently behaves as `require`

**Severity: P3**
**Location:** `internal/db/cassandra/cassandra.go:200-202`

`case "prefer", "require": t.InsecureSkipVerify = true` maps `prefer` to "always TLS, unverified", so a URL with `?ssl-mode=preferred` (or the dialog's `prefer`) fails against a plaintext server instead of falling back — unlike MySQL, whose `tlsConfig` returns a `fallback` bool for that mode.

**Fix:** implement the fallback (try TLS, retry plaintext on a TLS handshake failure) or document that `prefer` is unsupported.

---

# CC-06 — The column-type cache is only invalidated by this session's writes

**Severity: P3**
**Location:** `internal/db/cassandra/cassandra.go:313-337`

`s.types` is cleared only by local `invalidate()`/`UseDatabase`. DDL from cqlsh, another app connection, or a migration leaves the cache missing the new column, and `colTypes`' store is not re-checked against a concurrent `invalidate()`.

**Trigger:** `ALTER TABLE t ADD ts timestamp` from another client, then edit `ts` here: `types["ts"]` is absent → `goValue` returns the raw string → gocql rejects it (`can not marshal string into timestamp`). Not silent corruption, but confusing and requires reconnect.

**Fix:** learn types lazily on marshal failure, or invalidate on unknown-column errors.

---

# CC-07 — `USE` with a trailing comment fails with a driver-internal message

**Severity: P3 (related to CM-01)**
**Location:** `internal/db/cassandra/cassandra.go:272`, `:341-346`

`useRe` requires the statement to be only `USE <name>`, so `USE ks -- switch` is forwarded to gocql, whose `isUseStatement` matches and returns `ErrUseStmt` ("use statements aren't supported…"). The keyspace does not change and the user gets no hint that the session stayed put.

**Fix:** strip trailing comments before matching (the same token-based classification as CM-01 fixes both directions).

---

# CC-08 — Browse errors drop the statement/table context

**Severity: P3**
**Location:** `internal/db/cassandra/cassandra.go:381-404`, `internal/db/cassandra/rows.go:95-100`

`run` returns `it.Close()`'s error verbatim and `Rows` returns it unwrapped; the first error from the `ALLOW FILTERING` retry is discarded. The UI shows e.g. `gocql: not enough columns to scan into: have 2 want 3` with no table/filter/statement. `Count` and `Plan` wrap theirs.

**Fix:** wrap browse errors with the table and filter description, and include both attempts when retrying.

---

# OD-01 — ClickHouse: a `USE` with a comment bypasses the session switch and diverts queries to another database

**Severity: P1 (silent wrong-database reads/writes)**
**Location:** `internal/db/clickhouse/clickhouse.go:475-492`

Only a comment-free full-string `USE` matches `useRe` and is routed to `UseDatabase`; anything else (leading/trailing comment) falls through to `s.run` and executes on **one pooled connection** (pool size 8, `:47`), while `cur`/`opts` stay on the old database.

**Trigger (verified against ClickHouse 24.8):**
```sql
-- switch to system
USE system
```
`Exec` returns "Statement executed" and `CurrentDatabase()` still reports `tuidb`; a subsequent `SELECT currentDatabase()` returned `system` or `tuidb` 20/20 times once the pool had two connections. Because `Objects` returns refs with no schema (`:258`), browse queries are unqualified (`SELECT * FROM \`customers\``), so with a same-named table in two databases the grid can show or update either one, non-deterministically and without error.

**Fix:** classify with the lexer (strip comments) before matching, or reject `USE` in the editor path and require the database picker.

---

# OD-02 — Mongo and Redis ignore `RowQuery.Columns`, so "Visible columns" does nothing

**Severity: P2 (feature silently ineffective)**
**Location:** `internal/db/redis/rows.go:173-211`, `internal/db/mongo/mongo.go:520-542`

Both drivers build every column and never read `q.Columns`, while the SQL drivers project it (`internal/db/filter.go:191-198`). The column picker saves state and the footer says "N hidden col(s)", but the columns stay visible in both UIs.

**Trigger (verified):** `Rows{Columns: ["name"]}` on MongoDB returns `[_id name email notes balance extra tags address created_at]`; `Rows{Columns:["field"]}` on Redis returns `[field value]`.

**Fix:** apply the projection after building the row set (and keep a hidden key column available for identity).

---

# OD-03 — Redis: renaming a hash field or zset member onto an existing one destroys the existing value

**Severity: P2 (silent data loss)**
**Location:** `internal/db/redis/plan.go:186-187` (hash), `:300-301` (zset)

The rename is planned as `HDEL key old` + `HSET key new v` (and `ZREM`+`ZADD`), so if `new` already exists its value/score is overwritten with no check or warning — unlike SQL, where renaming a key onto an existing one fails.

**Trigger (verified):** hash `{a:1, b:2}`, rename `a`→`b` → `{b:1}`; `b`'s original `2` is gone.

**Fix:** check `HEXISTS`/`ZSCORE` before planning and reject the rename (or require an explicit overwrite).

---

# OD-04 — Mongo: `runCommand` never drains or kills the reply cursor

**Severity: P2 (partial results + leaked server cursor)**
**Location:** `internal/db/mongo/shell.go:342-348`

`dispatch("runCommand")` returns the raw reply (including `cursor.firstBatch`) without paging or killing the cursor, unlike `execCommand` (`mongo.go:598-620`) and `runCursor` (`shell.go:645`).

**Trigger (verified):** `db.adminCommand({aggregate: 1, pipeline: [{$currentOp: {}}], cursor: {batchSize: 1}})` returns one row embedding `firstBatch`, and `$currentOp.idleCursors` shows the live `admin.$cmd.aggregate` cursor until its idle timeout. The same applies to `db.runCommand({find: …})` and `listCollections`.

**Fix:** detect a `cursor` in the reply, page it through the existing cursor runner, and kill it on completion/cancel.

---

# OD-05 — Redis: "Copy Script As → CREATE" silently stops at 100 000 elements

**Severity: P2 (incomplete backup script)**
**Location:** `internal/db/redis/redis.go:427`, `internal/db/redis/rows.go:20-22`

`createScript` fetches with `s.fetch(ctx, key, typ, 0, 0)`, which caps at `maxLoad` (100 000) and returns no truncation signal, then renders recreate commands for what it got.

**Trigger:** a hash/list/stream with >100 000 elements → the copied `HSET`/`RPUSH`/`XADD` script recreates only the first 100 000; restoring from it silently loses data. `Rows` reports the cap via `Truncated`; `createScript` has no equivalent.

**Fix:** propagate truncation into the script (comment or error) instead of emitting a partial restore.

---

# OD-06 — Redis: the object list is silently capped at 10 000 keys

**Severity: P2 (keys appear not to exist)**
**Location:** `internal/db/redis/redis.go:26`, `:271-291`

`Objects` stops at `maxKeys = 10000` unique keys and sorts/slices that arbitrary SCAN-ordered subset; `db.Object` has no truncation field and neither UI indicates it.

**Trigger:** a production database with 50 000 keys → the sidebar/web list shows 10 000; the client-side filter can never find the rest.

**Fix:** surface a truncation marker and support paging/searching server-side.

---

# OD-07 — Redis: a value that happens to be `"OK"` is rendered as a message and disappears

**Severity: P3**
**Location:** `internal/db/redis/exec.go:218-224`

`render` special-cases any bulk-string reply equal to `"OK"`, independent of the command.

**Trigger (verified):** `SET k OK`, then `GET k` → `HasRows=false, Message="OK", Rows=[]`; the grid shows no value. Same for `HGET`, `LPOP`, etc.

**Fix:** use the command's reply type/status rather than the payload text.

---

# OD-08 — Mongo: a statement whose completion value is `undefined` reports `Affected: 0` after writes

**Severity: P3**
**Location:** `internal/db/mongo/shell.go:556-565`

The `k:"u"` path returns `&db.Result{Message: msg}` without `Affected: sh.aff` (the `k:"v"` path sets it), so write counts are lost when the last expression evaluates to `undefined`.

**Trigger:** `db.c.insertMany([{…},{…}]); print("done")` → message `done`, `Affected 0`, although rows were written.

**Fix:** set `Affected: sh.aff` in both paths.

---

# OD-09 — ClickHouse: object refs carry no schema, so browse/DDL statements are unqualified

**Severity: P3 (correctness depends on uncontrolled session state)**
**Location:** `internal/db/clickhouse/clickhouse.go:250-258`, `:300`, `:311-320`

Unlike Postgres (`meta.go:53`), ClickHouse builds `db.ObjRef{Name: …, Kind: …}` without `Schema`, so `BuildSelect`/`DDL`/`Script` emit unqualified names that resolve against whichever pooled connection serves them.

**Trigger:** any statement that changes a single connection's default database (OD-01, or a `USE` in `Bootstrap`) → browsing/DDL hits a different database or fails depending on the connection. Masked today because both UIs browse only the current database.

**Fix:** stamp the database into every ref and qualify generated SQL.

---

# OD-10 — Redis/ClickHouse: `UseDatabase` closes the old pool while in-flight statements still hold it

**Severity: P3 (spurious errors under concurrency)**
**Location:** `internal/db/redis/redis.go:250-254`, `internal/db/clickhouse/clickhouse.go:220-224`

Both swap the new handle in under the lock and then `old.Close()`; a statement that fetched the old handle just before the swap fails mid-flight instead of finishing or retrying.

**Trigger:** the web API's `POST /use` (`internal/web/handlers.go:170-181`) while a `/rows` or `/exec` is running on the same session → `redis: client is closed`, etc.

**Fix:** reference-count or defer closing until no in-flight statement holds the old handle.

---

# UG-01 — Row actions in a query result leave the inline editor open, so the next Enter writes into a different row

**Severity: P1 (edit written to the wrong row; original edit lost)**
**Location:** `internal/ui/resultedit.go:207-223`, `internal/ui/grid.go:317-330`, `internal/ui/grid.go:332`, contrast `internal/ui/tab_table.go:656-657`

`resultAction` calls `g.AddRow()`/`DuplicateRows()`/`DeleteRows()`/`SetCell()` without first finishing an open inline editor. `FinishEdit` resolves the target from the current cursor (`r := g.cur()`), not from where the editor was opened.

**Trigger (verified):** run `select * from customers`, focus results, press `l`, `e`, type `XYZ` on Ann's name, press prefix `i` (add row) before Enter, then Enter. `AddRow` moves the cursor to the new row; Enter writes `AnnXYZ` into that new row and Ann's edit is lost:
```
after prefix+i: row 1 state=1(rowNew) cursor row=1 editing=true
after enter:    INSERT INTO "public"."customers" ("name") VALUES ('AnnXYZ')
```
The table tab is safe only because its `Action` finishes the edit first (`tab_table.go:656-657`, covered by `TestFlowPrefixActionWhileEditingSavesTheEditFirst`); `queryTab`/`resultAction` has no equivalent, so prefix `delete`/`D` variants can likewise drop the text or land it elsewhere.

**Fix:** call `g.FinishEdit(true)` (or cancel) at the start of `resultAction`, mirroring `tab_table.Action`.

---

# UG-02 — Preview in the Structure/Index view panics before the structure has loaded

**Severity: P1 (crash)**
**Location:** `internal/ui/struct_view.go:228-230`, `internal/ui/tab_table.go:857-860`, `:941-952`

`Statements()` sends any non-data view to `structStatements()` → `newDef()`, which iterates `t.structGrid.Rows` with no nil guard. `structGrid` is created only by `buildStructure` from the async `structureLoadedMsg`, while `setView` sets `t.view = viewStructure` immediately and starts the load.

**Trigger (verified):** open a table, press prefix `)` (Structure), then prefix `P` (preview) before the structure arrives:
```
panic: nil pointer dereference
  ui.(*tableTab).newDef(...) struct_view.go:230
  ui.(*tableTab).Statements  tab_table.go:860
```
`add_row`/`delete_rows`/`set_value` were hardened against exactly this state (`Action` lines 682-686; `Do` line 819), and `TestFlowStructureViewBeforeLoadDoesNotPanic` covers them, but `Statements` was not.

**Fix:** return an error when `structGrid == nil` or `!structLoaded`, as `structPending()` already does for `Commit`.

---

# UG-03 — Reload in the Structure/Index view never re-reads the columns

**Severity: P2 (stale structure; commits plan against the wrong definition)**
**Location:** `internal/ui/tab_table.go:836-839`, `internal/ui/struct_view.go:41-50`

`reload_tab` sets `t.structLoaded = false` and calls `loadStructure`, but `loadStructure` starts from `cached := ws.meta[ref]` and only calls `Columns` when that cache is nil; the structure-view reload never invalidates it. The data-view reload does (`t.cols = nil; t.ws.forget(&t.ref)`, `tab_table.go:851-853`).

**Trigger:** another session/editor adds or drops a column, then prefix `alt+r` in the Structure view. Indexes/FKs/DDL are re-fetched, the column grid is not; committing any structure change plans against the stale `TableDef` ("column X was renamed from Y, which does not exist", or an ADD for a column that already exists).

**Fix:** `ws.forget(&t.ref)` (and `t.cols = nil`) before `loadStructure` on reload.

---

# UG-04 — Structure/DDL loads have no epoch and cannot be cancelled

**Severity: P2 (stale response wins; pending edits dropped)**
**Location:** `internal/ui/struct_view.go:41-85`, `internal/ui/tab_table.go:356-366`, `:284-293`

Unlike `loadRows`/`loadCols`, `loadStructure` uses `ws.async` with no `t.seq`, does not set `t.loading`, is not added to `t.cancels`, and `structureLoadedMsg` is applied unconditionally; `buildStructure` rebuilds both grids.

**Trigger:** press `alt+r` twice quickly in the Structure view. Both loads run; a slower first response lands last, replaces the newer structure, and `SetData` silently drops structure edits made after the first response. Because `Busy()` is `t.loading` (false here), prefix `.` cannot cancel a hung structure/DDL fetch.

**Fix:** give structure loads the same seq/loading/cancel treatment as row loads.

---

# UG-05 — `applyCols` re-enables the grid during a reload, so an edit typed in that window is lost

**Severity: P2 (silent edit loss)**
**Location:** `internal/ui/tab_table.go:447`, `:208`, `:331`

`load()` sets `t.grid.Editable = false` ("typing into them would be lost"), and `fill`/`SetData` restores editability. `applyCols` — called from the `colsMetaMsg` handler for any seq — unconditionally re-enables the grid while `t.loading` is still true.

**Trigger:** prefix `alt+r` on a remote table (columns are cleared, so `loadCols`/`loadRows` race); the columns response usually arrives first and re-enables the stale grid; the user edits a cell; the rows response calls `SetData`, discarding the pending edit without a message. `Commit` is guarded by `t.loading`, so this is edit loss rather than a wrong write.

**Fix:** don't re-enable the grid while `t.loading`, or make `applyCols` seq-aware.

---

# UG-06 — FK edits are keyed by the drafted column name and survive column deletion

**Severity: P2 (invalid/duplicate constraints)**
**Location:** `internal/ui/struct_view.go:663`, `:281-296`

`fkForm` stores `t.fkEdits[col]` with the current (possibly renamed) name, while `newDef` decides which loaded FKs to drop by matching original names and then appends every non-nil `fkEdits` entry regardless of whether its column still exists.

**Trigger:** (a) rename `a` → `b`, then edit `b`'s FK: the loaded FK on `a` is not dropped and a second FK on `b` is added (duplicate/conflicting constraint); (b) add an FK then delete the column row: `newDef` still emits `ADD CONSTRAINT … FOREIGN KEY (deleted_col)`, blocking the commit with a confusing error.

**Fix:** key FK edits by original column name (or index), and drop edits whose column is gone.

---

# UG-07 — Paging one page past the end produces contradictory chrome

**Severity: P3**
**Location:** `internal/ui/tab_table.go:754`, `:1004`, `:1107-1110`

`next_page` checks only `loadedRows >= t.limit` (never the count), so a table with exactly `limit` rows allows one empty page. The empty page renders "No rows yet · add a row" even with `offset > 0`, and the footer's `first = 0` special case turns `offset+loadedRows` into `"rows 0–N of N"` while showing none.

**Fix:** use the count (or a `loaded < limit` check) to disable Next, and word the empty state for offset > 0.

---

# UG-08 — Commit/Discard/reload in the DDL/JSON/Tree view act on the invisible data grid

**Severity: P3 (commits changes the user cannot see)**
**Location:** `internal/ui/tab_table.go:857-876`, `:841-854`, `:1088`

`Statements`/`Commit`/`Discard` only special-case `viewStructure`/`viewIndex`, so in `viewDDL`/`viewJSON`/`viewTree` prefix `s` sends the data grid's pending edits, whose indicator is only drawn for `viewData`. Conversely, `alt+r` in the DDL view reloads rows and never refreshes `t.ddlBase`, so the visible DDL cannot be refreshed.

**Fix:** refuse/redirect commit in non-data views, and make reload refresh the active view.

---

# UG-09 — Duplicate result column names reset the cursor column on every reload

**Severity: P3**
**Location:** `internal/ui/grid.go:68-105`

`sameNames` compares name→width maps, so `select id, id from t` yields `len(old)==1 != len(cols)==2` and `SetData` treats every reload as "different columns", resetting `col`/`left`. Editing such a result makes the cursor jump to column 0 after every commit.

**Fix:** compare ordered `(name, width)` pairs, not a map.

---
