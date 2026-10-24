# plan-frontend.md — Web UI for tuidb (TanStack) on the existing Go engine

> On approval this file is copied verbatim to `plan-frontend.md` at the repo root.

## Context

tuidb is a Go terminal client for 12 database engines. All engine logic already lives behind
one contract, `db.Session` (`internal/db/types.go`): metadata, `Rows`/`Count`, `Exec`, `Plan`/
`PlanAdmin`/`PlanTable`, `Apply`, with `db.Split`, `db.IsReadOnly`, `db.FormatValue`,
`safemode.Gate`, and the `store` package (history, saved queries, table state) around it. The
Bubble Tea UI (`internal/ui`) is only a renderer plus orchestration on top of that contract.

Goal: the same binary gains `tuidb --frontend`, a local HTTP+JSON API over the same engine, and a
TanStack web app embedded in the binary. The TUI stays untouched. Web and TUI share
connections.toml, history, saved queries and table state because both go through `config` and
`store`.

Non-goals (not in this plan): multi-user/remote hosting, auth beyond a local token, structure
editor (`PlanTable`) in the browser, process list, themes/keymap parity, SSR (no TanStack Start).

## Precondition

`go.mod`/`go.sum` currently carry unresolved merge conflicts (DynamoDB AWS SDK vs. Cassandra
driver) and `internal/db/filter.go` is also marked `UU`. Nothing builds until that is resolved;
step 0 of milestone 1 is to resolve them (keep both dependency sets) and get `make test` green.

## Architecture

```
browser (web/, TanStack Router + Query + Table, CodeMirror)
   │  fetch JSON, Authorization: Bearer <token>; AbortController cancels → ctx cancel
   ▼
internal/web  (net/http, Go 1.22 ServeMux patterns)        ← NEW, thin
   ├─ sessions map: id → *ui.Opened + safemode.Level + config.Connection + result cache
   ├─ handlers call db.Session / store / config directly
   └─ embed web/dist (go:embed) and serve it at /
   ▼
internal/db, internal/store, internal/config, internal/safemode   ← UNCHANGED
```

Design rules that keep the API tiny:

- **Server-side result cache.** Every `Rows`/`Exec` result gets a `resultId`; the server keeps
  the `*db.Result` (already bounded by the row limit). Edits reference `(resultId, rowIndex,
  colIndex)`. The server builds `[]db.Change` from the cached *original* `any` values exactly as
  `Grid.Changes` does (`internal/ui/grid.go:430`), so bytes/time/decimal round-trip without
  inventing a JSON type system. Cells go to the browser as `string | null` via `db.FormatValue`.
  Key columns come from `Session.Columns(ref)` (the `PK` flag, cached per ref like
  `Workspace.meta`), not from `ResultColumn`; an `/exec` result is editable only when its
  statement is a single-table select whose full PK is present, mirroring `resultedit.go`. The
  first version only edits table-tab results; editable query results are milestone 3.
- **Safe mode stays server-side.** `POST …/plan` returns the statements plus
  `{confirm, password}` from `safemode.Level.Gate(readOnly)` with `readOnly` computed via
  `db.IsReadOnly` (same loop as `Workspace.gate`, `internal/ui/workspace.go:263`). `POST …/apply`
  takes an optional `password`, compared to `Opened.Password`. The browser can never skip the gate.
- **Cancellation = request cancellation.** Handler ctx is the driver ctx (the Session contract
  already requires honoring it). No job queue, no SSE.
- **Reuse `ui.DefaultConnect`** (`internal/ui/connect.go:32`) for password_cmd + SSH tunnel +
  driver open. The web package imports `ui` for this one function; no refactor.

## Go changes

### `main.go`
- If the `--frontend` flag is set (opt-in; the TUI is the default and nothing starts a server otherwise), call `web.Run(...)` after the same `config.Load`/
  `LoadConnections`/`LoadState` setup (lift lines 50–79 into a `deps()` helper used by both
  paths). Companion flags, only meaningful with `--frontend`: `--listen 127.0.0.1:7788`, `--open` (open browser), `--token` (default random).
  Prints `http://127.0.0.1:7788/#token=…` on start.

### `internal/web/` (new package, ~5 files)
- `server.go`: `Run`, mux, bearer-token middleware (constant-time compare), `Origin` check
  (must be the listen origin or absent), JSON helpers, error → `{error}` with 4xx/5xx.
