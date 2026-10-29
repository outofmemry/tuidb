# Super test: fix status (2026-10-29)

Findings come from `supertest-web.md` (W-) and `supertest-tui.md` (T-). Fixes were made in three headless
claude-work jobs on top of the uncommitted working tree, then checked here: `go vet ./...`, `go test ./...`,
`gofmt`, `npx tsc --noEmit`, `npm run test:keys`, `node --test web/test/*.test.mjs` (70 pass), `make web`,
`go build`. Nothing is committed.

Checked by hand after the fixes: W-P1-01, W-P1-02, W-P2-01, W-P2-05, W-P2-13, W-P2-14, the schema switch
(W-P2-07 / T-P2-04), the BEGIN auto-pin with its "transaction open" chip and close guard (T-P1-03 / T-P2-05 /
W-P2-06), and follow foreign key in the terminal (T-P1-01).

## Shared engine (both UIs)

| Finding | Status |
|---|---|
| W-P1-03 MongoDB dates shift by the server's UTC offset on edit | Fixed: the Mongo driver hands every date to the grid in UTC, matching the JSON view and `parseDate`; round-trip test added. Other drivers unchanged. |
| T-P1-02 / W-P2-08 completion after `schema.` or `alias.` | Fixed in both UIs: the qualifier stays, and `alias.` offers the columns of the aliased table in its schema. |
| T-P2-04 / T-P2-09 / W-P2-07 / T-P3-21 schema switch is sidebar-only | Fixed: switching the schema sets the session schema (`search_path` on Postgres) through a shared call and a new `POST /api/sessions/:id/schema`; editor, search, Open anything, completion, status bar, schema diff and `schema=` URLs follow it; the web keeps it across reloads; search hits are labelled `schema.table` outside the default schema. |
| T-P1-03 / T-P2-05 / W-P2-06 transactions | Fixed: a `BEGIN` on an unpinned tab keeps one connection until COMMIT/ROLLBACK (toast says so); `[tx]` in the terminal tab title, "transaction open" chip on the web; closing, unpinning or quitting with a transaction open asks first. |
| T-P2-07 / W-P2-19 help and `--keys` parity | Fixed: `--keys` lists the Console context; the web help follows the terminal rule (focused context, then global). |
| T-P2-01 paging without a sort is not stable | Fixed for Postgres and MySQL/MariaDB: pages are ordered by the primary key when no sort is chosen. Cassandra cannot (no ORDER BY across partitions); ClickHouse left as is. |
| T-P2-08 / T-P2-13 serial → IDENTITY in DDL, export and diff; diff FKs point at the TO schema | Fixed: serial columns keep their `nextval` default, the SQL export appends `setval` after the data, and schema-diff foreign keys are rebased to the FROM schema (tests added). |
| T-P2-02 `--mcp` dies on one malformed line | Fixed: JSON-RPC parse error (-32700) is answered and the server keeps serving; pending answers are flushed. |
| T-P3-02 / web "double-escaped SQL" in errors | Fixed: errors show the database's text plus a `Statement:` line, no `%q` escaping. |
| T-P3-10 / web error position | Fixed: Postgres errors end with "at line L, column C" in both UIs. |

## Web UI

