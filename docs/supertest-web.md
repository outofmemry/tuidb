# Super test: web UI (2026-10-29)

- **Build:** `051da0e`, uncommitted changes present (32 modified files and 2 untracked ones, per `git status`). The embedded bundle `internal/web/dist/assets/index-DX7r8FOc.js` (built 00:06) is newer than every file under `web/src`, so it matches the current source.
- **Server:** production build served by the Go server at `http://127.0.0.1:7799/#token=devtoken`. Prefix `C-d`, `row_limit` 300, `query_timeout_secs` 300.
- **Browser:** built-in browser pane of the Claude desktop app (Chromium, MacIntel), emulated at 1280×860, at 375×812 (mobile preset) and at the pane's own width.
- **Data:** PostgreSQL 16.15 `shop (postgres)` (Silent), MySQL 8.4.13 `mysql test` (Alert Mode 2), MongoDB 7.0.45, Redis 7.4.12, and MariaDB 11.4.13 (added for the test).
- **Method:** I drove the UI by real clicks and typing where the tool allows it, and otherwise by DOM events: synthetic `KeyboardEvent` chords for keys the tool cannot send, `DragEvent` for HTML5 drag, React-compatible `input` events for some fields. I verified state through the DOM and the API, checked the server code for each finding, and reproduced each finding at least twice unless it is marked "seen once".
- **Environment limits** (details under "Could not test"):
  - The host consumes ⌘ shortcuts.
  - Clipboard writes are denied.
  - I intercepted downloads in-page (`HTMLAnchorElement.click`) so nothing reached disk.
  - `window.open` replaces the current tab.
- **Clean-up:** the test connections (`maria test`, `mongo auth`, `redis ro`), the saved query, the metrics board and the temporary tables are removed. The group name is back to `Local` and `mysql test` is back on `alert2`. `connections.toml` now differs from the original only in key order.

## Findings

### P1

#### W-P1-01 Any SQL error in a query tab crashes the whole web app ("Something went wrong!")
- **Where:** `web/src/components/query/QueryTab.tsx:539` (`cur.cells.map`) and `:551` (dependency list `cur?.cells.length`). The server sends `"cells":null,"columns":null` for a failed statement (`POST /api/sessions/{id}/exec`, `execItem` in `internal/web/handlers.go`).
- **Steps:** connect `shop (postgres)` → New query → `select x from nope` → Run.
- **Other triggers that crash the same way:**
  - Run all on `select 1; select 'two'; select x from nope;`.
  - Saved queries drawer → Run on `select count(*) from shop.orders where status = :status`, with `[editor] query_params = false`.
  - Any statement that fails at the server, for example a history entry like `kill process 30976` (code path; not clicked).
- **Expected:** the error shows in the result tab. PLAN M1: "errors with positions"; plan-frontend: exec returns `error` per statement.
- **Actual:** the page is replaced by `Something went wrong!` / `Show Error`. The console shows `TypeError: Cannot read properties of null (reading 'length') at qF (index-DX7r8FOc.js:90:21505)`. A page reload restores the tabs, but the result and the editor state are gone.
- **Notes:** reproduced 3 times with 3 different triggers. Because every query error kills the app, I could not test error positions, `return_on_error` or the slow-query prompt.

#### W-P1-02 New table designer reverts every edit: only the default `id integer` table can be created
- **Where:** `web/src/components/grid/StructureEditor.tsx:44-51`. `base` is a `useMemo` that depends on `columns`. While creating a table, the effect `if (loaded && seen.current !== base) setD(base)` resets the draft on every render.
- **Steps:**
  1. Sidebar `+` → New table… → name `webtest_new3` → OK.
  2. Click "Add column" (real click and JS click). Nothing is added.
  3. Triple-click "Column 1 name" and type `pk_id`. The field shows `id` again.
  4. Preview CREATE. It always produces `CREATE TABLE "public"."webtest_new3" ("id" integer NOT NULL, PRIMARY KEY ("id"))`.