- `sessions.go`: `sessions` map guarded by a mutex; `open(conn) → id` via `ui.DefaultConnect`;
  level from `safemode.Parse(orDefault(c.SafeMode, cfg.General.DefaultSafeMode))` (fail-closed
  to Safe2 like `workspace.go:86`); keep-alive ping ticker when `cfg.General.KeepAlive`; result
  cache `map[resultId]cached{ref, cols, *db.Result}` with `DELETE` to evict.
- `handlers_meta.go`, `handlers_data.go`, `handlers_store.go`: one small func per route below.
- `changes.go`: `buildChanges(c cached, edits) []db.Change` — port of `Grid.Changes`; special
  values from the browser are `{"$": "null"|"default"|"empty"}` or `{"$expr": "NOW()"}` →
  `db.Null`/`db.Default`/`db.Empty`/`db.Expr`; everything else is a string.
- `static.go`: `//go:embed dist` + SPA fallback to `index.html`. `dist` is produced by the web
  build and committed-or-built in CI (Makefile `web` target); a stub `dist/index.html` is
  checked in so `go build` never breaks without Node.

### API (all under `/api`, JSON)

| Method/Path | Engine call |
|---|---|
| `GET /connections` · `PUT /connections` | `config.LoadConnections` / `SaveConnections` (whole list, same as TUI) |
| `POST /connections/test` | `ui.DefaultConnect` + `Ping`, then close |
| `POST /sessions {name}` → `{id, caps, serverInfo, database, schema, level}` | open |
| `DELETE /sessions/{id}` · `POST …/ping` | `Close` / `Ping` |
| `GET …/databases` · `POST …/database {name}` | `Databases` / `UseDatabase` |
| `GET …/schemas` · `GET …/objects?schema=` | `Schemas` / `Objects` |
| `GET …/objects/{schema}/{name}/{columns,indexes,triggers,fks,ddl}` · `…/script?kind=` | the matching `Session` method |
| `POST …/rows {ref, filters, sort, columns, limit, offset}` → `{resultId, columns, cells, docs, truncated, duration}` | `Rows` |
| `POST …/count {ref, filters, estimate}` | `Count` |
| `POST …/exec {text, limit}` → `[{stmt, resultId?, columns, cells, affected, message, duration, error}]` | `db.Split(caps.Dialect, text)` then `Exec` per statement, stops on first error when `cfg.Editor.ReturnOnError`; appends `store.History` like `Workspace.record` |
| `DELETE …/results/{rid}` | evict cache |
| `POST …/plan {resultId, edits, deletes, inserts}` → `{statements:[{display, readOnly}], confirm, password, atomic}` | `buildChanges` → `Plan` → gate |
| `POST …/plan/admin {op, ref, opts}` | `PlanAdmin` → gate |
| `POST …/apply {statements? planId, password?}` | re-check gate server-side, `Apply` (uses the plan cached under `planId` so the browser cannot submit arbitrary SQL through the GUI path; arbitrary SQL goes through `/exec`, which is gated the same way) |
| `GET …/history?q=&limit=` · `DELETE …/history/{id}` | `store.History` |
| `GET /queries` · `POST /queries` · `PUT /queries/{path}` · `DELETE /queries/{path}` | `store.Queries` |
| `GET/PUT /state/table?conn=&db=&table=` | `store.State.Table` / `SetTable` (shared widths/filters/sort with the TUI) |
| `GET /meta` → `{operators: db.Operators(dialect) per dialect, safeModes: safemode.All, config: General+Editor+Table}` | static |

`/exec` gating (the TUI gates editor runs too, `queryTab.run`, `internal/ui/tab_query.go:250`):
the server runs `db.Split`, computes `readOnly` for the batch, and if the level
requires confirm/password it returns `409 {gate:{confirm,password}, statements}` instead of
running; the browser re-posts with `confirmed: true` / `password`. One code path, same as GUI.

## Frontend (`web/`)

Stack, deliberately short: Vite + React + TypeScript, `@tanstack/react-router` (file routes),
`@tanstack/react-query`, `@tanstack/react-table` + `@tanstack/react-virtual`, CodeMirror 6
(`@codemirror/lang-sql`). Plain CSS with variables (dark/light, mirrors `ui/theme.go` names).
No UI kit, no state library (Query cache + a small `useReducer` per table tab is enough).

Routes:
- `/` — connection list grouped like the sidebar (`config.Groups`), connect/test/edit/new form
  (fields = `config.Connection`, URL paste → `POST /connections/parse` is **not** added; the
  form has the same fields the TUI welcome form has).