| Finding | Status |
|---|---|
| W-P1-01 any SQL error crashes the app | Fixed: failed statements are normalised to empty cells/columns (`utils/results.ts`, unit test) and the error shows in the result tab. |
| W-P1-02 New table designer drops every edit | Fixed: the draft is initialised once while creating. |
| W-P2-05 Cancel on Preview CREATE closes the designer | Fixed: the preview opens over the designer. New: Escape/✕/outside click now asks "Discard the new table?" once the draft changed. |
| W-P2-01 / W-P2-02 limits above `row_limit` | Fixed: `/api/meta` exposes `rowLimit`; page size is capped with "capped at 300 per page by row_limit" in the footer and Next stays usable; the query Limit menu stops at the configured limit; the server marks a clamped page `truncated`. |
| W-P2-03 switching Data/Structure/DDL loses structure edits | Fixed: asks first. |
| W-P2-04 MySQL column reorder | Fixed: rows move in the editor and every move is planned. |
| W-P2-09 native `window.confirm` | Fixed: the app confirm lives in `utils/prompts.ts` and is used everywhere; confirm buttons name the action. |
| W-P2-10 functions and sequences open nothing | Fixed: they open their definition in a query tab. |
| W-P2-11 Copy link misses the live filter | Fixed. |
| W-P2-12 deep link without the token hammers the server | Fixed: a plain "This link needs the access token…" page; pollers stop on 401. |
| W-P2-13 MongoDB without credentials opens as connected | Fixed: the open fails and the start page explains it; Redis `NOAUTH` wording mapped too. |
| W-P2-14 Settings: Escape dead after Save | Fixed: window-level Escape (top dialog only), focus returns into the dialog. |
| W-P2-15 connection form loses typing on Escape/backdrop | Fixed: asks when the form changed. |
| W-P2-16 masked URL field mangles a paste | Fixed: select-all on focus plus a "Paste URL…" button. |
| W-P2-17 invalid colour accepted | Fixed: form and server accept `#rgb`/`#rrggbb` (names are converted). |
| W-P2-18 Beautify/Uglify reformat the whole buffer | Fixed: the statement under the cursor, same rule as the terminal. |
| Web P3 list | All done except two that were already correct (Columns search above 8 columns, "Nothing matches" in Open anything). "Save needs name and host" was not added: the server only needs a name. |
| New: autocomplete popup stays open on blur / over dialogs | Fixed. |

## Terminal UI

| Finding | Status |
|---|---|
| T-P1-01 follow foreign key opens the table unfiltered | Fixed (flow test). |
| T-P1-04 "taking too long" prompt outlives the query, Cancel lies | Fixed: the prompt closes when the statement finishes, Cancel after the fact says so, timing excludes the prompt. |
| T-P2-03 filter summary shows the unapplied filter | Fixed: summary, SQL popover and export use the applied filter and mark "not applied". |
| T-P2-06 `[keys]` override silently unbinds another action; hard-coded hints | Fixed: start-up and `--keys` warn; frame, structure and welcome hints render from the keymap. |
| T-P2-10 Variables dialog says "Enter run" but only saves | Fixed: saves and runs with the values bound; hint when `query_params` is off (both UIs). |
| T-P2-11 edited `.sql` file tab closes without asking | Fixed: `●` mark, Save / Don't save / Cancel on close and quit, vim `:q!` and `:w`. |
| T-P2-12 "copied" with no clipboard | Fixed: `pbcopy` / `wl-copy` / `xclip` / `xsel` / `clip` first, then OSC 52, else "Could not copy". |
| T-P3-01 … T-P3-22 | All done: failure icon on refusals, word wrap at spaces, dialogs keep input on a validation error, connection-form discard asks, numbered multi-sort markers, tabs in previews, Ctrl+C says how to quit (lock screen too), sidebar `1 2 3`, read-only/assistant hints, filter-bar Enter applies, negative timeout refused and `[theme.colors]` warnings, backup form says the tool is missing (and still opens, so Ctrl+t can set the path), hex-edit wording, `--check-update` with no releases, JSON copy keeps column order and INSERT names the table, "Could not load objects", bulk-update preview applies with Enter and counts rows. |

## Known gaps

- Web "Copy as INSERT" from a result that cannot be edited (no primary key in the select) still says `table_name`: the server only reports the table for editable results.
- Cassandra and ClickHouse still page without an order when no sort is chosen.
- The start page shows a stale `open` / Resume badge for workspaces from a server that has since restarted (clicking lands on "This session has ended"). Not in the reports; found while verifying.
- Not retested by hand after the fixes: everything not listed under "Checked by hand" above (covered by the new unit and flow tests only).