- **Expected:** PLAN M12 item 1 says "add / rename / delete columns, type, nullability, default …, new table".
- **Actual:** column, index and foreign-key edits and renames are all discarded immediately.
- **Notes:** reproduced 3 times on PostgreSQL. The editor for an existing table keeps its edits (renaming `stock` to `stock_qty` worked).

#### W-P1-03 MongoDB dates are shown in the server's local time without a zone, but edits are written as UTC: one edit shifts the stored date
- **Where:**
  - `internal/db/format.go:28` formats a `time.Time` as `2006-01-02 15:04:05.999999` in its own location; the Mongo driver returns local time.
  - `internal/db/mongo/bsonx.go:120-128` (`parseDate`) parses a zone-less string as UTC.
- **Steps:**
  1. Add a MongoDB connection with credentials (`mongodb://webtuidb:…@127.0.0.1:57017/webtuidb?authSource=admin`) and open `customers`.
  2. The grid shows `created_at` = `2024-01-02 08:34:05`; the JSON view of the same document shows `{"$date": "2024-01-02T03:04:05Z"}`. The machine is on +05:30.
  3. Edit the cell to `2024-01-02 08:34:06` → Preview SQL.
- **Expected:** a one-second change.
- **Actual:** `db.customers.updateOne({"_id": {"$numberLong": "1"}}, {"$set": {"created_at": {"$date":"2024-01-02T08:34:06Z"}}})`. The stored instant moves by +5 h 30 min.
- **Notes:**
  - Not sent (discarded).
  - Seen once in the UI. The cause is confirmed in code, and the grid/JSON mismatch was visible on all 3 rows.
  - `FormatValue` is shared, so the terminal UI is probably affected the same way; the sibling tester should confirm.
  - Any server whose time zone is not UTC corrupts every date it edits.

### P2

#### W-P2-01 Rows per page above `row_limit`: rows past 300 cannot be reached, and nothing says so
- **Where:**
  - The server clamps the limit (`internal/web/handlers.go:450` `ss.clampLimit`, `internal/web/sessions.go:283-288`).
  - The client keeps its own limit (`web/src/components/grid/DataTab.tsx:581-582`) and disables Next when `cells.length < limit` (`:574`).
- **Steps:** `shop.customers` (340 rows) → Rows per page `500` (or `99999`) → Enter.
- **Expected:** 340 rows, or a message that the page is capped at 300.
- **Actual:**
  - The footer shows `‹ Prev 1–300 of ≈340 Next ›`, Next is disabled, and the "stopped at the row limit" chip is absent.
  - `POST /rows` with `limit: 99999` returns 300 cells and `truncated:false`.
  - Rows 301–340 cannot be reached until the user lowers the number.
- **Notes:** reproduced with 500 and 99999, in the UI and through the API.

#### W-P2-02 Query tab "Limit" options 1000 / 10000 / No limit are silently capped at 300
- **Where:** `internal/web/sessions.go:283` (`n <= 0 || n > rl → rl`), used by `/exec` and `/exec/stream`.
- **Steps:** Query tab → Limit ▾ → No limit → `select * from shop.orders` (1500 rows) → Run.
- **Expected:** the selected limit, or options that respect `row_limit`.
- **Actual:**
  - `300 rows in 106 ms · stopped at the row limit`.
  - Through the API: `limit` 1000, 10000 and 0 all return 300 rows (`truncated:true`), and `limit` 100 returns 100.
  - "Load more" does work (600).
- **Notes:** reproduced twice. The dropdown offers values that cannot take effect.

#### W-P2-03 Switching Data / Structure / DDL silently throws away unsent structure changes
- **Where:** `web/src/components/grid/DataTab.tsx:487` calls `setView` with no guard. `StructureEditor.tsx:55` says leaving the view "drops the draft".
- **Steps:** `shop.products` → View: Structure → rename `stock` to `stock_qty` and Add column `note` (the tab shows ● and "Preview changes" lists 2 statements) → View: DDL (or Data) → back to Structure.
- **Expected:** a confirmation first. DESIGN.md: "Anything that loses work asks first".
- **Actual:** no prompt; the rename and the new column are gone and the ● disappears.
- **Notes:** reproduced twice (to DDL and to Data).