- `/s/$sessionId` — workspace: left sidebar (databases select, schemas, objects tree with
  filter box, pinned/recent from state), tab strip, main pane, bottom console (statement log).
  - **Table tab**: filter bar (column / operator from `/meta` / value, AND/OR, enable toggles),
    sort by header click, hidden columns, page size = row limit, offset paging, count badge
    (estimate then exact on click), virtualized grid, cell edit on double-click, row
    insert/duplicate/delete, pending-change count, **Preview** (shows statements from `/plan`),
    **Commit** → gate dialog (confirm or password) → `/apply` → reload. Json/Tree view when
    `caps.Document`.
  - **Query tab**: CodeMirror, Run current (statement under cursor via `db.StatementAt` ported
    to TS? no — send cursor offset, server returns the split; keep splitting in Go), Run all,
    row-limit picker, results as sub-tabs (`cfg.Editor.ResultMode`), cancel via AbortController,
    save to queries, open from history/queries panes.
  - **Object actions** (context menu): DDL, Copy script as…, truncate/drop/rename/clone →
    `/plan/admin` → same gate dialog → `/apply`.
- Token: read once from `location.hash`, kept in `sessionStorage`, sent as Bearer header.

## Milestones (each ends green and usable)

1. **API skeleton + serve** — `main.go` split, `internal/web` with sessions, meta, rows/count,
   exec, static stub. Test: `httptest` against an in-package `fakeSession` (copy the shape of
   `internal/ui/fake_test.go`) covering token auth, rows → cells formatting, exec splitting and
   gate 409, plan/apply building Changes incl. special values and no-PK key fallback. Integration
   test (`TUIDB_INTEGRATION=1`) against compose postgres: open → rows → edit → apply → verify.
2. **Web scaffold + browse** — Vite app, router, connections page, workspace with sidebar and
   read-only table tab + query tab. Dev: `vite` proxies `/api` to `:7788`.
3. **Edit + gate + admin** — pending edits, preview/commit, gate dialog, admin ops, history and
   saved queries panes, table state round-trip with the TUI.
4. **Embed + build** — `make web` (npm ci && vite build → `internal/web/dist`), `go:embed`,
   `--open`, README section. CI builds web before `go build`.

## Files

- Modify: `main.go`, `Makefile`, `README.md` (--frontend section), `.gitignore` (`web/node_modules`,
  built `dist` except stub).
- New: `internal/web/{server,sessions,changes,handlers_meta,handlers_data,handlers_store,static}.go`,
  `internal/web/{server,changes}_test.go`, `internal/web/postgres_integration_test.go`,
  `internal/web/dist/index.html` (stub), `web/` (Vite project: `src/routes/*`, `src/api.ts`,
  `src/components/{Grid,FilterBar,Editor,GateDialog,Sidebar}.tsx`, `src/styles.css`).
- Reused, unchanged: `db.Session`, `db.Split`, `db.IsReadOnly`, `db.FormatValue`,
  `db.Operators`, `safemode.Gate`, `ui.DefaultConnect`, `store.{History,Queries,State}`,
  `config.{LoadConnections,SaveConnections,Groups}`.

## Verification

- `make test` (vet + unit, including new `internal/web` tests).
- `make up && make smoke` runs the new postgres integration test alongside the existing ones.
- Manual: `go run . --frontend --open`, in the browser pane: connect to compose postgres, open a
  table, filter + sort, edit a cell, Preview shows the UPDATE, Commit with Safe Mode 2 asks for
  the password, row changes; run `select 1; drop table nope;` in the editor and see the gate
  409 → confirm dialog; then open the TUI (`go run . postgres-local`) and confirm the same
  history entry and filter state appear.
- `cd web && npm run build && npx tsc --noEmit`.

## Risks / shortcuts

- `shortcut: cells are display strings (FormatValue); edited values are sent as strings like the TUI does. Upgrade to typed cells if an engine rejects text params for some type.`
- `shortcut: result cache is unbounded per session apart from the row limit; evict on tab close. Add an LRU if memory shows up.`
- Local-only by design: binds loopback, random bearer token, Origin check. Binding elsewhere is
  refused unless `--insecure-listen` is passed (documented as "credentials go over the wire").
- Structure editing (`PlanTable`), process list, and query autocomplete are out; the TUI keeps them.