#### W-P2-04 MySQL column reorder: ▲/▼ show nothing, and each move replaces the previous one
- **Where:** `web/src/components/grid/StructureEditor.tsx:103` (`move` → `onPlan(planMoveColumn(...))` per click) and `:147-148`.
- **Steps:** `mysql test` → `customers` → Structure → ▼ on `email`, then ▼ on `balance` → Preview changes.
- **Expected:** the rows move in the editor and both moves are planned. PLAN M15 item 23: "drag columns to reorder where `CanReorderColumns`".
- **Actual:**
  - The rows stay in place (`id,name,email,notes,balance,created_at,meta`).
  - The preview holds only the last move: `ALTER TABLE customers MODIFY COLUMN created_at timestamp NULL DEFAULT CURRENT_TIMESTAMP AFTER notes`.
  - The `email` move is silently dropped.
- **Notes:** reproduced twice (▲ on `created_at` then ▼ on `id`, and the pair above). Nothing was sent.

#### W-P2-05 New table: Cancel on the preview closes the whole designer
- **Where:** New table flow (`StructureEditor` in creating mode → plan dialog).
- **Steps:** Sidebar `+` → New table… → Preview CREATE → Cancel (or ✕).
- **Expected:** back to the designer to adjust it.
- **Actual:** both dialogs close and the design is lost.
- **Notes:** reproduced twice. Combined with W-P1-02, the designer cannot be iterated at all.

#### W-P2-06 Unpinning a query tab with an open transaction rolls it back with no warning
- **Where:** query tab Pin button (`DELETE /api/sessions/{id}/pin/{tab}`).
- **Steps:** New query → Pin → `begin; update shop.products set name='TX TEST' where id=5;` → Run all → click "Pinned".
- **Expected:** a warning that the open transaction will be rolled back, plus a visible "transaction open" state while pinned. The task asked to check "BEGIN … then see the pending state".
- **Actual:**
  - Toast `Connection released`.
  - The update is rolled back (product 5 is still `Product 5`; no `idle in transaction` session is left).
  - While pinned, the only indication is the "Pinned" button and `This tab now keeps one connection: BEGIN and SET carry over.`
- **Notes:** reproduced twice (ids 1 and 5).

#### W-P2-07 The sidebar schema switch is cosmetic: search, Open anything, autocomplete and the status bar keep using the session schema
- **Where:**
  - The sidebar keeps its own state (`Sidebar.tsx:35`).
  - Everything else uses `info.schema`: `Tools.tsx:239,246` (search), `routes/Workspace.tsx:754` (palette), `QueryTab.tsx:120` (completion objects), `routes/Workspace.tsx:671` (status bar).
- **Steps:** sidebar Schema → `shop`, then:
  1. `C-d /` → search `Zürich`, all tables.
  2. `C-d p` → `products`.
  3. Look at the status bar.
- **Expected:** the search finds the 42 customers in `shop.customers`. PLAN M9 item 9 also asks for "Search in Database across all schemas in one run, results grouped by schema".
- **Actual:**
  - `4/4 tables. No matches.` (it searched `public`).
  - Open anything returns nothing for `products` or `shop.cust`.
  - The status bar still reads `webtuidb.public`.
  - The search dialog has no schema option.
  - The sidebar schema also resets to `public` on reload and on browser back/forward.
- **Notes:** reproduced twice (search before and after switching).

#### W-P2-08 Autocomplete after `alias.` ignores the schema and offers another table's columns
- **Where:** `web/src/components/query/Completion.ts:43` (`aliasesIn` drops the schema: `.split('.').pop()`).
- **Steps:** Query tab → type `select * from shop.customers c where c.me`.
- **Expected:** columns of `shop.customers` (id, name, email, city, vip, …). PLAN M11 item 1 asks for "alias.column".
- **Actual:**
  - The list offers `meta` and `name`; `meta` exists only in `public.customers`.
  - `c.ci` / `c.cit` offers nothing (`city` exists only in `shop.customers`).
  - The network log shows `object/columns?schema=public&name=customers`.
- **Notes:** reproduced twice.

#### W-P2-09 Native `window.confirm` is used in three places; in the built-in browser it returns false, so the actions silently do nothing
- **Where:** none of these files imports the app's own `confirm`.
  - `web/src/components/grid/SetValueDialog.tsx`: Set value → Advanced "Use expression", and "Use nextval".
  - `Extras.tsx:25`: safe-mode change on a connection with no stored password.
  - `web/src/utils/api.ts:117`: saved query "already exists. Replace it?".
- **Steps:** right-click a cell → Set value… → Advanced `stock + 1` → Use expression.
- **Expected:** the app's own confirm dialog (DESIGN.md: one dialog style; `Dialogs.tsx:53` says `ask`/`confirm` replace the window ones).
- **Actual:** no visible dialog, the Set value dialog stays open and nothing is staged. With `window.confirm` stubbed to return true, the change is staged (`stock on id 1: 1 stock + 1`).
- **Notes:** reproduced twice. In a normal browser the user gets an unstyled native box instead.

#### W-P2-10 Functions and sequences: a click, "Open" and "Open in new tab" do nothing
- **Where:** `routes/Workspace.tsx:250` (`openObj` returns early for kinds other than table/view/matview).
- **Steps:** sidebar → click `customer_order_count` (function) or `ticket_seq` (sequence); also right-click → Open / Open in new tab.
- **Expected:** open its definition (PLAN M1: "views/matviews/functions/procedures (DDL editor)"), or leave those items out of the menu.
- **Actual:** no tab, no dialog and no message. "Show DDL" and "Edit definition" do work.
- **Notes:** reproduced twice.

#### W-P2-11 "Copy link" on a table tab leaves out the filter currently applied
- **Where:** `routes/Workspace.tsx:408` uses `t.filters` (the filters the tab was opened with, from a deep link), not the tab's live filter state.
- **Steps:** `shop.customers` → filter `city = São Paulo` → Apply → tab ▾ → Copy link.
- **Expected:** `/s/<sid>/t/shop/customers?filter=…` (PLAN M15 item 19).
- **Actual:** copied text `http://127.0.0.1:7799/s/b3efc72db14d00be/t/shop/customers`.
- **Notes:** seen once in the UI (text read from the `writeText` argument). The code confirms it.

#### W-P2-12 A deep link opened without the token shows a blank page and hammers the server with 401s
- **Where:** app root (`web/src/main.tsx:21-25`). A 401 without `login` renders the normal tree, which keeps refetching.
- **Steps:** open a fresh browser tab at `http://127.0.0.1:7799/s/<sid>/t/shop/customers?filter=…` with no `#token=`. This is the link "Copy link" produces, by design without the token.
- **Expected:** a page that says the token is missing and how to get it (the URL that `webtuidb --frontend` prints).
- **Actual:**
  - An empty `#root`.
  - The 250-entry resource buffer filled in under 3 s: `/api/meta` ×84, `/api/sessions/<sid>` ×79, `/jobs` ×44, `/api/mcp/approvals` ×43, all 401.
  - The console had about 14,900 errors within seconds.
- **Notes:** seen once (I closed the tab to stop the load).

#### W-P2-13 A MongoDB connection that needs authentication opens as connected and shows raw driver text
- **Where:** workspace open for `mongo docs` (deliberately configured without credentials).
- **Steps:** start page → `mongo docs` → Connect.
- **Expected:** a clear failure ("The user name or password was not accepted…", which `hint()` in `Connections.tsx:416` already has), with the driver text behind "Technical details".
- **Actual:**
  - The workspace opens with a green ● in the status bar and the chip `MongoDB 7.0.45`.
  - The sidebar shows `list collections: (Unauthorized) Command listCollections requires authentication` with Retry.
  - `GET /objects` and `GET /databases` return 500, and the database switcher is missing.
- **Notes:** reproduced twice (Connect, then Resume).

#### W-P2-14 Settings: Escape and the Tab trap stop working after Save
- **Where:** `web/src/components/shell/Dialogs.tsx:32-38`. Escape is handled by `onKeyDown` on the dialog box. After Save, the focused Save button becomes disabled and focus drops to `<body>`.
- **Steps:** start page → Settings → tick `log_original_values` → Save → press Escape.
- **Expected:** the dialog closes (DESIGN.md: menus and dialogs close on Escape) and focus stays inside the dialog.
- **Actual:** nothing happens; `document.activeElement` is `BODY`. The same happens after a keymap row's Save.
- **Notes:** reproduced twice.

#### W-P2-15 The connection form closes on Escape, a backdrop click or Cancel and loses the typed data without asking
- **Where:** `web/src/routes/Connections.tsx:229` (`onClose` → `setEdit(null)`); `Dialogs.tsx:33` (Escape) and `:41` (backdrop `onMouseDown`).
- **Steps:** Add connection → type a name (or fill the whole form) → press Escape, or click outside the dialog.
- **Expected:** "Discard the changes to this connection?" (DESIGN.md: anything that loses work asks first).
- **Actual:** the form closes immediately and everything typed is gone. One stray click outside the large form loses it.
- **Notes:** reproduced twice (Escape; the code confirms Cancel and the backdrop).

#### W-P2-16 Connection form: the URL field is pre-filled with a generated URL and masked, so pasting into it produces a mangled URL
- **Where:** `web/src/routes/Connections.tsx:284-286`: `type="password"`, `value={url}` kept in step with the fields by `/connections/format`.
- **Steps:** Add connection → click into "Connection URL" → paste or type `mariadb://webtuidb:webtuidb@127.0.0.1:53307/webtuidb`.
- **Expected:** the pasted URL replaces the field and fills the form.
- **Actual:** the field already held `postgresql://127.0.0.1/?name=`, invisible behind the mask. The text went in at the caret: `postgresql://127.0.0.1/?mariadb://webtuidb:webtuidb@127.0.0.1:53307/webtuidbname=`.
- **Notes:** seen once; the code confirms it. It works after select-all.

#### W-P2-17 An invalid connection colour is accepted and saved
- **Where:** the connection form `Color` field → `PUT /api/connections` (no validation).
- **Steps:** Add connection → Color `notacolor` → Save.
- **Expected:** refused with a message, or a colour picker.
- **Actual:** saved as `color = "notacolor"`, and copied URLs carry `statusColor=notacolor`.
- **Notes:** seen once.

#### W-P2-18 Beautify / Uglify reformat the whole buffer, not the statement under the cursor (parity)
- **Where:** `web/src/components/query/QueryTab.tsx:341-344` (`target()` returns the whole document when nothing is selected); compare `internal/ui/query_params.go`.
- **Steps:** editor `select … order by 1;select 1`, caret in the second statement → Beautify.
- **Expected:** only `select 1` is reformatted (PLAN M9 item 6, both UIs).
- **Actual:** both statements are reformatted.
- **Notes:** reproduced twice (Beautify and Uglify).

#### W-P2-19 The web help listing differs from the terminal help (CLAUDE.md asks for identical listings)
- **Where:** the web help dialog lists every context, `global` first. `internal/ui/help.go:46` lists the current context, then `global`, and nothing else.
- **Steps:** `C-d ?` in both UIs from the grid.
- **Expected:** CLAUDE.md: "same action names, same descriptions, same order".
- **Actual:** the web shows 183 rows grouped by context; the terminal shows a context-dependent subset in a different order.
- **Notes:** code comparison plus the web dialog. The sibling TUI tester can confirm the terminal side.

## UX friction (P3)

- **Connection form:**
  - Save is disabled with no hint about what is missing.
  - Test with an empty form says `connection needs a name`; a test should not need a name.
  - Labels are not tied to their inputs (`field()` renders `<label>` with no `htmlFor`), so screen readers announce unnamed text boxes.
  - Opened from a pasted URL, the form is titled `Edit 127.0.0.1/webtuidb` although the connection is new.
  - An invalid pasted URL reads `That URL isn't valid: unsupported URL scheme ""`.
- **Confirmations:** delete connection, kill process and discard all use a generic `Continue` button instead of naming the action (`Delete`).
- **Clipboard errors** show raw DOM text: `Could not copy: Failed to execute 'writeText' on 'Clipboard': Write permission denied.` and `NotAllowedError: …`.
- **Recent files** right after opening a `.sql` file in the browser: `No file was opened yet.` (`Extras.tsx:334`). The wording contradicts what just happened.
- **Settings:**
  - Labels are raw config keys (`estimate_count_over`).
  - Section names are lowercase while `Keymap` is capitalised.
  - `default_safe_mode` is a free-text field whose hint says `(silent, alert, ...)`, but `alert` is rejected.
  - Validation errors appear one at a time.
  - After saving the theme it says `Saved. Restart webtuidb for these to apply.` although the theme already applied.
  - There is no `[ai]` section, yet the assistant tells the user to `set [ai] api_key or api_key_cmd in the config`.
- **Start page** has no key bindings at all: `C-d ?` does nothing there.
- **Sidebar:**
  - "Recent" shows `orders` twice (`public.orders` and `shop.orders`) without the schema.
  - A dropped table stays in Recent.
- **Grid paging:**
  - Rows per page `0` or `-1` is silently ignored while the field keeps showing it.
  - The Row limit dialog ignores offset `-10` without a message.
  - After a query error the footer reads `‹ Prev 0–0 Next ›`.
- **Filters:**
  - `BETWEEN 10,50` is refused (`needs two values: low AND high`) while IN takes commas.
  - The Columns popover has no search, which matters on `wide` with 22 columns.
- **Grid editing:**
  - After Send, the edited row jumps to the end of the page (no stable order without a sort).
  - Add row appends the new row below the last loaded row and neither scrolls to it nor focuses it.
  - On an empty table the grid hides the column headers ("This table has no rows yet").
- **Error toasts** show double-escaped SQL: `apply "UPDATE \"shop\".\"orders\" SET …": ERROR: …`.
- **Quick look on `wide.blob`** (8-byte PNG header):
  - The Image view shows a broken image with no message.
  - The Text view says `0 characters` for 6 characters.
- **Design-rule slips** (DESIGN.md: no all-caps labels, no middle-dot joins):
  - `PRETTY` / `MINIFY` buttons are all caps.
  - Header tooltips use `·` joins (`text · nullable · no default · no key`).
- **JSON export** writes `jsonb` as an escaped string (`"attrs": "{\"n\": 1, …}"`) instead of nested JSON.
- **Import:** after a failed import the dialog offers only Close; to retry with other options the user must reopen it and pick the file again.
- **Structure editor:** "Add column" on an existing table defaults the type to `integer` without saying so.
- **Cancel** (`pg_sleep(30)`) leaves the previous run's result in the pane, then opens a process list that reads `No other processes.`
- **Saved queries:**
  - After Save query the tab keeps the title `Query 2`.
  - The name `webtest/orders by status` is saved as `webtest_orders_by_status.sql`, not into a folder.
- **Wording:** the query results bar says `Commit` where the data tab says `Send changes`.
- **Metrics board:** the result menu's "Add to metrics board…" is disabled (`the web board form takes no query yet`) while the chart toolbar's "Add to Metrics Board" works.
- **Chart:** no y-axis values, and series with very different magnitudes (count vs sum) hide the smaller one.
- **Keys:** `C-d I` in a query tab says `import: nothing to do here`.
- **Open anything** with no match shows an empty list and no message.
- **Redis `NOAUTH Authentication required`:**
  - The start page shows the generic `Check the connection details and try again.` (`hint()` misses this wording).
  - `Connecting to redis cache…` stays on screen next to the failure.
  - From the connection switcher the raw text `connect redis cache: connect: NOAUTH …` appears as a toast.
- **Read-only connection** (`redis ro`):
  - The sidebar offers New table, Import, Clone, Rename, Truncate and Delete.
  - Delete asks for the typed name and a `Cascade: also drop everything that depends on leaderboard? Cancel: no.` question (meaningless for a Redis key), and only then refuses.
  - "Cancel means no" in a confirm is confusing in itself (Sidebar.tsx:92-93, :110, :162).
- **Backup:** "Review command" stays disabled until a file name is typed, while the field's placeholder `{database}-{date}.sql` looks like a default.
- **Assistant:** the chat History dialog has no title (only ✕).
- **Layout:** a 63-character table name gives a 589 px tab with no truncation.
- **Mobile (375 px):** the active tab is not scrolled into view in the tab bar.
- **MongoDB:** collections are listed under "Tables".
- **Table without a primary key** (`no_pk`): the only explanation is the tooltip on the `read only` chip; double-click opens a viewer without saying why it cannot be edited.
- **Connection details:** the label `database` is lowercase among capitalised labels.

## Verified OK

- **M1 core:**
  - Connect / Resume.
  - Sidebar list, filter (fuzzy, `^`, `$`, case-insensitive, `Nothing matches zzz.`), refresh, schema picker, context menus per object kind.
  - Tab reuse rule: a clean table tab is replaced.
  - Paging, Next and Count exactly (`page 2 of 2`).
  - Sort by click, shift-click multi-sort, third click removes the sort.
  - Filter bar: every operator, Any column, Raw condition (`;` refused by the server), AND/OR (6 vs 84 rows), Show SQL (`WHERE "city" = 'Zürich' OR "vip" = 'true'`), Clear all.
  - Cell editor: Tab moves to the next cell with the correct value (P1-02 of the review stays fixed); Escape.
  - Set value NULL / expression; Undo per change; Discard all; Preview SQL.
  - Send on Silent; check-constraint error keeps the pending change.
  - Multi-change send is atomic (a unique violation in change 2 also rolls back change 1).
  - Add / duplicate (PK omitted) / delete row.
  - Reload asks before discarding pending changes.
  - `no_pk` is read only.
  - Views and materialized views are read only.
  - Row detail (`C-d space`); console (`C-d C-c`) with timestamps and Errors-only.
  - Open anything (tables, saved queries, actions, features).
  - Reconnect, disconnect and close-tab guards; `beforeunload` guard.
  - Safe mode: Alert 2 asks before sending on MySQL; Safe 2 → Silent asks for the password; a wrong password shows `Wrong password`; the right one applies and persists.
- **M1 editor:**
  - Run and Run all with `-- name:` result tabs.
  - Message view (`1 row affected`).
  - Cancel `pg_sleep(30)` (backend cancelled).
  - Explain as a tree.
  - Pin (BEGIN carries over).
  - Limit 100, `stopped at the row limit`, Load more (600).
  - Autocomplete for `schema.` objects; Tab accepts.
  - Editable results: single-table select with the PK → edit → Commit (1 pending) writes.
  - History drawer.
  - Save query and Saved queries drawer.
- **M2 data tooling:**
  - Export CSV, JSON, SQL+gzip (current page or whole table) with the `{table}-{date}` name, listed as jobs.
  - Import CSV: header, delimiter auto-detected, quoted comma, `Empty is NULL`, `NULL as NULL`.
  - Bad rows: a short row is an error (`line 2 has 2 fields, expected 4`); a type error rolls back with a clear message.
  - Bulk update needs a filter, then previews `UPDATE … WHERE "id" IN ('1','2')`.
  - Chart (bar/line/pie, `-- barchart x:, y:`); Add to Metrics Board from the chart; process list with kill (confirm, `Killed process 30976`).
- **M3 admin:**
  - Users dialog (roles, attributes, grants).
  - Diagram tab renders (77 SVG nodes).
  - Metrics board tab (board, refresh, rename/duplicate/delete buttons).
  - Assistant explains the missing key.
  - Split right (`C-d "`), close split (`C-d M-q`); tab drag reorder (synthetic DnD).
  - Plugins dialog explains where plugins go.
  - MCP activity explains `[mcp] enabled`.
- **M4/M6/M7 connections:**
  - Sort by Driver (remembered); group Rename; group Delete with typed confirmation (exact name only; a mismatch says `The name did not match; nothing deleted.`).
  - Duplicate (`name copy`); Copy URL without the password; Copy URL with the password asks first.
  - Export with and without a passphrase; Include passwords is disabled without a passphrase.
  - Import: wrong passphrase `wrong passphrase or corrupt file`; existing names skipped; `password_cmd`/`bootstrap` dropped and listed.
  - "Open file…" viewer.
  - Settings: theme applies, safe-mode and `row_limit` validation, keymap editor refuses a taken key (`this takes the key from new_query_tab (e)`).
- **M8–M10:**
  - Connection details dialog.
  - Database switcher (`postgres`, `webtuidb current`).
  - Set value dialog (EMPTY/NULL/DEFAULT/NOW()/gen_random_uuid()/date/file/expression/nextval).
  - Quick look Raw/Tree/Hex/Base64/Image, PRETTY/MINIFY; JSON pretty-print does not stage a change.
  - Hex edit with validation (`Hex needs pairs of 0-9 a-f.`) and `'\x…ff'`.
  - A 20 KB text opens in 0.5 s with `20000 characters`.
- **M11/M12:**
  - Column width drag and double-click reset.
  - Reload restores workspace tabs and the table filter.
  - Structure editor on an existing table: rename and add column preview.
  - DDL view.
  - Redis key folders (`user ▸ 3`).
- **M13/M14:**
  - Schema diff plans 15 statements (app → shop); Data diff and Fake data dialogs open.
  - Backup/Restore dialogs list options and report `pg_dump is not on PATH. Install it, or set its path.`
  - Jobs drawer lists finished and failed jobs.
- **M15:**
  - These keys verified by synthetic chords: `C-d ? p K n k l \ . S / u b B E I e t ] [ w 0 C-c \` " M-" M-q M-0 M-n M-K M-p M-x O d m A`. Each did something or answered with a message.
  - Header menu: sort, filter, hide, copy name/values/definition.
  - Row menu; tab menu (close variants, rename, move, copy link); Escape and outside click close menus.
  - Phone width: no horizontal page scroll, the Objects drawer opens and closes on an outside tap, the ⋯ menu fits, dialogs are full screen.
  - Light theme renders; browser back and forward between workspaces; two workspaces open at once.
  - The Redis read-only connection refuses SET with `this connection is read-only; data-changing statements are refused`, and the key grid is read only.
  - The MongoDB JSON view works.

## Could not test

- **⌘ shortcuts** (⌘Enter, ⌘A in fields): the desktop host consumes ⌘ key events, and Ctrl+Enter does not run on macOS. I used the buttons and synthetic chords instead.
- **Clipboard contents:** writes are denied in this browser. I checked the copied text through the `writeText` argument.
- **Real downloads:** intercepted on purpose so nothing reached disk. File names and contents were checked from the blob.
- **"Open workspace in a new browser tab":** `window.open` replaced the current tab in this browser.
- **Native HTML5 drag with the mouse:** the tool's drag does not start HTML5 DnD; I used synthetic `DragEvent`s.
- **Passcode / lock screen:** not configured (needs `webtuidb -set-passcode`).
- **Assistant with a real key; MCP server** (off); **plugins** (none installed).
- **Backup / restore runs:** `pg_dump`, `mysqldump` and `mongodump` are not on PATH.
- **Slow-query prompt:** `query_timeout_secs` is 300.
- **Error positions and `return_on_error`:** blocked by W-P1-01.
- **Read-only refusal on the saved `redis cache` connection:** the server requires a password the saved connection lacks (`NOAUTH`). I tested on an added `redis ro` connection with the password and `read_only = true`.
- **Editor typing latency:** not measured quantitatively. Typing via the tool showed no visible lag, and grid/table loads were under 0.5 s; nothing took longer than 2 s apart from the deliberate `pg_sleep`.
